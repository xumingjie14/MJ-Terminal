/* Generated cross-module imports (v0.58 modularization):
   every name below is owned by the sibling module it is imported
   from -- the same names the pre-modular page shared through one
   script scope. */
import { APPS, api, esc } from './core';
import { glyph } from './glyphs';
import { pdfZoomDefault } from './settings';
import { openApp } from './windows';
/* ----- built-in PDF viewer (v0.65, detail-fix ⑥) -----
   macOS Preview grammar over pdf.js (vendored in the EXE,
   served by the daemon at /assets/pdfjs/ -- fully offline).
   One window PER DOCUMENT: opening a PDF pushes a throwaway
   APPS entry (nodesk, like the other viewers) whose window the
   ordinary machinery (Stage Manager, session memory, cascade)
   treats like any other; closing the window runs the entry's
   onclose hook, which destroys the pdf.js document and removes
   the entry again. The daemon validates the file BEFORE any
   window exists (POST /api/pdf/open: inside Data/files, .pdf,
   %PDF- header), and the bytes ride the existing range-capable
   /api/files/raw. The pure seams (zoom scale, scroll->page,
   find matching/navigation, rotation) live in pure/fv.ts and
   are node-tested; this module is the DOM/pdf.js glue. */
export const PDFV=new Map();
let pdfSeq=0;
const PDFGEOM=new Map(); /* path -> session geometry, restore only */
try{ (window as any).__mjRestorePDF=(p: string, geom: any)=>{ if(geom) PDFGEOM.set(p,geom); openPDFViewer(p); }; }catch(e){}

let pdfjsWait: any=null;
function pdfjsLib(): Promise<any>{
 const w: any=window as any;
 if(w.pdfjsLib) return Promise.resolve(w.pdfjsLib);
 if(w.pdfjsLibError) return Promise.reject(new Error(w.pdfjsLibError));
 if(!pdfjsWait) pdfjsWait=new Promise((res,rej)=>{
  const t0=Date.now();
  const iv=setInterval(()=>{
   if(w.pdfjsLib){ clearInterval(iv); res(w.pdfjsLib); }
   else if(w.pdfjsLibError){ clearInterval(iv); rej(new Error(w.pdfjsLibError)); }
   else if(Date.now()-t0>15000){ clearInterval(iv); rej(new Error('PDF 引擎加载超时')); }
  },100);
 });
 return pdfjsWait;
}
function pdfEvent(st: any, event: string, pages?: number, detail?: string){
 api('/api/pdf/event',{method:'POST',headers:{'Content-Type':'application/json'},
  body:JSON.stringify({path:st.path,event,pages:pages||0,detail:detail||''})}).catch(()=>{});
}
const msgOf=(e: any)=>String((e&&(e.message||e.name))||e||'未知错误');

export async function openPDFViewer(p: string){
 for(const st of PDFV.values()){ if(st.path===p){ openApp(st.id); return; } }
 const r=await api('/api/pdf/open',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:p})}).catch(()=>null);
 if(!r||r.error){ alert('打开 PDF 失败：'+((r&&r.error)||'接口没回应')); return; }
 const id='pdfview-'+(++pdfSeq);
 const app: any={id,e:'',g:'g-sys',glyph:'',n:r.name,nodesk:true,gkey:'file',pdfPath:p,
  onclose:()=>destroyPDFInstance(id)};
 APPS.push(app);
 const st: any={ id, path:p, name:r.name, app,
  lib:null, task:null, doc:null, pages:[], numPages:0,
  mode:pdfZoomDefault(), pct:100, scale:1, rotation:0, cur:0, gen:0,
  slots:[], tops:[], heights:[], textCache:new Map(),
  find:{open:false,q:'',flat:[],byPage:new Map(),cur:-1,busy:false},
  side:true, pwCb:null, pwCancelled:false, destroyed:false,
  thumbDone:new Set(), thumbEls:[], thumbObs:null, ro:null, scrollRaf:0, findTimer:0, zoomTimer:0,
  els:{} };
 PDFV.set(id,st);
 openApp(id);
 const w=document.getElementById('win-'+id);
 if(w){
  const g=PDFGEOM.get(p); PDFGEOM.delete(p);
  if(g){ if(g.l)w.style.left=g.l; if(g.t)w.style.top=g.t; if(g.wd)w.style.width=g.wd; }
  else { w.style.left=(120+(pdfSeq%5)*36)+'px'; w.style.top=(70+(pdfSeq%5)*30)+'px'; }
  if(!w.style.width) w.style.width='900px';
  if(!w.style.height) w.style.height='660px';
 }
 buildChrome(st);
 loadPDF(st);
}

