'use strict';
// Skynet's panel: the chat with the orchestrator, the cards of the sessions that ask something,
// and (skynet-voice.js) the microphone. It talks to /api/skynet/*, which the server passes to
// Skynet's own service.

const SKY = { open: localStorage.getItem('cw.sky.open') === '1', entries: [], status: null, es: null, el: null, unread: 0, retry: 0 };
const skyOn = () => !!(S.config && S.config.skynet_enabled);
const skyName = () => (S.config && S.config.skynet_name) || 'Skynet';

function skyConnect() {
  if (!skyOn()) { if (SKY.es) { SKY.es.close(); SKY.es = null; } return; }
  if (SKY.es) return;
  const es = new EventSource('/api/skynet/events');
  SKY.es = es;
  es.onmessage = ev => {
    SKY.retry = 0;
    const m = JSON.parse(ev.data);
    if (m.type === 'history') { SKY.entries = m.entries || []; SKY.status = m.status; }
    else if (m.type === 'status') SKY.status = m.status;
    else if (m.type === 'entry') {
      SKY.entries.push(m.entry);
      if (SKY.entries.length > 300) SKY.entries.shift();
      skyAttention(m.entry);
    } else if (m.type === 'update') {
      const i = SKY.entries.findIndex(e => e.id === m.entry.id);
      if (i >= 0) SKY.entries[i] = m.entry;
    }
    skyRender();
  };
  es.onerror = () => {
    // The server or the service restarted: reconnect with a growing pause (the browser's own
    // retry gives up on a non-200 answer, e.g. while Skynet starts).
    es.close(); SKY.es = null;
    if (!skyOn()) return;
    SKY.retry = Math.min(SKY.retry + 1, 10);
    setTimeout(skyConnect, 500 * SKY.retry);
  };
}

// A card or an answer while the panel is closed: a toast and a counter on its button.
function skyAttention(e) {
  if (SKY.open && document.hasFocus()) return;
  if (e.kind === 'notice') {
    SKY.unread++;
    toast('✦ ' + e.name + ' pregunta: ' + e.text.split('\n')[0].slice(0, 90));
  } else if (e.kind === 'assistant') {
    SKY.unread++;
    if (!SKY.open) toast('✦ ' + (e.text.length > 160 ? e.text.slice(0, 160) + '…' : e.text));
  }
  if (!SKY.open) render();
}

