'use strict';
// cw — the app's window. Plain DOM, no build step: the server embeds this directory.

// ---------- helpers ----------
const $ = (sel, el = document) => el.querySelector(sel);
const isMac = /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);
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
const statusText = { busy: 'trabajando', waiting: 'pide permiso', idle: 'lista', sleeping: 'suspendida · no corre Claude' };
const plural = (n, one, many) => n + ' ' + (n === 1 ? one : many);
const THEMES = { azul: ['#8c95cf', '#a5aee0'], morado: ['#a594d6', '#bcaee3'], verde: ['#79ad8a', '#9cc4a8'],
  naranja: ['#d39a6a', '#e2b58c'], rosa: ['#c98bab', '#dba8c2'], gris: ['#a8a59e', '#c2bfb8'] };
function applyTheme(name, mode) {
  const [a, b] = THEMES[name] || THEMES.azul;
  const root = document.documentElement;
  root.dataset.theme = mode === 'light' ? 'light' : 'dark';
  const r = root.style;
  r.setProperty('--accent', a); r.setProperty('--accent2', b);
  const rgb = [1, 3, 5].map(i => parseInt(a.slice(i, i + 2), 16)).join(',');
  r.setProperty('--accentbg', `rgba(${rgb},.16)`); r.setProperty('--accentring', `rgba(${rgb},.28)`);
  for (const t of terms.values()) t.term.options.theme = termTheme();
}

