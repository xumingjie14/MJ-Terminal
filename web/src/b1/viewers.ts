/* Generated cross-module imports (v0.58 modularization):
   every name below is owned by the sibling module it is imported
   from -- the same names the pre-modular page shared through one
   script scope. */
import { api, esc } from './core';
import { glyph } from './glyphs';
import { gotoSys, imgZoomDefault, uiScaleFactor } from './settings';
import { openApp } from './windows';
import { openPDFViewer } from './pdfview';
import { renderDesk } from './desktop';
import { DESKFILES, SUBLIST, exportDeskFile, loadDeskFiles, setWinTitle, toast, __bind_SUBLIST } from './appsload';
/* ----- open routing: everything resolves inside the system ----- */
export let openDeskFile: any = async function openDeskFile(p,kind){
 const name=p.split('/').pop();
 /* v0.51: installed productivity components get first claim on
    the kinds they own (office docs -> the office suite chosen
    in 设置, images -> ImageGlass, text/code -> VSCodium). The
    daemon decides from what is actually installed. "install"
    means an office file with no suite yet: guide the user to
    the component download, never fail silently; declining falls
    through to the built-in viewers below. */
 if(kind!=='pdf'&&kind!=='zip'&&kind!=='exe'&&kind!=='dir'){
  const cr=await api('/api/components/openfile',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:p})}).catch(()=>null);
  if(cr&&cr.error){ alert(cr.error); return; }
  if(cr&&cr.action==='launched') return;
  if(cr&&cr.action==='install'){
   if(confirm('打开「'+name+'」需要先装办公组件（'+cr.name+'）。\n现在去「设置 → 组件」下载吗？包不小，装好以后这类文件都用它打开。')){
    gotoSys('components'); return;
   }
  }
 }
 if(kind==='image') return openImageViewer(p);
 if(kind==='text'||kind==='docx') return openTextViewer(p);
 if(kind==='csv') return openGridViewer(p);
 if(kind==='dir') return openZipList(p);
 if(kind==='pdf'){
  /* v0.65 （细节修复 ⑥): the built-in PDF viewer -- never a
     host app, never the isolated browser. The viewer module
     validates with the daemon before opening any window. */
  return openPDFViewer(p);
 }
 if(kind==='zip'){
  if(!confirm('解压「'+name+'」？\n会在终端的 Data/files 里生成一个同名文件夹，桌面上多一个文件夹图标。')) return;
  const r=await api('/api/files/unzip',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:p})}).catch(()=>null);
  if(!r||r.error){ alert('解压失败：'+((r&&r.error)||'接口没回应')); return; }
  toast('已解压到文件夹「'+r.dir+'」（'+r.count+' 个文件）');
  await loadDeskFiles(); renderDesk();
  return;
 }
 if(kind==='exe'){
  /* Deliberate friction: an imported program really runs on this
     PC. Nothing happens unless the user confirms here. */
  if(!confirm('运行「'+name+'」？\n这是从电脑导入的可执行文件，确认后会真的在这台电脑上运行它。')) return;
  const r=await api('/api/files/run',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:p})}).catch(()=>null);
  if(r&&r.error) alert(r.error);
  return;
 }
 openFilePanel(p,kind); /* xlsx / pptx / unknown */
};
/* ----- image viewer (v0.66, detail-fix ⑦): macOS Preview model -----
   The pre-v0.66 viewer was a bare <img> with a fit/100% class
   toggle -- no wheel zoom (the user's original complaint), no
   pan, no rotate. This is a real viewer over the same singleton
   'imgview' window: the natural bitmap sits in a positioned
   FRAME (the rotated bounding box) inside the stage; wheel
   zooms continuously, anchored at the pointer; an overflowing
   frame drag-pans with clamped edges; prev/next walks the
   sibling images of the same place. Rendering is a CSS
   transform on the <img> itself (never a canvas redraw), so
   animated GIF/WebP keep animating at any zoom, and static
   images are the browser's own scaling of the natural bitmap.
   All geometry (fit scale, wheel/step zoom, anchoring, pan
   clamping, rotation, nav stepping) is the pure img* seams in
   pure/fv.ts; this module is DOM glue. Opens, hops and decode
   failures are reported to the daemon's log-only sink
   (POST /api/files/event) so the console hub holds them under
   [files], the tag this path's feature already owns. */