function destroyPDFInstance(id: string){
 const st=PDFV.get(id); if(!st) return;
 PDFV.delete(id);
 st.destroyed=true;
 if(st.ro){ try{st.ro.disconnect()}catch(e){} }
 if(st.thumbObs){ try{st.thumbObs.disconnect()}catch(e){} }
 st.slots.forEach((s: any)=>{ if(s.task){ try{s.task.cancel()}catch(e){} } });
 if(st.task){ try{st.task.destroy()}catch(e){} }
 if(st.doc){ try{st.doc.destroy()}catch(e){} }
 const ix=APPS.findIndex((a: any)=>a.id===id); if(ix>=0) APPS.splice(ix,1);
 const w=document.getElementById('win-'+id); if(w) w.remove();
}
function closeWin(st: any){
 const w=document.getElementById('win-'+st.id); if(!w) return;
 const btn: any=w.querySelector('[data-a="close"]'); if(btn) btn.click();
}

/* ---------- window chrome ---------- */
function buildChrome(st: any){
 const b=document.getElementById('body-'+st.id); if(!b) return;
 b.style.padding='12px 14px';
 b.style.overflow='hidden'; /* the viewer's own scroller owns scrolling */
 b.innerHTML=`<div class="pdfv">
  <div class="pdfv-tools">
   <button class="btn gray pdfv-tb" data-a="side" title="边栏（缩略图 / 目录）">${glyph('sidebar',15)}</button>
   <span class="pdfv-name" title="${esc(st.path)}">${esc(st.name)}</span>
   <span class="pdfv-flex"></span>
   <button class="btn gray pdfv-tb" data-a="prev" title="上一页">‹</button>
   <input class="pdfv-pagein" data-a="pagein" value="–" autocomplete="off">
   <span class="pdfv-of">/ –</span>
   <button class="btn gray pdfv-tb" data-a="next" title="下一页">›</button>
   <button class="btn gray pdfv-tb" data-a="zout" title="缩小（⌘−）">−</button>
   <span class="seg pdfv-zseg"><button data-m="fit-width">适宽</button><button data-m="fit-page">适页</button><button data-m="actual">实际</button></span>
   <button class="btn gray pdfv-tb" data-a="zin" title="放大（⌘+）">+</button>
   <span class="tag pdfv-zpct"></span>
   <button class="btn gray pdfv-tb" data-a="rot" title="旋转 90°">${glyph('rotate',15)}</button>
   <button class="btn gray pdfv-tb" data-a="find" title="查找（⌘F）">${glyph('search',15)}</button>
  </div>
  <div class="pdfv-bar pdfv-find" style="display:none">
   <input class="pdfv-fq" placeholder="在文档中查找" autocomplete="off">
   <span class="tag pdfv-fcount"></span>
   <button class="btn gray pdfv-tb" data-a="fprev" title="上一个（⇧回车）">‹</button>
   <button class="btn gray pdfv-tb" data-a="fnext" title="下一个（回车）">›</button>
   <button class="btn gray pdfv-tb" data-a="fclose" title="关闭查找（Esc）">✕</button>
  </div>
  <div class="pdfv-bar pdfv-pw" style="display:none">
   <span>此 PDF 需要密码才能打开</span>
   <input class="pdfv-pwin" type="password" placeholder="输入密码" autocomplete="off">
   <button class="btn" data-a="pwok">打开</button>
   <button class="btn gray" data-a="pwcancel">取消</button>
   <span class="pdfv-pwerr" style="color:#ff9f0a"></span>
  </div>
  <div class="pdfv-main">
   <div class="pdfv-side"><div class="pdfv-ol"></div><div class="pdfv-thumbs"></div></div>
   <div class="pdfv-scroll" tabindex="0"><div class="pdfv-pages"></div></div>
  </div>
  <div class="pdfv-status">正在载入…</div>
  <div class="pdfv-err" style="display:none"><p class="pdfv-errmsg"></p><button class="btn gray" data-a="errclose">关闭窗口</button></div>
 </div>`;
 const q=(s: string)=>b.querySelector(s);
 st.els={ body:b, scroll:q('.pdfv-scroll'), pages:q('.pdfv-pages'), side:q('.pdfv-side'),
  ol:q('.pdfv-ol'), thumbs:q('.pdfv-thumbs'), pageIn:q('.pdfv-pagein'), of:q('.pdfv-of'),
  zpct:q('.pdfv-zpct'), zseg:q('.pdfv-zseg'), status:q('.pdfv-status'), err:q('.pdfv-err'),
  findBar:q('.pdfv-find'), fq:q('.pdfv-fq'), fcount:q('.pdfv-fcount'),
  pwBar:q('.pdfv-pw'), pwIn:q('.pdfv-pwin'), pwErr:q('.pdfv-pwerr') };
 const on=(sel: string,fn: any)=>{ const el=q(sel); if(el) el.onclick=fn; };
 on('[data-a="side"]',()=>toggleSide(st));
 on('[data-a="prev"]',()=>gotoPage(st,st.cur-1));
 on('[data-a="next"]',()=>gotoPage(st,st.cur+1));
 on('[data-a="zin"]',()=>zoomStep(st,1));
 on('[data-a="zout"]',()=>zoomStep(st,-1));
 on('[data-a="rot"]',()=>rotateDoc(st));
 on('[data-a="find"]',()=>openFind(st));
 on('[data-a="fprev"]',()=>stepFind(st,-1));
 on('[data-a="fnext"]',()=>stepFind(st,1));
 on('[data-a="fclose"]',()=>closeFind(st));
 on('[data-a="pwok"]',()=>submitPw(st));
 on('[data-a="pwcancel"]',()=>{ st.pwCancelled=true; if(st.task){ try{st.task.destroy()}catch(e){} } closeWin(st); });
 on('[data-a="errclose"]',()=>closeWin(st));
 st.els.zseg.querySelectorAll('button').forEach((x: any)=>x.onclick=()=>{ st.mode=x.dataset.m; if(st.mode==='actual') st.pct=100; applyZoom(st); });
 st.els.pageIn.addEventListener('keydown',(ev: any)=>{ if(ev.key==='Enter'){ commitPageInput(st); } ev.stopPropagation(); });
 st.els.pageIn.addEventListener('click',(ev: any)=>ev.stopPropagation());
 st.els.fq.addEventListener('input',()=>{ clearTimeout(st.findTimer); st.findTimer=setTimeout(()=>runFind(st),220); });
 st.els.pwIn.addEventListener('keydown',(ev: any)=>{ if(ev.key==='Enter') submitPw(st); ev.stopPropagation(); });
 st.els.scroll.addEventListener('scroll',()=>onScroll(st),{passive:true});
 const w=document.getElementById('win-'+st.id);
 if(w) w.addEventListener('keydown',(ev: any)=>pdfKeys(st,ev));
 if(typeof ResizeObserver!=='undefined'){
  st.ro=new ResizeObserver(()=>{ if(st.destroyed||!st.doc) return;
   clearTimeout(st.zoomTimer); st.zoomTimer=setTimeout(()=>{ if(st.mode!=='percent'&&st.mode!=='actual') applyZoom(st); else keepRenderWindow(st); },160); });
  st.ro.observe(st.els.scroll);
 }
}
function setStatus(st: any, t: string){ const el=st.els.status; if(!el) return; el.textContent=t||''; el.style.display=t?'':'none'; }
function loadFail(st: any, msg: string){
 setStatus(st,'');
 const e=st.els.err; if(!e) return;
 e.querySelector('.pdfv-errmsg').textContent=msg;
 e.style.display='flex';
 if(st.els.scroll) st.els.scroll.style.visibility='hidden';
 if(st.els.side) st.els.side.style.visibility='hidden';
}

