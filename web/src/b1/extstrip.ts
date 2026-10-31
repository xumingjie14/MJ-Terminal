/* Generated cross-module imports (v0.58 modularization):
   every name below is owned by the sibling module it is imported
   from -- the same names the pre-modular page shared through one
   script scope. */
import { api, esc, extCards, extLabelEl, extLive, thumbBad, verQ, __bind_extLabelEl, __bind_extLive } from './core';
import { SETTINGS } from './settings';
import { openApp, syncDots } from './windows';
import { onWinClosed, stageOn } from './stage';
import { cleanWinTitle, webIconHTML } from './entries';
import { ctxOpen } from './ctxmenu';
/* ----- external app windows in the stage strip (v0.18 窗口统一·乙) -----
   The daemon registers the independent Chrome app windows it launches
   (YouTube, X, ...). With Stage Manager on they appear as static icon
   cards in a group at the left, under the in-system thumbnails;
   clicking one asks the daemon to bring that real window to front. */
/* v0.56 item 1: two card shapes, one setting (stageDisplay,
   default preview). Preview cards show the daemon's real
   window snapshot (PrintWindow on the host, /api/extwin/thumb);
   identity cards lead with the cleaned page title and the
   real app icon -- never the old identical letter tiles for
   browser windows.
   TEST2 rework (field report: preview never loaded): test1's
   <img src> + onerror design was doomed -- the daemon answered
   a window's FIRST request 404 (no frame cached yet) and the
   view then blacklisted the window for the whole session, so
   no preview ever appeared. The daemon now captures the first
   frame synchronously, and the view loads previews with
   fetch(): a refusal carries X-Thumb-Reason -- "minimized" is
   transient (this round just shows identity; the next poll
   retries), every other reason is sticky for the session
   (thumbBad: no more requests for that window, no 404 spam).
   TEST3 rework (field report: previews flicker, lag ~4 s, and
   wide/narrow windows distort): cards are built ONCE per
   window and mutated in place -- a poll never rebuilds the
   strip. Frames are versioned (ETag): the loader sends
   If-None-Match, a 304 leaves the DOM untouched, and a new
   frame is preloaded off-DOM and swapped in only after it has
   loaded (the old frame stays visible until then -- no blank
   flash, no thumb/identity flapping). While the strip is open
   the poll runs at ~1 s with live=1 (the daemon shortens its
   freshness window to match) and the opening asks every card
   for one fresh frame; strip closed, the loop falls back to
   the lazy 4 s poll and the daemon captures nothing in the
   background. The media box is fixed-size and the frame
   renders object-fit:contain -- never stretched, never
   cropped; caption rows have fixed heights, so a frame
   arriving never shifts the layout.
   (thumbBad / extCards / extLabelEl / extLive are declared
   at the very top of this script block -- refreshExtStrip
   can run during initial evaluation; see the note there.) */
export function extPreviewOn(){ return !SETTINGS||SETTINGS.stageDisplay!=='identity'; }
export function extCardClass(x,prev){
 return 'extcard'+(prev?' prev':'')+(x.active?' active':'')+(x.minimized?' mini':'');
}
export function extCardInnerHTML(x,prev){
 const title=cleanWinTitle(x.title)||x.app||'应用';
 if(prev) return `<img class="ethumb" alt=""><span class="ecap">${extIconHTML(x)}<b>${esc(title)}</b></span>`;
 return `<span class="actdot"></span>${extIconHTML(x)}<span class="et"><b>${esc(title)}</b><span>${esc(x.app||'')}</span></span>`;
}
export function extCardHTML(x,preview){
 const prev=preview&&!thumbBad.has(x.hwnd);
 return `<button class="${extCardClass(x,prev)}" data-hwnd="${x.hwnd}">${extCardInnerHTML(x,prev)}</button>`;
}
export function extIconHTML(x){
 if(x.ikind==='firefox') return `<img class="extRealIcon" alt="" src="${verQ('/api/firefox/icon')}" onerror="this.remove()">`;
 if(x.ikind==='chrome') return `<img class="extRealIcon" alt="" src="${verQ('/api/chrome/icon')}" onerror="this.remove()">`;
 return webIconHTML({name:x.app,url:x.url||''},true);
}
/* v0.56 test2 item 3: right-click on a strip card -- the
   per-window menu. External windows get the three operations
   the winmgr honestly has (focus / minimize / WM_CLOSE). */
