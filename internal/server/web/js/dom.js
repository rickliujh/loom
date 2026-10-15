// DOM construction from text only. Nothing in the UI turns a string into
// markup: every node is created here, and every piece of data lands in a text
// node or an attribute. That is what keeps the page safe under a strict CSP
// with data that comes from modules, logs and diffs.

const SVG_NS = 'http://www.w3.org/2000/svg';

// Properties set as DOM properties rather than attributes, so that `value`,
// `checked` and friends reflect live state.
const PROPS = new Set(['value', 'checked', 'selected', 'disabled', 'indeterminate', 'readOnly', 'multiple', 'open']);

/**
 * h(tag, attrs, ...children) builds an element.
 *   attrs.class    string, or array of strings/falsy (falsy entries dropped)
 *   attrs.on*      function → addEventListener (onClick → "click")
 *   attrs.ref      function called with the element
 *   attrs.style... not supported on purpose: CSP forbids inline styles.
 *   other keys     attributes; true → "", false/null/undefined → omitted
 * children: strings/numbers become text nodes; arrays are flattened;
 * null/undefined/false are skipped.
 */
export function h(tag, attrs, ...children) {
  const el = document.createElement(tag);
  applyAttrs(el, attrs);
  append(el, children);
  return el;
}

function applyAttrs(el, attrs) {
  if (!attrs) return;
  for (const [key, val] of Object.entries(attrs)) {
    if (val === undefined || val === null || val === false) {
      if (PROPS.has(key) && key in el && val === false) el[key] = false;
      continue;
    }
    if (key === 'class') {
      const cls = Array.isArray(val) ? val.filter(Boolean).join(' ') : String(val);
      if (cls) el.setAttribute('class', cls);
    } else if (key === 'ref') {
      val(el);
    } else if (key.startsWith('on') && typeof val === 'function') {
      el.addEventListener(key.slice(2).toLowerCase(), val);
    } else if (key === 'style') {
      throw new Error('inline styles are not allowed (CSP); use a class');
    } else if (PROPS.has(key) && key in el) {
      el[key] = val;
    } else if (key === 'text') {
      el.textContent = String(val);
    } else {
      el.setAttribute(key, val === true ? '' : String(val));
    }
  }
}

/** Appends children (nested arrays flattened, null/false skipped). */
export function append(parent, ...children) {
  for (const c of children) {
    if (c === null || c === undefined || c === false || c === true) continue;
    if (Array.isArray(c)) { append(parent, ...c); continue; }
    parent.appendChild(c instanceof Node ? c : document.createTextNode(String(c)));
  }
  return parent;
}

export function clear(el) {
  while (el.firstChild) el.removeChild(el.firstChild);
  return el;
}

export function replace(el, ...children) {
  clear(el);
  append(el, children);
  return el;
}

export function frag(...children) {
  return append(document.createDocumentFragment(), children);
}

export function text(s) {
  return document.createTextNode(String(s));
}

/** An icon from the sprite in index.html. */
export function icon(name, cls) {
  const svg = document.createElementNS(SVG_NS, 'svg');
  svg.setAttribute('class', cls ? `icon ${cls}` : 'icon');
  svg.setAttribute('aria-hidden', 'true');
  svg.setAttribute('focusable', 'false');
  const use = document.createElementNS(SVG_NS, 'use');
  use.setAttribute('href', `#i-${name}`);
  svg.appendChild(use);
  return svg;
}

/**
 * highlight(text, needle) returns nodes with each case-insensitive match of
 * needle wrapped in <mark>.
 */
export function highlight(str, needle) {
  const s = String(str ?? '');
  if (!needle) return [s];
  const lower = s.toLowerCase();
  const n = needle.toLowerCase();
  const out = [];
  let i = 0;
  for (;;) {
    const j = lower.indexOf(n, i);
    if (j < 0) break;
    if (j > i) out.push(s.slice(i, j));
    out.push(h('mark', null, s.slice(j, j + n.length)));
    i = j + n.length;
  }
  if (i < s.length) out.push(s.slice(i));
  return out;
}

/** Marks the given indices (from a fuzzy match) in str. */
export function markIndices(str, indices) {
  if (!indices || !indices.length) return [str];
  const set = new Set(indices);
  const out = [];
  let buf = '';
  let inMark = false;
  for (let i = 0; i < str.length; i++) {
    const m = set.has(i);
    if (m !== inMark) {
      if (buf) out.push(inMark ? h('mark', null, buf) : buf);
      buf = '';
      inMark = m;
    }
    buf += str[i];
  }
  if (buf) out.push(inMark ? h('mark', null, buf) : buf);
  return out;
}

/** Splits text on Go template actions so "{{ … }}" can be shown distinctly. */
export function templateSpans(str) {
  const s = String(str ?? '');
  const out = [];
  let i = 0;
  for (;;) {
    const a = s.indexOf('{{', i);
    if (a < 0) break;
    const b = s.indexOf('}}', a + 2);
    if (b < 0) break;
    if (a > i) out.push(s.slice(i, a));
    out.push(h('span', { class: 'tpl' }, s.slice(a, b + 2)));
    i = b + 2;
  }
  if (i < s.length) out.push(s.slice(i));
  return out;
}

export function debounce(fn, ms) {
  let t = 0;
  const d = (...args) => {
    clearTimeout(t);
    t = setTimeout(() => fn(...args), ms);
  };
  d.cancel = () => clearTimeout(t);
  d.flush = (...args) => { clearTimeout(t); fn(...args); };
  return d;
}

/** Triggers a browser download of text content. */
export function downloadText(filename, content, type = 'text/plain') {
  const blob = new Blob([content], { type: `${type};charset=utf-8` });
  const url = URL.createObjectURL(blob);
  const a = h('a', { href: url, download: filename, class: 'sr-only' });
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

let idSeq = 0;
export function uid(prefix = 'u') {
  idSeq += 1;
  return `${prefix}-${idSeq}`;
}
