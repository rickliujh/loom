// Folds a job's event stream into the module/operation tree, the log, the
// live diffs and the PRs. Pure: no DOM, testable under node --test.

const TERMINAL = new Set(['succeeded', 'failed', 'cancelled', 'interrupted']);
const SEP = '\u0001';

export const pathKey = (path) => (path || []).join(SEP);

export function createJobModel() {
  const m = {
    nodes: new Map(), // key → module node
    roots: [],
    logs: [],
    diffs: [],
    prs: [],
    state: null,
    error: '',
    lastSeq: 0,
  };

  function ensureModule(path, name) {
    const key = pathKey(path);
    let n = m.nodes.get(key);
    if (n) {
      if (name && !n.module) n.module = name;
      return n;
    }
    n = {
      type: 'module', key, path: [...path], name: path[path.length - 1] || '(job)', module: name || '',
      status: 'pending', ops: [], children: [], start: null, durationMs: null, reason: '', error: '', failed: false, total: 0, current: null,
    };
    m.nodes.set(key, n);
    if (path.length > 1) {
      const parent = ensureModule(path.slice(0, -1));
      parent.children.push(n);
    } else {
      m.roots.push(n);
    }
    return n;
  }

  function ensureOp(node, ev) {
    const opKey = `${node.key}#${ev.index || 0}:${ev.op}`;
    let op = node.ops.find((o) => o.key === opKey);
    if (!op) {
      op = { type: 'op', key: opKey, moduleKey: node.key, path: node.path, name: ev.op, kind: ev.kind || '', index: ev.index || 0, status: 'pending', durationMs: null, error: '', reason: '', start: null };
      node.ops.push(op);
      node.ops.sort((a, b) => a.index - b.index);
    }
    if (ev.total) node.total = ev.total;
    if (ev.kind && !op.kind) op.kind = ev.kind;
    return op;
  }

  function markFailedUp(node) {
    let k = node.path;
    while (k.length) {
      const n = m.nodes.get(pathKey(k));
      if (n) n.failed = true;
      k = k.slice(0, -1);
    }
  }

  // A job's terminal state settles what is still running. A failed job
  // fails whatever it left running, and never leaves its root green: a
  // failure the events did not pin to a module (a load error, a failure
  // after the last module ended) is still the root's.
  function settleRunning(status) {
    const settled = status === 'succeeded' ? 'ok' : status === 'failed' ? 'failed' : 'cancelled';
    for (const n of m.nodes.values()) {
      if (n.status === 'running') {
        n.status = settled;
        if (settled === 'failed') markFailedUp(n);
      }
      for (const o of n.ops) if (o.status === 'running') o.status = settled;
    }
    if (status === 'failed') {
      for (const r of m.roots) {
        if (r.status === 'ok' || r.status === 'running') r.status = 'failed';
        r.failed = true;
      }
    }
  }

  /** apply(event) → a short description of what changed: 'tree'|'log'|'diff'|'pr'|'state'. */
  m.apply = (ev) => {
    if (ev.seq) m.lastSeq = Math.max(m.lastSeq, ev.seq);
    const path = ev.path || [];
    switch (ev.type) {
      case 'job.state':
        m.state = ev.state;
        if (ev.error) m.error = ev.error;
        if (TERMINAL.has(ev.state)) settleRunning(ev.state);
        return 'state';
      case 'module.start': {
        const n = ensureModule(path, ev.module);
        n.status = 'running';
        n.start = ev.time;
        return 'tree';
      }
      case 'module.end': {
        const n = ensureModule(path, ev.module);
        // A module can fail outside any operation: a child that would not
        // clone or load, an if predicate that errored, a cancellation
        // between steps. module.end carries that error.
        if (ev.error) {
          n.error = ev.error;
          markFailedUp(n);
        }
        n.status = n.failed ? 'failed' : 'ok';
        if (ev.durationMs !== undefined) n.durationMs = ev.durationMs;
        n.current = null;
        return 'tree';
      }
      case 'module.skip': {
        const n = ensureModule(path, ev.module);
        n.status = 'skipped';
        n.reason = ev.reason || '';
        return 'tree';
      }
      case 'op.start': {
        const n = ensureModule(path);
        const op = ensureOp(n, ev);
        op.status = 'running';
        op.start = ev.time;
        n.current = op;
        if (n.status === 'pending') n.status = 'running';
        return 'tree';
      }
      case 'op.end': {
        const n = ensureModule(path);
        const op = ensureOp(n, ev);
        op.status = ev.error ? 'failed' : 'ok';
        op.error = ev.error || '';
        if (ev.durationMs !== undefined) op.durationMs = ev.durationMs;
        if (ev.error) markFailedUp(n);
        if (n.current === op) n.current = null;
        return 'tree';
      }
      case 'op.skip': {
        const n = ensureModule(path);
        const op = ensureOp(n, ev);
        op.status = 'skipped';
        op.reason = ev.reason || '';
        return 'tree';
      }
      case 'log': {
        const l = ev.log || {};
        let moduleKey = '';
        let opKey = '';
        if (path.length) {
          const n = ensureModule(path);
          moduleKey = n.key;
          if (n.current) opKey = n.current.key;
        }
        m.logs.push({
          seq: ev.seq, time: ev.time, path, moduleKey, opKey,
          level: String(l.level || 'INFO').toUpperCase(), msg: l.msg || '', attrs: l.attrs || [],
          section: !!l.section, dispatch: !!l.dispatch, root: !!l.root,
        });
        return 'log';
      }
      case 'diff.file':
        m.diffs.push({ path, target: ev.target || '', diff: ev.diff || {} });
        return 'diff';
      case 'pr.created':
        m.prs.push({ path, ...(ev.pr || {}) });
        return 'pr';
      default:
        return '';
    }
  };

  return m;
}

/** Groups live diff.file events into the /diff response shape. */
export function liveDiff(model, incomplete) {
  const targets = [];
  const byKey = new Map();
  for (const d of model.diffs) {
    const key = `${pathKey(d.path)}|${d.target}`;
    let t = byKey.get(key);
    if (!t) {
      t = { path: d.path, repo: d.target, branch: '', files: [] };
      byKey.set(key, t);
      targets.push(t);
    }
    t.files.push(d.diff);
  }
  return { mode: 'live', incomplete: !!incomplete, targets };
}

/** Does a log entry belong to the selected tree node? */
export function inScope(entry, sel) {
  if (!sel) return true;
  if (sel.type === 'op') return entry.opKey === sel.key;
  if (!sel.key) return true;
  return entry.moduleKey === sel.key || entry.moduleKey.startsWith(sel.key + SEP);
}
