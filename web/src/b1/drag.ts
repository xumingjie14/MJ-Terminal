/* Generated cross-module imports (v0.58 modularization):
   every name below is owned by the sibling module it is imported
   from -- the same names the pre-modular page shared through one
   script scope. */
import { iconsEl } from './core';
import { deskPitch } from './settings';
import { LAYOUT, deskSortMode, folderById, folderItems, saveLayout, setDeskSort, suppressClickUntil, __bind_suppressClickUntil } from './entries';
import { DESKORDER, renderDesk, __bind_DESKORDER } from './desktop';
import { openFolderId, openFolderPop } from './folders';
/* ----- pure drag logic (v0.31): Launchpad reorder math -----
   Kept free of DOM access so it can be unit-tested under node. The
   desktop reduces to a column-major list of fixed slots (columns
   grow left from the right edge, exactly how deskPos lays out the
   default slots); the folder grid reduces to the same "insertion
   index + shifted assignments" shape in row-major form. */
/*PURE-BEGIN*/
export function dragColOf(cx,baseCX,pitch){ return Math.round((baseCX-cx)/pitch) }
/* How many of `centers` (given in visual order) rank before the
   pointer in column-major reading order: smaller column first
   (column 0 is the rightmost), then top-to-bottom in a column. The
   result is an insertion index in centers' coordinate space. */
export function dragInsertIndex(centers,px,py,baseCX,pitch){
 const pc=dragColOf(px,baseCX,pitch);
 let n=0;
 for(const c of centers){
  const cc=dragColOf(c.x,baseCX,pitch);
  if(cc<pc||(cc===pc&&c.y<py)) n++;
 }
 return n;
}
/* Final slot index for every item when the item at originIdx moves
   to targetIdx: a Map visualIndex -> slotIndex covering ALL indices
   (the dragged item included); the others keep their relative order
   and slide by one slot. Always a permutation of 0..count-1. */
export function dragShiftMap(count,originIdx,targetIdx){
 const m=new Map();
 m.set(originIdx,targetIdx);
 const others=[]; for(let i=0;i<count;i++) if(i!==originIdx) others.push(i);
 others.forEach((v,j)=>{ m.set(v, j>=targetIdx ? j+1 : j); });
 return m;
}
/* A copy of arr with the element at `from` moved to `to`. */
export function dragMovedArray(arr,from,to){
 const a=arr.slice(); const x=a.splice(from,1)[0]; a.splice(to,0,x); return a;
}
/* The pre-v0.35 position of an entry: its stored free position
   (clamped to the window, the old deskPos rule) or, lacking one,
   its old default slot -- source-order slots, 8 per column. Kept
   exact: the v0.35 migration derives the desktop order from where
   the old model would have painted each icon. */
export function effPosFor(stored,slot,W,H,pitch){
 if(stored) return {x:Math.max(0,Math.min(stored.x,W-96)),y:Math.max(42,Math.min(stored.y,H-120))};
 const col=Math.floor(slot/8), row=slot%8;
 return {x:Math.max(4,W-118-col*pitch), y:54+row*pitch};
}
/* Rows per column for the v0.35 grid: as many 104px rows as the
   window height fits between the menu bar offset and a bottom
   reserve, at least one. */
export function deskRowsFor(H,pitch){ return Math.max(1,Math.floor((H-130)/pitch)+1) }
/* The grid cell of list index i (column-major: columns grow left
   from the right edge, exactly the old default-slot geometry, but
   rows-per-column follows the window height instead of a fixed 8,
   so tall desktops stop spilling past the bottom edge). */
export function cellPosFor(i,W,H,pitch){
 const rows=deskRowsFor(H,pitch);
 const col=Math.floor(i/rows), row=i%rows;
 return {x:Math.max(4,W-118-col*pitch), y:54+row*pitch};
}
/* The desktop order implied by a set of pre-v0.35 positions:
   column-major reading order (rightmost column first, top to
   bottom) over the effective positions, ties broken by x, then by
   baseline slot -- a total order, so colliding entries still get
   distinct, deterministic places. entries: [{key, stored, slot}] */
export function orderFromPositions(entries,W,H,pitch){
 const baseCX=W-72;
 const deco=entries.map(e=>{
  const p=effPosFor(e.stored,e.slot,W,H,pitch);
  return {key:e.key,slot:e.slot,x:p.x,y:p.y,col:dragColOf(p.x+46,baseCX,pitch)};
 });
 deco.sort((a,b)=>(a.col-b.col)||(a.y-b.y)||(a.x-b.x)||(a.slot-b.slot));
 return deco.map(d=>d.key);
}
/* How many pairs in a list of top-left points overlap for icons of
   w x h px. */
