/* Node assertions for the v0.59 change-feed client core: the
   pure event hub (topic routing, command routing, dedupe) and
   the reconnect backoff schedule. Same extract-and-assert
   pattern as spot_test: the functions live in index.html
   between the "events pure" markers.

   Run: node events_test.js
*/
'use strict';
const fs = require('fs');
const assert = require('assert');

const html = fs.readFileSync(__dirname + '/index.html', 'utf8');
const m = html.match(/\/\* === events pure \(node-testable\) === \*\/([\s\S]*?)\/\* === end events pure === \*\//);
assert(m, 'events pure block not found in index.html');
const { createEvHub, evBackoffDelay, evDedupeKey } = new Function(
  m[1] + '\nreturn { createEvHub, evBackoffDelay, evDedupeKey };'
)();

// --- backoff: 1s doubling, capped at 15s, negatives clamp ---
assert.strictEqual(evBackoffDelay(0), 1000, 'first retry waits 1s');
assert.strictEqual(evBackoffDelay(1), 2000, 'second retry waits 2s');
assert.strictEqual(evBackoffDelay(2), 4000, 'third retry waits 4s');
assert.strictEqual(evBackoffDelay(3), 8000, 'fourth retry waits 8s');
assert.strictEqual(evBackoffDelay(4), 15000, 'fifth retry hits the cap');
assert.strictEqual(evBackoffDelay(20), 15000, 'backoff stays capped');
assert.strictEqual(evBackoffDelay(-3), 1000, 'negative attempts clamp to the floor');

// --- dedupe key ---
assert.strictEqual(evDedupeKey('files', 'a.txt'), 'files|a.txt');
assert.strictEqual(evDedupeKey('nodes', ''), 'nodes|');
assert.strictEqual(evDedupeKey('nodes', undefined), 'nodes|');

// --- topic routing: only the subscribed topic fires ---
{
  let clock = 100000;
  const hub = createEvHub(() => clock);
  const got = [];
  hub.on('files', ev => got.push(ev.id));
  assert.strictEqual(hub.dispatch('{"topic":"layout","id":"x"}'), 'event', 'other topics still count as events');
  assert.deepStrictEqual(got, [], 'layout event must not reach the files subscriber');
  assert.strictEqual(hub.dispatch('{"topic":"files","id":"a.txt","ts":1}'), 'event');
  assert.deepStrictEqual(got, ['a.txt'], 'files event delivered with its id');
  assert.strictEqual(hub.subscriberCount('files'), 1);
  assert.strictEqual(hub.subscriberCount('nodes'), 0);
}

// --- dedupe: same topic+id inside the window drops, then passes ---
{
  let clock = 500000;
  const hub = createEvHub(() => clock);
  let n = 0;
  hub.on('files', () => n++);
  assert.strictEqual(hub.dispatch('{"topic":"files","id":"r.txt"}'), 'event');
  assert.strictEqual(hub.dispatch('{"topic":"files","id":"r.txt"}'), 'dup', 'immediate repeat dedupes');
  clock += 1000;
  assert.strictEqual(hub.dispatch('{"topic":"files","id":"r.txt"}'), 'dup', 'repeat inside 1.5s still dedupes');
  clock += 600;
  assert.strictEqual(hub.dispatch('{"topic":"files","id":"r.txt"}'), 'event', 'repeat after the window passes');
  assert.strictEqual(n, 2, 'subscriber saw exactly the two real deliveries');
  // a different id is a different change
  assert.strictEqual(hub.dispatch('{"topic":"files","id":"other.txt"}'), 'event');
  // id-less events always pass (they are state-change pings)
  assert.strictEqual(hub.dispatch('{"topic":"files"}'), 'event');
  assert.strictEqual(hub.dispatch('{"topic":"files"}'), 'event');
}

// --- command routing: the view-command family stays separate ---
{
  const hub = createEvHub(() => 0);
  const cmds = [];
  let topicHits = 0;
  hub.onCmd(m => cmds.push(m.cmd));
  hub.on('files', () => topicHits++);
  assert.strictEqual(hub.dispatch('{"cmd":"spotlight","ts":1}'), 'cmd');
  assert.deepStrictEqual(cmds, ['spotlight'], 'spotlight command delivered');
  assert.strictEqual(topicHits, 0, 'commands never reach topic subscribers');
}

// --- unsubscribe + malformed input ---
{
  const hub = createEvHub(() => 0);
  let n = 0;
  const off = hub.on('nodes', () => n++);
  hub.dispatch('{"topic":"nodes"}');
  off();
  hub.dispatch('{"topic":"nodes"}');
  assert.strictEqual(n, 1, 'unsubscribed handler stops receiving');
  assert.strictEqual(hub.dispatch('not json'), 'bad', 'malformed JSON is bad');
  assert.strictEqual(hub.dispatch('42'), 'bad', 'non-object JSON is bad');
  assert.strictEqual(hub.dispatch('{"unrelated":true}'), 'ignored', 'unknown shapes are ignored');
}

// --- a throwing subscriber cannot break the hub or the others ---
{
  const hub = createEvHub(() => 0);
  let ok = 0;
  hub.on('files', () => { throw new Error('boom'); });
  hub.on('files', () => ok++);
  assert.strictEqual(hub.dispatch('{"topic":"files","id":"z"}'), 'event');
  assert.strictEqual(ok, 1, 'healthy subscriber still receives after a sibling throws');
}

console.log('events_test: all assertions passed');
