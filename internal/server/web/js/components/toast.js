// Transient notices, announced to assistive technology through a live region.

import { h, icon } from '../dom.js';

let region = null;

function ensureRegion() {
  if (region && document.body.contains(region)) return region;
  region = h('div', { class: 'toasts', role: 'status', 'aria-live': 'polite' });
  document.body.appendChild(region);
  return region;
}

export function toast(message, { kind = 'info', timeout = 4000, action } = {}) {
  const r = ensureRegion();
  const glyph = kind === 'error' ? 'alert' : kind === 'ok' ? 'check-circle' : 'info';
  const el = h('div', { class: ['toast', kind === 'error' && 'toast-err'] },
    icon(glyph),
    h('div', { class: 'toast-body' }, message,
      action ? h('div', { class: 'mt-1' }, h('button', { class: 'link-btn', type: 'button', onClick: () => { action.run(); dismiss(); } }, action.label)) : null),
    h('button', { class: 'toast-close', type: 'button', 'aria-label': 'Dismiss', onClick: () => dismiss() }, icon('x', 'icon-sm')),
  );
  if (kind === 'error') el.setAttribute('data-kind', 'error');
  r.appendChild(el);
  let t = 0;
  const dismiss = () => { clearTimeout(t); el.remove(); };
  if (timeout) t = setTimeout(dismiss, kind === 'error' ? Math.max(timeout, 8000) : timeout);
  return dismiss;
}

export function toastError(err, prefix) {
  const msg = err && err.message ? err.message : String(err);
  return toast(prefix ? `${prefix}: ${msg}` : msg, { kind: 'error' });
}
