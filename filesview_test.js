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
const { FV_GROUPS, fvGroupOf, fvInGroup, fvSortList, fvVisible, fvToggleSel, fvRangeSel, FV_RECENT_CAP, fvMediaGroup, fvExtOf, WPFIT_MODES, WPFIT_BASE, wpFitValid, wpFitKey, wpFitCSS, wpFitShowsWallItem, PDFZOOM_MODES, pdfZoomValid, pdfZoomKey, pdfResolveScale, pdfPageAtScroll, pdfRenderWindow, pdfFindMatches, pdfFindStep, pdfRotateNext, pdfWheelZoom, IMGZOOM_MODES, imgZoomValid, imgZoomKey, IMG_SCALE_MIN, IMG_SCALE_MAX, imgEffSize, imgFitScale, imgResolveScale, imgWheelZoom, imgStepScale, imgAnchorPos, imgClampPan, imgRotateNext, imgStepIndex, imgDblTarget } = new Function(
  m[1] + '\nreturn { FV_GROUPS, fvGroupOf, fvInGroup, fvSortList, fvVisible, fvToggleSel, fvRangeSel, FV_RECENT_CAP, fvMediaGroup, fvExtOf, WPFIT_MODES, WPFIT_BASE, wpFitValid, wpFitKey, wpFitCSS, wpFitShowsWallItem, PDFZOOM_MODES, pdfZoomValid, pdfZoomKey, pdfResolveScale, pdfPageAtScroll, pdfRenderWindow, pdfFindMatches, pdfFindStep, pdfRotateNext, pdfWheelZoom, IMGZOOM_MODES, imgZoomValid, imgZoomKey, IMG_SCALE_MIN, IMG_SCALE_MAX, imgEffSize, imgFitScale, imgResolveScale, imgWheelZoom, imgStepScale, imgAnchorPos, imgClampPan, imgRotateNext, imgStepIndex, imgDblTarget };'
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

// --- v0.63: wallpaper fit modes (the 设为壁纸 dialog, the
// desktop application and 设置 → 外观 share this mapping) ---
assert.deepStrictEqual(WPFIT_MODES.map(m => m.id), ['fill', 'fit', 'center', 'stretch'], 'four fit modes in macOS order');
assert.deepStrictEqual(WPFIT_MODES.map(m => m.n), ['填充', '适合', '居中', '拉伸'], 'fit mode labels');
for (const id of ['fill', 'fit', 'center', 'stretch']) assert(wpFitValid(id), id + ' is a valid fit');
for (const bad of ['', 'cover', 'contain', 'FILL', null, undefined]) assert(!wpFitValid(bad), String(bad) + ' is not a valid fit');
assert.strictEqual(wpFitKey('fit'), 'fit', 'a valid fit passes through');
assert.strictEqual(wpFitKey('bogus'), 'fill', 'an unknown fit normalizes to fill');
assert.strictEqual(wpFitKey(null), 'fill', 'a missing fit normalizes to fill');
assert.strictEqual(wpFitCSS('fill', '/api/wallpaper?v=1'), WPFIT_BASE + ' center/cover no-repeat url("/api/wallpaper?v=1")', 'fill = cover');
assert.strictEqual(wpFitCSS('fit', '/api/wallpaper?v=1'), WPFIT_BASE + ' center/contain no-repeat url("/api/wallpaper?v=1")', 'fit = contain over the base tone');
assert.strictEqual(wpFitCSS('center', '/api/wallpaper?v=1'), WPFIT_BASE + ' center/auto no-repeat url("/api/wallpaper?v=1")', 'center = natural size');
assert.strictEqual(wpFitCSS('stretch', '/api/wallpaper?v=1'), WPFIT_BASE + ' center/100% 100% no-repeat url("/api/wallpaper?v=1")', 'stretch = exact frame');
assert.strictEqual(wpFitCSS('bogus', '/x'), wpFitCSS('fill', '/x'), 'an unknown mode renders as fill');
// the 设为壁纸 menu item exists exactly for image files
assert(wpFitShowsWallItem(F('a.png', 'image', 1, 1)), 'an image gets the menu item');
assert(!wpFitShowsWallItem(F('a.pdf', 'pdf', 1, 1)), 'a pdf does not');
assert(!wpFitShowsWallItem(F('d', 'dir', 0, 1)), 'a folder does not');
assert(!wpFitShowsWallItem(null) && !wpFitShowsWallItem(undefined), 'no file, no item');

// --- v0.65: PDF viewer pure logic （细节修复 ⑥) ---
assert.deepStrictEqual(PDFZOOM_MODES.map(m => m.id), ['fit-width', 'fit-page', 'actual'], 'pdf zoom modes mirror the daemon domain');
assert.deepStrictEqual(PDFZOOM_MODES.map(m => m.n), ['适宽', '适页', '实际大小'], 'pdf zoom labels');
for (const id of ['fit-width', 'fit-page', 'actual']) assert(pdfZoomValid(id), id + ' valid');
for (const bad of ['', 'percent', 'fill', 'FIT-WIDTH', null, undefined]) assert(!pdfZoomValid(bad), String(bad) + ' invalid');
assert.strictEqual(pdfZoomKey('fit-page'), 'fit-page');
assert.strictEqual(pdfZoomKey('bogus'), 'fit-width', 'unknown zoom normalizes to the default');
assert.strictEqual(pdfZoomKey(null), 'fit-width');
// scale resolution: A4-ish page 595x842 in an 800x600 viewport
assert.strictEqual(pdfResolveScale('fit-width', 100, 800, 600, 595, 842), 800 / 595, 'fit-width fills the width');
assert.strictEqual(pdfResolveScale('fit-page', 100, 800, 600, 595, 842), 600 / 842, 'fit-page is the min ratio');
assert.strictEqual(pdfResolveScale('actual', 250, 800, 600, 595, 842), 1, 'actual is always 100%');
assert.strictEqual(pdfResolveScale('percent', 150, 800, 600, 595, 842), 1.5, 'percent is explicit');
assert.strictEqual(pdfResolveScale('fit-width', 100, 0, 600, 595, 842), 1, 'no container yet -> no zero scale');
assert.strictEqual(pdfResolveScale('fit-width', 100, 800, 600, 0, 842), 1, 'a sizeless page -> no NaN scale');
assert.strictEqual(pdfResolveScale('percent', 100000, 800, 600, 595, 842), 10, 'scale is clamped at the top');
assert.strictEqual(pdfResolveScale('percent', 1, 800, 600, 595, 842), 0.05, 'scale is clamped at the bottom');
// scroll -> current page: three 100px pages, 14px gaps, tops as laid out
const tops = [14, 128, 242], heights = [100, 100, 100];
assert.strictEqual(pdfPageAtScroll(tops, heights, 0, 200), 0, 'top of the doc is page 1');
assert.strictEqual(pdfPageAtScroll(tops, heights, 60, 200), 1, 'centre in page 2');
assert.strictEqual(pdfPageAtScroll(tops, heights, 20, 20), 0, 'centre in the gap belongs to the page above');
assert.strictEqual(pdfPageAtScroll(tops, heights, 100000, 200), 2, 'past the end clamps to the last page');
assert.strictEqual(pdfPageAtScroll([], [], 0, 200), 0, 'no pages -> 0');
assert.strictEqual(pdfPageAtScroll(tops, heights, -50, 100), 0, 'negative scroll clamps to the first page');
// the lazy render window around the current page
assert.deepStrictEqual(pdfRenderWindow(0, 500, 3), [0, 1, 2, 3], 'window at the start');
assert.deepStrictEqual(pdfRenderWindow(250, 500, 3), [247, 248, 249, 250, 251, 252, 253], 'window in the middle');
assert.deepStrictEqual(pdfRenderWindow(499, 500, 3), [496, 497, 498, 499], 'window at the end');
assert.deepStrictEqual(pdfRenderWindow(0, 2, 3), [0, 1], 'window larger than the doc');
// find: case-insensitive, non-overlapping, [start,end) pairs
assert.deepStrictEqual(pdfFindMatches('Hello hello HELLO', 'hello'), [{ s: 0, e: 5 }, { s: 6, e: 11 }, { s: 12, e: 17 }], 'find is case-insensitive');
assert.deepStrictEqual(pdfFindMatches('aaaa', 'aa'), [{ s: 0, e: 2 }, { s: 2, e: 4 }], 'matches do not overlap');
assert.deepStrictEqual(pdfFindMatches('abc', ''), [], 'empty query finds nothing');
assert.deepStrictEqual(pdfFindMatches('abc', 'z'), [], 'no hits');
assert.deepStrictEqual(pdfFindMatches('', 'x'), [], 'empty text');
assert.deepStrictEqual(pdfFindMatches('铭杰的终端机，铭杰', '铭杰'), [{ s: 0, e: 2 }, { s: 7, e: 9 }], 'CJK find');
// find navigation with wrap
assert.strictEqual(pdfFindStep(-1, 5, 1), 0, 'from none, forward -> first');
assert.strictEqual(pdfFindStep(-1, 5, -1), 4, 'from none, backward -> last');
assert.strictEqual(pdfFindStep(4, 5, 1), 0, 'forward wraps');
assert.strictEqual(pdfFindStep(0, 5, -1), 4, 'backward wraps');
assert.strictEqual(pdfFindStep(2, 5, 1), 3);
assert.strictEqual(pdfFindStep(0, 0, 1), -1, 'no matches -> -1');
// rotation steps stay in the quarter-turn domain
assert.strictEqual(pdfRotateNext(0), 90);
assert.strictEqual(pdfRotateNext(270), 0);
assert.strictEqual(pdfRotateNext(360), 90);
assert.strictEqual(pdfRotateNext(-90), 0);
// ctrl+wheel zoom (v0.65-test2): the pure wheel decision seam
const approx = (a, b) => Math.abs(a - b) < 1e-9;
assert.deepStrictEqual(pdfWheelZoom(-100, false, 1), { zoom: false, scale: 1 }, 'plain wheel never zooms');
assert.deepStrictEqual(pdfWheelZoom(100, false, 2), { zoom: false, scale: 2 }, 'plain wheel down keeps the scale');
assert.deepStrictEqual(pdfWheelZoom(-100, undefined, 1), { zoom: false, scale: 1 }, 'no ctrl flag, no zoom');
{ const d = pdfWheelZoom(-100, true, 1); assert(d.zoom && approx(d.scale, Math.exp(0.1)), 'wheel up (deltaY<0) zooms in'); }
{ const d = pdfWheelZoom(100, true, 1); assert(d.zoom && approx(d.scale, Math.exp(-0.1)), 'wheel down (deltaY>0) zooms out'); }
{ const up = pdfWheelZoom(-100, true, 1), dn = pdfWheelZoom(100, true, up.scale); assert(approx(dn.scale, 1), 'in then out by one notch returns to the start'); }
{ let s = 1; for (let i = 0; i < 10; i++) s = pdfWheelZoom(-4, true, s).scale; assert(approx(s, pdfWheelZoom(-40, true, 1).scale), 'small trackpad deltas accumulate like one bigger delta'); }
{ const one = pdfWheelZoom(-4, true, 1); assert(one.zoom && one.scale > 1 && one.scale < 1.01, 'a tiny trackpad delta is a smooth small step, not a button jump'); }
assert.strictEqual(pdfWheelZoom(-100, true, 9.9).scale, 10, 'wheel zoom clamps at the top of the band');
assert.deepStrictEqual(pdfWheelZoom(-100, true, 10), { zoom: false, scale: 10 }, 'already at max: no zoom decision');
assert.strictEqual(pdfWheelZoom(100, true, 0.055).scale, 0.05, 'wheel zoom clamps at the bottom of the band');
assert.deepStrictEqual(pdfWheelZoom(100, true, 0.05), { zoom: false, scale: 0.05 }, 'already at min: no zoom decision');
assert.deepStrictEqual(pdfWheelZoom(0, true, 1.5), { zoom: false, scale: 1.5 }, 'a zero delta decides nothing');
assert.deepStrictEqual(pdfWheelZoom(NaN, true, 1.5), { zoom: false, scale: 1.5 }, 'a NaN delta decides nothing');
assert.deepStrictEqual(pdfWheelZoom(-100, true, 0), { zoom: true, scale: Math.exp(0.1) }, 'a degenerate current scale reads as 100%');

// --- image viewer (v0.66, detail-fix ⑦): zoom domain + open modes ---
assert(IMGZOOM_MODES.some(m => m.id === 'fit') && IMGZOOM_MODES.some(m => m.id === 'actual'), 'image zoom modes are fit + actual');
assert(imgZoomValid('fit') && imgZoomValid('actual') && !imgZoomValid('fill') && !imgZoomValid(''), 'imgZoom domain is exactly fit/actual');
assert.strictEqual(imgZoomKey('actual'), 'actual', 'a known imgZoom passes through');
assert.strictEqual(imgZoomKey('bogus'), 'fit', 'an unknown imgZoom normalizes to fit');
assert.strictEqual(imgZoomKey(null), 'fit', 'an unset imgZoom reads as fit');

// --- effective size: quarter turns swap the axes ---
assert.deepStrictEqual(imgEffSize(400, 300, 0), { w: 400, h: 300 }, 'no rotation keeps the box');
assert.deepStrictEqual(imgEffSize(400, 300, 90), { w: 300, h: 400 }, '90 degrees swaps the box');
assert.deepStrictEqual(imgEffSize(400, 300, 180), { w: 400, h: 300 }, '180 keeps the box');
assert.deepStrictEqual(imgEffSize(400, 300, 270), { w: 300, h: 400 }, '270 swaps the box');
assert.deepStrictEqual(imgEffSize(400, 300, -90), { w: 300, h: 400 }, 'a negative rotation normalizes');

// --- fit scale: whole image visible, never enlarged past 100% ---
assert(approx(imgFitScale(2000, 1000, 0, 500, 500), 0.25), 'a big image fits by its tighter axis');
assert.strictEqual(imgFitScale(100, 50, 0, 500, 500), 1, 'a small image fits at 100%, never enlarged');
assert(approx(imgFitScale(400, 300, 90, 300, 600), 1), 'fit uses the rotated box (300x400 in 300x600 is exactly 100%)');
assert.strictEqual(imgFitScale(0, 0, 0, 500, 500), 1, 'degenerate natural size falls back to 1');
assert.strictEqual(imgFitScale(100, 100, 0, 0, 0), 1, 'a zero container falls back to 1');

// --- resolve scale per mode ---
assert.strictEqual(imgResolveScale('actual', 3, 2000, 1000, 0, 500, 500), 1, 'actual is always 100%');
assert(approx(imgResolveScale('fit', 3, 2000, 1000, 0, 500, 500), 0.25), 'fit derives from the container, ignoring the custom scale');
assert.strictEqual(imgResolveScale('custom', 2.5, 2000, 1000, 0, 500, 500), 2.5, 'custom keeps the user scale');
assert.strictEqual(imgResolveScale('custom', 99, 2000, 1000, 0, 500, 500), IMG_SCALE_MAX, 'custom clamps at the top of the band');
assert.strictEqual(imgResolveScale('custom', 0.001, 2000, 1000, 0, 500, 500), IMG_SCALE_MIN, 'custom clamps at the bottom of the band');
assert.strictEqual(imgResolveScale('custom', NaN, 2000, 1000, 0, 500, 500), 1, 'a degenerate custom scale reads as 100%');

// --- wheel zoom: plain wheel zooms (no modifier gate), continuous, clamped ---
{ const d = imgWheelZoom(-100, 1); assert(d.zoom && approx(d.scale, Math.exp(0.1)), 'plain wheel up zooms in'); }
{ const d = imgWheelZoom(100, 1); assert(d.zoom && approx(d.scale, Math.exp(-0.1)), 'plain wheel down zooms out'); }
{ const up = imgWheelZoom(-100, 1), dn = imgWheelZoom(100, up.scale); assert(approx(dn.scale, 1), 'one notch in then out returns to the start'); }
{ let s = 1; for (let i = 0; i < 10; i++) s = imgWheelZoom(-4, s).scale; assert(approx(s, imgWheelZoom(-40, 1).scale), 'small trackpad deltas accumulate like one bigger delta'); }
assert.strictEqual(imgWheelZoom(-100, 15.9).scale, IMG_SCALE_MAX, 'wheel zoom clamps at the image band top (16x, wider than the PDF band)');
assert.deepStrictEqual(imgWheelZoom(-100, IMG_SCALE_MAX), { zoom: false, scale: IMG_SCALE_MAX }, 'already at max: no zoom decision');
assert.strictEqual(imgWheelZoom(100, 0.055).scale, IMG_SCALE_MIN, 'wheel zoom clamps at the bottom');
assert.deepStrictEqual(imgWheelZoom(0, 1.5), { zoom: false, scale: 1.5 }, 'a zero delta decides nothing');
assert.deepStrictEqual(imgWheelZoom(NaN, 1.5), { zoom: false, scale: 1.5 }, 'a NaN delta decides nothing');

// --- step zoom: fixed ratio, clamped ---
assert(approx(imgStepScale(1, 1).scale, 1.25), 'one step in is x1.25');
assert(approx(imgStepScale(1, -1).scale, 0.8), 'one step out is /1.25');
{ const a = imgStepScale(1, 1), b = imgStepScale(a.scale, -1); assert(approx(b.scale, 1), 'step in then out returns to the start'); }
assert.deepStrictEqual(imgStepScale(IMG_SCALE_MAX, 1), { zoom: false, scale: IMG_SCALE_MAX }, 'stepping at the top stays');
assert.deepStrictEqual(imgStepScale(IMG_SCALE_MIN, -1), { zoom: false, scale: IMG_SCALE_MIN }, 'stepping at the bottom stays');

// --- pointer anchoring: the content point under the cursor stays put ---
{
  const a = imgAnchorPos(300, 200, 100, 50, 400, 300, 800, 600);
  assert(approx(a.x, -100) && approx(a.y, -100), 'doubling the frame around the cursor keeps the pointed fraction');
  const fx = (300 - a.x) / 800, fy = (200 - a.y) / 600;
  assert(approx(fx, 0.5) && approx(fy, 0.5), 'the pointed fraction is identical before and after');
}
{
  const a = imgAnchorPos(100, 50, 100, 50, 400, 300, 800, 600);
  assert(approx(a.x, 100) && approx(a.y, 50), 'zooming at the frame corner keeps the corner fixed');
}
assert.deepStrictEqual(imgAnchorPos(10, 10, 0, 0, 0, 0, 100, 100), { x: 0, y: 0 }, 'a degenerate frame anchors nowhere');

// --- pan clamping: smaller axis centred, larger axis edge-clamped ---
assert.deepStrictEqual(imgClampPan(999, -999, 200, 100, 500, 400), { x: 150, y: 150 }, 'a frame smaller than the viewport is centred on both axes, wherever it was dragged');
assert.deepStrictEqual(imgClampPan(50, 50, 1000, 800, 500, 400), { x: 0, y: 0 }, 'an overflowing frame cannot slide past its top-left edge');
assert.deepStrictEqual(imgClampPan(-900, -900, 1000, 800, 500, 400), { x: -500, y: -400 }, 'an overflowing frame cannot slide past its bottom-right edge');
assert.deepStrictEqual(imgClampPan(-250, -200, 1000, 800, 500, 400), { x: -250, y: -200 }, 'a position inside the slide range passes through');
assert.deepStrictEqual(imgClampPan(-250, 999, 1000, 100, 500, 400), { x: -250, y: 150 }, 'axes clamp independently (x slides, y centres)');

// --- rotation composition: fit after a quarter turn uses the swapped box ---
assert.strictEqual(imgRotateNext(0), 90); assert.strictEqual(imgRotateNext(90), 180);
assert.strictEqual(imgRotateNext(270), 0, 'rotation wraps to 0');
assert.strictEqual(imgRotateNext(-90), 0, 'a negative rotation normalizes before stepping');
{ let r = 0; for (let i = 0; i < 4; i++) r = imgRotateNext(r); assert.strictEqual(r, 0, 'four quarter turns come home'); }
assert(approx(imgResolveScale('fit', 1, 400, 300, 90, 150, 150), 0.375), 'fit of the rotated 300x400 box into 150x150 is 0.375');

// --- navigation: wrap-around stepping, nowhere on an unknown current ---
assert.strictEqual(imgStepIndex(0, 3, 1), 1, 'next from the first');
assert.strictEqual(imgStepIndex(2, 3, 1), 0, 'next wraps last -> first');
assert.strictEqual(imgStepIndex(0, 3, -1), 2, 'prev wraps first -> last');
assert.strictEqual(imgStepIndex(-1, 3, 1), -1, 'an unknown current navigates nowhere');
assert.strictEqual(imgStepIndex(0, 0, 1), -1, 'an empty list navigates nowhere');
assert.strictEqual(imgStepIndex(5, 3, 1), -1, 'an out-of-range current navigates nowhere');

// --- double-click toggle ---
assert.strictEqual(imgDblTarget('fit'), 'actual', 'double-click at fit goes to actual size');
assert.strictEqual(imgDblTarget('custom'), 'fit', 'double-click zoomed goes back to fit');
assert.strictEqual(imgDblTarget('actual'), 'fit', 'double-click at actual goes back to fit');

console.log('filesview_test: all assertions passed');
