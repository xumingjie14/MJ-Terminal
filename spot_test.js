/* Node assertions for the Spotlight pure functions (v0.50):
   ranking (prefix before substring, recency weighting) and the
   match tiers. Same extract-and-assert pattern as dockmag_test:
   the functions live in index.html between the "spot pure"
   markers.

   Run: node spot_test.js
*/
'use strict';
const fs = require('fs');
const assert = require('assert');

const html = fs.readFileSync(__dirname + '/index.html', 'utf8');
const m = html.match(/\/\* === spot pure \(node-testable\) === \*\/([\s\S]*?)\/\* === end spot pure === \*\//);
assert(m, 'spot pure block not found in index.html');
const { spotTier, spotRankList } = new Function(
  m[1] + '\nreturn { spotTier, spotRankList };'
)();

// --- tiers ---
assert.strictEqual(spotTier('set', 'Settings'), 2, 'prefix match tiers 2');
assert.strictEqual(spotTier('ett', 'Settings'), 1, 'substring match tiers 1');
assert.strictEqual(spotTier('xyz', 'Settings'), 0, 'no match tiers 0');
assert.strictEqual(spotTier('', 'Settings'), 1, 'empty query is neutral');
assert.strictEqual(spotTier('  ', 'Settings'), 1, 'blank query is neutral');
assert.strictEqual(spotTier('SET', 'settings'), 2, 'matching is case-insensitive');

// --- ranking with a query: prefix before substring, misses dropped ---
const items = [
  { key: 'a', name: 'Notes' },        // no match for "se"
  { key: 'b', name: 'User Settings' },// substring
  { key: 'c', name: 'Settings' },     // prefix
  { key: 'd', name: 'Reset' },        // substring
];
const ranked = spotRankList(items, 'se', []);
assert.deepStrictEqual(ranked.map(x => x.key), ['c', 'b', 'd'],
  'prefix first, then substrings in source order, misses dropped');

// --- recency weighting breaks ties inside a tier ---
const rec = ['d', 'b']; // d most recent
const ranked2 = spotRankList(items, 'se', rec);
assert.deepStrictEqual(ranked2.map(x => x.key), ['c', 'd', 'b'],
  'within the substring tier the more recent item wins');
const ranked3 = spotRankList(items, 'se', ['c']);
assert.strictEqual(ranked3[0].key, 'c', 'a recent prefix still leads');

// --- empty query: recency order, then source order; nothing dropped ---
const ranked4 = spotRankList(items, '', ['d', 'a']);
assert.deepStrictEqual(ranked4.map(x => x.key), ['d', 'a', 'b', 'c'],
  'empty query orders by recency, leftovers keep source order');

// --- input is not mutated ---
const before = items.map(x => x.key).join(',');
spotRankList(items, 'se', rec);
assert.strictEqual(items.map(x => x.key).join(','), before, 'input array untouched');

console.log('spot_test: all assertions passed');
