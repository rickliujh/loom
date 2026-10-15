// Overview tab: the `loom inspect` report as a tree. Listed submodules expand
// in place by re-querying with modules:[path]; the summary is recomputed from
// the tree, so it always covers exactly what is on screen.

import { h, icon, replace, templateSpans, downloadText } from '../dom.js';
import * as api from '../api.js';
import { prefs } from '../store.js';
import { moduleChip, crumbs, opKind, typeBadge } from '../components/chip.js';
import { copyButton } from '../components/copy.js';
import { showCli } from '../components/cli.js';
import { toastError } from '../components/toast.js';
import { plural, breadcrumb } from '../lib/format.js';
import { rootOf, loadErrorBanner } from './module.js';

export function renderOverview(el, ctx) {
  let depthAll = false;
  let noFetch = prefs.get('inspect.noFetch', false);
  let report = null;
  let loading = false;
  let seq = 0;

  const summaryEl = h('div');
  const treeEl = h('div', { class: 'itree' });
  const depthSw = switchInput('Describe every level', depthAll, (v) => { depthAll = v; load(); });
  const fetchSw = switchInput('Don’t fetch remote modules', noFetch, (v) => { noFetch = v; prefs.set('inspect.noFetch', v); load(); });

  const toolbar = h('div', { class: 'toolbar' },
      depthSw, fetchSw,
      h('span', { class: 'spacer' }),
      h('button', { class: 'btn btn-sm', type: 'button', onClick: () => report && showCli({ command: report.cli }, { title: 'Inspect as CLI' }) }, icon('terminal', 'icon-sm'), 'Copy as CLI'),
      h('button', { class: 'btn btn-sm', type: 'button', onClick: () => report && downloadText('inspect.json', JSON.stringify(report, null, 2), 'application/json') }, icon('download', 'icon-sm'), 'JSON'));

  if (ctx.loadError) {
    el.appendChild(loadErrorBanner(ctx.loadError, ctx));
    return {};
  }

  el.appendChild(h('div', { class: 'stack' },
    toolbar,
    h('p', { class: 'small muted' }, 'Nothing runs to build this view: no operation, no dynamic-param command, no condition. Values reflect the Params & Run form.'),
    summaryEl,
    treeEl));

  const unsub = ctx.onLive(() => { if (!depthAll && !loading) { report = clone(ctx.live); paint(); } });
  report = clone(ctx.live || ctx.base);
  paint();

  async function load() {
    const my = ++seq;
    loading = true;
    treeEl.classList.add('loading');
    try {
      const r = await api.inspect({ source: ctx.source, params: ctx.getParams({ forInspect: true }), depth: depthAll ? 0 : 1, modules: [], noFetch });
      if (my !== seq) return;
      report = r;
    } catch (err) {
      if (my !== seq) return;
      replace(summaryEl, loadErrorBanner(err, ctx));
      return;
    } finally {
      loading = false;
      treeEl.classList.remove('loading');
    }
    paint();
  }

  async function expand(node, path, btn) {
    btn.disabled = true;
    replace(btn, icon('spinner', 'icon-sm spin'), 'Reading…');
    try {
      const r = await api.inspect({ source: ctx.source, params: ctx.getParams({ forInspect: true }), depth: 1, modules: [path.join('/')], noFetch });
      const sub = r && r.modules && r.modules[0] && r.modules[0].module;
      if (sub) {
        for (const k of Object.keys(node)) delete node[k];
        Object.assign(node, sub);
      }
      paint();
    } catch (err) {
      btn.disabled = false;
      replace(btn, icon('expand', 'icon-sm'), 'Expand');
      toastError(err, `Could not read ${breadcrumb(path)}`);
    }
  }

  function paint() {
    const root = rootOf(report);
    if (!root) { replace(treeEl, h('div', { class: 'skel skel-card' })); return; }
    const basePath = report.modules[0].path || [root.instance];
    const roll = rollup(root, basePath);
    const problems = [...(report.problems || []), ...roll.errors.map((e) => `${breadcrumb(e.path)}: ${e.error}`)];
    replace(summaryEl, h('div', { class: 'summary-box' },
      roll.missing.length
        ? h('div', { class: 'banner banner-warn' }, icon('alert'), h('div', { class: 'banner-body' },
          h('div', { class: 'banner-title' }, `${plural(roll.missing.length, 'required parameter')} not supplied — a run would fail`),
          h('ul', { class: 'stack-sm mt-1' }, roll.missing.map((m) => h('li', { class: 'row row-wrap' },
            h('code', null, m.name), crumbs(m.path),
            m.path.length > 1 ? h('span', { class: 'small muted' }, '— its parent must pass it (module authoring issue)') : null)))))
        : h('div', { class: 'banner banner-ok' }, icon('check-circle'), h('div', { class: 'banner-body' },
          h('div', { class: 'banner-title' }, 'Every required parameter of the modules shown is satisfied'))),
      roll.unexpanded.length
        ? h('div', { class: 'banner banner-info' }, icon('info'), h('div', { class: 'banner-body' },
          h('div', { class: 'banner-title' }, `${plural(roll.unexpanded.length, 'submodule')} not expanded — ${roll.unexpanded.length === 1 ? 'it' : 'they'} may need parameters of ${roll.unexpanded.length === 1 ? 'its' : 'their'} own`),
          h('div', { class: 'row row-wrap' }, roll.unexpanded.map((p) => crumbs(p)))))
        : null,
      problems.length
        ? h('div', { class: 'banner banner-err' }, icon('x-circle'), h('div', { class: 'banner-body' },
          h('div', { class: 'banner-title' }, `${plural(problems.length, 'module')} could not be described`),
          problems.map((p) => h('div', { class: 'mono small' }, p))))
        : null));
    replace(treeEl, nodeView(root, basePath, true));
  }

  function nodeView(node, path, isRoot) {
    const children = node.modules || [];
    const head = h('div', { class: 'inode-head' },
      isRoot
        ? moduleChip(node.instance || node.name, { root: children.length > 0 })
        : h('span', { class: 'handoff' }, h('span', { class: 'tri' }, '▸ '), h('strong', { class: 'mono' }, node.instance)),
      !isRoot && node.name && node.name !== node.instance ? h('span', { class: 'muted mono small' }, `(${node.name})`) : null,
      node.source ? h('span', { class: 'src' }, node.source) : (isRoot && node.dir ? h('span', { class: 'src' }, node.dir) : null),
      node.sourceTemplate ? h('span', { class: 'src' }, '← ', templateSpans(node.sourceTemplate)) : null,
      node.remote ? h('span', { class: 'badge' }, icon('repo'), 'remote') : null,
      node.if ? h('span', { class: 'cond' }, `if: ${node.if}`) : null,
      node.listed ? h('span', { class: 'badge', title: 'Listed but not read: only what the parent declares is known' }, node.unfetched ? 'not fetched' : 'listed …') : null,
      node.cycle ? h('span', { class: 'badge badge-err' }, 'cycle') : null,
      node.listed && !node.cycle ? (() => {
        const b = h('button', { class: 'btn btn-sm', type: 'button' }, icon('expand', 'icon-sm'), 'Expand');
        b.addEventListener('click', () => expand(node, path, b));
        return b;
      })() : null);

    if (node.listed || node.cycle) return h('div', { class: 'inode' }, head);

    const body = h('div', { class: 'inode-body' },
      node.error ? h('div', { class: 'banner banner-err' }, icon('x-circle'), h('div', { class: 'banner-body mono small' }, node.error)) : null,
      (node.warnings || []).length ? h('div', { class: 'banner banner-warn' }, icon('alert'), h('div', { class: 'banner-body' },
        node.warnings.map((w) => h('div', { class: 'small' }, w)))) : null,
      node.params && node.params.length ? block('Parameters', paramsTable(node.params), node.params.length) : null,
      node.target ? block('Target', targetView(node.target)) : null,
      node.operations && node.operations.length ? block(`Operations (${node.operations.length}, in order)`, opList(node.operations)) : null,
      (node.excludes || []).length || (node.includes || []).length ? block('File filters', h('div', { class: 'card-body small stack-sm' },
        (node.excludes || []).length ? h('div', null, h('span', { class: 'muted' }, 'excludes '), node.excludes.map((x) => h('code', { class: 'mr' }, `${x} `))) : null,
        (node.includes || []).length ? h('div', null, h('span', { class: 'muted' }, 'includes '), node.includes.map((x) => h('code', null, `${x} `))) : null)) : null);

    return h('div', { class: 'inode' }, head, body,
      children.length ? h('div', { class: 'inode-children' }, children.map((c) => nodeView(c, [...path, c.instance], false))) : null);
  }

  return {
    shown() { if (!depthAll) { report = clone(ctx.live || ctx.base); paint(); } },
    destroy() { unsub(); },
  };
}

