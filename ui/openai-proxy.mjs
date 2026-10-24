import crypto from 'node:crypto';
import { ndjson } from './server.mjs';

const UUID_REGEX = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/**
 * Resolves the target user ID from request headers or retrieves/creates a default user.
 */
export async function resolveUser(backend, headers = {}) {
  // 1. Direct X-User-Id header
  const customID = headers['x-user-id'];
  if (customID && UUID_REGEX.test(customID)) return customID;

  // 2. Bearer token if formatted as UUID
  const auth = headers['authorization'] || '';
  if (auth.startsWith('Bearer ')) {
    const token = auth.slice(7).trim();
    if (UUID_REGEX.test(token)) return token;
  }

  // 3. Look up existing users in Memory Engine
  try {
    const userList = await backend('/api/v1/users?page_size=1');
    if (userList?.users?.length > 0) {
      return userList.users[0].id;
    }

    // 4. Create default proxy user if none exists
    const newUser = await backend('/api/v1/users', {
      method: 'POST',
      body: JSON.stringify({
        username: 'openai_proxy_user',
        display_name: 'OpenAI Proxy User'
      })
    });
    return newUser.id;
  } catch {
    // Fallback pseudo-UUID if backend is unreachable during validation
    return '00000000-0000-0000-0000-000000000001';
  }
}

/**
 * Injects retrieved memories into messages payload for upstream LLM.
 */
export function injectMemoryContext(messages, memories = []) {
  if (!memories.length) return messages;

  const validMemories = memories.filter(r => {
    const m = r.memory || r;
    return m && !['ARCHIVED', 'DELETED'].includes(m.status);
  });

  if (!validMemories.length) return messages;

  const lines = validMemories.slice(0, 8).map((r, i) => {
    const m = r.memory || r;
    const type = m.memory_type || 'FACT';
    return `[${i + 1}] (${type}) ${m.content}`;
  });

  const memoryContextMsg = {
    role: 'system',
    content: `[Persistent Long-Term Memory Context]\nThe following saved memories and facts were retrieved for this conversation:\n${lines.join('\n')}\nUse these saved memories naturally to inform your response. Do not invent contradictory facts.`
  };

  const copy = [...messages];
  const lastUserIdx = copy.findLastIndex(m => m.role === 'user');
  if (lastUserIdx !== -1) {
    copy.splice(lastUserIdx, 0, memoryContextMsg);
  } else {
    copy.unshift(memoryContextMsg);
  }
  return copy;
}

/**
 * Handles GET /v1/models
 */
export async function handleModels(req, res, { ollamaURL, fetcher = fetch }) {
  try {
    const upstream = await fetcher(ollamaURL + '/api/tags', {
      signal: AbortSignal.timeout(5000)
    });
    if (!upstream.ok) {
      throw new Error('Ollama service unavailable');
    }
    const tags = await upstream.json();
    const data = (tags.models || []).map(m => ({
      id: m.name,
      object: 'model',
      created: Math.floor(new Date(m.modified_at || Date.now()).getTime() / 1000),
      owned_by: 'ollama'
    }));

    res.writeHead(200, {
      'Content-Type': 'application/json',
      'Access-Control-Allow-Origin': '*'
    });
    res.end(JSON.stringify({ object: 'list', data }));
  } catch (err) {
    res.writeHead(502, {
      'Content-Type': 'application/json',
      'Access-Control-Allow-Origin': '*'
    });
    res.end(JSON.stringify({
      error: {
        message: err.message || 'Failed to list models',
        type: 'server_error',
        code: 502
      }
    }));
  }
}

/**
 * Handles POST /v1/chat/completions
 */