async function skySend(text, source = 'texto') {
  text = (text || '').trim();
  if (!text) return;
  const r = await fetch('/api/skynet/send', { method: 'POST', credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CW': '1' }, body: JSON.stringify({ text, source }) });
  if (!r.ok) toast((await r.text()).trim() || r.statusText, true);
}

function skyToggle(force) {
  if (!skyOn()) { dlgSettings('skynet'); return; }
  SKY.open = force ?? !SKY.open;
  localStorage.setItem('cw.sky.open', SKY.open ? '1' : '0');
  if (SKY.open) SKY.unread = 0;
  render();
  for (const t of terms.values()) t.refit();
  if (SKY.open) setTimeout(() => $('.sky textarea')?.focus(), 30);
}

// renderSky adds or removes the panel at the right of the app.
function renderSky(app) {
  const show = SKY.open && skyOn();
  app.classList.toggle('with-sky', show);
  let el = $('.sky', app);
  if (!show) { el?.remove(); SKY.el = null; return; }
  if (!el) {
    const input = h('textarea', { class: 'skyin', rows: 1, placeholder: 'Escribe o di «' + skyName() + '…»',
      oninput: ev => { ev.target.style.height = 'auto'; ev.target.style.height = Math.min(ev.target.scrollHeight, 140) + 'px'; },
      onkeydown: ev => {
        if (ev.key === 'Enter' && !ev.shiftKey && !ev.isComposing) {
          ev.preventDefault(); const v = ev.target.value; ev.target.value = ''; ev.target.style.height = 'auto'; skySend(v);
        } else if (ev.key === 'Escape') { ev.preventDefault(); skyToggle(false); }
      } });
    el = h('aside', { class: 'sky' },
      h('header', {}, h('img', { class: 'logo', src: 'icon.svg', alt: '' }), h('b', {}, skyName()), h('span', { class: 'skystate' }),
        h('span', { class: 'ib', title: 'Conversación nueva', onclick: () => dlgConfirm('¿Empezar una conversación nueva?', skyName() + ' olvida lo que hablaron; las sesiones no cambian.', 'Empezar de nuevo',
          () => fetch('/api/skynet/reset', { method: 'POST', credentials: 'same-origin', headers: { 'X-CW': '1' } }), false) }, '⟲'),
        h('span', { class: 'ib', title: 'Ajustes de ' + skyName(), onclick: () => dlgSettings('skynet') }, '⚙'),
        h('span', { class: 'ib', title: 'Cerrar (Alt+K)', onclick: () => skyToggle(false) }, '✕')),
      h('div', { class: 'skylist' }),
      h('div', { class: 'skyvoice' }),
      h('div', { class: 'skyinput' }, input, h('button', { class: 'mic', title: 'Hablar (Alt+J)', onclick: () => window.skyVoice?.push() }, '🎙')));
    app.append(el);
    SKY.el = el;
  }
  skyRender();
}

// md renders the little Markdown Claude uses (paragraphs, lists, **bold**, `code`) as elements.
function md(text) {
  const inline = s => s.split(/(\*\*[^*]+\*\*|`[^`]+`)/).filter(Boolean).map(p =>
    p.startsWith('**') && p.endsWith('**') ? h('b', {}, p.slice(2, -2)) : p.startsWith('`') && p.endsWith('`') ? h('code', {}, p.slice(1, -1)) : p);
  const out = [];
  let list = null;
  for (const line of text.split('\n')) {
    const m = line.match(/^\s*(?:[-*•]|\d+[.)])\s+(.*)$/);
    if (m) { if (!list) { list = h('ul'); out.push(list); } list.append(h('li', {}, ...inline(m[1]))); continue; }
    list = null;
    if (line.trim()) out.push(h('p', {}, ...inline(line.replace(/^#+\s*/, ''))));
  }
  return out;
}

const skyKindIcon = { voz: '🎙 ', boton: '☝ ' };
function skyEntryEl(e) {
  switch (e.kind) {
    case 'user':
      return h('div', { class: 'm u' }, e.source && skyKindIcon[e.source] ? h('small', {}, skyKindIcon[e.source] + (e.source === 'voz' ? 'voz' : 'opción')) : null, e.text);
    case 'assistant':
      return h('div', { class: 'm s' }, ...md(e.text));
    case 'tool':
      return h('div', { class: 'tool', title: e.text }, '⚙ ', h('b', {}, e.tool), ' ', e.text.length > 70 ? e.text.slice(0, 70) + '…' : e.text);
    case 'notice': {
      const goTo = () => { if (e.ws === S.ws) { const f = findSlot(e.session); f ? focusSlot(e.session) : toast('Esa sesión ya no está'); } else go(e.ws); };
      const opts = (e.options || []).filter(o => !/^(chat about this|type something)/i.test(o.texto));
      return h('div', { class: 'ask' + (e.resolved ? ' done' : '') },
        h('div', { class: 't' }, h('span', { class: 'd ' + (e.resolved ? 'idle' : 'waiting') }), (e.wsTitle || e.ws) + ' › ' + e.tab + ' › ' + e.name + (e.perm ? ' pide permiso' : ' pregunta'),
          e.resolved ? h('span', { class: 'ok' }, ' · respondida') : null),
        h('div', { class: 'q' }, e.text),
        e.resolved ? null : h('div', { class: 'opts' },
          ...opts.map(o => h('button', { class: 'btn', onclick: () => skySend(`Responde a la sesión "${e.name}" (id ${e.session.slice(0, 8)}) con la opción ${o.n} (${o.texto})` +
            (e.perm ? '. Lo autorizo yo.' : '.'), 'boton') }, o.n + ' · ' + o.texto)),
          h('button', { class: 'btn ghost', onclick: goTo }, 'Ir a la sesión')));
    }
    case 'error':
      return h('div', { class: 'info err' }, e.text);
    default:
      return h('div', { class: 'info' }, e.text);
  }
}

function skyRender() {
  // The counter on the sidebar's button.
  const b = $('.side .skyb');
  if (b) { b.classList.toggle('unread', SKY.unread > 0); b.dataset.n = SKY.unread || ''; }
  const el = SKY.el;
  if (!el) return;
  const st = SKY.status || {};
  const label = { iniciando: 'iniciando…', listo: 'listo', pensando: 'pensando…', detenido: 'en espera', error: 'error' }[st.state] || '…';
  const v = window.skyVoice?.state();
  $('.skystate', el).replaceChildren(...[h('span', { class: 'd ' + (st.state === 'pensando' || st.state === 'iniciando' ? 'busy' : st.state === 'error' ? 'waiting' : 'idle') }),
    v && v.on ? (v.label + ' · ') : '', label, st.alias ? h('span', { class: 'al' }, '· ' + st.alias) : ''].filter(x => x !== ''));
  if (st.error) $('.skystate', el).title = st.error;
  const list = $('.skylist', el);
  const atBottom = list.scrollHeight - list.scrollTop - list.clientHeight < 40;
  list.replaceChildren(...(SKY.entries.length ? SKY.entries.map(skyEntryEl) :
    [h('div', { class: 'skyempty' }, h('b', {}, 'Hola, soy ' + skyName() + '.'),
      h('div', {}, 'Veo todas tus sesiones y puedo actuar en ellas: «¿cómo van las pestañas?», «en kyc dile que corra los tests», «abre una pestaña para infra».'))]));
  if (atBottom) list.scrollTop = list.scrollHeight;
  window.skyVoice?.renderBar($('.skyvoice', el));
}
