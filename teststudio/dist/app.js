import {TestRunner, flattenCollection} from './runner.js';
import {redact} from './assertions.js';

const $ = id => document.getElementById(id);
const labels = ['Tài khoản & OTP', 'Sản phẩm & đơn hàng', 'Upload avatar', 'Sản phẩm trong live', 'Live đã kết thúc', 'Đổi mật khẩu', 'Chặn tài khoản chưa verify'];
const symbols = {passed: '✓', failed: '!', skipped: '↷', running: '◌', waiting: 'Ⅱ', idle: '○'};
let collection, runner, all = [], selectedFolders = new Set(), results = [], plan = [], activeKey = '', selectedKey = '', filter = 'all', pendingResolver = null, detailTab = 'response', collapsed = new Set();
let startedAt = 0, active = false, currentStatus = 'idle', completedVerified = false, lastReport;
const toast = message => { $('toast').textContent = message; $('toast').classList.add('visible'); clearTimeout(toast.timer); toast.timer = setTimeout(() => $('toast').classList.remove('visible'), 6000); };
const el = (tag, className, text) => { const n = document.createElement(tag); if (className) n.className = className; if (text !== undefined) n.textContent = text; return n; };
const folderLabel = folder => labels[collection.item.findIndex(f => f.name === folder)] || folder;
const mode = () => document.querySelector('[name=account_mode]:checked').value;

function renderSuites() {
  $('suite-list').replaceChildren();
  collection.item.forEach((folder, index) => {
    const label = el('label', 'suite-option');
    const input = document.createElement('input'); input.type = 'checkbox'; input.checked = selectedFolders.has(folder.name);
    input.disabled = active || (index === 0 && mode() === 'existing');
    if (index === 0 && mode() === 'existing') input.checked = false;
    input.addEventListener('change', () => { input.checked ? selectedFolders.add(folder.name) : selectedFolders.delete(folder.name); renderCases(); });
    label.append(input, el('span', 'suite-label', labels[index] || folder.name), el('span', 'suite-count', String(folder.item.length)));
    $('suite-list').append(label);
  });
  $('stat-loaded').textContent = all.length; $('total-badge').textContent = all.length;
}
function visibleCases() {
  return active || currentStatus === 'finished' || currentStatus === 'stopped' ? plan : all.filter(s => selectedFolders.has(s.folder) && !(mode() === 'existing' && s.folder.startsWith('01 -')));
}
function renderCases() {
  const list = $('case-list'), cases = visibleCases();
  list.replaceChildren();
  for (const folder of collection.item) {
    const group = cases.filter(s => s.folder === folder.name);
    if (!group.length) continue;
    const failed = results.filter(r => r.folder === folder.name && r.status === 'failed');
    if (filter === 'failed' && !failed.length) continue;
    const heading = el('button', 'suite-heading' + (collapsed.has(folder.name) ? ' collapsed' : ''));
    heading.append(el('span', 'chevron', '⌄'), el('strong', '', folderLabel(folder.name)), el('small', '', `${results.filter(r => r.folder === folder.name).length} / ${group.length}`));
    heading.addEventListener('click', () => { collapsed.has(folder.name) ? collapsed.delete(folder.name) : collapsed.add(folder.name); renderCases(); });
    list.append(heading);
    if (collapsed.has(folder.name)) continue;
    for (const step of group) {
      const result = results.find(r => r.key === step.key);
      if (filter === 'failed' && result?.status !== 'failed') continue;
      const state = activeKey === step.key && active ? (currentStatus === 'waiting' ? 'waiting' : 'running') : result?.status || 'idle';
      const row = el('button', `case-row ${state} ${activeKey === step.key && active ? 'active' : ''} ${selectedKey === step.key ? 'selected' : ''}`);
      row.dataset.key = step.key; row.title = step.name;
      row.append(el('span', `case-state ${state}`, symbols[state]), el('span', 'case-id', step.key), el('span', 'case-description', step.name));
      const meta = el('div', 'case-meta'); meta.append(el('span', `method ${step.request.method}`, step.request.method), el('span', `response-code ${result?.status || ''}`, result?.code || (state === 'waiting' ? 'chờ' : '—'))); row.append(meta);
      row.addEventListener('click', () => { selectedKey = step.key; renderDetails(); renderCases(); }); list.append(row);
    }
  }
  $('footer-count').textContent = `${results.length} / ${cases.length}`;
}
function renderStats() {
  for (const status of ['passed', 'failed', 'skipped']) $('stat-' + status).textContent = results.filter(r => r.status === status).length;
  const total = visibleCases().length;
  $('stat-selected').textContent = total ? `trong ${total} case chọn` : 'chưa chọn';
  $('progress-bar').style.width = (total ? results.length / total * 100 : 0) + '%';
}
function renderDetails() {
  const target = $('details-content'), step = all.find(s => s.key === selectedKey);
  if (!step) return;
  target.replaceChildren();
  const result = results.find(r => r.key === step.key);
  target.append(el('span', 'method ' + step.request.method, step.request.method + '  ·  ' + step.key), el('div', 'detail-title', step.name));
  let url = result?.url || (typeof step.request.url === 'string' ? step.request.url : step.request.url.raw);
  url = runner ? runner.replace(url) : url.replace('{{base_url}}', $('base-url').value);
  target.append(el('div', 'detail-url', url));
  if (result) {
    target.append(el('div', 'detail-summary', `${result.code || '—'} HTTP  ·  ${result.duration ?? 0} ms${result.attempts?.length ? `  ·  ${result.attempts.length} lần thử lại` : ''}`));
    if (result.error || result.reason) target.append(el('p', 'detail-error', result.error || result.reason));
    if (result.tests?.length) {
      target.append(el('div', 'detail-label', 'ASSERTION'));
      const tests = el('div', 'detail-tests');
      for (const test of result.tests) tests.append(el('div', 'test-assertion' + (test.passed ? '' : ' failed'), test.name + (test.message ? ': ' + test.message : '')));
      target.append(tests);
    }
  }
  const tabs = el('div', 'detail-tabs');
  for (const [key, label] of [['request', 'Request'], ['response', 'Response']]) {
    const b = el('button', key === detailTab ? 'active' : '', label); b.addEventListener('click', () => { detailTab = key; renderDetails(); }); tabs.append(b);
  }
  target.append(tabs);
  let body = result ? result[detailTab] : detailTab === 'request' ? step.request.body?.raw || 'Không có body' : 'Response sẽ xuất hiện khi chạy bước này.';
  try { body = redact(JSON.parse(body)); } catch {}
  target.append(el('pre', 'detail-code', typeof body === 'string' ? body : JSON.stringify(body ?? 'Không có nội dung', null, 2)));
}

