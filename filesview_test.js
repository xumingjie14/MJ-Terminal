/* Node assertions for the Files (Finder) pure functions
   (v0.50): smart grouping by kind, group+search+sort
   composition, the recent cap, and the selection helpers.
   Extract-and-assert like dockmag_test.

   Run: node filesview_test.js
*/
'use strict';
const fs = require('fs');
const assert = require('assert');

const html = fs.readFileSync(__dirname + '/index.html', 'utf8');
const m = html.match(/\/\* === fv pure \(node-testable\) === \*\/([\s\S]*?)\/\* === end fv pure === \*\//);
assert(m, 'fv pure block not found in index.html');
const { FV_GROUPS, fvGroupOf, fvInGroup, fvSortList, fvVisible, fvToggleSel, fvRangeSel, FV_RECENT_CAP } = new Function(
  m[1] + '\nreturn { FV_GROUPS, fvGroupOf, fvInGroup, fvSortList, fvVisible, fvToggleSel, fvRangeSel, FV_RECENT_CAP };'
)();

const F = (name, kind, size, mtime) => ({ name, path: name, kind, size, mtime, ext: name.split('.').pop() });

// --- grouping by daemon kind ---
assert.strictEqual(fvGroupOf(F('a.png', 'image', 1, 1)), 'image');
assert.strictEqual(fvGroupOf(F('a.pdf', 'pdf', 1, 1)), 'doc');
assert.strictEqual(fvGroupOf(F('a.txt', 'text', 1, 1)), 'doc');
assert.strictEqual(fvGroupOf(F('a.docx', 'docx', 1, 1)), 'doc');
assert.strictEqual(fvGroupOf(F('a.csv', 'csv', 1, 1)), 'sheet');
assert.strictEqual(fvGroupOf(F('a.xlsx', 'xlsx', 1, 1)), 'sheet');
assert.strictEqual(fvGroupOf(F('a.zip', 'zip', 1, 1)), 'zip');
assert.strictEqual(fvGroupOf(F('a.exe', 'exe', 1, 1)), 'other');
assert.strictEqual(fvGroupOf(F('d', 'dir', 0, 1)), 'other');
assert(FV_GROUPS.some(g => g.id === 'all') && FV_GROUPS.some(g => g.id === 'recent'), 'sidebar has all + recent');
assert(fvInGroup(F('a.png', 'image', 1, 1), 'all'), 'all contains everything');
assert(fvInGroup(F('a.png', 'image', 1, 1), 'recent'), 'recent pre-filter contains everything');
assert(!fvInGroup(F('a.png', 'image', 1, 1), 'doc'), 'image is not in doc');

const files = [
  F('b.png', 'image', 300, 100),
  F('a.pdf', 'pdf', 100, 300),
  F('c.csv', 'csv', 200, 200),
  F('d.zip', 'zip', 50, 400),
];

// --- group + sort composition ---
assert.deepStrictEqual(fvVisible(files, 'all', '', 'name', 1).map(f => f.name),
  ['a.pdf', 'b.png', 'c.csv', 'd.zip'], 'name sort ascending');
assert.deepStrictEqual(fvVisible(files, 'all', '', 'size', -1).map(f => f.name),
  ['b.png', 'c.csv', 'a.pdf', 'd.zip'], 'size sort descending');
assert.deepStrictEqual(fvVisible(files, 'all', '', 'mtime', -1).map(f => f.name),
  ['d.zip', 'a.pdf', 'c.csv', 'b.png'], 'mtime sort descending');
assert.deepStrictEqual(fvVisible(files, 'doc', '', 'name', 1).map(f => f.name),
  ['a.pdf'], 'doc group filters');
assert.deepStrictEqual(fvVisible(files, 'all', 'P', 'name', 1).map(f => f.name),
  ['a.pdf', 'b.png', 'd.zip'], 'search filters case-insensitively inside the group (zip contains a p)');
assert.deepStrictEqual(fvVisible(files, 'sheet', 'zzz', 'name', 1), [], 'search can empty a group');

// --- recent: newest first by mtime, capped, still sorted view of the cap ---
assert.deepStrictEqual(fvVisible(files, 'recent', '', 'mtime', -1).map(f => f.name),
  ['d.zip', 'a.pdf', 'c.csv', 'b.png'], 'recent is mtime-descending');
const many = Array.from({ length: FV_RECENT_CAP + 5 }, (_, i) => F('f' + i + '.txt', 'text', i, i));
const rec = fvVisible(many, 'recent', '', 'name', 1);
assert.strictEqual(rec.length, FV_RECENT_CAP, 'recent is capped');
assert(!rec.some(f => f.mtime < 5), 'the cap keeps the newest, not the first N');

// --- kind sort groups by smart group, name inside ---
const ks = fvSortList(files, 'kind', 1).map(f => fvGroupOf(f));
assert.deepStrictEqual(ks, ['image', 'doc', 'sheet', 'zip'], 'kind sort follows group order');

// --- selection helpers ---
assert.deepStrictEqual(fvToggleSel(['a'], 'b').sort(), ['a', 'b'], 'toggle adds');
assert.deepStrictEqual(fvToggleSel(['a', 'b'], 'b'), ['a'], 'toggle removes');
assert.deepStrictEqual(fvRangeSel(['a', 'b', 'c', 'd'], 'b', 'd'), ['b', 'c', 'd'], 'range forward');
assert.deepStrictEqual(fvRangeSel(['a', 'b', 'c', 'd'], 'd', 'b'), ['b', 'c', 'd'], 'range backward');
assert.deepStrictEqual(fvRangeSel(['a', 'b'], 'x', 'b'), ['b'], 'missing anchor selects the target only');

// --- inputs not mutated ---
const before = files.map(f => f.name).join(',');
fvVisible(files, 'all', '', 'size', 1);
fvSortList(files, 'mtime', -1);
assert.strictEqual(files.map(f => f.name).join(','), before, 'source array untouched');

console.log('filesview_test: all assertions passed');
