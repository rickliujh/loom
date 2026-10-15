// Boot: exchange the URL token for a session cookie, build the shell, start
// the router.

import { h, icon, replace } from './dom.js';
import * as api from './api.js';
import { on, state, getTheme, setTheme } from './store.js';
import { route, start, onRoute, href } from './router.js';
import { toast } from './components/toast.js';
import { renderCatalogue } from './views/catalogue.js';
import { renderModule } from './views/module.js';
import { renderJob } from './views/job.js';
import { renderHistory } from './views/history.js';
import { renderGenerate } from './views/generate.js';
import { renderBulk } from './views/bulk.js';
import { renderHelp } from './views/help.js';

const app = document.getElementById('app');

// ---- Errors: never silent. A toast for the user, and a marker in the DOM so
// an automated smoke test can see a script error.
function reportError(msg) {
  let box = document.getElementById('js-errors');
  if (!box) {
    box = h('ul', { id: 'js-errors', class: 'sr-only', 'data-js-errors': '' });
    document.body.appendChild(box);
  }
  box.appendChild(h('li', null, msg));
  toast(`Something went wrong in the page: ${msg}`, { kind: 'error' });
}
window.addEventListener('error', (e) => reportError(e.message || String(e.error)));
window.addEventListener('unhandledrejection', (e) => {
  const r = e.reason;
  if (r && r.name === 'AbortError') return;
  if (r instanceof api.ApiError) {
    if (r.status !== 401) toast(r.message, { kind: 'error' });
    return;
  }
  reportError(r && r.message ? r.message : String(r));
});

// ---- Session gate

