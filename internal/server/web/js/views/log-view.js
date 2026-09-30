// The job log, rendered like the CLI's pretty handler: module chip gutter,
// "≡ root ≡" for the orchestrator, "▸ parent › child" hand-offs, indentation
// by depth, key/value attrs, and multi-line attrs (shell output) folded.
//
// At most CAP lines are in the DOM; older ones are kept in memory and shown
// on request.

import { h, icon, highlight, debounce, replace } from '../dom.js';
import { prefs } from '../store.js';
import { moduleChip } from '../components/chip.js';
import { timeOfDay, plural } from '../lib/format.js';
import { inScope } from '../lib/job-model.js';

const CAP = 5000;
const LEVELS = ['DEBUG', 'INFO', 'WARN', 'ERROR'];
const MODES = [['dry-run:', 'mode-dry'], ['local-run:', 'mode-local']];

export function createLogView({ model, downloadUrl, onClearScope }) {
  const levels = new Set(prefs.get('log.levels', ['INFO', 'WARN', 'ERROR']));
  let search = '';
  let scope = null; // selected tree node
  let cap = CAP;
  let follow = true;
  let shown = []; // entries currently rendered, in order
  let pending = [];
  let raf = 0;
  let hiddenEarlier = 0;

  const scroller = h('div', { class: 'log-scroll', tabindex: '0', role: 'log', 'aria-label': 'Job log', 'aria-live': 'off' });
  const list = h('div', { class: 'log-lines' });
  const moreBox = h('div', { class: 'log-more', hidden: true });
  const emptyEl = h('div', { class: 'log-empty', hidden: true });
  const jumpBtn = h('button', { class: 'btn btn-sm', type: 'button', onClick: () => setFollow(true) }, icon('arrow-down', 'icon-sm'), 'Jump to latest');
  const jump = h('div', { class: 'jump-latest', hidden: true }, jumpBtn);
  scroller.append(moreBox, list, emptyEl, jump);

  const statusEl = h('span', { class: 'small muted', 'aria-live': 'polite' });
  const scopeNote = h('div', { class: 'log-filter-note', hidden: true });

  const levelSeg = h('div', { class: 'seg', role: 'group', 'aria-label': 'Levels shown' }, LEVELS.map((lv) => h('button', {
    type: 'button', 'aria-pressed': String(levels.has(lv)), title: `Show ${lv.toLowerCase()} lines`,
    onClick: (e) => {
      if (levels.has(lv)) levels.delete(lv); else levels.add(lv);
      e.currentTarget.setAttribute('aria-pressed', String(levels.has(lv)));
      prefs.set('log.levels', [...levels]);
      rerender();
    },
  }, lv === 'WARN' ? 'Warn' : lv[0] + lv.slice(1).toLowerCase())));

  const searchIn = h('input', {
    class: 'input', type: 'search', placeholder: 'Search the log', 'aria-label': 'Search the log',
    onInput: debounce(() => { search = searchIn.value.trim(); rerender(); }, 150),
  });
  const followBtn = h('button', { class: 'btn btn-sm', type: 'button', 'aria-pressed': 'true', title: 'Keep the newest line in view', onClick: () => setFollow(!follow) });
  const paintFollow = () => {
    followBtn.setAttribute('aria-pressed', String(follow));
    replace(followBtn, icon(follow ? 'arrow-down' : 'lock', 'icon-sm'), follow ? 'Following' : 'Scroll locked');
  };
  paintFollow();

  const tools = h('div', { class: 'log-tools' },
    levelSeg,
    h('div', { class: 'search' }, icon('search', 'icon-sm'), searchIn),
    followBtn,
    h('span', { class: 'spacer' }),
    statusEl,
    downloadUrl ? h('a', { class: 'btn btn-sm btn-ghost', href: downloadUrl, download: '' }, icon('download', 'icon-sm'), 'log.txt') : null);

  const el = h('div', { class: 'log-panel' }, tools, scopeNote, scroller);

  scroller.addEventListener('scroll', () => {
    const atBottom = scroller.scrollTop + scroller.clientHeight >= scroller.scrollHeight - 24;
    if (!atBottom && follow) { follow = false; paintFollow(); }
    if (atBottom && !follow) { follow = true; paintFollow(); }
    jump.hidden = follow;
  });

  function setFollow(v) {
    follow = v;
    paintFollow();
    jump.hidden = follow;
    if (follow) scroller.scrollTop = scroller.scrollHeight;
  }

  function matches(e) {
    if (!levels.has(e.level === 'WARNING' ? 'WARN' : e.level)) return false;
    if (!inScope(e, scope)) return false;
    if (search) {
      const s = search.toLowerCase();
      if (e.msg.toLowerCase().includes(s)) return true;
      if (e.path.join(' ').toLowerCase().includes(s)) return true;
      return e.attrs.some(([k, v]) => `${k} ${v}`.toLowerCase().includes(s));
    }
    return true;
  }

  function line(e) {
    const depth = Math.max(0, e.path.length - 1);
    const name = e.path[e.path.length - 1] || '';
    const lv = e.level === 'WARNING' ? 'WARN' : e.level;
    const cls = ['log-line', `l-${lv.toLowerCase()}`, e.section && 'section'];
    const body = h('div', { class: 'body' });
    body.style.setProperty('--depth', String(depth));
    const msgLine = h('div', { class: 'msgline' });

    if (e.dispatch && e.path.length > 1) {
      msgLine.append(h('span', { class: 'handoff' }, h('span', { class: 'tri' }, '▸ '), h('span', { class: 'parent' }, `${name} › `), h('strong', null, highlight(e.msg, search))));
    } else {
      if (name) msgLine.append(moduleChip(name, { root: e.root && e.path.length === 1, level: lv }));
      const prefix = lv === 'ERROR' ? 'error: ' : lv === 'WARN' ? 'warning: ' : lv === 'DEBUG' ? 'debug: ' : '';
      if (prefix) msgLine.append(h('span', { class: 'lvl' }, prefix));
      const mode = MODES.find(([p]) => e.msg.startsWith(p));
      const msg = h('span', { class: 'msg' });
      if (mode) msg.append(h('span', { class: mode[1] }, mode[0]), ...highlight(e.msg.slice(mode[0].length), search));
      else msg.append(...highlight(e.msg, search));
      msgLine.append(msg);
    }
    body.append(msgLine);

    const inline = e.attrs.filter(([, v]) => !String(v).includes('\n'));
    const blocks = e.attrs.filter(([, v]) => String(v).includes('\n'));
    if (inline.length) {
      body.append(h('div', { class: 'attrs' }, inline.map(([k, v]) => [
        h('span', { class: 'k' }, highlight(k, search)),
        h('span', { class: 'v' }, v === '' ? '""' : highlight(String(v), search))])));
    }
    for (const [k, v] of blocks) {
      const lines = String(v).replace(/\n+$/, '').split('\n');
      const open = !!search && String(v).toLowerCase().includes(search.toLowerCase());
      body.append(h('details', { open },
        h('summary', null, icon('chevron-right'), `${k} · ${plural(lines.length, 'line')}`),
        h('pre', null, highlight(lines.join('\n'), search))));
    }
    return h('div', { class: cls, 'data-seq': e.seq },
      h('span', { class: 't', title: e.time || '' }, timeOfDay(e.time)),
      body);
  }

  function paintMore() {
    moreBox.hidden = hiddenEarlier === 0;
    replace(moreBox, h('button', { class: 'btn btn-sm', type: 'button', onClick: () => { cap += CAP; rerender(true); } },
      icon('chevron-down', 'icon-sm'), `Show ${Math.min(CAP, hiddenEarlier)} earlier of ${hiddenEarlier} hidden`));
  }

  function paintEmpty() {
    const any = list.firstChild;
    emptyEl.hidden = !!any;
    if (!any) {
      const total = model.logs.length;
      emptyEl.textContent = total === 0
        ? (model.state === 'queued' ? 'Waiting for the job to start…' : 'No log lines yet.')
        : `${plural(total, 'line')} hidden by the filters. ${levels.has('DEBUG') ? '' : 'Debug lines are off by default.'}`;
    }
  }

  function paintStatus() {
    const total = model.logs.length;
    statusEl.textContent = `${shown.length + hiddenEarlier === total ? plural(total, 'line') : `${shown.length + hiddenEarlier} of ${plural(total, 'line')}`}`;
  }

  function rerender(keepScroll = false) {
    const all = model.logs.filter(matches);
    hiddenEarlier = Math.max(0, all.length - cap);
    shown = all.slice(hiddenEarlier);
    const prevHeight = scroller.scrollHeight;
    replace(list, ...shown.map(line));
    paintMore();
    paintEmpty();
    paintStatus();
    if (keepScroll) scroller.scrollTop += scroller.scrollHeight - prevHeight;
    else if (follow) scroller.scrollTop = scroller.scrollHeight;
  }

  function flush() {
    raf = 0;
    const add = pending.filter(matches);
    pending = [];
    if (!add.length) { paintStatus(); return; }
    const f = document.createDocumentFragment();
    for (const e of add) { f.appendChild(line(e)); shown.push(e); }
    list.appendChild(f);
    while (shown.length > cap) {
      shown.shift();
      list.removeChild(list.firstChild);
      hiddenEarlier++;
    }
    paintMore();
    paintEmpty();
    paintStatus();
    if (follow) scroller.scrollTop = scroller.scrollHeight;
  }

  return {
    el,
    append(entry) {
      pending.push(entry);
      if (!raf) raf = requestAnimationFrame(flush);
    },
    rerender,
    setScope(node, label) {
      scope = node;
      if (node) {
        scopeNote.hidden = false;
        replace(scopeNote, icon('tree', 'icon-sm'), h('span', null, 'Showing ', h('strong', null, label), ' only'),
          h('button', { class: 'link-btn', type: 'button', onClick: () => onClearScope && onClearScope() }, 'Show all'));
      } else {
        scopeNote.hidden = true;
      }
      rerender();
    },
    setStatus(s) {
      const text = { live: '● live', connecting: 'connecting…', reconnecting: 'reconnecting…', ended: '' }[s] ?? '';
      statusEl.dataset.stream = s;
      if (text) statusEl.title = `Stream: ${text}`;
    },
    destroy() { if (raf) cancelAnimationFrame(raf); },
  };
}
