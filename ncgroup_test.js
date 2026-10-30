/* Node assertions for the Notification Center pure functions
   (v0.47), same extract-and-assert pattern as dockmag_test.js:
   the functions live in index.html between the "ncgroup pure"
   markers so this file can lift them out without a DOM.

   Covered semantics (from the macOS User Guide's Notification
   Center behaviour): events stack BY SOURCE with the newest on
   top, group order follows each group's newest event, and the
   widget layout honours show/order with a safe fallback.

   Run: node ncgroup_test.js
*/
'use strict';
const fs = require('fs');
const assert = require('assert');

const html = fs.readFileSync(__dirname + '/index.html', 'utf8');
const m = html.match(/\/\* === ncgroup pure \(node-testable\) === \*\/([\s\S]*?)\/\* === end ncgroup pure === \*\//);
assert(m, 'ncgroup pure block not found in index.html');
const { ncGroupEvents, ncLayoutWidgets, ncEventCategory, ncGroupKey } = new Function(
  m[1] + '\nreturn { ncGroupEvents, ncLayoutWidgets, ncEventCategory, ncGroupKey };'
)();

const ev = (id, source, time) => ({ id, source, time, title: 't' + id });

// --- grouping: stacks by source, newest first everywhere ---
const list = [
  ev('n5', 'node', 500),
  ev('n4', 'sandbox', 400),
  ev('n3', 'node', 300),
  ev('n2', 'node', 200),
  ev('n1', 'files', 100),
];
const g = ncGroupEvents(list);
assert.strictEqual(g.length, 3, 'three sources -> three groups');
assert.strictEqual(g[0].source, 'node', 'group with the newest event comes first');
assert.deepStrictEqual(g[0].items.map(e => e.id), ['n5', 'n3', 'n2'], 'stack keeps newest-first order');
assert.strictEqual(g[1].source, 'sandbox');
assert.strictEqual(g[2].source, 'files');
assert.strictEqual(g[2].items.length, 1, 'single-event source stays a single card');

// clear semantics inputs: dismiss removes exactly one id,
// clear-source removes exactly one group -- exercised here as
// the grouping invariants the UI relies on after those calls.
const afterDismiss = ncGroupEvents(list.filter(e => e.id !== 'n3'));
assert.deepStrictEqual(afterDismiss[0].items.map(e => e.id), ['n5', 'n2'], 'dismissing the middle of a stack keeps the rest stacked');
const afterClear = ncGroupEvents(list.filter(e => e.source !== 'node'));
assert.strictEqual(afterClear.length, 2, 'clearing a source removes only its group');
assert(afterClear.every(x => x.source !== 'node'), 'cleared source is gone entirely');

// expand count: a stack of N renders one collapsed card with
// count N and expands to N cards (the renderer's contract).
assert.strictEqual(g[0].items.length, 3, 'stack count = items length');

// degenerate inputs
assert.deepStrictEqual(ncGroupEvents([]), [], 'empty list -> no groups');
assert.deepStrictEqual(ncGroupEvents(null), [], 'null -> no groups');
assert.deepStrictEqual(ncGroupEvents(undefined), [], 'undefined -> no groups');

// --- widget layout: order + show honoured, junk falls back ---
const DEF = [{ id: 'calendar', show: true }, { id: 'server', show: true }, { id: 'node', show: true }];
assert.deepStrictEqual(ncLayoutWidgets(''), DEF, 'empty setting -> default layout');
assert.deepStrictEqual(ncLayoutWidgets(null), DEF, 'null -> default layout');
assert.deepStrictEqual(ncLayoutWidgets('{oops'), DEF, 'broken JSON -> default layout');
assert.deepStrictEqual(ncLayoutWidgets('[]'), DEF, 'empty array -> default layout');
assert.deepStrictEqual(
  ncLayoutWidgets('[{"id":"node","show":false},{"id":"calendar","show":true}]'),
  [{ id: 'node', show: false }, { id: 'calendar', show: true }],
  'stored order and show flags are honoured'
);
assert.deepStrictEqual(
  ncLayoutWidgets('[{"id":"weather","show":true}]'), DEF,
  'unknown widget id (weather has no data source) -> default layout'
);
assert.deepStrictEqual(
  ncLayoutWidgets('[{"id":"node","show":true},{"id":"node","show":true}]'), DEF,
  'duplicate ids -> default layout'
);
// the fallback must be a fresh copy: mutating it must not leak
// into the next call's default.
const a = ncLayoutWidgets('');
a[0].show = false;
assert.deepStrictEqual(ncLayoutWidgets(''), DEF, 'default layout is not shared mutable state');

// --- v0.54: the 系统/应用 split -- app events stack per app,
// system events keep stacking by source, legacy events (no
// category) read as system. ---
assert.strictEqual(ncEventCategory({ source: 'node' }), 'system', 'no category -> system');
assert.strictEqual(ncEventCategory({ source: 'app', category: 'app', appId: 'firefox' }), 'app');
assert.strictEqual(ncGroupKey({ source: 'node' }), 'node', 'system key = source');
assert.strictEqual(ncGroupKey({ source: 'app', category: 'app', appId: 'prod-krita' }), 'app:prod-krita', 'app key = app:<id>');
const aev = (id, appId, appName, time) => ({ id, source: 'app', category: 'app', appId, appName, time, title: 't' + id });
const mixed = [
  aev('a3', 'store-telegram', 'Telegram', 900),
  ev('s1', 'node', 800),
  aev('a2', 'firefox', 'Firefox', 700),
  aev('a1', 'store-telegram', 'Telegram', 600),
];
const mg = ncGroupEvents(mixed);
assert.strictEqual(mg.length, 3, 'two apps + one source -> three groups');
assert.strictEqual(mg[0].key, 'app:store-telegram', 'newest group first (telegram install pair)');
assert.deepStrictEqual(mg[0].items.map(e => e.id), ['a3', 'a1'], 'per-app stack keeps its own events only');
assert.strictEqual(mg[0].name, 'Telegram', 'app group carries the app name');
assert.strictEqual(mg[0].category, 'app');
assert.strictEqual(mg[1].key, 'node', 'system group keeps the source key');
assert.strictEqual(mg[1].category, 'system');
assert.strictEqual(mg[2].key, 'app:firefox', 'a second app is its own stack, not merged into 系统');

console.log('ncgroup_test: all assertions passed');
