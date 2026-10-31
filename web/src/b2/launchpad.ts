/* Generated cross-module imports (v0.58 modularization):
   every name below is owned by the sibling module it is imported
   from -- the same names the pre-modular page shared through one
   script scope. */
import { $50, vaultLockedNow } from './fvhead';
import { closeMbMenu } from './mbmenus';
import { closeSpot } from './spotlight';
/* ================= Launchpad ================= */
export const lpEl=$50('launchpad'), lpPagesEl=$50('lp-pages'), lpDotsEl=$50('lp-dots'), lpQ=$50('lp-q');
export let lpIsOpen=false, lpPage=0, lpDrag=null, lpWheelAt=0;
export function lpEntries(){
 const byKey={}, out=[];
 const push=(key,name,iconHTML,open)=>{ byKey[key]={key,name,iconHTML,open}; };
 /* v0.55: same containment as the desktop -- one entry's icon
    builder failing degrades that entry to a placeholder tile,
    never the whole Launchpad list. */
 const pushSafe=(key,name,buildIcon,open)=>{ let h; try{ h=buildIcon(); }catch(e){ console.error('[mj-lp] 图标构建失败，已降级：'+key,e); h='<div class="appico g-sys">?</div>'; } push(key,name,h,open); };
 const inFolder=folderedKeys(); /* v0.56: membership hides members here too, every kind */
 APPS.forEach(a=>{ if(!a.nodesk&&!inFolder.has('app:'+a.id)) pushSafe('app:'+a.id,a.n,()=>appIcon(a),a.id==='firefox'?()=>launchFirefoxEntry():()=>openApp(a.id)); });
 WEBAPPS.filter(a=>!inFolder.has('web:'+a.url)).forEach(a=>pushSafe('web:'+a.url,a.name,()=>webIconHTML(a),()=>openWebApp(a.url)));
 LAYOUT.folders.forEach(f=>pushSafe('folder:'+f.id,f.name,()=>folderIconHTML(f),el=>openFolderPop(f.id,el)));
 STOREAPPS.filter(a=>!inFolder.has('store:'+a.id)).forEach(a=>pushSafe('store:'+a.id,a.name,()=>storeIconHTML(a),()=>launchStoreApp(a.id)));
 BOXAPPS.filter(a=>!inFolder.has('box:'+a.id)).forEach(a=>pushSafe('box:'+a.id,a.name,()=>storeIconHTML(a),()=>launchBoxApp(a.id)));
 PRODAPPS.filter(c=>!inFolder.has('prod:'+c.id)).forEach(c=>pushSafe('prod:'+c.id,c.name,()=>prodIconHTML(c),()=>launchProdApp(c.id)));
 DESKORDER.forEach(k=>{ if(byKey[k]) out.push(byKey[k]); });
 Object.keys(byKey).forEach(k=>{ if(!out.some(e=>e.key===k)) out.push(byKey[k]); });
 return out;
}
export function lpVisible(){
 const all=lpEntries();
 const keys=new Set(lpFilterKeys(all,lpQ.value));
 return all.filter(e=>keys.has(e.key));
}
export function openLP(){
 if(vaultLockedNow()) return;
 closeMbMenu(); closeSpot();
 lpIsOpen=true; lpPage=0; lpQ.value='';
 lpEl.classList.remove('hidden');
 lpEl.classList.remove('open'); void lpEl.offsetWidth; lpEl.classList.add('open');
 paintLP();
 setTimeout(()=>{ try{ lpQ.focus(); }catch(e){} },40);
}
export function closeLP(){ if(!lpIsOpen) return; lpIsOpen=false; lpEl.classList.add('hidden'); }
window.__lpClose=()=>closeLP();
export function lpPageGo(d){
 const pages=lpPageCount(lpVisible().length,innerWidth,innerHeight);
 const np=Math.max(0,Math.min(pages-1,lpPage+d));
 if(np!==lpPage){ lpPage=np; paintLP(); }
}
export function paintLP(){
 let vis;
 try{ vis=lpVisible(); }
 catch(e){ console.error('[mj-lp] 名单构建失败',e); lpPagesEl.innerHTML='<div class="lp-empty">名单加载失败，详情看控制台</div>'; lpDotsEl.innerHTML=''; return; }
 const dims=lpDims(innerWidth,innerHeight);
 const pages=lpPageCount(vis.length,innerWidth,innerHeight);
 if(lpPage>=pages) lpPage=pages-1;
 if(lpPage<0) lpPage=0;
 lpPagesEl.innerHTML='';
 if(!vis.length) lpPagesEl.innerHTML='<div class="lp-empty">没有匹配的应用</div>';
 for(let p=0;p<pages;p++){
  const pg=document.createElement('div');
  pg.className='lp-page';
  pg.style.display=p===lpPage?'grid':'none';
  pg.style.gridTemplateColumns='repeat('+dims.cols+',108px)';
  pg.style.columnGap='26px'; pg.style.rowGap='22px';
  vis.slice(p*dims.per,(p+1)*dims.per).forEach(en=>pg.appendChild(lpItemEl(en,vis)));
  lpPagesEl.appendChild(pg);
 }
 lpDotsEl.innerHTML=pages>1?vis.map((_,i)=>i).filter(i=>i<pages).map(i=>'<i data-p="'+i+'" class="'+(i===lpPage?'on':'')+'"></i>').join(''):'';
 lpDotsEl.querySelectorAll('i').forEach(d=>d.onclick=()=>{ lpPage=+d.dataset.p; paintLP(); });
}
export function lpItemEl(en,vis){
 const b=document.createElement('button');
 b.className='lp-item'; b.dataset.key=en.key;
 b.innerHTML=en.iconHTML+'<span class="lpn">'+esc(en.name)+'</span>';
 b.onclick=()=>{
  if(Date.now()<suppressClickUntil) return;
  if(en.key.indexOf('folder:')===0){ en.open(b); return; } // folder opens in place, over Launchpad
  closeLP(); en.open();
 };
 b.addEventListener('pointerdown',ev=>{
  if(ev.button!==0||lpQ.value.trim()) return; // no reordering inside search results
  const r=b.getBoundingClientRect();
  lpDrag={el:b,en,sx:ev.clientX,sy:ev.clientY,ox:r.left,oy:r.top,active:false};
  try{ b.setPointerCapture(ev.pointerId); }catch(e){}
 });
 b.addEventListener('pointermove',ev=>{
  if(!lpDrag||lpDrag.el!==b) return;
  const st=lpDrag, dx=ev.clientX-st.sx, dy=ev.clientY-st.sy;
  if(!st.active){
   if(Math.hypot(dx,dy)<6) return;
   const g=b.cloneNode(true);
   g.classList.add('lp-ghost');
   g.style.left=st.ox+'px'; g.style.top=st.oy+'px'; g.style.width=b.offsetWidth+'px'; g.style.margin='0';
   document.body.appendChild(g);
   st.ghost=g; st.active=true;
   b.classList.add('dragsrc');
   suppressClickUntil=Date.now()+400;
  }
  st.ghost.style.left=(st.ox+dx)+'px';
  st.ghost.style.top=(st.oy+dy)+'px';
  st.px=ev.clientX; st.py=ev.clientY;
 });
 const finish=commit=>{
  if(!lpDrag||lpDrag.el!==b) return;
  const st=lpDrag; lpDrag=null;
  if(st.ghost) st.ghost.remove();
  b.classList.remove('dragsrc');
  if(!st.active) return;
  suppressClickUntil=Date.now()+300;
  if(!commit) return;
  const dims=lpDims(innerWidth,innerHeight);
  const pageItems=vis.slice(lpPage*dims.per,(lpPage+1)*dims.per);
  const originInPage=pageItems.findIndex(e=>e.key===st.en.key);
  if(originInPage<0) return;
  const pageEls=[...lpPagesEl.querySelectorAll('.lp-page')].find(pg=>pg.style.display!=='none');
  if(!pageEls) return;
  const centers=[...pageEls.querySelectorAll('.lp-item')].map(el=>{ const r=el.getBoundingClientRect(); return {x:r.left+r.width/2,y:r.top+r.height/2}; });
  let cnt=lpInsertIndex(centers,st.px!=null?st.px:st.sx,st.py!=null?st.py:st.sy);
  const selfC=centers[originInPage];
  const selfBefore=selfC&&(selfC.y<st.py-40||(Math.abs(selfC.y-st.py)<=40&&selfC.x<st.px));
  let toInPage=cnt-(selfBefore?1:0);
  toInPage=Math.max(0,Math.min(pageItems.length-1,toInPage));
  if(toInPage===originInPage) return;
  const fromVis=lpPage*dims.per+originInPage, toVis=lpPage*dims.per+toInPage;
  const visKeys=vis.map(e=>e.key);
  const moved=dragMovedArray(visKeys,fromVis,toVis);
  /* write back through the SAME slots of DESKORDER: only the
     Launchpad-visible keys permute, everything else (imported
     file icons) keeps its exact place -- one layout source. */
  const slotOf=new Map(); DESKORDER.forEach((k,i)=>{ if(visKeys.indexOf(k)>=0) slotOf.set(k,i); });
  const next=DESKORDER.slice();
  moved.forEach((k,i)=>{ next[slotOf.get(visKeys[i])]=k; });
  DESKORDER=next;
  persistDeskPos(); renderDesk(); paintLP();
 };
 b.addEventListener('pointerup',()=>finish(true));
 b.addEventListener('pointercancel',()=>finish(false));
 return b;
}
lpQ.addEventListener('input',()=>{ lpPage=0; paintLP(); });
lpQ.addEventListener('keydown',e=>{
 if(e.key==='Enter'){
  const vis=lpVisible();
  if(vis.length){ const en=vis[0]; if(en.key.indexOf('folder:')===0){ en.open(null); } else { closeLP(); en.open(); } }
 }
 e.stopPropagation();
});
lpEl.addEventListener('click',e=>{ if(e.target===lpEl||e.target===lpPagesEl||e.target.classList.contains('lp-page')) closeLP(); });
lpEl.addEventListener('wheel',e=>{
 if(!lpIsOpen) return;
 const now=Date.now(); if(now-lpWheelAt<240) return;
 if(Math.abs(e.deltaY)<8) return;
 lpWheelAt=now;
 lpPageGo(e.deltaY>0?1:-1);
},{passive:true});
addEventListener('resize',()=>{ if(lpIsOpen) paintLP(); });
/* Dock entry: Launchpad sits right after the first pinned app
   （工作站， the Finder slot), where macOS keeps it. It is a
   launcher, not an app: no running dot, no window. */
