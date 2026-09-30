// The one place that talks to /api/v1 (docs/reference/serve-api.md).
//
// Every request is same-origin, carries the session cookie, and sends
// Content-Type: application/json on anything that is not a GET, because the
// server refuses a mutating request without it (CSRF defence).

import { emit } from './store.js';

const BASE = '/api/v1';

export class ApiError extends Error {
  constructor(status, code, message, details) {
    super(message || code || `HTTP ${status}`);
    this.name = 'ApiError';
    this.status = status;
    this.code = code || 'internal';
    this.details = details || null;
  }
}

function qs(query) {
  if (!query) return '';
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(query)) {
    if (v === undefined || v === null || v === '' || v === false) continue;
    p.set(k, v === true ? '1' : String(v));
  }
  const s = p.toString();
  return s ? `?${s}` : '';
}

export function apiUrl(path, query) {
  return BASE + path + qs(query);
}

/**
 * request(method, path, {body, query, signal, text}) resolves with the parsed
 * body (null for 204) or rejects with ApiError. A 401 also tells the app the
 * session is gone, so it can show the "open the printed URL" screen.
 */
export async function request(method, path, opts = {}) {
  const { body, query, signal, quiet401 } = opts;
  const headers = { Accept: 'application/json' };
  const init = { method, headers, credentials: 'same-origin', signal, cache: 'no-store' };
  if (method !== 'GET' && method !== 'HEAD') {
    headers['Content-Type'] = 'application/json';
    if (body !== undefined) init.body = JSON.stringify(body);
    else if (method === 'POST' || method === 'PUT') init.body = '{}';
  }
  let res;
  try {
    res = await fetch(apiUrl(path, query), init);
  } catch (err) {
    if (err && err.name === 'AbortError') throw err;
    throw new ApiError(0, 'network', 'Cannot reach the loom server. Is `loom serve` still running?');
  }
  if (res.status === 204) return null;
  const ctype = res.headers.get('Content-Type') || '';
  let data = null;
  if (ctype.includes('application/json')) {
    try { data = await res.json(); } catch { data = null; }
  } else {
    data = await res.text();
  }
  if (!res.ok) {
    const e = data && typeof data === 'object' && data.error ? data.error : {};
    const err = new ApiError(res.status, e.code, e.message || (typeof data === 'string' && data.trim()) || res.statusText, e.details);
    err.body = data;
    if (res.status === 401 && !quiet401) emit('unauthorized', err);
    throw err;
  }
  return data;
}

export const get = (path, query, opts) => request('GET', path, { ...opts, query });
export const post = (path, body, opts) => request('POST', path, { ...opts, body });
export const put = (path, body, query, opts) => request('PUT', path, { ...opts, body, query });
export const del = (path, query, opts) => request('DELETE', path, { ...opts, query });

/** Formats an ApiError's details (422 load errors and the like) as lines. */
export function errorLines(err) {
  const lines = [];
  const d = err && err.details;
  if (!d) return lines;
  if (typeof d === 'string') lines.push(d);
  else if (Array.isArray(d)) d.forEach((x) => lines.push(typeof x === 'string' ? x : JSON.stringify(x)));
  else if (typeof d === 'object') {
    for (const [k, v] of Object.entries(d)) {
      if (Array.isArray(v)) v.forEach((x) => lines.push(typeof x === 'string' ? x : JSON.stringify(x)));
      else if (k !== 'field') lines.push(`${k}: ${typeof v === 'string' ? v : JSON.stringify(v)}`);
    }
  }
  return lines;
}

// ---- Endpoints (one function each, so the load-bearing surface is visible) ----

export const session = {
  create: (token) => request('POST', '/session', { body: { token }, quiet401: true }),
  destroy: () => request('DELETE', '/session', { quiet401: true }),
};

export const info = (opts) => get('/info', null, opts);

export const modules = {
  list: (refresh) => get('/modules', { refresh: refresh ? 1 : '' }),
  create: (dir, name) => post('/modules', { dir, name }),
};

export const inspect = (body, opts) => post('/inspect', body, opts);
export const validate = (body, opts) => post('/validate', body, opts);
export const paramsCheck = (body, opts) => post('/params/check', body, opts);
/**
 * yamlParse(text) resolves {value} or {error: {line, col, message}}. The
 * contract shows the error form as a result; a server that answers it with a
 * 4xx status is read the same way, so a line/column is never lost.
 */
