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
   clamps to it, ctrl+wheel zoom clamps to it). */
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
/* Ctrl+wheel zoom (v0.65-test2, the Acrobat/Chrome-PDF
   convention): plain wheel keeps scrolling -- only a
   ctrl+wheel event zooms, which is also exactly how Edge
   reports a trackpad pinch, so pinch rides the same path.
   Continuous, not stepped: the new scale is the current one
   times exp(-deltaY * PDF_WHEEL_K), so one standard notch
   (|deltaY| = 100) lands near x1.105 and small trackpad
   deltas accumulate into a smooth glide; the result clamps
   to the same band pdfResolveScale enforces. deltaY is in
   pixel units (the caller normalizes line/page deltaModes
   before calling). A zero/NaN delta, or pushing against a
   clamp the scale already sits on, decides zoom:false so
   the caller skips the relayout. */
const PDF_WHEEL_K=0.001;
function pdfWheelZoom(deltaY,ctrlKey,scale){
 const s=(+scale>0&&isFinite(scale))?+scale:1;
 if(!ctrlKey) return {zoom:false,scale:s};
 const d=+deltaY;
 if(!d||!isFinite(d)) return {zoom:false,scale:s};
 const ns=Math.min(PDF_SCALE_MAX,Math.max(PDF_SCALE_MIN,s*Math.exp(-d*PDF_WHEEL_K)));
 return {zoom:ns!==s,scale:ns};
}