export function showExtCtx(x,y,hwnd){
 const items=[
  {act:'focus',label:'切换到该窗口',icon:'switch'},
  {act:'min',label:'最小化',icon:'minimize'},
  {act:'close',label:'关闭窗口',icon:'closex',danger:true},
 ];
 ctxOpen(items,x,y,async act=>{
  const ep=act==='focus'?'focus':act==='min'?'minimize':'close';
  await api('/api/extwin/'+ep,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({hwnd})}).catch(()=>null);
  setTimeout(refreshExtStrip,350);
 });
}
/* Internal stages: while a window is shrunk into the strip it
   supports exactly 切换到 (the same as clicking it) and 关闭
   (the same as its red light). Nothing else is offered --
   no fake entries. */
export function showStageCtx(x,y,app){
 const items=[
  {act:'front',label:'切换到',icon:'switch'},
  {act:'close',label:'关闭',icon:'closex',danger:true},
 ];
 ctxOpen(items,x,y,act=>{
  if(act==='front'){ openApp(app.id); return; }
  const w=document.getElementById('win-'+app.id);
  if(w){ w.classList.add('hidden'); onWinClosed(app.id); syncDots(); }
 });
}
/* One card's frame pump (test3): conditional (If-None-Match),
   double-buffered. A 304 means the pixels we already show are
   current -- the DOM is not touched. A 200 is preloaded
   off-DOM and swapped in only once loaded. "minimized" parks
   the card in identity shape for now (minWait) but keeps
   pumping on later polls; any other refusal is session-sticky
   via thumbBad. */
export async function pumpThumb(card){
 if(card.busy) return;
 card.busy=true;
 try{
  const headers={};
  if(card.ver) headers['If-None-Match']=card.ver;
  let q='/api/extwin/thumb?hwnd='+card.hwnd;
  if(extLive) q+='&live=1';
  if(card.needFresh){ q+='&fresh=1'; card.needFresh=false; }
  const r=await fetch(q,{headers:headers,cache:'no-store'});
  if(r.status===304){ card.minWait=false; }
  else if(r.ok){
   const ver=(r.headers&&r.headers.get('ETag'))||'';
   const blob=await r.blob();
   const u=URL.createObjectURL(blob);
   const ok=await new Promise(res=>{ const im=new Image(); im.onload=()=>res(true); im.onerror=()=>res(false); im.src=u; });
   if(ok){
    const old=card.blob;
    card.blob=u; card.ver=ver; card.minWait=false;
    if(card.img) card.img.src=u;
    if(old) URL.revokeObjectURL(old);
   } else URL.revokeObjectURL(u);
  } else {
   const reason=(r.headers&&r.headers.get('X-Thumb-Reason'))||'';
   if(reason==='minimized'){ card.minWait=true; }
   else if(reason){ thumbBad.add(card.hwnd); }
   /* no reason (a bare network failure): retry next poll */
  }
 }catch(e){ /* network hiccup: retry next poll, not sticky */ }
 card.busy=false;
}
/* Mutate one card in place: rebuild its inner structure ONLY
   when the shape (preview/identity) or the icon identity
   changes; otherwise touch just the classes and the texts
   that actually changed. */
