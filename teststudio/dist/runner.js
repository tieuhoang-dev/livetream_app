import {redact} from './assertions.js';

export function flattenCollection(collection) {
  const result = [];
  function walk(items, parents, inherited) {
    for (const item of items || []) {
      const events = [...inherited, ...(item.event || [])];
      if (item.item) walk(item.item, [...parents, item.name], events);
      else if (item.request) result.push({
        key: item.name.split(' |')[0], name: item.name.split(' |').slice(1).join(' |') || item.name,
        folder: parents[0] || 'Bộ test', request: item.request, events
      });
    }
  }
  walk(collection.item, [], collection.event || []);
  return result;
}

export class ScriptSandbox {
  constructor(factory = () => new Worker(new URL('./sandbox.worker.js', import.meta.url), {type: 'module'})) {
    this.worker = factory(); this.pending = new Map(); this.counter = 0;
    this.worker.onmessage = event => {
      const item = this.pending.get(event.data.id);
      if (item) { clearTimeout(item.timer); this.pending.delete(event.data.id); item.resolve(event.data); }
    };
    this.worker.onerror = event => { for (const item of this.pending.values()) item.reject(new Error(event.message || 'Lỗi script')); this.pending.clear(); };
  }
  run(input) {
    const id = ++this.counter;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => { this.pending.delete(id); reject(new Error('Script chạy quá thời gian cho phép')); }, 5000);
      this.pending.set(id, {resolve, reject, timer}); this.worker.postMessage({...input, id});
    });
  }
  close() {
    this.worker.terminate();
    for (const item of this.pending.values()) { clearTimeout(item.timer); item.reject(new Error('Phiên đã kết thúc')); }
    this.pending.clear();
  }
}

