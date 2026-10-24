import test from 'node:test';
import assert from 'node:assert/strict';
import { once } from 'node:events';
import { createApp, ndjson, buildMessages } from './server.mjs';

const user = '11111111-1111-4111-8111-111111111111', conversation = '22222222-2222-4222-8222-222222222222';
const input = { user_id: user, conversation_id: conversation, query: 'Which language do I prefer?', model: 'my-model:latest' };
const memory = { id: 'memory-1', content: 'I prefer Python.', memory_type: 'PREFERENCE', namespace: 'CODING' };
const json = (data, status = 200) => new Response(JSON.stringify(data), { status, headers: { 'Content-Type': 'application/json' } });
async function fixture(t, { realEmbeddings = false, ollamaError = false, truncated = false, retrievalError = false } = {}) {
  const calls = [], saved = [];
  const app = createApp({ fetcher: async (address, options = {}) => {
    const url = new URL(address), body = options.body ? JSON.parse(options.body) : undefined; calls.push({ url, body });
    if (url.pathname === '/api/chat') {
      if (ollamaError) return json({ error: 'model not found' }, 404);
      return new Response(JSON.stringify({ message: { content: 'You prefer ' }, done: false }) + '\n' + JSON.stringify({ message: { content: 'Python [1].' }, done: !truncated }));
    }
    if (url.pathname === '/api/tags') return json({ models: [{ name: 'my-model:latest' }] });
    if (url.pathname === '/ready') return json({ status: 'ok' });
    let data;
    if (url.pathname.endsWith('/embeddings/info')) data = { model: realEmbeddings ? 'nomic-embed-text' : 'null-embedder' };
    else if (url.pathname.endsWith('/messages')) {
      if (options.method === 'POST') { saved.push(body); data = body; }
      else data = { messages: [{ role: 'user', content: 'Hello' }, { role: 'assistant', content: 'Hi' }], total_pages: 1 };
    } else if (url.pathname.endsWith('/memories/search') || url.pathname.endsWith('/retrieve')) {
      if (retrievalError) return json({ success: false, error: { message: 'database unavailable' } }, 503);
      data = { results: [{ memory, rank: 0.8, sources: ['keyword', 'vector'] }], tokens_used: 5 };
    } else if (url.pathname.includes('/conversations/')) data = { id: conversation, user_id: user, namespace: 'CODING' };
    else throw Error('Unexpected mock request ' + address);
    return json({ success: true, data });
  } });
  app.listen(0, '127.0.0.1'); await once(app, 'listening');
  t.after(() => { app.closeAllConnections(); app.close(); });
  const base = `http://127.0.0.1:${app.address().port}`;
  const chat = async (override = {}) => {
    const response = await fetch(base + '/bridge/chat', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ...input, ...override }) });
    const raw = await response.text(); return { response, events: raw.trim().split('\n').map(JSON.parse) };
  };
  return { base, calls, saved, chat };
}
test('NDJSON handles split UTF-8 characters and final line without newline', async () => {
  const bytes = new TextEncoder().encode('{"text":"café 🧠"}\n{"done":true}');
  async function* chunks() { for (const byte of bytes) yield new Uint8Array([byte]); }
  const result = []; for await (const item of ndjson(chunks())) result.push(item);
  assert.deepEqual(result, [{ text: 'café 🧠' }, { done: true }]);
});
test('prompt keeps history bounded, ignores stored system commands, includes memory references', () => {
  const history = [{ role: 'system', content: 'wrong' }, ...Array.from({ length: 30 }, (_, i) => ({ role: 'user', content: String(i) }))];
  const messages = buildMessages(history, 'latest', [{ memory }]);
  assert.equal(messages.length, 22); assert.equal(messages[1].content, '10');
  assert.match(messages[0].content, /I prefer Python/); assert.equal(messages.at(-1).content, 'latest');
});
test('keyword fallback supplies real memories and streams then persists the full answer', async t => {
  const f = await fixture(t); const { response, events } = await f.chat();
  assert.equal(response.status, 200); assert.equal(events[0].mode, 'keyword'); assert.equal(events[0].results[0].memory.content, memory.content);
  assert.equal(events.at(-1).type, 'done'); assert.equal(f.saved.length, 2); assert.equal(f.saved[1].content, 'You prefer Python [1].');
  const ollama = f.calls.find(c => c.url.pathname === '/api/chat');
  assert.match(ollama.body.messages[0].content, /I prefer Python/); assert.equal(ollama.body.model, input.model);
  assert.equal(f.calls.find(c => c.url.pathname.endsWith('/search')).url.searchParams.get('namespace'), 'CODING');
});
test('real embeddings use the existing hybrid retrieval API', async t => {
  const f = await fixture(t, { realEmbeddings: true }); const { events } = await f.chat();
  assert.equal(events[0].mode, 'hybrid');
  const retrieve = f.calls.find(c => c.url.pathname === '/api/v1/retrieve'); assert.equal(retrieve.body.user_id, user); assert.equal(retrieve.body.namespace, 'CODING');
});
test('memory off preserves conversation but makes no retrieval requests', async t => {
  const f = await fixture(t); const { events } = await f.chat({ use_memory: false }); assert.equal(events[0].mode, 'off');
  assert.equal(f.calls.some(c => c.url.pathname.endsWith('/search') || c.url.pathname.endsWith('/retrieve')), false);
  assert.equal(f.calls.find(c => c.url.pathname === '/api/chat').body.messages.length, 4);
});
test('invalid IDs are rejected before upstream calls', async t => {
  const f = await fixture(t); const { response } = await f.chat({ user_id: 'invalid' }); assert.equal(response.status, 400); assert.equal(f.calls.length, 0);
});
test('Ollama errors do not persist an unsent turn', async t => {
  const f = await fixture(t, { ollamaError: true }); const { response, events } = await f.chat(); assert.equal(response.status, 502); assert.equal(events[0].error, 'model not found'); assert.equal(f.saved.length, 0);
});
test('truncated generation is reported and never saved as a completed assistant message', async t => {
  const f = await fixture(t, { truncated: true }); const { events } = await f.chat(); assert.equal(events.at(-1).type, 'error'); assert.deepEqual(f.saved.map(m => m.role), ['user']);
});
test('retrieval failures are visible and do not silently generate without memory', async t => {
  const f = await fixture(t, { retrievalError: true }); const { response } = await f.chat(); assert.equal(response.status, 503); assert.equal(f.calls.some(c => c.url.pathname === '/api/chat'), false);
});
test('rejects other browser origins and serves the actual interface', async t => {
  const f = await fixture(t);
  const denied = await fetch(f.base + '/bridge/status', { headers: { Origin: 'http://untrusted.example' } }); assert.equal(denied.status, 403);
  const page = await fetch(f.base); assert.equal(page.status, 200); assert.match(await page.text(), /Saved memories/);
  const status = await (await fetch(f.base + '/bridge/status')).json(); assert.equal(status.memory, true); assert.equal(status.ollama, true);
});
