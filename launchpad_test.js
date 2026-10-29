/* Node assertions for the Launchpad pure functions (v0.50):
   pagination math, insertion-index geometry and the name
   filter. Extract-and-assert like dockmag_test.

   Run: node launchpad_test.js
*/
'use strict';
const fs = require('fs');
const assert = require('assert');

const html = fs.readFileSync(__dirname + '/index.html', 'utf8');
const m = html.match(/\/\* === lp pure \(node-testable\) === \*\/([\s\S]*?)\/\* === end lp pure === \*\//);
assert(m, 'lp pure block not found in index.html');
const { lpDims, lpPageCount, lpPageOfIndex, lpInsertIndex, lpFilterKeys } = new Function(
  m[1] + '\nreturn { lpDims, lpPageCount, lpPageOfIndex, lpInsertIndex, lpFilterKeys };'
)();

// --- grid dims clamp to the macOS-ish ranges ---
let d = lpDims(1920, 1080);
assert(d.cols >= 5 && d.cols <= 8 && d.rows >= 3 && d.rows <= 6, 'dims within clamps at 1080p');
assert.strictEqual(d.per, d.cols * d.rows, 'per = cols*rows');
d = lpDims(800, 600);
assert.strictEqual(d.cols, 5, 'narrow viewport clamps to 5 columns');
assert.strictEqual(d.rows, 3, 'short viewport clamps to 3 rows');
d = lpDims(4000, 3000);
assert.strictEqual(d.cols, 8, 'huge viewport clamps to 8 columns');
assert.strictEqual(d.rows, 6, 'huge viewport clamps to 6 rows');

// --- pagination: a full page fits, one extra item spills ---
const per = lpDims(1440, 900).per;
assert.strictEqual(lpPageCount(0, 1440, 900), 1, 'empty still has one page');
assert.strictEqual(lpPageCount(per, 1440, 900), 1, 'exactly one full page');
assert.strictEqual(lpPageCount(per + 1, 1440, 900), 2, 'one extra item = second page');
assert.strictEqual(lpPageOfIndex(0, 1440, 900), 0);
assert.strictEqual(lpPageOfIndex(per - 1, 1440, 900), 0, 'last slot of page 1');
assert.strictEqual(lpPageOfIndex(per, 1440, 900), 1, 'first slot of page 2');

// --- insertion index over a 3x2 grid of centres (row-major) ---
const C = [
  { x: 100, y: 100 }, { x: 300, y: 100 }, { x: 500, y: 100 },
  { x: 100, y: 300 }, { x: 300, y: 300 }, { x: 500, y: 300 },
];
assert.strictEqual(lpInsertIndex([], 10, 10), 0, 'no items -> 0');
assert.strictEqual(lpInsertIndex(C, 50, 100), 0, 'before the first centre');
assert.strictEqual(lpInsertIndex(C, 200, 100), 1, 'between col 1 and 2 of row 1');
assert.strictEqual(lpInsertIndex(C, 600, 100), 3, 'past row 1');
assert.strictEqual(lpInsertIndex(C, 50, 300), 3, 'start of row 2');
assert.strictEqual(lpInsertIndex(C, 350, 290), 5, 'row 2, after two centres');
assert.strictEqual(lpInsertIndex(C, 600, 400), 6, 'past everything');

// --- name filter ---
const entries = [
  { key: 'app:sys', name: '设置' },
  { key: 'app:core', name: '节点' },
  { key: 'web:x', name: 'X Twitter' },
];
assert.deepStrictEqual(lpFilterKeys(entries, ''), ['app:sys', 'app:core', 'web:x'], 'empty query keeps all, in order');
assert.deepStrictEqual(lpFilterKeys(entries, 'twit'), ['web:x'], 'case-insensitive substring');
assert.deepStrictEqual(lpFilterKeys(entries, '设'), ['app:sys'], 'CJK substring');
assert.deepStrictEqual(lpFilterKeys(entries, 'zzz'), [], 'no match -> empty');

console.log('launchpad_test: all assertions passed');
