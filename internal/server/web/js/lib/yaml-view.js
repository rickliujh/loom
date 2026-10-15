// Text-level YAML helpers. The browser has no YAML parser, and a structured
// param must never be round-tripped through JSON (a version written 1.10
// would come back as 1.1), so these work on the text itself: they find where
// each top-level key's value is written and cut it out verbatim. The server
// (/yaml/parse) remains the judge of whether the text is valid.

const KEY_RE = /^("(?:[^"\\]|\\.)*"|'(?:[^']|'')*'|[^\s#\-?:{}[\]&*!|>%@`][^:]*?|-[^\s:][^:]*?)\s*:(?:\s+(.*))?$/;

function unquoteKey(k) {
  if (k.startsWith('"')) {
    try { return JSON.parse(k); } catch { return k.slice(1, -1); }
  }
  if (k.startsWith("'")) return k.slice(1, -1).replace(/''/g, "'");
  return k;
}

/** Strips a trailing " # comment" from a plain inline value. */
function stripComment(s) {
  if (!s) return '';
  if (s.startsWith('"') || s.startsWith("'")) return s.trim();
  const i = s.search(/\s#/);
  return (i >= 0 ? s.slice(0, i) : s).trim();
}

function isBlankOrComment(line) {
  const t = line.trim();
  return t === '' || t.startsWith('#');
}

export function dedent(lines) {
  let min = Infinity;
  for (const l of lines) {
    if (l.trim() === '') continue;
    const ind = l.length - l.trimStart().length;
    if (ind < min) min = ind;
  }
  if (!Number.isFinite(min)) min = 0;
  return lines.map((l) => (l.trim() === '' ? '' : l.slice(min)));
}

/**
 * splitTopLevel(text) → Map(key → {inline, block}) for a YAML mapping
 * document. inline is the text after "key:" on the same line (comment
 * stripped), block the dedented lines that follow it. Returns null when the
 * document is not a block mapping (a flow map, a list, a scalar).
 */
export function splitTopLevel(text) {
  const lines = String(text ?? '').replace(/\r\n?/g, '\n').split('\n');
  const out = new Map();
  let cur = null;
  let started = false;
  const finish = () => {
    if (!cur) return;
    // Trailing blank lines and column-0 comments belong to what follows.
    while (cur.lines.length && (cur.lines[cur.lines.length - 1].trim() === '' || /^#/.test(cur.lines[cur.lines.length - 1]))) cur.lines.pop();
    out.set(cur.key, { inline: cur.inline, block: dedent(cur.lines).join('\n') });
    cur = null;
  };
  for (const line of lines) {
    if (!started) {
      if (line.startsWith('%') || line.trim() === '---' || isBlankOrComment(line)) continue;
      started = true;
    }
    if (line.trim() === '---' || line.trim() === '...') { finish(); break; }
    const col0 = line.length > 0 && !/^\s/.test(line);
    if (col0 && !line.startsWith('#') && !/^-(\s|$)/.test(line)) {
      const m = KEY_RE.exec(line);
      if (!m) {
        if (!cur && out.size === 0) return null;
        finish();
        return out.size ? out : null;
      }
      finish();
      cur = { key: unquoteKey(m[1].trim()), inline: stripComment(m[2] || ''), lines: [] };
      continue;
    }
    if (!cur) {
      if (isBlankOrComment(line)) continue;
      return null; // a sequence or scalar document
    }
    cur.lines.push(line);
  }
  finish();
  return out;
}

/**
 * valueText(entry) → the YAML text of one top-level value, ready for a
 * textarea: the inline flow value, or the dedented block below the key.
 */
export function valueText(entry) {
  if (!entry) return '';
  const inl = entry.inline;
  if (inl && !/^[|>][-+0-9]*$/.test(inl) && !inl.startsWith('#')) {
    if (entry.block.trim()) return `${inl}\n${entry.block}`.trimEnd() + '\n';
    return inl;
  }
  return entry.block ? entry.block.replace(/\s+$/, '') + '\n' : '';
}

/** Plain-scalar text for a string param whose YAML value parsed as a number or boolean. */
export function scalarText(entry) {
  if (!entry) return '';
  return entry.inline || '';
}

export function indent(text, n = 2) {
  const pad = ' '.repeat(n);
  return String(text).replace(/\s+$/, '').split('\n').map((l) => (l.trim() === '' ? '' : pad + l)).join('\n');
}

const PLAIN_KEY = /^[A-Za-z_][A-Za-z0-9_.-]*$/;

export function yamlKey(k) {
  return PLAIN_KEY.test(k) ? k : JSON.stringify(k);
}

/** A JSON string literal is a valid YAML double-quoted scalar. */
export function yamlString(s) {
  return JSON.stringify(String(s));
}

/**
 * composeParams(entries, formattedScalars) builds a params file:
 * entries = [{name, type, text}] where text is the raw textarea content for a
 * list/map param or the plain string for a string param. formattedScalars, if
 * given, is the /yaml/format output for the string params (a mapping), used
 * verbatim; otherwise strings are written double-quoted.
 */
export function composeParams(entries, formattedScalars) {
  const parts = [];
  if (formattedScalars && formattedScalars.trim() && formattedScalars.trim() !== '{}') {
    parts.push(formattedScalars.replace(/\s+$/, ''));
  } else {
    for (const e of entries) {
      if (e.type === 'list' || e.type === 'map') continue;
      parts.push(`${yamlKey(e.name)}: ${yamlString(e.text)}`);
    }
  }
  for (const e of entries) {
    if (e.type !== 'list' && e.type !== 'map') continue;
    const body = String(e.text).replace(/\s+$/, '');
    if (!body) continue;
    parts.push(`${yamlKey(e.name)}:\n${indent(body, 2)}`);
  }
  return parts.length ? parts.join('\n') + '\n' : '';
}

/** Number of top-level items for a status line ("3 items", "2 keys"). */
export function describeValue(v) {
  if (Array.isArray(v)) return `${v.length} ${v.length === 1 ? 'item' : 'items'}`;
  if (v && typeof v === 'object') {
    const n = Object.keys(v).length;
    return `${n} ${n === 1 ? 'key' : 'keys'}`;
  }
  if (v === null || v === undefined) return 'empty';
  return typeof v;
}

export function kindOf(v) {
  if (Array.isArray(v)) return 'list';
  if (v && typeof v === 'object') return 'map';
  if (v === null || v === undefined) return 'null';
  return 'scalar';
}
