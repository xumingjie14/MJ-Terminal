/* Pure functions (node-testable): this file is NOT bundled. The
   build transforms it on its own and inlines the result between
   the "fv pure" marker comments in the page, where these
   declarations become page globals shared by every bundle -- the
   same arrangement the pre-modular page had. Keep it free of
   imports, exports and outside references: the node suites lift
   this code out of the built page and eval it standalone. */
/* The Finder's smart groups over the Data/files library. kind
   comes from the daemon's fileKindOf; the library is a single
   flat layer, so groups are by TYPE, not by folder.
   v0.54: 音乐/视频 groups classify by EXTENSION (the daemon's
   fileKindOf leaves media at 'other'); media still flows into
   全部/最近 like everything else. */
const FV_GROUPS=[
 {id:'all',n:'全部文件'},{id:'recent',n:'最近导入'},{id:'image',n:'图片'},
 {id:'music',n:'音乐'},{id:'video',n:'视频'},
 {id:'doc',n:'文档'},{id:'sheet',n:'表格'},{id:'zip',n:'压缩包'},{id:'other',n:'其他'}
];
const FV_AUDIO_EXTS=['mp3','flac','wav','m4a','aac','ogg','opus','aiff','aif','ape','wma','mid','midi'];
const FV_VIDEO_EXTS=['mp4','mkv','webm','mov','avi','m4v','wmv','flv','mpg','mpeg','3gp','ts','m2ts'];
function fvExtOf(f){
 if(f&&f.ext) return String(f.ext).toLowerCase();
 const n=(f&&f.name)||''; const i=n.lastIndexOf('.');
 return i>0?n.slice(i+1).toLowerCase():'';
}
function fvMediaGroup(f){
 const e=fvExtOf(f);
 if(FV_AUDIO_EXTS.indexOf(e)>=0) return 'music';
 if(FV_VIDEO_EXTS.indexOf(e)>=0) return 'video';
 return '';
}
function fvGroupOf(f){
 const mg=fvMediaGroup(f);
 if(mg) return mg;
 switch(f&&f.kind){
  case 'image': return 'image';
  case 'pdf': case 'text': case 'docx': case 'pptx': return 'doc';
  case 'csv': case 'xlsx': return 'sheet';
  case 'zip': return 'zip';
  default: return 'other'; // exe, dir, other
 }
}
function fvInGroup(f,gid){
 if(gid==='all'||gid==='recent') return true;
 return fvGroupOf(f)===gid;
}
function fvSortList(list,key,dir){
 const d=(dir<0)?-1:1;
 const grpRank=f=>FV_GROUPS.findIndex(g=>g.id===fvGroupOf(f));
 const byName=(a,b)=>a.name.toLowerCase().localeCompare(b.name.toLowerCase());
 const cmp={
  name:byName,
  size:(a,b)=>((a.size||0)-(b.size||0))||byName(a,b),
  kind:(a,b)=>(grpRank(a)-grpRank(b))||byName(a,b),
  mtime:(a,b)=>((a.mtime||0)-(b.mtime||0))||byName(a,b)
 }[key]||byName;
 return (list||[]).slice().sort((a,b)=>d*cmp(a,b));
}
/* The visible list for a group + in-window search + sort.
   'recent' is the newest FV_RECENT_CAP by mtime across the
   whole library (search still applies inside it). */
const FV_RECENT_CAP=30;
function fvVisible(files,gid,q,sortKey,sortDir){
 let list=(files||[]).filter(f=>fvInGroup(f,gid));
 if(gid==='recent'){
  list=list.slice().sort((a,b)=>((b.mtime||0)-(a.mtime||0))||a.name.toLowerCase().localeCompare(b.name.toLowerCase())).slice(0,FV_RECENT_CAP);
 }
 const s=(q||'').trim().toLowerCase();
 if(s) list=list.filter(f=>f.name.toLowerCase().indexOf(s)>=0);
 if(gid==='recent'&&sortKey==='mtime') return list; // already mtime-desc
 return fvSortList(list,sortKey,sortDir);
}
/* Selection helpers: toggle one path in/out of a selection
   array; the inclusive range between two paths in the visible
   order (shift-click). Both pure, both return new arrays. */
