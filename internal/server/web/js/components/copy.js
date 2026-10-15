// Clipboard helpers. 127.0.0.1 is a secure context, so the async clipboard
// API is normally present; the textarea fallback covers the rest.

import { h, icon, replace } from '../dom.js';
import { toast } from './toast.js';

export async function copyText(text) {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch { /* fall through */ }
  const ta = h('textarea', { class: 'sr-only', readonly: true });
  ta.value = text;
  document.body.appendChild(ta);
  ta.select();
  let ok = false;
  try { ok = document.execCommand('copy'); } catch { ok = false; }
  ta.remove();
  return ok;
}

/** A small button that copies text() and confirms in place. */
export function copyButton(getText, { label = 'Copy', iconOnly = true, cls = 'btn btn-ghost btn-sm btn-icon', what = 'Copied' } = {}) {
  const btn = h('button', {
    class: iconOnly ? cls : cls.replace('btn-icon', ''),
    type: 'button',
    title: label,
    'aria-label': label,
    onClick: async (e) => {
      e.stopPropagation();
      const t = typeof getText === 'function' ? getText() : getText;
      const ok = await copyText(t);
      if (ok) {
        replace(btn, icon('check'), iconOnly ? null : 'Copied');
        setTimeout(() => replace(btn, icon('copy'), iconOnly ? null : label), 1400);
        toast(what, { kind: 'ok', timeout: 1600 });
      } else {
        toast('Copy failed — select the text and copy it by hand.', { kind: 'error' });
      }
    },
  }, icon('copy'), iconOnly ? null : label);
  return btn;
}
