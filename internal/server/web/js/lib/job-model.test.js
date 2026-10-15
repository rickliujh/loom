import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createJobModel, inScope, liveDiff } from './job-model.js';

test('events build the module/op tree and scope the log', () => {
  const m = createJobModel();
  const ev = [
    { seq: 1, type: 'job.state', state: 'running' },
    { seq: 2, type: 'module.start', path: ['root'], module: 'root-mod' },
    { seq: 3, type: 'module.start', path: ['root', 'a'], module: 'child' },
    { seq: 4, type: 'op.start', path: ['root', 'a'], op: 'render', kind: 'newFiles', index: 1, total: 2 },
    { seq: 5, type: 'log', path: ['root', 'a'], log: { level: 'INFO', msg: 'writing file', attrs: [['path', 'x']] } },
    { seq: 6, type: 'diff.file', path: ['root', 'a'], target: 'repo', diff: { path: 'x', status: 'added', unified: '@@ -0,0 +1 @@\n+x\n' } },
    { seq: 7, type: 'op.end', path: ['root', 'a'], op: 'render', kind: 'newFiles', index: 1, total: 2, durationMs: 5 },
    { seq: 8, type: 'op.start', path: ['root', 'a'], op: 'fmt', kind: 'shell', index: 2, total: 2 },
    { seq: 9, type: 'op.end', path: ['root', 'a'], op: 'fmt', kind: 'shell', index: 2, total: 2, error: 'exit 2' },
    { seq: 10, type: 'module.end', path: ['root', 'a'], durationMs: 9 },
    { seq: 11, type: 'job.state', state: 'failed', error: 'boom' },
  ];
  ev.forEach(m.apply);
  const root = m.roots[0];
  assert.equal(root.children[0].status, 'failed');
  assert.equal(root.status, 'failed');
  assert.deepEqual(root.children[0].ops.map((o) => o.status), ['ok', 'failed']);
  assert.equal(m.logs.length, 1);
  assert.ok(inScope(m.logs[0], root));
  assert.ok(inScope(m.logs[0], root.children[0].ops[0]));
  assert.ok(!inScope(m.logs[0], root.children[0].ops[1]));
  assert.equal(m.error, 'boom');
  assert.equal(liveDiff(m).targets[0].files.length, 1);
  assert.equal(m.lastSeq, 11);
});

test('a module that fails outside an operation is failed, and so is its ancestry', () => {
  const m = createJobModel();
  [
    { seq: 1, type: 'job.state', state: 'running' },
    { seq: 2, type: 'module.start', path: ['root'], module: 'wrapper' },
    { seq: 3, type: 'module.start', path: ['root', 'ok'], module: 'deploy' },
    { seq: 4, type: 'module.end', path: ['root', 'ok'], module: 'deploy' },
    { seq: 5, type: 'module.start', path: ['root', 'bad'], module: 'deploy' },
    { seq: 6, type: 'module.end', path: ['root', 'bad'], module: 'deploy', error: 'cloning target: repository not found' },
    { seq: 7, type: 'module.end', path: ['root'], module: 'wrapper', error: 'bad: cloning target: repository not found' },
    { seq: 8, type: 'job.state', state: 'failed', error: 'bad: cloning target' },
  ].forEach(m.apply);
  const root = m.roots[0];
  assert.equal(root.status, 'failed');
  assert.equal(root.children[1].status, 'failed');
  assert.equal(root.children[1].error, 'cloning target: repository not found');
  // A sibling that finished before the failure did succeed.
  assert.equal(root.children[0].status, 'ok');
});

test('a failed job fails what is still running and never leaves its root green', () => {
  const m = createJobModel();
  [
    { seq: 1, type: 'module.start', path: ['root'], module: 'wrapper' },
    { seq: 2, type: 'module.start', path: ['root', 'a'], module: 'child' },
    { seq: 3, type: 'op.start', path: ['root', 'a'], op: 'step', kind: 'shell', index: 1, total: 1 },
    { seq: 4, type: 'job.state', state: 'failed', error: 'boom' },
  ].forEach(m.apply);
  const root = m.roots[0];
  assert.equal(root.status, 'failed');
  assert.equal(root.children[0].status, 'failed');
  assert.equal(root.children[0].ops[0].status, 'failed');

  // Every module ended cleanly, yet the job failed afterwards.
  const late = createJobModel();
  [
    { seq: 1, type: 'module.start', path: ['root'], module: 'm' },
    { seq: 2, type: 'module.end', path: ['root'], module: 'm' },
    { seq: 3, type: 'job.state', state: 'failed', error: 'reading the diff back failed' },
  ].forEach(late.apply);
  assert.equal(late.roots[0].status, 'failed');

  // A cancelled job still reads as cancelled.
  const c = createJobModel();
  [
    { seq: 1, type: 'module.start', path: ['root'], module: 'm' },
    { seq: 2, type: 'job.state', state: 'cancelled' },
  ].forEach(c.apply);
  assert.equal(c.roots[0].status, 'cancelled');
});
