// node --test internal/server/web/js/lib/
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { splitTopLevel, valueText, composeParams, indent } from './yaml-view.js';

test('splitTopLevel keeps structured values verbatim', () => {
  const doc = [
    '# guestbook',
    'appName: guestbook',
    'version: 1.10   # stays text',
    'sources:',
    '  - repoURL: https://charts.example.com',
    '    targetRevision: 1.10',
    '  - ref: values',
    '',
    'labels: {team: platform}',
    'tags:',
    '- a',
    '- b',
  ].join('\n');
  const m = splitTopLevel(doc);
  assert.deepEqual([...m.keys()], ['appName', 'version', 'sources', 'labels', 'tags']);
  assert.equal(m.get('version').inline, '1.10');
  assert.equal(valueText(m.get('sources')), '- repoURL: https://charts.example.com\n  targetRevision: 1.10\n- ref: values\n');
  assert.equal(valueText(m.get('labels')), '{team: platform}');
  assert.equal(valueText(m.get('tags')), '- a\n- b\n');
});

test('splitTopLevel handles quoted keys and rejects non-maps', () => {
  const m = splitTopLevel('"odd key": 1\n\'it\'\'s\': x\n');
  assert.deepEqual([...m.keys()], ['odd key', "it's"]);
  assert.equal(splitTopLevel('- a\n- b\n'), null);
  assert.equal(splitTopLevel('{a: 1}'), null);
});

test('composeParams writes strings quoted and structured text indented', () => {
  const out = composeParams([
    { name: 'appName', type: 'string', text: 'guest"book' },
    { name: 'sources', type: 'list', text: '- repoURL: x\n  targetRevision: 1.10\n' },
  ]);
  assert.equal(out, 'appName: "guest\\"book"\nsources:\n  - repoURL: x\n    targetRevision: 1.10\n');
  const back = splitTopLevel(out);
  assert.equal(valueText(back.get('sources')), '- repoURL: x\n  targetRevision: 1.10\n');
});

test('indent leaves blank lines empty', () => {
  assert.equal(indent('a:\n\n  b: 1', 2), '  a:\n\n    b: 1');
});
