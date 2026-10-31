/* Generated cross-module imports (v0.58 modularization):
   every name below is owned by the sibling module it is imported
   from -- the same names the pre-modular page shared through one
   script scope. */
import { APPS } from './core';
import { SETTINGS, dockPos, saveSetting } from './settings';
import { bring, syncDots } from './windows';
import { refreshExtStrip } from './extstrip';
import { saveSession } from './session';
/* ----- stage manager (v0.15): one app front, others as live thumbnails ----- */
export let openSet=new Set(), frontApp=null, lastApp='home';
export function stageOn(){ if(SETTINGS) return !!SETTINGS.stageManager; return localStorage.getItem('mj-stage')==='1' }
export function applyStage(w,idx){
 if(!w.classList.contains('staged')){
  w.dataset.sg=JSON.stringify({l:w.style.left,t:w.style.top,wd:w.style.width});
  w.classList.add('staged');
 }
 /* v0.46: position mirror only -- with the Dock on the left edge
    the strip stacks along the right edge instead (same order,
    same scale, same behaviour); applyDockLayout re-runs
    layoutStrip() after a position flip so staged windows move. */
 if(dockPos()==='left'){ w.style.left='auto'; w.style.right='10px'; w.style.transformOrigin='top right'; }
 else { w.style.right=''; w.style.left='10px'; w.style.transformOrigin=''; }
 w.style.top=(64+idx*116)+'px';
 w.style.zIndex=20+idx;
 const gw=w.offsetWidth||700;
 w.style.transform=`scale(${(148/gw).toFixed(4)})`;
}
export function restoreStage(w){
 if(!w.classList.contains('staged')) return;
 w.classList.remove('staged');
 w.style.transform='';
 w.style.right=''; w.style.transformOrigin=''; /* v0.46: drop the strip mirror */
 try{const g=JSON.parse(w.dataset.sg||'{}'); if(g.l)w.style.left=g.l; if(g.t)w.style.top=g.t; if(g.wd)w.style.width=g.wd;}catch(e){}
}
export function layoutStrip(){
 let idx=0;
 APPS.forEach(a=>{
  if(a.id===frontApp||!openSet.has(a.id)) return;
  const w=document.getElementById('win-'+a.id); if(!w) return;
  applyStage(w,idx++);
 });
}
export function setFront(id){
 frontApp=id;
 const w=document.getElementById('win-'+id);
 if(w){ restoreStage(w); w.classList.remove('hidden'); bring(w); }
 layoutStrip();
 syncDots();
}
export function onWinClosed(id){
 saveSession();
 if(!stageOn()||!openSet.has(id)) return;
 openSet.delete(id);
 const w=document.getElementById('win-'+id);
 if(w) restoreStage(w);
 if(frontApp===id){
  const rest=APPS.map(a=>a.id).filter(x=>openSet.has(x));
  if(rest.length) setFront(rest[rest.length-1]); else frontApp=null;
 } else layoutStrip();
}
export function toggleStage(on){
 if(SETTINGS){ SETTINGS.stageManager=on; saveSetting({stageManager:on}); }
 else localStorage.setItem('mj-stage',on?'1':'0');
 updateStageUI();
 if(on){
  openSet=new Set(APPS.map(a=>a.id).filter(x=>{const w=document.getElementById('win-'+x);return w&&!w.classList.contains('hidden')}));
  if(!openSet.size) openSet.add('home');
  const f=openSet.has(lastApp)?lastApp:[...openSet][0];
  const w=document.getElementById('win-'+f);
  if(w) w.classList.remove('hidden');
  setFront(f);
 } else {
  openSet.forEach(x=>{const w=document.getElementById('win-'+x); if(w){ restoreStage(w); w.classList.remove('hidden'); }});
  frontApp=null;
 }
 syncDots();
}
export function updateStageUI(){
 const b=document.getElementById('mb-stage'); if(b) b.classList.toggle('on',stageOn());
 refreshExtStrip();
}
export const __bind_frontApp: any = { get(): any { return frontApp; }, set(v: any): any { frontApp = v; return v; } };

export const __bind_lastApp: any = { get(): any { return lastApp; }, set(v: any): any { lastApp = v; return v; } };

export const __bind_openSet: any = { get(): any { return openSet; }, set(v: any): any { openSet = v; return v; } };
