import { test } from 'node:test';
import assert from 'node:assert/strict';
import { fuzzy, fuzzyFilter } from './fuzzy.js';

test('fuzzy matches subsequences and prefers substrings', () => {
  assert.equal(fuzzy('obs', 'onboard-service') !== null, true);
  assert.equal(fuzzy('xyz', 'onboard-service'), null);
  const r = fuzzyFilter([{ n: 'platform-rollout' }, { n: 'onboard' }], 'onb', [(x) => x.n]);
  assert.equal(r[0].item.n, 'onboard');
});
