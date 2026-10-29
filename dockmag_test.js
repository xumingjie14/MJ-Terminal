/* Node assertions for the Dock magnification pure functions
   (v0.46), same extract-and-assert pattern as the v0.31 drag
   pure-function tests: the functions live in index.html between
   the "dockmag pure" markers so this file can lift them out and
   exercise them without a DOM.

   Run: node dockmag_test.js
*/
'use strict';
const fs = require('fs');
const assert = require('assert');

const html = fs.readFileSync(__dirname + '/index.html', 'utf8');
const m = html.match(/\/\* === dockmag pure \(node-testable\) === \*\/([\s\S]*?)\/\* === end dockmag pure === \*\//);
assert(m, 'dockmag pure block not found in index.html');
const { dockMagScale, dockMagSizes } = new Function(
  m[1] + '\nreturn { dockMagScale, dockMagSizes };'
)();

const R = 177;    // DOCK_RADIUS_PITCHES(3) * (DOCK_BASE 52 + DOCK_GAP 7)
const PEAK = 1.85;
const EPS = 1e-9;

// peak exactly at distance 0
assert(Math.abs(dockMagScale(0, R, PEAK) - PEAK) < EPS, 'scale(0) must equal peak');
// flat top: derivative ~0 at the peak (quarter-radius still near peak)
assert(dockMagScale(R * 0.25, R, PEAK) > 1 + (PEAK - 1) * 0.85, 'cosine top should be flat-ish');
// at and beyond the radius: exactly 1
assert.strictEqual(dockMagScale(R, R, PEAK), 1, 'scale(radius) must be 1');
assert.strictEqual(dockMagScale(R * 2, R, PEAK), 1, 'scale(2*radius) must be 1');
assert.strictEqual(dockMagScale(1e6, R, PEAK), 1, 'scale(far) must be 1');
// symmetric
for (const d of [0, 10, 59, 118, 176.9, 177, 300]) {
  assert.strictEqual(dockMagScale(-d, R, PEAK), dockMagScale(d, R, PEAK), 'symmetry at ' + d);
}
// monotonic non-increasing on [0, R]
let prev = Infinity;
for (let d = 0; d <= R; d += 3) {
  const s = dockMagScale(d, R, PEAK);
  assert(s <= prev + EPS, 'monotonic decrease violated at ' + d);
  assert(s >= 1 && s <= PEAK, 'range violated at ' + d);
  prev = s;
}
// degenerate radius never divides by zero / never magnifies
assert.strictEqual(dockMagScale(0, 0, PEAK), 1, 'radius 0 must be safe');
assert.strictEqual(dockMagScale(5, -3, PEAK), 1, 'negative radius must be safe');

// sizes: 7 icons at pitch 59, pointer over the middle icon's centre
const centers = [0, 59, 118, 177, 236, 295, 354];
const sizes = dockMagSizes(centers, 177, 52, R, PEAK);
assert.strictEqual(sizes.length, 7);
assert(Math.abs(sizes[3] - 52 * PEAK) < 1e-6, 'centre icon at peak size');
assert(Math.abs(sizes[0] - 52) < 1e-6 && Math.abs(sizes[6] - 52) < 1e-6, 'rim icons at base size');
assert(Math.abs(sizes[2] - sizes[4]) < 1e-9, 'sizes symmetric around the pointer');
assert(sizes[3] > sizes[2] && sizes[2] > sizes[1] && sizes[1] > sizes[0], 'sizes fall off outward');
for (const s of sizes) assert(s >= 52 && s <= 52 * PEAK, 'size within [base, base*peak]');

// pointer far away: everything at base
const far = dockMagSizes(centers, 5000, 52, R, PEAK);
assert(far.every(s => s === 52), 'far pointer leaves all icons at base');

console.log('dockmag_test: all assertions passed');
