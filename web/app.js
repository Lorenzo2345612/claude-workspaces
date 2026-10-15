'use strict';
// cw — the app's window. Plain DOM, no build step: the server embeds this directory.

// ---------- helpers ----------
const $ = (sel, el = document) => el.querySelector(sel);
function h(tag, attrs, ...kids) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v == null || v === false) continue;
    if (k === 'class') el.className = v;
    else if (k === 'style' && typeof v === 'object') Object.assign(el.style, v);
    else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
    else if (k === 'html') el.innerHTML = v;
    else el.setAttribute(k, v === true ? '' : v);
  }
  for (const c of kids.flat(Infinity)) {
    if (c == null || c === false) continue;
    el.append(c instanceof Node ? c : document.createTextNode(String(c)));
  }
  return el;
}
const api = {
  async get(path, params) {
    const u = new URL('/api/get/' + path, location.origin);
    for (const [k, v] of Object.entries(params || {})) if (v != null) u.searchParams.set(k, v);
    const r = await fetch(u, { credentials: 'same-origin' });
    const j = await r.json();
    if (!r.ok) throw new Error(j.error || r.statusText);
    return j;
  },
  async do(action, body) {
    const r = await fetch('/api/do', { method: 'POST', credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json', 'X-CW': '1' }, body: JSON.stringify({ action, ...body }) });
    const j = await r.json().catch(() => ({}));
    if (!r.ok) throw new Error(j.error || r.statusText);
    return j;
  },
};
let toastTimer;
function toast(msg, err) {
  document.querySelectorAll('.toast').forEach(t => t.remove());
  const t = h('div', { class: 'toast' + (err ? ' err' : '') }, msg);
  document.body.append(t);
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => t.remove(), err ? 5200 : 2400);
}
async function act(action, body) {
  try { return await api.do(action, { ws: S.ws, ...body }); }
  catch (e) { toast(e.message, true); throw e; }
}
const statusText = { busy: 'trabajando', waiting: 'pide permiso', idle: 'lista', sleeping: 'dormida · Enter para retomarla' };
const plural = (n, one, many) => n + ' ' + (n === 1 ? one : many);
const THEMES = { azul: ['#7c8cff', '#a78bfa'], morado: ['#a78bfa', '#c084fc'], verde: ['#4ade80', '#2dd4bf'],
  naranja: ['#fb923c', '#fbbf24'], rosa: ['#f472b6', '#c084fc'], gris: ['#9ca3af', '#cbd5e1'] };
function applyTheme(name) {
  const [a, b] = THEMES[name] || THEMES.azul;
  const r = document.documentElement.style;
  r.setProperty('--accent', a); r.setProperty('--accent2', b);
  const rgb = [1, 3, 5].map(i => parseInt(a.slice(i, i + 2), 16)).join(',');
  r.setProperty('--accentbg', `rgba(${rgb},.14)`); r.setProperty('--accentring', `rgba(${rgb},.25)`);
  for (const t of terms.values()) t.term.options.theme = termTheme();
}

// ---------- state ----------
const S = {
  info: null, config: null, aliases: [], defaultAlias: 'claude',
  workspaces: [], ws: null, state: null, es: null,
  launchDir: new URLSearchParams(location.search).get('dir') || '',
};
const terms = new Map(); // slot id → Term

function slotOf(id) { return S.state && S.state.slots[id]; }
function curTab() { return S.state && S.state.tabs ? S.state.tabs.find(t => t.id === S.state.active) || S.state.tabs[0] : null; }
function curView(tab = curTab()) { return tab ? tab.views.find(v => v.id === tab.active) || tab.views[0] : null; }
function findSlot(id) {
  for (const t of S.state?.tabs || []) for (const v of t.views) if (v.slots.includes(id)) return { tab: t, view: v };
  return {};
}
function viewNames(v) { return v.slots.map(id => slotOf(id)?.name || '…').join(' · '); }

// ---------- terminals ----------
function termTheme() {
  const cs = getComputedStyle(document.documentElement);
  const accent = cs.getPropertyValue('--accent').trim() || '#7c8cff';
  return {
    background: '#0b0c10', foreground: '#d4d7e0', cursor: accent, cursorAccent: '#0b0c10',
    selectionBackground: 'rgba(124,140,255,.32)',
    black: '#1c1f26', red: '#f87171', green: '#4ade80', yellow: '#fbbf24', blue: '#7c8cff', magenta: '#c084fc', cyan: '#67e8f9', white: '#d4d7e0',
    brightBlack: '#5d6373', brightRed: '#fca5a5', brightGreen: '#86efac', brightYellow: '#fde68a', brightBlue: '#a5b4fc', brightMagenta: '#d8b4fe', brightCyan: '#a5f3fc', brightWhite: '#ffffff',
  };
}
const enc = new TextEncoder();
class Term {
  constructor(id) {
    this.id = id;
    this.ws = S.ws;
    this.el = h('div', { class: 'termhost' });
    this.term = new Terminal({
      fontFamily: getComputedStyle(document.documentElement).getPropertyValue('--mono'),
      fontSize: (S.config && S.config.app_font_size) || 13.5, lineHeight: 1.12, cursorBlink: true,
      scrollback: 0, allowProposedApi: true, theme: termTheme(), macOptionIsMeta: true,
      rightClickSelectsWord: false, drawBoldTextInBrightColors: false,
    });
    this.fit = new FitAddon.FitAddon();
    this.term.loadAddon(this.fit);
    try { const u = new Unicode11Addon.Unicode11Addon(); this.term.loadAddon(u); this.term.unicode.activeVersion = '11'; } catch (e) {}
    try { this.term.loadAddon(new WebLinksAddon.WebLinksAddon((ev, uri) => window.open(uri, '_blank', 'noopener'))); } catch (e) {}
    try { this.term.loadAddon(new ClipboardAddon.ClipboardAddon()); } catch (e) {}
    this.term.open(this.el);
    this.term.attachCustomKeyEventHandler(ev => this.key(ev));
    this.term.onData(d => this.send(enc.encode(d)));
    this.term.onBinary(d => this.send(Uint8Array.from(d, c => c.charCodeAt(0))));
    this.term.onResize(({ cols, rows }) => this.sock && this.sock.readyState === 1 && this.sock.send(JSON.stringify({ type: 'resize', cols, rows })));
    this.el.addEventListener('contextmenu', ev => { ev.preventDefault(); openCtx('slot', this.id, ev.clientX, ev.clientY); });
    this.el.addEventListener('mousedown', () => { if (curView()?.focus !== this.id) focusSlot(this.id, false); }, true);
    this.ro = new ResizeObserver(() => this.refit());
    this.ro.observe(this.el);
    this.retry = 0;
    this.connect();
  }
  key(ev) {
    if (ev.type !== 'keydown') return true;
    if (document.querySelector('.scrim')) return false; // a dialog is open over the terminal
    if (ev.key === 'Escape' && ctx) { if (ev.type === 'keydown') closeCtx(); ev.preventDefault(); return false; }
    if (shortcut(ev)) { ev.preventDefault(); return false; }
    const mod = ev.ctrlKey || ev.metaKey;
    if (mod && !ev.altKey && (ev.key === 'v' || ev.key === 'V')) return false; // the browser pastes
    if (mod && !ev.altKey && (ev.key === 'c' || ev.key === 'C') && (ev.shiftKey || this.term.hasSelection())) {
      if (this.term.hasSelection()) { navigator.clipboard.writeText(this.term.getSelection()).catch(() => {}); this.term.clearSelection(); }
      return false;
    }
    return true;
  }
  send(bytes) { if (this.sock && this.sock.readyState === 1) this.sock.send(bytes); }
  refit() {
    if (!this.el.offsetWidth || !this.el.offsetHeight) return;
    try { this.fit.fit(); } catch (e) {}
  }
  connect() {
    if (this.disposed) return;
    this.refit();
    const q = new URLSearchParams({ ws: this.ws, slot: this.id, cols: this.term.cols, rows: this.term.rows });
    const sock = new WebSocket(`ws://${location.host}/api/pty?${q}`);
    sock.binaryType = 'arraybuffer';
    this.sock = sock;
    sock.onopen = () => { this.retry = 0; this.setConn(false); sock.send(JSON.stringify({ type: 'resize', cols: this.term.cols, rows: this.term.rows })); };
    sock.onmessage = ev => {
      if (typeof ev.data === 'string') { try { const j = JSON.parse(ev.data); if (j.error) this.term.write('\r\n\x1b[31m' + j.error + '\x1b[0m\r\n'); } catch (e) {} return; }
      this.term.write(new Uint8Array(ev.data));
    };
    sock.onclose = () => {
      if (this.disposed || this.sock !== sock) return;
      if (!slotOf(this.id)) return;
      this.setConn(true);
      this.retry = Math.min(this.retry + 1, 6);
      setTimeout(() => { if (!this.disposed && slotOf(this.id)) this.connect(); }, 250 * this.retry);
    };
  }
  setConn(lost) {
    let c = this.el.parentElement && this.el.parentElement.querySelector('.conn');
    if (lost && !c && this.el.parentElement) this.el.parentElement.append(h('div', { class: 'conn' }, 'reconectando…'));
    if (!lost && c) c.remove();
  }
  focus() { this.term.focus(); }
  dispose() {
    this.disposed = true;
    try { this.sock && this.sock.close(); } catch (e) {}
    this.ro.disconnect();
    this.term.dispose();
    this.el.remove();
  }
}
function getTerm(id) {
  let t = terms.get(id);
  if (!t) { t = new Term(id); terms.set(id, t); }
  return t;
}
function pruneTerms() {
  for (const [id, t] of terms) if (!slotOf(id) || t.ws !== S.ws) { t.dispose(); terms.delete(id); }
}

// ---------- keyboard ----------
function shortcut(ev) {
  if (!S.ws || !S.state || $('.scrim')) return false;
  if (!ev.altKey || ev.ctrlKey || ev.metaKey) return false;
  const tab = curTab(), view = curView();
  const k = ev.key.length === 1 ? ev.key.toLowerCase() : ev.key;
  const views = tab ? tab.views : [];
  const vi = view ? views.indexOf(view) : -1;
  switch (k) {
    case 'ArrowLeft': if (views.length) selectView(views[(vi - 1 + views.length) % views.length].id); return true;
    case 'ArrowRight': if (views.length) selectView(views[(vi + 1) % views.length].id); return true;
    case 'ArrowUp': case 'ArrowDown': {
      const tabs = S.state.tabs, i = tabs.indexOf(tab), d = k === 'ArrowUp' ? -1 : 1;
      if (tabs.length) selectTab(tabs[(i + d + tabs.length) % tabs.length].id);
      return true;
    }
    case 't': dlgNewTab(); return true;
    case 'n': if (tab) dlgNewSession(tab.id, 'claude'); return true;
    case 'y': if (tab) dlgNewSession(tab.id, 'shell'); return true;
    case 'i': if (tab) dlgNewSession(tab.id, 'resume'); return true;
    case 'r': if (view?.focus) dlgEditSession(view.focus); return true;
    case 'p': if (view?.focus && slotOf(view.focus)?.kind === 'claude') dlgBulkAlias('slot', view.focus); return true;
    case 'w': if (view?.focus) confirmCloseSlot(view.focus); return true;
    case 'm': { const r = $('.ws').getBoundingClientRect(); wsMenu(r.left + 12, r.bottom + 4); return true; }
    case 'h': dlgSettings('keys'); return true;
    case ',': dlgSettings('general'); return true;
  }
  if (/^[1-9]$/.test(k)) { const v = views[+k - 1]; if (v) selectView(v.id); return true; }
  return false;
}
document.addEventListener('keydown', ev => {
  if (ev.key === 'Escape') {
    if (closeCtx()) { ev.preventDefault(); return; }
    const top = [...document.querySelectorAll('.scrim')].pop();
    if (top) { top._close?.(); ev.preventDefault(); return; }
  }
  if (ev.target.closest && ev.target.closest('.xterm')) return; // handled by the terminal
  if (shortcut(ev)) ev.preventDefault();
});

// ---------- navigation ----------
async function selectTab(id) {
  if (!S.state || S.state.active === id) return;
  S.state.active = id; render();
  await act('tab.select', { tab: id }).catch(() => {});
}
async function selectView(id) {
  const tab = S.state.tabs.find(t => t.views.some(v => v.id === id));
  if (!tab) return;
  S.state.active = tab.id; tab.active = id; render();
  const v = tab.views.find(v => v.id === id);
  if (v?.focus) setTimeout(() => terms.get(v.focus)?.focus(), 30);
  await act('view.select', { view: id }).catch(() => {});
}
async function focusSlot(id, focusTerm = true) {
  const { tab, view } = findSlot(id);
  if (!view) return;
  S.state.active = tab.id; tab.active = view.id; view.focus = id; render();
  if (focusTerm) setTimeout(() => terms.get(id)?.focus(), 30);
  await act('slot.focus', { slot: id }).catch(() => {});
}
function stageSize() {
  const st = $('.stage');
  const t = [...terms.values()].find(t => t.el.offsetWidth);
  if (t) return { cols: t.term.cols, rows: t.term.rows };
  if (!st) return {};
  return { cols: Math.max(40, Math.floor((st.clientWidth - 40) / 8.2)), rows: Math.max(10, Math.floor((st.clientHeight - 60) / 17.5)) };
}

// ---------- rendering ----------
function render() {
  const root = $('#root');
  if (!S.ws) { pruneTerms(); renderHome(root); return; }
  if (!S.state) return;
  if (S.state.mode !== 'app') { renderNotOpen(root); return; }
  pruneTerms();
  let app = $('.app', root);
  if (!app) {
    root.replaceChildren();
    app = h('div', { class: 'app' }, h('aside', { class: 'side' }), h('div', { class: 'main' }, h('div', { class: 'sessions' }), h('div', { class: 'stage' })));
    root.append(app);
  }
  renderSide($('.side', app));
  renderSessions($('.sessions', app));
  renderStage($('.stage', app));
  renderCtx();
  document.title = S.state.title + ' — cw';
}

function renderSide(side) {
  const st = S.state, nTabs = st.tabs.length, nSess = Object.keys(st.slots).length;
  side.replaceChildren(
    h('div', { class: 'ws', title: 'Workspace (Alt+M)', onclick: ev => { const r = ev.currentTarget.getBoundingClientRect(); wsMenu(r.left + 12, r.bottom + 4); } },
      h('div', { class: 'logo' }, '≡'),
      h('div', { class: 'wsn' }, h('b', {}, st.title), h('span', { class: 'sub' }, plural(nTabs, 'pestaña', 'pestañas') + ' · ' + plural(nSess, 'sesión', 'sesiones'))),
      h('span', { class: 'chev' }, '⌄')),
    h('div', { class: 'sec' }, 'Pestañas', h('span', { class: 'add', title: 'Nueva pestaña (Alt+T)', onclick: () => dlgNewTab() }, '+')),
    h('div', { class: 'tabs' }, st.tabs.map((t, i) => h('div', {
      class: 'tab' + (t.id === st.active ? ' on' : ''), draggable: 'true', title: t.dir,
      onclick: () => selectTab(t.id),
      oncontextmenu: ev => { ev.preventDefault(); openCtx('tab', t.id, ev.clientX, ev.clientY); },
      ondragstart: ev => { ev.dataTransfer.setData('cw/tab', t.id); ev.dataTransfer.effectAllowed = 'move'; },
      ondragover: ev => { if (ev.dataTransfer.types.includes('cw/tab')) { ev.preventDefault(); ev.currentTarget.classList.add('drop'); } },
      ondragleave: ev => ev.currentTarget.classList.remove('drop'),
      ondrop: ev => { ev.preventDefault(); ev.currentTarget.classList.remove('drop'); const id = ev.dataTransfer.getData('cw/tab'); if (id && id !== t.id) act('tab.move', { tab: id, to: i }); },
    },
      h('div', { class: 'ic' }, '▤'),
      h('div', { class: 'tx' }, h('div', { class: 'nm' }, t.title), h('div', { class: 'dir' }, t.short)),
      h('div', { class: 'badges' }, t.busy ? h('span', { class: 'b busy', title: 'trabajando' }, '● ' + t.busy) : null,
        t.waiting ? h('span', { class: 'b wait', title: 'piden permiso' }, '? ' + t.waiting) : null,
        t.idle ? h('span', { class: 'b idle', title: 'listas' }, '○ ' + t.idle) : null,
        t.asleep ? h('span', { class: 'b asleep', title: 'dormidas' }, 'z ' + t.asleep) : null)))),
    h('div', { class: 'foot' },
      h('button', { class: 'ghost', onclick: () => dlgSettings('general') }, '⚙ Configuración'),
      h('button', { class: 'power', title: 'Apagar y guardar', onclick: () => confirmStop() }, '⏻')),
    h('div', { class: 'ver' }, 'cw ' + st.version),
  );
}

function dotFor(s) {
  if (!s) return h('span', { class: 'dot' });
  if (s.kind !== 'claude') return h('span', { class: 'dot shell' }, '❯');
  if (s.dead) return h('span', { class: 'dot dead', title: 'terminó con error' });
  return h('span', { class: 'dot ' + (s.status || 'idle'), title: statusText[s.status] || '' });
}
function aliasTag(s) { return s && s.kind === 'claude' && s.alias && s.alias !== 'claude' ? h('span', { class: 'tag' }, s.alias) : null; }

function renderSessions(bar) {
  const tab = curTab();
  if (!tab) { bar.replaceChildren(); return; }
  const view = curView(tab);
  bar.replaceChildren(
    ...tab.views.map((v, i) => {
      const on = view && v.id === view.id;
      const multi = v.slots.length > 1;
      const pill = h('div', {
        class: 's' + (on ? ' on' : ''), draggable: 'true',
        title: multi ? 'Vista combinada · clic derecho para sus acciones' : (slotOf(v.slots[0])?.short || ''),
        onclick: () => selectView(v.id),
        oncontextmenu: ev => { ev.preventDefault(); const it = ev.target.closest('[data-slot]'); openCtx('slot', it ? it.dataset.slot : (v.focus || v.slots[0]), ev.clientX, ev.clientY); },
        ondragstart: ev => { ev.dataTransfer.setData('cw/view', v.id); ev.dataTransfer.effectAllowed = 'move'; },
        ondragover: ev => { if (ev.dataTransfer.types.includes('cw/view')) { ev.preventDefault(); ev.currentTarget.classList.add('drop'); } },
        ondragleave: ev => ev.currentTarget.classList.remove('drop'),
        ondrop: ev => { ev.preventDefault(); ev.currentTarget.classList.remove('drop'); const id = ev.dataTransfer.getData('cw/view'); if (id && id !== v.id) act('view.move', { view: id, to: i }); },
      });
      if (multi) pill.append(h('span', { style: { color: 'var(--dim)' } }, '⧉'));
      v.slots.forEach((id, j) => {
        const s = slotOf(id);
        if (j > 0) pill.append(h('span', { class: 'sepbar' }, '│'));
        pill.append(h('span', { class: 'it' + (multi && on && v.focus === id ? ' f' : ''), 'data-slot': id,
          onclick: multi ? ev => { ev.stopPropagation(); focusSlot(id); } : null },
          dotFor(s), s ? s.name : '…', aliasTag(s),
          h('span', { class: 'x', title: 'Cerrar', onclick: ev => { ev.stopPropagation(); confirmCloseSlot(id); } }, '✕')));
      });
      return pill;
    }),
    h('div', { class: 'newbtn' },
      h('button', { class: 'pill', title: 'Nueva sesión de Claude (Alt+N)', onclick: () => dlgNewSession(tab.id, 'claude') }, '＋ Claude'),
      h('button', { class: 'pill', title: 'Nueva terminal (Alt+Y)', onclick: () => dlgNewSession(tab.id, 'shell') }, '＋ Terminal')),
    h('span', { class: 'right' }, tab.short),
  );
}

let stageKey = '';
function renderStage(stage) {
  const tab = curTab(), view = curView(tab);
  if (!view) {
    const key = 'empty:' + (tab ? tab.id : '');
    if (stageKey !== key) { stageKey = key; stage.className = 'stage'; stage.replaceChildren(emptyTab(tab)); }
    return;
  }
  const key = view.id + ':' + view.layout + ':' + view.slots.join(',');
  if (stageKey !== key) {
    stageKey = key;
    stage.className = 'stage ' + (view.slots.length > 1 ? view.layout || 'cols' : 'cols');
    stage.style.setProperty('--gc', Math.ceil(Math.sqrt(view.slots.length)));
    const hadFocus = document.activeElement && document.activeElement.closest && document.activeElement.closest('.termhost');
    stage.replaceChildren(...view.slots.map(id => {
      const frame = h('div', { class: 'term', 'data-slot': id }, h('div', { class: 'thead' }), h('div', { class: 'tbody' }));
      $('.tbody', frame).append(getTerm(id).el);
      return frame;
    }));
    requestAnimationFrame(() => {
      for (const id of view.slots) terms.get(id)?.refit();
      const f = terms.get(view.focus || view.slots[0]);
      if (f && (hadFocus || !$('.scrim'))) f.focus();
    });
  }
  for (const frame of stage.querySelectorAll('.term')) {
    const id = frame.dataset.slot, s = slotOf(id), multi = view.slots.length > 1;
    frame.classList.toggle('on', multi && view.focus === id);
    const head = $('.thead', frame);
    const meta = [];
    if (s && s.kind === 'claude') { if (s.alias) meta.push(s.alias); meta.push(s.dead ? 'terminó con error' : statusText[s.status] || 'lista'); }
    else if (s && s.name !== 'terminal') meta.push('terminal');
    if (s) meta.push(s.short);
    head.replaceChildren(dotFor(s), h('b', {}, s ? s.name : '…'), h('span', { class: 'meta' }, '· ' + meta.join(' · ')),
      h('span', { class: 'act' },
        multi ? h('button', { title: 'Separar de la vista', onclick: () => act('slot.split', { slot: id }) }, '⇱') : null,
        h('button', { title: 'Acciones', onclick: ev => { const r = ev.currentTarget.getBoundingClientRect(); openCtx('slot', id, r.right - 300, r.bottom + 4); } }, '⋯'),
        h('button', { class: 'danger', title: 'Cerrar sesión', onclick: () => confirmCloseSlot(id) }, '✕')));
    head.oncontextmenu = ev => { ev.preventDefault(); openCtx('slot', id, ev.clientX, ev.clientY); };
    head.onclick = ev => { if (!ev.target.closest('button') && multi) focusSlot(id); };
    let ov = $('.overlay', frame);
    if (s && s.dead) {
      if (!ov) {
        $('.tbody', frame).append(h('div', { class: 'overlay' }, h('div', { class: 'box' },
          h('b', {}, s.name + ' terminó con error'),
          h('p', {}, 'El proceso salió con el código ' + (s.deadStatus || '?') + '. Puedes reabrirla: una sesión de Claude retoma su conversación.'),
          h('div', { class: 'row' }, h('button', { class: 'btn', onclick: () => act('slot.close', { slot: id }) }, 'Cerrar'),
            h('button', { class: 'btn primary', onclick: () => act('slot.respawn', { slot: id }) }, '↻ Reabrir')))));
      }
    } else if (ov) ov.remove();
  }
}

function emptyTab(tab) {
  if (!tab) return h('div');
  const list = h('div', { class: 'list' }, h('div', { class: 'h' }, 'Conversaciones recientes en ' + tab.short));
  const card = h('div', { class: 'card' },
    h('h3', {}, tab.title),
    h('p', {}, 'Esta pestaña todavía no tiene sesiones. Trabaja en ' + tab.short + '.'),
    h('div', { class: 'acts' },
      h('button', { class: 'btn primary', onclick: () => dlgNewSession(tab.id, 'claude') }, '✦ Nueva sesión de Claude'),
      h('button', { class: 'btn', onclick: () => dlgNewSession(tab.id, 'shell') }, '❯ Terminal'),
      h('button', { class: 'btn', onclick: () => dlgNewSession(tab.id, 'resume') }, '↻ Retomar por ID')),
    list);
  api.get('recent', { ws: S.ws, tab: tab.id }).then(rows => {
    if (!rows.length) { list.append(h('div', { class: 'hint' }, 'No hay conversaciones anteriores en esta carpeta.')); return; }
    for (const c of rows) list.append(h('div', { class: 'row-item', onclick: () => dlgNewSession(tab.id, 'resume', c) },
      h('b', {}, c.title || shorten(c.first, 60)), h('span', { class: 'w' }, c.when + (c.alias && c.alias !== 'claude' ? ' · ' + c.alias : '')),
      h('span', { class: 'q' }, c.title ? '«' + c.first + '»' : c.id.slice(0, 8))));
  }).catch(() => {});
  return h('div', { class: 'empty' }, card);
}
const shorten = (s, n) => (s && s.length > n ? s.slice(0, n - 1) + '…' : s || '');

function renderNotOpen(root) {
  stageKey = '';
  const st = S.state;
  const inTerm = st.mode === 'terminal';
  root.replaceChildren(h('div', { class: 'start' }, h('div', { class: 'empty' }, h('div', { class: 'card' },
    h('h3', {}, st.title),
    h('p', {}, inTerm ? 'Este workspace está abierto en la versión de terminal. Para abrirlo aquí se guarda y se reinicia: sus sesiones retoman sus conversaciones y las que estaban trabajando reciben el mensaje de continuación.'
      : 'Este workspace está apagado.'),
    h('div', { class: 'acts' },
      h('button', { class: 'btn', onclick: () => go(null) }, '← Workspaces'),
      h('button', { class: 'btn primary', onclick: () => openWorkspace(st.ws, inTerm) }, inTerm ? 'Abrir aquí' : 'Abrir'))))));
}

// ---------- home ----------
function renderHome(root) {
  document.title = 'cw';
  stageKey = '';
  const launch = S.launchDir || S.info?.launchDir || '';
  const shortLaunch = launch && S.info && launch.startsWith(S.info.home) ? '~' + launch.slice(S.info.home.length) : launch;
  root.replaceChildren(h('div', { class: 'start' },
    h('div', { class: 'top' }, h('div', { class: 'logo' }, '≡'), h('h2', {}, 'Claude Workspaces'), h('span', { class: 'sp' }),
      h('button', { class: 'ghost', style: { flex: 'none', padding: '8px 14px' }, onclick: () => dlgSettings('general') }, '⚙ Configuración')),
    h('h3', {}, 'Workspaces'),
    h('p', { class: 'lead' }, 'Retoma uno, crea uno nuevo en una carpeta o empieza desde una conversación de Claude.'),
    h('div', { class: 'cards' },
      S.workspaces.map(w => {
        const state = w.state === 'app' ? h('span', { class: 'live' }, '● abierto')
          : w.state === 'terminal' ? h('span', { class: 'chip term' }, 'en la terminal') : h('span', { class: 'chip' }, 'apagado');
        const meta = [plural(w.tabs, 'pestaña', 'pestañas'), plural(w.sessions, 'sesión', 'sesiones')];
        if (w.state !== 'app' && w.when) meta.push(w.when);
        const names = w.tabNames || [];
        return h('div', { class: 'wcard', onclick: () => openWorkspace(w.name, false, w.state),
          oncontextmenu: ev => { ev.preventDefault(); cardMenu(w, ev.clientX, ev.clientY); } },
          h('div', { class: 'row' }, h('b', {}, w.title), state),
          h('div', { class: 'meta' }, meta.join(' · '),
            w.working ? h('span', { style: { color: 'var(--amber)' } }, ' · ● ' + w.working + (w.state === 'stopped' ? ' trabajando al apagar' : ' trabajando')) : null),
          h('div', { class: 'chips' }, names.slice(0, 4).map(n => h('span', { class: 'chip' }, n)), names.length > 4 ? h('span', { class: 'chip' }, '+' + (names.length - 4)) : null),
          h('button', { class: 'more', title: 'Acciones', onclick: ev => { ev.stopPropagation(); cardMenu(w, ev.clientX, ev.clientY); } }, '⋯'));
      }),
      h('div', { class: 'wcard new', onclick: () => dlgNewWorkspace(launch) },
        h('div', {}, h('div', { style: { fontSize: '22px' } }, '＋'), 'Nuevo workspace aquí', h('div', { class: 'meta' }, shortLaunch || 'elige una carpeta'))),
      h('div', { class: 'wcard new', onclick: () => dlgFromID() },
        h('div', {}, h('div', { style: { fontSize: '22px' } }, '↻'), 'Desde una conversación', h('div', { class: 'meta' }, 'pega el ID de una sesión de Claude'))),
    )));
}
function cardMenu(w, x, y) {
  const items = [{ grp: w.title }, { ic: '▸', label: w.state === 'terminal' ? 'Abrir aquí' : 'Abrir', run: () => openWorkspace(w.name, false, w.state) },
    { ic: '✎', label: 'Renombrar', run: () => dlgPrompt('Renombrar workspace', 'Nombre', w.title, v => api.do('ws.rename', { ws: w.name, title: v }).then(refreshHome)) }];
  if (w.state !== 'stopped') items.push({ ic: '⏻', label: 'Apagar y guardar', danger: true, run: () => dlgConfirm('¿Apagar ' + w.title + '?', 'Se guarda el estado y se cierran todas sus sesiones. Lo retomas desde aquí cuando quieras.', 'Apagar y guardar', () => api.do('ws.stop', { ws: w.name }).then(refreshHome)) });
  else items.push({ ic: '✕', label: 'Borrar', danger: true, run: () => dlgConfirm('¿Borrar ' + w.title + '?', 'Se borra su estado guardado. Las conversaciones de Claude quedan en disco.', 'Borrar', () => api.do('ws.delete', { ws: w.name }).then(refreshHome)) });
  popMenu(items, x, y);
}
async function refreshHome() { S.workspaces = await api.get('workspaces'); if (!S.ws) render(); }
async function openWorkspace(name, convert, state) {
  if (state === 'terminal' && !convert) {
    dlgConfirm('Abrir en la app', 'Este workspace está abierto en la versión de terminal. Para abrirlo aquí se guarda y se reinicia: sus sesiones retoman sus conversaciones y las que estaban trabajando reciben el mensaje de continuación.', 'Abrir aquí', () => openWorkspace(name, true), false);
    return;
  }
  try { await api.do('ws.open', { ws: name, convert: !!convert }); go(name); }
  catch (e) { toast(e.message, true); }
}

// ---------- routing & events ----------
function go(ws) {
  closeCtx();
  if (ws !== S.ws) {
    for (const t of terms.values()) t.dispose();
    terms.clear();
    stageKey = '';
    $('#root').replaceChildren();
  }
  S.ws = ws; S.state = null;
  const url = ws ? '#/ws/' + encodeURIComponent(ws) : '#/';
  if (location.hash !== url) history.pushState(null, '', url + '');
  listen();
  render();
}
window.addEventListener('popstate', () => go(routeWS()));
function routeWS() { const m = location.hash.match(/^#\/ws\/(.+)$/); return m ? decodeURIComponent(m[1]) : null; }
function listen() {
  if (S.es) S.es.close();
  const u = new URL('/api/events', location.origin);
  if (S.ws) u.searchParams.set('ws', S.ws);
  const es = new EventSource(u);
  S.es = es;
  es.addEventListener('state', ev => {
    const st = JSON.parse(ev.data);
    if (st.ws !== S.ws) return;
    const was = S.state;
    S.state = st;
    if (was && was.mode === 'app' && st.mode !== 'app') { go(null); return; }
    render();
  });
  es.addEventListener('workspaces', ev => { S.workspaces = JSON.parse(ev.data); if (!S.ws) render(); });
}

// ---------- context panel ----------
let ctx = null; // {kind, id, x, y, el}
function openCtx(kind, id, x, y) { closeCtx(); ctx = { kind, id, x, y }; renderCtx(); }
function closeCtx() { if (!ctx) return false; ctx.el?.remove(); ctx = null; return true; }
document.addEventListener('mousedown', ev => { if (ctx && ctx.el && !ctx.el.contains(ev.target) && !ev.target.closest('.scrim')) closeCtx(); }, true);
function renderCtx() {
  if (!ctx || !S.state) return;
  const items = ctx.kind === 'tab' ? tabItems(ctx.id) : slotItems(ctx.id);
  if (!items) { closeCtx(); return; }
  const [title, ...rest] = items;
  const el = h('div', { class: 'ctx' },
    h('h4', {}, h('span', { class: 't' }, title), h('span', { class: 'cl', title: 'Cerrar (Esc)', onclick: () => closeCtx() }, '✕')),
    rest.map(it => it.grp ? h('div', { class: 'grp' }, it.grp)
      : h('div', { class: 'it' + (it.danger ? ' danger' : '') + (it.on ? ' on' : ''), onclick: async () => {
        try { await it.run(); } catch (e) {}
        if (it.close) closeCtx(); else renderCtx();
      } }, h('span', { class: 'ic' }, it.ic || ''), h('span', { class: 'lb' }, it.label), it.k ? h('span', { class: 'k' }, it.k) : null)));
  const old = ctx.el;
  ctx.el = el;
  if (old) old.replaceWith(el); else document.body.append(el);
  const r = el.getBoundingClientRect();
  el.style.left = Math.max(8, Math.min(ctx.x, innerWidth - r.width - 8)) + 'px';
  el.style.top = Math.max(8, Math.min(ctx.y, innerHeight - r.height - 8)) + 'px';
}
function tabItems(id) {
  const st = S.state, i = st.tabs.findIndex(t => t.id === id), t = st.tabs[i];
  if (!t) return null;
  const hasClaude = t.views.some(v => v.slots.some(s => slotOf(s)?.kind === 'claude'));
  return ['Pestaña ' + t.title, { grp: 'Pestaña' },
    i > 0 && { ic: '↑', label: 'Mover arriba', run: () => act('tab.move', { tab: id, to: i - 1 }) },
    i < st.tabs.length - 1 && { ic: '↓', label: 'Mover abajo', run: () => act('tab.move', { tab: id, to: i + 1 }) },
    { ic: '▸', label: 'Renombrar pestaña', run: () => dlgPrompt('Renombrar pestaña', 'Nombre', t.title, v => act('tab.rename', { tab: id, title: v })) },
    { ic: '✦', label: 'Nueva sesión de Claude aquí', run: () => dlgNewSession(id, 'claude'), close: true },
    hasClaude && { ic: '✦', label: 'Cambiar alias de Claude de la pestaña…', run: () => dlgBulkAlias('tab', id) },
    { ic: '✕', label: 'Cerrar pestaña', danger: true, run: () => confirmCloseTab(id), close: true },
  ].filter(Boolean);
}
function slotItems(id) {
  const s = slotOf(id), { tab, view } = findSlot(id);
  if (!s || !view) return null;
  const vi = tab.views.indexOf(view), multi = view.slots.length > 1;
  const items = [multi ? viewNames(view) : s.name, { grp: 'Vista' }];
  if (vi > 0) items.push({ ic: '←', label: 'Mover a la izquierda', run: () => act('view.move', { view: view.id, to: vi - 1 }) });
  if (vi < tab.views.length - 1) items.push({ ic: '→', label: 'Mover a la derecha', run: () => act('view.move', { view: view.id, to: vi + 1 }) });
  for (const v of tab.views) if (v.id !== view.id) items.push({ ic: '⧉', label: 'Ver junto a ' + viewNames(v), run: () => act('view.join', { view: v.id, target: view.id }) });
  for (const t of S.state.tabs) if (t.id !== tab.id) items.push({ ic: '↦', label: 'Mover a la pestaña ' + t.title, run: () => act('view.totab', { view: view.id, tab: t.id }), close: true });
  if (multi) {
    for (const [l, name, ic] of [['cols', 'Distribución: columnas', '▥'], ['rows', 'Distribución: filas', '▤'], ['grid', 'Distribución: cuadrícula', '▦']])
      items.push({ ic, label: name, on: view.layout === l, k: view.layout === l ? '✓' : '', run: () => act('view.layout', { view: view.id, layout: l }) });
    if (view.slots.some(x => slotOf(x)?.kind === 'claude')) items.push({ ic: '✦', label: 'Cambiar alias de Claude de la vista…', run: () => dlgBulkAlias('view', view.id) });
    items.push({ ic: '✕', label: 'Cerrar la vista', danger: true, run: () => confirmCloseView(view.id), close: true });
  }
  items.push({ grp: multi ? 'Sesión ' + s.name : 'Sesión' });
  items.push({ ic: '◉', label: 'Ir a esta sesión', run: () => focusSlot(id), close: true });
  items.push({ ic: '▸', label: 'Renombrar', run: () => dlgPrompt('Renombrar sesión', 'Nombre', s.name, v => act('slot.rename', { slot: id, name: v })) });
  if (s.kind === 'claude') {
    items.push({ ic: '✎', label: 'Editar sesión…', run: () => dlgEditSession(id) });
    items.push({ ic: '✦', label: 'Cambiar alias de Claude', k: s.alias, run: () => dlgBulkAlias('slot', id) });
    items.push({ ic: '↻', label: 'Reiniciar', run: () => act('slot.respawn', { slot: id }) });
  }
  if (multi) items.push({ ic: '⇱', label: 'Separar de la vista', run: () => act('slot.split', { slot: id }) });
  items.push({ ic: '✕', label: 'Cerrar sesión', danger: true, run: () => confirmCloseSlot(id), close: true });
  return items;
}
// popMenu is a one-shot menu (closes after a choice), drawn like the context panel.
function popMenu(items, x, y) {
  closeCtx();
  const el = h('div', { class: 'ctx', style: { width: '300px' } }, items.map(it => it.grp ? h('div', { class: 'grp' }, it.grp)
    : h('div', { class: 'it' + (it.danger ? ' danger' : ''), onclick: () => { closeCtx(); it.run(); } },
      h('span', { class: 'ic' }, it.ic || ''), h('span', { class: 'lb' }, it.label), it.k ? h('span', { class: 'k' }, it.k) : null)));
  ctx = { kind: 'menu', el };
  document.body.append(el);
  const r = el.getBoundingClientRect();
  el.style.left = Math.max(8, Math.min(x, innerWidth - r.width - 8)) + 'px';
  el.style.top = Math.max(8, Math.min(y, innerHeight - r.height - 8)) + 'px';
}
function wsMenu(x, y) {
  const others = S.workspaces.filter(w => w.name !== S.ws && w.state === 'app');
  popMenu([{ grp: 'Workspace' },
    { ic: '←', label: 'Todos los workspaces', run: () => go(null) },
    { ic: '▸', label: 'Renombrar workspace', run: () => dlgPrompt('Renombrar workspace', 'Nombre', S.state.title, v => act('ws.rename', { title: v })) },
    { ic: '↓', label: 'Guardar ahora', run: () => act('ws.save').then(() => toast('Estado guardado')) },
    { ic: '✦', label: 'Cambiar alias de Claude de todo…', run: () => dlgBulkAlias('all', '') },
    { ic: '⚙', label: 'Configuración', k: 'Alt+,', run: () => dlgSettings('general') },
    { ic: '?', label: 'Atajos de teclado', k: 'Alt+H', run: () => dlgSettings('keys') },
    ...(others.length ? [{ grp: 'Otros abiertos' }, ...others.map(w => ({ ic: '◆', label: w.title, run: () => go(w.name) }))] : []),
    { grp: '' },
    { ic: '⏻', label: 'Apagar y guardar', danger: true, run: () => confirmStop() },
  ], x, y);
}

// ---------- dialogs ----------
function modal({ title, sub, body, buttons, width, onclose }) {
  const err = h('span', { class: 'err' });
  const scrim = h('div', { class: 'scrim' });
  const close = () => { scrim.remove(); onclose?.(); const v = curView(); if (v?.focus) terms.get(v.focus)?.focus(); };
  scrim._close = close;
  scrim.addEventListener('mousedown', ev => { if (ev.target === scrim) close(); });
  const btns = buttons.map(b => h('button', { class: 'btn' + (b.primary ? ' primary' : '') + (b.danger ? ' danger' : ''), onclick: async ev => {
    if (!b.run) { close(); return; }
    const el = ev.currentTarget; el.disabled = true; err.textContent = '';
    try { const keep = await b.run(); if (keep !== true) close(); }
    catch (e) { err.textContent = e.message; }
    finally { el.disabled = false; }
  } }, b.label));
  const m = h('div', { class: 'modal', style: width ? { width } : null }, h('header', {}, title), sub ? h('div', { class: 'sub' }, sub) : null,
    h('div', { class: 'body' }, body), h('footer', {}, err, btns));
  scrim.append(m);
  document.body.append(scrim);
  m.addEventListener('keydown', ev => {
    if (ev.key === 'Enter' && !ev.shiftKey && ev.target.tagName !== 'TEXTAREA' && !ev.target.closest('.sugg')) {
      const p = btns.find(b => b.classList.contains('primary') || b.classList.contains('danger'));
      if (p && !p.disabled) { ev.preventDefault(); p.click(); }
    }
  });
  setTimeout(() => { const f = m.querySelector('input,textarea'); if (f) { f.focus(); f.select?.(); } else btns[btns.length - 1]?.focus(); }, 20);
  return { scrim, close, err };
}
const field = (label, input, hint) => h('div', {}, h('label', { class: 'l' }, label), input, hint ? h('div', { class: 'hint' }, hint) : null);
function seg(options, value, onchange) {
  const el = h('div', { class: 'seg' });
  const draw = () => el.replaceChildren(...options.map(o => h('button', { class: o.value === value ? 'on' : '', type: 'button', title: o.title || '',
    onclick: () => { value = o.value; draw(); onchange(value); } }, o.label)));
  draw();
  el.set = v => { value = v; draw(); };
  return el;
}
function check(label, value, onchange) {
  const el = h('div', { class: 'check' + (value ? ' on' : ''), onclick: () => { value = !value; el.classList.toggle('on', value); onchange(value); } }, h('i', {}, '✓'), label);
  return el;
}
function dlgPrompt(title, label, value, run) {
  const inp = h('input', { class: 'input', value });
  modal({ title, body: field(label, inp), buttons: [{ label: 'Cancelar' }, { label: 'Guardar', primary: true, run: async () => {
    const v = inp.value.trim(); if (!v) throw new Error('No puede quedar vacío.'); await run(v);
  } }] });
}
function dlgConfirm(title, detail, yes, run, danger = true) {
  modal({ title, body: h('div', { style: { color: 'var(--muted)', lineHeight: 1.55 } }, detail), buttons: [{ label: 'Cancelar' }, { label: yes, danger, primary: !danger, run }] });
}
function confirmCloseSlot(id) {
  const s = slotOf(id); if (!s) return;
  dlgConfirm('¿Cerrar ' + s.name + '?', s.kind === 'claude'
    ? 'Claude se cierra; la conversación queda en disco y puedes retomarla desde una pestaña vacía o por su ID.'
    : 'La terminal se cierra y deja de guardarse en el workspace.', 'Cerrar', () => act('slot.close', { slot: id }));
}
function confirmCloseView(id) {
  const { view } = (() => { for (const t of S.state.tabs) for (const v of t.views) if (v.id === id) return { view: v }; return {}; })();
  if (!view) return;
  dlgConfirm('¿Cerrar la vista con sus ' + view.slots.length + ' sesiones?', 'Se cierran ' + viewNames(view) + '. Las conversaciones de Claude quedan en disco.', 'Cerrar vista', () => act('view.close', { view: id }));
}
function confirmCloseTab(id) {
  const t = S.state.tabs.find(t => t.id === id); if (!t) return;
  const n = t.views.reduce((a, v) => a + v.slots.length, 0);
  dlgConfirm('¿Cerrar la pestaña ' + t.title + '?', n ? 'Se cierran sus ' + plural(n, 'sesión', 'sesiones') + '. Las conversaciones de Claude quedan en disco y puedes retomarlas después.' : 'La pestaña está vacía.', 'Cerrar pestaña', () => act('tab.close', { tab: id }));
}
function confirmStop() {
  dlgConfirm('¿Apagar ' + S.state.title + '?', 'Se guarda el estado y se cierran todas las sesiones. Lo retomas desde el inicio; las que estaban trabajando reciben el mensaje de continuación.', 'Apagar y guardar',
    async () => { await act('ws.stop'); go(null); });
}

async function loadConfig() {
  const c = await api.get('config');
  S.config = c.config; S.aliases = c.aliases; S.defaultAlias = c.default; S.shellAliases = c.shellAliases || []; S.configFile = c.configFile;
  applyTheme(S.config.theme);
  return c;
}
function aliasSeg(value, onchange) {
  return seg(S.aliases.map(a => ({ value: a.name, label: a.name, title: a.summary })), value, onchange);
}

function dlgNewSession(tabId, mode, conv) {
  const tab = S.state.tabs.find(t => t.id === tabId); if (!tab) return;
  let alias = conv?.alias || S.defaultAlias, resume = conv || null;
  const name = h('input', { class: 'input', placeholder: mode === 'shell' ? 'terminal' : 'p. ej. fix-matriz', value: conv ? (conv.title || '') : '' });
  const args = h('input', { class: 'input', placeholder: '--model opus' });
  const preview = h('div', { class: 'code' }, '…');
  const idInput = h('input', { class: 'input', placeholder: 'ID de la conversación (completo o sus primeros caracteres)', value: conv ? conv.id : '' });
  const results = h('div', { class: 'list' });
  const aSeg = aliasSeg(alias, v => { alias = v; upd(); });
  const body = h('div', { style: { display: 'grid', gap: '14px' } });
  let timer;
  const upd = () => {
    clearTimeout(timer);
    if (mode === 'shell') return;
    timer = setTimeout(async () => {
      try { const p = await api.get('preview', { ws: S.ws, name: name.value || 'nombre', alias, args: args.value, resume: mode === 'resume' && resume ? resume.id : '' }); preview.textContent = p.command; }
      catch (e) { preview.textContent = e.message; }
    }, 120);
  };
  const search = async () => {
    const q = idInput.value.trim();
    results.replaceChildren();
    if (q.length < 6) { results.append(h('div', { class: 'hint' }, q ? 'Escribe al menos 6 caracteres.' : 'Pega el ID: lo ves con /status en Claude o en el nombre del archivo .jsonl.')); return; }
    const rows = await api.get('conversations', { q, ws: S.ws }).catch(() => []);
    if (!rows.length) { results.append(h('div', { class: 'hint', style: { color: 'var(--red)' } }, 'No encontré ninguna conversación con ese ID.')); resume = null; return; }
    const pick = c => {
      resume = c; alias = c.alias || alias; aSeg.set(alias);
      if (!name.value) name.value = c.title || shorten((c.first || '').split(/\s+/).join('-'), 24);
      [...results.children].forEach((el, i) => el.classList.toggle('sel', rows[i] === c)); upd();
    };
    rows.forEach(c => results.append(h('div', { class: 'row-item', onclick: () => pick(c) },
      h('b', {}, (c.title || shorten(c.first, 50)) + '  ', h('span', { class: 'w' }, c.id.slice(0, 8))),
      h('span', { class: 'w' }, c.when + ' · ' + c.short), h('span', { class: 'q' }, c.openAs ? 'Ya está abierta aquí como ' + c.openAs : '«' + c.first + '»'))));
    pick(rows[0]);
  };
  idInput.addEventListener('input', () => { clearTimeout(idInput._t); idInput._t = setTimeout(search, 180); });
  name.addEventListener('input', upd); args.addEventListener('input', upd);
  const kinds = seg([{ value: 'claude', label: '✦ Claude' }, { value: 'shell', label: '❯ Terminal' }, { value: 'resume', label: '↻ Retomar por ID' }], mode, v => { mode = v; draw(); });
  const draw = () => {
    body.replaceChildren(kinds);
    if (mode === 'resume') body.append(field('Conversación', idInput), results);
    body.append(field('Nombre', name));
    if (mode !== 'shell') body.append(field('Alias de Claude', aSeg), field('Argumentos extra', args), field('Se ejecutará', preview));
    m.scrim.querySelector('header').textContent = mode === 'shell' ? 'Nueva terminal' : mode === 'resume' ? 'Retomar conversación de Claude' : 'Nueva sesión de Claude';
    primary.textContent = mode === 'shell' ? 'Abrir terminal' : mode === 'resume' ? 'Retomar' : 'Crear sesión';
    upd();
    setTimeout(() => (mode === 'resume' && !resume ? idInput : name).focus(), 10);
  };
  const m = modal({ title: '', sub: 'en la pestaña ' + tab.title + ' · ' + tab.short, body, buttons: [{ label: 'Cancelar' }, { label: 'Crear', primary: true, run: async () => {
    if (mode === 'resume' && !resume) throw new Error('Primero elige una conversación.');
    const n = name.value.trim();
    if (mode !== 'shell' && !n) throw new Error('Ponle un nombre a la sesión.');
    await api.do('slot.new', { ws: S.ws, tab: tabId, kind: mode === 'shell' ? 'shell' : 'claude', name: n, alias, args: args.value,
      resume: mode === 'resume' ? resume.id : '', ...stageSize() });
  } }] });
  const primary = m.scrim.querySelector('.btn.primary');
  draw();
  if (mode === 'resume' && conv) search();
  else if (mode === 'resume') search();
}

function dirInput(value) {
  const inp = h('input', { class: 'input', value, spellcheck: 'false' });
  const box = h('div', { class: 'sugg' });
  let sel = -1, rows = [];
  const load = async () => {
    rows = await api.get('dirs', { q: inp.value }).catch(() => []);
    sel = -1;
    box.replaceChildren(...rows.map((r, i) => h('div', { onmousedown: ev => { ev.preventDefault(); inp.value = r.short + '/'; inp.dispatchEvent(new Event('input')); inp.focus(); } }, r.short, r.recent ? h('small', {}, 'reciente') : null)));
  };
  inp.addEventListener('input', () => { clearTimeout(inp._t); inp._t = setTimeout(load, 120); });
  inp.addEventListener('focus', load);
  inp.addEventListener('keydown', ev => {
    if (!rows.length) return;
    if (ev.key === 'ArrowDown' || ev.key === 'ArrowUp') {
      ev.preventDefault(); sel = (sel + (ev.key === 'ArrowDown' ? 1 : -1) + rows.length) % rows.length;
      [...box.children].forEach((el, i) => el.classList.toggle('sel', i === sel));
    } else if (ev.key === 'Tab' && sel >= 0) { ev.preventDefault(); inp.value = rows[sel].short + '/'; load(); }
    else if (ev.key === 'Enter' && sel >= 0) { ev.preventDefault(); ev.stopPropagation(); inp.value = rows[sel].short; load(); }
  });
  const wrap = h('div', {}, inp, box);
  wrap.input = inp;
  return wrap;
}

function dlgNewTab() {
  const tab = curTab();
  let start = 'claude', alias = S.defaultAlias;
  const name = h('input', { class: 'input', placeholder: 'p. ej. bug-185' });
  const dir = dirInput(tab ? tab.short : (S.info?.launchShort || '~'));
  const sess = h('input', { class: 'input', placeholder: 'igual que la pestaña' });
  const extra = h('div', { style: { display: 'grid', gap: '14px' } });
  const drawExtra = () => extra.replaceChildren(...(start === 'claude' ? [field('Nombre de la sesión', sess), field('Alias de Claude', aliasSeg(alias, v => alias = v))] : []));
  drawExtra();
  modal({ title: 'Nueva pestaña', sub: 'Una pestaña agrupa las sesiones de una tarea, en su carpeta.', body: [
    field('Nombre', name), field('Carpeta', dir),
    field('Empezar con', seg([{ value: 'claude', label: '✦ Claude' }, { value: 'shell', label: '❯ Terminal' }, { value: 'empty', label: 'Vacía' }], start, v => { start = v; drawExtra(); })),
    extra], buttons: [{ label: 'Cancelar' }, { label: 'Crear pestaña', primary: true, run: async () => {
      const d = dir.input.value.trim();
      const title = name.value.trim() || d.replace(/\/+$/, '').split('/').pop();
      await api.do('tab.new', { ws: S.ws, title, dir: d, start, name: sess.value.trim() || title, alias, ...stageSize() });
    } }] });
}

function dlgEditSession(id) {
  const s = slotOf(id); if (!s) return;
  let alias = s.alias || S.defaultAlias, sendContinue = true;
  const name = h('input', { class: 'input', value: s.name });
  const args = h('input', { class: 'input', value: (s.args || []).map(a => /[\s'"]/.test(a) ? JSON.stringify(a) : a).join(' '), placeholder: '--model opus' });
  const preview = h('div', { class: 'code' }, '…');
  const note = h('div');
  const upd = async () => {
    const working = s.status === 'busy' || s.status === 'waiting';
    note.replaceChildren(alias !== s.alias ? check('Cambia el alias: se reinicia' + (working ? '. Mandarle «' + (S.config?.continue_message || 'continue') + '» porque está trabajando' : ''), sendContinue, v => sendContinue = v) : '');
    try { preview.textContent = (await api.get('preview', { ws: S.ws, name: name.value, alias, args: args.value, resume: s.sid })).command; } catch (e) {}
  };
  name.addEventListener('input', upd); args.addEventListener('input', upd);
  const body = s.kind === 'claude'
    ? [field('Nombre', name), field('Alias de Claude', aliasSeg(alias, v => { alias = v; upd(); })), field('Argumentos extra', args), note, field('Al reiniciar se ejecutará', preview)]
    : [field('Nombre', name)];
  const save = restart => async () => api.do('slot.edit', { ws: S.ws, slot: id, name: name.value, alias, args: args.value, restart, sendContinue });
  modal({ title: 'Editar ' + s.name, sub: s.kind === 'claude' ? 'Los argumentos nuevos se aplican al reiniciar la sesión.' : s.short, body,
    buttons: s.kind === 'claude' ? [{ label: 'Cancelar' }, { label: 'Guardar y reiniciar', run: save(true) }, { label: 'Guardar', primary: true, run: save(false) }]
      : [{ label: 'Cancelar' }, { label: 'Guardar', primary: true, run: save(false) }] });
  upd();
}

function dlgBulkAlias(scope, id) {
  let targets = [];
  const st = S.state;
  if (scope === 'slot') targets = [id];
  if (scope === 'view') for (const t of st.tabs) for (const v of t.views) if (v.id === id) targets = v.slots;
  if (scope === 'tab') { const t = st.tabs.find(t => t.id === id); t && t.views.forEach(v => targets.push(...v.slots)); }
  if (scope === 'all') st.tabs.forEach(t => t.views.forEach(v => targets.push(...v.slots)));
  targets = targets.filter(x => slotOf(x)?.kind === 'claude');
  if (!targets.length) { toast('No hay sesiones de Claude ahí.'); return; }
  let alias = scope === 'slot' ? slotOf(id).alias : S.defaultAlias, sendContinue = true;
  const plan = h('div', { class: 'code' });
  const btnLabel = () => targets.filter(x => slotOf(x).alias !== alias).length;
  const draw = () => {
    plan.replaceChildren(...targets.map(x => { const s = slotOf(x); const w = s.status === 'busy' || s.status === 'waiting';
      return h('div', {}, s.name + ' → ' + alias + ' · ' + (s.alias === alias ? 'sin cambio' : w && sendContinue ? 'se reinicia y continúa' : 'se reinicia')); }));
    const p = m && m.scrim.querySelector('.btn.primary'); if (p) p.textContent = 'Cambiar ' + plural(btnLabel(), 'sesión', 'sesiones');
  };
  const where = { slot: slotOf(id)?.name, view: 'la vista', tab: 'la pestaña ' + (st.tabs.find(t => t.id === id)?.title || ''), all: 'todo el workspace' }[scope];
  const m = modal({ title: scope === 'slot' ? 'Cambiar alias de Claude' : 'Cambiar alias de ' + where, sub: plural(targets.length, 'sesión', 'sesiones') + ' · ' + targets.map(x => slotOf(x).name).join(', '),
    body: [field('Nuevo alias', aliasSeg(alias, v => { alias = v; draw(); })),
      check('Mandar «' + (S.config?.continue_message || 'continue') + '» a las que estaban trabajando', sendContinue, v => { sendContinue = v; draw(); }), plan],
    buttons: [{ label: 'Cancelar' }, { label: 'Cambiar', primary: true, run: async () => {
      if (!btnLabel()) return;
      await api.do('alias.bulk', { ws: S.ws, scope, slot: id, view: id, tab: id, alias, sendContinue });
    } }] });
  draw();
}

function dlgNewWorkspace(dirValue) {
  const dir = dirInput(dirValue ? (S.info && dirValue.startsWith(S.info.home) ? '~' + dirValue.slice(S.info.home.length) : dirValue) : '~');
  const name = h('input', { class: 'input', placeholder: 'como la carpeta' });
  modal({ title: 'Nuevo workspace', sub: 'Empieza con una pestaña en la carpeta.', body: [field('Carpeta', dir), field('Nombre', name)],
    buttons: [{ label: 'Cancelar' }, { label: 'Crear workspace', primary: true, run: async () => {
      const r = await api.do('ws.new', { dir: dir.input.value.trim(), title: name.value.trim() });
      go(r.ws);
    } }] });
}

function dlgFromID() {
  const inp = h('input', { class: 'input', placeholder: 'ID de la conversación (completo o sus primeros caracteres)' });
  const list = h('div', { class: 'list' }, h('div', { class: 'hint' }, 'Se crea un workspace en la carpeta donde empezó la conversación, con ella retomada.'));
  let found = null;
  inp.addEventListener('input', () => { clearTimeout(inp._t); inp._t = setTimeout(async () => {
    const q = inp.value.trim(); found = null;
    if (q.length < 6) { list.replaceChildren(h('div', { class: 'hint' }, 'Escribe al menos 6 caracteres.')); return; }
    const rows = await api.get('conversations', { q }).catch(() => []);
    if (!rows.length) { list.replaceChildren(h('div', { class: 'hint', style: { color: 'var(--red)' } }, 'No encontré ninguna conversación con ese ID.')); return; }
    found = rows[0];
    list.replaceChildren(...rows.map((c, i) => h('div', { class: 'row-item' + (i === 0 ? ' sel' : ''), onclick: ev => {
      found = c; [...list.children].forEach(el => el.classList.remove('sel')); ev.currentTarget.classList.add('sel'); } },
      h('b', {}, c.title || shorten(c.first, 50)), h('span', { class: 'w' }, c.when + ' · ' + c.short), h('span', { class: 'q' }, '«' + c.first + '»'))));
  }, 180); });
  modal({ title: 'Workspace desde una conversación', body: [field('Conversación', inp), list], buttons: [{ label: 'Cancelar' }, { label: 'Crear workspace', primary: true, run: async () => {
    if (!found) throw new Error('Primero escribe un ID que exista.');
    const r = await api.do('ws.fromid', { resume: found.id, alias: found.alias });
    go(r.ws);
  } }] });
}

// ---------- settings ----------
async function dlgSettings(section) {
  await loadConfig().catch(e => toast(e.message, true));
  const scrim = h('div', { class: 'scrim' });
  const close = () => { scrim.remove(); const v = curView(); if (v?.focus) terms.get(v.focus)?.focus(); };
  scrim._close = close;
  scrim.addEventListener('mousedown', ev => { if (ev.target === scrim) close(); });
  const nav = h('nav'), sec = h('section');
  const sections = [['general', 'General'], ['aliases', 'Alias de Claude'], ['look', 'Apariencia'], ['keys', 'Atajos']];
  const set = async patch => { try { await api.do('config.set', { config: patch }); await loadConfig(); } catch (e) { toast(e.message, true); } };
  const draw = () => {
    nav.replaceChildren(h('div', { class: 'h' }, 'Configuración'), ...sections.map(([id, l]) => h('div', { class: 'i' + (id === section ? ' on' : ''), onclick: () => { section = id; draw(); } }, l)),
      h('div', { class: 'sp' }), h('div', { class: 'i', onclick: close }, '✕ Cerrar'));
    const c = S.config;
    if (section === 'general') {
      const msg = h('input', { class: 'input', style: { width: '240px' }, value: c.continue_message || '', onchange: ev => set({ continue_message: ev.target.value }) });
      const cargs = h('input', { class: 'input', style: { width: '240px' }, value: (c.claude_args || []).join(' '), placeholder: 'p. ej. --permission-mode auto',
        onchange: ev => set({ claude_args: ev.target.value.trim() ? ev.target.value.trim().split(/\s+/) : [] }) });
      sec.replaceChildren(h('h3', {}, 'General'),
        h('div', { class: 'setrow' }, h('div', {}, 'Mensaje de continuación', h('small', {}, 'Lo reciben las sesiones que estaban trabajando al apagar o reiniciar.')), msg),
        h('div', { class: 'setrow' }, h('div', {}, 'Arranque diferido', h('small', {}, 'Al retomar un workspace, las sesiones ociosas esperan a que pulses Enter en ellas.')),
          h('button', { class: 'toggle' + (c.lazy_start ? ' on' : ''), onclick: () => set({ lazy_start: !c.lazy_start }).then(draw) }, h('i'))),
        h('div', { class: 'setrow' }, h('div', {}, 'Autoguardado', h('small', {}, 'Cada cuánto se guarda el estado del workspace.')),
          seg([60, 120, 300].map(v => ({ value: v, label: v / 60 + ' min' })), c.autosave_seconds, v => set({ autosave_seconds: v }))),
        h('div', { class: 'setrow' }, h('div', {}, 'Argumentos para todas las sesiones', h('small', {}, 'Se agregan a cada sesión de Claude que se abra.')), cargs),
        h('div', { class: 'setrow' }, h('div', {}, 'Archivo de configuración', h('small', {}, S.configFile)), ''));
    } else if (section === 'aliases') {
      sec.replaceChildren(h('h3', {}, 'Alias de Claude'),
        h('p', { class: 'hint', style: { marginTop: 0, marginBottom: '14px' } }, 'Formas de lanzar Claude: variables de entorno y argumentos, como tus alias de la shell. La estrella marca el que se propone para las sesiones nuevas.'),
        ...S.aliases.map(a => h('div', { class: 'acard' },
          h('div', { class: 'tx' }, h('b', {}, a.name), a.name === S.defaultAlias ? h('span', { class: 'star' }, '★ por defecto') : null, h('div', {}, a.summary)),
          a.name !== S.defaultAlias ? h('button', { class: 'btn', onclick: () => set({ default_claude_alias: a.name }).then(draw) }, 'Por defecto') : null,
          !a.builtin ? h('button', { class: 'btn', onclick: () => dlgAlias(a, draw) }, 'Editar') : null,
          !a.builtin ? h('button', { class: 'btn', onclick: () => dlgConfirm('¿Borrar el alias ' + a.name + '?', 'Las sesiones que lo usan pasan al alias por defecto la próxima vez que se abran.', 'Borrar', async () => { await api.do('alias.delete', { name: a.name }); await loadConfig(); draw(); }) }, '✕') : null)),
        h('button', { class: 'btn primary', style: { marginTop: '6px' }, onclick: () => dlgAlias(null, draw) }, '＋ Agregar alias'),
        S.shellAliases.length ? h('div', { style: { marginTop: '22px' } },
          h('div', { class: 'hint', style: { marginBottom: '8px' } }, 'Detectados en tu shell — clic para agregarlos:'),
          h('div', { style: { display: 'flex', gap: '6px', flexWrap: 'wrap' } }, S.shellAliases.map(a => h('button', { class: 'chip', style: { cursor: 'pointer' }, title: a.summary,
            onclick: async () => { await api.do('alias.import', { name: a.name }).catch(e => toast(e.message, true)); await loadConfig(); draw(); } }, '＋ ' + a.name)))) : null);
    } else if (section === 'look') {
      const fs = c.app_font_size || 13.5;
      sec.replaceChildren(h('h3', {}, 'Apariencia'),
        h('div', { class: 'setrow' }, h('div', {}, 'Color'), h('div', { class: 'swatches' }, Object.entries(THEMES).map(([n, [col]]) =>
          h('i', { class: (c.theme || 'azul') === n ? 'on' : '', title: n, style: { background: col }, onclick: () => set({ theme: n }).then(draw) })))),
        h('div', { class: 'setrow' }, h('div', {}, 'Tamaño de letra de las terminales'),
          seg([12, 13, 14, 15, 16].map(v => ({ value: v, label: String(v) })), Math.round(fs), v => set({ app_font_size: v }).then(() => {
            for (const t of terms.values()) { t.term.options.fontSize = v; t.refit(); }
          }))));
    } else {
      const k = (a, b) => [h('div', {}, ...a.split(' ').map(x => h('kbd', {}, x))), h('span', {}, b)];
      sec.replaceChildren(h('h3', {}, 'Atajos'), h('div', { class: 'keys' },
        k('Alt+← Alt+→', 'sesión (vista) anterior / siguiente'), k('Alt+↑ Alt+↓', 'pestaña anterior / siguiente'), k('Alt+1…9', 'ir a la sesión N'),
        k('Alt+N', 'nueva sesión de Claude'), k('Alt+Y', 'nueva terminal'), k('Alt+I', 'retomar una conversación por su ID'), k('Alt+T', 'nueva pestaña'),
        k('Alt+R', 'editar la sesión actual'), k('Alt+P', 'cambiar el alias de Claude de la sesión'), k('Alt+W', 'cerrar la sesión actual'),
        k('Alt+M', 'menú del workspace'), k('Alt+,', 'configuración'), k('Alt+H', 'estos atajos'), k('Esc', 'cierra paneles y diálogos'),
        k('Ctrl+C', 'copia si hay texto seleccionado (si no, lo recibe la sesión)'), k('Ctrl+V', 'pega'),
        k('Arrastrar', 'selecciona y copia texto de una sesión; Mayús+arrastrar selecciona sin pasar por tmux'),
        k('Clic derecho', 'acciones de la pestaña, la sesión o la vista')));
    }
  };
  scrim.append(h('div', { class: 'settings', tabindex: '-1' }, nav, sec));
  document.body.append(scrim);
  draw();
  $('.settings', scrim).focus();
}
function dlgAlias(a, after) {
  const name = h('input', { class: 'input', value: a ? a.name : '', placeholder: 'p. ej. yolo' });
  const env = h('textarea', { class: 'input', placeholder: 'CLAUDE_CONFIG_DIR=~/.claude-2\nOTRA_VARIABLE=valor' }, a ? (a.env || []).join('\n') : '');
  const args = h('input', { class: 'input', value: a ? (a.args || []).join(' ') : '', placeholder: '--dangerously-skip-permissions' });
  modal({ title: a ? 'Editar alias ' + a.name : 'Nuevo alias de Claude', body: [field('Nombre', name),
    field('Variables de entorno', env, 'Una por línea, NOMBRE=valor.'), field('Argumentos', args,
      'Si dejas variables y argumentos vacíos, se ejecuta tu propio comando de la shell con ese nombre (alias o función).')],
    buttons: [{ label: 'Cancelar' }, { label: 'Guardar', primary: true, run: async () => {
      await api.do('alias.save', { old: a ? a.name : '', name: name.value.trim(), env: env.value.split('\n'), argList: args.value.trim() ? args.value.trim().split(/\s+/) : [] });
      await loadConfig(); after();
    } }] });
}

// ---------- start ----------
window.addEventListener('resize', () => { for (const t of terms.values()) t.refit(); if (ctx && ctx.kind !== 'menu') renderCtx(); });
(async function init() {
  try {
    const [info, , list] = await Promise.all([api.get('info'), loadConfig(), api.get('workspaces')]);
    S.info = info; S.workspaces = list;
  } catch (e) { $('#root').textContent = 'No pude conectar con cw: ' + e.message; return; }
  if (S.launchDir) history.replaceState(null, '', location.pathname + location.hash);
  go(routeWS());
})();
