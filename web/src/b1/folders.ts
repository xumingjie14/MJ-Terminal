/* Generated cross-module imports (v0.58 modularization):
   every name below is owned by the sibling module it is imported
   from -- the same names the pre-modular page shared through one
   script scope. */
import { APPS, esc } from './core';
import { glyph } from './glyphs';
import { SETTINGS } from './settings';
import { LAYOUT, WEBAPPS, entryMeta, folderById, folderItems, folderedKeys, recOpen, saveLayout, suppressClickUntil, __bind_suppressClickUntil } from './entries';
import { renderDesk } from './desktop';
import { desktopInsertAt, dragMovedArray, dragShiftMap, folderDissolvesAfter } from './drag';
import { BOXAPPS, DESKFILES, PRODAPPS, STOREAPPS } from './appsload';
/* ----- folder popup (v0.31): macOS Launchpad folder form -----
   The desktop dims and blurs behind a centered rounded panel; the
   title renames in place (click it; Enter or blur commits, Esc
   cancels); clicking outside the panel closes it -- there is no
   button bar. Inside, dragging reorders with the same live yield
   as the desktop (items slide aside via transforms); dragging an
   icon out of the panel drops it back onto the desktop at the
   pointed slot and closes the panel. A folder emptied that way
   dissolves; left with one icon it stays (Launchpad rules). */
export let openFolderEl=null, openFolderId=null, fpDrag=null;
export function openFolderPop(fid: any,anchorEl: any,opts?: any){
 opts=opts||{};
 const f=folderById(fid); if(!f) return;
 openFolderEl=anchorEl||openFolderEl; openFolderId=fid;
 const pop=document.getElementById('folderpop');
 /* v0.56: members resolve through entryMeta -- every kind
    renders and launches here exactly as on the desktop. */
 const metas=folderItems(f).map(k=>entryMeta(k)).filter(Boolean);
 pop.innerHTML=`<div class="fp-panel" id="fp-panel"><div class="fp-title" id="fp-title" title="点标题改名">${esc(f.name)}</div><div class="fp-grid" id="fp-grid">${metas.length?metas.map(m=>`<button class="fp-item" data-key="${esc(m.key)}">${m.icon()}<span>${esc(m.name)}</span></button>`).join(''):'<p class="mut fp-empty">空的：把桌面上的图标拖到这个文件夹上，或者点下面「添加」放进来。</p>'}</div><div class="fp-foot"><button id="fp-add">＋ 添加</button><button id="fp-imp">${glyph('import',13)} 导入文件</button></div></div>`;
 pop.classList.remove('hidden');
 pop.onclick=ev=>{ if(ev.target===pop) closeFolderPop(); };
 document.getElementById('fp-title').onclick=()=>startFolderTitleEdit();
 document.getElementById('fp-add').onclick=()=>showFolderPicker(fid);
 document.getElementById('fp-imp').onclick=()=>{ window.__mjFolderImport=fid; document.getElementById('deskimp').click(); };
 pop.querySelectorAll('.fp-item').forEach(it=>{
  it.onclick=()=>{ if(Date.now()<suppressClickUntil) return; const m=entryMeta(it.dataset.key); closeFolderPop(); if(window.__lpClose) window.__lpClose(); if(m){ recOpen(m.key); m.open(); } };
  attachFolderItemDrag(it,fid);
 });
 if(opts.editTitle) startFolderTitleEdit();
}
/* The in-folder 添加 picker: every entry that is not already in
   a folder (and is not a folder itself), one click files it in
   (move, not copy -- the one-home rule). */
