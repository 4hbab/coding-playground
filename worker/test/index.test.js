// Run with: npm test (uses Node's built-in test runner, no extra dependencies)
import { test } from 'node:test';
import assert from 'node:assert/strict';
import worker, { toAppRequest } from '../src/index.js';

const visitor = (init = {}, path = '/api/execute?x=1') =>
    new Request('https://pyplayground.example.workers.dev' + path, init);

test('keeps method, path, query and body', async () => {
    const req = toAppRequest(visitor({ method: 'POST', body: '{"code":"print(1)"}', headers: { 'content-type': 'application/json' } }));
    assert.equal(req.method, 'POST');
    assert.equal(new URL(req.url).pathname + new URL(req.url).search, '/api/execute?x=1');
    assert.equal(req.headers.get('content-type'), 'application/json');
    assert.equal(await req.text(), '{"code":"print(1)"}');
});

test('sets X-Client-IP from CF-Connecting-IP', () => {
    const req = toAppRequest(visitor({ headers: { 'cf-connecting-ip': '198.51.100.7' } }));
    assert.equal(req.headers.get('x-client-ip'), '198.51.100.7');
});

test('drops IP headers the visitor tried to forge', () => {
    const req = toAppRequest(visitor({
        headers: {
            'cf-connecting-ip': '198.51.100.7',
            'x-client-ip': '1.1.1.1',
            'x-forwarded-for': '1.1.1.1',
            'x-real-ip': '1.1.1.1',
            'true-client-ip': '1.1.1.1',
            forwarded: 'for=1.1.1.1',
        },
    }));
    assert.equal(req.headers.get('x-client-ip'), '198.51.100.7');
    for (const h of ['x-forwarded-for', 'x-real-ip', 'true-client-ip', 'forwarded']) {
        assert.equal(req.headers.get(h), null, `${h} must not reach the app`);
    }
});

test('a forged X-Client-IP never survives, even without CF-Connecting-IP', () => {
    const req = toAppRequest(visitor({ headers: { 'x-client-ip': '1.1.1.1' } }));
    assert.equal(req.headers.get('x-client-ip'), null);
});

test('keeps cookies so sign-in works', () => {
    const req = toAppRequest(visitor({ headers: { cookie: 'pyplayground_token=abc' } }));
    assert.equal(req.headers.get('cookie'), 'pyplayground_token=abc');
});

test("does not follow the app's redirects", () => {
    assert.equal(toAppRequest(visitor({}, '/auth/github/login')).redirect, 'manual');
});

test('returns the app response unchanged', async () => {
    const env = { APP: { fetch: async () => new Response('ok', { status: 201, headers: { 'set-cookie': 'a=b' } }) } };
    const res = await worker.fetch(visitor(), env);
    assert.equal(res.status, 201);
    assert.equal(res.headers.get('set-cookie'), 'a=b');
    assert.equal(await res.text(), 'ok');
});

test('answers 503 when the app is unreachable', async () => {
    const env = { APP: { fetch: async () => { throw new Error('tunnel down'); } } };
    const original = console.error;
    console.error = () => {}; // keep test output clean
    try {
        const res = await worker.fetch(visitor(), env);
        assert.equal(res.status, 503);
        assert.equal(res.headers.get('retry-after'), '120');
    } finally {
        console.error = original;
    }
});
