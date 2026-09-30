// Params & Run tab: a form generated from the module's declarations, live
// checks, presets, import/export, and the run actions.
//
// Values leave this form as typed. A string param sends its text; a list or
// map param sends its YAML text, which the server parses exactly as `-p`
// would. Nothing structured is ever rebuilt from JSON here.

import { h, icon, replace, debounce, downloadText, uid } from '../dom.js';
import * as api from '../api.js';
import { state, emit, prefs } from '../store.js';
import { navigate, href, setQuiet } from '../router.js';
import { typeBadge } from '../components/chip.js';
import { yamlField } from '../components/yaml-field.js';
import { menuButton } from '../components/menu.js';
import { confirmDialog, promptDialog } from '../components/modal.js';
import { toast, toastError } from '../components/toast.js';
import { showCli } from '../components/cli.js';
import { confirmExecute } from './confirm-execute.js';
import { splitTopLevel, valueText, scalarText, composeParams } from '../lib/yaml-view.js';
import { plural, relTime, baseName } from '../lib/format.js';
import { rootOf } from './module.js';
import { paramStateLabel } from './overview.js';

const STRUCT = (t) => t === 'list' || t === 'map';

export function renderParamsTab(el, ctx) {
  const root = rootOf(ctx.base);
  const decls = (root && root.params) || [];
  const fields = new Map();
  let lastCheck = null;
  let checkSeq = 0;
  let inspectSeq = 0;
  let presetsList = [];
  let dirty = false;

  const runChecks = debounce(check, 300);
  const runInspect = debounce(liveInspect, 650);
  const changed = () => { dirty = true; runChecks(); runInspect(); };

  // ---------- Advanced options ----------
  const adv = {
    author: h('input', { class: 'input', id: uid('adv'), placeholder: 'from loom.yaml, else git config', autocomplete: 'off' }),
    email: h('input', { class: 'input', id: uid('adv'), placeholder: 'from loom.yaml, else git config', autocomplete: 'off', type: 'email' }),
    targetPath: h('input', { class: 'input mono', id: uid('adv'), placeholder: 'managed workspace', autocomplete: 'off', spellcheck: 'false' }),
    keep: h('input', { type: 'checkbox' }),
  };

  // ---------- Fields ----------
  const listEl = h('div', { class: 'param-list' });
  if (!decls.length) {
    listEl.appendChild(h('div', { class: 'empty empty-compact' }, icon('sliders', 'icon-xl'),
      h('h3', null, 'This module declares no parameters'),
      h('p', null, 'It runs the same way every time. Preview it or run it from the panel.')));
  }
  for (const d of decls) fields.set(d.name, buildField(d));
  fields.forEach((f) => listEl.appendChild(f.el));

  // ---------- Side panel ----------
  const targetBox = h('div', { class: 'target-view' });
  const checkBox = h('div', { class: 'check-summary', 'aria-live': 'polite' });
  const lastPreviewEl = h('div', { class: 'small muted' });

  const runBtn = h('button', { class: 'btn btn-primary', type: 'button' }, icon('play'), 'Run', icon('chevron-down', 'icon-sm'));
  const runMenu = menuButton(runBtn, [
    { icon: 'play', title: 'Execute', desc: 'Push branches and open pull requests. You confirm first.', onSelect: () => act('execute') },
    { icon: 'eye', title: 'Dry run', desc: 'Walk every step, write and push nothing.', onSelect: () => act('dry-run') },
    { icon: 'folder', title: 'Local run', desc: 'Render and commit into a local workspace. No push, no PR.', onSelect: () => act('local') },
  ], { align: 'right' });

  // On a narrow screen the side panel sits below the form; keep the main
  // actions within reach at the bottom of the viewport.
  const runBtn2 = h('button', { class: 'btn btn-primary', type: 'button' }, icon('play'), 'Run', icon('chevron-down', 'icon-sm'));
  const mobileBar = h('div', { class: 'mobile-actions', role: 'group', 'aria-label': 'Run actions' },
    h('button', { class: 'btn', type: 'button', onClick: () => act('quick') }, icon('eye'), 'Preview'),
    h('button', { class: 'btn', type: 'button', onClick: () => act('full') }, icon('diff'), 'Full diff'),
    menuButton(runBtn2, [
      { icon: 'play', title: 'Execute', desc: 'Push and open pull requests, after you confirm.', onSelect: () => act('execute') },
      { icon: 'eye', title: 'Dry run', desc: 'Write and push nothing.', onSelect: () => act('dry-run') },
      { icon: 'folder', title: 'Local run', desc: 'Commit into a local workspace.', onSelect: () => act('local') },
    ], { up: true }));

  const side = h('aside', { class: 'run-side' },
    h('div', { class: 'card' },
      h('div', { class: 'card-body' }, h('div', { class: 'section-title' }, 'Resolved target'), targetBox),
      h('div', { class: 'card-body' }, h('div', { class: 'section-title' }, 'Checks'), checkBox),
      h('div', { class: 'card-body stack' },
        h('div', { class: 'action-bar' },
          h('button', { class: 'btn', type: 'button', title: 'Simulate the run and show newFiles/patch diffs. Executes nothing.', onClick: () => act('quick') }, icon('eye'), 'Quick preview'),
          h('button', { class: 'btn', type: 'button', title: 'Run locally, including shell steps, and diff each target. Pushes nothing.', onClick: () => act('full') }, icon('diff'), 'Full diff'),
          runMenu),
        h('button', { class: 'btn btn-ghost btn-sm', type: 'button', onClick: () => cliDialog() }, icon('terminal', 'icon-sm'), 'Copy as CLI'),
        lastPreviewEl),
      h('details', { class: 'card-body' },
        h('summary', { class: 'section-title' }, icon('gear', 'icon-sm'), 'Advanced'),
        h('div', { class: 'stack mt-2' },
          fieldWrap('Commit author', adv.author, 'Default for commitPush when loom.yaml sets none (--author).'),
          fieldWrap('Commit email', adv.email, 'Default for commitPush (--email).'),
          fieldWrap('Local run directory', adv.targetPath, 'Only for Local run. An absolute directory inside a root; empty uses a workspace the server manages.'),
          h('label', { class: 'check' }, adv.keep, 'Keep the full-diff workspace for inspection')))));

  // ---------- Presets bar ----------
  const presetSel = h('select', { class: 'select select-sm', 'aria-label': 'Preset' });
  const fileInput = h('input', { type: 'file', accept: '.yaml,.yml,text/yaml,application/yaml', class: 'sr-only', tabindex: '-1', onChange: () => importFile() });
  const presetBar = h('div', { class: 'preset-bar', role: 'group', 'aria-label': 'Presets and files' },
    h('span', { class: 'label' }, 'Preset'),
    presetSel,
    h('button', { class: 'btn btn-sm', type: 'button', onClick: () => loadPreset() }, 'Load'),
    h('button', { class: 'btn btn-sm', type: 'button', onClick: () => savePreset() }, icon('save', 'icon-sm'), 'Save as…'),
    h('button', { class: 'btn btn-sm btn-ghost', type: 'button', 'aria-label': 'Delete preset', title: 'Delete preset', onClick: () => deletePreset() }, icon('trash', 'icon-sm')),
    h('span', { class: 'spacer' }),
    h('button', { class: 'btn btn-sm', type: 'button', onClick: () => fileInput.click() }, icon('upload', 'icon-sm'), 'Import'),
    h('button', { class: 'btn btn-sm', type: 'button', onClick: () => exportFile() }, icon('download', 'icon-sm'), 'Export'),
    fileInput);

  const noticeEl = h('div');
  el.appendChild(h('div', { class: 'run-layout' },
    h('section', { 'aria-label': 'Parameters' },
      noticeEl,
      presetBar,
      h('div', { class: 'card' }, h('div', { class: 'card-body' }, listEl))),
    side,
    mobileBar));

  ctx.getParams = collect;
  paintTarget(ctx.base);
  paintChecks();
  paintLastPreview();
  loadPresets();
  prefillFromJob(ctx.query && ctx.query.job);

  if (decls.length) check();

  // ---------- Field construction ----------

  function buildField(d) {
    const id = uid('p');
    const helpId = `${id}-help`;
    const errId = `${id}-err`;
    const dynamic = d.state === 'dynamic';
    const structured = STRUCT(d.type);
    const errEl = h('div', { class: 'field-error', id: errId, 'aria-live': 'polite' });
    const stateEl = h('span', { class: ['small', `st-${d.state}`] }, paramStateLabel(d.state));
    const f = { decl: d, dynamic, structured, override: false, errEl, stateEl };

    const defaultText = structured
      ? (d.valueYaml && d.state === 'default' ? d.valueYaml : '')
      : (typeof d.default === 'string' ? d.default : '');

    let control;
    if (structured) {
      f.yaml = yamlField({
        id, kind: d.type, value: '', placeholder: defaultText,
        describedBy: [helpId, errId].join(' '),
        onInput: changed,
        onStatus: () => paintFieldError(f),
      });
      control = f.yaml.el;
      f.get = () => f.yaml.get();
      f.set = (t) => f.yaml.set(t);
    } else {
      const input = h('input', {
        class: 'input mono', id, placeholder: defaultText, autocomplete: 'off', spellcheck: 'false',
        'aria-describedby': [helpId, errId].join(' '), 'aria-required': d.required && !dynamic ? 'true' : null,
        onInput: changed,
      });
      control = input;
      f.get = () => input.value;
      f.set = (t) => { input.value = t ?? ''; };
      f.input = input;
    }

    const help = [];
    if (dynamic) help.push('Computed at run time by the command above.');
    else if (d.state === 'default') help.push(structured ? 'Leave empty to use the default shown.' : `Default: ${JSON.stringify(d.default)} — leave empty to use it.`);
    else if (d.required) help.push('Required.');
    else help.push('Optional.');
    if (structured) help.push(`YAML ${d.type}, sent as written.`);

    let body;
    if (dynamic) {
      const warn = h('div', { class: 'banner banner-warn', hidden: true }, icon('alert'), h('div', { class: 'banner-body small' },
        'With an override the command will not run; this value is used instead.'));
      const sw = h('input', { type: 'checkbox', 'aria-controls': id, onChange: () => {
        f.override = sw.checked;
        control.hidden = !sw.checked;
        warn.hidden = !sw.checked;
        if (sw.checked) (f.input || f.yaml.textarea).focus();
        changed();
      } });
      control.hidden = true;
      f.setOverride = (on) => { sw.checked = on; f.override = on; control.hidden = !on; warn.hidden = !on; };
      body = [
        h('div', { class: 'p-cmd' }, h('span', { class: 'dollar' }, '$'), h('span', { class: 'grow' }, d.command || '')),
        h('label', { class: 'switch' }, sw, h('span', { class: 'track' }), 'Override the command'),
        warn, control];
    } else {
      body = [control];
    }

    f.el = h('div', { class: 'param', 'data-param': d.name },
      h('div', { class: 'p-head' },
        h('label', { class: 'p-name', for: id }, d.name, d.required && !dynamic ? h('span', { class: 'req', 'aria-label': 'required' }, '*') : null),
        h('div', { class: 'p-tags' }, typeBadge(d.type), dynamic ? h('span', { class: 'badge badge-mauve' }, 'dynamic') : null, stateEl)),
      h('div', { class: 'p-body' }, body, h('div', { class: 'field-help', id: helpId }, help.join(' ')), errEl));
    return f;
  }

  function fieldWrap(label, input, help) {
    return h('div', { class: 'field' }, h('label', { class: 'field-label', for: input.id }, label), input, h('div', { class: 'field-help' }, help));
  }

  // ---------- Values ----------

  /** The params object sent to the server: only what the user supplied. */
  function collect({ forInspect = false } = {}) {
    const out = {};
    for (const [name, f] of fields) {
      if (f.dynamic && !f.override) continue;
      const v = f.get();
      if (f.structured) {
        if (!v.trim()) continue;
        if (forInspect && f.yaml.status.state === 'error') continue;
        out[name] = v;
      } else if (v !== '' || (f.dynamic && f.override)) {
        out[name] = v;
      }
    }
    return out;
  }

  function paintFieldError(f) {
    const ys = f.yaml ? f.yaml.status : null;
    const ce = lastCheck && lastCheck.fields && lastCheck.fields[f.decl.name];
    // The YAML field shows its own parse error; the check adds type errors.
    if (ce && ce.ok === false && !(ys && ys.state === 'error')) {
      replace(f.errEl, icon('alert', 'icon-sm'), ce.error);
      if (f.input) f.input.setAttribute('aria-invalid', 'true');
    } else {
      replace(f.errEl);
      if (f.input) f.input.removeAttribute('aria-invalid');
    }
  }

  async function check() {
    const my = ++checkSeq;
    try {
      const r = await api.paramsCheck({ source: ctx.source, params: collect() });
      if (my !== checkSeq) return;
      lastCheck = r || {};
    } catch (err) {
      if (my !== checkSeq) return;
      lastCheck = { failed: err.message };
    }
    fields.forEach(paintFieldError);
    paintChecks();
  }

  async function liveInspect() {
    const my = ++inspectSeq;
    try {
      const r = await api.inspect({ source: ctx.source, params: collect({ forInspect: true }), depth: 1, modules: [], noFetch: false });
      if (my !== inspectSeq) return;
      ctx.setLive(r);
      paintTarget(r);
      const node = rootOf(r);
      for (const p of (node && node.params) || []) {
        const f = fields.get(p.name);
        if (!f) continue;
        f.stateEl.className = `small st-${p.state}`;
        f.stateEl.textContent = paramStateLabel(p.state);
      }
    } catch (err) {
      if (my !== inspectSeq) return;
      replace(targetBox, h('div', { class: 'field-error' }, icon('alert', 'icon-sm'), err.message));
    }
  }

  function paintTarget(report) {
    const node = rootOf(report);
    const t = node && node.target;
    if (!t || !t.url) {
      replace(targetBox, h('div', { class: 'tv-line muted' }, icon('folder', 'icon-sm'),
        h('span', null, 'No target repository. A run writes into a directory; Local run uses a managed workspace unless you set one under Advanced.')));
      return;
    }
    const val = (s) => h('span', { class: ['tv-val', /\{\{/.test(s || '') && 'unresolved'] }, s || '—');
    replace(targetBox,
      h('div', { class: 'tv-line' }, icon('repo', 'icon-sm'), val(t.url)),
      h('div', { class: 'tv-line' }, icon('branch', 'icon-sm'), val(t.branch), h('span', { class: 'faint' }, '→'), val(t.featureBranch)),
      /\{\{/.test(`${t.url}${t.featureBranch}`) ? h('div', { class: 'small status-warn' }, 'Parts in braces resolve once their params are known.') : null);
  }

  function problems() {
    const out = { yaml: [], type: [], missing: [], undeclared: [] };
    for (const [name, f] of fields) {
      if (f.yaml && f.yaml.status.state === 'error' && (!f.dynamic || f.override)) out.yaml.push(name);
    }
    if (lastCheck && lastCheck.fields) {
      for (const [name, r] of Object.entries(lastCheck.fields)) if (r && r.ok === false && !out.yaml.includes(name)) out.type.push(name);
    }
    if (lastCheck && lastCheck.missing) out.missing = lastCheck.missing;
    if (lastCheck && lastCheck.undeclared) out.undeclared = lastCheck.undeclared;
    return out;
  }

  function paintChecks() {
    if (!decls.length) {
      replace(checkBox, line('ok', 'check-circle', 'Nothing to supply.'));
      return;
    }
    if (!lastCheck) { replace(checkBox, h('div', { class: 'skel skel-line skel-w-70' })); return; }
    if (lastCheck.failed) { replace(checkBox, line('err', 'alert', `Could not check the values: ${lastCheck.failed}`)); return; }
    const p = problems();
    const lines = [];
    if (p.missing.length) lines.push(line('err', 'alert', [`Missing ${plural(p.missing.length, 'required param')}: `, p.missing.map((n, i) => [i ? ', ' : '', jumpLink(n)])]));
    if (p.yaml.length) lines.push(line('err', 'alert', ['YAML errors in ', p.yaml.map((n, i) => [i ? ', ' : '', jumpLink(n)])]));
    if (p.type.length) lines.push(line('err', 'alert', ['Wrong type: ', p.type.map((n, i) => [i ? ', ' : '', jumpLink(n)])]));
    if (p.undeclared.length) lines.push(line('warn', 'alert', `Not declared by this module: ${p.undeclared.join(', ')}`));
    if (!lines.length) lines.push(line('ok', 'check-circle', 'Every required param is supplied and every value fits its type.'));
    replace(checkBox, lines);
  }

  function line(kind, ic, content) {
    return h('div', { class: 'cs-line' }, h('span', { class: `status-${kind}` }, icon(ic, 'icon-sm')), h('span', null, content));
  }

  function jumpLink(name) {
    return h('button', { class: 'link-btn mono', type: 'button', onClick: () => focusField(name) }, name);
  }

  function focusField(name) {
    const f = fields.get(name);
    if (!f) return;
    f.el.scrollIntoView({ block: 'center', behavior: 'smooth' });
    (f.input || (f.yaml && f.yaml.textarea) || f.el).focus({ preventScroll: true });
  }

  function paintLastPreview() {
    const lp = state.lastPreview.get(ctx.source);
    if (!lp) { replace(lastPreviewEl, 'Tip: Quick preview executes nothing — a safe first step.'); return; }
    const same = JSON.stringify(lp.params) === JSON.stringify(collect());
    replace(lastPreviewEl, icon('clock', 'icon-sm'), ' Last preview ', relTime(lp.at), ' · ',
      h('a', { href: href(`/jobs/${encodeURIComponent(lp.id)}/changes`) }, 'view'),
      same ? '' : ' · params changed since');
  }

  // ---------- Actions ----------

  function blockers() {
    const p = problems();
    const msgs = [];
    if (p.yaml.length) msgs.push(`fix the YAML in ${p.yaml.join(', ')}`);
    if (p.type.length) msgs.push(`fix the type of ${p.type.join(', ')}`);
    if (p.missing.length) msgs.push(`supply ${p.missing.join(', ')}`);
    return { msgs, first: p.yaml[0] || p.type[0] || p.missing[0] };
  }

  function request(kind) {
    const params = collect();
    const author = adv.author.value.trim();
    const email = adv.email.value.trim();
    switch (kind) {
      case 'quick': return { kind: 'diff', source: ctx.source, params, quick: true, keepWorkspace: false, author, email };
      case 'full': return { kind: 'diff', source: ctx.source, params, quick: false, keepWorkspace: adv.keep.checked, author, email };
      case 'local': return { kind: 'run', source: ctx.source, params, mode: 'local', targetPath: adv.targetPath.value.trim(), author, email };
      default: return { kind: 'run', source: ctx.source, params, mode: kind, targetPath: '', author, email };
    }
  }

  async function act(kind) {
    await check();
    const b = blockers();
    if (b.msgs.length) {
      toast(`Before running: ${b.msgs.join('; ')}.`, { kind: 'error' });
      if (b.first) focusField(b.first);
      return;
    }
    const tp = adv.targetPath.value.trim();
    if (kind === 'local' && tp && !tp.startsWith('/')) {
      toast('The local run directory must be an absolute path inside a root.', { kind: 'error' });
      adv.targetPath.focus();
      return;
    }
    const req = request(kind);
    if (kind === 'execute') {
      const ok = await confirmExecute(ctx, req);
      if (!ok) return;
    }
    try {
      const r = await api.jobs.create(req);
      emit('jobs-changed');
      if (req.kind === 'diff') state.lastPreview.set(ctx.source, { id: r.id, at: new Date().toISOString(), params: req.params });
      dirty = false;
      navigate(`/jobs/${encodeURIComponent(r.id)}${req.kind === 'diff' ? '/changes' : ''}`);
    } catch (err) {
      const lines = api.errorLines(err);
      toast(lines.length ? `${err.message} — ${lines.join('; ')}` : err.message, { kind: 'error' });
    }
  }

  const CLI_CHOICES = [
    { id: 'quick', label: 'Quick preview' }, { id: 'full', label: 'Full diff' },
    { id: 'execute', label: 'Execute' }, { id: 'dry-run', label: 'Dry run' }, { id: 'local', label: 'Local run' },
  ];

  async function cliDialog() {
    const current = prefs.get('cli.choice', 'execute');
    try {
      const r = await api.cli(request(current));
      showCli(r, {
        title: 'Copy as CLI', choices: CLI_CHOICES, current,
        onChoose: (id) => { prefs.set('cli.choice', id); return api.cli(request(id)); },
      });
    } catch (err) {
      toastError(err, 'Could not build the command');
    }
  }

  // ---------- Presets, import, export ----------

  async function loadPresets(select) {
    try {
      const r = await api.presets.list(ctx.source);
      presetsList = (r && r.presets) || [];
    } catch {
      presetsList = [];
    }
    replace(presetSel,
      h('option', { value: '' }, presetsList.length ? 'Choose a preset…' : 'No presets yet'),
      presetsList.map((p) => h('option', { value: p.name, selected: p.name === select }, p.name)));
  }

  async function loadPreset() {
    const name = presetSel.value;
    if (!name) { toast('Choose a preset first, or save the current values as one.'); presetSel.focus(); return; }
    if (dirty && !(await confirmDialog({ title: 'Replace the current values?', message: `Loading “${name}” replaces what is in the form.`, confirmLabel: 'Load preset' }))) return;
    try {
      const p = await api.presets.get(ctx.source, name);
      await applyDocument(p.yaml || '', p.params || {});
      toast(`Loaded preset “${name}”`, { kind: 'ok' });
    } catch (err) {
      toastError(err, `Could not load “${name}”`);
    }
  }

  async function savePreset() {
    const b = blockers();
    if (b.msgs.filter((m) => !m.startsWith('supply')).length) {
      toast(`Fix the values first: ${b.msgs.join('; ')}.`, { kind: 'error' });
      return;
    }
    const name = await promptDialog({
      title: 'Save preset', label: 'Preset name', value: presetSel.value || '', placeholder: 'prod', mono: true,
      help: 'Saved on the server, outside the module, for this module only.',
      validate: (v) => (!v ? 'Name the preset.' : /[/\\]/.test(v) ? 'A preset name cannot contain slashes.' : ''),
    });
    if (!name) return;
    if (presetsList.some((p) => p.name === name) && !(await confirmDialog({ title: `Overwrite “${name}”?`, message: 'A preset with this name exists. Saving replaces it.', confirmLabel: 'Overwrite' }))) return;
    try {
      const yaml = await composeYaml();
      await api.presets.save(ctx.source, name, yaml);
      await loadPresets(name);
      dirty = false;
      toast(`Saved preset “${name}”`, { kind: 'ok' });
    } catch (err) {
      toastError(err, 'Could not save the preset');
    }
  }

  async function deletePreset() {
    const name = presetSel.value;
    if (!name) { toast('Choose the preset to delete.'); presetSel.focus(); return; }
    if (!(await confirmDialog({ title: `Delete “${name}”?`, message: 'The preset is removed from the server. The form keeps its current values.', confirmLabel: 'Delete preset', danger: true }))) return;
    try {
      await api.presets.remove(ctx.source, name);
      await loadPresets();
      toast(`Deleted preset “${name}”`, { kind: 'ok' });
    } catch (err) {
      toastError(err, 'Could not delete the preset');
    }
  }

  async function importFile() {
    const file = fileInput.files && fileInput.files[0];
    fileInput.value = '';
    if (!file) return;
    if (file.size > 1024 * 1024) { toast('That file is larger than 1 MiB; a params file should be small.', { kind: 'error' }); return; }
    const text = await file.text();
    try {
      const r = await api.yamlParse(text);
      if (r.error) { toast(`${file.name}: line ${r.error.line}, column ${r.error.col}: ${r.error.message}`, { kind: 'error' }); return; }
      if (!r.value || typeof r.value !== 'object' || Array.isArray(r.value)) { toast(`${file.name} is not a params file: it must map param names to values.`, { kind: 'error' }); return; }
      await applyDocument(text, r.value);
      toast(`Imported ${file.name}`, { kind: 'ok' });
    } catch (err) {
      toastError(err, `Could not import ${file.name}`);
    }
  }

  async function exportFile() {
    try {
      const yaml = await composeYaml();
      if (!yaml.trim()) { toast('Nothing to export: every field is empty.'); return; }
      const name = (rootOf(ctx.base) || {}).name || baseName(ctx.source) || 'module';
      downloadText(`${name}-params.yaml`, yaml, 'application/yaml');
    } catch (err) {
      toastError(err, 'Could not export');
    }
  }

  /** A params file of the current values. Structured values are copied as text. */
  async function composeYaml() {
    const entries = [];
    const scalars = {};
    for (const [name, f] of fields) {
      if (f.dynamic && !f.override) continue;
      const v = f.get();
      if (f.structured) { if (v.trim()) entries.push({ name, type: f.decl.type, text: v }); continue; }
      if (v === '' && !(f.dynamic && f.override)) continue;
      entries.push({ name, type: 'string', text: v });
      scalars[name] = v;
    }
    let formatted = null;
    if (Object.keys(scalars).length) {
      try {
        const r = await api.yamlFormat(scalars);
        formatted = r && typeof r.text === 'string' ? r.text : null;
      } catch { formatted = null; }
    }
    const doc = composeParams(entries, formatted);
    if (doc.trim()) {
      const r = await api.yamlParse(doc);
      if (r.error) throw new Error(`the composed file does not parse (line ${r.error.line}: ${r.error.message})`);
    }
    return doc;
  }

  /**
   * Fills the form from a params document. Each value is cut from the text as
   * written; the parsed value only decides which keys exist and whether a
   * string param's value was written as a string.
   */
  async function applyDocument(text, parsed) {
    const split = splitTopLevel(text);
    const unknown = [];
    for (const [, f] of fields) {
      if (f.dynamic) f.setOverride(false);
      f.set('');
    }
    for (const [name, v] of Object.entries(parsed || {})) {
      const f = fields.get(name);
      if (!f) { unknown.push(name); continue; }
      const entry = split ? split.get(name) : null;
      let t;
      if (f.structured) {
        t = entry ? valueText(entry) : '';
        if (!t && v !== null && v !== undefined) {
          const r = await api.yamlFormat(v);
          t = (r && r.text) || '';
        }
      } else if (typeof v === 'string') {
        t = v;
      } else if (v === null || v === undefined) {
        t = '';
      } else {
        t = entry ? scalarText(entry) : String(v);
      }
      if (f.dynamic) f.setOverride(true);
      f.set(t);
    }
    dirty = false;
    runChecks.flush();
    runInspect.flush();
    if (unknown.length) toast(`Ignored ${plural(unknown.length, 'value')} this module does not declare: ${unknown.join(', ')}`, { kind: 'error' });
  }

  async function prefillFromJob(jobId) {
    if (!jobId) return;
    try {
      const job = await api.jobs.get(jobId);
      const req = job.request || {};
      for (const [, f] of fields) { if (f.dynamic) f.setOverride(false); f.set(''); }
      let lossy = false;
      for (const [name, v] of Object.entries(req.params || {})) {
        const f = fields.get(name);
        if (!f) continue;
        let t = v;
        if (v !== null && typeof v === 'object') {
          const r = await api.yamlFormat(v);
          t = (r && r.text) || '';
          lossy = true;
        }
        if (f.dynamic) f.setOverride(true);
        f.set(String(t ?? ''));
      }
      if (req.author) adv.author.value = req.author;
      if (req.email) adv.email.value = req.email;
      if (req.targetPath) adv.targetPath.value = req.targetPath;
      if (req.keepWorkspace) adv.keep.checked = true;
      replace(noticeEl, h('div', { class: 'banner banner-info mb-4' }, icon('history'), h('div', { class: 'banner-body' },
        h('div', { class: 'banner-title' }, 'Prefilled from an earlier job'),
        h('div', null, 'Edit the values, then preview or run. ', h('a', { href: href(`/jobs/${encodeURIComponent(jobId)}`) }, 'View that job')),
        lossy ? h('div', { class: 'small' }, 'Structured values were stored as data and have been re-serialized; check version-like numbers such as 1.10.') : null)));
      setQuiet(`/m/${encodeURIComponent(ctx.source)}/run`, {});
      runChecks.flush();
      runInspect.flush();
    } catch (err) {
      toastError(err, 'Could not load the job to prefill from');
    }
  }

  return {
    shown(q) { paintLastPreview(); if (q && q.job) prefillFromJob(q.job); },
    canLeave() { return true; },
    destroy() { runChecks.cancel(); runInspect.cancel(); },
  };
}
