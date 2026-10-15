// Job page (#/jobs/<id>/<tab>): live state, the module/operation tree, the
// log, the diffs and the result, all fed by the job's event stream.

import { h, icon, replace } from '../dom.js';
import * as api from '../api.js';
import { emit } from '../store.js';
import { href, navigate } from '../router.js';
import { statePill, nodeStatusIcon, crumbs } from '../components/chip.js';
import { copyButton } from '../components/copy.js';
import { showCli } from '../components/cli.js';
import { confirmDialog } from '../components/modal.js';
import { toast, toastError } from '../components/toast.js';
import { createJobModel, liveDiff } from '../lib/job-model.js';
import { createLogView } from './log-view.js';
import { createDiffView } from './diff-view.js';
import { clock, duration, relTime, absTime, jobTitle, jobKindLabel, isTerminal, parseTime, plural, isLocalSource } from '../lib/format.js';

const TABS = [
  { id: 'log', label: 'Log', icon: 'list' },
  { id: 'changes', label: 'Changes', icon: 'diff' },
  { id: 'result', label: 'Result', icon: 'check-circle' },
];

export function renderJob(outlet, params) {
  const id = params.id;
  const model = createJobModel();
  let job = null;
  let tab = params.tab || 'log';
  let stop = null;
  let tick = 0;
  let destroyed = false;
  let treeRaf = 0;
  let selected = null;
  let diffFetched = false;
  let diffAvailable = null; // null unknown, true/false after trying

  const titleEl = h('h1', null, h('span', { class: 'skel skel-title' }));
  const pillEl = h('span');
  const metaEl = h('div', { class: 'job-meta' });
  const actionsEl = h('div', { class: 'page-actions' });
  const bannerEl = h('div');
  const tabsEl = h('div', { class: 'tabs tabs-sm', role: 'tablist', 'aria-label': 'Job sections' });
  const treeEl = h('div', { class: 'card-body' });
  const sideEl = h('aside', { class: 'job-side card', 'aria-label': 'Modules and operations' },
    h('div', { class: 'card-head' }, h('h2', null, icon('tree', 'icon-sm'), 'Steps')), treeEl);
  const logView = createLogView({ model, downloadUrl: api.jobs.logUrl(id), onClearScope: () => select(null) });
  const diffView = createDiffView({ emptyHint: 'No target changed. A quick preview only sees newFiles and patch operations; a full diff also sees shell steps.' });
  const resultEl = h('div', { class: 'card-body stack' });
  const panels = {
    log: h('div', { role: 'tabpanel' }, logView.el),
    changes: h('div', { role: 'tabpanel', class: 'card-body' }, diffView.el),
    result: h('div', { role: 'tabpanel' }, resultEl),
  };
  let layoutEl = null;
  const mainCard = h('section', { class: 'card log-card' }, h('div', { class: 'card-head' }, tabsEl), panels.log, panels.changes, panels.result);

  layoutEl = h('div', { class: 'job-layout' }, sideEl, mainCard);
  outlet.appendChild(h('div', { class: 'page job-page' },
    h('nav', { class: 'mb-2 small', 'aria-label': 'Breadcrumb' }, h('a', { href: href('/jobs') }, icon('chevron-left', 'icon-sm'), 'Jobs')),
    h('div', { class: 'page-head job-head' },
      h('div', { class: 'title-block' }, titleEl, metaEl),
      actionsEl),
    bannerEl,
    layoutEl));

  paintTabs();
  paintTree();
  load();

  async function load() {
    try {
      job = await api.jobs.get(id);
    } catch (err) {
      if (destroyed) return;
      replace(outlet.firstChild, h('div', { class: 'empty' }, icon('history', 'icon-xl'),
        h('h3', null, err.status === 404 ? 'No such job' : 'Could not load this job'),
        h('p', null, err.status === 404 ? 'It may have been deleted from the history.' : err.message),
        h('div', { class: 'actions' }, h('a', { class: 'btn', href: href('/jobs') }, 'All jobs'))));
      return;
    }
    if (destroyed) return;
    model.state = model.state || job.state;
    document.title = `${jobTitle(job)} · ${jobKindLabel(job)} · loom`;
    if (tab === 'changes' && !hasDiffTab()) tab = 'log';
    paintHeader();
    paintTabs();
    paintResult();
    if (job.kind === 'generate' || job.kind === 'bulk') { sideEl.hidden = true; layoutEl.classList.add('no-side'); }
    stop = api.streamJob(id, {
      after: 0,
      onEvent,
      onEnd: () => refresh(),
      onStatus: (s) => { logView.setStatus(s); paintStream(s); },
    });
    tick = setInterval(paintElapsed, 1000);
  }

  function hasDiffTab() {
    if (!job) return true;
    return job.kind === 'diff' || (job.kind === 'run' && (job.request && job.request.mode) === 'local') || model.diffs.length > 0;
  }

  function onEvent(ev) {
    const what = model.apply(ev);
    if (what === 'log') logView.append(model.logs[model.logs.length - 1]);
    else if (what === 'tree') scheduleTree();
    else if (what === 'diff') { if (!diffFetched) diffView.set(liveDiff(model, false)); paintTabs(); }
    else if (what === 'pr') paintResult();
    else if (what === 'state') {
      if (job) job.state = ev.state;
      if (ev.error && job) job.error = ev.error;
      scheduleTree();
      paintHeader();
      emit('jobs-changed');
      if (isTerminal(ev.state)) refresh();
    }
  }

  async function refresh() {
    try {
      const j = await api.jobs.get(id);
      if (destroyed) return;
      job = j;
      paintHeader();
      paintResult();
      paintTabs();
      if (isTerminal(job.state) && hasDiffTab()) loadDiff();
    } catch { /* keep what we have */ }
  }

  async function loadDiff() {
    try {
      const d = await api.jobs.diff(id);
      if (destroyed) return;
      diffFetched = true;
      diffAvailable = true;
      diffView.set(d);
    } catch (err) {
      diffAvailable = false;
      if (model.diffs.length) diffView.set(liveDiff(model, job && job.state === 'failed'));
      else if (err.status !== 404) toastError(err, 'Could not load the diff');
      else diffView.set({ targets: [], incomplete: job && job.state === 'failed' });
    }
    paintTabs();
  }

  // ---------- Header ----------

  function paintHeader() {
    if (!job) return;
    const req = job.request || {};
    replace(titleEl, h('span', { class: 'break' }, jobTitle(job)), h('span', { class: 'muted' }, ` · ${jobKindLabel(job)}`));
    replace(pillEl, statePill(job.state, { large: true }));
    const src = req.source || req.module || (job.module && job.module.dir) || '';
    replace(metaEl,
      pillEl,
      h('span', { class: 'elapsed', 'data-elapsed': '' }),
      h('span', { title: absTime(job.createdAt) }, icon('clock', 'icon-sm'), `created ${relTime(job.createdAt)}`),
      src ? h('span', { class: 'mono small break' }, icon(isLocalSource(src) ? 'folder' : 'repo', 'icon-sm'),
        h('a', { href: href(`/m/${encodeURIComponent(src)}`) }, src)) : null,
      h('span', { class: 'mono small faint' }, job.id, copyButton(job.id, { label: 'Copy job id', what: 'Job id copied' })),
      h('span', { class: 'small faint stream-status' }));
    paintElapsed();

    const running = !isTerminal(job.state);
    const editable = job.kind === 'run' || job.kind === 'diff';
    replace(actionsEl,
      running ? h('button', { class: 'btn btn-danger', type: 'button', disabled: job.state === 'cancelling', onClick: cancel }, icon('stop'), job.state === 'cancelling' ? 'Cancelling…' : 'Cancel') : null,
      h('button', { class: 'btn', type: 'button', onClick: rerun, title: 'Start a new job with the same request' }, icon('refresh'), 'Re-run'),
      editable && src ? h('a', { class: 'btn', href: href(`/m/${encodeURIComponent(src)}/run`, { job: job.id }), title: 'Open the module form prefilled with this job’s values' }, icon('edit'), 'Re-run with edits') : null,
      job.cli ? h('button', { class: 'btn', type: 'button', onClick: () => showCli({ command: job.cli }, { title: 'This job as a command' }) }, icon('terminal'), 'Copy CLI') : null,
      h('a', { class: 'btn btn-ghost btn-icon', href: api.jobs.logUrl(id), download: `${id}.log`, title: 'Download log', 'aria-label': 'Download log' }, icon('download')),
      !running ? h('button', { class: 'btn btn-ghost btn-icon', type: 'button', title: 'Delete job', 'aria-label': 'Delete job', onClick: removeJob }, icon('trash')) : null);

    const err = job.error || model.error;
    replace(bannerEl,
      job.state === 'failed' && err ? h('div', { class: 'banner banner-err mb-4' }, icon('x-circle'), h('div', { class: 'banner-body' },
        h('div', { class: 'banner-title' }, 'The job failed'), h('pre', { class: 'mono' }, err))) : null,
      job.state === 'interrupted' ? h('div', { class: 'banner banner-warn mb-4' }, icon('alert'), h('div', { class: 'banner-body' },
        h('div', { class: 'banner-title' }, 'Interrupted'),
        h('div', null, 'loom serve stopped while this job was running. Branches may have been pushed; check the log, then re-run if needed.'))) : null,
      job.state === 'queued' ? h('div', { class: 'banner banner-info mb-4' }, icon('clock'), h('div', { class: 'banner-body' },
        h('div', null, 'Queued. Jobs that execute run one at a time; this one starts when the running job finishes.'))) : null,
      isTerminal(job.state) && job.result && job.result.prs && job.result.prs.length ? h('div', { class: 'banner banner-ok mb-4' }, icon('pr'), h('div', { class: 'banner-body' },
        h('div', { class: 'banner-title' }, `${plural(job.result.prs.length, 'pull request')} opened`),
        h('div', null, h('a', { href: href(`/jobs/${encodeURIComponent(id)}/result`) }, 'See them in Result')))) : null);
  }

  function paintElapsed() {
    if (!job) return;
    const el = metaEl.querySelector('[data-elapsed]');
    if (!el) return;
    const created = parseTime(job.createdAt);
    const start = parseTime(job.startedAt);
    const end = parseTime(job.endedAt);
    if (job.state === 'queued' && created) {
      replace(el, icon('clock', 'icon-sm'), `waiting ${clock(Date.now() - created)}`);
    } else if (start) {
      const ms = (end || new Date()) - start;
      replace(el, icon('clock', 'icon-sm'), isTerminal(job.state) ? `took ${duration(ms)}` : `running ${clock(ms)}`);
    } else {
      replace(el);
    }
    if (isTerminal(job.state) && tick) { clearInterval(tick); tick = 0; }
  }

  function paintStream(s) {
    const el = metaEl.querySelector('.stream-status');
    if (!el) return;
    el.textContent = s === 'reconnecting' ? 'reconnecting to the event stream…' : '';
  }

  // ---------- Tabs ----------

  function paintTabs() {
    const visible = TABS.filter((t) => t.id !== 'changes' || hasDiffTab());
    if (!visible.find((t) => t.id === tab)) tab = 'log';
    const counts = {
      changes: model.diffs.length || (diffView.get() ? (diffView.get().targets || []).reduce((n, t) => n + (t.files || []).length, 0) : 0),
      result: job && job.result && job.result.prs ? job.result.prs.length : 0,
    };
    replace(tabsEl, visible.map((t) => h('a', {
      class: 'tab', role: 'tab', href: href(`/jobs/${encodeURIComponent(id)}/${t.id}`), 'aria-selected': String(tab === t.id),
    }, icon(t.icon, 'icon-sm'), t.label, counts[t.id] ? h('span', { class: 'count' }, String(counts[t.id])) : null)));
    for (const [k, p] of Object.entries(panels)) p.hidden = k !== tab;
    if (tab === 'changes' && !diffFetched && job && isTerminal(job.state) && diffAvailable === null) loadDiff();
    if (tab === 'changes' && !diffView.get()) diffView.set(model.diffs.length || (job && isTerminal(job.state)) ? liveDiff(model, false) : null);
  }

  // ---------- Tree ----------

  function scheduleTree() {
    if (!treeRaf) treeRaf = requestAnimationFrame(() => { treeRaf = 0; paintTree(); });
  }

  function select(node) {
    selected = node;
    const label = node ? (node.type === 'op' ? `${node.path.join(' › ')} › ${node.name}` : node.path.join(' › ')) : '';
    logView.setScope(node, label);
    paintTree();
    if (node && tab !== 'log') navigate(`/jobs/${encodeURIComponent(id)}/log`);
  }

  function paintTree() {
    if (!model.roots.length) {
      replace(treeEl, h('p', { class: 'small muted' }, !job || job.state === 'queued' ? 'Steps appear here once the job starts.' : 'This job reports no module steps.'));
      return;
    }
    replace(treeEl, h('ul', { class: 'jtree' }, model.roots.map((n) => moduleItem(n, true))));
  }

  function moduleItem(n, isRoot) {
    const progress = n.status === 'running' && n.total ? `${n.ops.filter((o) => o.status !== 'running' && o.status !== 'pending').length}/${n.total}` : '';
    const btn = h('button', {
      class: 'jnode module', type: 'button', 'aria-pressed': String(selected === n),
      title: n.reason ? `Skipped: ${n.reason}` : `Filter the log to ${n.path.join(' › ')}`,
      onClick: () => select(selected === n ? null : n),
    },
    nodeStatusIcon(n.status),
    h('span', { class: 'jn-label mono' }, isRoot && n.children.length ? `≡ ${n.name} ≡` : n.name),
    h('span', { class: 'jn-dur' }, progress || (n.durationMs !== null ? duration(n.durationMs) : '')),
    n.module && n.module !== n.name ? h('span', { class: 'jn-sub' }, n.module) : null,
    n.reason ? h('span', { class: 'jn-sub' }, n.reason) : null);
    const kids = [
      ...n.children.map((c) => ({ i: order(c), el: moduleItem(c, false) })),
      ...n.ops.map((o) => ({ i: 1e6 + o.index, el: opItem(o) })),
    ].sort((a, b) => a.i - b.i).map((x) => x.el);
    return h('li', null, btn, kids.length ? h('ul', null, kids) : null);
  }

  // Children run before their parent's operations, in the order they started.
  function order(c) {
    const t = parseTime(c.start);
    return t ? t.getTime() / 1e7 : 0;
  }

  function opItem(o) {
    return h('li', null, h('button', {
      class: 'jnode op', type: 'button', 'aria-pressed': String(selected === o),
      title: o.error || o.reason || `Filter the log to operation ${o.name}`,
      onClick: () => select(selected === o ? null : o),
    },
    nodeStatusIcon(o.status),
    h('span', { class: 'jn-label mono' }, o.name, ' ', h('span', { class: 'jn-kind' }, o.kind)),
    h('span', { class: 'jn-dur' }, o.durationMs !== null ? duration(o.durationMs) : ''),
    o.error ? h('span', { class: 'jn-sub status-err' }, o.error) : null,
    o.reason ? h('span', { class: 'jn-sub' }, o.reason) : null));
  }

  // ---------- Result ----------

  function paintResult() {
    if (!job) { replace(resultEl, h('div', { class: 'skel skel-card' })); return; }
    const r = job.result || {};
    const prs = (r.prs && r.prs.length ? r.prs : model.prs) || [];
    const req = job.request || {};
    const blocks = [];
    if (prs.length) {
      blocks.push(h('section', { class: 'section' }, h('div', { class: 'section-title' }, `Pull requests`, h('span', { class: 'count' }, String(prs.length)), h('span', { class: 'rule' })),
        h('ul', { class: 'pr-list' }, prs.map((p) => h('li', { class: 'pr-item' }, icon('pr'),
          h('div', { class: 'pr-body' },
            h('span', { class: 'pr-title' }, p.title || p.url),
            p.path && p.path.length ? crumbs(p.path) : (p.module ? h('span', { class: 'small muted' }, p.module) : null),
            h('a', { class: 'pr-url', href: p.url, target: '_blank', rel: 'noopener noreferrer' }, p.url, ' ', icon('external', 'icon-sm'))),
          h('span', { class: 'spacer' }),
          copyButton(p.url, { label: 'Copy link', what: 'Link copied' })))),
        h('div', { class: 'mt-2' }, copyButton(() => prs.map((p) => p.url).join('\n'), { label: 'Copy all links', iconOnly: false, cls: 'btn btn-sm', what: 'Links copied' }))));
    }
    if (r.workspace) {
      blocks.push(h('section', { class: 'section' }, h('div', { class: 'section-title' }, 'Workspace', h('span', { class: 'rule' })),
        h('div', { class: 'row' }, icon('folder'), h('code', { class: 'break' }, r.workspace), copyButton(r.workspace, { label: 'Copy path', what: 'Path copied' })),
        h('p', { class: 'small muted mt-1' }, 'The local clones and commits of this run. Deleting the job removes it.')));
    }
    if (r.diff) {
      blocks.push(h('section', { class: 'section' }, h('div', { class: 'section-title' }, 'Changes', h('span', { class: 'rule' })),
        h('p', null, `${plural(r.diff.files || 0, 'file')} in ${plural(r.diff.targets || 0, 'target')}`, r.diff.incomplete ? ' — incomplete, the run failed' : '', ' · ',
          h('a', { href: href(`/jobs/${encodeURIComponent(id)}/changes`) }, 'view'))));
    }
    if ((job.kind === 'generate' || job.kind === 'bulk') && req.output) {
      blocks.push(h('section', { class: 'section' }, h('div', { class: 'section-title' }, job.kind === 'bulk' ? 'Wrapper module' : 'Generated module', h('span', { class: 'rule' })),
        h('div', { class: 'row row-wrap' }, icon('module'), h('code', { class: 'break' }, req.output),
          job.state === 'succeeded' ? h('a', { class: 'btn btn-primary btn-sm', href: href(`/m/${encodeURIComponent(req.output)}`) }, 'Open module', icon('arrow-right', 'icon-sm')) : null)));
    }
    blocks.push(requestView(job));
    if (!prs.length && !r.workspace && !r.diff && job.kind !== 'generate' && job.kind !== 'bulk' && isTerminal(job.state)) {
      blocks.unshift(h('p', { class: 'muted' }, job.state === 'succeeded' ? 'The job finished. It opened no pull request.' : 'No result.'));
    }
    replace(resultEl, blocks);
    paintTabs();
  }

  // ---------- Actions ----------

  async function cancel() {
    const ok = await confirmDialog({ title: 'Cancel this job?', message: 'Running shell steps, git and dynamic-param commands are stopped. Anything already pushed stays pushed.', confirmLabel: 'Cancel job', danger: true });
    if (!ok) return;
    try {
      await api.jobs.cancel(id);
      toast('Cancelling…');
    } catch (err) {
      toastError(err, 'Could not cancel');
    }
  }

  async function rerun() {
    if (job && job.kind === 'run' && job.request && job.request.mode === 'execute') {
      const ok = await confirmDialog({ title: 'Execute again?', message: 'This repeats the run as it was: it may push branches and open pull requests again. To review first, use “Re-run with edits”, which shows the confirmation with its full plan.', confirmLabel: 'Execute again' });
      if (!ok) return;
    }
    try {
      const r = await api.jobs.rerun(id);
      emit('jobs-changed');
      navigate(`/jobs/${encodeURIComponent(r.id)}`);
    } catch (err) {
      toastError(err, 'Could not re-run');
    }
  }

  async function removeJob() {
    const ok = await confirmDialog({ title: 'Delete this job?', message: 'It is removed from the history with its log and managed workspace. Pull requests it opened are not touched.', confirmLabel: 'Delete job', danger: true });
    if (!ok) return;
    try {
      await api.jobs.remove(id);
      emit('jobs-changed');
      toast('Job deleted', { kind: 'ok' });
      navigate('/jobs');
    } catch (err) {
      toastError(err, 'Could not delete');
    }
  }

  return {
    update(p) {
      if (p.id !== id) return false;
      tab = p.tab || 'log';
      paintTabs();
      return true;
    },
    destroy() {
      destroyed = true;
      if (stop) stop();
      clearInterval(tick);
      if (treeRaf) cancelAnimationFrame(treeRaf);
      logView.destroy();
    },
  };
}

