/* Generated cross-module imports (v0.58 modularization):
   every name below is owned by the sibling module it is imported
   from -- the same names the pre-modular page shared through one
   script scope. */
import { $50, fvFmtSize, fvFmtTime, fvKindLabel } from './fvhead';
/* ================= Files as Finder ================= */
export const FV={group:'all',view:'icon',sort:'name',dir:1,q:'',sel:new Set(),anchor:null};
export function fvVisibleList(){ return fvVisible(DESKFILES,FV.group,FV.q,FV.sort,FV.dir); }
export async function paintFiles(){
 if(!$50('fv-body')) return;
 await loadDeskFiles();
 paintFvSide(); paintFvBody(); paintFvPrev();
 const crumb=$50('fv-crumb');
 if(crumb) crumb.textContent=(FV_GROUPS.find(g=>g.id===FV.group)||{}).n||'全部文件';
}
export function paintFvSide(){
 const side=$50('fv-side'); if(!side) return;
 const counts={};
 DESKFILES.forEach(f=>{ const g=fvGroupOf(f); counts[g]=(counts[g]||0)+1; });
 side.innerHTML=FV_GROUPS.map(g=>{
  const c=g.id==='all'?DESKFILES.length:(g.id==='recent'?Math.min(FV_RECENT_CAP,DESKFILES.length):(counts[g.id]||0));
  return '<div class="fv-sit '+(FV.group===g.id?'on':'')+'" data-g="'+g.id+'"><span>'+g.n+'</span><span class="cnt">'+c+'</span></div>';
 }).join('');
 side.querySelectorAll('.fv-sit').forEach(el=>el.onclick=()=>{ FV.group=el.dataset.g; FV.sel.clear(); FV.anchor=null; paintFiles(); });
}
export function fvSelClick(f,ev,visible){
 if(ev.shiftKey&&FV.anchor){
  fvRangeSel(visible.map(x=>x.path),FV.anchor,f.path).forEach(p=>FV.sel.add(p));
 } else if(ev.ctrlKey||ev.metaKey){
  FV.sel=new Set(fvToggleSel(Array.from(FV.sel),f.path));
  FV.anchor=f.path;
 } else {
  FV.sel=new Set([f.path]); FV.anchor=f.path;
 }
 paintFvBody(); paintFvPrev();
}
export function fvItemHTML(f){
 return '<button class="fv-it '+(FV.sel.has(f.path)?'sel':'')+'" data-p="'+esc(f.path)+'">'+fileIconHTML(f)+'<span class="fvn">'+esc(f.name)+'</span></button>';
}
export function fvBindItems(root,visible){
 root.querySelectorAll('.fv-it,[data-fvrow]').forEach(el=>{
  const f=visible.find(x=>x.path===el.dataset.p); if(!f) return;
  el.addEventListener('click',ev=>{ if(Date.now()<suppressClickUntil) return; fvSelClick(f,ev,visible); });
  el.addEventListener('dblclick',()=>openDeskFile(f.path,f.kind));
  el.addEventListener('contextmenu',ev=>{
   ev.preventDefault(); ev.stopPropagation();
   if(!FV.sel.has(f.path)){ FV.sel=new Set([f.path]); FV.anchor=f.path; paintFvBody(); paintFvPrev(); }
   fvShowCtx(ev.clientX,ev.clientY,f);
  });
 });
}
export function paintFvBody(){
 const body=$50('fv-body'); if(!body) return;
 const visible=fvVisibleList();
 if(FV.view==='list'){
  if(!visible.length){ body.innerHTML='<div class="fv-none">这个分组里还没有文件。把文件拖进来，或点「导入…」。</div>'; return; }
  const arrow=k=>FV.sort===k?(FV.dir>0?' ↑':' ↓'):'';
  body.innerHTML='<table class="fv-tab"><thead><tr>'
   +'<th data-k="name">名称'+arrow('name')+'</th><th data-k="size">大小'+arrow('size')+'</th><th data-k="kind">类型'+arrow('kind')+'</th><th data-k="mtime">导入时间'+arrow('mtime')+'</th>'
   +'</tr></thead><tbody>'
   +visible.map(f=>'<tr data-fvrow data-p="'+esc(f.path)+'" class="'+(FV.sel.has(f.path)?'sel':'')+'"><td>'+esc(f.name)+'</td><td>'+(f.dir?'—':fvFmtSize(f.size))+'</td><td>'+fvKindLabel(f)+'</td><td>'+fvFmtTime(f.mtime)+'</td></tr>').join('')
   +'</tbody></table>';
  body.querySelectorAll('th[data-k]').forEach(th=>th.onclick=()=>{
   const k=th.dataset.k;
   if(FV.sort===k) FV.dir=-FV.dir;
   else { FV.sort=k; FV.dir=(k==='mtime')?-1:1; }
   paintFvBody();
  });
  fvBindItems(body,visible);
  return;
 }
 if(FV.view==='cols'){
  const groupFiles=pid=>DESKFILES.filter(f=>fvGroupOf(f)===pid);
  body.innerHTML='<div class="fv-cols"><div class="fv-col"><div class="fv-colh">分组</div>'
   +FV_GROUPS.filter(g=>g.id!=='all'&&g.id!=='recent').map(g=>'<button class="fv-it '+(FV.group===g.id?'sel':'')+'" data-gcol="'+g.id+'"><span class="fvn" style="font-size:13.5px">'+g.n+'</span><span class="cnt" style="opacity:.55;font-size:12px;margin-left:auto">'+groupFiles(g.id).length+'</span></button>').join('')
   +'</div><div class="fv-col"><div class="fv-colh">'+esc((FV_GROUPS.find(g=>g.id===FV.group)||{}).n||'')+'</div>'
   +(visible.length?visible.map(f=>fvItemHTML(f)).join(''):'<div class="fv-none">空的</div>')
   +'</div></div>';
  body.querySelectorAll('[data-gcol]').forEach(el=>el.onclick=()=>{ FV.group=el.dataset.gcol; FV.sel.clear(); FV.anchor=null; paintFiles(); });
  fvBindItems(body,visible);
  return;
 }
 body.innerHTML=visible.length
  ? '<div class="fv-grid">'+visible.map(f=>fvItemHTML(f)).join('')+'</div>'
  : '<div class="fv-none">这个分组里还没有文件。把文件拖进来，或点「导入…」。</div>';
 fvBindItems(body,visible);
}
export function fvSelFiles(){ return DESKFILES.filter(f=>FV.sel.has(f.path)); }
export async function fvDeleteFiles(files){
 if(!files.length) return;
 if(!confirm('删除选中的 '+files.length+' 个文件？\n删的是终端里的副本（在 Data/files 里），电脑上的原文件不受影响。')) return;
 let ok=0, fails=0;
 for(const f of files){
  const r=await api('/api/files/delete',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({name:f.name})}).catch(()=>null);
  if(!r||r.error){ fails++; continue; }
  ok++; delete LAYOUT.pos['file:'+f.name];
 }
 saveLayout();
 FV.sel.clear();
 await loadDeskFiles(); renderDesk(); paintFiles();
 if(fails) alert(fails+' 个文件删除失败');
 else toast('已删除 '+ok+' 个文件');
}
export async function fvRenameFile(f){
 const nn=prompt('新名字（不能带路径）',f.name);
 if(!nn||nn.trim()===f.name) return;
 const r=await api('/api/files/rename',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({old:f.name,new:nn.trim()})}).catch(()=>null);
 if(!r||r.error){ alert('改名失败：'+((r&&r.error)||'接口没回应')); return; }
 if(LAYOUT.pos['file:'+f.name]){ LAYOUT.pos['file:'+r.name]=LAYOUT.pos['file:'+f.name]; delete LAYOUT.pos['file:'+f.name]; }
 FV.sel=new Set([r.name]);
 saveLayout();
 await loadDeskFiles(); renderDesk(); paintFiles();
}
export function fvShowCtx(x,y,f){
 const sel=fvSelFiles();
 if(sel.length>1){
  ctxEl.innerHTML='<div class="ctxitem" id="fvx-exp">导出选中的 '+sel.length+' 个到电脑</div><div class="ctxitem" id="fvx-del" style="color:#ff9d97">删除选中的 '+sel.length+' 个</div>';
  ctxEl.classList.remove('hidden');
  ctxEl.style.left=Math.min(x,innerWidth-240)+'px';
  ctxEl.style.top=Math.min(y,innerHeight-140)+'px';
  $50('fvx-exp').onclick=()=>{ hideCtx(); sel.forEach(exportDeskFile); };
  $50('fvx-del').onclick=()=>{ hideCtx(); fvDeleteFiles(sel); };
  return;
 }
 showFileCtx(x,y,f);
}
export function paintFvPrev(){
 const pv=$50('fv-prev'); if(!pv) return;
 const sel=fvSelFiles();
 if(sel.length>1){
  pv.innerHTML='<div class="fv-pname">已选中 '+sel.length+' 个文件</div><div class="fv-pmeta">总大小：'+fvFmtSize(sel.reduce((a,f)=>a+(f.size||0),0))+'</div>'
   +'<div class="fv-pbtns"><button class="btn gray" id="fvp-exp">导出到电脑</button><button class="btn red" id="fvp-del">删除</button></div>';
  $50('fvp-exp').onclick=()=>sel.forEach(exportDeskFile);
  $50('fvp-del').onclick=()=>fvDeleteFiles(sel);
  return;
 }
 if(!sel.length){
  pv.innerHTML='<div class="fv-none" style="padding-top:34px">选中一个文件，这里显示预览和信息。</div>';
  return;
 }
 const f=sel[0];
 const thumb=(f.kind==='image'&&['jpg','jpeg','png','gif'].indexOf(f.ext)>=0)
  ? '<img class="fv-thumb" src="/api/files/thumb?name='+encodeURIComponent(f.name)+'" alt="">'
  : fileIconHTML(f);
 pv.innerHTML=thumb+'<div class="fv-pname">'+esc(f.name)+'</div>'
  +'<div class="fv-pmeta">大小：'+(f.dir?'—':fvFmtSize(f.size))+'<br>类型：'+fvKindLabel(f)+(f.ext?('（.'+esc(f.ext)+'）'):'')+'<br>导入时间：'+fvFmtTime(f.mtime)+'</div>'
  +'<div class="fv-pbtns"><button class="btn" id="fvp-open">打开</button><button class="btn gray" id="fvp-exp">导出到电脑</button><button class="btn gray" id="fvp-ren">重命名…</button><button class="btn red" id="fvp-del">删除</button></div>';
 $50('fvp-open').onclick=()=>openDeskFile(f.path,f.kind);
 $50('fvp-exp').onclick=()=>exportDeskFile(f);
 $50('fvp-ren').onclick=()=>fvRenameFile(f);
 $50('fvp-del').onclick=()=>fvDeleteFiles([f]);
}
window.__filesRefresh=()=>{ if($50('fv-body')) paintFiles(); };
/* v0.56: the desktop file menu's 在「文件」中显示 -- select the
   file in the 全部 group once the Files window exists. */
