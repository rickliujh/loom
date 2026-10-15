// Modal dialogs: focus is trapped inside, Escape closes, and focus returns to
// whatever opened the dialog.

import { h, icon, uid, replace } from '../dom.js';

const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

export function openModal({ title, body, footer, wide = false, onClose, initialFocus, dismissible = true }) {
  const opener = document.activeElement;
  const titleId = uid('mt');
  let closed = false;

  const close = (result) => {
    if (closed) return;
    closed = true;
    document.removeEventListener('keydown', onKey, true);
    scrim.remove();
    document.body.classList.remove('modal-open');
    if (opener && typeof opener.focus === 'function' && document.contains(opener)) opener.focus();
    if (onClose) onClose(result);
  };

  const dialog = h('div', { class: ['modal', wide && 'modal-wide'], role: 'dialog', 'aria-modal': 'true', 'aria-labelledby': titleId },
    h('div', { class: 'modal-head' },
      h('h2', { id: titleId }, title),
      dismissible && h('button', { class: 'btn btn-ghost btn-icon btn-sm', type: 'button', 'aria-label': 'Close', onClick: () => close(undefined) }, icon('x')),
    ),
    h('div', { class: 'modal-body' }, body),
    footer ? h('div', { class: 'modal-foot' }, footer) : null,
  );
  const scrim = h('div', {
    class: 'modal-scrim',
    onMousedown: (e) => { if (e.target === scrim && dismissible) close(undefined); },
  }, dialog);

  function onKey(e) {
    if (e.key === 'Escape' && dismissible) {
      e.preventDefault();
      e.stopPropagation();
      close(undefined);
      return;
    }
    if (e.key === 'Tab') {
      const items = [...dialog.querySelectorAll(FOCUSABLE)].filter((el) => el.offsetParent !== null);
      if (!items.length) return;
      const first = items[0];
      const last = items[items.length - 1];
      if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
      else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
      else if (!dialog.contains(document.activeElement)) { e.preventDefault(); first.focus(); }
    }
  }

  document.addEventListener('keydown', onKey, true);
  document.body.appendChild(scrim);
  document.body.classList.add('modal-open');
  const target = (typeof initialFocus === 'function' ? initialFocus() : initialFocus)
    || dialog.querySelector('.modal-body ' + FOCUSABLE)
    || dialog.querySelector(FOCUSABLE);
  if (target) target.focus();

  return { close, dialog };
}

/** confirm({title, message, confirmLabel, danger}) → Promise<boolean> */
export function confirmDialog({ title, message, confirmLabel = 'Confirm', danger = false, details }) {
  return new Promise((resolve) => {
    let cancelBtn;
    const m = openModal({
      title,
      body: h('div', { class: 'stack' },
        typeof message === 'string' ? h('p', null, message) : message,
        details || null,
      ),
      footer: [
        cancelBtn = h('button', { class: 'btn', type: 'button', onClick: () => m.close(false) }, 'Cancel'),
        h('button', { class: ['btn', danger ? 'btn-danger-solid' : 'btn-primary'], type: 'button', onClick: () => m.close(true) }, confirmLabel),
      ],
      onClose: (r) => resolve(r === true),
      initialFocus: () => cancelBtn,
    });
  });
}

/**
 * promptDialog({title, label, value, placeholder, help, validate, confirmLabel})
 * → Promise<string|null>. validate(value) returns an error message or "".
 */
export function promptDialog({ title, label, value = '', placeholder = '', help, validate, confirmLabel = 'Save', mono = false }) {
  return new Promise((resolve) => {
    const id = uid('pd');
    const errId = uid('pe');
    const err = h('div', { class: 'field-error', id: errId, 'aria-live': 'polite' });
    const input = h('input', { class: ['input', mono && 'mono'], id, value, placeholder, autocomplete: 'off', spellcheck: 'false', 'aria-describedby': errId });
    const submit = () => {
      const v = input.value.trim();
      const msg = validate ? validate(v) : (v ? '' : 'A value is required.');
      if (msg) {
        replace(err, icon('alert', 'icon-sm'), msg);
        input.setAttribute('aria-invalid', 'true');
        input.focus();
        return;
      }
      m.close(v);
    };
    const form = h('form', { class: 'field', onSubmit: (e) => { e.preventDefault(); submit(); } },
      h('label', { class: 'field-label', for: id }, label),
      input,
      help ? h('div', { class: 'field-help' }, help) : null,
      err,
    );
    const m = openModal({
      title,
      body: form,
      footer: [
        h('button', { class: 'btn', type: 'button', onClick: () => m.close(null) }, 'Cancel'),
        h('button', { class: 'btn btn-primary', type: 'button', onClick: submit }, confirmLabel),
      ],
      onClose: (r) => resolve(typeof r === 'string' ? r : null),
      initialFocus: () => input,
    });
    input.select();
  });
}
