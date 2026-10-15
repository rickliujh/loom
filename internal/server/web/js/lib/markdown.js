// A small, safe Markdown renderer for the help panel. parseMarkdown() turns
// text into a plain AST (testable without a DOM); renderMarkdown() turns that
// into DOM nodes through the caller's h(), so text only ever reaches the page
// as text nodes. Raw HTML in the source is shown as text, never interpreted.
//
// Covered: ATX headings, paragraphs, fenced code, inline code, emphasis,
// links, ordered/unordered (nested) lists, pipe tables, blockquotes, rules,
// and VitePress "::: tip" containers — what the loom docs use.

const FENCE_RE = /^(\s{0,3})(`{3,}|~{3,})\s*([^`\s]*)?.*$/;
const HEADING_RE = /^\s{0,3}(#{1,6})\s+(.*?)\s*#*\s*$/;
const HR_RE = /^\s{0,3}([-*_])(\s*\1){2,}\s*$/;
const LIST_RE = /^(\s*)([-*+]|\d{1,9}[.)])(\s+|$)(.*)$/;
const TABLE_SEP_RE = /^\s*\|?\s*:?-{1,}:?\s*(\|\s*:?-{1,}:?\s*)*\|?\s*$/;
const CONTAINER_RE = /^\s*:::\s*(\w+)?\s*(.*)$/;
const ANCHOR_RE = /\s*\{#[A-Za-z0-9_-]+\}\s*$/;

export function parseMarkdown(src) {
  const lines = String(src ?? '').replace(/\r\n?/g, '\n').split('\n');
  return parseBlocks(lines);
}

function isBlank(l) { return l.trim() === ''; }

function splitRow(line) {
  let s = line.trim();
  if (s.startsWith('|')) s = s.slice(1);
  if (s.endsWith('|') && !s.endsWith('\\|')) s = s.slice(0, -1);
  const cells = [];
  let cur = '';
  let inCode = false;
  for (let i = 0; i < s.length; i++) {
    const c = s[i];
    if (c === '\\' && s[i + 1] === '|') { cur += '|'; i++; continue; }
    if (c === '`') inCode = !inCode;
    if (c === '|' && !inCode) { cells.push(cur.trim()); cur = ''; continue; }
    cur += c;
  }
  cells.push(cur.trim());
  return cells;
}

function startsBlock(line, next) {
  return FENCE_RE.test(line) && /^(\s{0,3})(`{3,}|~{3,})/.test(line)
    || HEADING_RE.test(line)
    || HR_RE.test(line)
    || /^\s{0,3}>/.test(line)
    || LIST_RE.test(line)
    || CONTAINER_RE.test(line) && /^\s*:::/.test(line)
    || (line.includes('|') && next !== undefined && TABLE_SEP_RE.test(next) && next.includes('-'));
}

function parseBlocks(lines) {
  const blocks = [];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    if (isBlank(line)) { i++; continue; }

    // Fenced code
    const fm = /^(\s{0,3})(`{3,}|~{3,})\s*([^\s`]*)/.exec(line);
    if (fm) {
      const indent = fm[1].length;
      const fence = fm[2];
      const lang = fm[3] || '';
      const body = [];
      i++;
      while (i < lines.length) {
        const l = lines[i];
        if (l.trimStart().startsWith(fence[0].repeat(fence.length)) && l.trim().replace(new RegExp(`^\\${fence[0]}+`), '') === '') { i++; break; }
        body.push(indent ? l.replace(new RegExp(`^ {0,${indent}}`), '') : l);
        i++;
      }
      blocks.push({ type: 'code', lang, text: body.join('\n') });
      continue;
    }

    // ::: container
    const cm = /^\s*:::\s*(\w+)\s*(.*)$/.exec(line);
    if (cm) {
      const body = [];
      i++;
      let depth = 1;
      while (i < lines.length) {
        const l = lines[i];
        if (/^\s*:::\s*\w+/.test(l)) depth++;
        else if (/^\s*:::\s*$/.test(l)) { depth--; if (depth === 0) { i++; break; } }
        body.push(l);
        i++;
      }
      blocks.push({ type: 'callout', kind: cm[1].toLowerCase(), title: cm[2].trim(), blocks: parseBlocks(body) });
      continue;
    }

    const hm = HEADING_RE.exec(line);
    if (hm) {
      blocks.push({ type: 'heading', level: hm[1].length, inlines: parseInline(hm[2].replace(ANCHOR_RE, '')) });
      i++;
      continue;
    }

    if (HR_RE.test(line)) { blocks.push({ type: 'hr' }); i++; continue; }

    // Table
    if (line.includes('|') && i + 1 < lines.length && TABLE_SEP_RE.test(lines[i + 1]) && lines[i + 1].includes('-')) {
      const head = splitRow(line);
      const align = splitRow(lines[i + 1]).map((c) => (c.startsWith(':') && c.endsWith(':') ? 'center' : c.endsWith(':') ? 'right' : ''));
      i += 2;
      const rows = [];
      while (i < lines.length && !isBlank(lines[i]) && lines[i].includes('|')) {
        rows.push(splitRow(lines[i]).map(parseInline));
        i++;
      }
      blocks.push({ type: 'table', head: head.map(parseInline), align, rows });
      continue;
    }

    // Blockquote
    if (/^\s{0,3}>/.test(line)) {
      const body = [];
      while (i < lines.length && /^\s{0,3}>/.test(lines[i])) {
        body.push(lines[i].replace(/^\s{0,3}>\s?/, ''));
        i++;
      }
      blocks.push({ type: 'quote', blocks: parseBlocks(body) });
      continue;
    }

    // List
    const lm = LIST_RE.exec(line);
    if (lm) {
      const baseIndent = lm[1].length;
      const ordered = /\d/.test(lm[2]);
      const start = ordered ? parseInt(lm[2], 10) : 1;
      const items = [];
      while (i < lines.length) {
        const m = LIST_RE.exec(lines[i]);
        if (!m || m[1].length !== baseIndent || /\d/.test(m[2]) !== ordered) break;
        const contentIndent = m[1].length + m[2].length + Math.max(1, m[3].length);
        const itemLines = [m[4]];
        i++;
        while (i < lines.length) {
          const l = lines[i];
          if (isBlank(l)) {
            // A blank line continues the item only if indented content follows.
            const nxt = lines[i + 1];
            if (nxt !== undefined && !isBlank(nxt) && (nxt.length - nxt.trimStart().length) >= contentIndent) {
              itemLines.push('');
              i++;
              continue;
            }
            break;
          }
          const ind = l.length - l.trimStart().length;
          if (ind >= contentIndent) { itemLines.push(l.slice(contentIndent)); i++; continue; }
          const sub = LIST_RE.exec(l);
          if (sub && sub[1].length > baseIndent) { itemLines.push(l.slice(Math.min(ind, contentIndent))); i++; continue; }
          if (sub || startsBlock(l, lines[i + 1])) break;
          // Lazy continuation of the item's paragraph.
          itemLines.push(l.trim());
          i++;
        }
        items.push(parseBlocks(itemLines));
        while (i < lines.length && isBlank(lines[i])) {
          const nxt = lines[i + 1];
          const m2 = nxt !== undefined ? LIST_RE.exec(nxt) : null;
          if (m2 && m2[1].length === baseIndent) { i++; } else break;
        }
      }
      blocks.push({ type: 'list', ordered, start, items });
      continue;
    }

    // Paragraph
    const para = [];
    while (i < lines.length && !isBlank(lines[i]) && (para.length === 0 || !startsBlock(lines[i], lines[i + 1]))) {
      para.push(lines[i].trim());
      i++;
    }
    blocks.push({ type: 'para', inlines: parseInline(para.join('\n')) });
  }
  return blocks;
}

// ---- Inline ----

export function parseInline(src) {
  const s = String(src ?? '');
  const out = [];
  let buf = '';
  const flush = () => { if (buf) { out.push({ type: 'text', text: buf }); buf = ''; } };
  let i = 0;
  while (i < s.length) {
    const c = s[i];
    if (c === '\\' && i + 1 < s.length && /[\\`*_{}[\]()#+\-.!|<>~]/.test(s[i + 1])) {
      buf += s[i + 1];
      i += 2;
      continue;
    }
    if (c === '\n') {
      flush();
      out.push({ type: 'text', text: ' ' });
      i++;
      continue;
    }
    if (c === '`') {
      let n = 0;
      while (s[i + n] === '`') n++;
      const ticks = '`'.repeat(n);
      const end = s.indexOf(ticks, i + n);
      if (end > 0) {
        flush();
        let code = s.slice(i + n, end).replace(/\n/g, ' ');
        if (code.startsWith(' ') && code.endsWith(' ') && code.trim()) code = code.slice(1, -1);
        out.push({ type: 'code', text: code });
        i = end + n;
        continue;
      }
      buf += ticks;
      i += n;
      continue;
    }
    if (s.startsWith('<code v-pre>', i)) {
      const end = s.indexOf('</code>', i);
      if (end > 0) {
        flush();
        out.push({ type: 'code', text: s.slice(i + 12, end) });
        i = end + 7;
        continue;
      }
    }
    if (c === '<') {
      const am = /^<(https?:\/\/[^\s>]+)>/.exec(s.slice(i));
      if (am) {
        flush();
        out.push({ type: 'link', href: am[1], children: [{ type: 'text', text: am[1] }] });
        i += am[0].length;
        continue;
      }
      const br = /^<br\s*\/?>/i.exec(s.slice(i));
      if (br) { flush(); out.push({ type: 'br' }); i += br[0].length; continue; }
    }
    if (c === '!' && s[i + 1] === '[') {
      // Images are not loaded (CSP img-src is 'self'); show their alt text.
      const lk = matchLink(s, i + 1);
      if (lk) {
        flush();
        out.push({ type: 'text', text: lk.text });
        i = lk.end;
        continue;
      }
    }
    if (c === '[') {
      const lk = matchLink(s, i);
      if (lk) {
        flush();
        out.push({ type: 'link', href: lk.href, children: parseInline(lk.text) });
        i = lk.end;
        continue;
      }
    }
    if ((c === '*' || c === '_') ) {
      const dbl = s[i + 1] === c;
      const marker = dbl ? c + c : c;
      const prevCh = i > 0 ? s[i - 1] : ' ';
      const nextCh = s[i + marker.length];
      const leftFlank = nextCh !== undefined && !/\s/.test(nextCh);
      const intraword = c === '_' && /[A-Za-z0-9]/.test(prevCh);
      if (leftFlank && !intraword) {
        const end = findClose(s, i + marker.length, marker);
        if (end > 0) {
          flush();
          out.push({ type: dbl ? 'strong' : 'em', children: parseInline(s.slice(i + marker.length, end)) });
          i = end + marker.length;
          continue;
        }
      }
      buf += marker;
      i += marker.length;
      continue;
    }
    buf += c;
    i++;
  }
  flush();
  return out;
}

function findClose(s, from, marker) {
  let j = from;
  while (j < s.length) {
    const k = s.indexOf(marker, j);
    if (k < 0) return -1;
    if (s[k] === '`') return -1;
    const before = s[k - 1];
    const after = s[k + marker.length];
    const rightFlank = before !== undefined && !/\s/.test(before);
    const okAfter = marker[0] !== '_' || after === undefined || !/[A-Za-z0-9]/.test(after);
    // Do not let "*" close on the first half of a "**".
    const splitDouble = marker.length === 1 && s[k + 1] === marker && s[k - 1] !== marker;
    if (rightFlank && okAfter && !splitDouble && k > from) return k;
    j = k + marker.length;
  }
  return -1;
}

function matchLink(s, i) {
  // [text](href) with balanced brackets in text.
  let depth = 0;
  let j = i;
  for (; j < s.length; j++) {
    if (s[j] === '\\') { j++; continue; }
    if (s[j] === '[') depth++;
    else if (s[j] === ']') { depth--; if (depth === 0) break; }
  }
  if (depth !== 0 || s[j + 1] !== '(') return null;
  const close = s.indexOf(')', j + 2);
  if (close < 0) return null;
  let href = s.slice(j + 2, close).trim();
  const sp = href.search(/\s/);
  if (sp > 0) href = href.slice(0, sp);
  href = href.replace(/^<|>$/g, '');
  return { text: s.slice(i + 1, j), href, end: close + 1 };
}

// ---- Rendering ----

/**
 * renderMarkdown(src, {h, resolveLink}) → array of DOM nodes.
 * resolveLink(href) returns {href, external} or null (render as text).
 */
export function renderMarkdown(src, { h, resolveLink }) {
  const ast = parseMarkdown(src);
  return ast.map((b) => renderBlock(b, h, resolveLink));
}

function renderBlock(b, h, rl) {
  switch (b.type) {
    case 'heading': return h(`h${b.level}`, null, renderInlines(b.inlines, h, rl));
    case 'para': return h('p', null, renderInlines(b.inlines, h, rl));
    case 'code': return h('pre', { 'data-lang': b.lang || null }, h('code', null, b.text));
    case 'hr': return h('hr');
    case 'quote': return h('blockquote', null, b.blocks.map((x) => renderBlock(x, h, rl)));
    case 'callout': {
      const title = b.title || { tip: 'Tip', warning: 'Warning', danger: 'Danger', info: 'Info', details: 'Details' }[b.kind] || b.kind;
      return h('div', { class: ['callout', `callout-${b.kind}`] },
        h('div', { class: 'callout-title' }, title),
        b.blocks.map((x) => renderBlock(x, h, rl)));
    }
    case 'list': {
      const items = b.items.map((blocks) => {
        // A tight item holding one paragraph renders without a <p>.
        if (blocks.length === 1 && blocks[0].type === 'para') return h('li', null, renderInlines(blocks[0].inlines, h, rl));
        return h('li', null, blocks.map((x) => (x.type === 'para' && blocks.length > 1 && x === blocks[0]
          ? h('div', null, renderInlines(x.inlines, h, rl))
          : renderBlock(x, h, rl))));
      });
      return b.ordered ? h('ol', { start: b.start !== 1 ? b.start : null }, items) : h('ul', null, items);
    }
    case 'table':
      return h('div', { class: 'md-table' }, h('table', null,
        h('thead', null, h('tr', null, b.head.map((c, k) => h('th', { class: b.align[k] ? `al-${b.align[k]}` : null }, renderInlines(c, h, rl))))),
        h('tbody', null, b.rows.map((r) => h('tr', null, r.map((c) => h('td', null, renderInlines(c, h, rl))))))));
    default: return h('div');
  }
}

function renderInlines(list, h, rl) {
  return list.map((n) => {
    switch (n.type) {
      case 'text': return n.text;
      case 'br': return h('br');
      case 'code': {
        const m = /^loom skill (\S+)$/.exec(n.text);
        const target = m && rl ? rl(`skill:${m[1]}`) : null;
        const code = h('code', null, n.text);
        return target ? h('a', { href: target.href }, code) : code;
      }
      case 'strong': return h('strong', null, renderInlines(n.children, h, rl));
      case 'em': return h('em', null, renderInlines(n.children, h, rl));
      case 'link': {
        const target = rl ? rl(n.href) : null;
        const kids = renderInlines(n.children, h, rl);
        if (!target) return h('span', { class: 'md-link-text' }, kids);
        return h('a', target.external
          ? { href: target.href, target: '_blank', rel: 'noopener noreferrer' }
          : { href: target.href }, kids);
      }
      default: return '';
    }
  });
}
