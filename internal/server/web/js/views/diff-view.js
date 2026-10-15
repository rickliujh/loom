// Diff viewer: files grouped by module breadcrumb and target, unified or
// split, with line numbers, per-file copy and collapse.

import { h, icon, replace } from '../dom.js';
import { prefs } from '../store.js';
import { crumbs } from '../components/chip.js';
import { copyButton } from '../components/copy.js';
import { parseUnified, splitRows, statusLetter } from '../lib/diff.js';
import { plural } from '../lib/format.js';

const BIG = 1500; // lines: larger files start collapsed

export function createDiffView({ emptyHint } = {}) {
  let layout = prefs.get('diff.layout', window.matchMedia('(max-width: 760px)').matches ? 'unified' : 'split');
  let data = null;
  const collapsed = new Set();
  const el = h('div', { class: 'diff-view' });

  function stats(d) {
    let add = 0; let del = 0; let files = 0;
    for (const t of d.targets || []) for (const f of t.files || []) {
      files++;
      const p = parseUnified(f.unified || '');
      add += p.add; del += p.del;
    }
    return { add, del, files };
  }

  function paint() {
    if (!data) {
      replace(el, h('div', { class: 'stack-sm' }, [90, 70, 80].map((w) => h('div', { class: `skel skel-line skel-w-${w === 80 ? 70 : w}` }))));
      return;
    }
    const s = stats(data);
    const incomplete = data.incomplete
      ? h('div', { class: 'banner banner-warn mb-3' }, icon('alert'), h('div', { class: 'banner-body' },
        h('div', { class: 'banner-title' }, 'Incomplete — the run failed'),
        h('div', null, 'These are the changes made before the failure. Later operations did not run, so the full change would differ.')))
      : null;
    if (!s.files) {
      replace(el, incomplete, h('div', { class: 'empty' }, icon('diff', 'icon-xl'), h('h3', null, 'No changes'),
        h('p', null, emptyHint || 'Nothing in any target would change.')));
      return;
    }
    const allKeys = [];
    const toolbar = h('div', { class: 'diff-tools' },
      h('span', { class: 'diff-stat' }, `${plural(s.files, 'file')} in ${plural(data.targets.filter((t) => t.files && t.files.length).length, 'target')} · `,
        h('span', { class: 'add' }, `+${s.add}`), ' ', h('span', { class: 'del' }, `−${s.del}`)),
      h('span', { class: 'spacer' }),
      h('div', { class: 'seg', role: 'group', 'aria-label': 'Diff layout' },
        h('button', { type: 'button', 'aria-pressed': String(layout === 'unified'), onClick: () => setLayout('unified') }, icon('unified', 'icon-sm'), 'Unified'),
        h('button', { type: 'button', 'aria-pressed': String(layout === 'split'), onClick: () => setLayout('split') }, icon('split', 'icon-sm'), 'Split')),
      h('button', { class: 'btn btn-sm', type: 'button', onClick: () => { allKeys.forEach((k) => collapsed.add(k)); paint(); } }, icon('collapse', 'icon-sm'), 'Collapse all'),
      h('button', { class: 'btn btn-sm', type: 'button', onClick: () => { collapsed.clear(); paint(); } }, icon('expand', 'icon-sm'), 'Expand all'));

    const groups = data.targets.filter((t) => t.files && t.files.length).map((t, ti) => h('section', { class: 'diff-target' },
      h('div', { class: 'diff-target-head' },
        t.path && t.path.length ? crumbs(t.path) : null,
        t.repo ? h('span', { class: 'repo' }, icon('repo', 'icon-sm'), t.repo, t.branch ? ` (${t.branch})` : '') : null),
      t.files.map((f, fi) => {
        const key = `${ti}:${fi}:${f.path}`;
        allKeys.push(key);
        return fileView(f, key);
      })));
    replace(el, incomplete, toolbar, groups);
  }

  function setLayout(l) {
    layout = l;
    prefs.set('diff.layout', l);
    paint();
  }

  function fileView(f, key) {
    const parsed = parseUnified(f.unified || '');
    const lineCount = parsed.hunks.reduce((n, x) => n + x.lines.length, 0);
    if (!collapsed.has(`seen:${key}`)) {
      collapsed.add(`seen:${key}`);
      if (lineCount > BIG) collapsed.add(key);
    }
    const isCollapsed = collapsed.has(key);
    const bodyId = `df-${key.replace(/[^a-z0-9]/gi, '-')}`;
    const toggle = h('button', {
      class: 'toggle', type: 'button', 'aria-expanded': String(!isCollapsed), 'aria-controls': bodyId,
      'aria-label': `${isCollapsed ? 'Expand' : 'Collapse'} ${f.path}`,
      onClick: () => { if (collapsed.has(key)) collapsed.delete(key); else collapsed.add(key); paint(); },
    }, icon('chevron-down'));
    let body;
    if (f.binary) body = h('div', { class: 'dfile-note' }, 'Binary file — contents not shown.');
    else if (!parsed.hunks.length) body = h('div', { class: 'dfile-note' }, f.status === 'renamed' ? 'Renamed without changes.' : 'No textual changes.');
    else body = layout === 'split' ? splitTable(parsed) : unifiedTable(parsed);
    return h('div', { class: ['dfile', isCollapsed && 'collapsed'] },
      h('div', { class: 'dfile-head' },
        toggle,
        h('span', { class: ['fstat', `fstat-${f.status}`], title: f.status }, statusLetter(f.status)),
        h('span', { class: 'fpath' },
          f.oldPath && f.oldPath !== f.path ? [h('span', { class: 'old' }, f.oldPath), ' → '] : null,
          f.path),
        h('span', { class: 'counts' }, h('span', { class: 'add' }, `+${parsed.add}`), ' ', h('span', { class: 'del' }, `−${parsed.del}`)),
        copyButton(() => f.unified || '', { label: `Copy the diff of ${f.path}`, what: 'Diff copied' })),
      h('div', { class: 'dfile-body', id: bodyId }, isCollapsed ? null : body));
  }

  return {
    el,
    set(d) { data = d; paint(); },
    get() { return data; },
  };
}

