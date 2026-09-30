// A dropdown menu attached to a button: arrow keys move, Escape closes and
// returns focus, a click outside dismisses.

import { h, icon, uid } from '../dom.js';

/**
 * menuButton(button, items, {align, up}) wraps button in an anchor and wires
 * it to open a menu. items: [{icon, title, desc, danger, onSelect, disabled}]
 * or 'sep'. Returns the anchor element to insert in place of the button.
 */
export function menuButton(button, items, { align = 'right', up = false } = {}) {
  const id = uid('menu');
  button.setAttribute('aria-haspopup', 'menu');
  button.setAttribute('aria-expanded', 'false');
  button.setAttribute('aria-controls', id);
  const anchor = h('div', { class: 'menu-anchor' }, button);
  let menu = null;

  const close = (refocus = true) => {
    if (!menu) return;
    menu.remove();
    menu = null;
    button.setAttribute('aria-expanded', 'false');
    document.removeEventListener('mousedown', onOutside, true);
    if (refocus) button.focus();
  };
  const onOutside = (e) => { if (!anchor.contains(e.target)) close(false); };

  const open = () => {
    const list = typeof items === 'function' ? items() : items;
    menu = h('div', { class: ['menu', align === 'left' && 'menu-left', up && 'menu-up'], role: 'menu', id },
      list.map((it) => it === 'sep'
        ? h('div', { class: 'menu-sep', role: 'separator' })
        : h('button', {
          class: ['menu-item', it.danger && 'danger'], type: 'button', role: 'menuitem', disabled: !!it.disabled,
          onClick: () => { close(); it.onSelect(); },
        },
        it.icon ? icon(it.icon) : null,
        h('span', { class: 'stack-sm grow' },
          h('span', { class: 'mi-title' }, it.title),
          it.desc ? h('span', { class: 'mi-desc' }, it.desc) : null))));
    menu.addEventListener('keydown', (e) => {
      const btns = [...menu.querySelectorAll('.menu-item:not(:disabled)')];
      const i = btns.indexOf(document.activeElement);
      if (e.key === 'ArrowDown') { e.preventDefault(); btns[(i + 1) % btns.length].focus(); }
      else if (e.key === 'ArrowUp') { e.preventDefault(); btns[(i - 1 + btns.length) % btns.length].focus(); }
      else if (e.key === 'Home') { e.preventDefault(); btns[0].focus(); }
      else if (e.key === 'End') { e.preventDefault(); btns[btns.length - 1].focus(); }
      else if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); close(); }
      else if (e.key === 'Tab') { close(false); }
    });
    anchor.appendChild(menu);
    button.setAttribute('aria-expanded', 'true');
    document.addEventListener('mousedown', onOutside, true);
    const first = menu.querySelector('.menu-item:not(:disabled)');
    if (first) first.focus();
  };

  button.addEventListener('click', () => (menu ? close() : open()));
  button.addEventListener('keydown', (e) => {
    if ((e.key === 'ArrowDown' || e.key === 'ArrowUp') && !menu) { e.preventDefault(); open(); }
  });
  return anchor;
}
