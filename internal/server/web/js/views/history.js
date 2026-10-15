// Job history: the global #/jobs table and the module page's History tab.

import { h, icon, replace, debounce } from '../dom.js';
import * as api from '../api.js';
import { emit, prefs } from '../store.js';
import { href, navigate } from '../router.js';
import { statePill } from '../components/chip.js';
import { confirmDialog } from '../components/modal.js';
import { toast, toastError } from '../components/toast.js';
import { relTime, absTime, duration, jobTitle, jobKindLabel, isTerminal, plural, parseTime } from '../lib/format.js';

const KINDS = [['', 'All kinds'], ['run', 'Runs'], ['diff', 'Diffs'], ['generate', 'Generate'], ['bulk', 'Bulk']];
const STATES = [['', 'All states'], ['queued', 'Queued'], ['running', 'Running'], ['succeeded', 'Succeeded'], ['failed', 'Failed'], ['cancelled', 'Cancelled'], ['interrupted', 'Interrupted']];

function elapsed(j) {
  const a = parseTime(j.startedAt);
  if (!a) return '';
  const b = parseTime(j.endedAt) || new Date();
  return duration(b - a);
}

async function rerun(job, reload) {
  try {
    const r = await api.jobs.rerun(job.id);
    emit('jobs-changed');
    toast('Started a new job with the same request', { kind: 'ok' });
    navigate(`/jobs/${encodeURIComponent(r.id)}`);
  } catch (err) {
    toastError(err, 'Could not re-run');
    if (reload) reload();
  }
}

async function remove(job, reload) {
  const ok = await confirmDialog({
    title: 'Delete this job?',
    message: 'It is removed from the history, together with its log and any workspace the server kept for it. Pull requests it opened are not touched.',
    confirmLabel: 'Delete job', danger: true,
  });
  if (!ok) return;
  try {
    await api.jobs.remove(job.id);
    emit('jobs-changed');
    toast('Job deleted', { kind: 'ok' });
  } catch (err) {
    toastError(err, 'Could not delete');
  }
  reload();
}

export function jobsTable(list, { showModule = true, reload }) {
  return h('div', { class: 'table-wrap' }, h('table', { class: 'table table-stack' },
    h('thead', null, h('tr', null,
      h('th', null, 'State'),
      showModule ? h('th', null, 'Module') : null,
      h('th', null, 'Kind'),
      h('th', null, 'Started'),
      h('th', { class: 'num' }, 'Took'),
      h('th', { class: 'actions' }, h('span', { class: 'sr-only' }, 'Actions')))),
    h('tbody', null, list.map((j) => {
      const link = href(`/jobs/${encodeURIComponent(j.id)}`);
      const prs = j.result && j.result.prs ? j.result.prs.length : 0;
      return h('tr', null,
        h('td', null, h('a', { href: link, 'aria-label': `Job ${j.id}: ${j.state}` }, statePill(j.state))),
        showModule ? h('td', { class: 'stack-full' },
          h('a', { class: 'rowlink', href: link }, jobTitle(j)),
          h('div', { class: 'sub mono break' }, (j.module && j.module.dir) || (j.request && j.request.source) || '')) : null,
        h('td', null, jobKindLabel(j), prs ? h('span', { class: 'badge badge-ok', title: 'Pull requests opened' }, icon('pr'), String(prs)) : null),
        h('td', { title: absTime(j.createdAt) }, relTime(j.createdAt)),
        h('td', { class: 'num' }, elapsed(j)),
        h('td', { class: 'actions' },
          h('button', { class: 'btn btn-ghost btn-sm', type: 'button', title: 'Re-run with the same request', onClick: () => rerun(j, reload) }, icon('refresh', 'icon-sm'), 'Re-run'),
          h('button', { class: 'btn btn-ghost btn-sm btn-icon', type: 'button', title: 'Delete', 'aria-label': `Delete job ${j.id}`, disabled: !isTerminal(j.state), onClick: () => remove(j, reload) }, icon('trash', 'icon-sm'))));
    }))));
}