function renderGate(reason) {
  const main = h('main', { id: 'main', class: 'gate', tabindex: '-1' },
    h('div', { class: 'card gate-card' },
      h('div', { class: 'row' }, h('span', { class: 'brand-mark' }, icon('root')), h('span', { class: 'eyebrow' }, 'loom serve')),
      h('h1', null, reason === 'bad-token' ? 'This link has expired' : 'Open the link loom serve printed'),
      h('p', { class: 'muted' }, reason === 'bad-token'
        ? 'The token in the address did not match. Each start of loom serve makes a new one, so a link from an earlier run no longer works.'
        : 'This page needs the one-time link from your terminal. It carries a token that proves the browser belongs to you; nothing else can use the server.'),
      h('ol', null,
        h('li', null, 'Find the terminal where ', h('code', null, 'loom serve'), ' is running (or start it).'),
        h('li', null, 'Open the URL it printed, which looks like:'),
      ),
      h('pre', { class: 'code code-wrap' }, `${window.location.origin}/#token=…`),
      h('p', { class: 'small muted' }, 'The token stays in the address fragment, which the browser never sends over the network. It is swapped for a cookie and removed from the address bar.'),
    ),
  );
  replace(app, main);
  // Opening the printed URL in this same tab only changes the fragment,
  // which does not reload the page: start over when a token arrives.
  if (!gateWatching) {
    gateWatching = true;
    window.addEventListener('hashchange', () => {
      if (/(?:^#|&)token=/.test(window.location.hash)) window.location.reload();
    });
  }
}

let gateWatching = false;

async function establishSession() {
  const m = /(?:^#|&)token=([^&]+)/.exec(window.location.hash);
  if (m) {
    const token = decodeURIComponent(m[1]);
    // Remove the token from the address bar and history before anything else.
    history.replaceState(null, '', window.location.pathname + '#/');
    try {
      await api.session.create(token);
    } catch (err) {
      if (err instanceof api.ApiError && err.status === 401) return 'bad-token';
      throw err;
    }
  }
  try {
    state.info = await api.info({ quiet401: true });
    return 'ok';
  } catch (err) {
    if (err instanceof api.ApiError && err.status === 401) return m ? 'bad-token' : 'no-session';
    throw err;
  }
}

// ---- Shell

const NAV = [
  { path: '/', label: 'Modules', icon: 'module', match: (p) => p === '/' || p.startsWith('/m/') },
  { path: '/jobs', label: 'Jobs', icon: 'history', match: (p) => p.startsWith('/jobs') },
  { path: '/generate', label: 'Generate', icon: 'wand', match: (p) => p.startsWith('/generate') },
  { path: '/bulk', label: 'Bulk', icon: 'layers', match: (p) => p.startsWith('/bulk') },
  { path: '/help', label: 'Help', icon: 'book', match: (p) => p.startsWith('/help') },
];

const THEMES = ['system', 'light', 'dark'];
const THEME_ICON = { system: 'auto', light: 'sun', dark: 'moon' };

function themeButton() {
  const btn = h('button', { class: 'btn btn-ghost btn-icon', type: 'button' });
  const paint = () => {
    const t = getTheme();
    replace(btn, icon(THEME_ICON[t]));
    const next = THEMES[(THEMES.indexOf(t) + 1) % THEMES.length];
    btn.setAttribute('aria-label', `Theme: ${t}. Switch to ${next}.`);
    btn.title = `Theme: ${t} (click for ${next})`;
  };
  btn.addEventListener('click', () => {
    const t = getTheme();
    setTheme(THEMES[(THEMES.indexOf(t) + 1) % THEMES.length]);
    paint();
  });
  paint();
  return btn;
}

function queueIndicator() {
  const el = h('a', { class: 'queue-indicator', href: href('/jobs'), hidden: true });
  let timer = 0;
  const poll = async () => {
    clearTimeout(timer);
    if (document.visibilityState === 'visible') {
      try {
        const list = api.jobList(await api.jobs.list({ limit: 50 }));
        const running = list.filter((j) => j.state === 'running' || j.state === 'cancelling').length;
        const queued = list.filter((j) => j.state === 'queued').length;
        if (running || queued) {
          el.hidden = false;
          el.classList.toggle('active', running > 0);
          const label = [running && `${running} running`, queued && `${queued} queued`].filter(Boolean).join(' · ');
          replace(el, icon(running ? 'spinner' : 'clock', running ? 'icon-sm spin' : 'icon-sm'), h('span', { class: 'label' }, label));
          el.setAttribute('aria-label', `Jobs: ${label}`);
        } else {
          el.hidden = true;
        }
      } catch { /* the page shows its own errors */ }
    }
    timer = setTimeout(poll, 5000);
  };
  on('jobs-changed', () => poll());
  document.addEventListener('visibilitychange', () => { if (document.visibilityState === 'visible') poll(); });
  poll();
  return el;
}

function buildShell() {
  const navLinks = NAV.map((n) => h('a', { href: href(n.path), 'data-path': n.path }, icon(n.icon), h('span', null, n.label)));
  const main = h('main', { id: 'main', tabindex: '-1' });
  const info = state.info || {};
  const header = h('header', { class: 'app-header' },
    h('button', { class: 'skip-link', type: 'button', onClick: () => main.focus() }, 'Skip to content'),
    h('a', { class: 'brand', href: href('/'), 'aria-label': 'loom — modules' }, h('span', { class: 'brand-mark' }, icon('root')), 'loom'),
    h('nav', { class: 'app-nav', 'aria-label': 'Main' }, navLinks),
    h('div', { class: 'header-tools' }, queueIndicator(), themeButton()),
  );
  const footer = h('footer', { class: 'app-footer' },
    h('span', null, `loom ${info.version || ''}`.trim(), info.roots && info.roots.length ? ` · roots: ${info.roots.join(', ')}` : ''),
    h('span', null, h('button', {
      class: 'link-btn', type: 'button',
      onClick: async () => {
        try { await api.session.destroy(); } catch { /* ignore */ }
        renderGate('no-session');
      },
    }, icon('logout', 'icon-sm'), 'End session')),
  );
  replace(app, header, main, footer);
  onRoute((path) => {
    navLinks.forEach((a, i) => {
      if (NAV[i].match(path)) a.setAttribute('aria-current', 'page');
      else a.removeAttribute('aria-current');
    });
  });
  return main;
}

function notFound(outlet) {
  outlet.appendChild(h('div', { class: 'page page-narrow' },
    h('div', { class: 'empty' }, icon('help', 'icon-xl'), h('h3', null, 'Nothing here'),
      h('p', null, 'This address does not match a page of loom serve.'),
      h('div', { class: 'actions' }, h('a', { class: 'btn', href: href('/') }, 'Go to modules')))));
}

async function boot() {
  let status;
  try {
    status = await establishSession();
  } catch (err) {
    replace(app, h('main', { id: 'main', class: 'gate' }, h('div', { class: 'card gate-card' },
      h('h1', null, 'Cannot reach loom serve'),
      h('p', { class: 'muted' }, err.message || String(err)),
      h('button', { class: 'btn btn-primary', type: 'button', onClick: () => window.location.reload() }, icon('refresh'), 'Try again'))));
    return;
  }
  if (status !== 'ok') { renderGate(status); return; }

  on('unauthorized', () => renderGate('no-session'));

  const outlet = buildShell();
  route('/', renderCatalogue, 'catalogue');
  route('/m/:source/:tab?', renderModule, (p) => `module:${p.source}`);
  route('/jobs', renderHistory, 'history');
  route('/jobs/:id/:tab?', renderJob, (p) => `job:${p.id}`);
  route('/generate', renderGenerate, 'generate');
  route('/bulk', renderBulk, 'bulk');
  route('/help/:topic?', renderHelp, 'help');
  route('/404', notFound, '404');
  start(outlet);
}

boot();
