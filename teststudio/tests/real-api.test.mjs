import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {execFileSync} from 'node:child_process';
import {Worker} from 'node:worker_threads';
import {TestRunner} from '../dist/runner.js';

test('browser runner executes the real backend suite with interactive input', {skip: !process.env.STUDIO_REAL_API || !process.env.STUDIO_FIXTURE_HELPER, timeout: 120000}, async () => {
  const collection = JSON.parse(await readFile(new URL('../dist/data/collection.json', import.meta.url)));
  const python = process.env.STUDIO_FIXTURE_PYTHON || 'python3';
  const helper = process.env.STUDIO_FIXTURE_HELPER;
  const fixture = JSON.parse(execFileSync(python, [helper, 'config'], {encoding: 'utf8'}));
  const calls = [], prompts = [];
  const workerFactory = () => {
    const worker = new Worker(new URL('./node-sandbox.mjs', import.meta.url));
    const adapter = {postMessage: data => worker.postMessage(data), terminate: () => worker.terminate(), onmessage: null, onerror: null};
    worker.on('message', data => adapter.onmessage?.({data})); worker.on('error', error => adapter.onerror?.(error)); return adapter;
  };
  const runner = new TestRunner(collection, {workerFactory,
    fetchImpl: async (url, options) => { calls.push({url: String(url), method: options?.method || 'GET'}); return fetch(url, options); },
    requestInput: async input => {
      prompts.push(input.kind);
      if (input.kind === 'otp') {
        execFileSync(python, [helper, 'otp', input.email]);
        return {code: '012345'};
      }
      if (input.kind === 'file') return {file: new File([await readFile(new URL('../dist/data/avatar.png', import.meta.url))], 'avatar.png', {type: 'image/png'})};
      if (input.kind === 'credentials') return {email: `studio-negative-${crypto.randomUUID()}@example.invalid`, password: 'Private-test-only-123!'};
      throw new Error(`Unexpected input ${input.kind}: ${input.description}`);
    }
  });
  const folders = collection.item.filter(f => !f.name.startsWith('06 -')).map(f => f.name);
  const report = await runner.start({...fixture, base_url: process.env.STUDIO_REAL_API, account_mode: 'new'}, folders);
  assert.equal(report.failed, 0, JSON.stringify(report.results.filter(r => r.status === 'failed')));
  assert.equal(report.passed, 98); assert.equal(report.status, 'finished');
  assert.deepEqual(prompts, ['otp', 'otp', 'file', 'credentials']);
  assert.equal(calls.filter(c => c.url.endsWith('/api/users/register')).length, 3);
  assert.ok(runner.env.live_id && runner.env.ended_live_id);
  assert.ok(!JSON.stringify(report).includes(fixture.seller_password));
  process.stdout.write(`Real API: ${report.passed}/${report.total} cases; prompts=${prompts.join(', ')}; Register never replayed.\n`);
});
