import { test } from 'node:test';
import assert from 'node:assert/strict';
import { parseUnified, splitRows } from './diff.js';

test('parseUnified numbers lines and counts changes', () => {
  const d = parseUnified('@@ -1,3 +1,4 @@ spec:\n a\n-b\n+B\n+c\n d\n\\ No newline at end of file\n');
  assert.equal(d.add, 2);
  assert.equal(d.del, 1);
  const [h] = d.hunks;
  assert.equal(h.section, 'spec:');
  assert.deepEqual(h.lines.map((l) => [l.type, l.old, l.new]), [
    ['ctx', 1, 1], ['del', 2, null], ['add', null, 2], ['add', null, 3], ['ctx', 3, 4], ['meta', null, null],
  ]);
});

test('parseUnified skips file headers', () => {
  const d = parseUnified('diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -0,0 +1 @@\n+hi\n');
  assert.equal(d.hunks.length, 1);
  assert.equal(d.add, 1);
  assert.equal(d.del, 0);
});

test('splitRows pairs deletions with additions', () => {
  const { hunks } = parseUnified('@@ -1,2 +1,3 @@\n-a\n-b\n+A\n+B\n+C\n');
  const rows = splitRows(hunks[0]);
  assert.equal(rows.length, 3);
  assert.equal(rows[2].left, null);
  assert.equal(rows[2].right.text, 'C');
});
