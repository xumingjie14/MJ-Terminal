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
