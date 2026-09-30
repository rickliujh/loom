// Files tab: browse and edit a local module. Writes are conditional on the
// etag of the last read, so an edit made elsewhere is never overwritten
// without asking.

import { h, icon, replace, templateSpans } from '../dom.js';
import * as api from '../api.js';
import { setQuiet } from '../router.js';
import { confirmDialog, promptDialog, openModal } from '../components/modal.js';
import { toast, toastError } from '../components/toast.js';
import { copyButton } from '../components/copy.js';
import { indentSelection } from '../components/yaml-field.js';
import { bytes, dirName, baseName } from '../lib/format.js';

const CONFIG = new Set(['loom.yaml', 'loom.jsonnet']);
const READ_VIEW_MAX_LINES = 6000;

/** What a file is to loom, for the badges beside its name. */
export function fileRole(path, excludes = []) {
  const name = baseName(path);
  if (!path.includes('/') && CONFIG.has(name)) return { label: 'config', icon: 'config', cls: 'badge-accent' };
  if (path.startsWith('__functions/patches/') || /(^|\/)patches\//.test(path)) return { label: 'patch', icon: 'patch', cls: 'badge-warn' };
  if (path.startsWith('__functions/')) return { label: 'function', icon: 'gear', cls: '' };
  if (!path.includes('/') && name === 'README.md') return { label: 'not rendered', icon: 'file', cls: '' };
  if (excludes.some((x) => path === x || path.startsWith(`${x}/`))) return { label: 'excluded', icon: 'file', cls: '' };
  if (/\.libsonnet$/.test(name)) return { label: 'library', icon: 'file', cls: '' };
  return { label: 'template', icon: 'file-tpl', cls: 'badge-mauve' };
}

export function renderFiles(el, ctx) {
  const listings = new Map(); // dir path → entries
  const expanded = new Set(['']);
  let selected = ''; // path of the selected tree item
  let open = null; // {path, content, etag, original, truncated, binary}
  let mode = 'read';
  let saving = false;

  const excludes = () => {
    const r = ctx.base && ctx.base.modules && ctx.base.modules[0] && ctx.base.modules[0].module;
    return (r && r.excludes) || [];
  };

  const treeScroll = h('div', { class: 'ft-scroll' });
  const pane = h('div', { class: 'editor-pane' });
  const layout = h('div', { class: 'files-layout' },
    h('div', { class: 'files-tree' },
      h('div', { class: 'ft-tools' },
        h('span', { class: 'title' }, 'Files'),
        h('button', { class: 'btn btn-ghost btn-sm btn-icon', type: 'button', title: 'New file', 'aria-label': 'New file', onClick: () => create('file') }, icon('plus')),
        h('button', { class: 'btn btn-ghost btn-sm btn-icon', type: 'button', title: 'New folder', 'aria-label': 'New folder', onClick: () => create('dir') }, icon('folder')),
        h('button', { class: 'btn btn-ghost btn-sm btn-icon', type: 'button', title: 'Reload the tree', 'aria-label': 'Reload the tree', onClick: () => reloadTree() }, icon('refresh'))),
      treeScroll),
    pane);
  el.appendChild(h('div', { class: 'stack' },
    h('p', { class: 'small muted' }, 'Every file except loom.yaml, README.md and excluded paths is a template: its path and its content are rendered with the params. ',
      h('span', { class: 'tpl' }, '{{ … }}'), ' marks a template action.'),
    layout));

  paintEmptyPane();
  reloadTree().then(() => { if (ctx.query && ctx.query.path) openFile(ctx.query.path); });

  // ---------- Tree ----------

  async function fetchDir(path) {
    const r = await api.files.get(ctx.source, path);
    const entries = (r && r.entries) || [];
    listings.set(path, entries);
    return entries;
  }

  async function reloadTree() {
    const dirs = [...expanded];
    listings.clear();
    try {
      await Promise.all(dirs.map((d) => fetchDir(d).catch(() => expanded.delete(d))));
    } catch (err) {
      replace(treeScroll, h('div', { class: 'card-body small status-err' }, err.message));
      return;
    }
    paintTree();
  }

  function paintTree() {
    const entries = listings.get('');
    if (!entries) { replace(treeScroll, h('div', { class: 'card-body' }, [80, 60, 70, 50].map((w) => h('div', { class: `skel skel-line skel-w-${w === 80 ? 90 : w}` })))); return; }
    if (!entries.length) {
      replace(treeScroll, h('div', { class: 'card-body small muted' }, 'This module directory is empty.'));
      return;
    }
    replace(treeScroll, h('ul', { class: 'ft-list', role: 'tree', 'aria-label': 'Module files' }, items('', 0)));
  }

  function items(dir, depth) {
    const entries = listings.get(dir) || [];
    return entries.map((e) => {
      const path = dir ? `${dir}/${e.name}` : e.name;
      const isDir = e.kind === 'dir';
      const isOpen = isDir && expanded.has(path);
      const role = isDir ? null : fileRole(path, excludes());
      const btn = h('button', {
        class: 'ft-item', type: 'button', role: 'treeitem',
        'aria-expanded': isDir ? String(isOpen) : null,
        'aria-selected': String(selected === path),
        'aria-current': open && open.path === path ? 'true' : null,
        'data-path': path,
        title: path,
        onClick: () => (isDir ? toggle(path) : openFile(path)),
        onKeydown: (ev) => treeKeys(ev, path, isDir, isOpen),
      },
      isDir ? icon('chevron-right', 'chev') : h('span', { class: 'chev-space' }),
      icon(isDir ? (isOpen ? 'folder-open' : 'folder') : (role.label === 'template' && e.name.includes('{{') ? 'file-tpl' : role.icon)),
      h('span', { class: 'nm' }, templateSpans(e.name)),
      !isDir && e.size !== undefined ? h('span', { class: 'sz' }, bytes(e.size)) : null);
      btn.style.setProperty('--depth', String(depth));
      return h('li', { role: 'none' }, btn, isOpen ? h('ul', { class: 'ft-list', role: 'group' }, items(path, depth + 1)) : null);
    });
  }

  function treeKeys(ev, path, isDir, isOpen) {
    const all = [...treeScroll.querySelectorAll('.ft-item')];
    const i = all.indexOf(ev.currentTarget);
    if (ev.key === 'ArrowDown') { ev.preventDefault(); if (all[i + 1]) all[i + 1].focus(); }
    else if (ev.key === 'ArrowUp') { ev.preventDefault(); if (all[i - 1]) all[i - 1].focus(); }
    else if (ev.key === 'ArrowRight' && isDir && !isOpen) { ev.preventDefault(); toggle(path); }
    else if (ev.key === 'ArrowLeft' && isDir && isOpen) { ev.preventDefault(); toggle(path); }
    else if (ev.key === 'Delete') { ev.preventDefault(); selected = path; remove(path, isDir); }
    else if (ev.key === 'F2') { ev.preventDefault(); rename(path); }
  }

  async function toggle(path) {
    selected = path;
    if (expanded.has(path)) {
      expanded.delete(path);
    } else {
      expanded.add(path);
      if (!listings.has(path)) {
        try { await fetchDir(path); } catch (err) { expanded.delete(path); toastError(err, `Could not open ${path}`); }
      }
    }
    paintTree();
    focusItem(path);
  }

  function focusItem(path) {
    const b = [...treeScroll.querySelectorAll('.ft-item')].find((x) => x.dataset.path === path);
    if (b) b.focus();
  }

  // ---------- File pane ----------

  function paintEmptyPane() {
    layout.classList.remove('has-file');
    replace(pane, h('div', { class: 'editor-empty' }, h('div', { class: 'empty' }, icon('file', 'icon-xl'),
      h('h3', null, 'Pick a file'),
      h('p', null, 'Open a file from the tree to read it with its template actions marked, or to edit it. Use + to add a file or folder.'))));
  }

  function isDirty() { return !!open && mode === 'edit' && open.content !== open.original; }

  async function openFile(path, { force = false } = {}) {
    if (!force && isDirty() && open.path !== path) {
      const ok = await confirmDialog({ title: 'Discard unsaved changes?', message: `${open.path} has changes that are not saved.`, confirmLabel: 'Discard', danger: true });
      if (!ok) return;
    }
    selected = path;
    // Expand the parents so the tree shows where the file lives.
    const parts = path.split('/');
    for (let i = 1; i < parts.length; i++) {
      const d = parts.slice(0, i).join('/');
      if (!expanded.has(d)) {
        expanded.add(d);
        if (!listings.has(d)) { try { await fetchDir(d); } catch { /* shown below */ } }
      }
    }
    layout.classList.add('has-file');
    replace(pane, h('div', { class: 'editor-head' }, h('span', { class: 'path' }, path)), h('div', { class: 'editor-body' }, h('div', { class: 'read-view card-body' }, [90, 70, 80, 40, 60].map((w) => h('div', { class: `skel skel-line skel-w-${w === 80 ? 70 : w === 40 ? 30 : w === 60 ? 50 : w}` })))));
    try {
      const r = await api.files.get(ctx.source, path);
      if (r.kind === 'dir') { toggle(path); paintEmptyPane(); return; }
      open = { path, content: r.content || '', original: r.content || '', etag: r.etag, truncated: !!r.truncated, binary: !!r.binary };
      mode = open.binary || open.truncated ? 'read' : (CONFIG.has(path) ? 'edit' : 'read');
      setQuiet(`/m/${encodeURIComponent(ctx.source)}/files`, { path });
      paintTree();
      paintPane();
    } catch (err) {
      open = null;
      replace(pane, h('div', { class: 'card-body' }, h('div', { class: 'banner banner-err' }, icon('alert'), h('div', { class: 'banner-body' },
        h('div', { class: 'banner-title' }, `Could not open ${path}`), err.message))));
    }
  }

  function paintPane() {
    if (!open) { paintEmptyPane(); return; }
    const role = fileRole(open.path, excludes());
    const dirtyEl = h('span', { class: 'dirty', hidden: !isDirty() }, icon('dot', 'icon-sm'), 'unsaved');
    const saveBtn = h('button', { class: 'btn btn-primary btn-sm', type: 'button', disabled: !isDirty(), onClick: () => save() }, icon('save', 'icon-sm'), 'Save');
    const editable = !open.binary && !open.truncated;
    const seg = h('div', { class: 'seg', role: 'group', 'aria-label': 'View' },
      h('button', { type: 'button', 'aria-pressed': String(mode === 'read'), onClick: () => setMode('read') }, icon('eye', 'icon-sm'), 'Read'),
      h('button', { type: 'button', 'aria-pressed': String(mode === 'edit'), disabled: !editable, onClick: () => setMode('edit') }, icon('edit', 'icon-sm'), 'Edit'));

    const head = h('div', { class: 'editor-head' },
      h('button', { class: 'btn btn-ghost btn-sm only-narrow', type: 'button', onClick: async () => { if (isDirty() && !(await confirmDialog({ title: 'Discard unsaved changes?', message: `${open.path} has changes that are not saved.`, confirmLabel: 'Discard', danger: true }))) return; open = null; paintEmptyPane(); } }, icon('chevron-left', 'icon-sm'), 'Files'),
      h('span', { class: 'path' }, templateSpans(open.path)),
      h('span', { class: ['badge', role.cls] }, role.label),
      open.binary ? h('span', { class: 'badge' }, 'binary') : null,
      open.truncated ? h('span', { class: 'badge badge-warn' }, 'truncated') : null,
      dirtyEl,
      h('span', { class: 'spacer' }),
      editable ? seg : null,
      editable && mode === 'edit' ? saveBtn : null,
      copyButton(() => open.path, { label: 'Copy path', what: 'Path copied' }),
      h('button', { class: 'btn btn-ghost btn-sm btn-icon', type: 'button', title: 'Rename or move', 'aria-label': 'Rename or move', onClick: () => rename(open.path) }, icon('rename')),
      CONFIG.has(open.path) ? null : h('button', { class: 'btn btn-ghost btn-sm btn-icon', type: 'button', title: 'Delete', 'aria-label': 'Delete file', onClick: () => remove(open.path, false) }, icon('trash')));

    let body;
    let foot = null;
    if (open.binary) {
      body = h('div', { class: 'editor-empty' }, h('div', { class: 'empty' }, icon('file', 'icon-xl'), h('h3', null, 'Binary file'), h('p', null, 'Binary files are copied as they are; they cannot be shown or edited here.')));
    } else if (mode === 'edit') {
      const { el: editor, ta } = codeEditor(open.content, (v) => {
        open.content = v;
        const d = isDirty();
        dirtyEl.hidden = !d;
        saveBtn.disabled = !d;
      }, () => save());
      body = editor;
      foot = h('div', { class: 'editor-foot' }, h('span', null, h('span', { class: 'kbd' }, 'Ctrl'), '+', h('span', { class: 'kbd' }, 'S'), ' saves'), h('span', null, 'Tab indents · Esc then Tab leaves the editor'));
      setTimeout(() => ta.focus(), 0);
    } else {
      body = readView(open.content);
      if (open.truncated) foot = h('div', { class: 'editor-foot status-warn' }, icon('alert', 'icon-sm'), 'Only the first 1 MiB is shown. Files this large cannot be edited through loom serve.');
    }
    replace(pane, head, h('div', { class: 'editor-body' }, body), foot);
  }

  async function setMode(m) {
    if (m === mode) return;
    mode = m;
    paintPane();
  }

  async function save({ overwrite = false } = {}) {
    if (!open || saving) return;
    saving = true;
    try {
      let etag = open.etag;
      if (overwrite) {
        const fresh = await api.files.get(ctx.source, open.path);
        etag = fresh.etag;
      }
      const r = await api.files.put(ctx.source, open.path, open.content, etag);
      open.etag = (r && r.etag) || etag;
      open.original = open.content;
      toast(`Saved ${open.path}`, { kind: 'ok', timeout: 2000 });
      paintPane();
      if (CONFIG.has(open.path)) toast('The module config changed. Reload the page to see new params in the form.', { timeout: 6000 });
    } catch (err) {
      if (err instanceof api.ApiError && err.status === 409) conflict();
      else toastError(err, `Could not save ${open.path}`);
    } finally {
      saving = false;
    }
  }

  function conflict() {
    const m = openModal({
      title: 'This file changed on disk',
      body: h('div', { class: 'stack' },
        h('p', null, `${open.path} was changed somewhere else after you opened it. Saving now would replace those changes.`),
        h('p', { class: 'small muted' }, 'Tip: copy your text first if you want to merge by hand.')),
      footer: [
        h('button', { class: 'btn', type: 'button', onClick: () => m.close() }, 'Keep editing'),
        h('button', { class: 'btn', type: 'button', onClick: () => { m.close(); openFile(open.path, { force: true }); } }, 'Discard mine, load theirs'),
        h('button', { class: 'btn btn-danger-solid', type: 'button', onClick: () => { m.close(); save({ overwrite: true }); } }, 'Overwrite with mine'),
      ],
    });
  }

  // ---------- Create, rename, delete ----------

  function targetDir() {
    if (!selected) return '';
    const entries = listings.get(dirName(selected) === '/' ? '' : dirName(selected)) || [];
    const e = entries.find((x) => x.name === baseName(selected));
    if (e && e.kind === 'dir') return selected;
    return dirName(selected) === '/' ? '' : dirName(selected);
  }

  const validPath = (v) => {
    if (!v) return 'Enter a path.';
    if (v.startsWith('/')) return 'Use a path relative to the module.';
    if (v.split('/').some((s) => s === '..' || s === '.git')) return 'The path may not contain “..” or “.git”.';
    return '';
  };

  async function create(kind) {
    const dir = targetDir();
    const path = await promptDialog({
      title: kind === 'dir' ? 'New folder' : 'New file',
      label: 'Path inside the module', value: dir ? `${dir}/` : '', mono: true,
      placeholder: kind === 'dir' ? 'templates/apps' : 'templates/{{ .serviceName }}.yaml',
      help: 'Folders and file names may use template actions, for example {{ .serviceName }}.',
      validate: validPath, confirmLabel: 'Create',
    });
    if (!path) return;
    const clean = path.replace(/\/+$/, '');
    try {
      await api.files.create(ctx.source, clean, kind);
      const parent = clean.includes('/') ? clean.slice(0, clean.lastIndexOf('/')) : '';
      expanded.add(parent);
      await reloadTree();
      if (kind === 'file') { await openFile(clean, { force: true }); setMode('edit'); } else { selected = clean; paintTree(); focusItem(clean); }
    } catch (err) {
      toastError(err, `Could not create ${clean}`);
    }
  }

  async function rename(from) {
    if (open && open.path === from && isDirty()) { toast('Save or discard your changes before renaming this file.', { kind: 'error' }); return; }
    const to = await promptDialog({
      title: 'Rename or move', label: 'New path inside the module', value: from, mono: true,
      validate: (v) => validPath(v) || (v === from ? 'That is the current path.' : ''), confirmLabel: 'Rename',
    });
    if (!to) return;
    try {
      await api.files.move(ctx.source, from, to);
      if (open && (open.path === from || open.path.startsWith(`${from}/`))) open.path = to + open.path.slice(from.length);
      selected = to;
      const parent = to.includes('/') ? to.slice(0, to.lastIndexOf('/')) : '';
      expanded.add(parent);
      await reloadTree();
      if (open) paintPane();
      toast(`Renamed to ${to}`, { kind: 'ok' });
    } catch (err) {
      toastError(err, 'Could not rename');
    }
  }

  async function remove(path, isDir) {
    if (CONFIG.has(path)) { toast(`${path} cannot be deleted through loom serve; edit it instead.`, { kind: 'error' }); return; }
    const ok = await confirmDialog({
      title: `Delete ${isDir ? 'folder' : 'file'}?`,
      message: isDir ? `${path} will be deleted. Only an empty folder can be deleted.` : `${path} will be deleted from the module directory. This cannot be undone here.`,
      confirmLabel: 'Delete', danger: true,
    });
    if (!ok) return;
    try {
      await api.files.remove(ctx.source, path);
      if (open && open.path === path) { open = null; paintEmptyPane(); }
      selected = '';
      await reloadTree();
      toast(`Deleted ${path}`, { kind: 'ok' });
    } catch (err) {
      toastError(err, `Could not delete ${path}`);
    }
  }

  // Ctrl/Cmd+S while this tab is visible.
  const onKey = (e) => {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's' && !el.hidden && open && mode === 'edit') {
      e.preventDefault();
      save();
    }
  };
  document.addEventListener('keydown', onKey);

  return {
    shown(q) { if (q && q.path && (!open || open.path !== q.path)) openFile(q.path); },
    canLeave(unload) {
      if (!isDirty()) return true;
      if (unload) return false;
      return window.confirm(`Discard unsaved changes to ${open.path}?`);
    },
    destroy() { document.removeEventListener('keydown', onKey); },
  };
}

