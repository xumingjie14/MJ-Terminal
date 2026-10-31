/* Generated cross-module imports (v0.58 modularization):
   every name below is owned by the sibling module it is imported
   from -- the same names the pre-modular page shared through one
   script scope. */
import { APPS, iconsEl } from './core';
import { SETTINGS, deskPitch, deskRenderedOnce, __bind_deskRenderedOnce } from './settings';
import { appIcon, openApp } from './windows';
import { LAYOUT, WEBAPPS, deskSortMode, folderIconHTML, folderItems, folderedKeys, mkDeskIcon, saveLayout, sortedDeskOrder, stampAdded, webIconHTML } from './entries';
import { anyOverlapPos, cellPosFor, dragBaseCX, dragInsertIndex, dragState, effPosFor, orderFromPositions } from './drag';
import { openFolderPop } from './folders';
import { BOXAPPS, DESKFILES, PRODAPPS, STOREAPPS, fileIconHTML, launchBoxApp, launchFirefoxEntry, launchProdApp, launchStoreApp, prodIconHTML, showFileCtx, storeIconHTML } from './appsload';
import { openDeskFile, openWebApp } from './viewers';
/* ----- desktop order (v0.35): ONE ordered list is the layout truth -----
   Diagnosis of the pre-v0.35 bugs (icons stacked at rest; one drag
   reshuffled the whole grid): the renderer and the drag engine kept
   DIFFERENT position models. renderDesk painted free absolute
   positions from LAYOUT.pos -- or, for entries without one, a
   default slot counted in source order (apps, web apps, folders,
   store apps, files) -- with no collision avoidance between the
   two, so a default slot could land exactly on a stored position
   whenever the icon set changed (import, install, folder edits).
   deskPos also clamped stored positions to the CURRENT window, so
   positions saved at a larger size piled whole columns/rows onto
   the clamp edge, and finishDrag then wrote those clamped points
   back as permanent positions. The drag engine, meanwhile,
   re-derived its sequence by rounding the free coordinates onto a
   104px lattice (iconSeq/dragInsertIndex): off-lattice points
   (clamps, click-point folder positions, glides) rounded into the
   wrong columns, so the insertion index landed far from the drop
   point and dragShiftMap permuted a huge range of icons.
   The fix: DESKORDER -- one ordered list of icon keys covering all
   four kinds plus folders. renderDesk assigns column-major grid
   cells strictly by list index (cells are a pure function of the
   index and the current window size; rows-per-column follows the
   window height), the drag engine takes its sequence from the same
   list and measures hit/insertion against the painted rects, and
   LAYOUT.pos is demoted to a serialization of the list at the
   current geometry, rewritten after any render whose cells differ.
   Overlap is impossible by construction; a drag can only permute
   the range between the old and new index. */
