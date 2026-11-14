/* Generated cross-module imports (v0.58 modularization):
   every name below is owned by the sibling module it is imported
   from -- the same names the pre-modular page shared through one
   script scope. */
import { api, esc, extCards, extLabelEl, extLive, thumbBad, verQ, __bind_extLabelEl, __bind_extLive } from './core';
import { SETTINGS } from './settings';
import { openApp, syncDots } from './windows';
import { onWinClosed, stageOn } from './stage';
import { cleanWinTitle, webIconHTML } from './entries';
import { ctxOpen, loadWebApps } from './ctxmenu';
import { renderDesk } from './desktop';
import { toast } from './appsload';
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
/* v0.71 test3 (layer 1, L1-B2): cards are keyed by a STABLE
   string, not by the window handle: 'h:<hwnd>' while the app
   has a window, 'u:<lockURL>' while it does not. The same app
   keeps the same DOM node across its window's death and
   rebirth -- refreshExtStrip migrates the node between the
   two keys instead of removing one card and inserting
   another (the test2 "card vanishes, then pops back" feel is
   exactly what this layer exists to kill). */
export function extCardKey(x){ return x.hwnd>0?('h:'+x.hwnd):('u:'+(x.lockurl||x.url||'')); }
/* The daemon-derived resident state, in the card's existing
   caption/sub line (L1-S1): short release-note words, no new
   visual elements. */
export function extStateLabel(x){
 switch(x.resident){
  case 'back': return '后台运行中';
  case 'restoring': return '恢复中';
  case 'unstable': return '多次恢复未成功';
  default: return '';
 }
}
export function extCardClass(x,prev){
 return 'extcard'+(prev?' prev':'')+(x.active?' active':'')+(x.minimized?' mini':'');
}
export function extCardInnerHTML(x,prev){
 const title=cleanWinTitle(x.title)||x.app||'应用';
 const state=extStateLabel(x);
 if(prev) return `<img class="ethumb" alt=""><span class="ecap">${extIconHTML(x)}<b>${esc(title)}</b>${state?`<span class="estat">${esc(state)}</span>`:''}</span>`;
 const sub=state?((x.app?x.app+' · ':'')+state):(x.app||'');
 return `<span class="actdot"></span>${extIconHTML(x)}<span class="et"><b>${esc(title)}</b><span>${esc(sub)}</span></span>`;
}
export function extCardHTML(x,preview){
 const prev=preview&&x.hwnd>0&&!thumbBad.has(extCardKey(x));
 return `<button class="${extCardClass(x,prev)}" data-hwnd="${x.hwnd}">${extCardInnerHTML(x,prev)}</button>`;
}
export function extIconHTML(x){
 if(x.ikind==='firefox') return `<img class="extRealIcon" alt="" src="${verQ('/api/firefox/icon')}" onerror="this.remove()">`;
 if(x.ikind==='chrome') return `<img class="extRealIcon" alt="" src="${verQ('/api/chrome/icon')}" onerror="this.remove()">`;
 return webIconHTML({name:x.app,url:x.url||''},true);
}
/* v0.56 test2 item 3: right-click on a strip card -- the
   per-window menu. External windows get the operations the
   winmgr honestly has (focus / minimize / WM_CLOSE).
   v0.62 (detail-fix ③): the menu never actually opened in the
   field -- this call passed (items, x, y) into ctxOpen(x, y,
   items, onPick), so items.map threw on a number and the
   handler died before painting; the argument order is fixed.
   The menu also gains 隐藏其他窗口 (focusonly: stage-focus to
   this window, the others hide as an explicit stage action --
   stageguard.go). extCtxEndpoint is factored out so the JS
   suite can pin the act -> endpoint mapping. */