function requestView(job) {
  const req = job.request || {};
  const rows = [];
  if (req.mode) rows.push(['Mode', req.mode]);
  if (job.kind === 'diff') rows.push(['Fidelity', req.quick ? 'quick (executes nothing)' : 'full (runs locally)']);
  if (req.targetPath) rows.push(['Target path', req.targetPath]);
  if (req.refs) rows.push(['Pull requests', req.refs.join('\n')]);
  if (req.tokenEnv) rows.push(['Token variable', req.tokenEnv]);
  if (req.nameParam) rows.push(['Name param', req.nameParam]);
  if (req.name) rows.push(['Name', req.name]);
  if (req.author) rows.push(['Author', req.author]);
  if (req.email) rows.push(['Email', req.email]);
  const params = req.params || req.values || null;
  return h('section', { class: 'section' },
    h('div', { class: 'section-title' }, 'Request', h('span', { class: 'rule' })),
    rows.length ? h('dl', { class: 'kv' }, rows.map(([k, v]) => [h('dt', null, k), h('dd', { class: 'mono small' }, v)])) : null,
    params && Object.keys(params).length ? h('div', { class: 'mt-3' },
      h('div', { class: 'small muted mb-2' }, job.kind === 'generate' ? 'Values parameterized' : 'Params'),
      h('dl', { class: 'kv' }, Object.entries(params).map(([k, v]) => [
        h('dt', { class: 'mono' }, k),
        h('dd', null, typeof v === 'string' && !v.includes('\n') ? h('code', null, v) : h('pre', { class: 'code' }, typeof v === 'string' ? v : JSON.stringify(v, null, 2)))]))) : null,
    req.items ? h('div', { class: 'mt-3' }, h('div', { class: 'small muted mb-2' }, plural(req.items.length, 'item')),
      h('pre', { class: 'code' }, req.items.map((it) => JSON.stringify(it)).join('\n'))) : null);
}

