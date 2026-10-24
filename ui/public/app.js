import { initModelPicker } from './model-picker.js';
const $ = id => document.getElementById(id);
const settingsKey = 'memory-studio.settings';
let settings = {}; try { settings = JSON.parse(localStorage.getItem(settingsKey) || '{}'); } catch {}
let active = null, generating = false, controller = null, connected = {}, libraryPage = 1;
let latestContext = null;
const welcome = $('messages').innerHTML;
function el(tag, className, text) { const node = document.createElement(tag); if (className) node.className = className; if (text !== undefined) node.textContent = text; return node; }
function notice(text = '') { $('notice').textContent = text; $('notice').hidden = !text; }
async function api(path, options = {}) {
  const response = await fetch(path, { ...options, headers: { 'Content-Type': 'application/json' } });
  const data = await response.json();
  if (!response.ok || data.success === false) throw Error(typeof data.error === 'string' ? data.error : data.error?.message || 'Request failed');
  return data.data ?? data;
}
const post = (path, body) => api(path, { method: 'POST', body: JSON.stringify(body) });
function persist() { localStorage.setItem(settingsKey, JSON.stringify(settings)); }
function namespace() { return active?.namespace || settings.namespace || 'DEFAULT'; }
function resetContext() { latestContext = null; $('context-list').replaceChildren(el('div', 'empty-context', 'Relevant memories will appear here when you send a message.')); $('context-count').textContent = '0'; $('context-status').textContent = 'Ready when you are'; }
function renderContext(context) {
  latestContext = context;
  $('context-list').replaceChildren(); const results = context.results || [];
  $('context-count').textContent = results.length;
  $('context-status').textContent = context.mode === 'off' ? 'Memory is off for this reply' : `${results.length} ${results.length === 1 ? 'memory' : 'memories'} retrieved`;
  $('context-status').append(el('small', '', context.mode === 'off' ? 'Using conversation history only.' : `${context.mode === 'keyword' ? 'Keyword search' : 'Hybrid search'} · ~${context.tokens_used || 0} context tokens`));
  for (const [index, result] of results.entries()) {
    const card = el('article', 'context-card'); card.append(el('small', '', `[${index + 1}] ${result.memory.memory_type} · ${result.memory.namespace}`), el('p', '', result.memory.content), el('footer', '', (result.sources || []).join(' + '))); $('context-list').append(card);
  }
  if (!results.length) $('context-list').append(el('div', 'empty-context', context.mode === 'off' ? 'No memories were sent to the model.' : 'No matching memories yet. Add a useful detail to get started.'));
}
function message(role, content, saved = true) {
  if ($('welcome')) $('messages').replaceChildren();
  const article = el('article', `message ${role}`), body = el('div', 'message-content'), text = el('div', 'message-text', content);
  article.append(el('div', 'message-avatar', role === 'user' ? 'Y' : '✳'), body);
  body.append(el('div', 'message-name', role === 'user' ? 'You' : 'Assistant'), text);
  if (content && saved) addSave(body, content);
  $('messages').append(article); scrollChat(); return { text, body, article };
}
function addSave(body, content) { const actions = el('div', 'message-actions'), save = el('button', '', '◈ Save as memory'); save.type = 'button'; save.onclick = () => openMemory(content); actions.append(save); body.append(actions); }
function scrollChat() { $('messages').scrollTop = $('messages').scrollHeight; }
function newChat() { if (generating || loadingConversation) return; active = null; delete settings.last_chat; persist(); $('messages').innerHTML = welcome; $('page-title').textContent = 'New conversation'; resetContext(); notice(); $('sidebar').classList.remove('open'); loadConversations().catch(e => notice(e.message)); $('prompt').focus(); }
async function loadConversations() {
  if (!settings.user_id) return;
  const result = await api(`/api/v1/users/${settings.user_id}/conversations?page_size=100&namespace=${encodeURIComponent(settings.namespace || 'DEFAULT')}`);
  $('conversations').replaceChildren(); $('chat-count').textContent = result.total || 0;
  for (const conversation of result.conversations || []) {
    const button = el('button', active?.id === conversation.id ? 'active' : '', '◷  ' + (conversation.title || 'Untitled conversation'));
    button.disabled = generating; button.onclick = () => openConversation(conversation).catch(e => notice(e.message)); $('conversations').append(button);
  }
  if (!result.conversations?.length) $('conversations').append(el('p', 'small muted', 'Your next idea starts here.'));
}
let loadingConversation = false;
async function openConversation(conversation) {
  if (generating || loadingConversation) return;
  loadingConversation = true; $('send').disabled = true;
  try {
    const path = `/api/v1/conversations/${conversation.id}/messages?user_id=${settings.user_id}&page_size=200`;
    const first = await api(path); let items = first.messages || [];
    for (let page = 2; page <= first.total_pages; page++) items.push(...(await api(path + '&page=' + page)).messages || []);
    active = conversation; settings.last_chat = conversation.id; persist(); $('messages').replaceChildren(); $('page-title').textContent = conversation.title || 'Conversation';
    let traces = []; try { traces = await api(`/bridge/conversations/${conversation.id}/context?user_id=${settings.user_id}`); } catch { notice('Chat restored. Its saved memory maps could not be loaded.'); }
    for (const item of items) { const shown = message(item.role, item.content); const trace = traces.find(t => t.message_id === item.id); if (trace) addTrace(shown.body, trace); }
    resetContext(); $('sidebar').classList.remove('open'); await loadConversations();
  } finally { loadingConversation = false; $('send').disabled = false; }
}
async function refresh() {
  connected = await api('/bridge/status');
  const ready = connected.memory && connected.ollama;
  $('connection-state').classList.toggle('connected', ready);
  $('connection-state').replaceChildren(el('i'), document.createTextNode(ready ? 'Services connected' : 'Setup needed'));
  const previous = $('model').value || settings.model;
  $('model').replaceChildren();
  for (const model of connected.models) { const option = el('option', '', model.name); option.value = model.name; $('model').append(option); }
  if (!connected.models.length) { const option = el('option', '', connected.ollama ? 'No models installed' : 'Ollama is offline'); option.value = ''; $('model').append(option); }
  if (connected.models.some(m => m.name === previous)) $('model').value = previous;
  $('retrieval-mode').textContent = !connected.memory ? 'Start Memory Engine to enable retrieval.' : connected.embeddings?.model === 'null-embedder' ? 'Keyword retrieval active · Configure Ollama embeddings for semantic search.' : `Hybrid retrieval · ${connected.embeddings?.model || 'embedding status unavailable'}`;
  $('service-details').replaceChildren(el('div', '', `Memory Engine: ${connected.memory ? 'connected' : 'offline — start the Go server on port 8080'}`), el('div', '', `Ollama: ${connected.ollama ? 'connected' : 'offline — start Ollama on port 11434'}`), el('div', '', `${connected.models.length} installed models found`));
  if (!ready) notice(`Start ${[!connected.memory && 'Memory Engine', !connected.ollama && 'Ollama'].filter(Boolean).join(' and ')}, then open Connection settings → Refresh services. You can explore the interface now.`); else notice();
}
async function openSettings() {
  if (generating) return;
  $('settings-error').textContent = ''; $('settings-dialog').showModal();
  $('profile-select').replaceChildren(new Option('Create a new profile', ''));
  $('namespace').value = settings.namespace || 'DEFAULT';
  try {
    await refresh();
    if (connected.memory) {
      const users = await api('/api/v1/users?page_size=100');
      for (const user of users.users || []) $('profile-select').append(new Option(user.display_name || user.username, user.id));
      if (settings.user_id) $('profile-select').value = settings.user_id;
    }
  } catch (e) { $('settings-error').textContent = e.message; }
  $('name-label').hidden = !!$('profile-select').value;
}
function openMemory(content = '') { if (!settings.user_id) { openSettings(); return; } $('memory-error').textContent = ''; $('memory-content').value = content.slice(0, 10000); $('memory-dialog').showModal(); }
async function loadLibrary(append = false) {
  if (!settings.user_id) { openSettings(); return; }
  if (!append) { libraryPage = 1; $('library-list').replaceChildren(); if (!$('library-dialog').open) $('library-dialog').showModal(); }
  try {
    const data = await api(`/api/v1/users/${settings.user_id}/memories?status=${$('library-status').value}&namespace=${encodeURIComponent(namespace())}&page_size=20&page=${libraryPage}`);
    for (const memory of data.memories || []) {
      const card = el('article', 'library-card'); card.append(el('small', '', `${memory.memory_type} · ${memory.namespace}`), el('p', '', memory.content), el('div', 'source-pill', `${memory.source_type === 'EXTRACTED' ? 'Auto-saved from your message' : 'Saved memory'} · ${new Date(memory.created_at).toLocaleDateString()}`));
      const forget = el('button', 'outline-button', 'Forget');
      forget.onclick = async () => { forget.disabled = true; try { await post(`/api/v1/memories/${memory.id}/forget`, { user_id: settings.user_id, reason: 'Removed from Memory Studio library' }); card.remove(); resetContext(); } catch (e) { card.append(el('p', 'form-error', e.message)); forget.disabled = false; } };
      card.append(forget); $('library-list').append(card);
    }
    if (!data.total) $('library-list').append(el('p', 'muted', 'No saved memories in this workspace yet.'));
    $('more-memories').hidden = libraryPage >= data.total_pages;
  } catch (e) { $('library-list').append(el('p', 'form-error', e.message)); }
}
$('chat-form').onsubmit = async event => {
  event.preventDefault(); if (generating || loadingConversation) return;
  const query = $('prompt').value.trim(); if (!query) return;
  if (!settings.user_id) { await openSettings(); return; }
  if (!$('model').value) { notice('Choose an installed Ollama model in Connection settings.'); return; }
  generating = true; controller = new AbortController(); $('send').disabled = true; $('stop').hidden = false; $('model').disabled = true;
  let reply, complete = false, replySaved = false; notice();
  try {
    if (!active) active = await post(`/api/v1/users/${settings.user_id}/conversations`, { title: query.slice(0, 70), namespace: settings.namespace || 'DEFAULT' });
    settings.last_chat = active.id; persist(); $('page-title').textContent = active.title; message('user', query); $('prompt').value = ''; reply = message('assistant', 'Retrieving context and loading your model…', false);
    await loadConversations();
    const response = await fetch('/bridge/chat', { method: 'POST', signal: controller.signal, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ user_id: settings.user_id, conversation_id: active.id, model: $('model').value, query, use_memory: $('use-memory').checked, auto_remember: $('auto-remember').checked }) });
    if (!response.ok) { const error = await response.json(); throw Error(error.error || 'Chat request failed'); }
    const reader = response.body.getReader(), decoder = new TextDecoder(); let pending = '', answer = '';
    function handle(line) {
      if (!line.trim()) return; const item = JSON.parse(line);
      if (item.type === 'context') renderContext(item);
      if (item.type === 'token') { answer += item.content; reply.text.textContent = answer; scrollChat(); }
      if (item.type === 'reply_saved') { replySaved = true; addSave(reply.body, answer); $('stop').textContent = 'Stop memory check'; }
      if (item.type === 'memory_status') notice(item.message);
      if (item.type === 'learned') { notice(item.memories.length ? `${item.memories.length} personal ${item.memories.length === 1 ? 'fact saved' : 'facts saved'} for future conversations.` : 'Reply saved. No new personal facts to remember.'); if (item.memories.length) reply.body.append(el('div', 'learned-note', `◈ Remembered: ${item.memories.map(m => m.content).join(' · ')}`)); }
      if (item.type === 'warning') notice(item.message);
      if (item.type === 'trace') addTrace(reply.body, item.trace);
      if (item.type === 'error') throw Error(item.error);
      if (item.type === 'done') complete = true;
    }
    while (true) { const { value, done } = await reader.read(); if (done) break; pending += decoder.decode(value, { stream: true }); let end; while ((end = pending.indexOf('\n')) >= 0) { handle(pending.slice(0, end)); pending = pending.slice(end + 1); } }
    pending += decoder.decode(); handle(pending);
    if (!complete) throw Error('Reply interrupted before it was saved.');
  } catch (e) {
    const stopped = e.name === 'AbortError'; notice(replySaved ? 'Reply saved. Additional memory processing was interrupted.' : stopped ? 'Generation stopped. An incomplete reply is not saved to your conversation.' : e.message);
    if (reply && !replySaved) { if (reply.text.textContent === 'Retrieving context and loading your model…') reply.text.textContent = stopped ? 'Generation stopped.' : 'Could not complete this reply.'; reply.body.append(el('small', 'form-error', 'Incomplete reply · not saved')); }
    if (!replySaved && !$('prompt').value) $('prompt').value = query;
  } finally { generating = false; controller = null; $('send').disabled = false; $('stop').hidden = true; $('stop').textContent = 'Stop'; $('model').disabled = false; await loadConversations().catch(e => notice(e.message)); }
};
$('stop').onclick = () => controller?.abort();
$('prompt').onkeydown = event => { if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) { event.preventDefault(); $('chat-form').requestSubmit(); } };
$('new-chat').onclick = newChat; $('chat-nav').onclick = () => $('sidebar').classList.remove('open');
$('settings').onclick = openSettings; $('refresh').onclick = () => refresh().catch(e => $('settings-error').textContent = e.message);
$('profile-select').onchange = () => $('name-label').hidden = !!$('profile-select').value;
$('settings-form').onsubmit = async event => {
  event.preventDefault(); $('save-settings').disabled = true; $('settings-error').textContent = '';
  try {
    let id = $('profile-select').value;
    if (!id) { const username = $('profile-name').value.trim(); if (!/^[A-Za-z0-9_-]{3,50}$/.test(username)) throw Error('Enter a profile name with 3–50 letters, numbers, underscores or hyphens.'); id = (await post('/api/v1/users', { username, display_name: username })).id; }
    settings = { ...settings, user_id: id, namespace: $('namespace').value, model: $('model').value }; persist(); $('settings-dialog').close(); newChat();
  } catch (e) { $('settings-error').textContent = e.message; } finally { $('save-settings').disabled = false; }
};
$('model').onchange = () => { settings.model = $('model').value; persist(); };
$('auto-remember').checked = settings.auto_remember !== false;
$('auto-remember').onchange = () => { settings.auto_remember = $('auto-remember').checked; persist(); };
$('use-memory').onchange = () => { $('use-memory').parentElement.lastChild.textContent = $('use-memory').checked ? ' Memory enabled' : ' Memory off'; $('auto-remember').disabled = !$('use-memory').checked; };
$('add-memory').onclick = () => openMemory(); $('library-nav').onclick = () => loadLibrary();
$('library-status').onchange = () => loadLibrary();
$('more-memories').onclick = async () => { $('more-memories').disabled = true; libraryPage++; await loadLibrary(true); $('more-memories').disabled = false; };
$('memory-form').onsubmit = async event => {
  event.preventDefault(); $('save-memory').disabled = true; $('memory-error').textContent = '';
  try {
    const memory = await post('/api/v1/memories/remember', { user_id: settings.user_id, content: $('memory-content').value.trim(), memory_type: $('memory-type').value, importance: Number($('importance').value), namespace: namespace(), privacy_level: 'PRIVATE' });
    $('memory-dialog').close(); notice('Memory saved. It can now be found by keyword search.');
    if (connected.embeddings?.model && connected.embeddings.model !== 'null-embedder') {
      try { await post(`/api/v1/memories/${memory.id}/embed?user_id=${settings.user_id}`, {}); notice('Memory saved and embedded for semantic retrieval.'); } catch (e) { notice('Memory saved, but embedding failed: ' + e.message); }
    }
  } catch (e) { $('memory-error').textContent = e.message; } finally { $('save-memory').disabled = false; }
};
document.querySelectorAll('.close-dialog').forEach(button => button.onclick = () => button.closest('dialog').close());
$('toggle-panel').onclick = () => { if (innerWidth <= 1020) $('memory-panel').classList.toggle('open'); else $('memory-panel').hidden = !$('memory-panel').hidden; };
$('menu').onclick = () => $('sidebar').classList.toggle('open');
document.addEventListener('click', event => { const suggestion = event.target.closest('[data-prompt]'); if (suggestion) { $('prompt').value = suggestion.dataset.prompt; $('prompt').focus(); } });
document.addEventListener('keydown', event => { if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k' && !document.querySelector('dialog[open]')) { event.preventDefault(); newChat(); } });
function addTrace(body, trace) {
  const actions = body.querySelector('.message-actions') || el('div', 'message-actions');
  const button = el('button', 'trace-action', `◈ Memory map · ${trace.references.length} retrieved`); button.type = 'button'; button.onclick = () => showMap(trace); actions.append(button); if (!actions.parentElement) body.append(actions);
}
async function showMap(trace) {
  const container = $('map-content'); container.replaceChildren(); $('map-dialog').showModal();
  if (!trace) { container.append(el('p', 'muted', 'Send a message first, or open a saved reply’s Memory map.')); return; }
  const refs = trace.references || (trace.results || []).map((r, i) => ({ memory_id: r.memory.id, reference: i + 1, sources: r.sources }));
  const flow = el('div', 'map-flow'); flow.append(el('div', 'map-node', 'Your question'), el('span', 'map-arrow', '→'), el('div', 'map-node', `${refs.length} retrieved memories`), el('span', 'map-arrow', '→'), el('div', 'map-node', 'Model reply')); container.append(flow);
  for (const ref of refs) {
    const card = el('article', 'map-card'); card.append(el('small', '', `[${ref.reference}] · ${(ref.sources || []).join(' + ')}`)); container.append(card);
    try {
      const memory = await api(`/api/v1/memories/${ref.memory_id}?user_id=${settings.user_id}`);
      if (['DELETED', 'ARCHIVED'].includes(memory.status)) { card.append(el('p', 'muted', 'This memory is no longer available for recall.')); continue; }
      card.append(el('p', '', memory.content), el('small', '', `${memory.memory_type} · ${memory.namespace} · saved ${new Date(memory.created_at).toLocaleDateString()}`));
      if (memory.source_id) {
        const source = el('button', 'outline-button', 'Show original message'); source.onclick = async () => { source.disabled = true; try { const item = await api(`/api/v1/messages/${memory.source_id}?user_id=${settings.user_id}`); card.append(el('div', 'map-source', item.content)); } catch { card.append(el('small', '', 'The original message is no longer available.')); } }; card.append(source);
      }
    } catch { card.append(el('p', 'muted', 'This memory could not be loaded. It may have been removed.')); }
  }
  if (!refs.length) container.append(el('p', 'muted', 'No saved memories were retrieved for this reply.'));
  if (trace.learned_ids?.length) container.append(el('p', 'auto-explanation', `${trace.learned_ids.length} personal facts were saved after this reply. They are available in the memory library for future recall.`));
  container.append(el('small', 'muted', 'Memory cards show their current text. These links are retained when you restart the app.'));
}
$('memory-map').onclick = () => showMap(latestContext);
initModelPicker({ refresh, onError: notice });
refresh().then(async () => {
  await loadConversations();
  if (settings.user_id && settings.last_chat) {
    const conversation = await api(`/api/v1/conversations/${settings.last_chat}?user_id=${settings.user_id}`);
    if (conversation.namespace === (settings.namespace || 'DEFAULT')) await openConversation(conversation);
  }
}).catch(e => notice(e.message));