export function extCtxEndpoint(act){
 return act==='focus'?'focus':act==='focusonly'?'focusonly':act==='min'?'minimize':'close';
}
export function showExtCtx(x,y,key){
 const card=extCards.get(key);
 if(!card||!card.win) return;
 const w=card.win;
 /* v0.71 test3: a windowless (lock-pinned) card has exactly
    two honest actions -- bring the app back now, or unlock
    it. No window operations exist for a window that is not
    there. */
 if(card.hwnd===0){
  const items=[
   {act:'restore',label:'立即恢复',icon:'switch'},
   {act:'lockbg',label:'取消锁住后台',icon:'lock'},
  ];
  ctxOpen(x,y,items,async act=>{
   if(act==='restore'){
    const r=await api('/api/webapps/restore',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({url:w.url})}).catch(()=>null);
    if(r&&r.error){ toast(r.error); return; }
    toast('正在恢复「'+(w.app||'应用')+'」');
    setTimeout(refreshExtStrip,350);
    return;
   }
   if(act==='lockbg'){
    const r=await api('/api/webapps/lock',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({url:w.url,on:false})}).catch(()=>null);
    if(!r||r.error){ toast((r&&r.error)||'设置失败'); return; }
    toast('已取消锁住后台');
    setTimeout(refreshExtStrip,350);
   }
  });
  return;
 }
 const hwnd=card.hwnd;
 const items: any[]=[
  {act:'focus',label:'切换到该窗口',icon:'switch'},
  {act:'focusonly',label:'隐藏其他窗口',icon:'eyeoff'},
  {act:'min',label:'最小化',icon:'minimize'},
 ];
 /* v0.69 block 2, entry A: an app-mode window can be pinned
    into the WEBAPPS ledger as a desktop app. The pin posts
    only the handle -- the daemon knows the URL and title.
    Handled here, NOT through extCtxEndpoint (whose mapping
    the suites pin and whose fallthrough is 'close'). */
 if(w.appwindow&&w.url) items.push({act:'pin',label:'固定为桌面应用',icon:'pin'});
 /* v0.71 block 2: 锁住后台 -- the same app-window condition.
    The label flips with the daemon-reported lock state; the
    toggle posts the URL (the ledger identity) to
    /api/webapps/lock, also handled outside extCtxEndpoint.
    v0.71 review fix: a patrol-claimed window (daemon restarted
    under a running browser) has no site geometry key, so it
    never passes the appwindow test -- but when the daemon has
    aligned it to a locked app it reports locked=true with the
    lock URL filled in, and 取消锁住后台 must be reachable. */
 if(w.url&&(w.appwindow||w.locked)) items.push({act:'lockbg',label:w.locked?'取消锁住后台':'锁住后台',icon:'lock'});
 /* v0.71 test3 (L2-I4): a locked app's menu entry is
    隐藏到后台， not 关闭窗口 -- the daemon translates the
    close either way, and the menu must not promise a death
    the lock forbids. Unlocking first is the only real close. */
 if(w.locked) items.push({act:'close',label:'隐藏到后台',icon:'eyeoff'});
 else items.push({act:'close',label:'关闭窗口',icon:'closex',danger:true});
 ctxOpen(x,y,items,async act=>{
  if(act==='pin'){
   const r=await api('/api/webapps/pin',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({hwnd})}).catch(()=>null);
   if(r&&r.error){ toast(r.error); return; }
   await loadWebApps(); renderDesk();
   toast('已固定为桌面应用');
   return;
  }
  if(act==='lockbg'){
   const c2=extCards.get(key);
   const url=c2&&c2.win&&c2.win.url;
   if(!url){ toast('这个窗口已经关掉了'); return; }
   const on=!(c2.win.locked);
   const r=await api('/api/webapps/lock',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({url,on})}).catch(()=>null);
   if(!r||r.error){ toast((r&&r.error)||'设置失败'); return; }
   toast(on?'已锁住后台：关掉窗口会转入后台继续运行，点台前调度的卡片即可回来，通知不会断':'已取消锁住后台：之后关窗即真关');
   setTimeout(refreshExtStrip,350);
   return;
  }
  await api('/api/extwin/'+extCtxEndpoint(act),{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({hwnd})}).catch(()=>null);
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
 /* v0.62: same swapped-argument fix as showExtCtx above --
    this menu never opened either. */
 ctxOpen(x,y,items,act=>{
  if(act==='front'){ openApp(app.id); return; }
  const w=document.getElementById('win-'+app.id);
  if(w){ w.classList.add('hidden'); onWinClosed(app.id); syncDots(); if(typeof app.onclose==='function') app.onclose(); }
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
 if(card.busy||card.hwnd===0) return; /* a windowless card has no window to photograph */
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
   /* v0.71 test3 T-I1: a daemon-hidden window (X-Thumb-Reason:
      hidden) is transient exactly like a minimized one -- it
      comes back, so park on the last frame instead of banning
      the preview. thumbBad keys are card keys now
      ('h:<hwnd>'), so a ban follows the card, not the handle. */
   if(reason==='minimized'||reason==='hidden'){ card.minWait=true; }
   else if(reason){ thumbBad.add(card.key); }
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
 card.win=x; /* v0.69: the latest window record rides the card so the right-click menu knows whether this window can be pinned as a desktop app */
 const iconKey=(x.ikind||'')+'|'+(x.app||'')+'|'+(x.url||'');
 /* v0.71 test3: the resident state is part of the structural
    key -- a state change must repaint even when the shape and
    the icon do not change. */
 const state=extStateLabel(x);
 if(card.prev!==prev||card.iconKey!==iconKey||card.stateTxt!==state){
  card.el.innerHTML=extCardInnerHTML(x,prev);
  card.prev=prev; card.iconKey=iconKey; card.stateTxt=state;
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
 const sub=state?((x.app?x.app+' · ':'')+state):(x.app||'');
 if(card.subB&&card.sub!==sub){ card.subB.textContent=sub; card.sub=sub; }
}
export function newExtCard(x,prev){
 const el=document.createElement('button');
 const key=extCardKey(x);
 el.dataset.key=key;
 if(x.hwnd>0) el.dataset.hwnd=x.hwnd;
 const card={key:key,hwnd:x.hwnd,lockURL:x.lockurl||'',win:null,el:el,prev:null,iconKey:'',stateTxt:null,img:null,capB:null,subB:null,cls:'',title:null,sub:null,ver:'',blob:'',busy:false,minWait:false,needFresh:x.hwnd>0};
 el.onclick=async()=>{
  if(card.hwnd>0){
   const r=await api('/api/extwin/focus',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({hwnd:card.hwnd})}).catch(()=>null);
   if(!r||r.error) refreshExtStrip(); /* window closed or focus failed: re-sync the strip */
   return;
  }
  /* v0.71 test3: a windowless card asks the daemon to bring
     the app back (POST /api/webapps/restore -- the daemon
     clears the relaunch backoff and summons the window). */
  const url=(card.win&&card.win.url)||card.lockURL;
  const r=await api('/api/webapps/restore',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({url})}).catch(()=>null);
  if(r&&r.error) toast(r.error);
  setTimeout(refreshExtStrip,350);
 };
 el.oncontextmenu=ev=>{ ev.preventDefault(); ev.stopPropagation(); showExtCtx(ev.clientX,ev.clientY,card.key); };
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
 const listHwnds=new Set(list.filter(v=>v.hwnd>0).map(v=>v.hwnd));
 list.forEach((x,i)=>{
  const key=extCardKey(x);
  seen.add(key);
  let card=extCards.get(key);
  /* v0.71 test3 (L1-B2): in-place migration. A locked app's
     card survives its window: when the window dies the daemon
     lists the app windowless ('u:' key) and the SAME DOM node
     crosses over; when the window is reborn under a new
     handle the ghost card ('u:') crosses back. The frame and
     its ETag belong to the dead window and never cross. */
  if(!card&&(x.lockurl||x.url)){
   if(x.hwnd>0){
    const ghost=extCards.get('u:'+(x.lockurl||x.url));
    if(ghost){ extCards.delete(ghost.key); card=ghost; migrateExtCard(card,x); extCards.set(key,card); }
   } else {
    const url=x.lockurl||x.url;
    for(const c of Array.from(extCards.values())){
     if(c.hwnd>0&&!listHwnds.has(c.hwnd)&&(c.lockURL===url||(c.win&&(c.win.lockurl||c.win.url)===url))){
      extCards.delete(c.key); card=c; migrateExtCard(card,x); extCards.set(key,card); break;
     }
    }
   }
  }
  const wantPrev=preview&&x.hwnd>0&&!thumbBad.has(key);
  if(!card){ card=newExtCard(x,wantPrev); extCards.set(key,card); }
  card.hwnd=x.hwnd; if(x.lockurl) card.lockURL=x.lockurl;
  paintExtCard(card,x,wantPrev&&!card.minWait);
  const ref=box.children[i+1]||null;
  if(ref!==card.el) box.insertBefore(card.el,ref);
  if(wantPrev) pumpThumb(card);
 });
 Array.from(extCards).forEach(([key,card])=>{
  if(seen.has(key)) return;
  if(card.blob) URL.revokeObjectURL(card.blob);
  if(card.el.parentNode) card.el.parentNode.removeChild(card.el);
  else if(box.contains(card.el)) box.removeChild(card.el); /* v0.58: was box.children.indexOf(card.el) -- HTMLCollection has no indexOf in a real browser (the fake-DOM suites use arrays, which masked it); contains() is the intended check, cards are direct children of the strip */
  extCards.delete(key);
 });
 /* Live mode = strip rendered + the view itself visible. On
    the closed->open transition every card asks for one fresh
    frame instead of greeting the user with pre-open pictures. */
 const liveNow=!document.hidden;
 if(liveNow&&!extLive) extCards.forEach(c=>{ if(c.hwnd>0) c.needFresh=true; });
 __bind_extLive.set(liveNow);
}
/* migrateExtCard: re-key a card record for the other shape
   of the same app (windowed <-> windowless). The DOM node
   stays; everything window-shaped is reset so nothing of
   the dead window (frame, ETag, cooldowns) leaks into the
   new shape. */
export function migrateExtCard(card,x){
 card.key=extCardKey(x);
 card.hwnd=x.hwnd;
 card.lockURL=x.lockurl||card.lockURL||'';
 card.el.dataset.key=card.key;
 if(x.hwnd>0) card.el.dataset.hwnd=x.hwnd; else delete card.el.dataset.hwnd;
 if(card.blob){ URL.revokeObjectURL(card.blob); card.blob=''; }
 card.ver=''; card.minWait=false; card.needFresh=x.hwnd>0;
 card.prev=null; card.iconKey=''; card.stateTxt=null;
}
/* The strip's heartbeat (test3): ~1 s while live, the old
   lazy 4 s otherwise. refreshExtStrip itself reconciles the
   card set; this loop only paces it. */
(function extLoop(){
 setTimeout(async()=>{ try{ await refreshExtStrip(); }catch(e){} extLoop(); }, extLive?1000:4000);
})();
