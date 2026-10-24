import test from 'node:test';
import assert from 'node:assert/strict';
import { resolveUser, injectMemoryContext } from './openai-proxy.mjs';
import { createApp } from './server.mjs';

test('resolveUser handles direct header, Bearer token, and backend fallback', async () => {
  const mockBackend = async (path, opts) => {
    if (path.startsWith('/api/v1/users?')) {
      return { users: [{ id: '11111111-1111-1111-1111-111111111111' }] };
    }
    return {};
  };

  // 1. Direct header
  const id1 = await resolveUser(mockBackend, { 'x-user-id': '22222222-2222-2222-2222-222222222222' });
  assert.equal(id1, '22222222-2222-2222-2222-222222222222');

  // 2. Bearer token
  const id2 = await resolveUser(mockBackend, { 'authorization': 'Bearer 33333333-3333-3333-3333-333333333333' });
  assert.equal(id2, '33333333-3333-3333-3333-333333333333');

  // 3. Fallback to existing user
  const id3 = await resolveUser(mockBackend, {});
  assert.equal(id3, '11111111-1111-1111-1111-111111111111');
});

test('injectMemoryContext cleanly inserts persistent memories before user query', () => {
  const messages = [
    { role: 'system', content: 'You are an AI.' },
    { role: 'user', content: 'What language do I like?' }
  ];

  const memories = [
    { memory: { content: 'User prefers Go for backend development', memory_type: 'PREFERENCE', status: 'ACTIVE' } }
  ];

  const augmented = injectMemoryContext(messages, memories);
  assert.equal(augmented.length, 3);
  assert.equal(augmented[1].role, 'system');
  assert.match(augmented[1].content, /User prefers Go/);
  assert.equal(augmented[2].role, 'user');
});

test('GET /v1/models returns models in standard OpenAI format', async (t) => {
  let server;
  await new Promise(r => {
    server = createApp({
      fetcher: async (url) => {
        if (url.endsWith('/api/tags')) {
          return new Response(JSON.stringify({
            models: [
              { name: 'llama3.2:latest', modified_at: '2026-10-24T12:00:00Z' }
            ]
          }));
        }
        return new Response('{}');
      }
    }).listen(0, '127.0.0.1', r);
  });
  t.after(() => server.close());

  const port = server.address().port;
  const res = await fetch(`http://127.0.0.1:${port}/v1/models`);
  assert.equal(res.status, 200);
  const json = await res.json();
  assert.equal(json.object, 'list');
  assert.equal(json.data.length, 1);
  assert.equal(json.data[0].id, 'llama3.2:latest');
  assert.equal(json.data[0].owned_by, 'ollama');
});

test('POST /v1/chat/completions non-streaming responds with OpenAI completion object and memory context', async (t) => {
  let upstreamPrompt = null;
  let server;

  await new Promise(r => {
    server = createApp({
      fetcher: async (url, opts) => {
        if (url.includes('/api/v1/embeddings/info')) {
          return new Response(JSON.stringify({ success: true, data: { model: 'nomic-embed-text' } }));
        }
        if (url.includes('/api/v1/retrieve')) {
          return new Response(JSON.stringify({
            success: true,
            data: {
              results: [
                { memory: { content: 'User is building Memory Engine', memory_type: 'PROJECT', status: 'ACTIVE' } }
              ]
            }
          }));
        }
        if (url.includes('/api/v1/users')) {
          return new Response(JSON.stringify({ success: true, data: { users: [{ id: '11111111-1111-1111-1111-111111111111' }] } }));
        }
        if (url.endsWith('/api/chat')) {
          upstreamPrompt = JSON.parse(opts.body);
          return new Response(JSON.stringify({
            message: { role: 'assistant', content: 'You are working on Memory Engine!' }
          }));
        }
        return new Response('{}');
      }
    }).listen(0, '127.0.0.1', r);
  });
  t.after(() => server.close());

  const port = server.address().port;
  const res = await fetch(`http://127.0.0.1:${port}/v1/chat/completions`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      model: 'llama3.2',
      messages: [{ role: 'user', content: 'What project am I on?' }],
      stream: false
    })
  });

  assert.equal(res.status, 200);
  const json = await res.json();
  assert.equal(json.object, 'chat.completion');
  assert.equal(json.choices[0].message.content, 'You are working on Memory Engine!');
  
  // Verify that upstream received the injected memory
  assert.ok(upstreamPrompt);
  const sysMsg = upstreamPrompt.messages.find(m => m.role === 'system' && m.content.includes('Memory Engine'));
  assert.ok(sysMsg, 'Expected memory context in system messages');
});

test('POST /v1/chat/completions streaming returns SSE chunks and terminates with [DONE]', async (t) => {
  let server;
  await new Promise(r => {
    server = createApp({
      fetcher: async (url, opts) => {
        if (url.includes('/api/v1/embeddings/info')) {
          return new Response(JSON.stringify({ success: true, data: { model: 'null-embedder' } }));
        }
        if (url.includes('/api/v1/users')) {
          return new Response(JSON.stringify({ success: true, data: { users: [{ id: '11111111-1111-1111-1111-111111111111' }] } }));
        }
        if (url.endsWith('/api/chat')) {
          // Stream NDJSON chunks from Ollama
          const encoder = new TextEncoder();
          const stream = new ReadableStream({
            start(controller) {
              controller.enqueue(encoder.encode(JSON.stringify({ message: { content: 'Hello' }, done: false }) + '\n'));
              controller.enqueue(encoder.encode(JSON.stringify({ message: { content: ' World' }, done: true }) + '\n'));
              controller.close();
            }
          });
          return new Response(stream);
        }
        return new Response('{}');
      }
    }).listen(0, '127.0.0.1', r);
  });
  t.after(() => server.close());

  const port = server.address().port;
  const res = await fetch(`http://127.0.0.1:${port}/v1/chat/completions`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      model: 'llama3.2',
      messages: [{ role: 'user', content: 'Hi' }],
      stream: true
    })
  });

  assert.equal(res.status, 200);
  assert.match(res.headers.get('content-type'), /text\/event-stream/);

  const text = await res.text();
  assert.match(text, /"delta":{"content":"Hello"}/);
  assert.match(text, /"delta":{"content":" World"}/);
  assert.match(text, /data: \[DONE\]/);
});

test('OPTIONS /v1/chat/completions handles CORS preflight', async (t) => {
  let server;
  await new Promise(r => {
    server = createApp().listen(0, '127.0.0.1', r);
  });
  t.after(() => server.close());

  const port = server.address().port;
  const res = await fetch(`http://127.0.0.1:${port}/v1/chat/completions`, {
    method: 'OPTIONS',
    headers: {
      'Origin': 'http://localhost:8080',
      'Access-Control-Request-Method': 'POST'
    }
  });

  assert.equal(res.status, 204);
  assert.equal(res.headers.get('access-control-allow-origin'), '*');
});