function setConfigEnabled(enabled) {
  for (const input of $('config-form').querySelectorAll('input')) input.disabled = !enabled;
  $('check-button').disabled = !enabled; $('import-button').disabled = !enabled;
  renderSuites();
}
function renderState(event) {
  currentStatus = event.status; results = [...event.results]; plan = event.plan;
  if (event.type === 'step-start') {
    activeKey = event.step.key; selectedKey = activeKey; collapsed.delete(event.step.folder); detailTab = 'response';
  }
  if (event.type === 'waiting') { activeKey = event.input.step?.key || plan[event.cursor]?.key; selectedKey = activeKey; }
  const titles = {idle: 'Sẵn sàng kiểm thử', running: 'Đang chạy kiểm thử', waiting: 'Đang chờ bạn nhập', finished: 'Đã hoàn tất phiên chạy', stopped: 'Phiên đã kết thúc', error: 'Phiên gặp lỗi'};
  $('run-title').textContent = titles[currentStatus] || titles.idle;
  $('run-dot').className = 'run-dot ' + currentStatus;
  $('pause-button').disabled = !active || currentStatus === 'waiting' || runner?.pauseRequested;
  $('pause-button').textContent = runner?.pauseRequested ? 'Đang tạm dừng…' : 'Ⅱ Tạm dừng';
  $('run-subtitle').textContent = event.type === 'system' ? event.message : activeKey && active ? `${activeKey} · Giữ nguyên phiên, tiếp tục đúng bước` : `${results.length} / ${plan.length} bước đã xử lý`;
  $('footer-status').textContent = currentStatus === 'waiting' ? 'Phiên được giữ nguyên trong khi chờ bạn' : currentStatus === 'running' ? 'Request đang gửi tới backend của bạn' : `${results.filter(r => r.status === 'passed').length} bước đạt · ${results.filter(r => r.status === 'failed').length} bước chưa đạt`;
  $('export-button').disabled = !results.length;
  renderStats(); renderCases(); renderDetails();
  if (event.type === 'step-start') $('case-list').querySelector(`[data-key="${activeKey}"]`)?.scrollIntoView({block: 'nearest', behavior: 'smooth'});
  if (event.type === 'finished') finish(event);
}