export function anyOverlapPos(list,w,h){
 let n=0;
 for(let i=0;i<list.length;i++) for(let j=i+1;j<list.length;j++){
  if(Math.abs(list[i].x-list[j].x)<w&&Math.abs(list[i].y-list[j].y)<h) n++;
 }
 return n;
}
/* Launchpad rule: a folder emptied by dragging out dissolves;
   left with one icon it stays a folder. */
export function folderDissolvesAfter(items){ return items.length===0 }
/*PURE-END*/

/* ----- icon dragging (v0.31): Launchpad logic on the free desktop -----
   Pick up: the icon grows to 1.08x, translucent with a shadow, and a
   dashed placeholder holds its slot. During the drag the insertion
   index is recomputed from the pointer on every move and the other
   icons SLIDE (transitioned left/top) to open the gap, so the result
   is visible before release -- letting go never jumps. Hovering a
   folder tile blooms it immediately; dropping files the icon in
   (the folder keeps its name, its thumbnail re-renders). Dwelling
   ~600ms on a web-app icon arms it (same bloom); dropping then
   creates a folder from the two and opens it with the name ready to
   edit. A quick pass over an icon never arms -- it just reorders.
   Esc (or pointer cancel) restores everything, nothing is saved. */
export let dragState=null;
/* v0.56: the grid pitch is no longer a constant -- it derives
   from the desktop icon size setting (deskPitch(), defined with
   the settings plumbing). The drag math takes it as a parameter
   everywhere it used to read the constant. */
