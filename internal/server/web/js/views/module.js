// Module page (#/m/<source>/<tab>): header, tabs, and the shared context the
// tabs read from. Tab panels are built once and kept, so switching tabs never
// loses what was typed into the params form or an open editor.

import { h, icon, replace } from '../dom.js';
import * as api from '../api.js';
import { state } from '../store.js';
import { href, navigate } from '../router.js';
import { isLocalSource, baseName } from '../lib/format.js';
import { copyButton } from '../components/copy.js';
import { renderOverview } from './overview.js';
import { renderParamsTab } from './params-form.js';
import { renderFiles } from './files.js';
import { renderValidate } from './validate.js';
import { renderModuleHistory } from './history.js';

const TABS = [
  { id: 'run', label: 'Params & Run', icon: 'play' },
  { id: 'overview', label: 'Overview', icon: 'tree' },
  { id: 'files', label: 'Files', icon: 'files', local: true },
  { id: 'validate', label: 'Validate', icon: 'shield' },
  { id: 'history', label: 'History', icon: 'history' },
];

export function renderModule(outlet, params, query) {
  const source = params.source;
  const ctx = {
    source,
    local: isLocalSource(source),
    base: null, // inspect report with no params: declarations, defaults
    live: null, // latest inspect report with the form's params
    loadError: null,
    entry: (state.modules || []).find((m) => m.dir === source) || null,
    listeners: new Set(),
    onLive(fn) { this.listeners.add(fn); return () => this.listeners.delete(fn); },
    setLive(report) { this.live = report; this.listeners.forEach((fn) => fn(report)); paintHeaderTarget(); },
    getParams: () => ({}), // replaced by the params tab once it exists
    query,
  };

  const titleEl = h('h1', null, h('span', { class: 'skel skel-title' }));
  const targetEl = h('div', { class: 'subtitle' });
  const headActions = h('div', { class: 'page-actions' });
  const tabsEl = h('div', { class: 'tabs', role: 'tablist', 'aria-label': 'Module sections' });
  const panelHost = h('div');
  const panels = new Map();
  const destroyers = [];
  let current = null;

  const page = h('div', { class: 'page' },
    h('nav', { class: 'mb-2 small', 'aria-label': 'Breadcrumb' }, h('a', { href: href('/') }, icon('chevron-left', 'icon-sm'), 'Modules')),
    h('div', { class: 'page-head' },
      h('div', { class: 'title-block' },
        titleEl,
        h('div', { class: 'subtitle' },
          icon(ctx.local ? 'folder' : 'repo', 'icon-sm'),
          h('span', { class: 'path mono' }, source),
          copyButton(source, { label: 'Copy path', what: 'Path copied' })),
        targetEl),
      headActions),
    tabsEl,
    panelHost);
  outlet.appendChild(page);

  function paintTabs() {
    replace(tabsEl, TABS.map((t) => {
      const disabled = (t.local && !ctx.local) || (t.id === 'run' && ctx.loadError);
      return h('a', {
        class: 'tab', role: 'tab', href: href(`/m/${encodeURIComponent(source)}/${t.id}`),
        'aria-selected': String(current === t.id), 'aria-disabled': disabled ? 'true' : null,
        tabindex: current === t.id ? '0' : '-1',
        onKeydown: (e) => {
          const links = [...tabsEl.querySelectorAll('.tab')];
          const i = links.indexOf(e.currentTarget);
          if (e.key === 'ArrowRight') { e.preventDefault(); links[(i + 1) % links.length].focus(); }
          if (e.key === 'ArrowLeft') { e.preventDefault(); links[(i - 1 + links.length) % links.length].focus(); }
        },
      }, icon(t.icon, 'icon-sm'), t.label);
    }));
  }

  function show(tab, q) {
    const known = TABS.find((t) => t.id === tab);
    if (!known) tab = ctx.loadError ? 'overview' : 'run';
    if (tab === 'files' && !ctx.local) tab = 'overview';
    if (tab === 'run' && ctx.loadError) tab = 'overview';
    current = tab;
    ctx.query = q || {};
    paintTabs();
    for (const [id, p] of panels) p.el.hidden = id !== tab;
    if (!panels.has(tab)) {
      const el = h('div', { role: 'tabpanel' });
      panelHost.appendChild(el);
      const ctl = build(tab, el) || {};
      panels.set(tab, { el, ctl });
      if (ctl.destroy) destroyers.push(ctl.destroy);
    } else {
      const p = panels.get(tab);
      if (p.ctl.shown) p.ctl.shown(ctx.query);
    }
  }

  function build(tab, el) {
    switch (tab) {
      case 'overview': return renderOverview(el, ctx);
      case 'run': return renderParamsTab(el, ctx);
      case 'files': return renderFiles(el, ctx);
      case 'validate': return renderValidate(el, ctx);
      case 'history': return renderModuleHistory(el, ctx);
      default: return null;
    }
  }

  function paintHeader() {
    const root = ctx.base && ctx.base.modules && ctx.base.modules[0] ? ctx.base.modules[0].module : null;
    const name = (root && (root.name || root.instance)) || (ctx.entry && ctx.entry.name) || baseName(source.replace(/\/\/[^/]*$/, '')) || source;
    const orchestrator = root && root.modules && root.modules.length > 0;
    replace(titleEl,
      h('span', { class: ['title-mark', orchestrator && 'root'], title: orchestrator ? 'Orchestrator: composes other modules' : 'Module' }, icon(orchestrator ? 'root' : 'module')),
      h('span', { class: 'break' }, name),
      ctx.local ? null : h('span', { class: 'badge' }, icon('repo'), 'git'),
      ctx.loadError ? h('span', { class: 'badge badge-err' }, icon('alert'), 'does not load') : null);
    document.title = `${name} · loom`;
    replace(headActions,
      ctx.loadError ? null : h('a', { class: 'btn', href: href('/bulk', { module: source }), title: 'Scaffold a wrapper that runs this module once per item' }, icon('layers'), 'Bulk run…'));
    paintHeaderTarget();
  }

  function paintHeaderTarget() {
    const rep = ctx.live || ctx.base;
    const root = rep && rep.modules && rep.modules[0] ? rep.modules[0].module : null;
    if (!root) { replace(targetEl); return; }
    const t = root.target;
    if (!t || !t.url) {
      replace(targetEl, icon('folder', 'icon-sm'), h('span', { class: 'muted' }, 'No target repository — runs write into a directory'));
      return;
    }
    replace(targetEl, icon('branch', 'icon-sm'),
      h('span', { class: ['mono', 'break', /\{\{/.test(t.url) && 'status-warn'] }, t.url),
      t.branch ? h('span', { class: 'mono muted' }, `(${t.branch})`) : null,
      t.featureBranch ? [h('span', { class: 'faint' }, '→'), h('span', { class: ['mono', /\{\{/.test(t.featureBranch) && 'status-warn'] }, t.featureBranch)] : null);
  }

  async function loadBase() {
    try {
      ctx.base = await api.inspect({ source, params: {}, depth: 1, modules: [], noFetch: false });
      ctx.loadError = null;
    } catch (err) {
      if (err instanceof api.ApiError && err.status === 401) return;
      ctx.loadError = err;
    }
    paintHeader();
  }

  paintTabs();
  const initialTab = params.tab;
  loadBase().then(() => show(initialTab, query));

  return {
    update(p, q) {
      if (p.source !== source) return false;
      if (ctx.base || ctx.loadError) show(p.tab, q);
      return true;
    },
    canLeave(unload) {
      for (const [, p] of panels) if (p.ctl.canLeave && !p.ctl.canLeave(unload)) return false;
      return true;
    },
    destroy() { destroyers.forEach((d) => { try { d(); } catch { /* ignore */ } }); },
  };
}

/** Helper for tabs: the root inspection node of a report. */
export function rootOf(report) {
  return report && report.modules && report.modules[0] ? report.modules[0].module : null;
}

export function loadErrorBanner(err, { local, source } = {}) {
  const lines = api.errorLines(err);
  return h('div', { class: 'banner banner-err' }, icon('alert'),
    h('div', { class: 'banner-body' },
      h('div', { class: 'banner-title' }, err.status === 403 ? 'Not allowed' : 'This module does not load'),
      h('div', null, err.message),
      lines.length ? h('pre', { class: 'mono' }, lines.join('\n')) : null,
      local ? h('div', null, 'Open the ', h('a', { href: href(`/m/${encodeURIComponent(source)}/files`, { path: 'loom.yaml' }) }, 'Files tab'), ' to fix its config.') : null));
}

export { navigate };
