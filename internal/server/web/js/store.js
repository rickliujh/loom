// A tiny pub/sub plus the few pieces of state that outlive a single view.

const listeners = new Map();

export function on(event, fn) {
  if (!listeners.has(event)) listeners.set(event, new Set());
  listeners.get(event).add(fn);
  return () => listeners.get(event).delete(fn);
}

export function emit(event, payload) {
  const set = listeners.get(event);
  if (!set) return;
  for (const fn of [...set]) {
    try { fn(payload); } catch (err) { console.error(err); }
  }
}

// ---- Preferences: per-browser conveniences only. Storage may be blocked
// (private windows, disabled site data), so every access is guarded and the UI
// works the same without it.

const PREFIX = 'loom.';

export const prefs = {
  get(key, fallback) {
    try {
      const raw = window.localStorage.getItem(PREFIX + key);
      return raw === null ? fallback : JSON.parse(raw);
    } catch {
      return fallback;
    }
  },
  set(key, value) {
    try {
      window.localStorage.setItem(PREFIX + key, JSON.stringify(value));
    } catch { /* storage unavailable: keep the in-memory state only */ }
  },
  remove(key) {
    try { window.localStorage.removeItem(PREFIX + key); } catch { /* ignore */ }
  },
};

// Theme is stored as a bare string, because js/theme.js reads it before any
// module loads.
export function getTheme() {
  try {
    const t = window.localStorage.getItem('loom.theme');
    return t === 'light' || t === 'dark' ? t : 'system';
  } catch {
    return 'system';
  }
}

export function setTheme(t) {
  const root = document.documentElement;
  if (t === 'light' || t === 'dark') root.setAttribute('data-theme', t);
  else root.removeAttribute('data-theme');
  try {
    if (t === 'light' || t === 'dark') window.localStorage.setItem('loom.theme', t);
    else window.localStorage.removeItem('loom.theme');
  } catch { /* ignore */ }
}

// ---- Shared state ----

export const state = {
  info: null,
  modules: null,
  // Hand-off between views: "Re-run with edits" and wizard results pass data
  // to the module form without putting it in the URL.
  handoff: new Map(),
  // Last preview (diff job) per module source, for the Execute confirmation.
  lastPreview: new Map(),
};
