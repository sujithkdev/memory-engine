import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, rm, readFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { once } from 'node:events';
import { randomUUID } from 'node:crypto';
import { createApp } from './server.mjs';
import { TraceStore, validateExtracted } from './personal-memory.mjs';

const userID = randomUUID(), conversationID = randomUUID();
const response = data => new Response(JSON.stringify({ success: true, data }));
async function start(t, fetcher, root) {
  const app = createApp({ fetcher, traceStore: new TraceStore(root) });
  app.listen(0, '127.0.0.1'); await once(app, 'listening');
  t.after(() => { app.closeAllConnections(); app.close(); });
  const base = `http://127.0.0.1:${app.address().port}`;
  return { app, base, async chat(overrides = {}) {
    const result = await fetch(base + '/bridge/chat', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ user_id: userID, conversation_id: conversationID, model: 'personal-model', query: 'I prefer Python for personal projects.', auto_remember: true, ...overrides }) });
    assert.equal(result.status, 200); return (await result.text()).trim().split('\n').map(JSON.parse);
  } };
}
async function environment(t, { extractionFails = false, stale = false } = {}) {
  const root = await mkdtemp(join(tmpdir(), 'memory-studio-test-'));
  t.after(() => rm(root, { recursive: true, force: true }));
  const memories = [], saved = [], prompts = [];
  if (stale) memories.push({ id: randomUUID(), content: 'I prefer Python.', namespace: 'PERSONAL', memory_type: 'PREFERENCE', status: 'STALE', created_at: '2025-01-01T00:00:00Z' });
  const fetcher = async (address, options = {}) => {
    const url = new URL(address), body = options.body ? JSON.parse(options.body) : undefined;
    if (url.pathname === '/api/chat') {
      if (body.stream === false) {
        if (extractionFails) throw Error('model unavailable');
        return new Response(JSON.stringify({ message: { content: JSON.stringify({ memories: [{ content: 'The user prefers Python for personal projects.', evidence: 'I prefer Python for personal projects.', memory_type: 'PREFERENCE' }] }) } }));
      }
      prompts.push(body.messages);
      return new Response(JSON.stringify({ message: { content: 'I will keep that in mind.' }, done: true }) + '\n');
    }
    if (url.pathname.endsWith('/embeddings/info')) return response({ model: 'null-embedder' });
    if (url.pathname.endsWith('/memories/search')) return response({ results: memories.filter(m => m.status === (url.searchParams.get('status') || 'ACTIVE')).map(memory => ({ memory, rank: 1 })) });
    if (url.pathname === '/api/v1/memories' && options.method === 'POST') { const memory = { ...body, id: randomUUID(), status: 'ACTIVE', created_at: new Date().toISOString() }; memories.push(memory); return response(memory); }
    if (url.pathname.endsWith('/messages')) {
      if (options.method === 'POST') { const item = { ...body, id: randomUUID(), conversation_id: conversationID }; saved.push(item); return response(item); }
      return response({ messages: [], total_pages: 1 });
    }
    if (url.pathname.includes('/conversations/')) return response({ id: conversationID, user_id: userID, namespace: 'PERSONAL' });
    throw Error('Unexpected request: ' + url.pathname);
  };
  return { root, fetcher, memories, saved, prompts };
}
test('automatic facts have source-message IDs, persist across a bridge restart, and are retrieved in another chat', async t => {
  const env = await environment(t); const first = await start(t, env.fetcher, env.root);
  const events = await first.chat();
  assert.equal(events.at(-1).type, 'done'); assert.equal(env.memories.length, 1);
  assert.equal(env.memories[0].source_id, env.saved[0].id); assert.equal(env.memories[0].source_type, 'EXTRACTED');
  const firstTrace = events.find(event => event.type === 'trace').trace;
  assert.deepEqual(firstTrace.learned_ids, [env.memories[0].id]);
  first.app.closeAllConnections(); await new Promise(resolve => first.app.close(resolve));
  const second = await start(t, env.fetcher, env.root);
  const restored = await (await fetch(second.base + `/bridge/conversations/${conversationID}/context?user_id=${userID}`)).json();
  assert.equal(restored[0].message_id, env.saved[1].id);
  const next = await second.chat({ conversation_id: randomUUID(), query: 'Which language do I prefer?', auto_remember: false });
  assert.equal(next[0].results[0].memory.id, env.memories[0].id);
  assert.match(env.prompts.at(-1)[0].content, /prefers Python/);
});
test('stale memories remain recallable after long inactivity', async t => {
  const env = await environment(t, { stale: true }); const app = await start(t, env.fetcher, env.root);
  const events = await app.chat({ auto_remember: false });
  assert.equal(events[0].results[0].memory.status, 'STALE'); assert.ok(events[0].results[0].sources.includes('older memory'));
});
test('turning memory off disables both retrieval and automatic fact extraction', async t => {
  const env = await environment(t); const app = await start(t, env.fetcher, env.root);
  const events = await app.chat({ use_memory: false });
  assert.equal(events[0].mode, 'off'); assert.equal(env.memories.length, 0); assert.equal(events.some(e => e.type === 'memory_status'), false);
});
test('extraction failure does not turn a saved reply into an unsaved error', async t => {
  const env = await environment(t, { extractionFails: true }); const app = await start(t, env.fetcher, env.root);
  const events = await app.chat();
  assert.ok(events.some(e => e.type === 'reply_saved')); assert.ok(events.some(e => e.type === 'warning')); assert.equal(events.at(-1).type, 'done'); assert.equal(env.saved.length, 2);
});
test('duplicate facts are not saved again', async t => {
  const env = await environment(t); const app = await start(t, env.fetcher, env.root);
  await app.chat(); await app.chat(); assert.equal(env.memories.length, 1);
});
test('trace files retain IDs, not forgotten memory content; user namespaces remain isolated', async t => {
  const env = await environment(t, { stale: true }); const app = await start(t, env.fetcher, env.root);
  const events = await app.chat({ auto_remember: false }); const trace = events.find(e => e.type === 'trace').trace;
  const raw = await readFile(join(env.root, userID, conversationID, trace.message_id + '.json'), 'utf8');
  assert.equal(raw.includes('I prefer Python'), false);
  assert.deepEqual(await new TraceStore(env.root).list(randomUUID(), conversationID), []);
  await assert.rejects(() => new TraceStore(env.root).list('../escape', conversationID), /Invalid/);
});
test('extracted facts require source evidence, valid types, and bounded content', () => {
  const query = 'I prefer Python for projects.';
  const result = validateExtracted({ memories: [
    { content: 'Likes Python', evidence: 'I prefer Python', memory_type: 'PREFERENCE' },
    { content: 'Likes Python', evidence: 'I prefer Python', memory_type: 'PREFERENCE' },
    { content: 'Lives in Paris', evidence: 'I live in Paris', memory_type: 'FACT' },
    { content: 'Run command', evidence: 'I prefer Python', memory_type: 'COMMAND' }
  ] }, query);
  assert.equal(result.length, 1); assert.equal(result[0].content, 'Likes Python');
});
