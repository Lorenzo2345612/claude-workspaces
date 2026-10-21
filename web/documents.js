'use strict';

// El Markdown se construye con nodos de texto: HTML y recursos remotos no se ejecutan.
function markdownNodes(text) {
  const inline = text => text.split(/(`[^`]+`|\*\*[^*]+\*\*)/).filter(Boolean).map(part =>
    part.startsWith('`') ? h('code', {}, part.slice(1, -1)) :
    part.startsWith('**') ? h('strong', {}, part.slice(2, -2)) : part);
  const nodes = [];
  let list = null, fence = null, code = [];
  for (const line of text.replace(/\r\n/g, '\n').split('\n')) {
    if (fence !== null) {
      if (line.trim().startsWith(fence)) { nodes.push(h('pre', {}, h('code', {}, code.join('\n')))); fence = null; code = []; }
      else code.push(line);
      continue;
    }
    const marker = line.match(/^\s*(```+|~~~+)/);
    if (marker) { list = null; fence = marker[1]; continue; }
    const heading = line.match(/^(#{1,6})\s+(.+)$/);
    if (heading) { list = null; nodes.push(h('h' + heading[1].length, {}, ...inline(heading[2]))); continue; }
    const item = line.match(/^\s*([-*+] |\d+\. )(.+)$/);
    if (item) {
      const tag = /^\d/.test(item[1]) ? 'ol' : 'ul';
      if (!list || list.tagName.toLowerCase() !== tag) { list = h(tag); nodes.push(list); }
      list.append(h('li', {}, ...inline(item[2]))); continue;
    }
    list = null;
    if (/^\s*([-*_])(?:\s*\1){2,}\s*$/.test(line)) nodes.push(h('hr'));
    else if (line.startsWith('> ')) nodes.push(h('blockquote', {}, ...inline(line.slice(2))));
    else if (line.trim()) nodes.push(h('p', {}, ...inline(line)));
  }
  if (fence !== null) nodes.push(h('pre', {}, h('code', {}, code.join('\n'))));
  return nodes;
}

function renderDocuments(app) {
  const view = curView(), tabs = $('.document-tabs', app), panel = $('.document-panel', app), stage = $('.stage', app);
  const documents = view?.documents || [];
  tabs.hidden = !documents.length;
  const active = documents.find(d => d.id === view.documentActive);
  const select = id => {
    view.documentActive = id;
    render();
    act('document.select', { view: view.id, document: id }).catch(() => {});
  };
  tabs.replaceChildren(...(documents.length ? [
    h('button', { class: active ? '' : 'on', role: 'tab', 'aria-selected': !active, onclick: () => select('') }, 'Sesiones'),
    ...documents.map(d => h('div', { class: 'document-tab' },
      h('button', { class: active?.id === d.id ? 'on' : '', role: 'tab', 'aria-selected': active?.id === d.id, title: d.path, onclick: () => select(d.id) }, d.title),
      h('button', { 'aria-label': 'Cerrar ' + d.title, onclick: () => act('document.close', { view: view.id, document: d.id }).catch(() => {}) }, '×')))
  ] : []));
  stage.hidden = !!active;
  panel.hidden = !active;
  if (!active) { panel.dataset.key = ''; return; }
  const key = JSON.stringify([S.ws, view.id, active.id]);
  if (panel.dataset.key === key) return;
  panel.dataset.key = key;
  const content = h('article', { class: 'markdown-content' }, 'Cargando…');
  const load = async () => {
    try {
      const data = await api.get('document', { ws: S.ws, view: view.id, document: active.id });
      if (panel.dataset.key === key) content.replaceChildren(...markdownNodes(data.content));
    } catch (e) { if (panel.dataset.key === key) content.replaceChildren(h('p', { class: 'err' }, e.message)); }
  };
  panel.replaceChildren(h('div', { class: 'document-toolbar' }, h('span', {}, active.path),
    h('button', { class: 'btn', onclick: load }, 'Actualizar')), content);
  load();
}