function askUser(input) {
  return new Promise(resolve => {
    pendingResolver = resolve;
    const area = $('prompt-area'); area.replaceChildren();
    const card = el('form', 'prompt' + (input.kind === 'failure' ? ' failure' : ''));
    card.append(el('div', 'prompt-label', input.kind === 'failure' ? 'CẦN KIỂM TRA' : 'CHỜ BẠN NHẬP'));
    const dot = el('span'); card.firstChild.prepend(dot);
    card.append(el('h2', '', input.title), el('p', 'prompt-description', input.description));
    if (input.email) card.append(el('div', 'prompt-email', '✉  ' + input.email));
    const actions = el('div', 'prompt-actions');
    const proceed = el('button', 'button primary', input.kind === 'failure' ? 'Thử lại bước này' : 'Tiếp tục kiểm thử  →'); proceed.type = 'submit';
    let collect = () => ({});
    if (input.kind === 'otp') {
      const digits = el('div', 'otp-inputs'); digits.setAttribute('role', 'group'); digits.setAttribute('aria-label', 'Mã OTP 6 chữ số');
      const boxes = [];
      for (let i = 0; i < 6; i++) {
        const field = document.createElement('input'); field.type = 'text'; field.inputMode = 'numeric'; field.maxLength = 1; field.pattern = '[0-9]'; field.required = true; field.setAttribute('aria-label', 'Chữ số ' + (i + 1)); field.autocomplete = i === 0 ? 'one-time-code' : 'off';
        field.addEventListener('input', () => { field.value = field.value.replace(/\D/g, '').slice(-1); if (field.value) boxes[i + 1]?.focus(); proceed.disabled = boxes.some(b => !b.value) || (input.reset && !card.querySelector('[data-new-password]')?.value); });
        field.addEventListener('keydown', event => { if (event.key === 'Backspace' && !field.value) boxes[i - 1]?.focus(); });
        field.addEventListener('paste', event => { const text = event.clipboardData.getData('text').replace(/\D/g, '').slice(0, 6); if (text.length > 1) { event.preventDefault(); [...text].forEach((n, j) => { if (boxes[j]) boxes[j].value = n; }); boxes[Math.min(text.length, 5)].focus(); proceed.disabled = boxes.some(b => !b.value) || (input.reset && !card.querySelector('[data-new-password]')?.value); } });
        boxes.push(field); digits.append(field);
      }
      card.append(digits);
      let password;
      if (input.reset) {
        const label = el('label', 'field', 'Mật khẩu mới cho tài khoản buyer'); password = document.createElement('input'); password.type = 'password'; password.required = true; password.maxLength = 72; password.dataset.newPassword = ''; password.autocomplete = 'new-password';
        password.addEventListener('input', () => { proceed.disabled = boxes.some(b => !b.value) || !password.value; }); label.append(password); card.append(label);
      }
      card.append(el('div', 'prompt-hint', 'Bạn có thể dán cả 6 số. Nhập xong chỉ bấm Tiếp tục, không chạy lại Register. Mã cần lấy từ email thật của backend.'));
      proceed.disabled = true;
      collect = () => ({code: boxes.map(b => b.value).join(''), password: password?.value});
      setTimeout(() => boxes[0].focus(), 50);
    } else if (input.kind === 'credentials') {
      const email = document.createElement('input'), password = document.createElement('input');
      email.type = 'email'; password.type = 'password'; email.required = password.required = true;
      for (const [labelText, field] of [['Email tài khoản test', email], ['Mật khẩu', password]]) { const label = el('label', 'field', labelText); label.append(field); card.append(label); }
      collect = () => ({email: email.value.trim(), password: password.value});
    } else if (input.kind === 'file') {
      const label = el('label', 'field', 'Ảnh PNG để upload'); const field = document.createElement('input'); field.type = 'file'; field.accept = '.png,image/png'; field.className = 'prompt-file'; field.required = true; label.append(field); card.append(label);
      field.addEventListener('change', () => { const file = field.files[0]; field.setCustomValidity(file && (!/\.png$/i.test(file.name) || file.size > 10 * 1024 * 1024) ? 'Chọn ảnh PNG không quá 10 MB cho case này.' : ''); });
      const sample = el('button', 'button secondary', 'Dùng ảnh mẫu'); sample.type = 'button'; sample.addEventListener('click', () => completeInput({sample: true})); actions.append(sample);
      collect = () => ({file: field.files[0]});
    } else if (input.kind === 'value') {
      const label = el('label', 'field', input.key); const field = document.createElement('input'); field.type = 'text'; field.required = true; label.append(field); card.append(label); collect = () => ({value: field.value.trim()});
    } else if (input.kind === 'failure') {
      proceed.textContent = 'Thử lại bước này'; collect = () => ({action: 'retry'});
      for (const [label, action] of [['Tiếp tục bước sau', 'continue'], ['Bỏ qua phần còn lại của nhóm', 'skip-group']]) {
        const b = el('button', 'button secondary', label); b.type = 'button'; b.addEventListener('click', () => completeInput({action})); actions.append(b);
      }
      card.append(el('div', 'prompt-hint', 'Thử lại chỉ chạy bước hiện tại. Với request tạo dữ liệu, hãy kiểm tra response trước khi gửi lại.'));
    }
    actions.prepend(proceed); card.append(actions);
    card.addEventListener('submit', event => { event.preventDefault(); if (!card.reportValidity()) return; completeInput(collect()); });
    area.append(card); card.scrollIntoView({block: 'nearest', behavior: 'smooth'});
  });
}
function completeInput(answer) {
  if (!pendingResolver) return;
  const resolve = pendingResolver; pendingResolver = null;
  for (const button of $('prompt-area').querySelectorAll('button')) button.disabled = true;
  $('prompt-area').replaceChildren(); resolve(answer);
}