/* ---------- loading ---------- */
async function loadPDF(st: any){
 try{ st.lib=await pdfjsLib(); }
 catch(e){ loadFail(st,'PDF 引擎没能加载：'+msgOf(e)); return; }
 if(st.destroyed) return;
 const lib=st.lib;
 const task=lib.getDocument({
  url:'/api/files/raw?path='+encodeURIComponent(st.path),
  cMapUrl:'/assets/pdfjs/cmaps/', cMapPacked:true,
  standardFontDataUrl:'/assets/pdfjs/standard_fonts/',
 });
 st.task=task;
 task.onProgress=(pr: any)=>{ if(pr&&pr.total) setStatus(st,'正在载入 '+Math.min(99,Math.round(100*pr.loaded/pr.total))+'%'); };
 task.onPassword=(cb: any, reason: any)=>{
  st.pwCb=cb;
  if(reason===lib.PasswordResponses.PASSWORD_INCORRECT){ pdfEvent(st,'password-failed'); showPw(st,'密码不正确，再试一次。'); }
  else { pdfEvent(st,'password'); showPw(st,''); }
 };
 let doc: any;
 try{ doc=await task.promise; }
 catch(e){
  if(st.destroyed||st.pwCancelled) return;
  if(e&&e.name==='InvalidPDFException'){ pdfEvent(st,'corrupt',0,msgOf(e)); loadFail(st,'这个 PDF 的内容已损坏或不完整，解析不了。'); }
  else { pdfEvent(st,'error',0,msgOf(e)); loadFail(st,'打开失败：'+msgOf(e)); }
  return;
 }
 if(st.destroyed){ try{doc.destroy()}catch(e){} return; }
 st.doc=doc; st.numPages=doc.numPages;
 setStatus(st,'正在准备页面…');
 st.pages=[];
 for(let i=1;i<=doc.numPages;i++){
  st.pages.push(await doc.getPage(i));
  if(st.destroyed) return;
  if(i%50===0) setStatus(st,'正在准备页面 '+i+' / '+doc.numPages);
 }
 pdfEvent(st,'loaded',doc.numPages);
 st.els.of.textContent='/ '+doc.numPages;
 applyZoom(st);
 buildOutline(st);
 buildThumbs(st);
 setStatus(st,'');
}
function showPw(st: any, err: string){
 setStatus(st,'');
 st.els.pwBar.style.display='';
 st.els.pwErr.textContent=err;
 st.els.pwIn.value='';
 setTimeout(()=>{ if(st.els.pwIn) st.els.pwIn.focus(); },60);
}
function submitPw(st: any){
 const cb=st.pwCb; if(!cb) return;
 st.pwCb=null;
 st.els.pwBar.style.display='none';
 setStatus(st,'正在验证密码…');
 cb(st.els.pwIn.value);
}