(function(){
 const it=document.createElement('div');
 it.className='ditem';
 it.innerHTML='<span class="dlabel">启动台</span><button class="dockbtn" title="启动台"><div class="appico" style="background:linear-gradient(180deg,#9a9aa2,#55555e)"><svg width="30" height="30" viewBox="0 0 30 30" aria-hidden="true"><g fill="#fff"><rect x="3" y="3" width="7" height="7" rx="2"/><rect x="11.5" y="3" width="7" height="7" rx="2"/><rect x="20" y="3" width="7" height="7" rx="2"/><rect x="3" y="11.5" width="7" height="7" rx="2"/><rect x="11.5" y="11.5" width="7" height="7" rx="2"/><rect x="20" y="11.5" width="7" height="7" rx="2"/><rect x="3" y="20" width="7" height="7" rx="2"/><rect x="11.5" y="20" width="7" height="7" rx="2"/><rect x="20" y="20" width="7" height="7" rx="2"/></g></svg></div></button>';
 it.querySelector('.dockbtn').onclick=()=>{ lpIsOpen?closeLP():openLP(); };
 if(dockEl.children.length>1) dockEl.insertBefore(it,dockEl.children[1]);
 else dockEl.appendChild(it);
})();

export const __bind_lpIsOpen: any = { get(): any { return lpIsOpen; }, set(v: any): any { lpIsOpen = v; return v; } };

export const __bind_lpPage: any = { get(): any { return lpPage; }, set(v: any): any { lpPage = v; return v; } };

export const __bind_lpDrag: any = { get(): any { return lpDrag; }, set(v: any): any { lpDrag = v; return v; } };

export const __bind_lpWheelAt: any = { get(): any { return lpWheelAt; }, set(v: any): any { lpWheelAt = v; return v; } };

/* v0.59 block 3 (item #8): Launchpad reflects files / webapp /
   layout mutations live while it is open -- re-pull the file
   list, let bundle 1's own refresh settle the webapp/layout
   globals, then repaint in place. Closed Launchpad does
   nothing (it rebuilds its entries on open anyway). */
try{
 if(typeof MJEvents!=='undefined'&&MJEvents&&MJEvents.on){
  const lpLive=()=>{ if(!lpIsOpen) return; setTimeout(async()=>{ try{ await loadDeskFiles(); if(lpIsOpen) paintLP(); }catch(e){} },200); };
  MJEvents.on('files',lpLive);
  MJEvents.on('webapps',lpLive);
  MJEvents.on('layout',lpLive);
 }
}catch(e){}
