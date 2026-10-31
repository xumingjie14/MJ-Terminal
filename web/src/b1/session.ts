/* Generated cross-module imports (v0.58 modularization):
   every name below is owned by the sibling module it is imported
   from -- the same names the pre-modular page shared through one
   script scope. */
import { APPS, api, dockEl, esc } from './core';
import { appIcon, bring, openApp, syncDots } from './windows';
import { frontApp, onWinClosed, setFront, stageOn } from './stage';
/* ----- desktop session memory (v0.38, tray memory's page half) -----
   If the view is destroyed for real (the Edge frame's own X --
   the one close no in-page button can intercept), the rebuilt
   page used to come back as a bare first-run desktop. This
   ledger remembers which in-system windows were open, where they
   sat (for staged windows: the geometry Stage Manager restores
   from) and their stacking, and replays it on load. It lives in
   this view's localStorage, same as the shell profile's other
   per-view state. The daemon half (the view's screen frame) is
   remembered separately in shellview.go. */
export const SESS_KEY='mj-session';
export let sessTimer=null;
export function snapshotSession(){
 const wins=[];
 document.querySelectorAll('.win').forEach(w=>{
  if(w.classList.contains('hidden')) return;
  const id=w.id.slice(4);
  if(!APPS.some(a=>a.id===id)) return;
  const r: any={id,z:parseInt(w.style.zIndex||'0',10)||0};
  if(w.classList.contains('staged')){ try{ const g=JSON.parse(w.dataset.sg||'{}'); r.l=g.l||''; r.t=g.t||''; r.wd=g.wd||''; }catch(e){} }
  else { r.l=w.style.left; r.t=w.style.top; r.wd=w.style.width; }
  wins.push(r);
 });
 wins.sort((a,b)=>a.z-b.z);
 return {wins,front:frontApp||''};
}
export function saveSession(){
 clearTimeout(sessTimer);
 sessTimer=setTimeout(()=>{ try{ localStorage.setItem(SESS_KEY,JSON.stringify(snapshotSession())); }catch(e){} },200);
}
export function restoreSession(){
 let s=null; try{ s=JSON.parse(localStorage.getItem(SESS_KEY)||'null'); }catch(e){}
 if(!s||!Array.isArray(s.wins)||!s.wins.length) return;
 let n=0;
 for(const r of s.wins){
  if(!APPS.some(a=>a.id===r.id)) continue; /* the app no longer exists: skip it */
  openApp(r.id);
  const w=document.getElementById('win-'+r.id); if(!w) continue;
  if(w.classList.contains('staged')){ try{ const g=JSON.parse(w.dataset.sg||'{}'); if(r.l)g.l=r.l; if(r.t)g.t=r.t; if(r.wd)g.wd=r.wd; w.dataset.sg=JSON.stringify(g); }catch(e){} }
  else { if(r.l)w.style.left=r.l; if(r.t)w.style.top=r.t; if(r.wd)w.style.width=r.wd; }
  n++;
 }
 /* The boot default opened 工作站 unconditionally; if the ledger
    says it was closed, the ledger wins. */
 if(!s.wins.some(r=>r.id==='home')){
  const hw=document.getElementById('win-home');
  if(hw&&!hw.classList.contains('hidden')){ hw.classList.add('hidden'); onWinClosed('home'); }
 }
 if(s.front&&APPS.some(a=>a.id===s.front)&&document.getElementById('win-'+s.front)){
  if(stageOn()) setFront(s.front);
  else { const w=document.getElementById('win-'+s.front); if(w) bring(w); }
 }
 syncDots(); saveSession();
 if(n) api('/api/shell/restored',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({wins:n})}).catch(()=>{});
}
addEventListener('beforeunload',()=>{ try{ localStorage.setItem(SESS_KEY,JSON.stringify(snapshotSession())); }catch(e){} });
/* v0.55: FFSTATE is declared HERE, ahead of the Dock builder loop
   below -- that loop runs during initial script evaluation and calls
   appIcon() for every app, and since v0.54 appIcon('firefox') reads
   FFSTATE (via ffDeskIconHTML). In v0.54 the declaration sat ~770
   lines later, down in the Firefox section: the loop hit the
   temporal dead zone, the ReferenceError aborted the whole first
   script block mid-evaluation, every later let/const (LAYOUT,
   DESKORDER, ...) stayed uninitialized forever, the boot sequence
   never ran, and the desktop + Launchpad painted nothing. The state
   is still loaded by loadFFState() and consumed by ffDeskIconHTML()
   / paintFF in the Firefox section; only the declaration moved. */
export let FFSTATE=null;
export function dockIconHTML(a){
 /* One app's icon must never be able to abort script evaluation or
    a paint: fall back to its plain glyph tile and log. */
 try{ return appIcon(a); }
 catch(e){ console.error('[mj-dock] 图标构建失败，已降级为默认图标：'+a.id,e); return `<div class="appico ${a.g}">${esc(a.glyph)}</div>`; }
}
APPS.forEach(a=>{
 if(a.nodesk) return; /* file viewers (v0.32): windows only, no Dock icon */
 const it=document.createElement('div');it.className='ditem';
 it.innerHTML=`<span class="dlabel">${a.n}</span><button class="dockbtn">${dockIconHTML(a)}</button><span class="ddot" id="ddot-${a.id}"></span>`;
 it.querySelector('.dockbtn').onclick=()=>openApp(a.id);
 dockEl.appendChild(it);
});
export const __bind_sessTimer: any = { get(): any { return sessTimer; }, set(v: any): any { sessTimer = v; return v; } };

export const __bind_FFSTATE: any = { get(): any { return FFSTATE; }, set(v: any): any { FFSTATE = v; return v; } };
