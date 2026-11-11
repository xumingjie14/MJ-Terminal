/* Generated cross-module imports (v0.58 modularization):
   every name below is owned by the sibling module it is imported
   from -- the same names the pre-modular page shared through one
   script scope. */
import { dockEl } from './core';
import { SETTINGS, dockAutoHideOn, dockPos, settingsReady } from './settings';
import { layoutStrip, stageOn } from './stage';
import { dragState } from './drag';
/* ----- dock behaviour (v0.46「Dock 基础行为包」) -----
   Three pieces over the daemon settings dockPosition /
   dockAutoHide: Dock position (bottom default / left / right),
   auto-hide, and the calibrated magnification. The timing and
   geometry constants are named here, in one place -- the future
   unified-animation pass adopts them from this block. */
export const dockWrapEl=document.getElementById('dockwrap');
/* v0.47: the rest size is a setting now (dockIconSize 44-64,
   written by the Control Center slider); the CSS ships 52 as the
   pre-settings default. */
export function dockBase(){ const v=SETTINGS&&SETTINGS.dockIconSize; return (v>=44&&v<=64)?v:52 }
export const DOCK_PEAK=1.85;         // magnification at the pointer's icon (macOS magnitude, under 2x)
export const DOCK_RADIUS_PITCHES=3;  // falloff radius, in icon pitches (base + flex gap)
export const DOCK_GAP=7;             // #dock flex gap (px), the other half of the pitch
export const DOCK_EDGE_DWELL_MS=300; // pointer resting on the edge before the Dock slides in
export const DOCK_HIDE_GRACE_MS=400; // pointer away before the Dock slides out
export const DOCK_SLAM_MS=90;        // edge arrival within this window from >40px out = a deliberate slam: reveal at once
export const DOCK_SETTLE_MS=180;     // magnification snap-back on pointer leave (#dock.settle in CSS matches)


export let dockRAF=0, dockPointer=null;
export function dockIcons(){ return [...dockEl.querySelectorAll('.dockbtn .appico')] }
export function dockMagApply(){
 dockRAF=0;
 if(!dockPointer) return;
 const icons=dockIcons(); if(!icons.length) return;
 const vertical=dockPos()!=='bottom';
 /* measure-then-write: snapshot every centre from the current
    layout first, then write all sizes in one pass. The pre-v0.46
    code re-measured each icon after its predecessors had already
    been resized and let a CSS transition chase each write --
    that read as lag and jitter. Sizes are now written per frame
    with no transition at all. */
 const centers=icons.map(ic=>{ const r=ic.getBoundingClientRect(); return vertical? r.top+r.height/2 : r.left+r.width/2; });
 const sizes=dockMagSizes(centers,vertical?dockPointer.y:dockPointer.x,dockBase(),DOCK_RADIUS_PITCHES*(dockBase()+DOCK_GAP),DOCK_PEAK);
 icons.forEach((ic,i)=>{ ic.style.width=sizes[i]+'px'; ic.style.height=sizes[i]+'px'; });
}
export function dockMagReset(){
 if(dockRAF){ cancelAnimationFrame(dockRAF); dockRAF=0; }
 dockEl.classList.add('settle');
 dockIcons().forEach(ic=>{ ic.style.width=dockBase()+'px'; ic.style.height=dockBase()+'px'; });
 setTimeout(()=>dockEl.classList.remove('settle'),DOCK_SETTLE_MS+40);
}
dockEl.addEventListener('pointermove',e=>{
 dockPointer={x:e.clientX,y:e.clientY};
 if(!dockRAF) dockRAF=requestAnimationFrame(dockMagApply);
});
dockEl.addEventListener('pointerleave',()=>{ dockPointer=null; dockMagReset(); });

/* ----- dock auto-hide (v0.46) -----
   All timers are one-shot and event-driven: nothing runs while
   the view itself is hidden (pointermove stops, and the
   visibilitychange hook below drops any pending timer). */
export let dockHidden=false, dockShowT=0, dockHideT=0, dockLastX=-9999, dockLastY=-9999, dockPrevEdge={t:0,d:1e9};
export function dockSetHidden(h){
 if(h===dockHidden) return;
 dockHidden=h;
 dockWrapEl.classList.toggle('dock-hidden',h);
 if(h) dockMagReset();
 else { clearTimeout(dockHideT); dockHideT=0; }
}
/* Keep-visible holds: a desktop icon drag in flight (dragState is
   the desktop drag engine's global), an HTML5 drag over the view
   (handled in the dragover listener below) -- and, reserved for
   the Dock context menu in the NEXT replication item,
   window.__dockMenuOpen. The menu itself is not part of this
   item; only the hook is. */