function finish(event) {
  active = false; $('pause-button').disabled = true; $('stop-button').hidden = true; $('start-button').hidden = false; $('start-button').disabled = false; $('start-button').textContent = '▶ Phiên chạy mới';
  setConfigEnabled(true); $('prompt-area').replaceChildren();
  lastReport = runner.report();
  const banner = el('div', 'finished-banner'); banner.append(el('span', '', currentStatus === 'finished' ? '✓' : '↷'));
  const text = el('div'); text.append(el('h2', '', currentStatus === 'finished' ? 'Đã hoàn tất các bước đã chọn' : 'Phiên đã kết thúc'), el('p', '', event.error || `${lastReport.passed} đạt · ${lastReport.failed} chưa đạt · ${lastReport.skipped} bỏ qua. Bạn có thể xem chi tiết hoặc xuất báo cáo.`)); banner.append(text); $('prompt-area').append(banner);
  if (runner.env.seller_token && runner.env.buyer_token) {
    $('seller-password').value = runner.env.seller_password;
    $('buyer-password').value = runner.env.buyer_password;
    completedVerified = true; document.querySelector('[name=account_mode][value=existing]').checked = true;
  }
  activeKey = ''; renderSuites(); renderStats(); renderCases();
}

$('start-button').addEventListener('click', async () => {
  if (active || !$('config-form').reportValidity()) return;
  if ($('seller-email').value.trim() === $('buyer-email').value.trim()) return toast('Seller và buyer cần là hai tài khoản test khác nhau.');
  const config = {base_url: $('base-url').value.trim(), account_mode: mode(), seller_email: $('seller-email').value.trim(), seller_password: $('seller-password').value, buyer_email: $('buyer-email').value.trim(), buyer_password: $('buyer-password').value};
  try { new URL(config.base_url); } catch { return toast('Địa chỉ backend chưa hợp lệ.'); }
  if (!selectedFolders.size) return toast('Chọn ít nhất một nhóm test.');
  results = []; plan = []; activeKey = ''; active = true; startedAt = Date.now();
  setConfigEnabled(false); $('start-button').hidden = true; $('stop-button').hidden = false; $('pause-button').disabled = false;
  $('config-form').hidden = true; $('config-toggle').setAttribute('aria-expanded', 'false'); $('config-chevron').textContent = '⌄'; $('prompt-area').replaceChildren();
  runner = new TestRunner(collection, {requestInput: askUser, onEvent: renderState});
  try { await runner.start(config, [...selectedFolders]); } catch (error) { active = false; currentStatus = 'idle'; setConfigEnabled(true); $('start-button').hidden = false; $('stop-button').hidden = true; $('pause-button').disabled = true; $('config-form').hidden = false; toast(error.message); }
});
$('pause-button').addEventListener('click', () => { if (!runner || !active) return; if (pendingResolver && currentStatus === 'waiting') return; runner.pause(); $('pause-button').disabled = true; $('pause-button').textContent = 'Đang tạm dừng…'; });
$('stop-button').addEventListener('click', () => { if (!runner || !active) return; runner.stop(); if (pendingResolver) completeInput({action: 'stop'}); });
$('config-toggle').addEventListener('click', () => { $('config-form').hidden = !$('config-form').hidden; $('config-toggle').setAttribute('aria-expanded', String(!$('config-form').hidden)); $('config-chevron').textContent = $('config-form').hidden ? '⌄' : '⌃'; });
for (const radio of document.querySelectorAll('[name=account_mode]')) radio.addEventListener('change', () => { renderSuites(); renderCases(); renderStats(); });
for (const b of document.querySelectorAll('[data-filter]')) b.addEventListener('click', () => { filter = b.dataset.filter; document.querySelectorAll('[data-filter]').forEach(button => button.classList.toggle('active', button === b)); renderCases(); });
$('check-button').addEventListener('click', async () => {
  $('check-button').disabled = true; $('check-button').textContent = 'Đang kiểm tra…';
  try {
    const base = $('base-url').value.trim().replace(/\/$/, ''); const res = await fetch(base + '/api/products?page=1&page_size=1', {signal: AbortSignal.timeout(10000), credentials: 'omit'});
    if (!res.ok) throw new Error('HTTP ' + res.status); const body = await res.json(); if (!Array.isArray(body.data)) throw new Error('Response không đúng API sản phẩm');
    $('connection').className = 'connection good'; $('connection').replaceChildren(el('i'), document.createTextNode('Đã kết nối')); toast('Backend đã sẵn sàng.');
  } catch (error) { $('connection').className = 'connection bad'; $('connection').replaceChildren(el('i'), document.createTextNode('Chưa truy cập được')); toast('Không kết nối được backend. Kiểm tra URL/CORS. Với localhost, có thể cần mở bản Studio local hoặc cho phép quyền truy cập mạng nội bộ.'); }
  finally { $('check-button').disabled = false; $('check-button').textContent = 'Kiểm tra kết nối'; }
});
$('export-button').addEventListener('click', () => {
  const report = runner?.report() || lastReport; if (!report) return;
  const blob = new Blob([JSON.stringify(redact(report), null, 2)], {type: 'application/json'}); const link = document.createElement('a'); link.href = URL.createObjectURL(blob); link.download = 'livestream-test-results-' + new Date().toISOString().slice(0, 10) + '.json'; link.click(); setTimeout(() => URL.revokeObjectURL(link.href), 1000);
});
$('import-button').addEventListener('click', () => $('import-file').click());
$('import-file').addEventListener('change', async event => {
  const file = event.target.files[0]; if (!file) return;
  try {
    if (file.size > 2 * 1024 * 1024) throw new Error('Collection tối đa 2 MB.');
    const candidate = JSON.parse(await file.text()); if (!Array.isArray(candidate.item)) throw new Error('Chọn file Postman Collection JSON.');
    const flat = flattenCollection(candidate); if (!flat.length || flat.length > 500) throw new Error('Bộ test cần có từ 1 đến 500 request.');
    collection = candidate; all = flat; selectedFolders = new Set(collection.item.filter(f => f.item).map(f => f.name)); results = []; plan = []; runner = null; currentStatus = 'idle'; selectedKey = ''; collapsed = new Set();
    renderSuites(); renderCases(); renderStats(); toast('Đã nạp ' + all.length + ' test case. Studio hỗ trợ script và matcher dùng trong bộ Livestream đã kèm.');
  } catch (error) { toast(error.message); }
  event.target.value = '';
});
setInterval(() => { if (active && startedAt) { const s = Math.floor((Date.now() - startedAt) / 1000); $('elapsed').textContent = `${String(Math.floor(s / 60)).padStart(2, '0')}:${String(s % 60).padStart(2, '0')}`; } }, 1000);
window.addEventListener('beforeunload', event => { if (active) { event.preventDefault(); event.returnValue = ''; } });

try {
  const response = await fetch('./data/collection.json'); collection = await response.json(); all = flattenCollection(collection);
  selectedFolders = new Set(collection.item.map(f => f.name));
  collection.item.slice(1).forEach(f => collapsed.add(f.name));
  renderSuites(); renderCases(); renderStats();
} catch (error) { $('suite-list').textContent = 'Không nạp được bộ test. Hãy mở Studio qua HTTP server, không mở file HTML trực tiếp.'; toast(error.message); }
