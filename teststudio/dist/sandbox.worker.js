import {expectValue} from './assertions.js';

// Scripts run outside the UI thread. Requests are sent by the runner, not by scripts.
for (const key of ['fetch', 'XMLHttpRequest', 'WebSocket', 'Worker', 'importScripts', 'BroadcastChannel']) {
  try { self[key] = undefined; } catch {}
}
const uuid = () => crypto.randomUUID();
const decode = input => {
  const text = input.replace(/-/g, '+').replace(/_/g, '/');
  return new TextDecoder().decode(Uint8Array.from(atob(text), c => c.charCodeAt(0)));
};

self.onmessage = event => {
  const {id, scripts, env: initial, locals: localInitial, request, response} = event.data;
  const env = {...initial}, locals = {...localInitial}, tests = [], logs = [];
  let raw = request?.raw ?? '', next = undefined, skip = false, error = null;
  const replaceIn = text => String(text).replace(/\{\{\s*([^{}]+?)\s*\}\}/g, (all, key) => {
    if (key === '$randomUUID' || key === '$guid') return uuid();
    if (key === '$timestamp') return String(Math.floor(Date.now() / 1000));
    return locals[key] ?? env[key] ?? all;
  });
  const scope = target => ({get: key => target[key], set: (key, value) => { target[key] = value; }, unset: key => { delete target[key]; }, toObject: () => ({...target})});
  const pm = {
    environment: scope(env), collectionVariables: scope(env), globals: scope(env),
    variables: {...scope(locals), get: key => locals[key] ?? env[key], replaceIn},
    request: {body: {get raw() { return raw; }, update: value => { raw = typeof value === 'string' ? value : value.raw; }}},
    response: response ? {
      code: response.code, responseTime: response.duration,
      json: () => JSON.parse(response.text), text: () => response.text,
      headers: {get: name => Object.entries(response.headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1]},
      to: {have: {status: status => expectValue(response.code).equal(status)}}
    } : undefined,
    expect: expectValue,
    test: (name, callback) => {
      try { callback(); tests.push({name, passed: true}); }
      catch (error) { tests.push({name, passed: false, message: error.message}); }
    },
    execution: {setNextRequest: value => { next = value; }, skipRequest: () => { skip = true; }}
  };
  const require = name => {
    if (name !== 'buffer') throw new Error(`Module ${name} chưa được hỗ trợ`);
    return {Buffer: {from: (input, encoding) => ({toString: () => encoding === 'base64' ? decode(input) : input})}};
  };
  const console = Object.fromEntries(['info', 'log', 'warn', 'error', 'debug'].map(level => [level, (...args) => logs.push({level, text: args.map(String).join(' ')})]));
  try {
    for (const script of scripts) {
      new Function('pm', 'require', 'console', script)(pm, require, console);
      if (skip) break;
    }
  } catch (caught) { error = caught.message; }
  self.postMessage({id, env, locals, raw, tests, logs, next, skip, error});
};