export function renderHistory(outlet) {
  const f = prefs.get('history.filters', { module: '', kind: '', state: '' });
  let list = null;
  let destroyed = false;
  let timer = 0;
  const body = h('div', { 'aria-live': 'polite' });
  const countEl = h('span');

  const moduleIn = h('input', { class: 'input', type: 'search', placeholder: 'Filter by module name or path', value: f.module, 'aria-label': 'Filter by module', onInput: debounce(() => { f.module = moduleIn.value; save(); paint(); }, 100) });
  const kindSel = h('select', { class: 'select', 'aria-label': 'Kind', onChange: () => { f.kind = kindSel.value; save(); load(); } }, KINDS.map(([v, l]) => h('option', { value: v, selected: v === f.kind }, l)));
  const stateSel = h('select', { class: 'select', 'aria-label': 'State', onChange: () => { f.state = stateSel.value; save(); load(); } }, STATES.map(([v, l]) => h('option', { value: v, selected: v === f.state }, l)));
  const save = () => prefs.set('history.filters', f);

  outlet.appendChild(h('div', { class: 'page' },
    h('div', { class: 'page-head' },
      h('div', { class: 'title-block' }, h('h1', null, 'Jobs'), h('div', { class: 'subtitle' }, countEl)),
      h('div', { class: 'page-actions' }, h('button', { class: 'btn', type: 'button', onClick: () => load() }, icon('refresh'), 'Refresh'))),
    h('div', { class: 'toolbar' },
      h('div', { class: 'search' }, icon('search'), moduleIn),
      h('div', null, kindSel), h('div', null, stateSel)),
    body));

  async function load() {
    clearTimeout(timer);
    try {
      list = api.jobList(await api.jobs.list({ kind: f.kind, state: f.state, limit: 200 }));
    } catch (err) {
      if (!destroyed) replace(body, h('div', { class: 'banner banner-err' }, icon('alert'), h('div', { class: 'banner-body' }, err.message)));
      return;
    }
    if (destroyed) return;
    paint();
    // Keep live states current while anything is still moving.
    if (list.some((j) => !isTerminal(j.state))) timer = setTimeout(load, 3000);
  }

  function paint() {
    if (!list) {
      replace(body, h('div', { class: 'table-wrap card-body' }, Array.from({ length: 6 }, () => h('div', { class: 'skel skel-line skel-w-90' }))));
      return;
    }
    const q = f.module.trim().toLowerCase();
    const shown = q ? list.filter((j) => `${jobTitle(j)} ${(j.module && j.module.dir) || ''} ${(j.request && j.request.source) || ''}`.toLowerCase().includes(q)) : list;
    countEl.textContent = shown.length === list.length ? plural(list.length, 'job') : `${shown.length} of ${plural(list.length, 'job')}`;
    if (!shown.length) {
      const filtered = q || f.kind || f.state;
      replace(body, h('div', { class: 'empty' }, icon('history', 'icon-xl'),
        h('h3', null, filtered ? 'No job matches these filters' : 'No jobs yet'),
        h('p', null, filtered ? 'Clear a filter to see more.' : 'Jobs appear here when you preview, diff, run, generate or scaffold. Start from a module.'),
        h('div', { class: 'actions' }, filtered
          ? h('button', { class: 'btn', type: 'button', onClick: () => { f.module = ''; f.kind = ''; f.state = ''; moduleIn.value = ''; kindSel.value = ''; stateSel.value = ''; save(); load(); } }, 'Clear filters')
          : h('a', { class: 'btn btn-primary', href: href('/') }, 'Browse modules'))));
      return;
    }
    replace(body, jobsTable(shown, { reload: load }));
  }

  paint();
  load();
  return () => { destroyed = true; clearTimeout(timer); };
}

export function renderModuleHistory(el, ctx) {
  const body = h('div', { 'aria-live': 'polite' });
  el.appendChild(body);
  let timer = 0;
  async function load() {
    clearTimeout(timer);
    let list;
    try {
      list = api.jobList(await api.jobs.list({ module: ctx.source, limit: 100 }));
    } catch (err) {
      replace(body, h('div', { class: 'banner banner-err' }, icon('alert'), h('div', { class: 'banner-body' }, err.message)));
      return;
    }
    // The filter value the server matches on is not pinned down by the
    // contract; keep only this module's jobs whatever it returned.
    list = list.filter((j) => !j.module || !j.module.dir || j.module.dir === ctx.source || (j.request && (j.request.source === ctx.source || j.request.module === ctx.source)));
    if (!list.length) {
      replace(body, h('div', { class: 'empty' }, icon('history', 'icon-xl'), h('h3', null, 'No jobs for this module yet'),
        h('p', null, 'A Quick preview is a safe first job: it executes nothing and shows the file changes.'),
        h('div', { class: 'actions' }, h('a', { class: 'btn btn-primary', href: href(`/m/${encodeURIComponent(ctx.source)}/run`) }, icon('play'), 'Go to Params & Run'))));
      return;
    }
    replace(body, jobsTable(list, { showModule: false, reload: load }));
    if (list.some((j) => !isTerminal(j.state))) timer = setTimeout(load, 3000);
  }
  replace(body, h('div', { class: 'skel skel-card' }));
  load();
  return { shown: load, destroy: () => clearTimeout(timer) };
}
