/* Boot/render regression test (v0.55): runs the REAL script blocks
   of index.html under a fake DOM + stubbed fetch, the way the Edge
   view would load them, and asserts the desktop + Launchpad lists
   actually paint.

   It exists because of the v0.54 incident: the Dock builder loop
   runs during initial script evaluation and calls appIcon() for
   every app; v0.54's appIcon('firefox') read FFSTATE, whose `let`
   sat ~770 lines later in the same script block -- a temporal-dead-
   zone ReferenceError that aborted the whole block, left every
   later let/const uninitialized, skipped the boot sequence, and
   blanked the desktop and the Launchpad while menus/windows (pure
   function declarations, hoisted) kept working. node --check and
   the pure-function tests could not see it; only evaluating the
   page like a browser can.

   Also asserted: the v0.55 containment -- one malformed data record
   (a component whose name is null, which makes its icon builder
   throw) is skipped/degraded and everything else still renders,
   on the desktop AND in the Launchpad.

   Run: node deskboot_test.js   (reads ./index.html, ./apps.json
   not needed; all API data is stubbed inline) */
'use strict';
const fs = require('fs');
const vm = require('vm');
const assert = require('assert');
const path = require('path');

const html = fs.readFileSync(process.env.MJ_TEST_HTML || path.join(__dirname, 'index.html'), 'utf8');
const blocks = [...html.matchAll(/<script>([\s\S]*?)<\/script>/g)].map(m => m[1]);
assert.strictEqual(blocks.length, 3, 'expected 3 script blocks in index.html');

/* ---------- fake DOM ---------- */
class El {
  constructor(tag) {
    this.tagName = (tag || 'div').toUpperCase();
    this.children = [];
    this.style = {};
    this.dataset = {};
    this._cls = new Set();
    this._innerHTML = '';
    this.textContent = '';
    this.value = '';
    this.title = '';
    this.id = '';
    const self = this;
    this.classList = {
      add: (...c) => c.forEach(x => self._cls.add(x)),
      remove: (...c) => c.forEach(x => self._cls.delete(x)),
      toggle: (c, f) => { if (f === undefined) f = !self._cls.has(c); f ? self._cls.add(c) : self._cls.delete(c); return f; },
      contains: c => self._cls.has(c),
    };
  }
  set innerHTML(v) { this._innerHTML = String(v); if (v === '') this.children = []; }
  get innerHTML() { return this._innerHTML; }
  appendChild(c) { this.children.push(c); return c; }
  insertBefore(c, ref) { const i = ref ? this.children.indexOf(ref) : -1; if (i < 0) this.children.push(c); else this.children.splice(i, 0, c); return c; }
  removeChild(c) { const i = this.children.indexOf(c); if (i >= 0) this.children.splice(i, 1); return c; }
  remove() {}
  querySelectorAll() { return []; }
  querySelector() { if (!this._qs) this._qs = new El('div'); return this._qs; }
  addEventListener() {}
  removeEventListener() {}
  getBoundingClientRect() { return { left: 0, top: 0, right: 92, bottom: 100, width: 92, height: 100 }; }
  setPointerCapture() {}
  cloneNode() { return new El(this.tagName); }
  focus() {}
  click() {}
  closest() { return null; }
}

function baseData() {
  return {
    '/api/layout': { pos: { 'app:sys': { x: 1700, y: 60 } }, folders: [{ id: 'f1', name: '常用', items: ['https://x.com'] }] },
    '/api/apps': { apps: [
      { name: 'YouTube', url: 'https://www.youtube.com' },
      { name: 'X', url: 'https://x.com' },
      { name: 'GitHub', url: 'https://github.com' },
    ] },
    '/api/apps/catalog': { apps: [
      { id: 'telegram', name: 'Telegram', icon: '', homepage: 'https://telegram.org/', installed: true },
      { id: 'vlc', name: 'VLC', icon: '', homepage: 'https://www.videolan.org/', installed: true },
      { id: 'sumatra', name: 'SumatraPDF', icon: '', homepage: '', installed: false },
    ] },
    '/api/sandbox/apps': { apps: [
      { id: 'wechat', name: '微信', exe: 'C:\\WeChat\\WeChat.exe', present: true, sealed: true },
    ] },
    '/api/components': { components: [
      { id: 'onlyoffice', name: 'ONLYOFFICE', installed: false, phase: 'idle', progress: 0, size: 397286158 },
      { id: 'imageglass', name: 'ImageGlass', installed: true, phase: 'idle', progress: 0, size: 43934433 },
    ] },
    '/api/firefox': { installed: true, version: '153.5.1', phase: 'idle', progress: 0, size: 0, message: '' },
    '/api/settings': {},
    '/api/files': { files: [
      { name: '照片.jpg', path: '照片.jpg', kind: 'image', ext: 'jpg', size: 1024, mtime: 1761000000 },
      { name: '歌.mp3', path: '歌.mp3', kind: 'other', ext: 'mp3', size: 2048, mtime: 1761000100 },
    ] },
    '/api/system': { version: '0.55', os: 'windows', arch: 'amd64', hostname: 'PC', numCPU: 8, exeDir: 'C:\\MJ', dataDir: 'C:\\MJ\\Data', dataFallback: false },
  };
}

