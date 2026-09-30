// The confirmation before Execute: what will be cloned, which shell steps will
// run, and which operations push branches and open pull requests. It is read
// from a full inspect of the tree, which executes nothing.

import { h, icon, templateSpans, replace } from '../dom.js';
import * as api from '../api.js';
import { state } from '../store.js';
import { href } from '../router.js';
import { openModal } from '../components/modal.js';
import { crumbs, opKind } from '../components/chip.js';
import { plural, relTime } from '../lib/format.js';
import { rootOf } from './module.js';

function walk(node, path, acc) {
  if (node.listed || node.cycle) { acc.unknown.push({ path, node }); return; }
  if (node.error) acc.errors.push({ path, error: node.error });
  if (node.target && node.target.url) acc.targets.push({ path, t: node.target });
  for (const op of node.operations || []) {
    const item = { path, op };
    if (op.kind === 'shell') acc.shells.push(item);
    else if (op.kind === 'commitPush') acc.pushes.push(item);
    else if (op.kind === 'pr') acc.prs.push(item);
    else if (op.kind === 'llm') acc.llms.push(item);
  }
  for (const p of node.params || []) if (p.state === 'missing') acc.missing.push({ path, name: p.name });
  for (const c of node.modules || []) walk(c, [...path, c.instance], acc);
}

export function confirmExecute(ctx, req) {
  return new Promise((resolve) => {
    const body = h('div', { class: 'stack' },
      h('div', { class: 'stack-sm' }, Array.from({ length: 5 }, (_, i) => h('div', { class: `skel skel-line skel-w-${[90, 70, 50, 90, 30][i]}` }))));
    const confirmBtn = h('button', { class: 'btn btn-primary', type: 'button', disabled: true, onClick: () => m.close(true) }, icon('play'), 'Execute');
    const cancelBtn = h('button', { class: 'btn', type: 'button', onClick: () => m.close(false) }, 'Cancel');
    const m = openModal({
      title: 'Execute this run?',
      body,
      wide: true,
      footer: [cancelBtn, confirmBtn],
      onClose: (r) => resolve(r === true),
      initialFocus: () => cancelBtn,
    });

    api.inspect({ source: ctx.source, params: req.params, depth: 0, modules: [], noFetch: false }).then((rep) => {
      const root = rootOf(rep);
      const acc = { targets: [], shells: [], pushes: [], prs: [], llms: [], unknown: [], errors: [], missing: [] };
      if (root) walk(root, (rep.modules[0] && rep.modules[0].path) || [root.instance], acc);
      const lp = state.lastPreview.get(ctx.source);
      const pushing = acc.pushes.length + acc.prs.length;

      const section = (ic, title, items, render, empty) => h('section', { class: 'confirm-section' },
        h('h3', null, icon(ic, 'icon-sm'), title, h('span', { class: 'badge' }, String(items.length))),
        items.length ? h('ul', { class: 'confirm-list' }, items.map(render)) : h('p', { class: 'small muted' }, empty));

      replace(body, 
        acc.missing.length ? h('div', { class: 'banner banner-err' }, icon('alert'), h('div', { class: 'banner-body' },
          h('div', { class: 'banner-title' }, `${plural(acc.missing.length, 'required parameter')} missing — this run would fail`),
          acc.missing.map((x) => h('div', { class: 'row row-wrap' }, h('code', null, x.name), crumbs(x.path))))) : null,
        acc.errors.length ? h('div', { class: 'banner banner-err' }, icon('x-circle'), h('div', { class: 'banner-body' },
          h('div', { class: 'banner-title' }, 'Some modules could not be read'),
          acc.errors.map((x) => h('div', { class: 'small mono' }, `${x.path.join(' › ')}: ${x.error}`)))) : null,
        h('div', { class: pushing ? 'banner banner-warn' : 'banner banner-info' }, icon(pushing ? 'alert' : 'info'), h('div', { class: 'banner-body' },
          h('div', { class: 'banner-title' }, pushing
            ? `This run pushes ${plural(acc.pushes.length, 'branch', 'branches')} and opens ${plural(acc.prs.length, 'pull request')}.`
            : 'This run pushes nothing and opens no pull request.'),
          h('div', { class: 'small' }, 'Pushes and pull requests use your credentials and cannot be taken back from here.'))),
        section('repo', 'Target repositories', acc.targets, (x) => h('li', null,
          h('div', { class: 'cl-main' }, crumbs(x.path)),
          h('div', { class: 'cl-detail' }, templateSpans(x.t.url)),
          h('div', { class: 'cl-detail' }, icon('branch', 'icon-sm'), ` ${x.t.branch || '—'} → `, templateSpans(x.t.featureBranch || '—'))),
        'No module in this tree declares a target; runs write into a directory.'),
        section('shell', 'Shell commands that will run', acc.shells, (x) => h('li', null,
          h('div', { class: 'cl-main' }, crumbs(x.path), h('strong', { class: 'mono' }, x.op.name)),
          h('div', { class: 'cl-detail' }, '$ ', templateSpans(x.op.detail || '')),
          x.op.if ? h('div', { class: 'cl-detail status-warn' }, `only if: ${x.op.if}`) : null),
        'None.'),
        section('pr', 'Operations that push or open pull requests', [...acc.pushes, ...acc.prs], (x) => h('li', null,
          h('div', { class: 'cl-main' }, crumbs(x.path), h('strong', { class: 'mono' }, x.op.name), opKind(x.op.kind)),
          x.op.detail ? h('div', { class: 'cl-detail' }, templateSpans(x.op.detail)) : null,
          x.op.if ? h('div', { class: 'cl-detail status-warn' }, `only if: ${x.op.if}`) : null),
        'None.'),
        acc.llms.length ? section('sparkle', 'LLM operations', acc.llms, (x) => h('li', null,
          h('div', { class: 'cl-main' }, crumbs(x.path), h('strong', { class: 'mono' }, x.op.name)),
          h('div', { class: 'cl-detail' }, x.op.detail || '')), '') : null,
        acc.unknown.length ? h('div', { class: 'banner banner-warn' }, icon('alert'), h('div', { class: 'banner-body' },
          h('div', { class: 'banner-title' }, `${plural(acc.unknown.length, 'module')} could not be described and may do more`),
          acc.unknown.map((x) => crumbs(x.path)))) : null,
        h('p', { class: 'small muted' }, lp
          ? ['Last preview ', relTime(lp.at), JSON.stringify(lp.params) === JSON.stringify(req.params) ? ' with these params. ' : ' with different params. ',
            h('a', { href: href(`/jobs/${encodeURIComponent(lp.id)}/changes`) }, 'Review its changes')]
          : 'No preview of this module in this session. Quick preview shows the file changes and executes nothing.'),
      );
      confirmBtn.disabled = acc.missing.length > 0;
      replace(confirmBtn, icon('play'), pushing ? 'Execute and push' : 'Execute');
    }).catch((err) => {
      replace(body, h('div', { class: 'banner banner-err' }, icon('alert'), h('div', { class: 'banner-body' },
        h('div', { class: 'banner-title' }, 'Could not describe what this run will do'),
        h('div', null, err.message),
        h('div', { class: 'small' }, 'Without that description the run is not offered. Fix the problem and try again.'))));
    });
  });
}