/* ---------- layout / zoom ---------- */
function baseSize(st: any){
 const p=st.pages[0]; if(!p) return {w:595,h:842};
 const vp=p.getViewport({scale:1,rotation:st.rotation+(p.rotate||0)});
 return {w:vp.width,h:vp.height};
}
function layoutSlots(st: any){
 const cont=st.els.pages; cont.innerHTML=''; st.slots=[];
 const GAP=14;
 let y=GAP;
 st.tops=[]; st.heights=[];
 for(let i=0;i<st.numPages;i++){
  const p=st.pages[i];
  const vp=p.getViewport({scale:st.scale,rotation:st.rotation+(p.rotate||0)});
  const slot=document.createElement('div'); slot.className='pdfv-slot';
  slot.style.width=vp.width+'px'; slot.style.height=vp.height+'px';
  slot.innerHTML='<canvas class="pdfv-cv"></canvas><div class="pdfv-text"></div>';
  cont.appendChild(slot);
  st.slots.push({el:slot,cv:slot.querySelector('canvas'),textEl:slot.children[1],
   rendered:false,rendering:false,task:null,w:vp.width,h:vp.height});
  st.tops.push(y); st.heights.push(vp.height);
  y+=vp.height+GAP;
 }
}
function applyZoom(st: any){
 if(!st.doc||st.destroyed) return;
 const sc=st.els.scroll;
 const bs=baseSize(st);
 st.scale=pdfResolveScale(st.mode,st.pct,Math.max(0,sc.clientWidth-30),Math.max(0,sc.clientHeight-30),bs.w,bs.h);
 st.gen++;
 st.slots.forEach((s: any)=>{ if(s.task){ try{s.task.cancel()}catch(e){} } s.rendered=false; s.rendering=false; });
 layoutSlots(st);
 paintZoom(st); paintPageNum(st);
 sc.scrollTop=Math.max(0,(st.tops[st.cur]||0)-14);
 keepRenderWindow(st);
}
function paintZoom(st: any){
 st.els.zseg.querySelectorAll('button').forEach((x: any)=>x.classList.toggle('on',x.dataset.m===st.mode));
 st.els.zpct.textContent=st.mode==='percent'?(Math.round(st.scale*100)+'%')
  :st.mode==='actual'?'100%':st.mode==='fit-page'?'适页':'适宽';
 st.els.zpct.title='当前缩放 '+Math.round(st.scale*100)+'%';
}
const ZOOM_STEPS=[10,25,50,75,100,125,150,200,300,400,600,800];
function zoomStep(st: any, dir: number){
 const curPct=st.scale*100;
 let next=ZOOM_STEPS[dir>0?ZOOM_STEPS.length-1:0];
 if(dir>0){ for(const s of ZOOM_STEPS){ if(s>curPct+0.5){ next=s; break; } } }
 else { for(let i=ZOOM_STEPS.length-1;i>=0;i--){ if(ZOOM_STEPS[i]<curPct-0.5){ next=ZOOM_STEPS[i]; break; } } }
 st.mode='percent'; st.pct=next; applyZoom(st);
}
function rotateDoc(st: any){
 st.rotation=pdfRotateNext(st.rotation);
 st.thumbDone=new Set();
 applyZoom(st);
 buildThumbs(st);
 markThumbCur(st);
}
function toggleSide(st: any){
 st.side=!st.side;
 st.els.side.style.display=st.side?'':'none';
 if(st.mode!=='percent'&&st.mode!=='actual') applyZoom(st); else keepRenderWindow(st);
}