export let IMGV={cur:'',mode:'fit',scale:1,rot:0,natW:0,natH:0,x:0,y:0,err:false};
let imgDrag: any=null, imgBound=false, imgRO: any=null;
export function imageSiblings(p){
 const pool=p.includes('/')?SUBLIST:DESKFILES;
 return pool.filter(f=>f.kind==='image').map(f=>f.path);
}
function imgEvent(event: string, detail?: string){
 if(!IMGV.cur) return;
 api('/api/files/event',{method:'POST',headers:{'Content-Type':'application/json'},
  body:JSON.stringify({path:IMGV.cur,event,detail:detail||''})}).catch(()=>{});
}
function imgEls(){
 return { stage:document.getElementById('iv-stage'), frame:document.getElementById('iv-frame'),
  img:document.getElementById('iv-img') as any, err:document.getElementById('iv-err'),
  name:document.getElementById('iv-name'), zpct:document.getElementById('iv-zpct'),
  pv:document.getElementById('iv-prev') as any, nx:document.getElementById('iv-next') as any };
}
/* Lay the frame out from the state: size from the effective
   (post-rotation) box at the current scale, position clamped
   (centered on any axis where the frame fits). 'keep' preserves
   the current position (resize/rotate stay put); otherwise the
   frame re-centres. */
function layoutImage(keep){
 const {stage,frame,img,zpct}=imgEls(); if(!stage||!frame||!img) return;
 const vw=stage.clientWidth, vh=stage.clientHeight;
 if(!IMGV.natW||!IMGV.natH||!vw||!vh) return;
 IMGV.scale=imgResolveScale(IMGV.mode,IMGV.scale,IMGV.natW,IMGV.natH,IMGV.rot,vw,vh);
 const e=imgEffSize(IMGV.natW,IMGV.natH,IMGV.rot);
 const fw=e.w*IMGV.scale, fh=e.h*IMGV.scale;
 if(!keep){ IMGV.x=(vw-fw)/2; IMGV.y=(vh-fh)/2; }
 const c=imgClampPan(IMGV.x,IMGV.y,fw,fh,vw,vh); IMGV.x=c.x; IMGV.y=c.y;
 frame.style.width=fw+'px'; frame.style.height=fh+'px';
 frame.style.transform='translate('+IMGV.x+'px,'+IMGV.y+'px)';
 img.style.width=(IMGV.natW*IMGV.scale)+'px'; img.style.height=(IMGV.natH*IMGV.scale)+'px';
 img.style.transform='translate(-50%,-50%) rotate('+IMGV.rot+'deg)';
 stage.style.cursor=(fw>vw||fh>vh)?(imgDrag?'grabbing':'grab'):'default';
 if(zpct) zpct.textContent=Math.round(IMGV.scale*100)+'%';
}
/* Zoom to a new scale, keeping the stage point (cx,cy) --
   stage-centre when omitted -- fixed on the image. */
