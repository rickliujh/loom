// Hash router. Routes are "#/path?query". A view is a function
// (outlet, params, query) → controller, where the controller may be a cleanup
// function or an object {update(params, query) → bool, destroy(), canLeave()}.
// When the next route resolves to the same view key and update() returns true,
// the view stays mounted (tab switches inside a module page, say).

import { clear } from './dom.js';

const routes = [];
let current = null;
let outlet = null;
let ignoreNext = false;
let lastHash = '';

export function route(pattern, view, key) {
  // pattern like "/m/:source/:tab?"
  const names = [];
  const parts = pattern.split('/').filter(Boolean).map((seg) => {
    if (seg.startsWith(':') && seg.endsWith('?')) { names.push(seg.slice(1, -1)); return '(?:/([^/]+))?'; }
    if (seg.startsWith(':')) { names.push(seg.slice(1)); return '/([^/]+)'; }
    return '/' + seg.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  });
  const re = new RegExp('^' + parts.join('') + '/?$');
  routes.push({ re, names, view, key: key || pattern });
}

export function parseHash(hash = window.location.hash) {
  let h = hash.replace(/^#/, '');
  if (!h.startsWith('/')) h = '/' + h;
  const qi = h.indexOf('?');
  const path = qi >= 0 ? h.slice(0, qi) : h;
  const query = Object.fromEntries(new URLSearchParams(qi >= 0 ? h.slice(qi + 1) : ''));
  return { path, query };
}

function match(path) {
  for (const r of routes) {
    const m = r.re.exec(path);
    if (!m) continue;
    const params = {};
    r.names.forEach((n, i) => {
      if (m[i + 1] !== undefined) {
        try { params[n] = decodeURIComponent(m[i + 1]); } catch { params[n] = m[i + 1]; }
      }
    });
    return { r, params };
  }
  return null;
}

export function href(path, query) {
  const q = query ? new URLSearchParams(Object.entries(query).filter(([, v]) => v !== undefined && v !== null && v !== '')).toString() : '';
  return '#' + path + (q ? `?${q}` : '');
}

export function navigate(path, query, { replace = false } = {}) {
  const target = href(path, query);
  if (replace) {
    history.replaceState(null, '', target);
    render();
  } else if (window.location.hash === target) {
    render();
  } else {
    window.location.hash = target;
  }
}

/** Replaces the query of the current route without re-rendering. */
export function setQuiet(path, query) {
  const target = href(path, query);
  lastHash = target;
  history.replaceState(null, '', target);
}

function controllerOf(ret) {
  if (!ret) return {};
  if (typeof ret === 'function') return { destroy: ret };
  return ret;
}

export function render() {
  const { path, query } = parseHash();
  const hash = window.location.hash;
  if (current && current.ctl.canLeave && hash !== lastHash && !current.ctl.canLeave()) {
    // Put the old hash back without triggering another render.
    ignoreNext = true;
    history.replaceState(null, '', lastHash || '#/');
    return;
  }
  lastHash = hash;
  const m = match(path) || match('/404');
  if (!m) return;
  const key = typeof m.r.key === 'function' ? m.r.key(m.params) : m.r.key;
  if (current && current.key === key && current.ctl.update && current.ctl.update(m.params, query) === true) {
    emitRoute(path);
    return;
  }
  if (current && current.ctl.destroy) {
    try { current.ctl.destroy(); } catch (err) { console.error(err); }
  }
  clear(outlet);
  window.scrollTo(0, 0);
  const ctl = controllerOf(m.r.view(outlet, m.params, query));
  current = { key, ctl };
  emitRoute(path);
}

const routeListeners = new Set();
export function onRoute(fn) { routeListeners.add(fn); }
function emitRoute(path) { routeListeners.forEach((fn) => fn(path)); }

export function start(el) {
  outlet = el;
  window.addEventListener('hashchange', () => {
    if (ignoreNext) { ignoreNext = false; return; }
    render();
  });
  window.addEventListener('beforeunload', (e) => {
    if (current && current.ctl.canLeave && !current.ctl.canLeave(true)) {
      e.preventDefault();
      e.returnValue = '';
    }
  });
  render();
}