// ---------- state ----------
const S = {
  info: null, config: null, aliases: [], defaultAlias: 'claude',
  workspaces: [], ws: null, state: null, es: null,
  launchDir: new URLSearchParams(location.search).get('dir') || '',
  expanded: new Set(), // tabs open in the sidebar tree besides the current one
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
  const accent = cs.getPropertyValue('--accent').trim() || '#8c95cf';
  if (document.documentElement.dataset.theme === 'light') return {
    background: '#f7f6f3', foreground: '#2b2a27', cursor: accent, cursorAccent: '#f7f6f3', selectionBackground: 'rgba(85,97,179,.22)',
    black: '#2b2a27', red: '#b9473a', green: '#3f7d52', yellow: '#9a6b14', blue: '#4b57a8', magenta: '#8a4fa3', cyan: '#2f7c84', white: '#6f6c66',
    brightBlack: '#8a877f', brightRed: '#c9584a', brightGreen: '#4d8f61', brightYellow: '#a87a20', brightBlue: '#5866b8', brightMagenta: '#9a60b3', brightCyan: '#3d8c94', brightWhite: '#23221f',
  };
  return {
    background: '#141414', foreground: '#d8d5cf', cursor: accent, cursorAccent: '#141414', selectionBackground: 'rgba(140,149,207,.30)',
    black: '#1f1f1f', red: '#d7735f', green: '#79ad8a', yellow: '#d6a24b', blue: '#8c95cf', magenta: '#b694d6', cyan: '#7fb5b5', white: '#d8d5cf',
    brightBlack: '#67645e', brightRed: '#e5907e', brightGreen: '#97c4a5', brightYellow: '#e3b96c', brightBlue: '#a9b0de', brightMagenta: '#cbb0e3', brightCyan: '#9cc9c9', brightWhite: '#f3f1ed',
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
    // Option is Meta for Claude's shortcuts, but keyboards like the Latin American or Spanish ones
    // type symbols with it (Option+Q is @): a composed ASCII symbol is typed as itself.
    if (isMac && ev.altKey && !ev.ctrlKey && !ev.metaKey && /^[!-\/:-@\[-`{-~]$/.test(ev.key)) {
      this.term.input(ev.key, true); ev.preventDefault(); return false;
    }
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
  if (browserShortcut(ev)) return true;
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
    case 'k': skyToggle(); return true;
    case 'j': if (skyOn()) { window.skyVoice?.push(); return true; } return false;
    case 'm': { const r = $('.ws').getBoundingClientRect(); wsMenu(r.left + 12, r.bottom + 4); return true; }
    case 'h': dlgSettings('keys'); return true;
    case ',': dlgSettings('general'); return true;
  }
  if (/^[1-9]$/.test(k)) { const v = views[+k - 1]; if (v) selectView(v.id); return true; }
  return false;
}
// The browser's tab shortcuts, applied to the sessions of the current tab (not to the tabs):
// Ctrl+T new Claude session, Ctrl+W close the session, Ctrl+Shift+T reopen the last closed,
// Ctrl+Tab / Ctrl+PgDn next, Ctrl+Shift+Tab / Ctrl+PgUp previous, Ctrl+Shift+PgUp/PgDn move it,
// Ctrl+1…8 the Nth, Ctrl+9 the last. They can be turned off in the settings.
function browserShortcut(ev) {
  if (!(ev.ctrlKey || ev.metaKey) || ev.altKey || (S.config && S.config.browser_keys === false)) return false;
  const tab = curTab(), view = curView();
  if (!tab) return false;
  const views = tab.views, vi = view ? views.indexOf(view) : -1;
  const k = ev.key.length === 1 ? ev.key.toLowerCase() : ev.key;
  const go = d => { if (views.length) selectView(views[(vi + d + views.length) % views.length].id); };
  switch (k) {
    case 't': if (ev.shiftKey) act('slot.reopen', { tab: tab.id, ...stageSize() }).catch(() => {}); else dlgNewSession(tab.id, 'claude'); return true;
    case 'w': if (!ev.shiftKey && view?.focus) { confirmCloseSlot(view.focus); return true; } return false;
    case 'Tab': go(ev.shiftKey ? -1 : 1); return true;
    case 'PageDown': if (ev.shiftKey) { if (view && vi < views.length - 1) act('view.move', { view: view.id, to: vi + 1 }); } else go(1); return true;
    case 'PageUp': if (ev.shiftKey) { if (view && vi > 0) act('view.move', { view: view.id, to: vi - 1 }); } else go(-1); return true;
  }
  if (!ev.shiftKey && /^[1-9]$/.test(k)) {
    const v = k === '9' ? views[views.length - 1] : views[+k - 1];
    if (v) selectView(v.id);
    return true;
  }
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
    app = h('div', { class: 'app' }, h('aside', { class: 'side' }),
      h('div', { class: 'main' }, h('div', { class: 'bar' }), h('div', { class: 'document-tabs' }), h('div', { class: 'document-panel', hidden: true }), h('div', { class: 'stage' }), h('div', { class: 'status' })));
    root.append(app);
  }
  renderSide($('.side', app));
  renderBar($('.bar', app));
  renderStage($('.stage', app));
  renderDocuments(app);
  renderStatus($('.status', app));
  renderSky(app);
  // An open menu is left alone (rebuilding it on every status change made it flicker and closed
  // its submenus); it only closes when what it acts on is gone.
  if (ctx && ctx.kind !== 'menu' && !(ctx.kind === 'tab' ? tabItems(ctx.id) : slotItems(ctx.id))) closeCtx();
  document.title = S.state.title + ' — Claude Workspaces';
}

// The sidebar is a tree: each tab, and under the open ones their sessions (one row per view).
function renderSide(side) {
  const st = S.state;
  const counts = t => {
    const c = { busy: 0, waiting: 0, idle: 0, sleeping: 0 };
    t.views.forEach(v => v.slots.forEach(id => { const s = slotOf(id); if (s && s.kind === 'claude' && !s.dead) c[s.status === 'busy' ? 'busy' : s.status === 'waiting' ? 'waiting' : s.status === 'sleeping' ? 'sleeping' : 'idle']++; }));
    return h('span', { class: 'cnt' }, ['busy', 'waiting', 'idle', 'sleeping'].filter(k => c[k]).map(k => h('i', { title: statusLabel[k] }, h('span', { class: 'd ' + k }), c[k])));
  };
  const tree = h('div', { class: 'tree' });
  st.tabs.forEach((t, i) => {
    const open = t.id === st.active || S.expanded.has(t.id);
    const row = h('div', {
      class: 'trow tab' + (t.id === st.active ? ' cur' : ''), draggable: 'true', title: t.dir,
      onclick: ev => { if (ev.target.closest('.caret') && t.id !== st.active) { S.expanded.has(t.id) ? S.expanded.delete(t.id) : S.expanded.add(t.id); render(); return; } selectTab(t.id); },
      oncontextmenu: ev => { ev.preventDefault(); openCtx('tab', t.id, ev.clientX, ev.clientY); },
      ondragstart: ev => { ev.dataTransfer.setData('cw/tab', t.id); ev.dataTransfer.effectAllowed = 'move'; },
      ondragover: ev => { if (ev.dataTransfer.types.includes('cw/tab')) { ev.preventDefault(); ev.currentTarget.classList.add('drop'); } },
      ondragleave: ev => ev.currentTarget.classList.remove('drop'),
      ondrop: ev => { ev.preventDefault(); ev.currentTarget.classList.remove('drop'); const id = ev.dataTransfer.getData('cw/tab'); if (id && id !== t.id) act('tab.move', { tab: id, to: i }); },
    }, h('span', { class: 'caret' }, open ? '▾' : '▸'), h('span', { class: 'n' }, t.title), open ? null : counts(t),
      h('span', { class: 'add', title: 'Nueva sesión en esta pestaña', onclick: ev => { ev.stopPropagation(); dlgNewSession(t.id, 'claude'); } }, '+'));
    tree.append(row);
    if (!open) return;
    t.views.forEach((v, vi) => {
      const on = t.id === st.active && v.id === (curView(t)?.id);
      const multi = v.slots.length > 1;
      const first = slotOf(v.slots[0]);
      tree.append(h('div', {
        class: 'trow sub' + (on ? ' on' : ''), draggable: 'true', title: multi ? viewNames(v) : (first?.short || ''),
        onclick: () => selectView(v.id),
        oncontextmenu: ev => { ev.preventDefault(); openCtx('slot', v.focus || v.slots[0], ev.clientX, ev.clientY); },
        ondragstart: ev => { ev.dataTransfer.setData('cw/view', v.id); ev.dataTransfer.effectAllowed = 'move'; },
        ondragover: ev => { if (ev.dataTransfer.types.includes('cw/view')) { ev.preventDefault(); ev.currentTarget.classList.add('drop'); } },
        ondragleave: ev => ev.currentTarget.classList.remove('drop'),
        ondrop: ev => { ev.preventDefault(); ev.currentTarget.classList.remove('drop'); const id = ev.dataTransfer.getData('cw/view'); if (id && id !== v.id) act('view.move', { view: id, to: vi }); },
      }, multi ? h('span', { class: 'mark2' }, '⧉') : dotFor(first),
        h('span', { class: 'n' }, multi ? v.slots.map(id => slotOf(id)?.name || '…').join(' + ') : (first ? first.name : '…')),
        !multi && first && first.kind === 'claude' && first.alias && first.alias !== 'claude' ? h('span', { class: 'al' }, first.alias) : null,
        multi ? h('span', { class: 'cnt' }, v.slots.map(id => dotFor(slotOf(id)))) : null));
    });
    if (t.id === st.active) {
      tree.append(h('div', { class: 'trow sub newrow' },
        h('span', { onclick: () => dlgNewSession(t.id, 'claude') }, '+ Claude'),
        h('span', { onclick: () => dlgNewSession(t.id, 'shell') }, '+ Terminal')));
    }
  });
  side.replaceChildren(
    h('div', { class: 'wsrow', title: 'Workspace (Alt+M)', onclick: ev => { const r = ev.currentTarget.getBoundingClientRect(); wsMenu(r.left + 6, r.bottom + 4); } },
      h('img', { class: 'logo', src: 'icon.svg', alt: '' }), h('b', {}, st.title), h('span', { class: 'chev' }, '⌄')),
    h('div', { class: 'grp' }, h('span', {}, 'Pestañas'), h('span', { class: 'gadd', title: 'Nueva pestaña (Alt+T)', onclick: () => dlgNewTab() }, '+')),
    tree,
    h('div', { class: 'foot' },
      h('span', { class: 'ib', title: 'Ajustes', onclick: () => dlgSettings('general') }, '⚙'),
      h('span', { class: 'ib', title: 'Uso de Claude por cuenta', onclick: () => dlgUsage() }, '◔ Uso'),
      h('span', { class: 'ib', title: 'Memoria y CPU de cada sesión', onclick: () => dlgResources() }, '▤ Recursos'),
      skyOn() ? h('span', { class: 'ib skyb' + (SKY.unread ? ' unread' : '') + (SKY.open ? ' on' : ''), 'data-n': SKY.unread || '', title: skyName() + ' (Alt+K)', onclick: () => skyToggle() }, '✦') : null,
      h('span', { class: 'ib pw', title: 'Apagar y guardar', onclick: () => confirmStop() }, '⏻')));
}

const statusLabel = { busy: 'trabajando', waiting: 'pide permiso', idle: 'lista', sleeping: 'suspendida' };
function dotFor(s) {
  if (!s) return h('span', { class: 'd' });
  if (s.kind !== 'claude') return h('span', { class: 'shellmark' }, '❯');
  if (s.dead) return h('span', { class: 'd dead', title: 'terminó con error' });
  return h('span', { class: 'd ' + (s.status || 'idle'), title: statusLabel[s.status] || 'lista' });
}

// The bar above the sessions: where we are (tab / session) and the actions of the view.
function renderBar(bar) {
  const tab = curTab(), view = curView(tab);
  if (!tab) { bar.replaceChildren(); return; }
  const crumb = h('span', { class: 'crumb' }, tab.title);
  if (view) {
    crumb.append(h('span', { class: 'sl' }, ' / '));
    view.slots.forEach((id, j) => {
      const s = slotOf(id);
      if (j) crumb.append(h('span', { class: 'sl' }, ' + '));
      crumb.append(h('b', { class: view.slots.length > 1 && view.focus === id ? 'f' : '', onclick: () => focusSlot(id) }, s ? s.name : '…'));
      if (view.slots.length === 1 && s && s.kind === 'claude' && s.alias) crumb.append(h('span', { class: 'al' }, ' ' + s.alias));
    });
  }
  const focus = view && slotOf(view.focus || view.slots[0]);
  bar.replaceChildren(crumb,
    h('span', { class: 'path' }, focus ? focus.short : tab.short),
    h('span', { class: 'acts' },
      focus && focus.kind === 'claude' && !focus.dead ? (focus.status === 'sleeping'
        ? h('button', { title: 'Reanudar', onclick: () => act('slot.wake', { slot: focus.id }) }, '▶')
        : h('button', { title: 'Suspender: Claude deja de correr; la conversación queda', onclick: () => act('slot.suspend', { slot: focus.id }) }, '⏾')) : null,
      h('button', { title: 'Nueva sesión (Ctrl+T)', onclick: () => dlgNewSession(tab.id, 'claude') }, '+'),
      focus ? h('button', { title: 'Acciones', onclick: ev => { const r = ev.currentTarget.getBoundingClientRect(); openCtx('slot', focus.id, r.right - 230, r.bottom + 4); } }, '⋯') : null));
}

function renderStatus(el) {
  const view = curView(), s = view && slotOf(view.focus || view.slots[0]);
  const left = [];
  if (s) {
    left.push(dotFor(s));
    left.push(h('span', {}, s.kind === 'claude' ? (s.dead ? 'terminó con error' : statusLabel[s.status] || 'lista') + (s.alias ? ' · ' + s.alias : '') : 'terminal'));
    if (view.slots.length > 1) left.push(h('span', { class: 'sep' }, view.slots.length + ' sesiones · ' + { cols: 'columnas', rows: 'filas', grid: 'cuadrícula' }[view.layout || 'cols']));
  }
  const keys = S.config && S.config.browser_keys === false
    ? [['Alt N', 'nueva'], ['Alt W', 'cerrar'], ['Alt ←→', 'cambiar']]
    : [['Ctrl T', 'nueva'], ['Ctrl W', 'cerrar'], ['Ctrl Tab', 'siguiente']];
  el.replaceChildren(h('span', { class: 'l' }, left),
    h('span', { class: 'k' }, keys.map(([k, l]) => h('span', { class: 'kk' }, h('kbd', {}, k), ' ' + l)), h('span', { class: 'kk' }, 'clic derecho: acciones')));
}

let stageKey = '';
let resizingView = null;
function viewWeights(view) {
  const cols = Math.ceil(Math.sqrt(view.slots.length));
  const count = view.layout === 'grid' ? cols + Math.ceil(view.slots.length / cols) : view.slots.length;
  return Array.from({ length: count }, (_, i) => Number.isFinite(view.weights?.[i]) && view.weights[i] > 0 ? view.weights[i] : 1);
}
function applyViewWeights(stage, view) {
  stage.style.gridTemplateColumns = '';
  stage.style.gridTemplateRows = '';
  const weights = viewWeights(view);
  if (view.layout === 'grid') {
    const cols = Math.ceil(Math.sqrt(view.slots.length));
    stage.style.gridTemplateRows = weights.slice(cols).map(w => 'minmax(0, ' + w + 'fr)').join(' ');
    stage.style.gridTemplateColumns = weights.slice(0, cols).map(w => 'minmax(0, ' + w + 'fr)').join(' ');
  } else {
    stage.style[view.layout === 'rows' ? 'gridTemplateRows' : 'gridTemplateColumns'] = weights.map(w => 'minmax(0, ' + w + 'fr)').join(' ');
  }
}
function paneResizer(stage, initialView, index, horizontal = false) {
  const current = () => curView()?.id === initialView.id ? curView() : initialView;
  let view = current();
  const rows = horizontal || view.layout === 'rows', cols = Math.ceil(Math.sqrt(view.slots.length));
  if (view.layout === 'grid') {
    if (horizontal) {
      if (index % cols !== 0 || index + cols >= view.slots.length) return null;
      index = cols + Math.floor(index / cols);
    } else {
      if (index % cols === cols - 1 || index + 1 >= view.slots.length) return null;
      index %= cols;
    }
  } else if (horizontal || index >= view.slots.length - 1) return null;
  const handle = h('div', { class: 'pane-resizer' + (rows ? ' horizontal' : ''), role: 'separator', tabindex: '0',
    'aria-label': 'Cambiar tamaño entre sesiones', 'aria-orientation': rows ? 'horizontal' : 'vertical',
    title: 'Arrastra para cambiar tamaños; flechas para ajustar' });
  const change = (weights, delta) => {
    const sum = weights[index] + weights[index + 1], minimum = Math.min(0.2, sum / 4);
    weights[index] = Math.max(minimum, Math.min(sum - minimum, weights[index] + delta));
    weights[index + 1] = sum - weights[index];
    view.weights = weights;
    applyViewWeights(stage, view);
  };
  const save = () => act('view.resize', { view: view.id, weights: view.weights }).catch(() => {});
  handle.addEventListener('keydown', ev => {
    if (!['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown'].includes(ev.key)) return;
    ev.preventDefault(); ev.stopPropagation();
    view = current();
    const weights = viewWeights(view);
    change(weights, ['ArrowLeft', 'ArrowUp'].includes(ev.key) ? -0.1 : 0.1); save();
  });
  handle.addEventListener('pointerdown', ev => {
    ev.preventDefault(); ev.stopPropagation();
    view = current();
    const weights = viewWeights(view), original = [...weights];
    const start = rows ? ev.clientY : ev.clientX, rect = stage.getBoundingClientRect();
    const size = rows ? rect.height : rect.width;
    const total = (view.layout === 'grid' ? (rows ? weights.slice(cols) : weights.slice(0, cols)) : weights).reduce((a, b) => a + b, 0);
    resizingView = view.id;
    handle.setPointerCapture(ev.pointerId);
    const move = e => change([...original], ((rows ? e.clientY : e.clientX) - start) / Math.max(1, size) * total);
    const end = e => {
      handle.removeEventListener('pointermove', move);
      handle.removeEventListener('pointerup', end);
      handle.removeEventListener('pointercancel', cancel);
      resizingView = null;
      if (view.weights) save();
    };
    const cancel = () => {
      view.weights = original; applyViewWeights(stage, view);
      handle.removeEventListener('pointermove', move); handle.removeEventListener('pointerup', end);
      handle.removeEventListener('pointercancel', cancel); resizingView = null;
    };
    handle.addEventListener('pointermove', move); handle.addEventListener('pointerup', end); handle.addEventListener('pointercancel', cancel);
  });
  return handle;
}
function renderStage(stage) {
  const tab = curTab(), view = curView(tab);
  if (!view) {
    const key = 'empty:' + (tab ? tab.id : '');
    if (stageKey !== key) { stageKey = key; stage.className = 'stage'; stage.replaceChildren(emptyTab(tab)); }
    return;
  }
  const multi = view.slots.length > 1;
  const key = view.id + ':' + view.layout + ':' + view.slots.join(',');
  if (stageKey !== key) {
    stageKey = key;
    stage.className = 'stage ' + (multi ? view.layout || 'cols' : 'cols');
    stage.style.setProperty('--gc', Math.ceil(Math.sqrt(view.slots.length)));
    const hadFocus = document.activeElement && document.activeElement.closest && document.activeElement.closest('.termhost');
    stage.replaceChildren(...view.slots.map((id, index) => {
      const frame = h('div', { class: 'pane', 'data-slot': id }, multi ? h('div', { class: 'ph' }) : null, h('div', { class: 'banner-slot' }), h('div', { class: 'tbody' }));
      $('.tbody', frame).append(getTerm(id).el);
      const handle = multi && paneResizer(stage, view, index);
      if (handle) frame.append(handle);
      const rowHandle = multi && view.layout === 'grid' && paneResizer(stage, view, index, true);
      if (rowHandle) frame.append(rowHandle);
      return frame;
    }));
    requestAnimationFrame(() => {
      for (const id of view.slots) terms.get(id)?.refit();
      const f = terms.get(view.focus || view.slots[0]);
      if (f && (hadFocus || !$('.scrim'))) f.focus();
    });
  }
  if (resizingView !== view.id) applyViewWeights(stage, view);
  for (const frame of stage.querySelectorAll('.pane')) {
    const id = frame.dataset.slot, s = slotOf(id);
    frame.classList.toggle('focus', multi && view.focus === id);
    const head = $('.ph', frame);
    if (head) {
      head.replaceChildren(dotFor(s), h('b', {}, s ? s.name : '…'), s && s.kind === 'claude' && s.alias ? h('span', { class: 'al' }, s.alias) : null,
        h('span', { class: 'acts' },
          s && s.kind === 'claude' && !s.dead ? (s.status === 'sleeping'
            ? h('button', { title: 'Reanudar', onclick: () => act('slot.wake', { slot: id }) }, '▶')
            : h('button', { title: 'Suspender', onclick: () => act('slot.suspend', { slot: id }) }, '⏾')) : null,
          h('button', { title: 'Separar de la vista', onclick: () => act('slot.split', { slot: id }) }, '⇱'),
          h('button', { title: 'Acciones', onclick: ev => { const r = ev.currentTarget.getBoundingClientRect(); openCtx('slot', id, r.right - 230, r.bottom + 4); } }, '⋯'),
          h('button', { class: 'danger', title: 'Cerrar sesión', onclick: () => confirmCloseSlot(id) }, '✕')));
      head.oncontextmenu = ev => { ev.preventDefault(); openCtx('slot', id, ev.clientX, ev.clientY); };
      head.onclick = ev => { if (!ev.target.closest('button')) focusSlot(id); };
    }
    // A session that is not running: a strip on top with what to do, the terminal dimmed below.
    const slot = $('.banner-slot', frame), host = terms.get(id)?.el;
    const state = !s ? '' : s.dead ? 'dead' : s.status === 'sleeping' ? 'sleep' : '';
    if (slot.dataset.state !== state) {
      slot.dataset.state = state;
      slot.replaceChildren();
      if (state === 'sleep') slot.append(h('div', { class: 'banner' }, h('span', { class: 'd sleeping' }),
        h('span', {}, h('b', {}, s.name), ' está suspendida: no corre Claude.'),
        h('button', { class: 'btn primary', onclick: () => act('slot.wake', { slot: id }) }, 'Reanudar')));
      if (state === 'dead') slot.append(h('div', { class: 'banner err' }, h('span', { class: 'd dead' }),
        h('span', {}, h('b', {}, s.name), ' terminó con error (código ' + (s.deadStatus || '?') + ').'),
        h('button', { class: 'btn', onclick: () => act('slot.close', { slot: id }) }, 'Cerrar'),
        h('button', { class: 'btn primary', onclick: () => act('slot.respawn', { slot: id }) }, 'Reabrir')));
      if (host) host.classList.toggle('faded', state !== '');
    }
  }
}

function emptyTab(tab) {
  if (!tab) return h('div');
  const list = h('div', { class: 'list' });
  const big = (ic, label, sub, run) => h('div', { class: 'big', onclick: run }, h('b', {}, ic), h('span', {}, label), h('small', {}, sub));
  const card = h('div', { class: 'ec' },
    h('h3', {}, tab.title), h('p', {}, tab.short),
    h('div', { class: 'big3' },
      big('✦', 'Claude', 'Ctrl+T', () => dlgNewSession(tab.id, 'claude')),
      big('❯', 'Terminal', 'en esta carpeta', () => dlgNewSession(tab.id, 'shell')),
      big('↻', 'Retomar', 'por su ID', () => dlgNewSession(tab.id, 'resume'))),
    h('div', { class: 'grp' }, h('span', {}, 'Recientes en esta carpeta')), list);
  api.get('recent', { ws: S.ws, tab: tab.id }).then(rows => {
    if (!rows.length) { list.append(h('div', { class: 'hint' }, 'No hay conversaciones anteriores en esta carpeta.')); return; }
    for (const c of rows) list.append(h('div', { class: 'row-item', onclick: () => dlgNewSession(tab.id, 'resume', c) },
      h('b', {}, c.title || shorten(c.first, 60)), h('span', { class: 'w' }, c.when + (c.alias && c.alias !== 'claude' ? ' · ' + c.alias : '')),
      h('span', { class: 'q' }, c.title ? '«' + c.first + '»' : c.id.slice(0, 8))));
  }).catch(() => {});
  return h('div', { class: 'empty' }, card);
}

function renderNotOpen(root) {
  stageKey = '';
  const st = S.state;
  const inTerm = st.mode === 'terminal';
  root.replaceChildren(h('div', { class: 'empty' }, h('div', { class: 'ec' },
    h('h3', {}, st.title),
    h('p', { class: 'txt' }, inTerm ? 'Este workspace está abierto en la versión de terminal. Para abrirlo aquí se guarda y se reinicia: sus sesiones retoman sus conversaciones y las que estaban trabajando reciben el mensaje de continuación.'
      : 'Este workspace está apagado.'),
    h('div', { class: 'btnrow' },
      h('button', { class: 'btn', onclick: () => go(null) }, '← Workspaces'),
      h('button', { class: 'btn primary', onclick: () => openWorkspace(st.ws, inTerm) }, inTerm ? 'Abrir aquí' : 'Abrir')))));
}

// ---------- home ----------
function renderHome(root) {
  document.title = 'Claude Workspaces';
  stageKey = '';
  const launch = S.launchDir || S.info?.launchDir || '';
  root.replaceChildren(h('div', { class: 'home' },
    h('div', { class: 'top' }, h('img', { class: 'logo', src: 'icon.svg', alt: '' }), h('span', { class: 'brand' }, 'Claude Workspaces'), h('span', { class: 'sp' }),
      h('span', { class: 'ib', onclick: () => dlgUsage() }, '◔ Uso de Claude'),
      h('span', { class: 'ib', onclick: () => dlgSettings('general') }, '⚙ Ajustes')),
    h('h3', {}, 'Workspaces'),
    h('p', { class: 'lead' }, 'Retoma uno o crea uno nuevo.'),
    h('div', { class: 'cards' },
      S.workspaces.map(w => {
        const dot = w.state === 'app' ? 'idle' : w.state === 'terminal' ? 'waiting' : 'sleeping';
        const stateTxt = w.state === 'app' ? 'abierto' : w.state === 'terminal' ? 'en la terminal' : 'apagado';
        const meta = [plural(w.tabs, 'pestaña', 'pestañas'), plural(w.sessions, 'sesión', 'sesiones')];
        if (w.state !== 'app' && w.when) meta.push(w.when);
        return h('div', { class: 'card', onclick: () => openWorkspace(w.name, false, w.state),
          oncontextmenu: ev => { ev.preventDefault(); cardMenu(w, ev.clientX, ev.clientY); } },
          h('div', { class: 'ch' }, h('b', {}, w.title), h('span', { class: 'st ' + w.state }, h('span', { class: 'd ' + dot }), stateTxt)),
          h('div', { class: 'cm' }, meta.join(' · '),
            w.working ? h('span', { class: 'busyc' }, ' · ' + w.working + (w.state === 'stopped' ? ' trabajaban al apagar' : ' trabajando')) : null),
          h('div', { class: 'ct' }, (w.tabNames || []).slice(0, 5).join(' · ') + ((w.tabNames || []).length > 5 ? ' · +' + (w.tabNames.length - 5) : '')),
          h('button', { class: 'more', title: 'Acciones', onclick: ev => { ev.stopPropagation(); cardMenu(w, ev.clientX, ev.clientY); } }, '⋯'));
      }),
      h('div', { class: 'card new', onclick: () => dlgNewWorkspace(launch) }, h('span', {}, '+ Nuevo workspace'))),
  ));
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
    if (st.notice && was && was.notice !== st.notice) { try { toast(JSON.parse(st.notice).text); } catch (e) {} }
    if (was && was.mode === 'app' && st.mode !== 'app') { go(null); return; }
    render();
  });
  es.addEventListener('workspaces', ev => { S.workspaces = JSON.parse(ev.data); if (!S.ws) render(); });
}

// ---------- context menus ----------
// A short menu; items with `sub` open a submenu at their side. Choosing an action closes it.
let ctx = null; // {kind, id, x, y, el, subEl}
function openCtx(kind, id, x, y) { closeCtx(); ctx = { kind, id, x, y }; renderCtx(); }
function closeCtx() { if (!ctx) return false; ctx.el?.remove(); ctx.subEl?.remove(); ctx = null; return true; }
document.addEventListener('mousedown', ev => { if (ctx && !ev.target.closest('.menu') && !ev.target.closest('.scrim')) closeCtx(); }, true);
function placeMenu(el, x, y) {
  const r = el.getBoundingClientRect();
  el.style.left = Math.max(6, Math.min(x, innerWidth - r.width - 6)) + 'px';
  el.style.top = Math.max(6, Math.min(y, innerHeight - r.height - 6)) + 'px';
}
function menuEl(items, onPick) {
  const el = h('div', { class: 'menu' });
  for (const it of items) {
    if (it === '-') { el.append(h('div', { class: 'sep' })); continue; }
    if (it.head) { el.append(h('div', { class: 'mh' }, h('span', {}, it.head))); continue; }
    const row = h('div', { class: 'it' + (it.danger ? ' dg' : '') + (it.on ? ' on' : '') + (it.sub ? ' has' : '') },
      h('span', { class: 'lb' }, it.label), it.k ? h('span', { class: 'k' }, it.k) : null, it.sub ? h('span', { class: 'k' }, '›') : null);
    row.addEventListener('mouseenter', () => onPick(it, row, true));
    row.addEventListener('click', () => onPick(it, row, false));
    el.append(row);
  }
  return el;
}
function renderCtx() {
  if (!ctx || !S.state || ctx.kind === 'menu') return;
  const items = ctx.kind === 'tab' ? tabItems(ctx.id) : slotItems(ctx.id);
  if (!items) { closeCtx(); return; }
  showMenu(items, ctx.x, ctx.y);
}
function showMenu(items, x, y) {
  const keep = ctx;
  keep.el?.remove(); keep.subEl?.remove(); keep.subEl = null;
  const pick = async (it, row, hover) => {
    if (it.sub) {
      if (keep.subFor === it.label && keep.subEl && hover) return;
      keep.subEl?.remove(); keep.subFor = it.label;
      keep.el.querySelectorAll('.it.open').forEach(r => r.classList.remove('open'));
      row.classList.add('open');
      const sub = menuEl(it.sub(), async (s2, _r, hov) => { if (hov || !s2.run) return; closeCtx(); try { await s2.run(); } catch (e) {} });
      sub.classList.add('submenu');
      document.body.append(sub);
      keep.subEl = sub;
      const r = row.getBoundingClientRect();
      const left = r.right + 4 + sub.offsetWidth > innerWidth ? r.left - sub.offsetWidth - 4 : r.right + 4;
      placeMenu(sub, left, r.top - 5);
      return;
    }
    if (hover) { keep.subEl?.remove(); keep.subEl = null; keep.subFor = null; keep.el.querySelectorAll('.it.open').forEach(r => r.classList.remove('open')); return; }
    if (!it.run) return;
    closeCtx();
    try { await it.run(); } catch (e) {}
  };
  const el = menuEl(items, pick);
  keep.el = el;
  document.body.append(el);
  placeMenu(el, x, y);
}
function popMenu(items, x, y) { closeCtx(); ctx = { kind: 'menu' }; showMenu(items, x, y); }

function tabItems(id) {
  const st = S.state, i = st.tabs.findIndex(t => t.id === id), t = st.tabs[i];
  if (!t) return null;
  const claude = t.views.flatMap(v => v.slots).map(slotOf).filter(s => s && s.kind === 'claude' && !s.dead);
  const running = claude.filter(s => s.status !== 'sleeping').length;
  return [{ head: t.title },
    { label: 'Nueva sesión aquí', k: 'Ctrl T', run: () => dlgNewSession(id, 'claude') },
    { label: 'Renombrar', run: () => dlgPrompt('Renombrar pestaña', 'Nombre', t.title, v => act('tab.rename', { tab: id, title: v })) },
    { label: 'Cambiar carpeta…', k: t.short, run: () => dlgDir('Carpeta de la pestaña ' + t.title, 'Las sesiones nuevas de la pestaña empiezan aquí.', t.short, v => act('tab.dir', { tab: id, dir: v })) },
    running ? { label: 'Suspender la pestaña', k: running + ' de Claude', run: () => act('tab.suspend', { tab: id }) }
      : claude.length ? { label: 'Reanudar la pestaña', run: () => act('tab.wake', { tab: id }) } : null,
    claude.length ? { label: 'Alias de Claude de la pestaña…', run: () => dlgBulkAlias('tab', id) } : null,
    { label: 'Mover', sub: () => [i > 0 && { label: 'Arriba', run: () => act('tab.move', { tab: id, to: i - 1 }) },
      i < st.tabs.length - 1 && { label: 'Abajo', run: () => act('tab.move', { tab: id, to: i + 1 }) }].filter(Boolean) },
    '-',
    { label: 'Cerrar pestaña', danger: true, run: () => confirmCloseTab(id) },
  ].filter(Boolean);
}
function slotItems(id) {
  const s = slotOf(id), { tab, view } = findSlot(id);
  if (!s || !view) return null;
  const vi = tab.views.indexOf(view), multi = view.slots.length > 1;
  const others = tab.views.filter(v => v.id !== view.id);
  const items = [{ head: s.name + (multi ? '  ·  vista de ' + view.slots.length : '') },
    { label: 'Renombrar', run: () => dlgPrompt('Renombrar sesión', 'Nombre', s.name, v => act('slot.rename', { slot: id, name: v })) }];
  if (s.kind === 'claude') {
    items.push({ label: 'Editar sesión…', run: () => dlgEditSession(id) });
    items.push({ label: 'Cambiar alias', sub: () => S.aliases.map(a => ({ label: a.name, k: a.name === s.alias ? '✓' : '', run: () => a.name === s.alias ? null : act('alias.bulk', { scope: 'slot', slot: id, alias: a.name, sendContinue: true }) })) });
    if (s.status === 'sleeping') items.push({ label: 'Reanudar', run: () => act('slot.wake', { slot: id }) });
    else if (!s.dead) items.push({ label: 'Suspender', k: 'deja de correr', run: () => act('slot.suspend', { slot: id }) });
    items.push({ label: 'Reiniciar', run: () => act('slot.respawn', { slot: id }) });
  }
  items.push('-');
  if (others.length) items.push({ label: 'Ver junto a', sub: () => others.map(v => ({ label: viewNames(v), run: () => act('view.join', { view: v.id, target: view.id }) })) });
  if (multi) {
    items.push({ label: 'Distribución', sub: () => [['cols', 'Columnas'], ['rows', 'Filas'], ['grid', 'Cuadrícula']].map(([l, n]) => ({ label: n, k: view.layout === l ? '✓' : '', run: () => act('view.layout', { view: view.id, layout: l }) })) });
    items.push({ label: 'Separar de la vista', run: () => act('slot.split', { slot: id }) });
    if (view.slots.some(x => slotOf(x)?.kind === 'claude')) items.push({ label: 'Alias de Claude de la vista…', run: () => dlgBulkAlias('view', view.id) });
  }
  items.push({ label: 'Mover', sub: () => [
    vi > 0 && { label: 'Arriba en la pestaña', run: () => act('view.move', { view: view.id, to: vi - 1 }) },
    vi < tab.views.length - 1 && { label: 'Abajo en la pestaña', run: () => act('view.move', { view: view.id, to: vi + 1 }) },
    ...S.state.tabs.filter(t => t.id !== tab.id).map(t => ({ label: 'A la pestaña ' + t.title, run: () => act('view.totab', { view: view.id, tab: t.id }) })),
  ].filter(Boolean) });
  items.push('-');
  if (multi) items.push({ label: 'Cerrar la vista', danger: true, run: () => confirmCloseView(view.id) });
  items.push({ label: 'Cerrar sesión', danger: true, k: S.config?.browser_keys === false ? 'Alt W' : 'Ctrl W', run: () => confirmCloseSlot(id) });
  return items;
}
function cardMenu(w, x, y) {
  const items = [{ head: w.title }, { label: w.state === 'terminal' ? 'Abrir aquí' : 'Abrir', run: () => openWorkspace(w.name, false, w.state) },
    { label: 'Renombrar', run: () => dlgPrompt('Renombrar workspace', 'Nombre', w.title, v => api.do('ws.rename', { ws: w.name, title: v }).then(refreshHome)) }, '-'];
  if (w.state !== 'stopped') items.push({ label: 'Apagar y guardar', danger: true, run: () => dlgConfirm('¿Apagar ' + w.title + '?', 'Se guarda el estado y se cierran todas sus sesiones. Lo retomas desde aquí cuando quieras.', 'Apagar y guardar', () => api.do('ws.stop', { ws: w.name }).then(refreshHome)) });
  else items.push({ label: 'Borrar', danger: true, run: () => dlgConfirm('¿Borrar ' + w.title + '?', 'Se borra su estado guardado. Las conversaciones de Claude quedan en disco.', 'Borrar', () => api.do('ws.delete', { ws: w.name }).then(refreshHome)) });
  popMenu(items, x, y);
}
function wsMenu(x, y) {
  const others = S.workspaces.filter(w => w.name !== S.ws && w.state === 'app');
  popMenu([
    { label: 'Todos los workspaces', run: () => go(null) },
    { label: 'Renombrar…', run: () => dlgPrompt('Renombrar workspace', 'Nombre', S.state.title, v => act('ws.rename', { title: v })) },
    { label: 'Carpeta por defecto…', k: S.state.dirShort, run: () => dlgDir('Carpeta por defecto del workspace', 'Las pestañas nuevas empiezan aquí.', S.state.dirShort, v => act('ws.dir', { dir: v })) },
    { label: 'Guardar ahora', run: () => act('ws.save').then(() => toast('Estado guardado')) },
    { label: 'Reabrir sesión cerrada', k: 'Ctrl ⇧ T', run: () => act('slot.reopen', { tab: curTab()?.id, ...stageSize() }) },
    { label: 'Alias de Claude de todo…', run: () => dlgBulkAlias('all', '') },
    { label: 'Uso de Claude…', run: () => dlgUsage() },
    { label: 'Recursos…', run: () => dlgResources() },
    { label: '✦ ' + skyName(), k: skyOn() ? 'Alt K' : 'apagado', run: () => skyToggle() },
    ...(others.length ? ['-', ...others.map(w => ({ label: w.title, k: 'abierto', run: () => go(w.name) }))] : []),
    '-',
    { label: 'Ajustes', k: 'Alt ,', run: () => dlgSettings('general') },
    { label: 'Apagar y guardar', danger: true, run: () => confirmStop() },
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
  applyTheme(S.config.theme, S.config.app_mode);
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

function dlgDir(title, sub, value, run) {
  const dir = dirInput(value);
  modal({ title, sub, body: field('Carpeta', dir), buttons: [{ label: 'Cancelar' }, { label: 'Guardar', primary: true, run: async () => {
    const v = dir.input.value.trim(); if (!v) throw new Error('Escribe una carpeta.'); await run(v);
  } }] });
}

function dlgNewTab() {
  const tab = curTab();
  let start = 'claude', alias = S.defaultAlias;
  const name = h('input', { class: 'input', placeholder: 'p. ej. bug-185' });
  const dir = dirInput(S.state.dirShort || (tab ? tab.short : '~'));
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

// A new workspace, in a folder or around an existing conversation (pasting its ID).
function dlgNewWorkspace(dirValue, startMode) {
  let mode = startMode || 'dir', found = null;
  const dir = dirInput(dirValue ? (S.info && dirValue.startsWith(S.info.home) ? '~' + dirValue.slice(S.info.home.length) : dirValue) : '~');
  const name = h('input', { class: 'input', placeholder: 'como la carpeta' });
  const idIn = h('input', { class: 'input', placeholder: 'ID de la conversación (completo o sus primeros caracteres)' });
  const list = h('div', { class: 'list' });
  idIn.addEventListener('input', () => { clearTimeout(idIn._t); idIn._t = setTimeout(async () => {
    const q = idIn.value.trim(); found = null;
    if (q.length < 6) { list.replaceChildren(h('div', { class: 'hint' }, 'Escribe al menos 6 caracteres.')); return; }
    const rows = await api.get('conversations', { q }).catch(() => []);
    if (!rows.length) { list.replaceChildren(h('div', { class: 'hint err' }, 'No encontré ninguna conversación con ese ID.')); return; }
    found = rows[0];
    list.replaceChildren(...rows.map((c, i) => h('div', { class: 'row-item' + (i === 0 ? ' sel' : ''), onclick: ev => {
      found = c; [...list.children].forEach(el => el.classList.remove('sel')); ev.currentTarget.classList.add('sel'); } },
      h('b', {}, c.title || shorten(c.first, 50)), h('span', { class: 'w' }, c.when + ' · ' + c.short), h('span', { class: 'q' }, '«' + c.first + '»'))));
  }, 180); });
  const body = h('div', { class: 'stack' });
  const draw = () => {
    body.replaceChildren(seg([{ value: 'dir', label: 'En una carpeta' }, { value: 'id', label: 'Desde una conversación' }], mode, v => { mode = v; draw(); }));
    if (mode === 'dir') body.append(field('Carpeta', dir), field('Nombre', name));
    else body.append(field('Conversación', idIn), list, h('div', { class: 'hint' }, 'Se crea en la carpeta donde empezó la conversación, con ella retomada.'));
    setTimeout(() => (mode === 'dir' ? dir.input : idIn).focus(), 10);
  };
  draw();
  modal({ title: 'Nuevo workspace', body, buttons: [{ label: 'Cancelar' }, { label: 'Crear workspace', primary: true, run: async () => {
    if (mode === 'dir') { const r = await api.do('ws.new', { dir: dir.input.value.trim(), title: name.value.trim() }); go(r.ws); return; }
    if (!found) throw new Error('Primero escribe un ID que exista.');
    const r = await api.do('ws.fromid', { resume: found.id, alias: found.alias });
    go(r.ws);
  } }] });
}
function dlgFromID() { dlgNewWorkspace('', 'id'); }

// ---------- settings ----------
async function dlgSettings(section) {
  await loadConfig().catch(e => toast(e.message, true));
  const scrim = h('div', { class: 'scrim' });
  const close = () => { scrim.remove(); const v = curView(); if (v?.focus) terms.get(v.focus)?.focus(); };
  scrim._close = close;
  scrim.addEventListener('mousedown', ev => { if (ev.target === scrim) close(); });
  const nav = h('nav'), sec = h('section');
  const sections = [['general', 'General'], ['aliases', 'Alias de Claude'], ['skynet', 'Skynet'], ['look', 'Apariencia'], ['keys', 'Atajos']];
  const set = async patch => {
    try { await api.do('config.set', { config: patch }); await loadConfig(); } catch (e) { toast(e.message, true); }
    if ('skynet_enabled' in patch || 'skynet_voice' in patch) { skyConnect(); window.skyVoice?.sync(); render(); }
  };
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
        h('div', { class: 'setrow' }, h('div', {}, 'Cambiar de alias sin cuota', h('small', {}, 'Si una cuenta de Claude llega al límite (5 horas o semanal), sus sesiones detenidas pasan a un alias de otra cuenta con cuota y continúan.')),
          h('button', { class: 'toggle' + (c.auto_switch_alias !== false ? ' on' : ''), onclick: () => set({ auto_switch_alias: c.auto_switch_alias === false }).then(draw) }, h('i'))),
        h('div', { class: 'setrow' }, h('div', {}, 'Atajos de navegador', h('small', {}, 'Ctrl+T, Ctrl+W, Ctrl+Tab… actúan sobre las sesiones de la pestaña. Apágalos si los quieres para Claude (Ctrl+W borra una palabra).')),
          h('button', { class: 'toggle' + (c.browser_keys !== false ? ' on' : ''), onclick: () => set({ browser_keys: c.browser_keys === false }).then(draw) }, h('i'))),
        h('div', { class: 'setrow' }, h('div', {}, 'Historial de consumo', h('small', {}, 'Lectura local en segundo plano; intervalo en segundos.')),
          h('input', { class: 'input', type: 'number', min: '60', max: '86400', value: c.usage_interval_seconds || 3600, style: { width: '110px' }, onchange: ev => set({ usage_interval_seconds: Number(ev.target.value) }) })),
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
          h('button', { class: 'btn', onclick: () => dlgAlias(a, draw) }, 'Editar'),
          S.aliases.length > 1 ? h('button', { class: 'btn', onclick: () => dlgConfirm('¿Borrar el alias ' + a.name + '?', 'Las sesiones que lo usan pasan al alias por defecto la próxima vez que se abran.', 'Borrar', async () => { await api.do('alias.delete', { name: a.name }); await loadConfig(); draw(); }) }, '✕') : null)),
        h('button', { class: 'btn primary', style: { marginTop: '6px' }, onclick: () => dlgAlias(null, draw) }, '＋ Agregar alias'),
        S.shellAliases.length ? h('div', { style: { marginTop: '22px' } },
          h('div', { class: 'hint', style: { marginBottom: '8px' } }, 'Detectados en tu shell — clic para agregarlos:'),
          h('div', { style: { display: 'flex', gap: '6px', flexWrap: 'wrap' } }, S.shellAliases.map(a => h('button', { class: 'chip', style: { cursor: 'pointer' }, title: a.summary,
            onclick: async () => { await api.do('alias.import', { name: a.name }).catch(e => toast(e.message, true)); await loadConfig(); draw(); } }, '＋ ' + a.name)))) : null);
    } else if (section === 'skynet') {
      const toggle = (key, on, invert) => h('button', { class: 'toggle' + (on ? ' on' : ''), onclick: () => set({ [key]: invert ? on === false : !on }).then(draw) }, h('i'));
      const name = h('input', { class: 'input', style: { width: '180px' }, value: c.skynet_name || '', placeholder: 'Skynet', onchange: ev => set({ skynet_name: ev.target.value }) });
      const alias = h('select', { class: 'input', style: { width: '200px' }, onchange: ev => set({ skynet_alias: ev.target.value }) },
        h('option', { value: '' }, 'Automático'), ...S.aliases.map(a => h('option', { value: a.name, selected: c.skynet_alias === a.name }, a.name)));
      sec.replaceChildren(h('h3', {}, 'Skynet'),
        h('p', { class: 'hint', style: { marginTop: 0, marginBottom: '6px' } }, 'Un orquestador: una sesión de Claude que ve todas tus sesiones y actúa en ellas cuando se lo pides, por texto o por voz. No conoce el código de tus proyectos.'),
        h('div', { class: 'setrow' }, h('div', {}, 'Encendido', h('small', {}, 'Ábrelo con Alt+K o con ✦ en la barra lateral.')), toggle('skynet_enabled', !!c.skynet_enabled)),
        h('div', { class: 'setrow' }, h('div', {}, 'Nombre', h('small', {}, 'Con él lo llamas por voz: «' + (c.skynet_name || 'Skynet') + ', ¿cómo van las sesiones?»')), name),
        h('div', { class: 'setrow' }, h('div', {}, 'Modelo'), seg([{ value: 'sonnet', label: 'Sonnet' }, { value: 'haiku', label: 'Haiku' }, { value: 'opus', label: 'Opus' }], c.skynet_model || 'sonnet', v => set({ skynet_model: v }).then(draw))),
        h('div', { class: 'setrow' }, h('div', {}, 'Alias', h('small', {}, 'Su consumo sale de esa cuenta. En automático usa una con cuota y cambia a otra cuando se acaba.')), alias),
        h('div', { class: 'setrow' }, h('div', {}, 'Avisar cuando una sesión pregunta', h('small', {}, 'Te muestra la pregunta con sus opciones y la respondes desde aquí.')), toggle('skynet_notify', c.skynet_notify !== false, true)),
        h('div', { class: 'setrow' }, h('div', {}, 'Escuchar el micrófono', h('small', {}, 'Di su nombre y luego la instrucción. La voz se transcribe en este equipo (Whisper); la primera vez descarga el modelo.')), toggle('skynet_voice', !!c.skynet_voice)),
        h('div', { class: 'setrow' }, h('div', {}, 'Aprobar permisos sin preguntarme', h('small', {}, 'Apagado, te pregunta antes de autorizar cualquier permiso de una sesión.')), toggle('skynet_auto_approve', !!c.skynet_auto_approve)));
    } else if (section === 'look') {
      const fs = c.app_font_size || 13.5;
      sec.replaceChildren(h('h3', {}, 'Apariencia'),
        h('div', { class: 'setrow' }, h('div', {}, 'Modo', h('small', {}, 'Oscuro o claro; las terminales cambian con él.')),
          seg([{ value: 'dark', label: 'Oscuro' }, { value: 'light', label: 'Claro' }], c.app_mode === 'light' ? 'light' : 'dark', v => set({ app_mode: v }).then(draw))),
        h('div', { class: 'setrow' }, h('div', {}, 'Color de acento'), h('div', { class: 'swatches' }, Object.entries(THEMES).map(([n, [col]]) =>
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
        k('Ctrl+T', 'nueva sesión de Claude en la pestaña'), k('Ctrl+W', 'cerrar la sesión'), k('Ctrl+Shift+T', 'reabrir la última sesión cerrada'),
        k('Ctrl+Tab Ctrl+PgDn', 'sesión siguiente'), k('Ctrl+Shift+Tab Ctrl+PgUp', 'sesión anterior'), k('Ctrl+Shift+PgUp/PgDn', 'mover la sesión'),
        k('Ctrl+1…8 Ctrl+9', 'ir a la sesión N / a la última'),
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

// ---------- usage and resources ----------
const fmtTok = n => n >= 1e9 ? (n / 1e9).toFixed(1).replace(/\.0$/, '') + ' mil M' : n >= 1e6 ? (n / 1e6).toFixed(1).replace(/\.0$/, '') + ' M' : n >= 1e3 ? Math.round(n / 1e3) + ' k' : String(n);
const fmtMB = mb => mb >= 1024 ? (mb / 1024).toFixed(1) + ' GB' : Math.round(mb) + ' MB';
function fmtReset(iso) {
  if (!iso || iso.startsWith('0001')) return '';
  const t = new Date(iso), d = t - Date.now();
  const inTxt = d <= 0 ? 'ya' : d < 3600e3 ? 'en ' + Math.ceil(d / 60e3) + ' min' : d < 86400e3 ? 'en ' + Math.floor(d / 3600e3) + ' h ' + Math.floor(d / 60e3) % 60 + ' min' : 'en ' + Math.floor(d / 86400e3) + (Math.floor(d / 86400e3) === 1 ? ' día' : ' días');
  const hm = t.toLocaleTimeString('es-MX', { hour: '2-digit', minute: '2-digit', hour12: false });
  const same = t.toDateString() === new Date().toDateString();
  return same ? 'a las ' + hm + ' (' + inTxt + ')' : t.toLocaleDateString('es-MX', { weekday: 'short', day: 'numeric', month: 'short' }) + ', ' + hm + ' (' + inTxt + ')';
}
function usageBar(label, w) {
  if (!w) return null;
  const pct = Math.round(Math.max(0, Math.min(100, w.pct))), lvl = pct >= 90 ? 'hi' : pct >= 70 ? 'mid' : '';
  return h('div', { class: 'urow' }, h('span', { class: 'ul' }, label),
    h('div', { class: 'ubar ' + lvl }, h('i', { style: { width: Math.min(100, pct) + '%' } })),
    h('b', { title: pct + ' % utilizado' }, (100 - pct) + ' % libre'), h('span', { class: 'ur' }, 'se reinicia ' + fmtReset(w.resetsAt)));
}
function usageHistoryPanel(history) {
  const chart = h('div', { class: 'stack' });
  const days = h('select', { class: 'input', 'aria-label': 'Periodo' }, [1, 7, 30].map(n => h('option', { value: n, selected: n === 7 }, n + (n === 1 ? ' día' : ' días'))));
  const alias = h('select', { class: 'input', 'aria-label': 'Alias' }, h('option', { value: '' }, 'Todos los alias'),
    [...new Set(history.points.map(p => p.alias))].sort().map(a => h('option', { value: a }, a)));
  const metrics = [['total', 'Tokens totales'], ['in', 'Entrada'], ['out', 'Salida'], ['cacheW', 'Caché escritura'], ['cacheR', 'Caché lectura']];
  const metric = h('select', { class: 'input', 'aria-label': 'Tokens' }, metrics.map(([key, label]) => h('option', { value: key }, label)));
  const value = p => metric.value === 'total' ? Object.values(p.tokens).reduce((a, b) => a + b, 0) : p.tokens[metric.value];
  const draw = () => {
    const end = Math.floor(Date.now() / 3600000) * 3600, start = end - (Number(days.value) * 24 - 1) * 3600;
    const groups = new Map();
    for (const p of history.points) {
      if (p.hour < start || p.hour > end || (alias.value && p.alias !== alias.value)) continue;
      const key = JSON.stringify([p.alias, p.model]);
      if (!groups.has(key)) groups.set(key, []);
      groups.get(key).push(p);
    }
    chart.replaceChildren();
    if (!groups.size) chart.append(h('div', { class: 'hint' }, 'Sin consumo registrado en este periodo.'));
    for (const [key, points] of groups) {
      const [name, model] = JSON.parse(key), hours = new Map();
      for (const p of points) hours.set(p.hour, (hours.get(p.hour) || 0) + value(p));
      const series = [];
      for (let hour = start; hour <= end; hour += 3600) series.push([hour, hours.get(hour) || 0]);
      const peak = Math.max(1, ...series.map(p => p[1]));
      const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
      svg.setAttribute('viewBox', '0 0 640 180');
      svg.setAttribute('role', 'img');
      svg.setAttribute('aria-label', name + ' · ' + model + ' · ' + metric.selectedOptions[0].text);
      const add = (tag, attrs) => {
        const el = document.createElementNS(svg.namespaceURI, tag);
        for (const [k, v] of Object.entries(attrs)) el.setAttribute(k, v);
        svg.append(el); return el;
      };
      const x = i => 52 + i / Math.max(1, series.length - 1) * 576;
      const y = n => 144 - n / peak * 120;
      for (const n of [0, peak / 2, peak]) {
        add('line', { x1: 52, x2: 628, y1: y(n), y2: y(n), class: 'history-axis' });
        add('text', { x: 46, y: y(n) + 4, 'text-anchor': 'end' }).textContent = fmtTok(Math.round(n));
      }
      add('polyline', { points: series.map((p, i) => x(i) + ',' + y(p[1])).join(' '), class: 'history-line', fill: 'none' });
      const date = hour => new Date(hour * 1000).toLocaleString('es-MX', { month: 'short', day: 'numeric', hour: '2-digit' });
      add('text', { x: 52, y: 172 }).textContent = date(start);
      add('text', { x: 628, y: 172, 'text-anchor': 'end' }).textContent = date(end);
      const crosshair = add('line', { x1: 52, x2: 52, y1: 20, y2: 144, class: 'history-crosshair', visibility: 'hidden' });
      const tip = h('div', { class: 'hint', role: 'status' }, 'Tokens/hora · máximo ' + fmtTok(peak));
      const show = i => {
        const p = series[Math.max(0, Math.min(series.length - 1, i))];
        crosshair.setAttribute('visibility', 'visible');
        crosshair.setAttribute('x1', x(i)); crosshair.setAttribute('x2', x(i));
        tip.textContent = p[1].toLocaleString('es-MX') + ' tokens · ' + date(p[0]);
      };
      let selected = 0;
      svg.setAttribute('tabindex', '0');
      svg.addEventListener('pointermove', ev => {
        const r = svg.getBoundingClientRect();
        selected = Math.max(0, Math.min(series.length - 1, Math.round(((ev.clientX - r.left) / r.width * 640 - 52) / 576 * (series.length - 1))));
        show(selected);
      });
      svg.addEventListener('focus', () => show(selected));
      svg.addEventListener('keydown', ev => {
        if (!['ArrowLeft', 'ArrowRight'].includes(ev.key)) return;
        ev.preventDefault(); ev.stopPropagation();
        selected = Math.max(0, Math.min(series.length - 1, selected + (ev.key === 'ArrowRight' ? 1 : -1))); show(selected);
      });
      const table = h('table', {}, h('thead', {}, h('tr', {}, ['Hora', 'Entrada', 'Salida', 'Caché escritura', 'Caché lectura'].map(label => h('th', {}, label)))),
        h('tbody', {}, points.map(p => h('tr', {}, h('td', {}, date(p.hour)), ['in', 'out', 'cacheW', 'cacheR'].map(k => h('td', {}, p.tokens[k].toLocaleString('es-MX')))))));
      chart.append(h('div', { class: 'ucard history-chart' }, h('b', {}, name + ' · ' + model),
        h('span', { class: 'ul' }, metric.selectedOptions[0].text + ' por hora'), svg, tip,
        h('details', {}, h('summary', {}, 'Ver datos exactos'), h('div', { class: 'history-table' }, table))));
    }
  };
  for (const control of [days, alias, metric]) control.addEventListener('change', draw);
  draw();
  return h('section', { class: 'stack' }, h('h3', {}, 'Historial por alias y modelo'),
    h('div', { class: 'history-filters' }, days, alias, metric),
    h('div', { class: 'hint' }, history.at.startsWith('0001') ? 'Primera lectura pendiente.' : 'Actualizado: ' + new Date(history.at).toLocaleString('es-MX')),
    h('div', { class: 'hint' }, 'Las carpetas compartidas sin atribución previa aparecen como «Sin atribuir»; no se duplican por alias.'), chart);
}
async function dlgUsage() {
  const body = h('div', { class: 'stack' }, h('div', { class: 'hint' }, 'Consultando el uso de cada cuenta…'));
  const m = modal({ title: 'Uso de Claude', sub: 'Por cuenta: la ventana de 5 horas, la semanal y los tokens gastados.', width: '640px', body,
    buttons: [{ label: 'Actualizar', run: async () => { await fill(true); return true; } }, { label: 'Cerrar', primary: true }] });
  async function fill(fresh) {
    let rows;
    try { rows = await api.get('usage', { fresh: fresh ? '1' : null }); } catch (e) { body.replaceChildren(h('div', { class: 'hint err' }, e.message)); return; }
    body.replaceChildren(...rows.map(a => {
      const card = h('div', { class: 'ucard' }, h('div', { class: 'uh' }, h('b', {}, a.aliases.join(' · ')), h('span', {}, [a.plan, a.short].filter(Boolean).join(' · '))));
      if (a.external) { card.append(h('div', { class: 'hint' }, 'Proveedor externo (' + a.external + '): sin límites de Claude.')); return card; }
      if (a.error) card.append(h('div', { class: 'hint warn' }, a.error));
      [usageBar('Sesión (5 h)', a.session), usageBar('Semana', a.week), usageBar('Semana · Opus', a.weekOpus), usageBar('Semana · Sonnet', a.weekSonnet)].forEach(x => x && card.append(x));
      const tok = (label, t) => h('div', { class: 'utok' }, h('span', { class: 'ul' }, label), h('b', {}, fmtTok(t.in + t.out + t.cacheW + t.cacheR)),
        h('span', { class: 'ur' }, 'entrada ' + fmtTok(t.in) + ' · salida ' + fmtTok(t.out) + ' · caché ' + fmtTok(t.cacheW + t.cacheR)));
      card.append(tok('Tokens · sesión', a.tokSession), tok('Tokens · semana', a.tokWeek));
      if (a.sharedWith && a.sharedWith.length) card.append(h('div', { class: 'hint' }, 'Tokens sumados con ' + a.sharedWith.join(', ') + ': comparten la carpeta de conversaciones.'));
      return card;
    }));
    try { body.append(usageHistoryPanel(await api.get('usage-history'))); }
    catch (e) { body.append(h('div', { class: 'hint err' }, e.message)); }
  }
  fill(false);
  return m;
}
async function dlgResources() {
  if (!S.ws) return;
  const body = h('div', { class: 'stack' }, h('div', { class: 'hint' }, 'Midiendo…'));
  let alive = true;
  modal({ title: 'Recursos', sub: 'Memoria y CPU de cada sesión con todos sus procesos; se actualiza cada 2 segundos.', width: '680px', body,
    buttons: [{ label: 'Cerrar', primary: true }], onclose: () => { alive = false; } });
  while (alive) {
    try {
      const r = await api.get('resources', { ws: S.ws });
      const maxMB = Math.max(1, ...(r.sessions || []).map(s => s.rssMB));
      body.replaceChildren(
        h('div', { class: 'rsum' },
          h('div', {}, h('span', {}, 'Claude'), h('b', {}, fmtMB(r.claudeMB)), h('small', {}, Math.round(r.claudeCPU) + ' % CPU')),
          h('div', {}, h('span', {}, 'Terminales'), h('b', {}, fmtMB(r.shellMB)), h('small', {}, Math.round(r.shellCPU) + ' % CPU')),
          r.machineMB ? h('div', {}, h('span', {}, 'Equipo'), h('b', {}, fmtMB(r.machineMB - r.machineFreeMB)), h('small', {}, 'de ' + fmtMB(r.machineMB))) : null),
        h('div', { class: 'rtable' }, (r.sessions || []).map(s => h('div', { class: 'rrow' },
          h('span', { class: 'rn' }, s.kind === 'claude' ? '✦ ' : '❯ ', h('b', {}, s.name), h('small', {}, s.tab)),
          h('div', { class: 'rbar' }, h('i', { style: { width: (s.rssMB / maxMB * 100) + '%' } })),
          h('span', { class: 'rv' }, fmtMB(s.rssMB)), h('span', { class: 'rv' + (s.cpu >= 50 ? ' hot' : '') }, Math.round(s.cpu) + ' %'),
          h('span', { class: 'rv dim' }, s.procs + ' proc.')))),
        r.outsideN ? h('div', { class: 'hint' }, 'Fuera de este workspace hay ' + r.outsideN + ' procesos de Claude que usan ' + fmtMB(r.outsideMB) + '.') : null);
    } catch (e) { body.replaceChildren(h('div', { class: 'hint err' }, e.message)); }
    await new Promise(r => setTimeout(r, 2000));
  }
}

// ---------- new version ----------
// When a newer version is published, a small card offers to install it. Installing restarts
// the local server on the new binary; the sessions keep running and the window reloads.
async function checkUpdate() {
  const u = await api.get('update').catch(() => null);
  if (!u || !u.latest || document.querySelector('.updcard')) return;
  const card = h('div', { class: 'updcard' },
    h('div', {}, h('b', {}, 'Hay una versión nueva: ' + u.latest), h('small', {}, 'Tienes la ' + u.current + '. Tus sesiones siguen corriendo al actualizar.')),
    h('div', { class: 'btnrow' },
      h('button', { class: 'btn', onclick: () => { api.do('update.dismiss', { name: u.latest }).catch(() => {}); card.remove(); } }, 'Ahora no'),
      h('button', { class: 'btn primary', onclick: async ev => {
        const b = ev.currentTarget; b.disabled = true; b.textContent = 'Instalando…';
        try { await api.do('update.install'); }
        catch (e) { toast(e.message, true); b.disabled = false; b.textContent = 'Instalar'; return; }
        b.textContent = 'Reiniciando…';
        for (let i = 0; i < 60; i++) {
          await new Promise(r => setTimeout(r, 500));
          const v = await fetch('/api/ping', { credentials: 'same-origin' }).then(r => r.ok ? r.text() : '').catch(() => '');
          if (v.trim() === u.latest) { location.reload(); return; }
        }
        toast('El servidor no volvió a responder; vuelve a abrir la app.', true);
      } }, 'Instalar')));
  document.body.append(card);
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
  checkUpdate();
  skyConnect();
  window.skyVoice?.sync();
})();