function zoomImageTo(ns,cx?: number,cy?: number){
 const {stage,frame}=imgEls(); if(!stage||!frame) return;
 const vw=stage.clientWidth, vh=stage.clientHeight;
 const e=imgEffSize(IMGV.natW,IMGV.natH,IMGV.rot);
 const ow=e.w*IMGV.scale, oh=e.h*IMGV.scale;
 if(cx===undefined){ cx=vw/2; cy=vh/2; }
 const a=imgAnchorPos(cx,cy,IMGV.x,IMGV.y,ow,oh,e.w*ns,e.h*ns);
 IMGV.x=a.x; IMGV.y=a.y; IMGV.scale=ns; IMGV.mode='custom';
 layoutImage(true);
}
function setImageMode(mode){
 const {stage}=imgEls(); if(!stage) return;
 IMGV.mode=mode;
 if(mode==='fit'){ IMGV.scale=imgFitScale(IMGV.natW,IMGV.natH,IMGV.rot,stage.clientWidth,stage.clientHeight); }
 else if(mode==='actual'){ IMGV.scale=1; }
 layoutImage(false);
}
export function openImageViewer(p){
 openApp('imgview'); setWinTitle('imgview',p.split('/').pop());
 IMGV.cur=p; paintImageViewer();
 imgEvent('open');
}
export function paintImageViewer(){
 const {img,name,pv,nx,err}=imgEls(); if(!img) return;
 const sib=imageSiblings(IMGV.cur);
 const i=sib.indexOf(IMGV.cur);
 if(name) name.textContent=IMGV.cur.split('/').pop()+(sib.length>1?(' · '+(i+1)+' / '+sib.length):'');
 if(pv) pv.disabled=sib.length<2; if(nx) nx.disabled=sib.length<2;
 /* Every (re)paint restarts at the default-open zoom with no
    rotation -- an open and a prev/next hop alike (Preview
    restarts per image). onload lays out once the natural size
    is known; onerror is the honest failure state. */
 IMGV.mode=imgZoomDefault(); IMGV.rot=0; IMGV.natW=0; IMGV.natH=0; IMGV.err=false;
 if(err) err.style.display='none';
 img.style.visibility='hidden';
 img.onload=()=>{ IMGV.natW=img.naturalWidth; IMGV.natH=img.naturalHeight;
  if(!IMGV.natW||!IMGV.natH){ img.onerror(); return; }
  img.style.visibility=''; layoutImage(false); };
 img.onerror=()=>{ if(!IMGV.cur||img.getAttribute('src')!==imgRawSrc(IMGV.cur)) return;
  IMGV.err=true; img.style.visibility='hidden';
  if(err) err.style.display='flex';
  imgEvent('error','图片文件读不出来（可能已损坏或格式不受支持）'); };
 img.src=imgRawSrc(IMGV.cur);
}
function imgRawSrc(p){ return '/api/files/raw?path='+encodeURIComponent(p); }
export function stepImage(d){
 const sib=imageSiblings(IMGV.cur); if(sib.length<2) return;
 const i=imgStepIndex(sib.indexOf(IMGV.cur),sib.length,d); if(i<0) return;
 IMGV.cur=sib[i]; setWinTitle('imgview',IMGV.cur.split('/').pop()); paintImageViewer();
 imgEvent('nav');
}
function rotateImage(){
 IMGV.rot=imgRotateNext(IMGV.rot);
 /* Keep the frame's centre fixed across the quarter turn:
     the effective box changes shape, so re-centre at the same
     scale and let the clamp settle any overflow. */
 layoutImage(false);
}
/* Session hooks (the pdfview pattern): the snapshot asks for
   the current image and restores through the viewer, so a
   rebuilt view comes back showing the same picture. */
try{ (window as any).__mjImgPath=()=>IMGV.cur||'';
 (window as any).__mjRestoreIMG=(p: string)=>{ openImageViewer(p); }; }catch(e){}
/* All of the viewer's listeners, attached once from the
   pages bind (the imgview window is created once per view). */