export async function yamlParse(text, opts) {
  try {
    const r = await post('/yaml/parse', { text }, opts);
    return r || { value: null };
  } catch (err) {
    const e = err && err.body && err.body.error;
    if (e && (e.line !== undefined || e.col !== undefined)) return { error: e };
    if (err instanceof ApiError && (err.status === 400 || err.status === 422)) {
      return { error: { line: 0, col: 0, message: err.message } };
    }
    throw err;
  }
}
export const yamlFormat = (value, opts) => post('/yaml/format', { value }, opts);
export const cli = (jobRequest) => post('/cli', jobRequest);

export const files = {
  get: (source, path) => get('/files', { source, path: path || '' }),
  put: (source, path, content, ifMatch) => put('/files', ifMatch ? { content, ifMatch } : { content }, { source, path }),
  create: (source, path, kind) => post('/files', { source, path, kind }),
  remove: (source, path) => del('/files', { source, path }),
  move: (source, from, to) => post('/files/move', { source, from, to }),
};

export const jobs = {
  create: (req) => post('/jobs', req),
  list: (query) => get('/jobs', query),
  get: (id) => get(`/jobs/${encodeURIComponent(id)}`),
  diff: (id) => get(`/jobs/${encodeURIComponent(id)}/diff`),
  cancel: (id) => post(`/jobs/${encodeURIComponent(id)}/cancel`),
  rerun: (id, overrides) => post(`/jobs/${encodeURIComponent(id)}/rerun`, overrides ? { overrides } : {}),
  remove: (id) => del(`/jobs/${encodeURIComponent(id)}`),
  logUrl: (id) => apiUrl(`/jobs/${encodeURIComponent(id)}/log.txt`),
};

/** The job list endpoint returns an array; accept a {jobs: []} wrapper too. */
export function jobList(data) {
  if (Array.isArray(data)) return data;
  if (data && Array.isArray(data.jobs)) return data.jobs;
  return [];
}

export const presets = {
  list: (source) => get('/presets', { source }),
  get: (source, name) => get(`/presets/${encodeURIComponent(name)}`, { source }),
  save: (source, name, yaml) => put(`/presets/${encodeURIComponent(name)}`, { yaml }, { source }),
  remove: (source, name) => del(`/presets/${encodeURIComponent(name)}`, { source }),
};

export const docs = {
  index: () => get('/docs'),
  // A namespaced topic ("reference/cli-run") travels as one escaped segment,
  // which a {topic} and a {topic...} route pattern both accept.
  topic: (topic) => get(`/docs/${encodeURIComponent(topic)}`),
};

// ---- Server-Sent Events ----

const EVENT_TYPES = [
  'job.state', 'module.start', 'module.end', 'module.skip',
  'op.start', 'op.end', 'op.skip', 'log', 'diff.file', 'pr.created',
];

/**
 * streamJob(id, {after, onEvent, onEnd, onStatus}) follows a job's event
 * stream. Every event is delivered once and in order: the browser resumes
 * with Last-Event-ID on its own reconnects, a hard failure is retried with
 * ?after=<last seq>, and anything at or below the last seen seq is dropped.
 * Returns a stop() function.
 */
export function streamJob(id, { after = 0, onEvent, onEnd, onStatus } = {}) {
  let last = after;
  let es = null;
  let stopped = false;
  let retry = 0;
  let timer = 0;

  const status = (s) => { if (onStatus) onStatus(s); };

  const handle = (ev) => {
    let data;
    try { data = JSON.parse(ev.data); } catch { return; }
    const seq = Number(data.seq ?? ev.lastEventId ?? 0);
    if (seq && seq <= last) return;
    if (seq) last = seq;
    retry = 0;
    if (!data.type) data.type = ev.type === 'message' ? 'log' : ev.type;
    onEvent(data);
  };

  const open = () => {
    if (stopped) return;
    status(last > after || retry ? 'reconnecting' : 'connecting');
    es = new EventSource(apiUrl(`/jobs/${encodeURIComponent(id)}/events`, { after: last || '' }));
    es.onopen = () => status('live');
    for (const t of EVENT_TYPES) es.addEventListener(t, handle);
    es.addEventListener('message', handle);
    es.addEventListener('end', () => {
      stopped = true;
      es.close();
      status('ended');
      if (onEnd) onEnd();
    });
    es.onerror = () => {
      if (stopped) return;
      if (es.readyState === EventSource.CLOSED) {
        // The browser gave up (a non-200 answer, or the stream closed without
        // an `end`). Retry ourselves, resuming after the last seq we hold.
        status('reconnecting');
        retry += 1;
        const delay = Math.min(15000, 500 * 2 ** Math.min(retry, 5));
        timer = setTimeout(open, delay);
      } else {
        status('reconnecting');
      }
    };
  };

  open();
  return () => {
    stopped = true;
    clearTimeout(timer);
    if (es) es.close();
  };
}