/* ---------- scrolling / pages ---------- */
function gotoPage(st: any, i: number){
 if(!st.numPages) return;
 i=Math.max(0,Math.min(st.numPages-1,i));
 st.cur=i; paintPageNum(st); markThumbCur(st);
 st.els.scroll.scrollTop=Math.max(0,(st.tops[i]||0)-14);
 keepRenderWindow(st);
}
function onScroll(st: any){
 if(st.scrollRaf) return;
 st.scrollRaf=requestAnimationFrame(()=>{
  st.scrollRaf=0; if(st.destroyed||!st.numPages) return;
  const cur=pdfPageAtScroll(st.tops,st.heights,st.els.scroll.scrollTop,st.els.scroll.clientHeight);
  if(cur!==st.cur){ st.cur=cur; paintPageNum(st); markThumbCur(st); }
  keepRenderWindow(st);
 });
}
function paintPageNum(st: any){
 if(document.activeElement!==st.els.pageIn) st.els.pageIn.value=st.numPages?String(st.cur+1):'–';
}
function commitPageInput(st: any){
 const v=parseInt(st.els.pageIn.value,10);
 if(!isNaN(v)) gotoPage(st,v-1);
 paintPageNum(st);
 st.els.scroll.focus();
}
function keepRenderWindow(st: any){
 if(!st.numPages||st.destroyed) return;
 const want=new Set(pdfRenderWindow(st.cur,st.numPages,3));
 st.slots.forEach((s: any,i: number)=>{
  if(want.has(i)) renderPage(st,i);
  else if(s.rendered||s.rendering) unrenderPage(st,i);
 });
}
async function renderPage(st: any, i: number){
 const s=st.slots[i]; if(!s||s.rendered||s.rendering||st.destroyed) return;
 s.rendering=true;
 const gen=st.gen;
 try{
  const page=st.pages[i];
  const vp=page.getViewport({scale:st.scale,rotation:st.rotation+(page.rotate||0)});
  const dpr=window.devicePixelRatio||1;
  s.cv.width=Math.max(1,Math.round(vp.width*dpr));
  s.cv.height=Math.max(1,Math.round(vp.height*dpr));
  s.cv.style.width=vp.width+'px'; s.cv.style.height=vp.height+'px';
  const task=page.render({canvasContext:s.cv.getContext('2d'),viewport:vp,
   transform:dpr!==1?[dpr,0,0,dpr,0,0]:undefined});
  s.task=task;
  await task.promise;
  if(st.destroyed||gen!==st.gen) return;
  s.rendered=true;
  const tc=await pageText(st,i);
  if(st.destroyed||gen!==st.gen) return;
  paintTextDivs(st,i,vp,tc);
 }catch(e){
  if(!(e&&e.name==='RenderingCancelledException')&&gen===st.gen){ /* stays unrendered; the next window pass retries */ }
 }
 if(gen===st.gen){ s.rendering=false; s.task=null; }
}
function unrenderPage(st: any, i: number){
 const s=st.slots[i];
 if(s.task){ try{s.task.cancel()}catch(e){} s.task=null; }
 s.rendered=false; s.rendering=false;
 s.cv.width=0; s.cv.height=0;
 s.textEl.innerHTML='';
}

