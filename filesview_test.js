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
const { FV_GROUPS, fvGroupOf, fvInGroup, fvSortList, fvVisible, fvToggleSel, fvRangeSel, FV_RECENT_CAP, fvMediaGroup, fvExtOf } = new Function(
  m[1] + '\nreturn { FV_GROUPS, fvGroupOf, fvInGroup, fvSortList, fvVisible, fvToggleSel, fvRangeSel, FV_RECENT_CAP, fvMediaGroup, fvExtOf };'
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

// --- v0.54: media groups classify by extension (daemon kind
// stays 'other' for media; the group layer owns the split) ---
assert(FV_GROUPS.some(g => g.id === 'music' && g.n === '音乐'), 'sidebar has 音乐');
assert(FV_GROUPS.some(g => g.id === 'video' && g.n === '视频'), 'sidebar has 视频');
for (const e of ['mp3', 'flac', 'wav', 'm4a', 'aac', 'ogg', 'opus', 'aiff']) {
  assert.strictEqual(fvGroupOf(F('s.' + e, 'other', 1, 1)), 'music', e + ' -> music');
}
for (const e of ['mp4', 'mkv', 'webm', 'mov', 'avi', 'm4v']) {
  assert.strictEqual(fvGroupOf(F('v.' + e, 'other', 1, 1)), 'video', e + ' -> video');
}
assert.strictEqual(fvGroupOf(F('S.MP3', 'other', 1, 1)), 'music', 'extension match is case-insensitive');
// ext may be absent (zip sub-listings): fall back to the name.
assert.strictEqual(fvMediaGroup({ name: 'clip.mkv', kind: 'other' }), 'video', 'name fallback when ext missing');
assert.strictEqual(fvExtOf({ name: 'noext', kind: 'other' }), '', 'no extension -> empty');
// media files still live in 全部/最近 and filter in every view
// (fvVisible is the single filter all three views consume).
const media = [F('song.mp3', 'other', 10, 500), F('clip.mp4', 'other', 20, 600), F('a.pdf', 'pdf', 30, 700)];
assert.deepStrictEqual(fvVisible(media, 'music', '', 'name', 1).map(f => f.name), ['song.mp3'], 'music group filters');
assert.deepStrictEqual(fvVisible(media, 'video', '', 'name', 1).map(f => f.name), ['clip.mp4'], 'video group filters');
assert.strictEqual(fvVisible(media, 'all', '', 'name', 1).length, 3, 'media still in 全部');
assert.strictEqual(fvVisible(media, 'recent', '', 'mtime', -1).length, 3, 'media still in 最近');
assert(!fvInGroup(F('song.mp3', 'other', 1, 1), 'other'), 'an mp3 is music now, not 其他');
assert(fvInGroup(F('song.mp3', 'other', 1, 1), 'music'), 'mp3 in music');

console.log('filesview_test: all assertions passed');