export async function handleChatCompletions(req, res, { backend, fetcher = fetch, ollamaURL, body, signal }) {
  const model = body.model;
  if (!model || typeof model !== 'string') {
    res.writeHead(400, { 'Content-Type': 'application/json', 'Access-Control-Allow-Origin': '*' });
    return res.end(JSON.stringify({ error: { message: 'model parameter is required', type: 'invalid_request_error' } }));
  }

  const rawMessages = Array.isArray(body.messages) ? body.messages : [];
  const lastUserMsg = rawMessages.findLast(m => m.role === 'user');
  const query = typeof lastUserMsg?.content === 'string' ? lastUserMsg.content.trim() : '';

  const namespace = req.headers['x-namespace'] || 'DEFAULT';
  const userID = await resolveUser(backend, req.headers);

  // 1. Retrieve relevant memories if user query is present
  let retrieved = [];
  if (query && body.use_memory !== false) {
    try {
      const info = await backend('/api/v1/embeddings/info', { signal }).catch(() => ({ model: 'null-embedder' }));
      if (info.model === 'null-embedder') {
        const fts = await backend(`/api/v1/users/${userID}/memories/search?q=${encodeURIComponent(query)}&namespace=${encodeURIComponent(namespace)}&mode=OR&page_size=5`, { signal });
        retrieved = fts.results || [];
      } else {
        const resp = await backend('/api/v1/retrieve', {
          method: 'POST',
          signal,
          body: JSON.stringify({ user_id: userID, query, namespace, top_k: 5, max_tokens: 1500 })
        });
        retrieved = resp.results || [];
      }
    } catch {
      // Memory retrieval is resilient — fail forward to generation without crashing
      retrieved = [];
    }
  }

  // 2. Augment prompt with memory context
  const augmentedMessages = injectMemoryContext(rawMessages, retrieved);

  // 3. Send to Ollama
  const isStreaming = Boolean(body.stream);
  const upstream = await fetcher(ollamaURL + '/api/chat', {
    method: 'POST',
    signal,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      model,
      stream: isStreaming,
      messages: augmentedMessages.map(m => ({ role: m.role, content: m.content })),
      options: {
        temperature: body.temperature ?? 0.7,
        top_p: body.top_p ?? 1.0
      }
    })
  });

  if (!upstream.ok) {
    const errorJson = await upstream.json().catch(() => ({}));
    res.writeHead(502, { 'Content-Type': 'application/json', 'Access-Control-Allow-Origin': '*' });
    return res.end(JSON.stringify({
      error: {
        message: errorJson.error || 'Upstream LLM error',
        type: 'upstream_error',
        code: upstream.status
      }
    }));
  }

  const completionID = 'chatcmpl-' + crypto.randomUUID().replace(/-/g, '').slice(0, 24);
  const createdTime = Math.floor(Date.now() / 1000);

  // 4. Stream output (Server-Sent Events)
  if (isStreaming) {
    res.writeHead(200, {
      'Content-Type': 'text/event-stream; charset=utf-8',
      'Cache-Control': 'no-cache, no-transform',
      'Connection': 'keep-alive',
      'Access-Control-Allow-Origin': '*',
      'X-Accel-Buffering': 'no'
    });

    for await (const chunk of ndjson(upstream.body)) {
      if (chunk.error) {
        res.write(`data: ${JSON.stringify({ error: { message: chunk.error } })}\n\n`);
        break;
      }
      if (chunk.message?.content) {
        const payload = {
          id: completionID,
          object: 'chat.completion.chunk',
          created: createdTime,
          model,
          choices: [
            {
              index: 0,
              delta: { content: chunk.message.content },
              finish_reason: null
            }
          ]
        };
        res.write(`data: ${JSON.stringify(payload)}\n\n`);
      }
      if (chunk.done) {
        break;
      }
    }

    const finalPayload = {
      id: completionID,
      object: 'chat.completion.chunk',
      created: createdTime,
      model,
      choices: [
        {
          index: 0,
          delta: {},
          finish_reason: 'stop'
        }
      ]
    };
    res.write(`data: ${JSON.stringify(finalPayload)}\n\n`);
    res.write('data: [DONE]\n\n');
    return res.end();
  }

  // 5. Non-streaming output
  const reply = await upstream.json();
  const content = reply.message?.content || '';
  const promptLen = rawMessages.reduce((n, m) => n + (m.content?.length || 0), 0);
  const promptTokens = Math.ceil(promptLen / 4);
  const completionTokens = Math.ceil(content.length / 4);

  res.writeHead(200, {
    'Content-Type': 'application/json; charset=utf-8',
    'Access-Control-Allow-Origin': '*'
  });
  res.end(JSON.stringify({
    id: completionID,
    object: 'chat.completion',
    created: createdTime,
    model,
    choices: [
      {
        index: 0,
        message: {
          role: 'assistant',
          content
        },
        finish_reason: 'stop'
      }
    ],
    usage: {
      prompt_tokens: promptTokens,
      completion_tokens: completionTokens,
      total_tokens: promptTokens + completionTokens
    }
  }));
}
