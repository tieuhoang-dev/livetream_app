import test from 'node:test';
import assert from 'node:assert/strict';
import http from 'node:http';
import {checkConnection, normalizeBackendUrl} from '../dist/connection.js';

test('connection check sends a harmless JSON POST after the GET probe', async t => {
  const calls = [];
  const server = http.createServer(async (req, res) => {
    let body = ''; for await (const chunk of req) body += chunk;
    calls.push({method:req.method, path:req.url, headers:req.headers, body});
    const post = req.method === 'POST';
    res.writeHead(post ? 400 : 200, {'Content-Type':'application/json'});
    res.end(JSON.stringify(post ? {error:'Email required; Password required'} : {data:[]}));
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  t.after(() => new Promise(resolve => server.close(resolve)));
  const result = await checkConnection(`http://127.0.0.1:${server.address().port}/`);
  assert.equal(result.jsonPost, true);
  assert.equal(calls.length, 2);
  assert.equal(calls[1].path, '/api/users/login');
  assert.equal(calls[1].headers['content-type'], 'application/json');
  assert.equal(calls[1].headers.authorization, 'Bearer studio-connection-probe');
  assert.equal(calls[1].body, '{}');
});

test('GET success and POST rejection cannot report a successful connection', async () => {
  const calls = [];
  await assert.rejects(checkConnection('http://localhost:8080', async (url, options) => {
    calls.push(options);
    if (options.method === 'POST') throw new TypeError('Failed to fetch');
    return Response.json({data:[]});
  }), /GET đã được, nhưng POST JSON chưa qua/);
  assert.equal(calls.length, 2);
  assert.ok(calls.every(c => c.credentials === 'omit' && c.redirect === 'error'));
});

test('a redirect is blocked consistently instead of a GET-only false positive', async t => {
  const server = http.createServer((req, res) => {
    if (req.url.startsWith('/api/products')) { res.writeHead(307, {Location:'/redirected'}); res.end(); }
    else { res.writeHead(200, {'Content-Type':'application/json'}); res.end('{"data":[]}'); }
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  t.after(() => new Promise(resolve => server.close(resolve)));
  const base = `http://127.0.0.1:${server.address().port}`;
  assert.equal((await fetch(base + '/api/products')).status, 200);
  await assert.rejects(checkConnection(base), /GET .*trình duyệt không cung cấp response/);
});

test('backend input is a root URL, never an API endpoint or URL credential', () => {
  assert.equal(normalizeBackendUrl(' http://localhost:8080/ '), 'http://localhost:8080');
  assert.throws(() => normalizeBackendUrl('http://localhost:8080/api/users/register'), /chỉ cần URL gốc/);
  assert.throws(() => normalizeBackendUrl('http://user:password@localhost:8080'), /URL gốc/);
});