export function paintExtCard(card,x,prev){
 const iconKey=(x.ikind||'')+'|'+(x.app||'')+'|'+(x.url||'');
 if(card.prev!==prev||card.iconKey!==iconKey){
  card.el.innerHTML=extCardInnerHTML(x,prev);
  card.prev=prev; card.iconKey=iconKey;
  card.img=card.el.querySelector('img.ethumb');
  card.capB=card.el.querySelector('.ecap b')||card.el.querySelector('.et b');
  card.subB=card.el.querySelector('.et span');
  card.title=null; card.sub=null;
  if(prev&&card.img&&card.blob) card.img.src=card.blob; /* frame parked while the card wore identity shape */
 }
 const cls=extCardClass(x,prev);
 if(card.cls!==cls){ card.cls=cls; card.el.className=cls; }
 const title=cleanWinTitle(x.title)||x.app||'应用';
 if(card.capB&&card.title!==title){ card.capB.textContent=title; card.title=title; }
 if(card.subB&&card.sub!==x.app){ card.subB.textContent=x.app||''; card.sub=x.app||''; }
}
export function newExtCard(x,prev){
 const el=document.createElement('button');
 el.dataset.hwnd=x.hwnd;
 const card={hwnd:x.hwnd,el:el,prev:null,iconKey:'',img:null,capB:null,subB:null,cls:'',title:null,sub:null,ver:'',blob:'',busy:false,minWait:false,needFresh:true};
 el.onclick=async()=>{
  const r=await api('/api/extwin/focus',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({hwnd:card.hwnd})}).catch(()=>null);
  if(!r||r.error) refreshExtStrip(); /* window closed or focus failed: re-sync the strip */
 };
 el.oncontextmenu=ev=>{ ev.preventDefault(); ev.stopPropagation(); showExtCtx(ev.clientX,ev.clientY,card.hwnd); };
 paintExtCard(card,x,prev);
 return card;
}
export function clearExtCards(box){
 extCards.forEach(card=>{ if(card.blob) URL.revokeObjectURL(card.blob); });
 extCards.clear();
 __bind_extLabelEl.set(null);
 box.innerHTML='';
}
export async function refreshExtStrip(){
 const box=document.getElementById('extstrip'); if(!box) return;
 if(!stageOn()){ box.classList.add('hidden'); clearExtCards(box); __bind_extLive.set(false); return; }
 let list=[];
 try{ const r=await api('/api/extwin'); list=(r.windows||[]).filter(x=>!x.shell); }catch(e){}
 if(!list.length){ box.classList.add('hidden'); clearExtCards(box); __bind_extLive.set(false); return; }
 const preview=extPreviewOn();
 if(!extLabelEl){ __bind_extLabelEl.set(document.createElement('div')); extLabelEl.className='extlab'; extLabelEl.textContent='外部应用 · 点一下切过去'; }
 if(box.children[0]!==extLabelEl) box.insertBefore(extLabelEl,box.children[0]||null);
 box.classList.remove('hidden');
 const seen=new Set();
 list.forEach((x,i)=>{
  seen.add(x.hwnd);
  let card=extCards.get(x.hwnd);
  const wantPrev=preview&&!thumbBad.has(x.hwnd);
  if(!card){ card=newExtCard(x,wantPrev); extCards.set(x.hwnd,card); }
  paintExtCard(card,x,wantPrev&&!card.minWait);
  const ref=box.children[i+1]||null;
  if(ref!==card.el) box.insertBefore(card.el,ref);
  if(wantPrev) pumpThumb(card);
 });
 Array.from(extCards).forEach(([hwnd,card])=>{
  if(seen.has(hwnd)) return;
  if(card.blob) URL.revokeObjectURL(card.blob);
  if(card.el.parentNode) card.el.parentNode.removeChild(card.el);
  else if(box.contains(card.el)) box.removeChild(card.el); /* v0.58: was box.children.indexOf(card.el) -- HTMLCollection has no indexOf in a real browser (the fake-DOM suites use arrays, which masked it); contains() is the intended check, cards are direct children of the strip */
  extCards.delete(hwnd);
 });
 /* Live mode = strip rendered + the view itself visible. On
    the closed->open transition every card asks for one fresh
    frame instead of greeting the user with pre-open pictures. */
 const liveNow=!document.hidden;
 if(liveNow&&!extLive) extCards.forEach(c=>{ c.needFresh=true; });
 __bind_extLive.set(liveNow);
}
/* The strip's heartbeat (test3): ~1 s while live, the old
   lazy 4 s otherwise. refreshExtStrip itself reconciles the
   card set; this loop only paces it. */
(function extLoop(){
 setTimeout(async()=>{ try{ await refreshExtStrip(); }catch(e){} extLoop(); }, extLive?1000:4000);
})();
