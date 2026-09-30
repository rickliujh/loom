// Generate wizard (#/generate): turn a merged PR/MR into a reusable module.
// refs → token → values → name and output → preview → create.

import { h, icon, replace, uid, append } from '../dom.js';
import * as api from '../api.js';
import { state, emit } from '../store.js';
import { href } from '../router.js';
import { createStepper, dirPicker, watchJob } from '../components/wizard.js';
import { copyButton } from '../components/copy.js';
import { toast } from '../components/toast.js';
import { plural } from '../lib/format.js';

const REF_RE = [
  /^https?:\/\/[^/\s]+\/[^\s]+\/pull\/\d+\/?$/,
  /^https?:\/\/[^/\s]+\/[^\s]+\/-\/merge_requests\/\d+\/?$/,
  /^github:[\w.-]+\/[\w.-]+#\d+$/,
  /^gitlab:[\w./-]+!\d+$/,
];
const WELL_KNOWN = ['GITHUB_TOKEN', 'GITLAB_TOKEN', 'LOOM_GIT_TOKEN'];

export function renderGenerate(outlet) {
  const steps = [
    { id: 'refs', title: 'Pull requests' },
    { id: 'token', title: 'Token' },
    { id: 'values', title: 'Values to parameterize' },
    { id: 'output', title: 'Name and output' },
    { id: 'preview', title: 'Preview' },
    { id: 'create', title: 'Create' },
  ];
  let current = 0;
  let reached = 0;
  let stopWatch = null;
  let jobId = null;
  const data = { refs: [''], tokenEnv: '', customEnv: '', values: [{ name: '', value: '' }], name: '', overwrite: false };

  const stepper = createStepper(steps, { onGo: (i) => go(i) });
  const panel = h('div', { class: 'card wizard-panel' });
  const out = dirPicker({ label: 'Output directory', placeholder: 'modules/onboard-service', help: 'Must be inside a root. It should be new or empty.' });

  outlet.appendChild(h('div', { class: 'page' },
    h('div', { class: 'page-head' }, h('div', { class: 'title-block' },
      h('h1', null, h('span', { class: 'title-mark root' }, icon('wand')), 'Generate a module'),
      h('div', { class: 'subtitle' }, 'From a change you already made by hand: every occurrence of the values you name becomes a template action.'))),
    h('div', { class: 'wizard' }, h('div', null, stepper.el, stepper.compact), panel)));

  go(0);

  function go(i) {
    if (i > reached) return;
    current = i;
    stepper.set(current, reached);
    const s = steps[i].id;
    const body = h('div', { class: 'card-body' });
    replace(panel, body);
    ({ refs: stepRefs, token: stepToken, values: stepValues, output: stepOutput, preview: stepPreview, create: stepCreate })[s](body);
    const first = body.querySelector('input, textarea, select, button');
    if (first && s !== 'create') first.focus({ preventScroll: true });
  }

  function next(check) {
    const err = check ? check() : '';
    if (err) { toast(err, { kind: 'error' }); return; }
    reached = Math.max(reached, current + 1);
    go(current + 1);
  }

  function nav(check, { nextLabel = 'Next', back = true } = {}) {
    return h('div', { class: 'wizard-nav' },
      back && current > 0 ? h('button', { class: 'btn', type: 'button', onClick: () => go(current - 1) }, icon('chevron-left', 'icon-sm'), 'Back') : h('span'),
      h('button', { class: 'btn btn-primary', type: 'submit' }, nextLabel, icon('chevron-right', 'icon-sm')));
  }

  function form(check, ...children) {
    return h('form', { class: 'stack', onSubmit: (e) => { e.preventDefault(); next(check); } }, ...children, nav(check));
  }

  // ---- 1. refs
  function stepRefs(body) {
    const rows = h('div', { class: 'stack-sm' });
    const paint = () => replace(rows, data.refs.map((r, i) => {
      const inp = h('input', { class: 'input mono', value: r, placeholder: 'https://github.com/org/repo/pull/42', 'aria-label': `Pull request ${i + 1}`, spellcheck: 'false', autocomplete: 'off', onInput: () => { data.refs[i] = inp.value.trim(); } });
      return h('div', { class: 'row' }, inp,
        data.refs.length > 1 ? h('button', { class: 'btn btn-ghost btn-icon', type: 'button', 'aria-label': `Remove pull request ${i + 1}`, onClick: () => { data.refs.splice(i, 1); paint(); } }, icon('x')) : null);
    }));
    paint();
    const check = () => {
      const refs = data.refs.map((r) => r.trim()).filter(Boolean);
      if (!refs.length) return 'Name at least one pull request or merge request.';
      const bad = refs.find((r) => !REF_RE.some((re) => re.test(r)));
      if (bad) return `“${bad}” is not a PR or MR reference loom understands.`;
      data.refs = refs;
      return '';
    };
    append(body, h('h2', null, 'Which change should become a module?'),
      h('p', { class: 'lede' }, 'A GitHub pull request or GitLab merge request, by URL or short form. Several are combined into one module.'),
      form(check, rows,
        h('div', null, h('button', { class: 'btn btn-sm', type: 'button', onClick: () => { data.refs.push(''); paint(); rows.lastChild.querySelector('input').focus(); } }, icon('plus', 'icon-sm'), 'Add another')),
        h('div', { class: 'field-help' }, 'Accepted: https://github.com/o/r/pull/12 · https://gitlab.com/g/r/-/merge_requests/7 · github:o/r#12 · gitlab:g/r!7')));
  }

  // ---- 2. token
  function stepToken(body) {
    const env = (state.info && state.info.env) || {};
    const caps = (state.info && state.info.capabilities) || {};
    const names = [...new Set([...WELL_KNOWN.filter((n) => n !== 'LOOM_GIT_TOKEN'), ...Object.keys(env).filter((n) => n !== 'LOOM_GIT_TOKEN')])];
    const group = uid('tok');
    const custom = h('input', { class: 'input mono', value: data.customEnv, placeholder: 'MY_TOKEN', 'aria-label': 'Other variable name', spellcheck: 'false', onInput: () => { data.customEnv = custom.value.trim(); pick('custom'); } });
    const radio = (value, label, extra) => h('label', { class: 'env-option' },
      h('input', { type: 'radio', name: group, value, checked: (data.tokenEnv || '') === value || (value === 'custom' && data.tokenEnv === 'custom'), onChange: () => pick(value) }),
      h('span', { class: 'grow stack-sm' }, label), extra || null);
    const pick = (v) => {
      data.tokenEnv = v;
      body.querySelectorAll(`input[name="${group}"]`).forEach((r) => { r.checked = r.value === v; });
    };
    const opts = h('div', { class: 'env-options', role: 'radiogroup', 'aria-label': 'Token variable' },
      radio('', h('span', null, h('strong', null, 'Default'), h('span', { class: 'small muted' }, ' — GITHUB_TOKEN or GITLAB_TOKEN by provider; falls back to an authenticated gh / glab')),
        h('span', { class: 'row' }, caps.gh ? h('span', { class: 'badge badge-ok' }, 'gh') : null, caps.glab ? h('span', { class: 'badge badge-ok' }, 'glab') : null)),
      names.map((n) => radio(n, h('span', { class: 'env-name' }, n), env[n] ? h('span', { class: 'badge badge-ok' }, icon('check'), 'set') : h('span', { class: 'badge' }, 'not set'))),
      radio('custom', h('span', null, 'Another variable'), custom));
    const check = () => {
      if (data.tokenEnv === 'custom') {
        if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(data.customEnv)) return 'Enter the name of an environment variable.';
      } else if (data.tokenEnv && env[data.tokenEnv] === false) {
        // allowed: the server may still fall back to gh/glab, but say so.
        toast(`${data.tokenEnv} is not set in the server's environment; the fetch may fail.`);
      }
      return '';
    };
    append(body, h('h2', null, 'Which token reads the change?'),
      h('p', { class: 'lede' }, 'The server reads the variable from its own environment. Only whether it is set is shown here — never its value.'),
      form(check, opts));
  }

  // ---- 3. values
  function stepValues(body) {
    const rows = h('div', { class: 'values-table' });
    const paint = () => replace(rows,
      h('div', { class: 'values-row small muted' }, h('span', null, 'Param name'), h('span', null, 'Literal value in the change'), h('span')),
      data.values.map((v, i) => {
        const n = h('input', { class: 'input mono', value: v.name, placeholder: 'serviceName', 'aria-label': `Param name ${i + 1}`, spellcheck: 'false', onInput: () => { v.name = n.value.trim(); } });
        const val = h('input', { class: 'input mono', value: v.value, placeholder: 'payments', 'aria-label': `Value ${i + 1}`, spellcheck: 'false', onInput: () => { v.value = val.value; } });
        return h('div', { class: 'values-row' }, n, val,
          h('button', { class: 'btn btn-ghost btn-icon', type: 'button', 'aria-label': `Remove row ${i + 1}`, onClick: () => { data.values.splice(i, 1); if (!data.values.length) data.values.push({ name: '', value: '' }); paint(); } }, icon('x')));
      }));
    paint();
    const check = () => {
      const filled = data.values.filter((v) => v.name || v.value);
      for (const v of filled) {
        if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(v.name)) return `“${v.name || '(empty)'}” is not a valid param name.`;
        if (!v.value) return `Give ${v.name} the literal value it has in the change.`;
      }
      const names = filled.map((v) => v.name);
      const dup = names.find((n, i) => names.indexOf(n) !== i);
      if (dup) return `${dup} appears twice.`;
      return '';
    };
    append(body, h('h2', null, 'What should become a parameter?'),
      h('p', { class: 'lede' }, 'Name each value the way the module should call it. Every occurrence of the literal — in file contents and in file and folder names — becomes {{ .name }}. Leave the table empty to copy the change as is.'),
      form(check, rows,
        h('div', null, h('button', { class: 'btn btn-sm', type: 'button', onClick: () => { data.values.push({ name: '', value: '' }); paint(); } }, icon('plus', 'icon-sm'), 'Add a value'))));
  }

  // ---- 4. output
  function stepOutput(body) {
    const nameIn = h('input', { class: 'input mono', id: uid('gn'), value: data.name, placeholder: 'from the PR title', autocomplete: 'off', spellcheck: 'false', onInput: () => { data.name = nameIn.value.trim(); } });
    const ow = h('input', { type: 'checkbox', checked: data.overwrite, onChange: () => { data.overwrite = ow.checked; } });
    const check = () => out.validate();
    append(body, h('h2', null, 'Name it and choose where it goes'),
      form(check,
        h('div', { class: 'field' }, h('label', { class: 'field-label', for: nameIn.id }, 'Module name', h('span', { class: 'meta' }, 'optional')), nameIn,
          h('div', { class: 'field-help' }, 'metadata.name of the new module. Empty derives it from the PR title.')),
        out.el,
        h('label', { class: 'check' }, ow, 'The directory exists and has files: overwrite them'),
        h('div', { class: 'banner banner-warn' }, icon('alert'), h('div', { class: 'banner-body small' },
          'Generate writes files without asking. An existing, non-empty directory is refused unless you tick the box above.'))));
  }

  function request() {
    const values = {};
    for (const v of data.values) if (v.name && v.value) values[v.name] = v.value;
    return {
      kind: 'generate', refs: data.refs, values, name: data.name, output: out.get(),
      tokenEnv: data.tokenEnv === 'custom' ? data.customEnv : data.tokenEnv, overwrite: data.overwrite,
    };
  }

  // ---- 5. preview
  function stepPreview(body) {
    const req = request();
    const cliBox = h('div', null, h('div', { class: 'skel skel-line skel-w-90' }));
    api.cli(req).then((r) => replace(cliBox,
      h('div', { class: 'field-label' }, 'Same thing on the command line', h('span', { class: 'meta' }, copyButton(r.command, { label: 'Copy command', iconOnly: false, cls: 'btn btn-sm', what: 'Command copied' }))),
      h('pre', { class: 'code code-wrap' }, r.command))).catch((err) => replace(cliBox, h('div', { class: 'field-error' }, err.message)));
    const vals = Object.entries(req.values);
    append(body, h('h2', null, 'Review'),
      h('dl', { class: 'kv' },
        h('dt', null, plural(req.refs.length, 'Change')), h('dd', { class: 'mono small' }, req.refs.join('\n')),
        h('dt', null, 'Token'), h('dd', { class: 'mono small' }, req.tokenEnv || 'default'),
        h('dt', null, 'Parameters'), h('dd', null, vals.length ? vals.map(([k, v]) => h('div', { class: 'mono small' }, `${k} ← “${v}”`)) : h('span', { class: 'muted' }, 'none — the change is copied literally')),
        h('dt', null, 'Name'), h('dd', { class: 'mono small' }, req.name || '(from the PR title)'),
        h('dt', null, 'Output'), h('dd', { class: 'mono small' }, req.output, req.overwrite ? h('span', { class: 'badge badge-warn' }, 'overwrite') : null)),
      h('div', { class: 'small muted' }, 'What it writes: added files become templates, modified YAML becomes strategic merge patches under __functions/patches/, deletions and renames become shell steps, and a loom.yaml declares every parameter as required.'),
      cliBox,
      h('form', { class: 'stack', onSubmit: (e) => { e.preventDefault(); next(); } }, nav(null, { nextLabel: 'Create module' })));
  }

  // ---- 6. create
  async function stepCreate(body) {
    const req = request();
    if (jobId) { showJob(body); return; }
    const status = h('div', null, h('div', { class: 'skel skel-line skel-w-50' }));
    append(body, h('h2', null, 'Creating'), status);
    try {
      const r = await api.jobs.create(req);
      jobId = r.id;
      emit('jobs-changed');
      showJob(body);
    } catch (err) {
      replace(status, h('div', { class: 'banner banner-err' }, icon('alert'), h('div', { class: 'banner-body' },
        h('div', { class: 'banner-title' }, 'The server refused the request'), err.message,
        err.code === 'conflict' ? h('div', null, 'Go back to tick “overwrite”, or choose another directory.') : null)),
      h('div', { class: 'wizard-nav mt-3' }, h('button', { class: 'btn', type: 'button', onClick: () => go(3) }, icon('chevron-left', 'icon-sm'), 'Change the output')));
    }
  }

  function showJob(body) {
    const req = request();
    const box = h('div');
    const done = h('div');
    replace(body, h('h2', null, 'Creating the module'), box, done);
    if (stopWatch) stopWatch();
    stopWatch = watchJob(jobId, box, {
      onDone: (job) => replace(done, job.state === 'succeeded'
        ? h('div', { class: 'banner banner-ok' }, icon('check-circle'), h('div', { class: 'banner-body' },
          h('div', { class: 'banner-title' }, 'Module created'),
          h('div', { class: 'row row-wrap mt-1' },
            h('a', { class: 'btn btn-primary', href: href(`/m/${encodeURIComponent(req.output)}`) }, 'Open module', icon('arrow-right', 'icon-sm')),
            h('a', { class: 'btn', href: href(`/m/${encodeURIComponent(req.output)}/files`) }, icon('files'), 'Review its files'))))
        : h('div', { class: 'banner banner-err' }, icon('x-circle'), h('div', { class: 'banner-body' },
          h('div', { class: 'banner-title' }, `Generate ${job.state}`), job.error || 'See the job log.'))),
    });
  }

  return () => { if (stopWatch) stopWatch(); };
}