export function showFolderPicker(fid){
 const pop=document.getElementById('folderpop');
 const panel=document.getElementById('fp-panel'); if(!panel) return;
 panel.querySelectorAll('.fp-pick').forEach(x=>x.remove());
 const inAny=folderedKeys();
 const keys=[];
 APPS.forEach(a=>{ if(!a.nodesk) keys.push('app:'+a.id); });
 WEBAPPS.forEach(a=>keys.push('web:'+a.url));
 STOREAPPS.forEach(a=>keys.push('store:'+a.id));
 BOXAPPS.forEach(a=>keys.push('box:'+a.id));
 PRODAPPS.forEach(c=>keys.push('prod:'+c.id));
 if(!SETTINGS||SETTINGS.filesOnDesktop!==false) DESKFILES.forEach(f=>keys.push('file:'+f.name));
 const metas=keys.filter(k=>!inAny.has(k)).map(k=>entryMeta(k)).filter(Boolean);
 const ov=document.createElement('div');
 ov.className='fp-pick';
 ov.innerHTML=`<div class="fp-pick-panel"><h4>添加进文件夹</h4>${metas.length?metas.map(m=>`<button class="fp-pick-it" data-key="${esc(m.key)}">${m.icon()}<span>${esc(m.name)}</span></button>`).join(''):'<p class="mut">桌面上的东西都已经在文件夹里了。</p>'}<div class="fp-foot"><button id="fp-pick-x">完成</button></div></div>`;
 panel.appendChild(ov);
 ov.querySelector('#fp-pick-x').onclick=()=>{ ov.remove(); openFolderPop(fid,openFolderEl); };
 ov.querySelectorAll('.fp-pick-it').forEach(b=>b.onclick=()=>{
  const f=folderById(fid); if(!f) return;
  if(!folderItems(f).includes(b.dataset.key)) f.items.push(b.dataset.key);
  delete LAYOUT.pos[b.dataset.key];
  saveLayout(); renderDesk();
  b.remove();
  if(!ov.querySelector('.fp-pick-it')) ov.querySelector('.fp-pick-panel').insertAdjacentHTML('beforeend','<p class="mut">桌面上的东西都已经在文件夹里了。</p>');
 });
}
export function startFolderTitleEdit(){
 const f=folderById(openFolderId); const t=document.getElementById('fp-title');
 if(!f||!t||t.dataset.editing) return;
 t.dataset.editing='1';
 t.innerHTML=`<input class="fp-title-in" id="fp-title-in" maxlength="24" value="${esc(f.name)}">`;
 const inp=document.getElementById('fp-title-in');
 inp.focus(); inp.select();
 let done=false;
 const commit=ok=>{
  if(done) return; done=true;
  const v=inp.value.trim();
  if(ok&&v&&v!==f.name){ f.name=v.slice(0,24); saveLayout(); renderDesk(); }
  delete t.dataset.editing;
  t.textContent=f.name;
 };
 inp.onkeydown=ev=>{ ev.stopPropagation(); if(ev.key==='Enter') commit(true); else if(ev.key==='Escape') commit(false); };
 inp.onblur=()=>commit(true);
 inp.onclick=ev=>ev.stopPropagation();
}
export function closeFolderPop(){ document.getElementById('folderpop').classList.add('hidden'); openFolderEl=null; openFolderId=null; }
export function attachFolderItemDrag(it,fid){
 it.addEventListener('pointerdown',ev=>{
  if(ev.button!==0) return;
  ev.preventDefault();
  const r=it.getBoundingClientRect();
  fpDrag={it,fid,key:it.dataset.key,sx:ev.clientX,sy:ev.clientY,ox:r.left,oy:r.top,active:false,mode:'grid',targetIdx:0,originIdx:0};
  try{it.setPointerCapture(ev.pointerId)}catch(e){}
 });
 it.addEventListener('pointermove',ev=>{
  if(!fpDrag||fpDrag.it!==it) return;
  const st=fpDrag;
  const dx=ev.clientX-st.sx, dy=ev.clientY-st.sy;
  if(!st.active){
   if(Math.hypot(dx,dy)<6) return;
   beginFolderDrag(st);
  }
  st.ghost.style.left=(st.ox+dx)+'px';
  st.ghost.style.top=(st.oy+dy)+'px';
  const pr=st.panelRect;
  const inside=ev.clientX>=pr.left&&ev.clientX<=pr.right&&ev.clientY>=pr.top&&ev.clientY<=pr.bottom;
  if(inside){
   st.mode='grid';
   const t=folderSlotIndexAt(st,ev.clientX,ev.clientY);
   if(t!==st.targetIdx){ st.targetIdx=t; applyFolderShift(st,false); }
  } else if(st.mode!=='out'){
   st.mode='out'; applyFolderShift(st,true);
  }
 });
 const up=ev=>{
  if(!fpDrag||fpDrag.it!==it) return;
  const st=fpDrag; fpDrag=null;
  if(!st.active) return;
  __bind_suppressClickUntil.set(Date.now()+300);
  finishFolderDrag(st);
 };
 it.addEventListener('pointerup',up);
 it.addEventListener('pointercancel',()=>{
  if(!fpDrag||fpDrag.it!==it) return;
  const st=fpDrag; fpDrag=null;
  if(st.active) cancelFolderDrag(st);
 });
}
export function beginFolderDrag(st){
 const grid=document.getElementById('fp-grid');
 const panel=document.getElementById('fp-panel');
 const items=[...grid.querySelectorAll('.fp-item')];
 st.items=items;
 st.originIdx=items.indexOf(st.it);
 st.targetIdx=st.originIdx;
 st.slots=items.map(el=>{ const r=el.getBoundingClientRect(); return {x:r.left,y:r.top,w:r.width,h:r.height}; });
 st.panelRect=panel.getBoundingClientRect();
 const g=st.it.cloneNode(true);
 g.classList.add('fp-ghost');
 const s0=st.slots[st.originIdx];
 g.style.left=s0.x+'px'; g.style.top=s0.y+'px'; g.style.width=s0.w+'px'; g.style.height=s0.h+'px'; g.style.margin='0';
 document.getElementById('folderpop').appendChild(g);
 st.ghost=g;
 st.it.classList.add('drag-src');
 st.active=true;
}
export function folderSlotIndexAt(st,px,py){
 for(let i=0;i<st.slots.length;i++){
  const s=st.slots[i];
  if(px>=s.x&&px<=s.x+s.w&&py>=s.y&&py<=s.y+s.h) return i;
 }
 let best=st.originIdx, bd=Infinity;
 st.slots.forEach((s,i)=>{
  const d=Math.hypot(px-(s.x+s.w/2),py-(s.y+s.h/2));
  if(d<bd){ bd=d; best=i; }
 });
 return best;
}
export function applyFolderShift(st,reset){
 const m=reset?null:dragShiftMap(st.items.length,st.originIdx,st.targetIdx);
 st.items.forEach((el,i)=>{
  if(el===st.it) return;
  if(reset){ el.style.transform=''; return; }
  const slot=st.slots[m.get(i)], own=st.slots[i];
  const dx=slot.x-own.x, dy=slot.y-own.y;
  el.style.transform=(dx||dy)?`translate(${dx}px,${dy}px)`:'';
 });
}
export function finishFolderDrag(st){
 const f=folderById(st.fid);
 /* The pointer's release point, recovered from the ghost: the
    ghost rides at (origin + total delta), so ghost pos + the
    original grab offset is exactly where the pointer let go. */
 const px=(parseFloat(st.ghost.style.left)||0)+(st.sx-st.ox);
 const py=(parseFloat(st.ghost.style.top)||0)+(st.sy-st.oy);
 endFolderDragVisual(st);
 if(!f) return;
 if(st.mode==='grid'){
  if(st.targetIdx!==st.originIdx){
   f.items=dragMovedArray(f.items,st.originIdx,st.targetIdx);
   saveLayout(); renderDesk(); /* desktop thumbnail shows the new order */
  }
  openFolderPop(st.fid,openFolderEl);
  return;
 }
 /* Dragged out of the panel: back onto the desktop at the pointed
    slot; the panel closes. (v0.56: keyed, any kind.) */
 f.items=folderItems(f).filter(u=>u!==st.key);
 const dissolved=folderDissolvesAfter(f.items);
 if(dissolved) LAYOUT.folders=LAYOUT.folders.filter(x=>x.id!==st.fid);
 desktopInsertAt(st.key,px,py,dissolved?st.fid:null);
 if(dissolved) delete LAYOUT.pos['folder:'+st.fid];
 saveLayout(); closeFolderPop(); renderDesk();
}
export function cancelFolderDrag(st){
 applyFolderShift(st,true);
 endFolderDragVisual(st);
}
export function endFolderDragVisual(st){
 if(st.ghost) st.ghost.remove();
 if(st.it) st.it.classList.remove('drag-src');
}
/* ----- desktop context menu ----- */
export const ctxEl=document.getElementById('ctxmenu');
export function hideCtx(){ctxEl.classList.add('hidden')}
export const __bind_openFolderEl: any = { get(): any { return openFolderEl; }, set(v: any): any { openFolderEl = v; return v; } };

export const __bind_openFolderId: any = { get(): any { return openFolderId; }, set(v: any): any { openFolderId = v; return v; } };

export const __bind_fpDrag: any = { get(): any { return fpDrag; }, set(v: any): any { fpDrag = v; return v; } };