export function dragBaseCX(){ return innerWidth-72 }
export function iconSeq(){
 /* All desktop icons in THE desktop order (DESKORDER) -- the same
    list renderDesk painted, so slots, centers and indices describe
    the arrangement the user actually sees. Slots are spatial
    anchors: during a drag the ITEMS move between slots, the slots
    themselves never move. (Pre-v0.35 this list was re-derived by
    rounding the icons' free coordinates onto the 104px lattice,
    which disagreed with the painted layout whenever a position was
    off-lattice; one drag then permuted a huge range of icons.) */
 const els=[...iconsEl.querySelectorAll('.icon')];
 const rank=new Map(DESKORDER.map((k,i)=>[k,i]));
 els.sort((a,b)=>(rank.get(a.dataset.key)??1e9)-(rank.get(b.dataset.key)??1e9));
 return els.map((el,i)=>({el,key:el.dataset.key,kind:el.dataset.kind,
   x:parseFloat(el.style.left)||0, y:parseFloat(el.style.top)||0,
   w:el.offsetWidth||92, h:el.offsetHeight||100, dom:i}));
}
export function attachDrag(el,key,kind){
 el.addEventListener('pointerdown',ev=>{
  if(ev.button!==0) return;
  ev.preventDefault();
  const r=el.getBoundingClientRect();
  dragState={el,key,kind,sx:ev.clientX,sy:ev.clientY,ox:r.left,oy:r.top,active:false};
  try{el.setPointerCapture(ev.pointerId)}catch(e){}
 });
 el.addEventListener('pointermove',ev=>{
  if(!dragState||dragState.el!==el) return;
  const ds=dragState;
  const dx=ev.clientX-ds.sx, dy=ev.clientY-ds.sy;
  if(!ds.active){
   if(Math.hypot(dx,dy)<5) return;
   beginDrag(ds);
  }
  el.style.left=Math.max(0,Math.min(innerWidth-96,ds.ox+dx))+'px';
  el.style.top=Math.max(42,Math.min(innerHeight-110,ds.oy+dy))+'px';
  updateDrag(ds,ev.clientX,ev.clientY);
 });
 const up=ev=>{
  if(!dragState||dragState.el!==el) return;
  const ds=dragState; dragState=null;
  if(!ds.active) return;
  __bind_suppressClickUntil.set(Date.now()+300);
  finishDrag(ds,ev.clientX,ev.clientY);
 };
 el.addEventListener('pointerup',up);
 el.addEventListener('pointercancel',()=>{
  if(!dragState||dragState.el!==el) return;
  const ds=dragState; dragState=null;
  if(ds.active){ __bind_suppressClickUntil.set(Date.now()+300); cancelDrag(ds); }
 });
}
export function beginDrag(ds){
 const seq=iconSeq();
 const oi=seq.findIndex(s=>s.el===ds.el);
 ds.seq=seq; ds.originIdx=oi; ds.targetIdx=oi;
 ds.slots=seq.map(s=>({x:s.x,y:s.y}));
 ds.centers=ds.slots.map((s,i)=>({x:s.x+seq[i].w/2,y:s.y+seq[i].h/2}));
 ds.hoverEl=null; ds.hoverMode='reorder'; ds.armEl=null; ds.armed=false; ds.armTimer=null;
 const ph=document.createElement('div');
 ph.className='icon-ph';
 ph.style.left=ds.slots[oi].x+'px'; ph.style.top=ds.slots[oi].y+'px'; ph.style.height=seq[oi].h+'px';
 iconsEl.appendChild(ph); ds.ph=ph;
 ds.el.classList.add('dragging');
 ds.active=true;
}
export function updateDrag(ds,px,py){
 /* What is the pointer over? The dragged icon and the placeholder
    are skipped; everything else is hit-tested at its CURRENT
    (possibly mid-slide) position -- what the user sees is what
    counts. */
 let hit=null;
 for(let si=ds.seq.length-1;si>=0;si--){
  const s=ds.seq[si];
  if(s.el===ds.el) continue;
  const r=s.el.getBoundingClientRect();
  if(px>=r.left&&px<=r.right&&py>=r.top&&py<=r.bottom){ hit=s.el; break; }
 }
 /* v0.56: dragging into the OPEN folder's panel files the icon
    into that folder (the third way in, beside the tile drop and
    the 添加 picker). */
 const fpGrid=document.getElementById('fp-grid');
 if(openFolderId&&ds.kind!=='folder'){
  const panel=document.getElementById('fp-panel');
  if(panel){ const pr=panel.getBoundingClientRect();
   if(px>=pr.left&&px<=pr.right&&py>=pr.top&&py<=pr.bottom){
    disarmDrag(ds);
    ds.seq.forEach(s=>s.el.classList.remove('bud'));
    if(fpGrid) fpGrid.classList.add('dropIn');
    ds.el.style.transform='scale(.55)';
    ds.hoverEl=null; ds.hoverMode='folderIn';
    return;
   } }
 }
 if(fpGrid) fpGrid.classList.remove('dropIn');
 let mode='reorder';
 /* v0.56: ANY entry kind can be filed into a folder now (item
    4); stack-to-create-a-folder stays a web-apps gesture. */
 if(hit&&hit.dataset.fid&&ds.kind!=='folder') mode='folder';
 else if(hit&&ds.kind==='web'&&hit.dataset.kind==='web') mode='stack';
 /* Stack-to-create arming: only a steady ~600ms dwell on the SAME
    icon arms it. Moving off (or onto anything else) disarms. */
 if(mode==='stack'){
  if(ds.armEl!==hit){
   disarmDrag(ds);
   ds.armEl=hit;
   ds.armTimer=setTimeout(()=>{
    if(dragState!==ds||ds.armEl!==hit) return;
    ds.armed=true; hit.classList.add('bud');
   },600);
  }
 } else if(ds.armEl) disarmDrag(ds);
 /* Folder tiles bloom at once; an armed stack target keeps its
    bloom. Every other tile stays plain -- names never change. */
 ds.seq.forEach(s=>{ if(s.el!==hit) s.el.classList.remove('bud'); });
 if(mode==='folder'&&hit) hit.classList.add('bud');
 /* Over a folder the dragged icon shrinks, as if into the folder. */
 ds.el.style.transform=(mode==='folder')?'scale(.55)':'';
 ds.hoverEl=hit; ds.hoverMode=mode;
 if(mode!=='reorder') return; /* folder/stack: freeze the reorder gap */
 let t=dragInsertIndex(ds.centers,px,py,dragBaseCX(),deskPitch());
 t=Math.max(0,Math.min(ds.seq.length-1,t));
 if(t!==ds.targetIdx){ ds.targetIdx=t; applyDragShift(ds); }
}
export function applyDragShift(ds){
 const m=dragShiftMap(ds.seq.length,ds.originIdx,ds.targetIdx);
 ds.seq.forEach((s,i)=>{
  if(s.el===ds.el) return;
  const slot=ds.slots[m.get(i)];
  s.el.style.left=slot.x+'px'; s.el.style.top=slot.y+'px';
 });
 if(ds.ph){ ds.ph.style.left=ds.slots[ds.targetIdx].x+'px'; ds.ph.style.top=ds.slots[ds.targetIdx].y+'px'; }
}
export function disarmDrag(ds){
 if(ds.armTimer){ clearTimeout(ds.armTimer); ds.armTimer=null; }
 if(ds.armEl) ds.armEl.classList.remove('bud');
 ds.armEl=null; ds.armed=false;
}
export function finishDrag(ds,cx,cy){
 const el=ds.el;
 const armedEl=(ds.armed&&ds.armEl)?ds.armEl:null;
 disarmDrag(ds);
 ds.seq.forEach(s=>s.el.classList.remove('bud'));
 el.style.transform='';
 const fpGridEl=document.getElementById('fp-grid'); if(fpGridEl) fpGridEl.classList.remove('dropIn');
 /* 0) v0.56: dropped into the OPEN folder's panel. */
 if(ds.hoverMode==='folderIn'&&openFolderId&&ds.kind!=='folder'){
  const f=folderById(openFolderId);
  if(f&&!folderItems(f).includes(ds.key)) f.items.push(ds.key);
  delete LAYOUT.pos[ds.key];
  endDragVisual(ds);
  saveLayout(); renderDesk();
  openFolderPop(openFolderId,null);
  return;
 }
 /* 1) Dropped onto a folder tile: file it in (v0.56: any kind).
       The folder keeps its name; its thumbnail re-renders from
       its contents on the next renderDesk. */
 if(ds.hoverMode==='folder'&&ds.hoverEl&&ds.kind!=='folder'){
  const f=folderById(ds.hoverEl.dataset.fid);
  if(f&&!folderItems(f).includes(ds.key)) f.items.push(ds.key);
  delete LAYOUT.pos[ds.key];
  endDragVisual(ds);
  saveLayout(); renderDesk();
  return;
 }
 /* 2) Dropped while armed on another web app: create a folder from
       the two, then open it with the name ready to edit. */
 if(armedEl&&ds.kind==='web'){
  const ar=armedEl.getBoundingClientRect();
  if(cx>=ar.left&&cx<=ar.right&&cy>=ar.top&&cy<=ar.bottom){
   const turl=armedEl.dataset.key.slice(4), durl=ds.key.slice(4);
   if(turl!==durl){
    const id='f'+Date.now().toString(36);
    const tp={x:Math.round(parseFloat(armedEl.style.left)||0),y:Math.round(parseFloat(armedEl.style.top)||0)};
    LAYOUT.folders.push({id,name:'文件夹',items:['web:'+turl,'web:'+durl]});
    LAYOUT.pos['folder:'+id]=tp;
    delete LAYOUT.pos[armedEl.dataset.key]; delete LAYOUT.pos[ds.key];
    endDragVisual(ds);
    saveLayout(); renderDesk();
    const tile=document.querySelector(`.icon[data-fid="${id}"]`);
    if(tile) openFolderPop(id,tile,{editTitle:true});
    return;
   }
  }
 }
 /* 3) Plain reorder (or a drop back home): move the key inside
       THE order (ds.seq is DESKORDER order, so the indices line
       up), glide the dragged icon from the pointer into its slot,
       then re-render: the repaint lands every icon on its new cell
       and persistDeskPos serializes the order back to LAYOUT.pos.
       Positions cannot jump -- the glide target IS the cell the
       repaint will assign. */
 const target=ds.targetIdx;
 /* v0.56 item 7: a manual drag is a manual order -- any active
    sort steps aside (the surfaces re-read the same key). */
 if(target!==ds.originIdx&&deskSortMode()!=='manual') setDeskSort('manual');
 __bind_DESKORDER.set(dragMovedArray(DESKORDER,ds.originIdx,target));
 el.classList.remove('dragging');
 el.style.left=ds.slots[target].x+'px';
 el.style.top=ds.slots[target].y+'px';
 if(ds.ph) ds.ph.remove();
 renderDesk();
}
export function endDragVisual(ds){
 ds.el.classList.remove('dragging');
 ds.el.style.transform='';
 if(ds.ph) ds.ph.remove();
}
export function cancelDrag(ds){
 disarmDrag(ds);
 ds.seq.forEach((s,i)=>{
  s.el.classList.remove('bud');
  s.el.style.left=ds.slots[i].x+'px'; s.el.style.top=ds.slots[i].y+'px';
 });
 endDragVisual(ds);
}
/* Insert a desktop icon arriving from a folder at the slot the
   pointer indicates: the key is spliced into THE order at the
   insertion index measured over the currently painted cells (the
   same counting the desktop drag uses), and the caller's
   save + re-render paints and persists the result. excludeFid
   names a folder tile that is about to vanish (dissolved), so it
   takes no part in the sequence. */
export function desktopInsertAt(key,px,py,excludeFid){
 const seq=iconSeq().filter(s=>!(excludeFid&&s.key==='folder:'+excludeFid));
 const centers=seq.map(s=>({x:s.x+s.w/2,y:s.y+s.h/2}));
 let t=dragInsertIndex(centers,px,py,dragBaseCX(),deskPitch());
 t=Math.max(0,Math.min(seq.length,t));
 const keys=seq.map(s=>s.key).filter(k=>k!==key);
 keys.splice(Math.min(t,keys.length),0,key);
 __bind_DESKORDER.set(keys);
 delete LAYOUT.pos[key];
}
export const __bind_dragState: any = { get(): any { return dragState; }, set(v: any): any { dragState = v; return v; } };
