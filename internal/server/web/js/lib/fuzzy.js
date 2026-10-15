// Subsequence fuzzy matching for the module filter: every query character must
// appear in order; consecutive runs and word starts score higher.

const BOUNDARY = /[\s/_\-.›]/;

/** fuzzy(query, text) → {score, indices} or null when text does not match. */
export function fuzzy(query, text) {
  const q = String(query || '').toLowerCase().replace(/\s+/g, '');
  const t = String(text || '');
  const tl = t.toLowerCase();
  if (!q) return { score: 0, indices: [] };

  // A plain substring is always the best explanation of a match.
  const sub = tl.indexOf(q);
  if (sub >= 0) {
    const indices = [];
    for (let i = 0; i < q.length; i++) indices.push(sub + i);
    const atBoundary = sub === 0 || BOUNDARY.test(t[sub - 1]);
    return { score: 1000 + (atBoundary ? 200 : 0) - sub - (t.length - q.length) * 0.1, indices };
  }

  const indices = [];
  let score = 0;
  let ti = 0;
  let prev = -2;
  for (let qi = 0; qi < q.length; qi++) {
    const c = q[qi];
    let found = -1;
    // Prefer a word start ahead of the plain next occurrence.
    for (let j = ti; j < tl.length; j++) {
      if (tl[j] !== c) continue;
      if (found < 0) found = j;
      if (j === 0 || BOUNDARY.test(t[j - 1])) { found = j; break; }
      if (j === prev + 1) { found = j; break; }
    }
    if (found < 0) return null;
    indices.push(found);
    score += 10;
    if (found === prev + 1) score += 15;
    if (found === 0 || BOUNDARY.test(t[found - 1])) score += 20;
    score -= Math.min(found - ti, 10);
    prev = found;
    ti = found + 1;
  }
  return { score: score - t.length * 0.05, indices };
}

/**
 * fuzzyFilter(items, query, fields) ranks items by their best-matching field.
 * fields: [(item) → string]. Returns [{item, score, matches: [indices|null]}].
 */
export function fuzzyFilter(items, query, fields) {
  const out = [];
  for (const item of items) {
    let best = null;
    const matches = fields.map((f) => {
      const m = fuzzy(query, f(item));
      if (m && (!best || m.score > best)) best = m.score;
      return m ? m.indices : null;
    });
    if (best !== null) out.push({ item, score: best, matches });
  }
  if (query) out.sort((a, b) => b.score - a.score);
  return out;
}