function clone(x) { return x ? JSON.parse(JSON.stringify(x)) : x; }

function block(title, content, count) {
  return h('div', { class: 'inode-block' }, h('div', { class: 'ib-head' }, title, count ? h('span', { class: 'faint' }, String(count)) : null), content);
}

function switchInput(label, checked, onChange) {
  const input = h('input', { type: 'checkbox', checked, onChange: () => onChange(input.checked) });
  return h('label', { class: 'switch' }, input, h('span', { class: 'track' }), label);
}

const STATE_TEXT = { missing: 'required', unset: 'optional' };

export function paramStateLabel(s) { return STATE_TEXT[s] || s; }

function compact(v) {
  if (v === undefined || v === null) return '';
  if (typeof v === 'string') return JSON.stringify(v);
  const s = JSON.stringify(v);
  const size = Array.isArray(v) ? `[${plural(v.length, 'item')}]` : `{${plural(Object.keys(v).length, 'key')}}`;
  return `${size} ${s.length > 120 ? s.slice(0, 117) + '…' : s}`;
}

function paramsTable(params) {
  return h('table', { class: 'ptable' }, h('tbody', null, params.map((p) => {
    let val;
    switch (p.state) {
      case 'provided':
      case 'default':
        val = [p.valueYaml && (p.type === 'list' || p.type === 'map')
          ? h('details', null, h('summary', { class: 'mono' }, compact(p.value)), h('pre', { class: 'code mt-1' }, p.valueYaml))
          : h('span', null, `= ${compact(p.value)}`),
        p.from !== undefined && p.from !== null ? h('span', { class: 'from' }, ' ← ', templateSpans(typeof p.from === 'string' ? p.from : JSON.stringify(p.from))) : null];
        break;
      case 'dynamic':
        val = [h('span', { class: 'faint' }, '$ '), p.command, p.default !== undefined ? h('span', { class: 'from' }, ` (falls back to ${compact(p.default)})`) : null];
        break;
      case 'missing': val = h('span', { class: 'status-err' }, 'must be supplied'); break;
      case 'unresolved': val = [h('span', { class: 'status-warn' }, 'resolved at run time'), p.from !== undefined ? h('span', { class: 'from' }, ' ← ', templateSpans(typeof p.from === 'string' ? p.from : JSON.stringify(p.from))) : null]; break;
      default: val = h('span', { class: 'faint' }, p.type === 'list' ? '[]' : p.type === 'map' ? '{}' : '""');
    }
    return h('tr', null,
      h('td', { class: 'pn' }, p.name, p.required ? h('span', { class: 'status-err', title: 'required' }, '*') : null, ' ', typeBadge(p.type)),
      h('td', { class: ['ps', `st-${p.state}`] }, paramStateLabel(p.state)),
      h('td', { class: 'pv' }, val));
  })));
}