async function bootScenario(label, mutate) {
  const data = baseData();
  if (mutate) mutate(data);
  const byId = new Map();
  const errors = [];
  const quietConsole = {
    log: () => {}, warn: (...a) => errors.push(a.join(' ')), error: (...a) => errors.push(a.join(' ')),
  };
  const sandbox = {
    console: quietConsole,
    document: {
      getElementById(id) { if (!byId.has(id)) { const e = new El('div'); e.id = id; byId.set(id, e); } return byId.get(id); },
      createElement(tag) { return new El(tag); },
      querySelectorAll() { return []; },
      querySelector() { return null; },
      addEventListener() {},
      body: new El('body'),
      documentElement: new El('html'),
      hidden: false,
    },
    localStorage: { _m: new Map(), getItem(k) { return this._m.has(k) ? this._m.get(k) : null; }, setItem(k, v) { this._m.set(k, String(v)); }, removeItem(k) { this._m.delete(k); } },
    fetch: async url => {
      const p = String(url).split('?')[0];
      const body = data[p] === undefined ? {} : data[p];
      const t = JSON.stringify(body);
      return { ok: true, status: 200, text: async () => t, json: async () => JSON.parse(t) };
    },
    innerWidth: 1920, innerHeight: 1080,
    devicePixelRatio: 1,
    location: { href: 'http://127.0.0.1:18790/', reload() {}, search: '' },
    navigator: { userAgent: 'Mozilla/5.0 (Windows NT 10.0) Edge/126' },
    EventSource: class { constructor() {} addEventListener() {} close() {} },
    Image: class {},
    URL, Blob: class {},
    alert: () => {}, confirm: () => false,
    setInterval, clearInterval, setTimeout, clearTimeout,
    requestAnimationFrame: fn => setTimeout(() => fn(Date.now()), 0),
    getComputedStyle: () => ({ getPropertyValue: () => '' }),
    addEventListener: () => {},
    matchMedia: () => ({ matches: false, addEventListener: () => {} }),
  };
  sandbox.window = sandbox;
  sandbox.globalThis = sandbox;
  vm.createContext(sandbox);
  // The core regression assertion: evaluating the page's scripts
  // must not throw. (v0.54 threw a TDZ ReferenceError out of the
  // Dock builder loop in block 1.)
  for (let i = 0; i < blocks.length; i++) {
    vm.runInContext(blocks[i], sandbox, { filename: 'block' + (i + 1) + '.js' });
  }
  await new Promise(r => setTimeout(r, 400));
  const probe = vm.runInContext(`(function(){
    const out = {};
    out.deskorderLen = DESKORDER.length;
    out.iconKeys = iconsEl.children.map(c => c.dataset.key);
    out.lpLen = lpEntries().length;
    out.lpKeys = lpEntries().map(e => e.key);
    return out;
  })()`, sandbox);
  console.log('[' + label + '] DESKORDER=' + probe.deskorderLen +
    ' icons=' + probe.iconKeys.length + ' lp=' + probe.lpLen +
    (errors.length ? ' (contained errors logged: ' + errors.length + ')' : ''));
  return probe;
}

(async () => {
  // 1) Normal machine: everything paints, Firefox included.
  const p1 = await bootScenario('normal');
  assert(p1.deskorderLen >= 20, 'DESKORDER populated, got ' + p1.deskorderLen);
  assert(p1.iconKeys.includes('app:firefox'), 'desktop shows the Firefox icon (the v0.54 TDZ victim)');
  assert(p1.iconKeys.includes('app:browser'), 'desktop shows built-in apps');
  assert(p1.iconKeys.includes('store:telegram'), 'desktop shows installed store apps');
  assert(p1.iconKeys.includes('box:wechat'), 'desktop shows boxed apps');
  assert(p1.iconKeys.includes('prod:imageglass'), 'desktop shows components');
  assert(p1.iconKeys.includes('web:https://github.com'), 'desktop shows web apps outside folders');
  assert(p1.iconKeys.includes('file:歌.mp3'), 'desktop shows imported files');
  assert(p1.lpLen >= 15, 'Launchpad entries populated, got ' + p1.lpLen);
  assert(p1.lpKeys.includes('app:firefox'), 'Launchpad shows Firefox');

  // 2) Firefox mid-download with an error phase: FFSTATE-driven
  //    icon code must still not break the lists.
  const p2 = await bootScenario('ff-error', d => {
    d['/api/firefox'] = { installed: false, version: '', phase: 'error', progress: 0, size: 90000000, message: 'x' };
  });
  assert(p2.iconKeys.includes('app:firefox'), 'desktop still paints with Firefox in error state');
  assert(p2.lpLen >= 15, 'Launchpad still paints with Firefox in error state');

  // 3) Sabotage: one component record is malformed (name: null) so
  //    its icon builder throws. v0.55 containment: that entry is
  //    skipped on the desktop / degraded in the Launchpad, and
  //    everything else renders.
  const p3 = await bootScenario('sabotage', d => {
    d['/api/components'] = { components: [
      { id: 'broken', name: null, installed: false, phase: 'idle', progress: 0, size: 100 },
      { id: 'imageglass', name: 'ImageGlass', installed: true, phase: 'idle', progress: 0, size: 43934433 },
    ] };
  });
  assert(!p3.iconKeys.includes('prod:broken'), 'malformed component skipped on desktop');
  assert(p3.iconKeys.includes('prod:imageglass'), 'healthy component still on desktop');
  assert(p3.iconKeys.includes('app:firefox'), 'built-ins still on desktop after sabotage');
  assert(p3.lpKeys.includes('prod:imageglass'), 'healthy component still in Launchpad');
  assert(p3.lpLen >= 15, 'Launchpad survives sabotage, got ' + p3.lpLen);

  console.log('deskboot_test: all assertions passed');
  process.exit(0);
})().catch(e => { console.error('deskboot_test FAILED:', e && e.stack || e); process.exit(1); });