function unifiedTable(parsed) {
  const rows = [];
  for (const hk of parsed.hunks) {
    rows.push(h('tr', { class: 'hunk' }, h('td', { class: 'ln' }), h('td', { class: 'ln' }), h('td', { class: 'mk' }), h('td', { class: 'tx' }, hk.header)));
    for (const l of hk.lines) {
      if (l.type === 'meta') { rows.push(h('tr', { class: 'meta' }, h('td', { class: 'ln' }), h('td', { class: 'ln' }), h('td', { class: 'mk' }), h('td', { class: 'tx' }, l.text))); continue; }
      rows.push(h('tr', { class: l.type === 'ctx' ? '' : l.type },
        h('td', { class: 'ln' }, l.old ?? ''),
        h('td', { class: 'ln' }, l.new ?? ''),
        h('td', { class: 'mk' }, l.type === 'add' ? '+' : l.type === 'del' ? '−' : ' '),
        h('td', { class: 'tx' }, l.text)));
    }
  }
  return h('table', { class: 'dtable' }, h('tbody', null, rows));
}

function splitTable(parsed) {
  const rows = [];
  const side = (l, which) => {
    if (!l) return [h('td', { class: ['ln', 'empty-side', which === 'r' && 'gap'] }), h('td', { class: 'tx side empty-side' })];
    const t = l.type === 'ctx' ? '' : l.type;
    return [
      h('td', { class: ['ln', t, which === 'r' && 'gap'] }, which === 'l' ? (l.old ?? '') : (l.new ?? '')),
      h('td', { class: ['tx', 'side', t] }, l.text)];
  };
  for (const hk of parsed.hunks) {
    rows.push(h('tr', { class: 'hunk' }, h('td', { class: 'ln' }), h('td', { class: 'tx', colspan: '3' }, hk.header)));
    for (const r of splitRows(hk)) {
      if (r.meta) { rows.push(h('tr', { class: 'meta' }, h('td', { class: 'ln' }), h('td', { class: 'tx', colspan: '3' }, r.meta.text))); continue; }
      rows.push(h('tr', null, side(r.left, 'l'), side(r.right, 'r')));
    }
  }
  return h('table', { class: 'dtable split' }, h('tbody', null, rows));
}
