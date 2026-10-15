// A YAML textarea for list and map params. The text is the value: it is sent
// as typed and never rebuilt from a parsed copy, so `1.10` stays `1.10`. The
// server parses it (debounced) only to report errors and the value's shape.

import { h, icon, debounce, replace } from '../dom.js';
import * as api from '../api.js';
import { describeValue, kindOf } from '../lib/yaml-view.js';

const INDENT = '  ';

/**
 * yamlField({id, kind, value, placeholder, describedBy, onInput, onStatus})
 * → {el, textarea, get(), set(text), status}. status is
 * {state: 'empty'|'pending'|'ok'|'error', message, value}.
 */
export function yamlField({ id, kind, value = '', placeholder = '', describedBy = '', onInput, onStatus, rows }) {
  const statusEl = h('div', { class: 'yaml-status', id: `${id}-status`, 'aria-live': 'polite' });
  const ta = h('textarea', {
    class: 'textarea mono', id, spellcheck: 'false', autocomplete: 'off', autocapitalize: 'off',
    rows: rows || Math.min(14, Math.max(4, value.split('\n').length + 1)),
    placeholder,
    'aria-describedby': [describedBy, `${id}-status`, `${id}-kbd`].filter(Boolean).join(' '),
  });
  ta.value = value;
  let escArmed = false;
  let seq = 0;
  const field = {
    el: null, textarea: ta,
    status: { state: value.trim() ? 'pending' : 'empty' },
    get: () => ta.value,
    set(text) { ta.value = text || ''; autoRows(); validateNow(); },
  };

  const setStatus = (s) => {
    field.status = s;
    ta.setAttribute('aria-invalid', s.state === 'error' ? 'true' : 'false');
    switch (s.state) {
      case 'ok':
        replace(statusEl, h('span', { class: 'field-ok' }, icon('check', 'icon-sm'), `valid ${kind} · ${describeValue(s.value)}`), kbdHint());
        break;
      case 'error':
        replace(statusEl, h('span', { class: 'field-error' }, icon('alert', 'icon-sm'), s.message), kbdHint());
        break;
      case 'pending':
        replace(statusEl, h('span', { class: 'muted' }, 'checking…'), kbdHint());
        break;
      default:
        replace(statusEl, h('span', { class: 'muted' }, placeholder ? 'empty — the default applies' : 'empty'), kbdHint());
    }
    if (onStatus) onStatus(s);
  };

  const kbdHint = () => h('span', { class: 'faint small', id: `${id}-kbd` }, 'Tab indents · Esc then Tab leaves');

  async function validateNow() {
    const text = ta.value;
    const my = ++seq;
    if (!text.trim()) { setStatus({ state: 'empty' }); return field.status; }
    setStatus({ state: 'pending' });
    try {
      const r = await api.yamlParse(text);
      if (my !== seq) return field.status;
      if (r.error) {
        const e = r.error;
        const where = e.line ? `Line ${e.line}${e.col ? `, column ${e.col}` : ''}: ` : '';
        setStatus({ state: 'error', message: `${where}${e.message}`, line: e.line, col: e.col });
      } else {
        const k = kindOf(r.value);
        if (k !== kind) {
          setStatus({ state: 'error', message: `This is YAML, but a ${k === 'scalar' ? 'single value' : k}, not a ${kind}.${kind === 'list' ? ' Start each item with “- ”.' : ' Write key: value lines.'}` });
        } else {
          setStatus({ state: 'ok', value: r.value });
        }
      }
    } catch (err) {
      if (my !== seq) return field.status;
      setStatus({ state: 'error', message: `Could not check the YAML: ${err.message}` });
    }
    return field.status;
  }
  const validate = debounce(validateNow, 350);
  field.validate = validateNow;

  function autoRows() {
    const n = ta.value.split('\n').length + 1;
    ta.rows = Math.min(24, Math.max(rows || 4, n));
  }

  ta.addEventListener('input', () => {
    autoRows();
    setStatus({ state: ta.value.trim() ? 'pending' : 'empty' });
    validate();
    if (onInput) onInput(ta.value);
  });

  ta.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') { escArmed = true; return; }
    if (e.key === 'Tab' && !escArmed && !e.ctrlKey && !e.metaKey && !e.altKey) {
      e.preventDefault();
      indentSelection(ta, e.shiftKey);
      ta.dispatchEvent(new Event('input'));
      return;
    }
    if (e.key === 'Enter' && !e.shiftKey && !e.ctrlKey && !e.metaKey) {
      // Keep the current indentation; step in after "key:" or a bare "-".
      const { selectionStart: s, value: v } = ta;
      const lineStart = v.lastIndexOf('\n', s - 1) + 1;
      const line = v.slice(lineStart, s);
      let ind = /^\s*/.exec(line)[0];
      const list = /^(\s*)- /.exec(line);
      if (/:\s*$/.test(line) || /^\s*-\s*$/.test(line)) ind += INDENT;
      else if (list && /^\s*- [^:]+:\s/.test(line)) ind = list[1] + INDENT;
      e.preventDefault();
      insertText(ta, '\n' + ind);
      ta.dispatchEvent(new Event('input'));
      return;
    }
    escArmed = false;
  });
  ta.addEventListener('blur', () => { escArmed = false; });

  field.el = h('div', { class: 'yaml-field stack-sm' }, ta, statusEl);
  setStatus(field.status);
  if (value.trim()) validateNow();
  return field;
}

function insertText(ta, text) {
  const { selectionStart: s, selectionEnd: e } = ta;
  ta.setRangeText(text, s, e, 'end');
}

export function indentSelection(ta, outdent) {
  const v = ta.value;
  const s = ta.selectionStart;
  const e = ta.selectionEnd;
  const lineStart = v.lastIndexOf('\n', s - 1) + 1;
  if (s === e && !outdent) { insertText(ta, INDENT); return; }
  const lineEnd = e > s && v[e - 1] === '\n' ? e - 1 : (v.indexOf('\n', e) === -1 ? v.length : v.indexOf('\n', e));
  const block = v.slice(lineStart, lineEnd);
  const lines = block.split('\n');
  let delta0 = 0;
  let total = 0;
  const out = lines.map((l, i) => {
    if (outdent) {
      const n = l.startsWith(INDENT) ? INDENT.length : (l.startsWith(' ') ? 1 : 0);
      if (i === 0) delta0 = -n;
      total -= n;
      return l.slice(n);
    }
    if (i === 0) delta0 = INDENT.length;
    total += INDENT.length;
    return INDENT + l;
  });
  ta.setRangeText(out.join('\n'), lineStart, lineEnd, 'preserve');
  ta.selectionStart = Math.max(lineStart, s + delta0);
  ta.selectionEnd = Math.max(ta.selectionStart, e + total);
}
