// Validate tab: `loom validate [-r]`, findings grouped by module label.

import { h, icon, replace } from '../dom.js';
import * as api from '../api.js';
import { prefs } from '../store.js';
import { moduleChip } from '../components/chip.js';
import { copyButton } from '../components/copy.js';
import { plural } from '../lib/format.js';
import { rootOf, loadErrorBanner } from './module.js';

export function renderValidate(el, ctx) {
  let recursive = prefs.get('validate.recursive', false);
  let seq = 0;
  const out = h('div', { 'aria-live': 'polite' });
  const sw = h('input', { type: 'checkbox', checked: recursive, onChange: () => { recursive = sw.checked; prefs.set('validate.recursive', recursive); cliCode.textContent = cliText(); run(); } });
  // validate has no /cli form in the API; its command line is simple enough to
  // spell out here.
  const cliText = () => `loom validate ${ctx.source}${recursive ? ' --recursive' : ''}`;
  const cliCode = h('code', { class: 'small muted break' }, cliText());
  const cliEl = h('span', { class: 'row' }, icon('terminal', 'icon-sm'), cliCode, copyButton(cliText, { label: 'Copy command', what: 'Command copied' }));
  const runBtn = h('button', { class: 'btn btn-primary', type: 'button', onClick: () => run() }, icon('shield'), 'Validate');

  el.appendChild(h('div', { class: 'stack' },
    h('div', { class: 'toolbar' },
      runBtn,
      h('label', { class: 'switch' }, sw, h('span', { class: 'track' }), 'Also validate the modules it composes'),
      h('span', { class: 'spacer' }),
      cliEl),
    h('p', { class: 'small muted' }, 'Checks the config and the files it renders. Warnings are worth a look; errors would stop a run. Runs nothing.'),
    out));

  const rootName = () => { const r = rootOf(ctx.base); return (r && (r.name || r.instance)) || 'root'; };

  async function run() {
    const my = ++seq;
    runBtn.disabled = true;
    replace(out, h('div', { class: 'stack-sm' }, [90, 60, 75].map((w) => h('div', { class: `skel skel-line skel-w-${w === 60 ? 50 : w === 75 ? 70 : 90}` }))));
    try {
      const r = await api.validate({ source: ctx.source, recursive });
      if (my !== seq) return;
      paint(r);
    } catch (err) {
      if (my !== seq) return;
      replace(out, loadErrorBanner(err, ctx));
    } finally {
      if (my === seq) runBtn.disabled = false;
    }
  }

  function paint(r) {
    const errors = r.errors || [];
    const warnings = r.warnings || [];
    const groups = new Map();
    const add = (f, kind) => {
      const key = f.module || '';
      if (!groups.has(key)) groups.set(key, []);
      groups.get(key).push({ ...f, kind });
    };
    errors.forEach((f) => add(f, 'err'));
    warnings.forEach((f) => add(f, 'warn'));
    const checked = r.count ? `${plural(r.count, 'module')} checked` : '';
    const head = r.valid
      ? h('div', { class: 'banner banner-ok' }, icon('check-circle'), h('div', { class: 'banner-body' },
        h('div', { class: 'banner-title' }, warnings.length ? `Valid, with ${plural(warnings.length, 'warning')}` : 'Valid'),
        checked ? h('div', { class: 'small' }, checked) : null))
      : h('div', { class: 'banner banner-err' }, icon('x-circle'), h('div', { class: 'banner-body' },
        h('div', { class: 'banner-title' }, `${plural(errors.length, 'error')}${warnings.length ? `, ${plural(warnings.length, 'warning')}` : ''}`),
        checked ? h('div', { class: 'small' }, checked) : null));
    const keys = [...groups.keys()].sort((a, b) => (a === '' ? -1 : b === '' ? 1 : a.localeCompare(b)));
    replace(out, head, keys.map((k) => {
      const items = groups.get(k).sort((a, b) => (a.kind === b.kind ? 0 : a.kind === 'err' ? -1 : 1));
      const parts = k ? k.split(' › ') : [rootName()];
      return h('section', { class: 'finding-group mt-4' },
        h('div', { class: 'fg-head' },
          k ? [moduleChip(parts[0], { root: true, soft: true }), parts.slice(1).map((p) => [h('span', { class: 'faint' }, '›'), h('span', { class: 'mono small' }, p)])]
            : moduleChip(rootName(), { soft: true }),
          h('span', { class: 'small muted' }, plural(items.length, 'finding'))),
        h('ul', { class: 'findings' }, items.map((f) => h('li', { class: ['finding', f.kind] },
          icon(f.kind === 'err' ? 'x-circle' : 'alert'),
          h('span', { class: 'sr-only' }, f.kind === 'err' ? 'Error: ' : 'Warning: '),
          h('span', { class: 'msg' }, f.message)))));
    }));
  }

  run();
  return {};
}
