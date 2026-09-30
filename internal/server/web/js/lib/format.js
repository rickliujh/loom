// Formatting helpers. Pure functions: no DOM, testable under node --test.

export function duration(ms) {
  if (ms === null || ms === undefined || Number.isNaN(ms)) return '';
  if (ms < 0) ms = 0;
  if (ms < 1000) return `${Math.round(ms)}ms`;
  const s = ms / 1000;
  if (s < 10) return `${s.toFixed(1)}s`;
  if (s < 60) return `${Math.round(s)}s`;
  const m = Math.floor(s / 60);
  const rs = Math.floor(s % 60);
  if (m < 60) return `${m}m ${String(rs).padStart(2, '0')}s`;
  const hr = Math.floor(m / 60);
  return `${hr}h ${String(m % 60).padStart(2, '0')}m`;
}

/** Elapsed clock for a running job: 0:07, 1:02:03. */
export function clock(ms) {
  if (!(ms >= 0)) return '';
  const total = Math.floor(ms / 1000);
  const hr = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  const ss = String(s).padStart(2, '0');
  return hr ? `${hr}:${String(m).padStart(2, '0')}:${ss}` : `${m}:${ss}`;
}

export function parseTime(t) {
  if (!t) return null;
  const d = new Date(t);
  return Number.isNaN(d.getTime()) ? null : d;
}

export function relTime(t, now = Date.now()) {
  const d = parseTime(t);
  if (!d) return '';
  const diff = (now - d.getTime()) / 1000;
  if (diff < 0) return 'just now';
  if (diff < 45) return 'just now';
  if (diff < 90) return '1 min ago';
  if (diff < 3600) return `${Math.round(diff / 60)} min ago`;
  if (diff < 5400) return '1 hour ago';
  if (diff < 86400) return `${Math.round(diff / 3600)} hours ago`;
  if (diff < 172800) return 'yesterday';
  if (diff < 604800) return `${Math.round(diff / 86400)} days ago`;
  return d.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
}

export function absTime(t) {
  const d = parseTime(t);
  if (!d) return '';
  return d.toLocaleString(undefined, { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

export function timeOfDay(t) {
  const d = parseTime(t);
  if (!d) return '';
  return d.toLocaleTimeString(undefined, { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

export function bytes(n) {
  if (n === null || n === undefined) return '';
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(n < 10240 ? 1 : 0)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}

export function plural(n, one, many) {
  return `${n} ${n === 1 ? one : (many || `${one}s`)}`;
}

export function breadcrumb(path) {
  return (path || []).join(' › ');
}

/** True for a git URL source, false for an absolute or file:// local path. */
export function isLocalSource(source) {
  return typeof source === 'string' && (source.startsWith('/') || source.startsWith('file://'));
}

export function isGitUrl(source) {
  return typeof source === 'string' && !isLocalSource(source) && source.length > 0;
}

/** Last path element, for titles. */
export function baseName(p) {
  if (!p) return '';
  const s = String(p).replace(/\/+$/, '');
  const i = s.lastIndexOf('/');
  return i >= 0 ? s.slice(i + 1) : s;
}

export function dirName(p) {
  const s = String(p || '').replace(/\/+$/, '');
  const i = s.lastIndexOf('/');
  return i > 0 ? s.slice(0, i) : (i === 0 ? '/' : '');
}

export function joinPath(...parts) {
  return parts.filter((x) => x !== undefined && x !== null && x !== '')
    .join('/')
    .replace(/\/{2,}/g, '/');
}

/** A short title for a job, from its record. */
export function jobTitle(job) {
  const name = (job.module && job.module.name) || baseName(job.module && job.module.dir) || baseName(job.request && (job.request.source || job.request.module || job.request.output)) || job.id;
  return name;
}

const MODE_LABEL = { execute: 'Execute', 'dry-run': 'Dry run', local: 'Local run' };

export function jobKindLabel(job) {
  const req = job.request || {};
  switch (job.kind) {
    case 'run': return MODE_LABEL[req.mode || job.mode] || 'Run';
    case 'diff': return (req.quick || job.quick) ? 'Quick preview' : 'Full diff';
    case 'generate': return 'Generate';
    case 'bulk': return 'Bulk scaffold';
    default: return job.kind || 'Job';
  }
}

export const TERMINAL = new Set(['succeeded', 'failed', 'cancelled', 'interrupted']);
export const isTerminal = (state) => TERMINAL.has(state);

export const STATE_LABEL = {
  queued: 'Queued', running: 'Running', cancelling: 'Cancelling', succeeded: 'Succeeded',
  failed: 'Failed', cancelled: 'Cancelled', interrupted: 'Interrupted',
};
