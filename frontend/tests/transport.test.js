import assert from 'node:assert/strict';
import { test, beforeEach, afterEach } from 'node:test';
import { apiFetch, apiStatus, getAPIError, unavailableNodes } from '../src/utils/apiFetch.js';
import { splitLogChunk, reconnectDelay } from '../src/utils/wsAuth.js';
import { secureStorage } from '../src/utils/storage.js';
import viteConfig from '../vite.config.js';

const originalStorage = globalThis.localStorage;

beforeEach(() => {
  const values = new Map();
  globalThis.localStorage = {
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
    removeItem: (key) => values.delete(key),
  };
  for (const path of Object.keys(apiStatus)) delete apiStatus[path];
});

afterEach(() => {
  if (originalStorage === undefined) delete globalThis.localStorage;
  else globalThis.localStorage = originalStorage;
});

test('partial inventory metadata rejects malformed values and removes duplicates', () => {
  for (const value of ['invalid', '{}', 'null']) {
    assert.deepEqual(unavailableNodes(new Response('', { headers: { 'X-LightHouse-Unavailable-Nodes': value } })), []);
  }
  const response = new Response('', { headers: { 'X-LightHouse-Unavailable-Nodes': '["spoke-east",null,"",42,"spoke-east"]' } });
  assert.deepEqual(unavailableNodes(response), ['spoke-east']);
});

test('API requests preserve Request headers and attach session authentication', async (context) => {
  secureStorage.setItem('token', 'test-session');
  context.mock.method(globalThis, 'fetch', async (input, init) => {
    assert.equal(init.headers.get('X-LightHouse-Client'), 'web');
    assert.equal(init.headers.get('Authorization'), 'Bearer test-session');
    assert.equal(init.headers.get('X-Custom'), 'original');
    return Response.json([]);
  });
  await apiFetch(new Request('http://localhost/api/containers', { headers: { 'X-Custom': 'original' } }));
  assert.equal(apiStatus['/api/containers'].error, '');
});

test('partial inventory warnings clear after a successful refresh', async (context) => {
  let unavailable = true;
  context.mock.method(globalThis, 'fetch', async () => Response.json([], {
    headers: unavailable ? { 'X-LightHouse-Unavailable-Nodes': '["spoke-east"]' } : {},
  }));
  await apiFetch('/api/images');
  assert.deepEqual([...apiStatus['/api/images'].unavailableNodes], ['spoke-east']);
  unavailable = false;
  await apiFetch('/api/images');
  assert.deepEqual([...apiStatus['/api/images'].unavailableNodes], []);
});

test('late responses cannot replace the latest inventory status', async (context) => {
  let finishOldRequest;
  let requests = 0;
  context.mock.method(globalThis, 'fetch', () => {
    requests += 1;
    if (requests === 1) return new Promise((resolve) => { finishOldRequest = resolve; });
    return Promise.resolve(Response.json([]));
  });
  const oldRequest = apiFetch('/api/containers');
  await apiFetch('/api/containers');
  finishOldRequest(new Response('', { status: 503 }));
  await oldRequest;
  assert.equal(apiStatus['/api/containers'].error, '');
});

test('HTTP failures are reported, while request cancellation preserves the current status', async (context) => {
  context.mock.method(globalThis, 'fetch', async () => new Response('', { status: 502 }));
  await apiFetch('/api/networks');
  assert.match(apiStatus['/api/networks'].error, /502/);
  context.mock.method(globalThis, 'fetch', async () => { throw new DOMException('Cancelled', 'AbortError'); });
  await assert.rejects(apiFetch('/api/networks'), { name: 'AbortError' });
  assert.match(apiStatus['/api/networks'].error, /502/);
});

test('API errors preserve actionable server messages and tolerate non-JSON failures', async () => {
  assert.equal(await getAPIError(Response.json({ error: 'Spoke upgrade required' })), 'Spoke upgrade required');
  assert.equal(await getAPIError(new Response('<html>proxy error</html>'), 'Node unavailable'), 'Node unavailable');
});

test('log chunks preserve partial lines and split multiline Docker output', () => {
  const first = splitLogChunk('first line\nsecond');
  assert.deepEqual(first, { lines: ['first line'], remainder: 'second' });
  const second = splitLogChunk(' line\r\nthird line\n', first.remainder);
  assert.deepEqual(second, { lines: ['second line', 'third line'], remainder: '' });
  assert.deepEqual(splitLogChunk('without a newline'), { lines: [], remainder: 'without a newline' });
});

test('WebSocket retries back off without exceeding thirty seconds', () => {
  assert.deepEqual([0, 1, 2, 3, 4, 100].map(reconnectDelay), [3000, 6000, 12000, 24000, 30000, 30000]);
});

test('development API, OAuth, and WebSocket requests reach the backend', () => {
  for (const path of ['/api', '/auth', '/ws']) {
    assert.equal(viteConfig.server.proxy[path].target, process.env.LIGHTHOUSE_API_TARGET || 'http://127.0.0.1:8000');
  }
  assert.equal(viteConfig.server.proxy['/ws'].ws, true);
  assert.equal(viteConfig.server.proxy['/api'].ws, true);
});