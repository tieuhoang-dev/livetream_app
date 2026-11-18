import test from 'node:test';
import assert from 'node:assert/strict';
import {Worker} from 'node:worker_threads';
import http from 'node:http';
import {readFile} from 'node:fs/promises';
import {TestRunner, flattenCollection} from '../dist/runner.js';
import {expectValue, redact} from '../dist/assertions.js';

export function workerFactory() {
  const worker = new Worker(new URL('./node-sandbox.mjs', import.meta.url));
  const adapter = {postMessage: data => worker.postMessage(data), terminate: () => worker.terminate(), onmessage: null, onerror: null};
  worker.on('message', data => adapter.onmessage?.({data})); worker.on('error', error => adapter.onerror?.(error)); return adapter;
}
const collection = JSON.parse(await readFile(new URL('../dist/data/collection.json', import.meta.url)));
const onboarding = structuredClone(collection);
onboarding.item = onboarding.item.slice(0, 1);
onboarding.item[0].item = onboarding.item[0].item.slice(0, 3);
const config = {account_mode: 'new', seller_email: 'seller@example.invalid', seller_password: 'secret-password', buyer_email: 'buyer@example.invalid', buyer_password: 'other-secret'};

async function fakeBackend(t) {
  const calls = []; let verified = false;
  const token = Buffer.from(JSON.stringify({alg: 'HS256'})).toString('base64url') + '.' + Buffer.from(JSON.stringify({user_id: '11111111-1111-4111-8111-111111111111'})).toString('base64url') + '.test-signature';
  const server = http.createServer(async (req, res) => {
    let input = ''; for await (const chunk of req) input += chunk;
    const body = JSON.parse(input || '{}'); calls.push({path: req.url, body});
    let code = 200, result = {status: 'success'};
    if (req.url.endsWith('/verify')) {
      if (body.code === '012345') verified = true;
      else { code = 400; result = {error: 'Invalid verification code'}; }
    }
    if (req.url.endsWith('/login')) {
      if (verified) result = {status: 'success', token};
      else { code = 400; result = {error: 'User not verified'}; }
    }
    res.writeHead(code, {'Content-Type': 'application/json'}); res.end(JSON.stringify(result));
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  t.after(() => new Promise(resolve => server.close(resolve)));
  return {calls, base_url: `http://127.0.0.1:${server.address().port}`};
}

test('loads every HTTP case and supports the matchers used by the collection', () => {
  assert.equal(flattenCollection(collection).length, 104);
  expectValue('token').to.be.a('string').and.not.empty;
  expectValue([]).to.eql([]); expectValue({id: 1}).not.to.have.property('user');
  expectValue('012345').to.match(/^\d{6}$/); expectValue([1]).to.have.lengthOf(1);
  assert.throws(() => expectValue(200).to.eql(400));
  assert.deepEqual(redact({password: 'p', token: 't', nested: {code: '123456', name: 'safe'}}), {password: '••••••', token: '••••••', nested: {code: '••••••', name: 'safe'}});
});

test('waits for typed OTP and resumes the same cursor; Register runs once', async t => {
  const backend = await fakeBackend(t); let resolveOtp, reached;
  const waiting = new Promise(resolve => { reached = resolve; });
  const runner = new TestRunner(onboarding, {workerFactory, requestInput: input => {
    assert.equal(input.kind, 'otp'); assert.equal(input.email, config.seller_email);
    reached(); return new Promise(resolve => { resolveOtp = resolve; });
  }});
  const run = runner.start({...config, base_url: backend.base_url}, [onboarding.item[0].name]);
  await waiting;
  assert.equal(runner.status, 'waiting'); assert.equal(runner.cursor, 1);
  assert.deepEqual(backend.calls.map(c => c.path), ['/api/users/register']);
  await new Promise(resolve => setTimeout(resolve, 30));
  assert.equal(backend.calls.length, 1);
  resolveOtp({code: '012345'}); resolveOtp({code: '999999'});
  const report = await run;
  assert.equal(report.passed, 3); assert.equal(report.failed, 0);
  assert.deepEqual(backend.calls.map(c => c.path), ['/api/users/register', '/api/users/verify', '/api/users/login']);
  assert.equal(backend.calls[1].body.code, '012345');
  assert.ok(!JSON.stringify(report).includes('secret-password'));
  assert.equal(report.results[1].request.code, '••••••');
  assert.equal(report.results[2].response.token, '••••••');
});

test('retrying a wrong OTP only retries Verify, never Register', async t => {
  const backend = await fakeBackend(t); let prompts = 0;
  const runner = new TestRunner(onboarding, {workerFactory, requestInput: async input => {
    if (input.kind === 'otp') return {code: ++prompts === 1 ? '000000' : '012345'};
    assert.equal(input.kind, 'failure'); return {action: 'retry'};
  }});
  const report = await runner.start({...config, base_url: backend.base_url}, [onboarding.item[0].name]);
  assert.equal(report.passed, 3); assert.equal(prompts, 2);
  assert.equal(backend.calls.filter(c => c.path.endsWith('/register')).length, 1);
  assert.equal(backend.calls.filter(c => c.path.endsWith('/verify')).length, 2);
  assert.equal(report.results.find(r => r.key === 'AUTH-002').attempts.length, 1);
});

test('stopping while waiting for OTP sends no Verify or Login', async t => {
  const backend = await fakeBackend(t); let release, reached;
  const waiting = new Promise(resolve => { reached = resolve; });
  const runner = new TestRunner(onboarding, {workerFactory, requestInput: () => { reached(); return new Promise(resolve => { release = resolve; }); }});
  const run = runner.start({...config, base_url: backend.base_url}, [onboarding.item[0].name]);
  await waiting; runner.stop(); release({action: 'stop'});
  assert.equal((await run).status, 'stopped');
  assert.equal(backend.calls.length, 1);
});

test('a manual pause resumes without replaying completed requests', async t => {
  const backend = await fakeBackend(t); let reached, release;
  const paused = new Promise(resolve => { reached = resolve; });
  let runner;
  runner = new TestRunner(onboarding, {workerFactory, onEvent: event => { if (event.type === 'step-result' && event.step.key === 'AUTH-001') runner.pause(); }, requestInput: input => {
    if (input.kind === 'pause') { reached(); return new Promise(resolve => { release = resolve; }); }
    return Promise.resolve({code: '012345'});
  }});
  const run = runner.start({...config, base_url: backend.base_url}, [onboarding.item[0].name]);
  await paused; assert.equal(backend.calls.length, 1); release({});
  assert.equal((await run).passed, 3);
  assert.equal(backend.calls.filter(c => c.path.endsWith('/register')).length, 1);
});

test('reset flow waits for OTP/new password and preserves the same run', async t => {
  const reset = structuredClone(collection);
  reset.item = reset.item.filter(f => f.name.startsWith('06 -'));
  reset.item[0].item = reset.item[0].item.slice(0, 5);
  let password = 'other-secret', consumed = false;
  const calls = [];
  const token = 'test.' + Buffer.from(JSON.stringify({user_id:'22222222-2222-4222-8222-222222222222'})).toString('base64url') + '.signature';
  const server = http.createServer(async (req,res) => {
    let raw='';for await(const chunk of req)raw+=chunk;
    const body=JSON.parse(raw);calls.push({url:req.url,body});
    let code=200,result={status:'success'};
    if(req.url.endsWith('/password/reset')) {
      if(consumed){code=400;result={error:'otp_expired'};}
      else if(body.code==='012345'){consumed=true;password=body.new_password;}
      else{code=400;result={error:'invalid_otp'};}
    }
    if(req.url.endsWith('/login')) {
      if(body.password===password)result={status:'success',token};
      else{code=400;result={error:'Invalid password'};}
    }
    res.writeHead(code,{'Content-Type':'application/json'});res.end(JSON.stringify(result));
  });
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  t.after(()=>new Promise(resolve=>server.close(resolve)));
  let release,reached;
  const wait=new Promise(resolve=>{reached=resolve});
  const runner=new TestRunner(reset,{workerFactory,requestInput:input=>{
    assert.equal(input.kind,'otp');assert.equal(input.reset,true);reached();return new Promise(resolve=>{release=resolve});
  }});
  const run=runner.start({...config,account_mode:'existing',base_url:`http://127.0.0.1:${server.address().port}`},[reset.item[0].name]);
  await wait;assert.equal(calls.length,1);assert.equal(runner.cursor,1);
  release({code:'012345',password:'new-private-password'});
  const report=await run;
  assert.equal(report.passed,5);assert.equal(report.failed,0);
  assert.equal(calls.filter(c=>c.url.endsWith('/reset-request')).length,1);
  assert.equal(runner.env.buyer_password,'new-private-password');
  assert.ok(!JSON.stringify(report).includes('new-private-password'));
});


test('collection scripts cannot change the configured backend destination', async t => {
  const backend = await fakeBackend(t);
  const custom = {info:{name:'destination'},item:[{name:'Requests',item:[{
    name:'BOUNDARY-001 | Keep backend origin',
    event:[{listen:'prerequest',script:{exec:["pm.environment.set('base_url', 'https://outside.invalid');"]}}],
    request:{method:'GET',url:'{{base_url}}/api/products'}
  }]}]};
  const runner = new TestRunner(custom,{workerFactory,requestInput:async()=>({action:'continue'})});
  const report = await runner.start({...config,base_url:backend.base_url},['Requests']);
  assert.equal(report.passed,1);
  assert.equal(backend.calls.length,1);
  assert.equal(new URL(report.results[0].url).origin,backend.base_url);
});

test('AUTH-001 keeps its real JSON body on a network failure and never auto-retries Register', async () => {
  const seller = structuredClone(onboarding);
  seller.item[0].item = seller.item[0].item.slice(0, 1);
  let sent, requests = 0;
  const runner = new TestRunner(seller, {workerFactory,
    fetchImpl: async (url, options) => {
      requests++; sent = {url, body:JSON.parse(options.body)};
      throw new TypeError('Failed to fetch');
    },
    requestInput: async input => {
      assert.equal(input.kind, 'failure');
      assert.equal(input.result.transportError, true);
      assert.equal(input.result.request.email, config.seller_email);
      assert.equal(input.result.request.password, '••••••');
      return {action:'continue'};
    }
  });
  const report = await runner.start({...config,base_url:'http://localhost:8080'},[seller.item[0].name]);
  assert.equal(requests, 1);
  assert.equal(sent.url, 'http://localhost:8080/api/users/register');
  assert.equal(sent.body.email, config.seller_email);
  assert.equal(sent.body.password, config.seller_password);
  assert.ok(sent.body.username);
  assert.equal(report.failed, 1);
  assert.equal(report.results[0].request.email, config.seller_email);
  assert.equal(report.results[0].code, null);
  assert.match(report.results[0].error, /POST http:\/\/localhost:8080\/api\/users\/register/);
  assert.ok(!JSON.stringify(report).includes(config.seller_password));
});

for (const useDefault of [true, false]) {
  test(`preserves browser fetch receiver through Register, Verify and Login (${useDefault ? 'default' : 'injected'} fetch)`, async t => {
    const backend = await fakeBackend(t);
    const originalFetch = globalThis.fetch;
    let fetchCalls = 0;
    function receiverCheckedFetch(...args) {
      if (this !== globalThis) throw new TypeError("Failed to execute 'fetch' on 'Window': Illegal invocation");
      fetchCalls++;
      return Reflect.apply(originalFetch, globalThis, args);
    }
    const options = {workerFactory, requestInput: async input => {
      assert.equal(input.kind, 'otp'); return {code: '012345'};
    }};
    if (useDefault) {
      globalThis.fetch = receiverCheckedFetch;
      t.after(() => { globalThis.fetch = originalFetch; });
    } else options.fetchImpl = receiverCheckedFetch;
    const runner = new TestRunner(onboarding, options);
    const report = await runner.start({...config, base_url: backend.base_url}, [onboarding.item[0].name]);
    assert.equal(report.passed, 3, JSON.stringify(report));
    assert.equal(report.failed, 0);
    assert.equal(fetchCalls, 3);
    assert.deepEqual(backend.calls.map(c => c.path), ['/api/users/register', '/api/users/verify', '/api/users/login']);
  });
}

test('carrier fixture resolves numeric route IDs without any shipment request', async t => {
  const folder = collection.item.find(item => item.item?.some(row => row.name.startsWith('SHIP-001')));
  let received;
  const server = http.createServer(async (req, res) => {
    assert.equal(req.url, '/api/sales/shipping-settings');
    let raw = ''; for await (const chunk of req) raw += chunk;
    received = JSON.parse(raw);
    res.writeHead(200, {'Content-Type': 'application/json'});
    res.end(JSON.stringify({configured: true, settings: received}));
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  t.after(() => new Promise(resolve => server.close(resolve)));
  const runner = new TestRunner(collection, {workerFactory, requestInput: async () => { throw new Error('unexpected input'); }});
  const report = await runner.start({...config, account_mode: 'existing', base_url: `http://127.0.0.1:${server.address().port}`, seller_token: 'fixture-token',
    ghn_pickup_province_id: '1', ghn_pickup_district_id: '100', ghn_pickup_ward_code: '001'}, [folder.name]);
  assert.equal(report.failed, 0);
  assert.equal(report.passed, 1);
  assert.equal(received.pickup_address.carrier_province_id, 1);
  assert.equal(received.pickup_address.carrier_district_id, 100);
  assert.equal(received.pickup_address.carrier_ward_code, '001');
  assert.ok(!Object.hasOwn(received, 'same_province_fee'));
});

test('Studio authenticates once on the shared socket URL and correlates live commands', async () => {
  const frames = [], instances = [];
  class Socket extends EventTarget {
    constructor(url) { super(); this.url = url; this.readyState = 0; instances.push(this); queueMicrotask(() => { this.readyState = 1; this.dispatchEvent(new Event('open')); }); }
    send(raw) { const frame = JSON.parse(raw); frames.push(frame); queueMicrotask(() => this.dispatchEvent(new MessageEvent('message', {data: JSON.stringify({type:'result', request_id:frame.request_id, data:frame.type === 'authenticate' ? {user_id:'seller'} : {id:'live-fixture', status:frame.type === 'live.end' ? 'end' : 'start'}})}))); }
    close() { this.readyState = 3; this.dispatchEvent(new Event('close')); }
  }
  const runner = new TestRunner({item:[]}, {webSocketFactory: url => new Socket(url)});
  runner.env = {seller_token:'fixture-bearer'}; runner.backendUrl = 'http://localhost:8080';
  const live = await runner.wsRequest('seller', {type:'live.create', payload:{title:'Fixture'}});
  await runner.wsRequest('seller', {type:'live.end', payload:{livestream_id:live.id}});
  assert.equal(instances.length, 1); assert.equal(instances[0].url, 'ws://localhost:8080/api/ws');
  assert.deepEqual(frames.map(frame => frame.type), ['authenticate','live.create','live.end']);
  assert.equal(frames[0].payload.token, 'fixture-bearer'); assert.equal(new Set(frames.map(frame => frame.request_id)).size, 3);
  runner.stop();
});

test('stopping Studio closes a pending live command without resending it', async () => {
  const frames = []; let reached;
  const waiting = new Promise(resolve => { reached = resolve; });
  class Socket extends EventTarget {
    constructor() { super(); this.readyState = 0; queueMicrotask(() => { this.readyState = 1; this.dispatchEvent(new Event('open')); }); }
    send(raw) { const frame = JSON.parse(raw); frames.push(frame); if (frame.type === 'authenticate') queueMicrotask(() => this.dispatchEvent(new MessageEvent('message', {data:JSON.stringify({type:'result', request_id:frame.request_id, data:{user_id:'seller'}})}))); else reached(); }
    close() { this.readyState = 3; this.dispatchEvent(new Event('close')); }
  }
  const runner = new TestRunner({item:[]}, {webSocketFactory: () => new Socket()});
  runner.env = {seller_token:'fixture'}; runner.backendUrl = 'http://localhost:8080';
  const pending = runner.wsRequest('seller', {type:'live.create', payload:{title:'Fixture'}});
  await waiting; runner.stop(); await assert.rejects(pending, /Đã dừng/);
  assert.equal(frames.filter(frame => frame.type === 'live.create').length, 1);
});