window.__filesReveal=(path)=>{ if(!$50('fv-body')) return false; FV.group='all'; FV.q=''; FV.sel=new Set([path]); FV.anchor=path; paintFiles(); return true; };
window.__bindFiles=function(){
 const w=$50('win-files');
 if(w){ w.style.width='min(1060px,96vw)'; w.style.height='min(640px,calc(100vh - 120px))'; }
 const seg=$50('fv-viewseg');
 if(seg) seg.querySelectorAll('button').forEach(b=>{
  b.classList.toggle('on',b.dataset.v===FV.view);
  b.onclick=()=>{ FV.view=b.dataset.v; seg.querySelectorAll('button').forEach(x=>x.classList.toggle('on',x===b)); paintFvBody(); };
 });
 const sortSel=$50('fv-sort');
 if(sortSel){ sortSel.value=FV.sort; sortSel.onchange=()=>{ FV.sort=sortSel.value; FV.dir=(FV.sort==='mtime')?-1:1; paintFvBody(); }; }
 const q=$50('fv-q');
 if(q) q.addEventListener('input',()=>{ FV.q=q.value; paintFvBody(); });
 const imp=$50('fv-import');
 if(imp) imp.onclick=()=>{ const f=$50('deskimp'); if(f) f.click(); };
 paintFiles();
};
/* v0.59 block 3 (item #8): a files mutation made anywhere --
   a rename in this view, an import from the desktop, another
   window -- re-pulls and repaints THIS view in place.
   paintFiles() already re-fetches via the typed API and no-ops
   when the Files window is not in the DOM. The short delay lets
   bundle 1's own refresh land first so both surfaces converge
   in one pass. */
try{
 if(typeof MJEvents!=='undefined'&&MJEvents&&MJEvents.on){
  MJEvents.on('files',()=>{ setTimeout(()=>{ try{ paintFiles(); }catch(e){} },180); });
 }
}catch(e){}