function readView(content) {
  const lines = content.split('\n');
  if (lines.length > 1 && lines[lines.length - 1] === '') lines.pop();
  if (lines.length > READ_VIEW_MAX_LINES) {
    return h('div', { class: 'read-view' }, h('pre', { class: 'lc' }, content));
  }
  return h('div', { class: 'read-view', tabindex: '0', 'aria-label': 'File content' }, h('table', null, h('tbody', null,
    lines.map((l, i) => h('tr', null, h('td', { class: 'ln' }, String(i + 1)), h('td', { class: 'lc' }, templateSpans(l)))))));
}

/** A textarea with a line-number gutter. */
function codeEditor(value, onChange, onSave) {
  const gutter = h('div', { class: 'gutter', 'aria-hidden': 'true' });
  const ta = h('textarea', { spellcheck: 'false', autocomplete: 'off', autocapitalize: 'off', 'aria-label': 'File content', wrap: 'off' });
  ta.value = value;
  let lineCount = 0;
  const paintGutter = () => {
    const n = ta.value.split('\n').length;
    if (n === lineCount) return;
    lineCount = n;
    gutter.textContent = Array.from({ length: n }, (_, i) => i + 1).join('\n');
  };
  let escArmed = false;
  ta.addEventListener('input', () => { paintGutter(); onChange(ta.value); });
  ta.addEventListener('scroll', () => { gutter.scrollTop = ta.scrollTop; });
  ta.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') { escArmed = true; return; }
    if (e.key === 'Tab' && !escArmed && !e.ctrlKey && !e.metaKey && !e.altKey) {
      e.preventDefault();
      indentSelection(ta, e.shiftKey);
      ta.dispatchEvent(new Event('input'));
      return;
    }
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') { e.preventDefault(); e.stopPropagation(); onSave(); return; }
    escArmed = false;
  });
  paintGutter();
  const el = h('div', { class: 'code-editor' }, gutter, ta);
  return { el, ta };
}

