// The CLI's visual vocabulary, in the browser: the inverted module chip, the
// reserved "≡ root ≡" chip for the orchestrator, the "▸ parent › child"
// hand-off, and job state pills.

import { h, icon } from '../dom.js';
import { STATE_LABEL } from '../lib/format.js';

/** moduleChip(name, {root, level, soft}) */
export function moduleChip(name, { root = false, level = '', soft = false, title } = {}) {
  const lv = String(level || '').toUpperCase();
  const cls = ['chip',
    root && 'chip-root',
    soft && 'chip-soft',
    lv === 'ERROR' && 'chip-err',
    lv === 'WARN' && 'chip-warn'];
  return h('span', { class: cls, title: title || name }, root ? `≡ ${name} ≡` : name);
}

/** crumbs(path, {rootChip}) → "root › child › grandchild" with the root as a chip. */
export function crumbs(path, { rootChip = true, chips = false } = {}) {
  const p = path || [];
  const nodes = [];
  p.forEach((seg, i) => {
    if (i > 0) nodes.push(h('span', { class: 'sep', 'aria-hidden': 'true' }, '›'));
    if (i === 0 && rootChip && p.length > 1) nodes.push(moduleChip(seg, { root: true, soft: true }));
    else if (chips) nodes.push(moduleChip(seg, { soft: true }));
    else nodes.push(h('span', { class: 'crumb' }, seg));
  });
  return h('span', { class: 'crumbs', 'aria-label': p.join(' › ') }, nodes);
}

/** handoff(parent, child) → "▸ parent › child" */
export function handoff(parent, child) {
  return h('span', { class: 'handoff' },
    h('span', { class: 'tri', 'aria-hidden': 'true' }, '▸ '),
    parent ? h('span', { class: 'parent' }, `${parent} › `) : null,
    h('span', null, child));
}

const STATE_ICON = {
  queued: 'clock', running: 'spinner', cancelling: 'spinner', succeeded: 'check-circle',
  failed: 'x-circle', cancelled: 'skip', interrupted: 'alert',
};

export function stateIcon(state, cls = '') {
  const name = STATE_ICON[state] || 'pending';
  const spin = state === 'running' || state === 'cancelling';
  return icon(name, [cls, spin && 'spin'].filter(Boolean).join(' '));
}

export function statePill(state, { large = false } = {}) {
  return h('span', { class: ['pill', `st-${state}`, large && 'pill-lg'] },
    stateIcon(state), STATE_LABEL[state] || state);
}

/** Icon + colour class for the job tree and inspect states. */
export function nodeStatusIcon(status) {
  switch (status) {
    case 'running': return h('span', { class: 'status-run', title: 'Running' }, icon('spinner', 'spin'));
    case 'ok': return h('span', { class: 'status-ok', title: 'Done' }, icon('check-circle'));
    case 'failed': return h('span', { class: 'status-err', title: 'Failed' }, icon('x-circle'));
    case 'skipped': return h('span', { class: 'status-muted', title: 'Skipped' }, icon('skip'));
    case 'cancelled': return h('span', { class: 'status-muted', title: 'Cancelled' }, icon('stop'));
    default: return h('span', { class: 'status-muted', title: 'Pending' }, icon('pending'));
  }
}

const KIND_ICON = { newFiles: 'files', patch: 'patch', shell: 'shell', llm: 'sparkle', commitPush: 'commit', pr: 'pr' };

export function opKind(kind) {
  return h('span', { class: ['kind', `kind-${kind}`] }, icon(KIND_ICON[kind] || 'dot', 'icon-sm'), kind || 'op');
}

export function typeBadge(type) {
  if (!type || type === 'string') return null;
  return h('span', { class: 'badge badge-accent badge-mono' }, type);
}
