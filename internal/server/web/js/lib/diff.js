// Unified diff parsing for the diff viewer. Pure: no DOM.

const HUNK_RE = /^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@(.*)$/;

/**
 * parseUnified(text) → {hunks, add, del}. Each hunk is
 * {header, section, oldStart, newStart, lines}, each line
 * {type: 'ctx'|'add'|'del'|'meta', text, old, new}. File header lines
 * ("diff --git", "---", "+++", "index …") before the first hunk are dropped;
 * the viewer shows the path from the structured record instead.
 */
export function parseUnified(text) {
  const src = String(text ?? '');
  const lines = src.split('\n');
  if (lines.length && lines[lines.length - 1] === '') lines.pop();
  const hunks = [];
  let cur = null;
  let oldNo = 0;
  let newNo = 0;
  let add = 0;
  let del = 0;
  for (const line of lines) {
    const m = HUNK_RE.exec(line);
    if (m) {
      oldNo = Number(m[1]);
      newNo = Number(m[3]);
      cur = { header: line, section: (m[5] || '').trim(), oldStart: oldNo, newStart: newNo, lines: [] };
      hunks.push(cur);
      continue;
    }
    if (!cur) continue; // file headers
    if (line.startsWith('+')) {
      cur.lines.push({ type: 'add', text: line.slice(1), old: null, new: newNo++ });
      add++;
    } else if (line.startsWith('-')) {
      cur.lines.push({ type: 'del', text: line.slice(1), old: oldNo++, new: null });
      del++;
    } else if (line.startsWith('\\')) {
      cur.lines.push({ type: 'meta', text: line, old: null, new: null });
    } else {
      // A context line; an empty line inside a hunk is a context line whose
      // leading space was trimmed somewhere along the way.
      cur.lines.push({ type: 'ctx', text: line.startsWith(' ') ? line.slice(1) : line, old: oldNo++, new: newNo++ });
    }
  }
  return { hunks, add, del };
}

/**
 * splitRows(hunk) pairs deletions with the additions that replace them, for a
 * side-by-side view. → [{left, right}] where each side is a line or null.
 */
export function splitRows(hunk) {
  const rows = [];
  const ls = hunk.lines;
  let i = 0;
  while (i < ls.length) {
    const l = ls[i];
    if (l.type === 'ctx') { rows.push({ left: l, right: l }); i++; continue; }
    if (l.type === 'meta') { rows.push({ meta: l }); i++; continue; }
    const dels = [];
    const adds = [];
    while (i < ls.length && ls[i].type === 'del') dels.push(ls[i++]);
    while (i < ls.length && ls[i].type === 'add') adds.push(ls[i++]);
    const n = Math.max(dels.length, adds.length);
    for (let k = 0; k < n; k++) rows.push({ left: dels[k] || null, right: adds[k] || null });
  }
  return rows;
}

export function statusLetter(status) {
  return { added: 'A', deleted: 'D', modified: 'M', renamed: 'R' }[status] || '?';
}
