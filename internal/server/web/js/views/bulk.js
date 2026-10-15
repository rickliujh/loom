// Bulk wizard (#/bulk): scaffold a wrapper that runs one module once per item.
// module → items → naming → output → preview → create.

import { h, icon, replace, uid, debounce, append } from '../dom.js';
import * as api from '../api.js';
import { state, emit } from '../store.js';
import { href } from '../router.js';
import { createStepper, dirPicker, watchJob } from '../components/wizard.js';
import { copyButton } from '../components/copy.js';
import { toast } from '../components/toast.js';
import { fuzzyFilter } from '../lib/fuzzy.js';
import { composeParams } from '../lib/yaml-view.js';
import { plural, baseName } from '../lib/format.js';

const STRUCT = (t) => t === 'list' || t === 'map';

export function renderBulk(outlet, _params, query) {
  const steps = [
    { id: 'module', title: 'Module' },
    { id: 'items', title: 'Items' },
    { id: 'naming', title: 'Naming' },
    { id: 'output', title: 'Output' },
    { id: 'preview', title: 'Preview' },
    { id: 'create', title: 'Create' },
  ];
  let current = 0;
  let reached = 0;
  let stopWatch = null;
  let jobId = null;
  const data = { module: null, source: query.module || '', params: [], rows: [{}], nameParam: '', name: '', target: false };
  const stepper = createStepper(steps, { onGo: (i) => go(i) });
  const panel = h('div', { class: 'card wizard-panel' });
  const out = dirPicker({ label: 'Output directory', placeholder: 'batches/q3-onboarding', help: 'Must be inside a root and hold no loom config yet; bulk never overwrites.' });

  outlet.appendChild(h('div', { class: 'page' },
    h('div', { class: 'page-head' }, h('div', { class: 'title-block' },
      h('h1', null, h('span', { class: 'title-mark root' }, icon('layers')), 'Scaffold a bulk run'),
      h('div', { class: 'subtitle' }, 'A wrapper module that runs one module once per item. You maintain only the items.'))),
    h('div', { class: 'wizard' }, h('div', null, stepper.el, stepper.compact), panel)));

  if (data.source) selectModule(data.source).then(() => { reached = 1; go(1); }).catch(() => go(0));
  else go(0);

  function go(i) {
    if (i > reached) return;
    current = i;
    stepper.set(current, reached);
    const body = h('div', { class: 'card-body' });
    replace(panel, body);
    ({ module: stepModule, items: stepItems, naming: stepNaming, output: stepOutput, preview: stepPreview, create: stepCreate })[steps[i].id](body);
  }

  function next(check) {
    const err = check ? check() : '';
    if (err) { toast(err, { kind: 'error' }); return; }
    reached = Math.max(reached, current + 1);
    go(current + 1);
  }

  function nav({ nextLabel = 'Next' } = {}) {
    return h('div', { class: 'wizard-nav' },
      current > 0 ? h('button', { class: 'btn', type: 'button', onClick: () => go(current - 1) }, icon('chevron-left', 'icon-sm'), 'Back') : h('span'),
      h('button', { class: 'btn btn-primary', type: 'submit' }, nextLabel, icon('chevron-right', 'icon-sm')));
  }

  function form(check, ...children) {
    return h('form', { class: 'stack', onSubmit: (e) => { e.preventDefault(); next(check); } }, ...children, nav());
  }

  async function selectModule(source) {
    const rep = await api.inspect({ source, params: {}, depth: 1, modules: [], noFetch: false });
    const root = rep && rep.modules && rep.modules[0] && rep.modules[0].module;
    data.source = source;
    data.module = root;
    data.params = ((root && root.params) || []).filter((p) => p.state !== 'dynamic');
    data.target = !!(root && root.target && root.target.url);
    data.rows = [{}];
    data.nameParam = data.params.find((p) => p.required && !STRUCT(p.type)) ? data.params.find((p) => p.required && !STRUCT(p.type)).name : '';
    data.name = `bulk-${(root && root.name) || baseName(source)}`;
    if (!out.get()) out.set(`batches/${data.name}`);
  }

  // ---- 1. module
  function stepModule(body) {
    const listEl = h('div', { class: 'picker-list', role: 'radiogroup', 'aria-label': 'Module' });
    const search = h('input', { class: 'input', type: 'search', placeholder: 'Filter modules', 'aria-label': 'Filter modules', onInput: debounce(() => paint(), 80) });
    const urlIn = h('input', { class: 'input mono', placeholder: 'or a git URL: https://github.com/org/modules.git//onboard', 'aria-label': 'Module git URL', spellcheck: 'false' });
    let mods = state.modules;
    const paint = () => {
      if (!mods) { replace(listEl, h('div', { class: 'card-body' }, h('div', { class: 'skel skel-line skel-w-70' }))); return; }
      const usable = mods.filter((m) => !m.loadError);
      const ranked = fuzzyFilter(usable, search.value.trim(), [(m) => m.name, (m) => m.rel]).map((r) => r.item);
      if (!ranked.length) { replace(listEl, h('div', { class: 'card-body small muted' }, 'No module matches.')); return; }
      replace(listEl, ranked.map((m) => h('button', {
        class: 'picker-item', type: 'button', role: 'radio', 'aria-checked': String(m.dir === data.source),
        onClick: async () => {
          try { await selectModule(m.dir); next(); } catch (err) { toast(err.message, { kind: 'error' }); }
        },
      }, icon('module'), h('span', { class: 'stack-sm grow' }, h('span', { class: 'pi-name' }, m.name), h('span', { class: 'pi-path' }, m.dir)),
      h('span', { class: 'small muted' }, m.params ? plural(m.params.total, 'param') : ''))));
    };
    paint();
    if (!mods) api.modules.list(false).then((r) => { mods = (r && r.modules) || []; state.modules = mods; paint(); }).catch(() => { mods = []; paint(); });
    append(body, h('h2', null, 'Which module should run once per item?'),
      h('p', { class: 'lede' }, 'The wrapper names it as a child for every item. The module itself is never modified.'),
      search, listEl,
      h('form', { class: 'row', onSubmit: async (e) => {
        e.preventDefault();
        const v = urlIn.value.trim();
        if (!v) return;
        try { await selectModule(v); next(); } catch (err) { toast(err.message, { kind: 'error' }); }
      } }, urlIn, h('button', { class: 'btn', type: 'submit' }, 'Use')));
  }

  // ---- 2. items
  function stepItems(body) {
    const cols = data.params;
    const table = h('table');
    const paint = () => {
      replace(table,
        h('thead', null, h('tr', null, h('th', { class: 'rownum' }, '#'),
          cols.map((p) => h('th', { scope: 'col' }, p.name, p.required ? h('span', { class: 'req', 'aria-label': 'required' }, '*') : null, p.type && p.type !== 'string' ? h('span', { class: 'ty' }, p.type) : null)),
          h('th', { class: 'rowact' }, h('span', { class: 'sr-only' }, 'Remove')))),
        h('tbody', null, data.rows.map((row, ri) => h('tr', null,
          h('td', { class: 'rownum' }, String(ri + 1)),
          cols.map((p, ci) => h('td', null, cell(row, ri, p, ci))),
          h('td', { class: 'rowact' }, h('button', { class: 'btn btn-ghost btn-sm btn-icon', type: 'button', 'aria-label': `Remove item ${ri + 1}`, onClick: () => { data.rows.splice(ri, 1); if (!data.rows.length) data.rows.push({}); paint(); } }, icon('x', 'icon-sm')))))));
    };
    const cell = (row, ri, p, ci) => {
      const def = typeof p.default === 'string' ? p.default : '';
      const attrs = {
        class: 'cell', 'aria-label': `${p.name}, item ${ri + 1}`, placeholder: STRUCT(p.type) ? (p.valueYaml || (p.type === 'list' ? '- …' : 'key: value')).split('\n')[0] : def,
        spellcheck: 'false', 'data-r': ri, 'data-c': ci,
        onInput: (e) => { row[p.name] = e.target.value; e.target.removeAttribute('aria-invalid'); },
        onPaste: (e) => onPaste(e, ri, ci),
      };
      const el = STRUCT(p.type) ? h('textarea', attrs) : h('input', attrs);
      el.value = row[p.name] || '';
      return el;
    };
    const onPaste = (e, ri, ci) => {
      const text = (e.clipboardData || window.clipboardData).getData('text');
      if (!text.includes('\t') && !text.includes('\n')) return;
      if (cols[ci] && STRUCT(cols[ci].type) && !text.includes('\t')) return; // YAML pasted into a YAML cell
      e.preventDefault();
      const lines = text.replace(/\r/g, '').replace(/\n$/, '').split('\n');
      lines.forEach((ln, k) => {
        const r = ri + k;
        while (data.rows.length <= r) data.rows.push({});
        ln.split('\t').forEach((v, j) => { const p = cols[ci + j]; if (p) data.rows[r][p.name] = v.trim(); });
      });
      paint();
      toast(`Pasted ${plural(lines.length, 'row')}`, { kind: 'ok', timeout: 1500 });
    };
    paint();
    const check = () => {
      const items = collectItems();
      if (!items.length) return 'Add at least one item.';
      for (let i = 0; i < data.rows.length; i++) {
        for (const p of cols) {
          const v = (data.rows[i][p.name] || '').trim();
          if (p.required && p.state !== 'default' && !v && Object.values(data.rows[i]).some((x) => String(x || '').trim())) {
            const input = table.querySelector(`[data-r="${i}"][data-c="${cols.indexOf(p)}"]`);
            if (input) { input.setAttribute('aria-invalid', 'true'); input.focus(); }
            return `Item ${i + 1} needs ${p.name}.`;
          }
        }
      }
      return '';
    };
    append(body, h('h2', null, `Items for ${(data.module && data.module.name) || data.source}`),
      h('p', { class: 'lede' }, 'One row per run. Empty cells use the module’s default. Paste rows from a spreadsheet (tab-separated) into any cell. list and map cells take YAML.'),
      !cols.length ? h('div', { class: 'banner banner-info' }, icon('info'), h('div', { class: 'banner-body' }, 'This module declares no parameters to vary; every item runs it the same way.')) : null,
      form(check, h('div', { class: 'grid-editor' }, table),
        h('div', { class: 'row' }, h('button', { class: 'btn btn-sm', type: 'button', onClick: () => { data.rows.push({}); paint(); const last = table.querySelector(`[data-r="${data.rows.length - 1}"]`); if (last) last.focus(); } }, icon('plus', 'icon-sm'), 'Add item'),
          h('span', { class: 'small muted' }, plural(data.rows.length, 'row'))),
        (data.module && data.module.params || []).some((p) => p.state === 'dynamic')
          ? h('p', { class: 'small muted' }, 'Dynamic params are left out: each run computes them inside the module.') : null));
  }

  function collectItems() {
    const items = [];
    for (const row of data.rows) {
      const item = {};
      for (const p of data.params) {
        const v = row[p.name];
        if (v !== undefined && String(v).trim() !== '') item[p.name] = STRUCT(p.type) ? String(v) : String(v).trim();
      }
      if (Object.keys(item).length) items.push(item);
    }
    return items;
  }

  // ---- 3. naming
  function stepNaming(body) {
    const nameIn = h('input', { class: 'input mono', id: uid('bn'), value: data.name, autocomplete: 'off', spellcheck: 'false', onInput: () => { data.name = nameIn.value.trim(); } });
    const strings = data.params.filter((p) => !STRUCT(p.type));
    const sel = h('select', { class: 'select', id: uid('np'), onChange: () => { data.nameParam = sel.value; } },
      h('option', { value: '' }, '(item index: 0, 1, 2 …)'),
      strings.map((p) => h('option', { value: p.name, selected: p.name === data.nameParam }, p.name)));
    const check = () => {
      if (data.nameParam) {
        const vals = collectItems().map((it) => it[data.nameParam]);
        if (vals.some((v) => !v)) return `Every item needs a ${data.nameParam} to be named by it.`;
        const dup = vals.find((v, i) => vals.indexOf(v) !== i);
        if (dup) return `Two items share ${data.nameParam} “${dup}”; names must be unique.`;
      }
      return '';
    };
    append(body, h('h2', null, 'How are runs named?'),
      form(check,
        h('div', { class: 'field' }, h('label', { class: 'field-label', for: sel.id }, 'Name each item by'), sel,
          h('div', { class: 'field-help' }, 'The instance name shows in run logs, diffs and local-run directories. A param value is easier to recognise than an index.')),
        h('div', { class: 'field' }, h('label', { class: 'field-label', for: nameIn.id }, 'Wrapper name', h('span', { class: 'meta' }, 'metadata.name')), nameIn),
        data.target
          ? h('div', { class: 'banner banner-info' }, icon('pr'), h('div', { class: 'banner-body small' },
            `${(data.module && data.module.name) || 'This module'} declares its own target, so every item gets its own branch and pull request (${plural(collectItems().length, 'PR')}). The generated file explains how to batch them into one.`))
          : null));
  }

  // ---- 4. output
  function stepOutput(body) {
    append(body, h('h2', null, 'Where should the wrapper go?'), form(() => out.validate(), out.el));
  }

  function request() {
    return { kind: 'bulk', module: data.source, name: data.name, nameParam: data.nameParam, items: collectItems(), output: out.get() };
  }

  // ---- 5. preview
  function stepPreview(body) {
    const req = request();
    const itemsYaml = req.items.map((it) => {
      const entries = data.params.filter((p) => it[p.name] !== undefined).map((p) => ({ name: p.name, type: p.type || 'string', text: it[p.name] }));
      const doc = composeParams(entries).replace(/\n$/, '');
      return doc.split('\n').map((l, i) => (i === 0 ? `- ${l}` : `  ${l}`)).join('\n');
    }).join('\n') + '\n';
    const cliBox = h('div', null, h('div', { class: 'skel skel-line skel-w-90' }));
    api.cli(req).then((r) => replace(cliBox,
      h('div', { class: 'field-label' }, 'Same thing on the command line', h('span', { class: 'meta' }, copyButton(r.command, { label: 'Copy command', iconOnly: false, cls: 'btn btn-sm', what: 'Command copied' }))),
      h('pre', { class: 'code code-wrap' }, r.command))).catch((err) => replace(cliBox, h('div', { class: 'field-error' }, err.message)));
    append(body, h('h2', null, 'Review'),
      h('dl', { class: 'kv' },
        h('dt', null, 'Module'), h('dd', { class: 'mono small' }, req.module),
        h('dt', null, 'Wrapper'), h('dd', { class: 'mono small' }, req.name || '(default)'),
        h('dt', null, 'Named by'), h('dd', { class: 'mono small' }, req.nameParam || 'item index'),
        h('dt', null, 'Output'), h('dd', { class: 'mono small' }, `${req.output}/loom.jsonnet`)),
      h('div', { class: 'field' }, h('div', { class: 'field-label' }, `Items (${req.items.length})`, h('span', { class: 'meta' }, copyButton(itemsYaml, { label: 'Copy items', what: 'Items copied' }))),
        h('pre', { class: 'code' }, itemsYaml)),
      h('p', { class: 'small muted' }, 'The server validates the items against the module’s declarations, writes loom.jsonnet, and loads it back to check it before reporting success.'),
      cliBox,
      h('form', { class: 'stack', onSubmit: (e) => { e.preventDefault(); next(); } }, nav({ nextLabel: 'Create wrapper' })));
  }

  // ---- 6. create
  async function stepCreate(body) {
    if (jobId) { showJob(body); return; }
    const status = h('div', null, h('div', { class: 'skel skel-line skel-w-50' }));
    append(body, h('h2', null, 'Creating'), status);
    try {
      const r = await api.jobs.create(request());
      jobId = r.id;
      emit('jobs-changed');
      showJob(body);
    } catch (err) {
      replace(status, h('div', { class: 'banner banner-err' }, icon('alert'), h('div', { class: 'banner-body' },
        h('div', { class: 'banner-title' }, 'The server refused the request'), err.message, api.errorLines(err).map((l) => h('div', { class: 'mono small' }, l)))),
      h('div', { class: 'wizard-nav mt-3' }, h('button', { class: 'btn', type: 'button', onClick: () => go(1) }, icon('chevron-left', 'icon-sm'), 'Edit the items')));
    }
  }

  function showJob(body) {
    const req = request();
    const box = h('div');
    const done = h('div');
    replace(body, h('h2', null, 'Creating the wrapper'), box, done);
    if (stopWatch) stopWatch();
    stopWatch = watchJob(jobId, box, {
      onDone: (job) => replace(done, job.state === 'succeeded'
        ? h('div', { class: 'banner banner-ok' }, icon('check-circle'), h('div', { class: 'banner-body' },
          h('div', { class: 'banner-title' }, 'Wrapper created'),
          h('div', null, 'Preview it before running: a Quick preview executes nothing.'),
          h('div', { class: 'row row-wrap mt-1' },
            h('a', { class: 'btn btn-primary', href: href(`/m/${encodeURIComponent(req.output)}/run`) }, icon('eye'), 'Open and preview'),
            h('a', { class: 'btn', href: href(`/m/${encodeURIComponent(req.output)}/files`, { path: 'loom.jsonnet' }) }, icon('files'), 'See loom.jsonnet'))))
        : h('div', { class: 'banner banner-err' }, icon('x-circle'), h('div', { class: 'banner-body' },
          h('div', { class: 'banner-title' }, `Bulk ${job.state}`), job.error || 'See the job log.'))),
    });
  }

  return () => { if (stopWatch) stopWatch(); };
}

