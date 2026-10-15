// Help (#/help/<topic>): the documentation `loom skill` serves, rendered by
// the safe Markdown renderer in lib/markdown.js.

import { h, icon, replace, debounce } from '../dom.js';
import * as api from '../api.js';
import { href } from '../router.js';
import { renderMarkdown } from '../lib/markdown.js';

const DEFAULT_TOPIC = 'guide/ai-agents';
const GROUP_TITLE = { guide: 'Guide', reference: 'Reference', spec: 'Specs' };
let topicsCache = null;

/** The index: a JSON list, or the Markdown list `loom skill list` prints. */
export function parseIndex(data) {
  if (data && typeof data === 'object') {
    const list = Array.isArray(data) ? data : (data.topics || []);
    return list.map((t) => (typeof t === 'string' ? { name: t, title: t } : { name: t.name, title: t.title || t.name }));
  }
  const out = [];
  for (const line of String(data || '').split('\n')) {
    const m = /^\s*[-*]\s+`([^`]+)`\s*(?:[—–-]\s*(.*))?$/.exec(line);
    if (m) out.push({ name: m[1], title: (m[2] || m[1]).trim() });
  }
  return out;
}

export function renderHelp(outlet, params) {
  let topic = params.topic || DEFAULT_TOPIC;
  let topics = topicsCache;
  let seq = 0;
  const navList = h('div', { class: 'topics' });
  const filter = h('input', { class: 'input input-sm', type: 'search', placeholder: 'Filter topics', 'aria-label': 'Filter topics', onInput: debounce(() => paintNav(), 80) });
  const article = h('article', { class: 'md', 'aria-live': 'polite' });

  outlet.appendChild(h('div', { class: 'page' },
    h('div', { class: 'page-head' }, h('div', { class: 'title-block' },
      h('h1', null, h('span', { class: 'title-mark' }, icon('book')), 'Help'),
      h('div', { class: 'subtitle' }, 'The documentation that ships in this loom binary — the same text ', h('code', null, 'loom skill'), ' prints.'))),
    h('div', { class: 'help-layout' },
      h('nav', { class: 'help-nav card card-pad', 'aria-label': 'Topics' }, h('div', { class: 'mb-3' }, filter), navList),
      h('div', { class: 'card card-pad' }, article))));

  function resolveLink(hrefText) {
    if (hrefText.startsWith('skill:')) {
      const name = hrefText.slice(6);
      return { href: href(`/help/${encodeURIComponent(name)}`) };
    }
    if (/^(https?:|mailto:)/.test(hrefText)) return { href: hrefText, external: true };
    const m = /^\/(guide|reference)\/([\w.-]+?)(?:\.md)?(?:#.*)?$/.exec(hrefText);
    if (m) return { href: href(`/help/${encodeURIComponent(`${m[1]}/${m[2]}`)}`) };
    return null;
  }

  function paintNav() {
    if (!topics) { replace(navList, [70, 50, 90, 60, 80].map((w) => h('div', { class: `skel skel-line skel-w-${w === 60 ? 50 : w === 80 ? 70 : w}` }))); return; }
    const q = filter.value.trim().toLowerCase();
    const groups = new Map();
    for (const t of topics) {
      if (q && !`${t.name} ${t.title}`.toLowerCase().includes(q)) continue;
      const g = t.name.split('/')[0];
      if (!groups.has(g)) groups.set(g, []);
      groups.get(g).push(t);
    }
    if (!groups.size) { replace(navList, h('p', { class: 'small muted' }, 'No topic matches.')); return; }
    replace(navList, [...groups.entries()].map(([g, list]) => h('div', { class: 'topic-group' },
      h('div', { class: 'section-title' }, GROUP_TITLE[g] || g),
      h('ul', null, list.map((t) => h('li', null, h('a', {
        href: href(`/help/${encodeURIComponent(t.name)}`), 'aria-current': t.name === topic ? 'page' : null, title: t.name,
      }, t.title)))))));
  }

  async function loadTopic() {
    const my = ++seq;
    replace(article, h('div', { class: 'skel skel-title' }), [90, 70, 90, 50].map((w) => h('div', { class: `skel skel-line skel-w-${w}` })));
    try {
      const text = await api.docs.topic(topic);
      if (my !== seq) return;
      replace(article, renderMarkdown(typeof text === 'string' ? text : String(text && text.text || ''), { h, resolveLink }));
      const t = topics && topics.find((x) => x.name === topic);
      document.title = `${t ? t.title : topic} · Help · loom`;
    } catch (err) {
      if (my !== seq) return;
      replace(article, h('div', { class: 'empty' }, icon('book', 'icon-xl'),
        h('h3', null, err.status === 404 ? `No topic “${topic}”` : 'Could not load this topic'),
        h('p', null, err.message),
        h('div', { class: 'actions' }, h('a', { class: 'btn', href: href('/help') }, 'Back to the guide'))));
    }
  }

  paintNav();
  if (!topics) {
    api.docs.index().then((d) => { topics = parseIndex(d); topicsCache = topics; paintNav(); }).catch(() => { topics = []; paintNav(); });
  }
  loadTopic();

  return {
    update(p) {
      topic = p.topic || DEFAULT_TOPIC;
      paintNav();
      loadTopic();
      window.scrollTo(0, 0);
      return true;
    },
  };
}