export function bindImageViewer(){
 if(imgBound) return; imgBound=true;
 const {stage,img}=imgEls(); if(!stage||!img) return;
 const pv=document.getElementById('iv-prev') as any, nx=document.getElementById('iv-next') as any;
 if(pv) pv.onclick=()=>stepImage(-1);
 if(nx) nx.onclick=()=>stepImage(1);
 const zin=document.getElementById('iv-zin') as any, zout=document.getElementById('iv-zout') as any;
 if(zin) zin.onclick=()=>{ const d=imgStepScale(IMGV.scale,1); if(d.zoom) zoomImageTo(d.scale); };
 if(zout) zout.onclick=()=>{ const d=imgStepScale(IMGV.scale,-1); if(d.zoom) zoomImageTo(d.scale); };
 const fit=document.getElementById('iv-fit') as any, act=document.getElementById('iv-actual') as any, rot=document.getElementById('iv-rot') as any;
 if(fit) fit.onclick=()=>setImageMode('fit');
 if(act) act.onclick=()=>setImageMode('actual');
 if(rot) rot.onclick=()=>rotateImage();
 stage.addEventListener('wheel',(ev: any)=>{
  if(IMGV.err||!IMGV.natW) return;
  ev.preventDefault();
  let dy=ev.deltaY||0;
  if(ev.deltaMode===1) dy*=33; else if(ev.deltaMode===2) dy*=stage.clientHeight;
  const d=imgWheelZoom(dy,IMGV.scale); if(!d.zoom) return;
  const r=stage.getBoundingClientRect();
  /* v0.67: the anchor offset is VISUAL (clientX - rect.left)
     under a UI scale; zoomImageTo works in the stage's LOCAL
     space -- convert (uiToLocal). */
  const zf=uiScaleFactor();
  zoomImageTo(d.scale,uiToLocal(ev.clientX-r.left,zf),uiToLocal(ev.clientY-r.top,zf));
 },{passive:false});
 stage.addEventListener('pointerdown',(ev: any)=>{
  if(IMGV.err||!IMGV.natW) return;
  const fr=document.getElementById('iv-frame'); if(!fr) return;
  if(fr.offsetWidth<=stage.clientWidth&&fr.offsetHeight<=stage.clientHeight) return;
  /* v0.67: pan deltas are computed in LOCAL space (pointer
     converted at the boundary, factor pinned for the drag). */
  const zf=uiScaleFactor();
  imgDrag={zf,sx:uiToLocal(ev.clientX,zf),sy:uiToLocal(ev.clientY,zf),x:IMGV.x,y:IMGV.y};
  stage.setPointerCapture(ev.pointerId);
  stage.style.cursor='grabbing';
 });
 stage.addEventListener('pointermove',(ev: any)=>{
  if(!imgDrag) return;
  const e=imgEffSize(IMGV.natW,IMGV.natH,IMGV.rot);
  const c=imgClampPan(imgDrag.x+(uiToLocal(ev.clientX,imgDrag.zf)-imgDrag.sx),imgDrag.y+(uiToLocal(ev.clientY,imgDrag.zf)-imgDrag.sy),
   e.w*IMGV.scale,e.h*IMGV.scale,stage.clientWidth,stage.clientHeight);
  IMGV.x=c.x; IMGV.y=c.y;
  const fr=document.getElementById('iv-frame');
  if(fr) fr.style.transform='translate('+IMGV.x+'px,'+IMGV.y+'px)';
 });
 const endDrag=()=>{ if(!imgDrag) return; imgDrag=null; layoutImage(true); };
 stage.addEventListener('pointerup',endDrag);
 stage.addEventListener('pointercancel',endDrag);
 stage.addEventListener('dblclick',(ev: any)=>{
  if(IMGV.err||!IMGV.natW) return;
  const t=imgDblTarget(IMGV.mode);
  if(t==='actual'){ const r=stage.getBoundingClientRect(); const zf=uiScaleFactor(); zoomImageTo(1,uiToLocal(ev.clientX-r.left,zf),uiToLocal(ev.clientY-r.top,zf)); }
  else setImageMode('fit');
 });
 if(typeof ResizeObserver!=='undefined'){
  imgRO=new ResizeObserver(()=>{ if(IMGV.cur&&IMGV.natW) layoutImage(IMGV.mode!=='fit'); });
  imgRO.observe(stage);
 }
 document.addEventListener('keydown',(ev: any)=>{
  const w=document.getElementById('win-imgview');
  if(!w||w.classList.contains('hidden')) return;
  const t=ev.target as any;
  if(t&&(t.tagName==='INPUT'||t.tagName==='TEXTAREA'||t.isContentEditable)) return;
  if(ev.key==='ArrowLeft'){ stepImage(-1); return; }
  if(ev.key==='ArrowRight'){ stepImage(1); return; }
  if(IMGV.err||!IMGV.natW) return;
  const mod=ev.metaKey||ev.ctrlKey;
  if(ev.key==='+'||ev.key==='='){ ev.preventDefault(); const d=imgStepScale(IMGV.scale,1); if(d.zoom) zoomImageTo(d.scale); }
  else if(ev.key==='-'||ev.key==='_'){ ev.preventDefault(); const d=imgStepScale(IMGV.scale,-1); if(d.zoom) zoomImageTo(d.scale); }
  else if(ev.key==='0'){ ev.preventDefault(); if(mod) setImageMode(imgZoomDefault()); else zoomImageTo(1); }
  else if(!mod&&(ev.key==='f'||ev.key==='F')) setImageMode('fit');
  else if(!mod&&(ev.key==='r'||ev.key==='R')) rotateImage();
 });
}
export async function openTextViewer(p){
 openApp('textview'); setWinTitle('textview',p.split('/').pop());
 const pre=document.getElementById('tv-pre'); if(pre) pre.textContent='读取中…';
 const r=await api('/api/files/text?path='+encodeURIComponent(p)).catch(()=>null);
 if(!pre) return;
 if(!r||r.error){ pre.textContent='打开失败：'+((r&&r.error)||'接口没回应'); return; }
 document.getElementById('tv-name').textContent=r.name;
 document.getElementById('tv-note').textContent=(r.kind==='docx'?'文档预览（仅文字）':'')+(r.truncated?' · 仅显示前 2 MB':'');
 pre.textContent=r.text||'（空文件）';
}
export async function openGridViewer(p){
 openApp('gridview'); setWinTitle('gridview',p.split('/').pop());
 const tab=document.getElementById('gv-tab'); if(tab) tab.innerHTML='';
 const note=document.getElementById('gv-note'); if(note) note.textContent='读取中…';
 const r=await api('/api/files/csv?path='+encodeURIComponent(p)).catch(()=>null);
 if(!tab) return;
 if(!r||r.error){ if(note) note.textContent='打开失败：'+((r&&r.error)||'接口没回应'); return; }
 const rows=r.rows||[];
 tab.innerHTML=rows.map((row,i)=>'<tr>'+row.map(c=>i===0?`<th>${esc(c)}</th>`:`<td>${esc(c)}</td>`).join('')+'</tr>').join('')||'<tr><td>（空表格）</td></tr>';
 if(note) note.textContent=r.name+' · 共显示 '+rows.length+' 行'+(r.truncatedRows?'（文件更长，只显示前 200 行）':'')+(r.truncatedCols?' · 列数已截到前 50 列':'');
}
export async function openZipList(p){
 openApp('ziplist'); setWinTitle('ziplist',p.split('/').pop());
 const box=document.getElementById('zl-list'); if(box) box.innerHTML='加载中…';
 const r=await api('/api/files/sub?path='+encodeURIComponent(p)).catch(()=>null);
 if(!box) return;
 if(!r||r.error){ box.textContent='打开失败：'+((r&&r.error)||'接口没回应'); return; }
 __bind_SUBLIST.set(r.files||[]);
 box.innerHTML=SUBLIST.length?`<table>${SUBLIST.map(it=>`<tr class="file" data-p="${esc(it.path)}" data-k="${it.kind}" data-d="${it.dir}"><td>${glyph(it.dir?'folder':'file',15)}</td><td>${esc(it.name)}</td><td>${it.dir?'':(it.size/1024).toFixed(1)+' KB'}</td></tr>`).join('')}</table>`:'（空文件夹）';
 box.querySelectorAll('tr.file').forEach(tr=>tr.ondblclick=()=>{
  if(tr.dataset.d==='true'){ toast('子文件夹先不在这里展开（它在 Data/files 里）'); return; }
  openDeskFile(tr.dataset.p,tr.dataset.k);
 });
}
export function openFilePanel(p,kind){
 openApp('filepanel');
 const name=p.split('/').pop();
 setWinTitle('filepanel',name);
 const office=(kind==='xlsx'||kind==='pptx');
 document.getElementById('fp2-name').textContent=name;
 document.getElementById('fp2-desc').textContent=office
  ? '这个格式（'+(kind==='xlsx'?'Excel 表格':'PowerPoint 演示')+'）系统内暂时不能直接编辑。两个诚实的办法：传到网页版办公（腾讯文档）里打开编辑，或者导出到这台电脑、用电脑上的 Office 打开。'
  : '这个格式系统里还没有打开方式。可以先导出到这台电脑，用电脑上的软件打开。';
 document.getElementById('fp2-web').style.display=office?'':'none';
 document.getElementById('fp2-web').onclick=()=>openWebApp('https://docs.qq.com/desktop');
 document.getElementById('fp2-exp').onclick=ev=>{ ev.preventDefault(); exportDeskFile({path:p,name}); };
}

export let openWebApp: any = async function openWebApp(url){
 const r=await api('/api/appopen',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({url})}).catch(()=>null);
 if(r&&r.ok) return;
 if(r&&r.error) return; /* real error (no browser / bad url / test env): nothing more to do from a desktop icon */
 /* old daemon without /api/appopen answers non-JSON: open as a normal link */
 api('/api/open',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({url})});
};

export const __bind_openDeskFile: any = { get(): any { return openDeskFile; }, set(v: any): any { openDeskFile = v; return v; } };

export const __bind_openWebApp: any = { get(): any { return openWebApp; }, set(v: any): any { openWebApp = v; return v; } };

export const __bind_IMGV: any = { get(): any { return IMGV; }, set(v: any): any { IMGV = v; return v; } };
