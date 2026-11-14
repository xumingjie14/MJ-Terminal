/* v0.56 feature-logic regression test: boots the REAL index.html
   script blocks under the deskboot fake DOM and drives the new
   v0.56 frontend logic --
     * folder membership migration (legacy bare URLs -> web: keys)
       and kind-agnostic membership (a store app inside a folder
       leaves the desktop + Launchpad top level; entryMeta still
       resolves it for the folder grid);
     * cleanWinTitle suffix stripping (Stage Manager identity);
     * deskPitch() tracking the deskIconSize setting;
     * sortedDeskOrder in its four modes + renderDesk applying
       the active sort to DESKORDER.
   Run: node desk56_test.js */
'use strict';
const fs = require('fs');
const vm = require('vm');
const assert = require('assert');
const path = require('path');

const html = fs.readFileSync(process.env.MJ_TEST_HTML || path.join(__dirname, 'index.html'), 'utf8');
const blocks = [...html.matchAll(/<script>([\s\S]*?)<\/script>/g)].map(m => m[1]);
assert.strictEqual(blocks.length, 3, 'expected 3 script blocks in index.html');

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
    /* f1 mixes the two membership eras: a legacy bare URL and a
       v0.56 canonical key for a STORE app. */
    '/api/layout': { pos: {}, folders: [{ id: 'f1', name: '工具', items: ['https://x.com', 'store:telegram'] }] },
    '/api/apps': { apps: [
      { name: 'YouTube', url: 'https://www.youtube.com' },
      { name: 'X', url: 'https://x.com' },
      { name: 'GitHub', url: 'https://github.com' },
    ] },
    '/api/apps/catalog': { apps: [
      { id: 'telegram', name: 'Telegram', icon: '', homepage: 'https://telegram.org/', installed: true },
      { id: 'vlc', name: 'VLC', icon: '', homepage: 'https://www.videolan.org/', installed: true },
    ] },
    '/api/sandbox/apps': { apps: [
      { id: 'wechat', name: '微信', exe: 'C:\\WeChat\\WeChat.exe', present: true, sealed: true },
    ] },
    '/api/components': { components: [
      { id: 'imageglass', name: 'ImageGlass', installed: true, phase: 'idle', progress: 0, size: 43934433 },
    ] },
    '/api/firefox': { installed: true, version: '153.5.1', phase: 'idle', progress: 0, size: 0, message: '' },
    '/api/settings': {},
    '/api/files': { files: [
      { name: '歌.mp3', path: '歌.mp3', kind: 'other', ext: 'mp3', size: 2048, mtime: 1761000100 },
    ] },
    '/api/system': { version: '0.56', os: 'windows', arch: 'amd64', hostname: 'PC', numCPU: 8, exeDir: 'C:\\MJ', dataDir: 'C:\\MJ\\Data', dataFallback: false },
  };
}

