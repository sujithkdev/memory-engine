const llama = '<svg viewBox="0 0 24 28" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M6 11 4 3q0-2 2-1l4 7m4 0 3-7q2-1 2 1l-1 9M6 11q-3 2-2 7l-1 7m15-13q3 2 2 7l1 6M5 20l2 6m10-6-1 6M8 11q4-3 8 1l1 7q-5 5-10-1z"/><path d="M10 14h.1m4 0h.1m-4 4q2 2 4 0"/></svg>';
export function initModelPicker({ refresh, onError }) {
  const $ = id => document.getElementById(id);
  const select = $('model'), trigger = $('model-trigger'), popover = $('model-popover'), search = $('model-search'), list = $('model-options');
  trigger.querySelector('.llama-icon').innerHTML = llama;
  function close(focus = false) { popover.hidden = true; trigger.setAttribute('aria-expanded', 'false'); if (focus) trigger.focus(); }
  function render() {
    const selection = Array.from(select.options).find(o => o.value === select.value);
    $('model-label').textContent = selection?.textContent || 'Choose a model'; trigger.title = selection?.textContent || '';
    trigger.disabled = select.disabled; if (select.disabled) close();
    list.replaceChildren();
    const options = Array.from(select.options).filter(o => o.value && o.textContent.toLowerCase().includes(search.value.toLowerCase()));
    for (const option of options) {
      const button = document.createElement('button'); button.type = 'button'; button.className = 'model-option'; button.setAttribute('role', 'option'); button.setAttribute('aria-selected', String(option.value === select.value)); button.title = option.textContent;
      const icon = document.createElement('span'); icon.className = 'llama-icon'; icon.innerHTML = llama;
      const label = document.createElement('span'); label.className = 'model-name'; label.textContent = option.textContent;
      const tick = document.createElement('span'); tick.className = 'selected-tick'; tick.textContent = option.value === select.value ? '✓' : '';
      button.append(icon, label, tick); button.onclick = () => { select.value = option.value; select.dispatchEvent(new Event('change')); render(); close(true); }; list.append(button);
    }
    if (!options.length) { const empty = document.createElement('div'); empty.className = 'no-models'; empty.textContent = search.value ? 'No matching models.' : 'No models available. Start Ollama, then refresh.'; list.append(empty); }
  }
  trigger.onclick = () => { if (!popover.hidden) return close(); popover.hidden = false; trigger.setAttribute('aria-expanded', 'true'); search.value = ''; render(); search.focus(); };
  search.oninput = render;
  popover.onkeydown = event => {
    if (event.key === 'Escape') { event.preventDefault(); close(true); }
    if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) {
      const options = Array.from(list.querySelectorAll('button')); if (!options.length) return;
      event.preventDefault(); const current = options.indexOf(document.activeElement);
      const index = event.key === 'Home' ? 0 : event.key === 'End' ? options.length - 1 : event.key === 'ArrowDown' ? (current + 1) % options.length : (current <= 0 ? options.length - 1 : current - 1); options[index].focus();
    }
    if (event.key === 'Enter' && event.target === search) { event.preventDefault(); list.querySelector('button')?.click(); }
  };
  document.addEventListener('click', event => { if (!trigger.parentElement.contains(event.target)) close(); });
  trigger.parentElement.addEventListener('focusout', event => { if (event.relatedTarget && !trigger.parentElement.contains(event.relatedTarget)) close(); });
  $('refresh-models').onclick = async () => { $('refresh-models').disabled = true; try { await refresh(); render(); } catch (error) { onError(error.message); } finally { $('refresh-models').disabled = false; } };
  new MutationObserver(render).observe(select, { childList: true, attributes: true }); select.addEventListener('change', render); render();
}