function targetView(t) {
  const v = (s) => h('span', { class: ['tv-val', /\{\{/.test(s || '') && 'unresolved'] }, s ? templateSpans(s) : '—');
  return h('div', { class: 'card-body target-view' },
    h('div', { class: 'tv-line' }, icon('repo', 'icon-sm'), v(t.url), copyButton(t.url || '', { label: 'Copy URL' })),
    h('div', { class: 'tv-line' }, icon('branch', 'icon-sm'), v(t.branch), h('span', { class: 'faint' }, '→'), v(t.featureBranch)));
}

function opList(ops) {
  return h('ol', { class: 'oplist' }, ops.map((o, i) => h('li', null,
    h('span', { class: 'idx' }, String(i + 1)),
    h('span', { class: 'oname' }, o.name),
    opKind(o.kind),
    h('span', { class: 'odetail' }, templateSpans(o.detail || '')),
    o.if ? h('span', { class: 'ocond' }, `if: ${o.if}`) : null,
    o.error ? h('span', { class: 'oerr' }, o.error) : null)));
}

/** Missing params, listed modules and errors across the described tree. */
export function rollup(node, path, acc = { missing: [], unexpanded: [], errors: [] }) {
  if (node.listed) { acc.unexpanded.push(path); return acc; }
  if (node.error) acc.errors.push({ path, error: node.error });
  for (const p of node.params || []) if (p.state === 'missing' || p.state === 'required') acc.missing.push({ path, name: p.name });
  for (const c of node.modules || []) rollup(c, [...path, c.instance], acc);
  return acc;
}
