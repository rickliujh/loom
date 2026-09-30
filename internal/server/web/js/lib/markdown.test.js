import { test } from 'node:test';
import assert from 'node:assert/strict';
import { parseMarkdown, parseInline } from './markdown.js';

test('blocks: headings, fences, lists, tables, callouts', () => {
  const ast = parseMarkdown([
    '# Title {#t}',
    '',
    'Some *text* and `code`.',
    '',
    '```yaml',
    'a: {{ .x }}',
    '```',
    '',
    '- one',
    '- two',
    '  - nested',
    '',
    '| A | B |',
    '|---|:-:|',
    '| `a|b` | 2 |',
    '',
    '::: tip',
    'Hello',
    ':::',
  ].join('\n'));
  assert.deepEqual(ast.map((b) => b.type), ['heading', 'para', 'code', 'list', 'table', 'callout']);
  assert.equal(ast[0].inlines[0].text, 'Title');
  assert.equal(ast[2].text, 'a: {{ .x }}');
  assert.equal(ast[3].items.length, 2);
  assert.equal(ast[3].items[1][1].type, 'list');
  assert.equal(ast[4].rows[0][0][0].text, 'a|b');
  assert.equal(ast[4].align[1], 'center');
  assert.equal(ast[5].kind, 'tip');
});

test('inline: raw HTML stays text, v-pre becomes code', () => {
  const inl = parseInline('<script>x</script> <code v-pre>{{ .a }}</code> [l](/guide/x) **b**');
  assert.equal(inl[0].type, 'text');
  assert.ok(inl[0].text.includes('<script>'));
  assert.deepEqual(inl.filter((n) => n.type !== 'text').map((n) => n.type), ['code', 'link', 'strong']);
});

test('inline: snake_case is not emphasis', () => {
  const inl = parseInline('use some_param_name here');
  assert.equal(inl.length, 1);
  assert.equal(inl[0].text, 'use some_param_name here');
});