/* ---------- text layer + find ----------
   The text layer mirrors pdf.js's own TextLayer math
   (Util.transform of the item matrix, font height from the
   matrix, canvas-measured scaleX, ascent ratio from the font's
   bounding box) so selection lands on the glyphs; the divs are
   ours so find matches can be wrapped in <mark> per item. */
let measureCv: any=null;
function measureCtx(){ if(!measureCv) measureCv=document.createElement('canvas'); return measureCv.getContext('2d'); }
const ascentCache=new Map();
function fontAscentRatio(family: string){
 if(ascentCache.has(family)) return ascentCache.get(family);
 let ratio=0.8;
 try{
  const ctx=measureCtx(); ctx.font='30px '+family;
  const m=ctx.measureText('');
  const a=m.fontBoundingBoxAscent, d=Math.abs(m.fontBoundingBoxDescent||0);
  if(a&&a+d>0) ratio=a/(a+d);
 }catch(e){}
 ascentCache.set(family,ratio);
 return ratio;
}
async function pageText(st: any, i: number){
 let tc=st.textCache.get(i);
 if(tc) return tc;
 const raw=await st.pages[i].getTextContent();
 const items: any[]=[]; const starts: number[]=[]; let full='';
 raw.items.forEach((it: any)=>{ if(it&&typeof it.str==='string'){ starts.push(full.length); full+=it.str; items.push(it); } });
 tc={raw,items,starts,full};
 st.textCache.set(i,tc);
 return tc;
}
function paintTextDivs(st: any, i: number, vp: any, tc: any){
 const s=st.slots[i]; if(!s) return;
 const box=s.textEl; box.innerHTML='';
 box.style.width=vp.width+'px'; box.style.height=vp.height+'px';
 const U=st.lib.Util;
 const marks=st.find.byPage.get(i)||[];
 const ctx=measureCtx();
 tc.items.forEach((item: any,k: number)=>{
  const tx=U.transform(vp.transform,item.transform);
  const style=(tc.raw.styles&&tc.raw.styles[item.fontName])||{};
  const family=style.fontFamily||'sans-serif';
  const fontHeight=Math.hypot(tx[2],tx[3]);
  let angle=Math.atan2(tx[1],tx[0]);
  if(style.vertical) angle+=Math.PI/2;
  const ascent=fontHeight*fontAscentRatio(family);
  const div=document.createElement('div');
  div.className='pdfv-td';
  div.setAttribute('role','presentation');
  const start=tc.starts[k], str=item.str;
  let html='', pos=0;
  for(const m of marks){
   const ms=Math.max(m.s,start), me=Math.min(m.e,start+str.length);
   if(me<=ms) continue;
   const a=ms-start, bb=me-start;
   if(a>pos) html+=esc(str.slice(pos,a));
   html+='<mark class="'+(m.flat===st.find.cur?'cur':'')+'">'+esc(str.slice(a,bb))+'</mark>';
   pos=bb;
  }
  div.innerHTML=html+esc(str.slice(pos));
  div.style.fontSize=fontHeight+'px';
  div.style.fontFamily=family;
  if(angle===0){ div.style.left=tx[4]+'px'; div.style.top=(tx[5]-ascent)+'px'; }
  else { div.style.left=(tx[4]+ascent*Math.sin(angle))+'px'; div.style.top=(tx[5]-ascent*Math.cos(angle))+'px'; }
  let transform='';
  const shouldScale=str.length>1||(str!==' '&&item.transform[0]!==item.transform[3]);
  if(shouldScale){
   try{
    ctx.font=fontHeight+'px '+family;
    const w=ctx.measureText(str).width;
    if(w>0) transform+='scaleX('+((style.vertical?item.height:item.width)*vp.scale/w)+') ';
   }catch(e){}
  }
  if(angle!==0) transform+='rotate('+angle+'rad)';
  if(transform) div.style.transform=transform.trim();
  box.appendChild(div);
  if(item.hasEOL) box.appendChild(document.createElement('br'));
 });
}
function refreshMarks(st: any){
 st.slots.forEach((s: any,i: number)=>{
  if(!s.rendered) return;
  const tc=st.textCache.get(i); if(!tc) return;
  const page=st.pages[i];
  const vp=page.getViewport({scale:st.scale,rotation:st.rotation+(page.rotate||0)});
  paintTextDivs(st,i,vp,tc);
 });
}
function openFind(st: any){
 st.find.open=true;
 st.els.findBar.style.display='';
 st.els.fq.focus(); st.els.fq.select();
 if(st.find.q&&st.find.q!==st.els.fq.value) runFind(st);
}
function closeFind(st: any){
 st.find.open=false; st.find.q=''; st.find.flat=[]; st.find.byPage=new Map(); st.find.cur=-1;
 st.els.findBar.style.display='none';
 st.els.fq.value=''; paintFind(st);
 refreshMarks(st);
 st.els.scroll.focus();
}
async function runFind(st: any){
 const q=st.els.fq.value;
 st.find.q=q; st.find.byPage=new Map(); st.find.flat=[]; st.find.cur=-1;
 if(!q){ paintFind(st); refreshMarks(st); return; }
 st.find.busy=true; paintFind(st);
 for(let i=0;i<st.numPages;i++){
  if(st.destroyed||st.find.q!==q) return;
  const tc=await pageText(st,i);
  if(st.destroyed||st.find.q!==q) return;
  const ms=pdfFindMatches(tc.full,q);
  if(ms.length){
   const arr: any[]=[];
   for(const m of ms){ arr.push({s:m.s,e:m.e,flat:st.find.flat.length}); st.find.flat.push({page:i,s:m.s,e:m.e}); }
   st.find.byPage.set(i,arr);
  }
 }
 st.find.busy=false;
 paintFind(st); refreshMarks(st);
 if(st.find.flat.length){
  let idx=st.find.flat.findIndex((m: any)=>m.page>=st.cur);
  gotoMatch(st,idx<0?0:idx);
 }
}
function gotoMatch(st: any, idx: number){
 if(idx<0||idx>=st.find.flat.length) return;
 st.find.cur=idx;
 gotoPage(st,st.find.flat[idx].page);
 refreshMarks(st);
 paintFind(st);
}
function stepFind(st: any, dir: number){
 const idx=pdfFindStep(st.find.cur,st.find.flat.length,dir);
 if(idx>=0) gotoMatch(st,idx);
}
function paintFind(st: any){
 const f=st.find;
 st.els.fcount.textContent=f.busy?'查找中…':!f.q?'':!f.flat.length?'无结果':((f.cur+1)+' / '+f.flat.length);
}