function fvToggleSel(sel,path){
 const s=new Set(sel||[]);
 if(s.has(path)) s.delete(path); else s.add(path);
 return Array.from(s);
}
function fvRangeSel(paths,anchor,to){
 const i=(paths||[]).indexOf(anchor), j=(paths||[]).indexOf(to);
 if(i<0||j<0) return [to];
 const a=Math.min(i,j), b=Math.max(i,j);
 return paths.slice(a,b+1);
}
/* v0.63 （细节修复 ②): the custom wallpaper's fit modes. One
   mapping shared by the desktop application (applyWallpaper in
   the menubar module), the 设为壁纸 dialog's preview (the same
   CSS against a desktop-shaped frame, so the preview is the
   applied rendering at model scale) and the Settings page.
   The daemon persists the mode as the settings key
   wallpaperFit and validates it against the same four ids.
   Bands a non-covering mode leaves show WPFIT_BASE -- the
   desktop's base tone (the graphite preset's deep tone). */
const WPFIT_MODES=[
 {id:'fill',n:'填充'},{id:'fit',n:'适合'},
 {id:'center',n:'居中'},{id:'stretch',n:'拉伸'},
];
const WPFIT_BASE='#101014';
function wpFitValid(v){ return WPFIT_MODES.some(m=>m.id===v); }
function wpFitKey(v){ return wpFitValid(v)?v:'fill'; }
function wpFitCSS(mode,url){
 const size={fill:'cover',fit:'contain',center:'auto',stretch:'100% 100%'}[wpFitKey(mode)];
 return WPFIT_BASE+' center/'+size+' no-repeat url("'+String(url).replace(/["\\]/g,'')+'")';
}
/* The 设为壁纸 menu item exists exactly for image files (the
   daemon's fileKindOf classification) -- nothing else. */
function wpFitShowsWallItem(f){ return !!f&&f.kind==='image'; }
/* v0.65 （细节修复 ⑥): the built-in PDF viewer's pure logic --
   the viewer module (b1/pdfview.ts) is DOM/pdf.js glue over
   these seams, the same split the wallpaper dialog uses.
   Zoom modes mirror the daemon's pdfZoom settings domain
   (settings.go pdfZooms): fit-width / fit-page / actual, plus
   the viewer's own transient 'percent' once the user zooms by
   steps. An unknown value normalizes to 'fit-width', the
   default (pdfZoomKey is also the settings accessor's
   normalizer, like wpFitKey). */
const PDFZOOM_MODES=[
 {id:'fit-width',n:'适宽'},{id:'fit-page',n:'适页'},{id:'actual',n:'实际大小'},
];
function pdfZoomValid(v){ return PDFZOOM_MODES.some(m=>m.id===v); }
function pdfZoomKey(v){ return pdfZoomValid(v)?v:'fit-width'; }
/* The scale band every zoom path enforces (pdfResolveScale
   clamps to it, wheel zoom clamps to it). */
const PDF_SCALE_MIN=0.05, PDF_SCALE_MAX=10;
/* The scale (CSS px per PDF point) a zoom state means for one
   page in a container. fit modes derive from the container;
   'actual' is 100%; 'percent' is the explicit percentage.
   Degenerate inputs (no container yet, a page without a size)
   fall back to the percentage/actual reading so an open never
   produces a zero or NaN scale; the result is clamped to the
   viewer's sane band. */
function pdfResolveScale(mode,pct,contW,contH,pageW,pageH){
 let s=0;
 if(mode==='fit-width'&&contW>0&&pageW>0) s=contW/pageW;
 else if(mode==='fit-page'&&contW>0&&contH>0&&pageW>0&&pageH>0) s=Math.min(contW/pageW,contH/pageH);
 else if(mode==='actual') s=1;
 else s=(+pct||100)/100;
 if(!(s>0)||!isFinite(s)) s=1;
 return Math.min(PDF_SCALE_MAX,Math.max(PDF_SCALE_MIN,s));
}
/* Which page the scroll position is "on": the page whose slot
   contains the viewport's centre line; a centre inside the gap
   between pages belongs to the page above the gap (the last
   page that starts at or above the centre). tops/heights are
   the slots' content-space Y offsets and heights. */
function pdfPageAtScroll(tops,heights,scrollTop,viewH){
 const n=(tops||[]).length;
 if(!n) return 0;
 const c=(+scrollTop||0)+Math.max(0,+viewH||0)/2;
 let cur=0;
 for(let i=0;i<n;i++){ if(tops[i]<=c) cur=i; else break; }
 return Math.min(n-1,Math.max(0,cur));
}
/* The pages worth having rendered around the current one
   (canvases are the expensive part; a 500-page document keeps
   only this window alive, the rest are empty slots). */
function pdfRenderWindow(cur,total,radius){
 const out=[];
 const a=Math.max(0,(+cur||0)-(radius|0)), b=Math.min((+total||0)-1,(+cur||0)+(radius|0));
 for(let i=a;i<=b;i++) out.push(i);
 return out;
}
/* Case-insensitive, non-overlapping substring matches as
   [start,end) pairs -- the find engine's per-page core (a
   page's text is its items concatenated; matches never span
   pages, like Preview). */
function pdfFindMatches(text,query){
 const out=[];
 const t=String(text||''), q=String(query||'');
 if(!q) return out;
 const lt=t.toLowerCase(), lq=q.toLowerCase();
 let i=0;
 while(true){ const j=lt.indexOf(lq,i); if(j<0) break; out.push({s:j,e:j+q.length}); i=j+q.length; }
 return out;
}
/* Find navigation: step from the current match with wrap;
   from "no current" (-1), forward lands on the first match,
   backward on the last. No matches -> -1. */
function pdfFindStep(cur,total,dir){
 const n=+total||0;
 if(n<=0) return -1;
 if(cur<0||cur>=n) return dir<0?n-1:0;
 return ((cur+(dir<0?-1:1))%n+n)%n;
}
/* Per-document rotation steps (the toolbar's 旋转 button):
   always one of 0/90/180/270. */
function pdfRotateNext(rot){
 return (((+rot||0)%360)+360+90)%360;
}
/* Wheel zoom (v0.65-test2 shipped it Ctrl-gated, the
   Acrobat/Chrome-PDF convention; v0.67 removes the gate at
   the user's word -- plain wheel over the pages zooms, Ctrl
   rides the same path, and page movement moves to the
   keyboard / scrollbar / thumbnails). Continuous, not
   stepped: the new scale is the current one times
   exp(-deltaY * PDF_WHEEL_K), so one standard notch
   (|deltaY| = 100) lands near x1.105 and small trackpad
   deltas accumulate into a smooth glide; Edge reports a
   trackpad pinch as ctrl+wheel, so pinch rides this path
   too. The result clamps to the same band pdfResolveScale
   enforces. deltaY is in pixel units (the caller normalizes
   line/page deltaModes before calling). A zero/NaN delta,
   or pushing against a clamp the scale already sits on,
   decides zoom:false so the caller skips the relayout. */
const PDF_WHEEL_K=0.001;
function pdfWheelZoom(deltaY,scale){
 const s=(+scale>0&&isFinite(scale))?+scale:1;
 const d=+deltaY;
 if(!d||!isFinite(d)) return {zoom:false,scale:s};
 const ns=Math.min(PDF_SCALE_MAX,Math.max(PDF_SCALE_MIN,s*Math.exp(-d*PDF_WHEEL_K)));
 return {zoom:ns!==s,scale:ns};
}
/* v0.66 （细节修复 ⑦): the built-in image viewer's pure logic --
   the viewer glue (b1/viewers.ts) is DOM over these seams, the
   same split as the PDF viewer above. The viewer renders the
   natural bitmap in a positioned FRAME (the rotated bounding
   box, effW x effH CSS px at scale 1); all position math is the
   frame's top-left in viewport coordinates, so pan clamping,
   pointer anchoring and rotation compose without special cases.
   Zoom modes mirror the daemon's imgZoom settings domain
   (settings.go imgZooms): fit (the default) / actual, plus the
   viewer's transient 'custom' once the user zooms by hand. */
const IMGZOOM_MODES=[
 {id:'fit',n:'适合窗口'},{id:'actual',n:'实际大小'},
];
function imgZoomValid(v){ return IMGZOOM_MODES.some(m=>m.id===v); }
function imgZoomKey(v){ return imgZoomValid(v)?v:'fit'; }
/* The scale band every image zoom path enforces. The top is
   generous (pixel-peeping a small image is legitimate); the
   bottom keeps a huge photo from vanishing to a dot. */
const IMG_SCALE_MIN=0.05, IMG_SCALE_MAX=16;
/* The frame size at scale 1: a quarter turn swaps the axes. */
function imgEffSize(natW,natH,rot){
 const r=(((+rot||0)%360)+360)%360;
 if(r===90||r===270) return {w:+natH||0,h:+natW||0};
 return {w:+natW||0,h:+natH||0};
}
/* The "fit" scale: the whole frame inside the container,
   capped at 100% (fit never enlarges -- a small image opens at
   actual size, like Preview; wheel/buttons can still zoom in).
   Degenerate inputs fall back to 1. */
function imgFitScale(natW,natH,rot,contW,contH){
 const e=imgEffSize(natW,natH,rot);
 if(!(e.w>0)||!(e.h>0)||!(contW>0)||!(contH>0)) return 1;
 const s=Math.min(contW/e.w,contH/e.h);
 if(!(s>0)||!isFinite(s)) return 1;
 return Math.min(1,Math.max(IMG_SCALE_MIN,s));
}
/* The scale a zoom state means. fit derives from the
   container; actual is 100%; custom is the user's scale. */
function imgResolveScale(mode,custom,natW,natH,rot,contW,contH){
 if(mode==='actual') return 1;
 if(mode==='custom'){
  const s=+custom;
  if(s>0&&isFinite(s)) return Math.min(IMG_SCALE_MAX,Math.max(IMG_SCALE_MIN,s));
  return 1;
 }
 return imgFitScale(natW,natH,rot,contW,contH);
}
/* Wheel zoom (the headline of ⑦): an image window has no
   page-scroll role to protect, so the PLAIN wheel zooms (ctrl
   rides the same path). Same continuous law as pdfWheelZoom --
   one notch is about x1.105, trackpad deltas accumulate into a
   glide -- but no modifier gate and the wider image band. */
function imgWheelZoom(deltaY,scale){
 const s=(+scale>0&&isFinite(scale))?+scale:1;
 const d=+deltaY;
 if(!d||!isFinite(d)) return {zoom:false,scale:s};
 const ns=Math.min(IMG_SCALE_MAX,Math.max(IMG_SCALE_MIN,s*Math.exp(-d*PDF_WHEEL_K)));
 return {zoom:ns!==s,scale:ns};
}
/* Button/keyboard step zoom: a fixed ratio per step, clamped
   to the band; at a bound it reports the bound (zoom:false
   when nothing moved). */
function imgStepScale(scale,dir){
 const s=(+scale>0&&isFinite(scale))?+scale:1;
 const f=dir<0?1/1.25:1.25;
 const ns=Math.min(IMG_SCALE_MAX,Math.max(IMG_SCALE_MIN,s*f));
 return {zoom:ns!==s,scale:ns};
}
/* Pointer anchoring: given the frame at (x,y) sized w x h and
   a pointer at (px,py) in the same viewport coordinates, the
   frame position that keeps the pointed content point fixed
   when the frame becomes nw x nh. */
function imgAnchorPos(px,py,x,y,w,h,nw,nh){
 if(!(w>0)||!(h>0)) return {x:0,y:0};
 const fx=(px-x)/w, fy=(py-y)/h;
 return {x:px-fx*nw,y:py-fy*nh};
}
/* Pan clamping, per axis: a frame smaller than the viewport
   on that axis is CENTERED there (no pan on that axis at all);
   a larger frame may slide until its edges meet the
   viewport's -- it can never be dragged past them into empty
   space. */
function imgClampPan(x,y,w,h,vw,vh){
 const cx=w<=vw?(vw-w)/2:Math.min(0,Math.max(vw-w,x));
 const cy=h<=vh?(vh-h)/2:Math.min(0,Math.max(vh-h,y));
 return {x:cx,y:cy};
}
/* Per-image rotation steps (the toolbar's 旋转 button):
   always one of 0/90/180/270, like pdfRotateNext. */
function imgRotateNext(rot){
 return (((+rot||0)%360)+360+90)%360;
}
/* Prev/next navigation over the sibling list: step with wrap
   (the old viewer's stepImage semantics -- last -> first);
   an unknown current (or an empty list) navigates nowhere. */
function imgStepIndex(cur,total,dir){
 const n=+total||0;
 if(n<=0||cur<0||cur>=n) return -1;
 return ((cur+(dir<0?-1:1))%n+n)%n;
}
/* Double-click: at fit, jump to actual size; anywhere else,
   back to fit. */
function imgDblTarget(mode){ return mode==='fit'?'actual':'fit'; }
/* v0.67: the desktop shell's Ctrl+wheel guard. The Edge --app
   view zooms the WHOLE desktop UI on ctrl+wheel; the user
   wants that gone, but the viewers zoom on wheel input
   themselves (the PDF scroller .pdfv-scroll and the image
   stage #iv-stage own non-passive wheel handlers). The guard
   glue (b1/events.ts, capture phase on document) asks this
   seam about a ctrl+wheel event's ancestor chain -- an array
   of {id, cls} descriptors from the event target up to the
   root, built by the glue so the seam stays DOM-free:
   inside a viewer's scroll container the event PASSES (the
   viewer's own handler zooms and preventDefaults); anywhere
   else in the shell it is SWALLOWED (preventDefault, so the
   page never zooms -- the accompanying scroll is eaten too,
   the same trade Chrome's own PDF viewer makes). */
function shellWheelSwallow(chain){
 if(!chain) return true;
 for(let i=0;i<chain.length;i++){
  const d=chain[i]||{};
  if(d.id==='iv-stage') return false;
  if(/(^|\s)pdfv-scroll(\s|$)/.test(String(d.cls||''))) return false;
 }
 return true;
}
/* v0.67: the desktop UI scale （界面缩放比例， the user's
   "屏幕分辨率" ask, confirmed as display scaling). The
   persisted settings value is the percentage as a string
   (settings.go uiScales mirrors this domain: '100' the
   default). The mechanism is CSS zoom on the desktop
   document's root element (see applyUiScale in b1/settings.ts
   for the coordinate-space contract it imposes). */
const UISCALE_MODES=[80,90,100,110,125,150,175,200];
function uiScaleValid(v){ return UISCALE_MODES.indexOf(parseInt(v,10))>=0; }
function uiScaleKey(v){ return uiScaleValid(v)?String(parseInt(v,10)):'100'; }
function uiScaleFactorOf(v){ return (+uiScaleKey(v))/100; }
/* Pointer/rect space -> local (style/scroll) space under a
   root zoom factor z: getBoundingClientRect() and event
   clientX/clientY are reported in VISUAL (zoomed) units
   while style lengths, clientWidth and the offset/scroll
   properties stay in LOCAL (un-zoomed) units (the CSS zoom
   model). Every glue site that mixes the two divides the
   visual value by z through this seam. A degenerate z reads
   as 1 (no scale). */
function uiToLocal(v,z){
 const f=(+z>0&&isFinite(z))?+z:1;
 const n=+v;
 return isFinite(n)?n/f:0;
}