(async () => {
  const data = baseData();
  const byId = new Map();
  const errors = [];
  const sandbox = {
    console: { log: () => {}, warn: (...a) => errors.push(a.join(' ')), error: (...a) => errors.push(a.join(' ')) },
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
  for (let i = 0; i < blocks.length; i++) {
    vm.runInContext(blocks[i], sandbox, { filename: 'block' + (i + 1) + '.js' });
  }
  await new Promise(r => setTimeout(r, 400));

  const probe = vm.runInContext(`(function(){
    const out = {};
    const J = x => JSON.parse(JSON.stringify(x));
    const f = LAYOUT.folders[0];
    out.migrated = J(folderItems(f).slice());
    out.iconKeys = J(iconsEl.children.map(c => c.dataset.key));
    out.lpKeys = J(lpEntries().map(e => e.key));
    out.metaStore = (entryMeta('store:telegram') || {}).name || null;
    out.metaWeb = (entryMeta('web:https://x.com') || {}).name || null;
    out.metaFile = (entryMeta('file:歌.mp3') || {}).name || null;
    out.metaBad = entryMeta('store:nope');
    out.titles = J([cleanWinTitle('ChatGPT — Mozilla Firefox'), cleanWinTitle('Google - Google Chrome'), cleanWinTitle('普通标题'), cleanWinTitle('')]);
    SETTINGS.deskIconSize = 80;
    out.pitch80 = deskPitch();
    SETTINGS.deskIconSize = 9999;
    out.pitchBad = deskPitch();
    SETTINGS.deskIconSize = 54;
    const keys = ['web:https://github.com', 'app:term', 'store:vlc'];
    LAYOUT.opens = { 'store:vlc': 7, 'app:term': 1 };
    LAYOUT.added = { 'web:https://github.com': 300, 'app:term': 100, 'store:vlc': 200 };
    SETTINGS.deskSort = 'freq';
    out.sortFreq = J(sortedDeskOrder(keys));
    SETTINGS.deskSort = 'kind';
    out.sortKind = J(sortedDeskOrder(keys));
    SETTINGS.deskSort = 'added';
    out.sortAdded = J(sortedDeskOrder(keys));
    SETTINGS.deskSort = 'manual';
    out.sortManual = J(sortedDeskOrder(keys));
    /* renderDesk applies the active sort to DESKORDER itself */
    LAYOUT.opens = { 'app:sys': 42 };
    SETTINGS.deskSort = 'freq';
    renderDesk();
    out.firstAfterSort = DESKORDER[0];
    SETTINGS.deskSort = 'manual';
    return out;
  })()`, sandbox);

  assert.strictEqual(probe.migrated.join('|'), 'web:https://x.com|store:telegram', 'legacy URL migrates to web: key, canonical key passes through');
  assert(!probe.iconKeys.includes('store:telegram'), 'foldered store app leaves the desktop top level');
  assert(!probe.iconKeys.includes('web:https://x.com'), 'foldered web app leaves the desktop top level');
  assert(probe.iconKeys.includes('folder:f1'), 'the folder itself is on the desktop');
  assert(probe.iconKeys.includes('store:vlc'), 'unfoldered store app stays on the desktop');
  assert(!probe.lpKeys.includes('store:telegram'), 'foldered store app leaves the Launchpad top level');
  assert(probe.lpKeys.includes('folder:f1'), 'folder present in Launchpad');
  assert.strictEqual(probe.metaStore, 'Telegram', 'entryMeta resolves a foldered store app');
  assert.strictEqual(probe.metaWeb, 'X', 'entryMeta resolves a foldered web app');
  assert.strictEqual(probe.metaFile, '歌.mp3', 'entryMeta resolves an imported file');
  assert.strictEqual(probe.metaBad, null, 'entryMeta returns null for unknown keys');
  assert.strictEqual(probe.titles.join('|'), 'ChatGPT|Google|普通标题|', 'cleanWinTitle strips browser suffixes only');
  assert.strictEqual(probe.pitch80, 130, 'deskPitch = icon size + 50');
  assert.strictEqual(probe.pitchBad, 104, 'deskPitch falls back to the standard size on garbage');
  assert.strictEqual(probe.sortFreq[0], 'store:vlc', 'freq sort puts the most-opened first');
  assert.strictEqual(probe.sortKind[0], 'app:term', 'kind sort puts system apps first');
  assert.strictEqual(probe.sortAdded.join('|'), 'app:term|store:vlc|web:https://github.com', 'added sort is oldest-first');
  assert.strictEqual(probe.sortManual.join('|'), 'web:https://github.com|app:term|store:vlc', 'manual sort is a pass-through');
  assert.strictEqual(probe.firstAfterSort, 'app:sys', 'renderDesk applies the freq sort to DESKORDER');

  /* ----- v0.56 test2 additions -----
     * the unified GLYPHS set renders (one stroke language,
       currentColor) and ctxItemHTML paints item icons;
     * Stage preview cards use the FETCH loader (no <img src>
       at the thumb endpoint -- the test1 first-404 blacklist
       design is gone) and thumbBad windows fall to identity;
     * the web-app item menu carries 重命名 (item 6) next to
       调整窗口; browser/app tiles reference the right icon
       routes (real Chrome icon / self-drawn set). */
  const probe2 = vm.runInContext(`(function(){
    const out = {};
    out.glyphNames = ['folderplus','import','switch','minimize','closex','lock','rocket','gear','stage','wifi','battery','rename','trash','window'].filter(n => !GLYPHS[n]);
    const g = glyph('lock', 16);
    out.glyphSVG = g.includes('<svg') && g.includes('currentColor') && g.includes('viewBox="0 0 24 24"');
    out.ctxIcon = ctxItemHTML({act:'x', label:'打开', icon:'open'}).includes('ctxic');
    out.ctxNoIcon = !ctxItemHTML({act:'x', label:'打开'}).includes('ctxic');
    const win1 = {hwnd: 111, title: 'Chat — X', app: 'Chrome', ikind: 'chrome', url: 'https://x.com'};
    out.prevCard = extCardHTML(win1, true);
    thumbBad.add('h:222'); /* v0.71 test3: thumbBad keys are card keys now */
    out.badCard = extCardHTML({hwnd: 222, title: 'T', app: 'A', ikind: ''}, true);
    WEBAPPS = [{name: '旧名', url: 'https://x.com'}];
    showItemCtx(10, 10, 'web:https://x.com');
    out.webMenu = ctxEl.innerHTML;
    out.browserIcon = appIcon(APPS.find(a => a.id === 'browser'));
    out.homeIcon = appIcon(APPS.find(a => a.id === 'home'));
    out.imgviewIcon = appIcon(APPS.find(a => a.id === 'imgview'));
    out.cc = ccTile('stage', true, 'stage', '台前调度', '开');
    return out;
  })()`, sandbox);

  assert.strictEqual(probe2.glyphNames.join('|'), '', 'every chrome glyph name the menus/CC/menubar use exists in GLYPHS');
  assert(probe2.glyphSVG, 'glyph() emits a currentColor 24-grid svg');
  assert(probe2.ctxIcon && probe2.ctxNoIcon, 'ctxItemHTML renders the icon span only when the item carries one');
  assert(probe2.prevCard.includes('class="ethumb"'), 'preview card renders the (src-less) thumb img the fetch loader fills');
  assert(!probe2.prevCard.includes('/api/extwin/thumb?hwnd='), 'preview card must NOT hard-code the thumb URL as img src (test1 regression)');
  assert(!probe2.badCard.includes('ethumb'), 'a thumbBad window renders the identity card, no preview attempt');
  assert(probe2.webMenu.includes('重命名'), 'web-app item menu carries 重命名 (test2 item 6)');
  assert(probe2.webMenu.includes('调整窗口'), 'web-app item menu keeps 调整窗口');
  assert(probe2.browserIcon.includes('/api/chrome/icon'), 'browser tile uses Chrome real icon route');
  assert(probe2.homeIcon.includes('/api/appicon/home'), 'built-in tile uses the embedded self-drawn set route');
  assert(probe2.imgviewIcon.includes('<svg'), 'viewer apps render drawn glyphs, not emoji');
  assert(probe2.cc.includes('<svg'), 'Control Center tiles render drawn glyphs');

  /* ----- v0.62 additions (detail-fix trio) -----
     * ③ the Stage card right-click menu: three windows of ONE
       Chrome process render three independent cards; a card's
       contextmenu opens the per-window menu (the v0.56 build
       passed ctxOpen's arguments swapped, so it never opened)
       carrying the new 隐藏其他窗口 item; the act -> endpoint
       mapping posts to /api/extwin/focusonly.
     * ① the frontend half: a card click posts its OWN hwnd to
       /api/extwin/focus.
     * ⑤ opening a context menu reports the 'ctx' overlay
       session (and hideCtx closes it) to /api/view/overlay. */
  const probe3 = await vm.runInContext(`(async function(){
    const out = { posts: [] };
    const origFetch = fetch;
    const wins = { windows: [
      { hwnd: 101, title: '文档一', app: 'Chrome', ikind: 'chrome' },
      { hwnd: 102, title: '文档二', app: 'Chrome', ikind: 'chrome' },
      { hwnd: 103, title: '文档三', app: 'Chrome', ikind: 'chrome' },
    ] };
    fetch = async (url, opts) => {
      const p = String(url).split('?')[0];
      if (p === '/api/extwin' && !(opts && opts.method === 'POST')) {
        const t = JSON.stringify(wins);
        return { ok: true, status: 200, text: async () => t, json: async () => JSON.parse(t) };
      }
      if (opts && opts.method === 'POST') out.posts.push(p + ' ' + (opts.body || ''));
      return origFetch(url, opts);
    };
    hideCtx(); /* probe2 left a ctx session open; stageOverlay is transition-only, so close it before measuring */
    SETTINGS.stageManager = true;
    SETTINGS.stageDisplay = 'identity';
    await refreshExtStrip();
    const strip = document.getElementById('extstrip');
    out.stripChildren = strip.children.length;
    const card102 = strip.children.find(c => String(c.dataset.hwnd) === '102');
    const card101 = strip.children.find(c => String(c.dataset.hwnd) === '101');
    out.hasCtxHandler = !!(card102 && typeof card102.oncontextmenu === 'function');
    if (card102) card102.oncontextmenu({ preventDefault(){}, stopPropagation(){}, clientX: 10, clientY: 10 });
    out.menu = ctxEl.innerHTML;
    out.epFocus = extCtxEndpoint('focus');
    out.epFocusOnly = extCtxEndpoint('focusonly');
    out.epMin = extCtxEndpoint('min');
    out.epClose = extCtxEndpoint('close');
    if (card101) { await card101.onclick(); }
    hideCtx();
    out.postsJoined = out.posts.join('|');
    fetch = origFetch;
    SETTINGS.stageManager = false;
    await refreshExtStrip();
    return out;
  })()`, sandbox);

  assert.strictEqual(probe3.stripChildren, 4, 'three windows of one process render a label + three independent cards');
  assert(probe3.hasCtxHandler, 'a Stage card carries a contextmenu handler');
  assert(probe3.menu.includes('切换到该窗口'), 'card menu carries 切换到该窗口');
  assert(probe3.menu.includes('隐藏其他窗口'), 'card menu carries the new 隐藏其他窗口 (v0.62 item ③)');
  assert(probe3.menu.includes('data-act="focusonly"'), 'card menu wires 隐藏其他窗口 to the focusonly act');
  assert(probe3.menu.includes('最小化') && probe3.menu.includes('关闭窗口'), 'card menu keeps 最小化 / 关闭窗口');
  assert.strictEqual(probe3.epFocusOnly, 'focusonly', 'focusonly act maps to the focusonly endpoint');
  assert.strictEqual(probe3.epFocus, 'focus', 'focus act maps to the focus endpoint');
  assert.strictEqual(probe3.epMin, 'minimize', 'min act maps to the minimize endpoint');
  assert.strictEqual(probe3.epClose, 'close', 'close act maps to the close endpoint');
  assert(probe3.postsJoined.includes('/api/extwin/focus {"hwnd":101}'), 'clicking card 101 posts its own hwnd to /api/extwin/focus');
  assert(probe3.postsJoined.includes('/api/view/overlay') && probe3.postsJoined.includes('"kind":"ctx"') && probe3.postsJoined.includes('"on":true'), 'opening the card menu reports the ctx overlay session (⑤)');
  assert(probe3.postsJoined.includes('"on":false'), 'hideCtx reports the ctx overlay close (⑤)');

  /* ----- v0.71 test3 additions (layer 1, L1-B2 / L2-I4) -----
     * A locked app's Stage card outlives its window: when the
     * window dies, the daemon lists the app windowless and the
     * SAME DOM node crosses to the 'u:' key showing 恢复中；
     * when the window is reborn under a new handle the same
     * node crosses back. A windowless card's click posts to
     * /api/webapps/restore instead of focusing a dead handle,
     * its menu offers 立即恢复 / 取消锁住后台 only, and a locked
     * window's own menu says 隐藏到后台， never 关闭窗口. */
  const probe4 = await vm.runInContext(`(async function(){
    const out = { posts: [] };
    const origFetch = fetch;
    const U = 'https://tg.example.com/';
    let stage = 0;
    const frames = [
      { windows: [ { hwnd: 201, title: 'TG', app: 'Telegram', ikind: 'site', url: U, locked: true, lockurl: U, resident: 'front' } ] },
      { windows: [ { hwnd: 0, title: '', app: 'Telegram', url: U, locked: true, lockurl: U, resident: 'restoring' } ] },
      { windows: [ { hwnd: 202, title: 'TG', app: 'Telegram', ikind: 'site', url: U, locked: true, lockurl: U, resident: 'front' } ] },
    ];
    fetch = async (url, opts) => {
      const p = String(url).split('?')[0];
      if (p === '/api/extwin' && !(opts && opts.method === 'POST')) {
        const t = JSON.stringify(frames[stage]);
        return { ok: true, status: 200, text: async () => t, json: async () => JSON.parse(t) };
      }
      if (opts && opts.method === 'POST') { out.posts.push(p + ' ' + (opts.body || '')); return { ok: true, status: 200, text: async () => '{}', json: async () => ({}) }; }
      return origFetch(url, opts);
    };
    hideCtx();
    SETTINGS.stageManager = true;
    SETTINGS.stageDisplay = 'identity';
    await refreshExtStrip();
    const strip = document.getElementById('extstrip');
    out.children1 = strip.children.length;
    const before = strip.children.find(c => String(c.dataset.hwnd) === '201');
    out.key1 = before && before.dataset.key;
    if (before) { before.oncontextmenu({ preventDefault(){}, stopPropagation(){}, clientX: 10, clientY: 10 }); out.lockedMenu = ctxEl.innerHTML; hideCtx(); }
    stage = 1;
    await refreshExtStrip();
    out.children2 = strip.children.length;
    const ghost = strip.children.find(c => c.dataset.key === 'u:' + U);
    out.sameNodeGhost = !!ghost && ghost === before;
    out.ghostHwnd = ghost && ghost.dataset.hwnd;
    out.ghostText = ghost && ghost.innerHTML; /* the fake DOM's textContent is a plain '' property; innerHTML is the readable surface */
    if (ghost) { ghost.oncontextmenu({ preventDefault(){}, stopPropagation(){}, clientX: 10, clientY: 10 }); out.ghostMenu = ctxEl.innerHTML; hideCtx(); await ghost.onclick(); }
    stage = 2;
    await refreshExtStrip();
    const back = strip.children.find(c => String(c.dataset.hwnd) === '202');
    out.sameNodeBack = !!back && back === before;
    out.keyBack = back && back.dataset.key;
    fetch = origFetch;
    SETTINGS.stageManager = false;
    await refreshExtStrip();
    out.postsJoined = out.posts.join('|');
    return out;
  })()`, sandbox);

  assert.strictEqual(probe4.children1, 2, 'one locked window renders a label + one card');
  assert.strictEqual(probe4.key1, 'h:201', 'a windowed card is keyed by its handle');
  assert(probe4.lockedMenu && probe4.lockedMenu.includes('隐藏到后台') && !probe4.lockedMenu.includes('关闭窗口'), 'a locked window menu offers 隐藏到后台 and never 关闭窗口 (L2-I4)');
  assert.strictEqual(probe4.children2, 2, 'the windowless app keeps exactly one card (no vanish, no duplicate)');
  assert(probe4.sameNodeGhost, 'the SAME DOM node becomes the windowless card when the window dies (L1-B2)');
  assert(probe4.ghostText && probe4.ghostText.includes('恢复中'), 'the windowless card shows 恢复中');
  assert.strictEqual(probe4.ghostHwnd, undefined, 'the windowless card carries no stale handle');
  assert(probe4.ghostMenu && probe4.ghostMenu.includes('立即恢复') && probe4.ghostMenu.includes('取消锁住后台'), 'the windowless menu offers 立即恢复 / 取消锁住后台 only');
  assert(probe4.postsJoined.includes('/api/webapps/restore'), 'clicking the windowless card posts to /api/webapps/restore');
  assert(probe4.sameNodeBack, 'the SAME DOM node becomes the window card again when the window is reborn (L1-B2)');
  assert.strictEqual(probe4.keyBack, 'h:202', 'the reborn card is keyed by the new handle');

  console.log('desk56_test: all assertions passed');
  process.exit(0);
})().catch(e => { console.error('desk56_test FAILED:', e && e.stack || e); process.exit(1); });