/* ---------- sidebar: outline + thumbnails ---------- */
async function buildOutline(st: any){
 let ol: any=null;
 try{ ol=await st.doc.getOutline(); }catch(e){}
 const box=st.els.ol; if(!box) return;
 box.innerHTML='';
 if(!ol||!ol.length){ box.style.display='none'; return; }
 const add=(items: any[],depth: number)=>{
  for(const it of items){
   const row=document.createElement('div');
   row.className='pdfv-olr';
   row.style.paddingLeft=(8+depth*14)+'px';
   row.textContent=it.title||'（无题）';
   row.title=row.textContent;
   row.onclick=async()=>{ const idx=await resolveDest(st,it.dest); if(idx>=0) gotoPage(st,idx); };
   box.appendChild(row);
   if(it.items&&it.items.length) add(it.items,depth+1);
  }
 };
 add(ol,0);
}
async function resolveDest(st: any, dest: any){
 try{
  let d=dest;
  if(typeof d==='string') d=await st.doc.getDestination(d);
  if(!d||!d.length) return -1;
  const ref=d[0];
  if(typeof ref==='number') return ref;
  return await st.doc.getPageIndex(ref);
 }catch(e){ return -1; }
}
function buildThumbs(st: any){
 const box=st.els.thumbs; if(!box) return;
 box.innerHTML=''; st.thumbEls=[];
 for(let i=0;i<st.numPages;i++){
  const th=document.createElement('div');
  th.className='pdfv-th'+(i===st.cur?' cur':'');
  th.innerHTML='<canvas></canvas><span>'+(i+1)+'</span>';
  th.onclick=()=>gotoPage(st,i);
  box.appendChild(th);
  st.thumbEls.push(th);
 }
 if(st.thumbObs) st.thumbObs.disconnect();
 if(typeof IntersectionObserver!=='undefined'){
  st.thumbObs=new IntersectionObserver((es: any[])=>{
   es.forEach((en: any)=>{ if(en.isIntersecting){ const i=st.thumbEls.indexOf(en.target); if(i>=0) renderThumb(st,i); } });
  },{root:st.els.side,rootMargin:'240px'});
  st.thumbEls.forEach((th: any)=>st.thumbObs.observe(th));
 }
}
async function renderThumb(st: any, i: number){
 if(st.thumbDone.has(i)||st.destroyed) return;
 st.thumbDone.add(i);
 try{
  const page=st.pages[i];
  const rot=st.rotation+(page.rotate||0);
  const vp1=page.getViewport({scale:1,rotation:rot});
  const vp=page.getViewport({scale:132/vp1.width,rotation:rot});
  const cv=st.thumbEls[i].querySelector('canvas');
  const dpr=window.devicePixelRatio||1;
  cv.width=Math.max(1,Math.round(vp.width*dpr));
  cv.height=Math.max(1,Math.round(vp.height*dpr));
  cv.style.width=vp.width+'px'; cv.style.height=vp.height+'px';
  await page.render({canvasContext:cv.getContext('2d'),viewport:vp,
   transform:dpr!==1?[dpr,0,0,dpr,0,0]:undefined}).promise;
 }catch(e){ st.thumbDone.delete(i); }
}
function markThumbCur(st: any){
 st.thumbEls.forEach((th: any,i: number)=>th.classList.toggle('cur',i===st.cur));
 /* Keep the current thumb visible by scrolling the SIDEBAR
    only (scrollIntoView would also drag ancestor scrollers). */
 const th=st.thumbEls[st.cur], side=st.els.side;
 if(!th||!side||!st.side) return;
 const top=th.offsetTop, bottom=top+th.offsetHeight;
 if(top<side.scrollTop) side.scrollTop=top-4;
 else if(bottom>side.scrollTop+side.clientHeight) side.scrollTop=bottom-side.clientHeight+4;
}