export function dockHoldActive(){
 if(window.__dockMenuOpen) return true;
 if(typeof dragState!=='undefined'&&dragState&&dragState.active) return true;
 return false;
}
export function dockEdgeDist(x,y){
 const p=dockPos();
 if(p==='bottom') return innerHeight-1-y;
 if(p==='left') return x;
 return innerWidth-1-x;
}
export function dockNearDock(x,y){
 const r=dockEl.getBoundingClientRect(),m=14;
 return x>=r.left-m&&x<=r.right+m&&y>=r.top-m&&y<=r.bottom+m;
}
export function dockArmHide(){
 if(dockHideT||dockHidden) return;
 dockHideT=setTimeout(()=>{
  dockHideT=0;
  const d=dockEdgeDist(dockLastX,dockLastY);
  if(!dockHoldActive()&&!dockNearDock(dockLastX,dockLastY)&&!(d>=0&&d<=2)) dockSetHidden(true);
 },DOCK_HIDE_GRACE_MS);
}
export function dockAutoHideArm(){
 clearTimeout(dockShowT); dockShowT=0;
 clearTimeout(dockHideT); dockHideT=0;
 if(!dockAutoHideOn()){ dockSetHidden(false); return; }
 if(!dockNearDock(dockLastX,dockLastY)) dockArmHide();
}
document.addEventListener('pointermove',e=>{
 dockLastX=e.clientX; dockLastY=e.clientY;
 if(!dockAutoHideOn()||document.hidden) return;
 const now=performance.now(), d=dockEdgeDist(e.clientX,e.clientY);
 if(dockHidden){
  if(d<=2){
   const slam=(now-dockPrevEdge.t)<=DOCK_SLAM_MS&&dockPrevEdge.d>40;
   if(slam){ clearTimeout(dockShowT); dockShowT=0; dockSetHidden(false); }
   else if(!dockShowT){ dockShowT=setTimeout(()=>{ dockShowT=0; if(dockEdgeDist(dockLastX,dockLastY)<=4) dockSetHidden(false); },DOCK_EDGE_DWELL_MS); }
  } else if(d>8){ clearTimeout(dockShowT); dockShowT=0; }
 } else {
  if(dockNearDock(e.clientX,e.clientY)||d<=2||dockHoldActive()){ clearTimeout(dockHideT); dockHideT=0; }
  else dockArmHide();
 }
 dockPrevEdge={t:now,d};
});
/* HTML5 drags (a file dragged in from the host) do not produce a
   reliable pointermove stream; reveal near the Dock's edge while
   one is in flight and re-arm when it ends. Desktop icon drags
   are pointer-based and hold via dockHoldActive instead. */
document.addEventListener('dragover',e=>{
 dockLastX=e.clientX; dockLastY=e.clientY;
 if(!dockAutoHideOn()) return;
 clearTimeout(dockHideT); dockHideT=0;
 if(dockHidden&&dockEdgeDist(e.clientX,e.clientY)<=48) dockSetHidden(false);
});
document.addEventListener('drop',()=>dockAutoHideArm());
document.addEventListener('dragend',()=>dockAutoHideArm());
document.addEventListener('visibilitychange',()=>{
 if(document.hidden){ clearTimeout(dockShowT); dockShowT=0; clearTimeout(dockHideT); dockHideT=0; }
});

/* ----- dock layout application (v0.46) ----- */
export function applyDockLayout(){
 const p=dockPos();
 dockWrapEl.classList.toggle('pos-bottom',p==='bottom');
 dockWrapEl.classList.toggle('pos-left',p==='left');
 dockWrapEl.classList.toggle('pos-right',p==='right');
 dockMagReset();
 /* interim coexistence: while the Dock owns the left edge, the
    Stage Manager strip mirrors to the right edge (position only
    -- see applyStage and #extstrip.mirror). */
 const strip=document.getElementById('extstrip');
 if(strip) strip.classList.toggle('mirror',p==='left');
 if(stageOn()) layoutStrip();
 dockAutoHideArm();
}
/* v0.58: the settingsReady read is deferred past this bundle's
   own evaluation (a microtask). Module bodies no longer run
   in the page's original textual order, and settings.ts may
   evaluate after this module; the promise cannot resolve before
   the script finishes evaluating anyway (it waits on a fetch),
   so the subscription timing is unchanged. */
Promise.resolve().then(()=>{ settingsReady.then(()=>applyDockLayout()); });

export const __bind_dockRAF: any = { get(): any { return dockRAF; }, set(v: any): any { dockRAF = v; return v; } };

export const __bind_dockPointer: any = { get(): any { return dockPointer; }, set(v: any): any { dockPointer = v; return v; } };

export const __bind_dockHidden: any = { get(): any { return dockHidden; }, set(v: any): any { dockHidden = v; return v; } };

export const __bind_dockShowT: any = { get(): any { return dockShowT; }, set(v: any): any { dockShowT = v; return v; } };

export const __bind_dockHideT: any = { get(): any { return dockHideT; }, set(v: any): any { dockHideT = v; return v; } };

export const __bind_dockLastX: any = { get(): any { return dockLastX; }, set(v: any): any { dockLastX = v; return v; } };

export const __bind_dockLastY: any = { get(): any { return dockLastY; }, set(v: any): any { dockLastY = v; return v; } };

export const __bind_dockPrevEdge: any = { get(): any { return dockPrevEdge; }, set(v: any): any { dockPrevEdge = v; return v; } };