export let DESKORDER=[];
export let deskOrderReady=false;
export let deskNormalized=false;
export function deskCell(i){ return cellPosFor(i,innerWidth,innerHeight,deskPitch()) }
export function safeDeskEntry(key,build){
 /* v0.55: one bad entry (a malformed app record, an icon builder
    tripping over unexpected data) is skipped and logged -- it must
    never take the whole desktop list down with it. */
 try{ const el=build(); if(el) return {key,el}; }
 catch(e){ console.error('[mj-desk] 图标构建失败，已跳过：'+key,e); }
 return null;
}
export function collectDeskEntries(){
 /* The baseline enumeration, in the historical source order. Used
    for set membership and as the stable tie-break; the PAINTING
    order is DESKORDER, reconciled from this list. Imported files
    (v0.32) mix into the same desktop as the app icons, keyed
    file:<name>; they reorder/yield with the drag engine but can
    never enter a folder or stack into one (updateDrag only arms
    those modes for kind 'web'). Hidden wholesale by the
    filesOnDesktop setting; the files stay in Data/files. */
 const entries=[];
 const pushEn=en=>{ if(en) entries.push(en); };
 /* v0.56: folder membership hides the member from the top level
    for EVERY kind now (one-home rule), not just web apps. */
 const inFolder=folderedKeys();
 APPS.forEach(a=>{ if(a.nodesk||inFolder.has('app:'+a.id)) return; pushEn(safeDeskEntry('app:'+a.id,()=>mkDeskIcon('app:'+a.id,'app',appIcon(a),a.n,a.id==='firefox'?()=>launchFirefoxEntry():()=>openApp(a.id)))); });
 WEBAPPS.filter(a=>!inFolder.has('web:'+a.url)).forEach(a=>{ pushEn(safeDeskEntry('web:'+a.url,()=>mkDeskIcon('web:'+a.url,'web',webIconHTML(a),a.name,()=>openWebApp(a.url)))); });
 LAYOUT.folders.forEach(f=>{ pushEn(safeDeskEntry('folder:'+f.id,()=>{ const el=mkDeskIcon('folder:'+f.id,'folder',folderIconHTML(f),f.name,()=>openFolderPop(f.id,el),f.id); return el; })); });
 STOREAPPS.filter(a=>!inFolder.has('store:'+a.id)).forEach(a=>{ pushEn(safeDeskEntry('store:'+a.id,()=>mkDeskIcon('store:'+a.id,'native',storeIconHTML(a),a.name,()=>launchStoreApp(a.id)))); });
 BOXAPPS.filter(a=>!inFolder.has('box:'+a.id)).forEach(a=>{ pushEn(safeDeskEntry('box:'+a.id,()=>mkDeskIcon('box:'+a.id,'native',storeIconHTML(a),a.name,()=>launchBoxApp(a.id)))); });
 PRODAPPS.filter(c=>!inFolder.has('prod:'+c.id)).forEach(c=>{ pushEn(safeDeskEntry('prod:'+c.id,()=>mkDeskIcon('prod:'+c.id,'native',prodIconHTML(c),c.name,()=>launchProdApp(c.id)))); });
 if(!SETTINGS||SETTINGS.filesOnDesktop!==false){
  DESKFILES.filter(f=>!inFolder.has('file:'+f.name)&&!(LAYOUT.hiddenFiles||[]).includes(f.name)).forEach(f=>{
   pushEn(safeDeskEntry('file:'+f.name,()=>{
    const el=mkDeskIcon('file:'+f.name,'file',fileIconHTML(f),f.name,()=>openDeskFile(f.path,f.kind));
    el.oncontextmenu=ev=>{ ev.preventDefault(); ev.stopPropagation(); showFileCtx(ev.clientX,ev.clientY,f); };
    return el;
   }));
  });
 }
 return entries;
}
export function reconcileDeskOrder(entries){
 const slotOf=new Map(entries.map((e,i)=>[e.key,i]));
 if(!deskOrderReady){
  /* First render after load = the migration. The order is the
     reading order of where the OLD model would have painted each
     icon (its stored position, else its old default slot), so the
     desktop keeps its arrangement -- minus the overlaps, which the
     old model could encode and this one cannot express. Folder
     membership and the item set are untouched. If anything had to
     move (or two icons were painted on top of each other), flag it:
     the daemon's /api/layout handler also logs stored-position
     collisions to the console hub (module "layout"). */
  const deco=entries.map((e,i)=>{
   const st=LAYOUT.pos[e.key];
   return {key:e.key,stored:(st&&typeof st.x==='number'&&typeof st.y==='number')?st:null,slot:i};
  });
  DESKORDER=orderFromPositions(deco,innerWidth,innerHeight,deskPitch());
  deskOrderReady=true;
  const eff=deco.map(d=>effPosFor(d.stored,d.slot,innerWidth,innerHeight,deskPitch()));
  const byKey=new Map(deco.map(d=>[d.key,d]));
  let moved=false;
  DESKORDER.forEach((k,i)=>{
   const c=deskCell(i), d: any=byKey.get(k), p=effPosFor(d.stored,d.slot,innerWidth,innerHeight,deskPitch());
   if(p.x!==c.x||p.y!==c.y) moved=true;
  });
  if(moved||anyOverlapPos(eff,92,100)>0){
   deskNormalized=true;
   console.warn('[mj-desk] 旧布局存在图标重叠或错位，已按原阅读顺序自动规整为不重叠布局');
  }
  return;
 }
 /* Steady state: surviving keys keep their relative order; keys
    that vanished drop out; new keys (an imported file, an installed
    store app, a fresh folder) insert where their stored/default
    position reads among the CURRENT cells. */
 const keep=DESKORDER.filter(k=>slotOf.has(k));
 const have=new Set(keep);
 for(const fe of entries){
  if(have.has(fe.key)) continue;
  const st=LAYOUT.pos[fe.key];
  const stored=(st&&typeof st.x==='number'&&typeof st.y==='number')?st:null;
  const p=effPosFor(stored,slotOf.get(fe.key),innerWidth,innerHeight,deskPitch());
  const centers=keep.map((k,i)=>{ const c=deskCell(i); return {x:c.x+46,y:c.y+50}; });
  let t=dragInsertIndex(centers,p.x+46,p.y+50,dragBaseCX(),deskPitch());
  t=Math.max(0,Math.min(keep.length,t));
  keep.splice(t,0,fe.key);
  have.add(fe.key);
 }
 DESKORDER=keep;
}
export function persistDeskPos(){
 /* Serialize the order back into LAYOUT.pos at the current
    geometry (the on-disk format is unchanged: {pos, folders}).
    Only writes when some cell actually moved. */
 let dirty=false;
 DESKORDER.forEach((k,i)=>{
  const c=deskCell(i), cur=LAYOUT.pos[k];
  if(!cur||cur.x!==c.x||cur.y!==c.y){ LAYOUT.pos[k]={x:c.x,y:c.y}; dirty=true; }
 });
 if(dirty) saveLayout();
}
export function deskAssertNoOverlap(tag){
 /* Invariant self-check: after a render (and thereby after every
    settled drag), no two painted icon rects may overlap. There is
    no frontend->console-hub reporting path in the product (the
    diag endpoints are read-only), so violations go to
    console.error only. */
 if(dragState&&dragState.active) return;
 const els=[...iconsEl.querySelectorAll('.icon')];
 const rs=els.map(el=>el.getBoundingClientRect());
 const bad=[];
 for(let i=0;i<rs.length;i++) for(let j=i+1;j<rs.length;j++){
  const a=rs[i], b=rs[j];
  const ox=Math.min(a.right,b.right)-Math.max(a.left,b.left);
  const oy=Math.min(a.bottom,b.bottom)-Math.max(a.top,b.top);
  if(ox>4&&oy>4) bad.push(els[i].dataset.key+' x '+els[j].dataset.key);
 }
 if(bad.length) console.error('[mj-desk] icon overlap self-check failed ('+tag+'): '+bad.join('; '));
}
export function renderDesk(){
 /* v0.55: the paint chain degrades, never blanks. Per-entry build
    failures are already contained by safeDeskEntry; if collecting
    or reconciling still fails wholesale, log it and paint what we
    have in source order rather than leaving an empty desktop. */
 let entries=[];
 try{ entries=collectDeskEntries(); }
 catch(e){ console.error('[mj-desk] 名单构建失败，桌面降级为空名单绘制',e); }
 try{ reconcileDeskOrder(entries); }
 catch(e){ console.error('[mj-desk] 顺序调和失败，按来源顺序绘制',e); DESKORDER=entries.map(x=>x.key); deskOrderReady=true; }
 /* v0.56: first-seen stamps for the 添加日期 sort (top-level
    entries plus folder members), then the active sort (item 7)
    re-orders the top level. Both are no-ops in 手动 mode. */
 try{ stampAdded(entries.map(e=>e.key).concat(LAYOUT.folders.flatMap(f=>folderItems(f)))); }catch(e){}
 if(deskSortMode()!=='manual'){ const so=sortedDeskOrder(DESKORDER); if(so.join('')!==DESKORDER.join('')) DESKORDER=so; }
 const byKey=new Map(entries.map(e=>[e.key,e]));
 iconsEl.innerHTML='';
 DESKORDER.forEach((k,i)=>{
  const en=byKey.get(k); if(!en) return;
  const c=deskCell(i);
  en.el.style.left=c.x+'px'; en.el.style.top=c.y+'px';
  iconsEl.appendChild(en.el);
 });
 persistDeskPos();
 __bind_deskRenderedOnce.set(true);
 deskAssertNoOverlap('render');
}
/* Cells derive from the window size, so a resize re-derives them
   (order is untouched). Skipped mid-drag; the drop re-renders. */
export let _deskResizeT=null;
addEventListener('resize',()=>{
 if(dragState&&dragState.active) return;
 clearTimeout(_deskResizeT);
 _deskResizeT=setTimeout(()=>{ if(!(dragState&&dragState.active)) renderDesk(); },180);
});
export const __bind_DESKORDER: any = { get(): any { return DESKORDER; }, set(v: any): any { DESKORDER = v; return v; } };

export const __bind_deskOrderReady: any = { get(): any { return deskOrderReady; }, set(v: any): any { deskOrderReady = v; return v; } };

export const __bind_deskNormalized: any = { get(): any { return deskNormalized; }, set(v: any): any { deskNormalized = v; return v; } };

export const __bind__deskResizeT: any = { get(): any { return _deskResizeT; }, set(v: any): any { _deskResizeT = v; return v; } };