export class TestRunner {
  constructor(collection, {onEvent = () => {}, requestInput, workerFactory, fetchImpl = fetch, assetBase = new URL('./data/', import.meta.url)} = {}) {
    this.collection = collection; this.all = flattenCollection(collection); this.onEvent = onEvent;
    this.requestInput = requestInput; this.workerFactory = workerFactory; this.fetchImpl = fetchImpl; this.assetBase = assetBase;
    this.results = []; this.cursor = 0; this.status = 'idle'; this.env = {}; this.files = new Map(); this.sockets = new Map();
  }
  emit(type, extra = {}) { this.onEvent({type, cursor: this.cursor, status: this.status, results: this.results, plan: this.plan || [], ...extra}); }
  replace(text, locals = {}) {
    return String(text).replace(/\{\{\s*([^{}]+?)\s*\}\}/g, (all, key) => key === '$randomUUID' ? crypto.randomUUID() : (locals[key] ?? this.env[key] ?? all));
  }
  async ask(input) {
    if (this.stopped) throw new Error('Đã dừng');
    this.status = 'waiting'; this.emit('waiting', {input});
    const answer = await this.requestInput({...input, step: this.plan[this.cursor], cursor: this.cursor});
    if (this.stopped || answer?.action === 'stop') throw new Error('Đã dừng');
    this.status = 'running'; this.emit('running'); return answer;
  }
  pause() { this.pauseRequested = true; }
  stop() { this.stopped = true; this.abortController?.abort(); this.emit('stop-requested'); }
  async start(config, folders) {
    if (this.status !== 'idle') throw new Error('Phiên này đã được bắt đầu');
    this.env = {...config}; this.env.base_url = String(config.base_url || '').replace(/\/$/, '');
    this.env.run_id ||= Date.now().toString(36) + '_' + crypto.randomUUID().slice(0, 8);
    this.env.product_name ||= 'PM_' + this.env.run_id;
    this.env.missing_id ||= crypto.randomUUID();
    const url = new URL(this.backendUrl || this.env.base_url);
    this.backendUrl = this.env.base_url;
    if (!['http:', 'https:'].includes(url.protocol)) throw new Error('URL backend phải là HTTP/HTTPS');
    this.plan = this.all.filter(step => folders.includes(step.folder) && !(config.account_mode === 'existing' && step.folder.startsWith('01 -')));
    if (!this.plan.length) throw new Error('Chọn ít nhất một nhóm test');
    this.startedAt = Date.now(); this.status = 'running'; this.emit('started');
    try {
      while (this.cursor < this.plan.length && !this.stopped) {
        const step = this.plan[this.cursor];
        if (this.pauseRequested) { this.pauseRequested = false; await this.ask({kind: 'pause', title: 'Phiên đang tạm dừng', description: 'Tiếp tục từ đúng bước hiện tại.'}); }
        if (step.key === 'RESET-007' && this.emailReady) {
          this.record(step, {status: 'skipped', reason: 'Case cần Brevo tắt; môi trường vừa gửi OTP thành công.'}); this.cursor++; continue;
        }
        let result;
        try {
          await this.prepare(step);
          if (!step.startedInput) step.startedInput = Date.now();
          this.emit('step-start', {step});
          result = await this.execute(step);
        } catch (error) {
          if (this.stopped) break;
          result = {status: 'failed', error: error.message, tests: [], duration: 0};
        }
        if (this.stopped) break;
        this.record(step, result);
        if (result.status === 'failed') {
          const answer = await this.ask({kind: 'failure', title: 'Bước kiểm tra chưa đạt', description: result.error || result.tests?.find(t => !t.passed)?.message || 'Response khác kết quả mong đợi.', step, result});
          if (answer.action === 'retry') {
            if (step.runtimeBody && step.runtimeBody.includes('"code"')) {
              try { const body = JSON.parse(step.runtimeBody); body.code = ''; step.runtimeBody = JSON.stringify(body); } catch {}
            }
            continue;
          }
          if (answer.action === 'skip-group') {
            const folder = step.folder; this.cursor++;
            while (this.cursor < this.plan.length && this.plan[this.cursor].folder === folder) {
              this.record(this.plan[this.cursor], {status: 'skipped', reason: 'Bạn bỏ qua các bước còn lại trong nhóm.'}); this.cursor++;
            }
            continue;
          }
        }
        this.cursor++;
      }
      this.status = this.stopped ? 'stopped' : 'finished';
    } catch (error) {
      this.status = this.stopped || error.message === 'Đã dừng' ? 'stopped' : 'error'; this.error = error.message;
    } finally {
      for (const ws of this.sockets.values()) ws.close();
      this.sandbox?.close(); this.emit('finished', {error: this.error});
    }
    return this.report();
  }
  record(step, result) {
    const previous = this.results.find(r => r.key === step.key);
    const attempts = previous ? [...(previous.attempts || []), {status: previous.status, code: previous.code}] : [];
    const entry = {key: step.key, name: step.name, folder: step.folder, method: step.request.method, ...result, attempts};
    const index = this.results.findIndex(r => r.key === step.key);
    if (index >= 0) this.results[index] = entry; else this.results.push(entry);
    this.emit('step-result', {step, result: entry});
  }
  async credentials(actor) {
    if (this.env[actor + '_email'] && this.env[actor + '_password']) return;
    const answer = await this.ask({kind: 'credentials', actor, title: actor === 'unverified' ? 'Tài khoản riêng cho case chưa verify' : `Thông tin tài khoản ${actor}`, description: actor === 'unverified' ? 'Dùng email mới, khác seller/buyer. Tài khoản này được cố ý giữ chưa xác thực.' : 'Nhập email và mật khẩu để chạy tiếp.'});
    this.env[actor + '_email'] = answer.email; this.env[actor + '_password'] = answer.password;
  }
  async ensureToken(actor) {
    if (this.env[actor + '_token']) return;
    await this.credentials(actor);
    const response = await this.fetchImpl(this.backendUrl + '/api/users/login', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({email: this.env[actor + '_email'], password: this.env[actor + '_password']})});
    const body = await response.json();
    if (!response.ok || !body.token) throw new Error(body.error || `Không đăng nhập được ${actor}`);
    this.env[actor + '_token'] = body.token;
  }
  async prepare(step) {
    const bearer = step.request.auth?.bearer?.find(a => a.key === 'token')?.value || '';
    const role = bearer.match(/^\{\{(seller|buyer)_token\}\}$/)?.[1];
    if (role && !this.env[role + '_token']) await this.ensureToken(role);
    if (step.folder.startsWith('04 -') && !this.env.live_id) {
      await this.ensureToken('seller'); await this.ensureToken('buyer');
      this.emit('system', {message: 'Tạo phiên live qua WebSocket…'});
      const reply = await this.wsRequest('seller', {type: 'create_live', content: 'Test Studio live'}, 'create_live_success');
      this.env.live_id = reply.id; this.liveOwned = true;
    }
    if (step.folder.startsWith('05 -') && !this.env.ended_live_id) {
      if (this.env.live_id && this.liveOwned) {
        this.emit('system', {message: 'Kết thúc live và kiểm tra chặn tham gia lại…'});
        await this.wsRequest('seller', {type: 'end_live', content: this.env.live_id}, 'end_live_success');
        this.env.ended_live_id = this.env.live_id;
        await this.wsRequest('buyer', {type: 'join_live', content: this.env.live_id}, 'join_live_error');
      } else {
        this.emit('system', {message: 'Chuẩn bị live đã kết thúc qua WebSocket…'});
        const reply = await this.wsRequest('seller', {type: 'create_live', content: 'Test Studio ended live'}, 'create_live_success');
        await this.wsRequest('seller', {type: 'end_live', content: reply.id}, 'end_live_success');
        this.env.ended_live_id = reply.id;
        await this.wsRequest('buyer', {type: 'join_live', content: reply.id}, 'join_live_error');
      }
    }
    if (step.key.startsWith('NEG-')) await this.credentials('unverified');
    let raw = step.runtimeBody ?? step.request.body?.raw ?? '';
    const url = typeof step.request.url === 'string' ? step.request.url : step.request.url.raw;
    if (url.endsWith('/api/users/verify') || step.key === 'RESET-002') {
      const body = JSON.parse(raw);
      if (!/^\d{6}$/.test(body.code || '')) {
        const actor = step.key === 'RESET-002' || body.email.includes('buyer') ? 'buyer' : 'seller';
        const answer = await this.ask({kind: 'otp', actor, email: this.env[actor + '_email'], reset: step.key === 'RESET-002', title: step.key === 'RESET-002' ? 'Nhập mã xác nhận đổi mật khẩu' : `Xác thực email ${actor}`, description: 'Nhập OTP 6 số từ email. Studio giữ nguyên bước này, không đăng ký lại.'});
        body.code = answer.code;
        if (answer.password) this.env.buyer_new_password = answer.password;
        step.runtimeBody = JSON.stringify(body);
      }
    }
    if (step.request.body?.mode === 'formdata') {
      for (const field of step.request.body.formdata || []) {
        if (field.type !== 'file' || this.files.has(field.src)) continue;
        if (field.src.endsWith('.txt')) {
          this.files.set(field.src, new File(['This is not an avatar image.'], 'invalid-avatar.txt', {type: 'text/plain'}));
        } else {
          const answer = await this.ask({kind: 'file', title: 'Chọn ảnh để kiểm tra upload', description: 'Dùng ảnh PNG của bạn hoặc ảnh mẫu đi kèm bộ test.'});
          if (answer.sample) {
            const blob = await (await this.fetchImpl(new URL('avatar.png', this.assetBase))).blob();
            this.files.set(field.src, new File([blob], 'avatar.png', {type: 'image/png'}));
          } else this.files.set(field.src, answer.file);
        }
      }
    }
  }
  async wsRequest(actor, payload, expected) {
    await this.ensureToken(actor);
    let socket = this.sockets.get(actor);
    if (!socket || socket.readyState !== 1) {
      const url = this.backendUrl.replace(/^http/, 'ws') + '/api/auth/ws?token=' + encodeURIComponent(this.env[actor + '_token']);
      socket = new WebSocket(url); this.sockets.set(actor, socket);
      await new Promise((resolve, reject) => {
        const timer = setTimeout(() => reject(new Error('Không mở được WebSocket')), 10000);
        socket.addEventListener('open', () => { clearTimeout(timer); resolve(); }, {once: true});
        socket.addEventListener('error', () => { clearTimeout(timer); reject(new Error('Kết nối WebSocket thất bại')); }, {once: true});
      });
    }
    return new Promise((resolve, reject) => {
      const cleanup = () => { clearTimeout(timer); socket.removeEventListener('message', listener); };
      const listener = event => {
        let body; try { body = JSON.parse(event.data); } catch { return; }
        if (body.type === expected) { cleanup(); resolve(body); }
        else if (body.type?.endsWith('_error')) { cleanup(); reject(new Error(body.error || body.type)); }
      };
      const timer = setTimeout(() => { cleanup(); reject(new Error('WebSocket không trả kết quả đúng hạn')); }, 10000);
      socket.addEventListener('message', listener); socket.send(JSON.stringify(payload));
    });
  }
  async execute(step) {
    this.sandbox ||= new ScriptSandbox(this.workerFactory);
    const scripts = phase => step.events.filter(e => e.listen === phase).map(e => e.script.exec.join('\n'));
    const raw = step.runtimeBody ?? step.request.body?.raw ?? '';
    const pre = await this.sandbox.run({scripts: scripts('prerequest'), env: this.env, locals: {}, request: {raw}});
    if (this.stopped) throw new Error('Đã dừng');
    this.env = {...pre.env, base_url: this.backendUrl};
    if (pre.error) throw new Error(pre.error);
    if (pre.skip) return {status: 'skipped', reason: 'Script yêu cầu bỏ qua request.'};
    const path = typeof step.request.url === 'string' ? step.request.url : step.request.url.raw;
    const resolved = this.replace(path, pre.locals);
    if (resolved.includes('{{')) throw new Error('Chưa có đủ dữ liệu từ bước trước để tạo URL.');
    const url = new URL(resolved);
    if (url.origin !== new URL(this.backendUrl || this.env.base_url).origin || !url.pathname.startsWith(new URL(this.backendUrl || this.env.base_url).pathname.replace(/\/$/, '') + '/')) throw new Error('Request nằm ngoài backend đã cấu hình.');
    const headers = new Headers();
    for (const h of step.request.header || []) if (!h.disabled) headers.set(h.key, this.replace(h.value, pre.locals));
    const auth = step.request.auth;
    if (auth?.type === 'bearer') {
      const token = auth.bearer.find(a => a.key === 'token')?.value;
      headers.set('Authorization', 'Bearer ' + this.replace(token || '', pre.locals));
    }
    let body;
    if (step.request.body?.mode === 'raw') body = this.replace(pre.raw, pre.locals);
    else if (step.request.body?.mode === 'formdata') {
      body = new FormData();
      for (const field of step.request.body.formdata || []) {
        if (field.type === 'file') body.append(field.key, this.files.get(field.src));
        else body.append(field.key, this.replace(field.value || '', pre.locals));
      }
    }
    this.abortController = new AbortController();
    const timer = setTimeout(() => this.abortController.abort(), 30000); const started = performance.now();
    let response, text;
    try { response = await this.fetchImpl(url.href, {method: step.request.method, headers, body: ['GET', 'HEAD'].includes(step.request.method) ? undefined : body, signal: this.abortController.signal, credentials: 'omit', redirect: 'error'}); text = await response.text(); }
    catch (error) { throw new Error(error.name === 'AbortError' ? 'Request đã dừng hoặc quá 30 giây.' : 'Không gọi được backend. Kiểm tra URL, CORS, quyền truy cập mạng nội bộ hoặc mở bản local của Studio.'); }
    finally { clearTimeout(timer); }
    const duration = Math.round(performance.now() - started);
    const post = await this.sandbox.run({scripts: scripts('test'), env: this.env, locals: pre.locals, request: {raw: body instanceof FormData ? '' : body || ''}, response: {code: response.status, text, duration, headers: Object.fromEntries(response.headers)}});
    this.env = {...post.env, base_url: this.backendUrl};
    if (step.key === 'RESET-001' && response.status === 200) this.emailReady = true;
    let requestBody = body instanceof FormData ? {file: '[file upload]'} : body;
    try { requestBody = JSON.parse(requestBody); } catch {}
    let responseBody = text; try { responseBody = JSON.parse(text); } catch {}
    return {status: post.error || post.tests.some(t => !t.passed) ? 'failed' : 'passed', error: post.error, code: response.status, duration,
      tests: post.tests, url: url.href, request: redact(requestBody), response: redact(responseBody), responseHeaders: Object.fromEntries(response.headers)};
  }
  report() {
    return {name: this.collection.info?.name, startedAt: this.startedAt, status: this.status, cursor: this.cursor, total: this.plan?.length || 0,
      passed: this.results.filter(r => r.status === 'passed').length, failed: this.results.filter(r => r.status === 'failed').length,
      skipped: this.results.filter(r => r.status === 'skipped').length, results: this.results};
  }
}