/* ---------- keyboard ---------- */
function pdfKeys(st: any, ev: any){
 const t=ev.target;
 if(t===st.els.pwIn||t===st.els.pwBar) return;
 if((ev.metaKey||ev.ctrlKey)&&(ev.key==='f'||ev.key==='F')){ ev.preventDefault(); openFind(st); return; }
 if(t===st.els.fq){
  if(ev.key==='Enter'){ ev.preventDefault(); stepFind(st,ev.shiftKey?-1:1); }
  else if(ev.key==='Escape'){ ev.preventDefault(); closeFind(st); }
  return;
 }
 if(t===st.els.pageIn) return;
 if(ev.key==='Escape'&&st.find.open){ closeFind(st); return; }
 if(ev.metaKey||ev.ctrlKey){
  if(ev.key==='+'||ev.key==='='){ ev.preventDefault(); zoomStep(st,1); }
  else if(ev.key==='-'||ev.key==='_'){ ev.preventDefault(); zoomStep(st,-1); }
  else if(ev.key==='0'){ ev.preventDefault(); st.mode=pdfZoomDefault(); applyZoom(st); }
  return;
 }
 const sc=st.els.scroll;
 switch(ev.key){
  case 'PageDown': ev.preventDefault(); sc.scrollBy({top:sc.clientHeight*0.9}); break;
  case 'PageUp': ev.preventDefault(); sc.scrollBy({top:-sc.clientHeight*0.9}); break;
  case 'ArrowDown': ev.preventDefault(); sc.scrollBy({top:90}); break;
  case 'ArrowUp': ev.preventDefault(); sc.scrollBy({top:-90}); break;
  case 'Home': ev.preventDefault(); gotoPage(st,0); break;
  case 'End': ev.preventDefault(); gotoPage(st,st.numPages-1); break;
 }
}
