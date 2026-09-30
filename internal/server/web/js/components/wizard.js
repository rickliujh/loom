// Pieces shared by the Generate and Bulk wizards: the stepper, the output
// directory picker, and a compact live view of the job a wizard starts.

import { h, icon, replace, uid } from '../dom.js';
import * as api from '../api.js';
import { state } from '../store.js';
import { href } from '../router.js';
import { statePill } from './chip.js';
import { isTerminal } from '../lib/format.js';

/**
 * createStepper(steps, {onGo}) → {el, compact, set(current, reached)}.
 * Steps up to `reached` can be revisited by clicking them.
 */
export function createStepper(steps, { onGo }) {
  const list = h('ol', { class: 'stepper', 'aria-label': 'Steps' });
  const compact = h('div', { class: 'stepper-compact', 'aria-live': 'polite' });
  const set = (current, reached) => {
    replace(list, steps.map((s, i) => h('li', null, h('button', {
      type: 'button', class: i < current ? 'done' : '', disabled: i > reached,
      'aria-current': i === current ? 'step' : null,
      onClick: () => onGo(i),
    }, h('span', { class: 'num' }, i < current ? icon('check', 'icon-sm') : String(i + 1)), s.title))));
    compact.textContent = `Step ${current + 1} of ${steps.length} — ${steps[current].title}`;
  };
  return { el: list, compact, set };
}

/** dirPicker({label, help, value}) → {el, get(), set(rel), input}: a root plus a path inside it. */
export function dirPicker({ label, help, placeholder = 'modules/new-service', onInput } = {}) {
  const roots = (state.info && state.info.roots) || [];
  const id = uid('dir');
  const rootSel = h('select', { class: 'select', 'aria-label': `${label}: root` }, roots.map((r) => h('option', { value: r }, r)));
  const rel = h('input', { class: 'input', id, placeholder, autocomplete: 'off', spellcheck: 'false', 'aria-label': `${label}: path inside the root` });
  const resolved = h('div', { class: 'resolved-path' });
  const get = () => {
    const r = rel.value.trim().replace(/^\/+|\/+$/g, '');
    return r ? `${rootSel.value}/${r}` : '';
  };
  const sync = () => { const v = get(); resolved.textContent = v ? `→ ${v}` : ''; if (onInput) onInput(v); };
  rel.addEventListener('input', sync);
  rootSel.addEventListener('change', sync);
  const el = h('div', { class: 'field' },
    h('label', { class: 'field-label', for: id }, label),
    h('div', { class: 'dir-picker' }, rootSel, rel),
    resolved,
    help ? h('div', { class: 'field-help' }, help) : null);
  return {
    el, input: rel, get,
    set(v) { rel.value = v; sync(); },
    validate() {
      const r = rel.value.trim();
      if (!r) return 'Choose a directory inside a root.';
      if (r.split('/').includes('..')) return 'The path may not contain “..”.';
      return '';
    },
  };
}

/**
 * watchJob(id, el, {onDone(job)}) shows a job's state and its latest log
 * lines until it ends. Returns stop().
 */
export function watchJob(id, el, { onDone } = {}) {
  const pill = h('span');
  const lines = h('ul', { class: 'stack-sm small mono' });
  const recent = [];
  replace(el, h('div', { class: 'stack' },
    h('div', { class: 'row row-wrap' }, pill, h('a', { href: href(`/jobs/${encodeURIComponent(id)}`) }, 'Open the full job view', icon('arrow-right', 'icon-sm'))),
    lines));
  replace(pill, statePill('queued'));
  const stop = api.streamJob(id, {
    onEvent: (ev) => {
      if (ev.type === 'job.state') {
        replace(pill, statePill(ev.state));
        if (ev.state === 'queued') recent.push({ level: 'INFO', msg: 'Waiting for the running job to finish; jobs that execute run one at a time.' });
        if (ev.error) recent.push({ level: 'ERROR', msg: ev.error });
      }
      if (ev.type === 'log' && ev.log) recent.push(ev.log);
      while (recent.length > 6) recent.shift();
      replace(lines, recent.map((l) => h('li', { class: String(l.level).toUpperCase() === 'ERROR' ? 'status-err' : String(l.level).toUpperCase() === 'WARN' ? 'status-warn' : 'muted' }, l.msg)));
    },
    onEnd: async () => {
      try {
        const job = await api.jobs.get(id);
        if (onDone && isTerminal(job.state)) onDone(job);
      } catch { /* the job view has the details */ }
    },
  });
  return stop;
}
