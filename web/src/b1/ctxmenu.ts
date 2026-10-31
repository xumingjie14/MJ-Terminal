/* Generated cross-module imports (v0.58 modularization):
   every name below is owned by the sibling module it is imported
   from -- the same names the pre-modular page shared through one
   script scope. */
import { api, esc } from './core';
import { glyph } from './glyphs';
import { deskIconSize } from './settings';
import { openApp } from './windows';
import { FFSTATE } from './session';
import { LAYOUT, WEBAPPS, deskSortMode, entryMeta, folderById, folderItems, recOpen, saveLayout, setDeskIconSize, setDeskSort, suppressClickUntil, __bind_WEBAPPS, __bind_suppressClickUntil } from './entries';
import { renderDesk } from './desktop';
import { cancelDrag, dragState, __bind_dragState } from './drag';
import { cancelFolderDrag, closeFolderPop, ctxEl, fpDrag, hideCtx, openFolderPop, __bind_fpDrag } from './folders';
import { DESKFILES, PRODAPPS, STOREAPPS, loadFFState, loadProdApps, loadStoreApps, showFileCtx, toast } from './appsload';
/* ----- context menus (v0.56: one component, content by kind) -----
   ctxOpen paints items into the shared #ctxmenu; an item with a
   `sub` payload gets a hover submenu (radio items carry a ✓).
   The blank-desktop menu keeps its four classics and gains
   图标大小 / 排序方式 submenus (item 7); icon right-clicks get
   per-item menus (item 5) via showItemCtx. */
export function ctxItemHTML(it){
 const sub=it.sub?`<div class="ctxSub">${it.sub.map(ctxItemHTML).join('')}</div>`:'';
 /* test2: every item may carry `icon` -- a name from the one
    unified GLYPHS set. No emoji in menus, ever. */
 const ic=it.icon?`<span class="ctxic">${glyph(it.icon,16)}</span>`:'';
 return `<div class="ctxitem${it.sub?' hasSub':''}" data-act="${it.act||''}" ${it.arg?`data-arg="${esc(it.arg)}"`:''} ${it.danger?'style="color:#ff9d97"':''}>${it.radio?`<i class="radio">${it.on?'✓':''}</i>`:''}${ic}${esc(it.label)}${sub}</div>`;
}
export function ctxOpen(x,y,items,onPick){
 ctxEl.innerHTML=items.map(it=>it.sep?'<div style="height:1px;background:rgba(255,255,255,.16);margin:5px 8px"></div>':ctxItemHTML(it)).join('');
 ctxEl.classList.remove('hidden');
 const flip=x>innerWidth-440;
 ctxEl.querySelectorAll('.ctxSub').forEach(s=>s.classList.toggle('flip',flip));
 ctxEl.style.left=Math.max(4,Math.min(x,innerWidth-216))+'px';
 ctxEl.style.top=Math.max(30,Math.min(y,innerHeight-360))+'px';
 ctxEl.querySelectorAll('.ctxitem[data-act]').forEach(el=>{
  el.onclick=ev=>{
   if(el.classList.contains('hasSub')){ el.classList.toggle('openSub'); ev.stopPropagation(); return; }
   const act=el.dataset.act, arg=el.dataset.arg;
   hideCtx(); onPick&&onPick(act,arg);
  };
 });
}
export function showCtx(x,y){
 const sz=deskIconSize(), sm=deskSortMode();
 const sizes=[['小',42],['标准',54],['大',68],['特大',84]].map(([n,v])=>({act:'size',arg:String(v),label:n+'（'+v+'）',radio:true,on:sz===v}));
 const sorts=[['manual','手动'],['name','名称'],['freq','最常用'],['kind','种类'],['added','添加日期']].map(([v,n])=>({act:'sort',arg:v,label:n,radio:true,on:(sm===v)||(v==='manual'&&sm==='manual')}));
 ctxOpen(x,y,[
  {act:'nf',label:'新建文件夹',icon:'folderplus'},
  {act:'imp',label:'从电脑导入文件…',icon:'import'},
  {sep:true},
  {label:'图标大小',icon:'size',sub:sizes},
  {label:'排序方式',icon:'sort',sub:sorts},
  {sep:true},
  {act:'rf',label:'刷新',icon:'refresh'},
  {act:'ps',label:'个性化',icon:'palette'},
 ],(act,arg)=>{
  if(act==='nf') newFolder(x,y);
  else if(act==='imp') document.getElementById('deskimp').click();
  else if(act==='rf') location.reload();
  else if(act==='ps') openApp('sys');
  else if(act==='size') setDeskIconSize(+arg);
  else if(act==='sort') setDeskSort(arg);
 });
}
/* Per-item menu content by entry kind + state (item 5). Web
   apps: 打开 / 重命名…（test2 item 6: display name only, the
   URL is untouched -- still deliberately NO full 编辑 form) /
   调整窗口▸（记忆·手机·桌面·全屏， persisted on the entry,
   applied at next launch) / 卸载. Store: 打开 / 卸载 (the
   store's own uninstall, data warning included). Components
   & Firefox: 打开 / 卸载组件. System apps: 打开 only (their
   names are canonical -- no rename). Folders: 打开 / 重命名 /
   解散. Imported files delegate to showFileCtx (which carries
   重命名 for the real file). */
export function showItemCtx(x,y,key,kind,fid){
 const openIt=()=>{ const m=entryMeta(key); if(m){ recOpen(key); m.open(); } };
 if(key.startsWith('file:')){
  const f=DESKFILES.find(z=>'file:'+z.name===key);
  if(f){ showFileCtx(x,y,f); return; }
 }
 if(kind==='folder'||key.startsWith('folder:')){
  const f=folderById(fid||key.slice(7)); if(!f) return;
  ctxOpen(x,y,[
   {act:'open',label:'打开',icon:'open'},
   {act:'rename',label:'重命名…',icon:'rename'},
   {act:'dissolve',label:'解散文件夹',icon:'folderup',danger:true},
  ],act=>{
   if(act==='open'){ const el=document.querySelector(`.icon[data-key="${CSS.escape(key)}"]`); openFolderPop(f.id,el); }
   else if(act==='rename'){ const el=document.querySelector(`.icon[data-key="${CSS.escape(key)}"]`); openFolderPop(f.id,el,{editTitle:true}); }
   else if(act==='dissolve'){
    if(!confirm('解散「'+f.name+'」？里面的图标会回到桌面，文件夹本身删掉。')) return;
    folderItems(f).forEach(k=>{ delete LAYOUT.pos[k]; });
    LAYOUT.folders=LAYOUT.folders.filter(z=>z.id!==f.id);
    delete LAYOUT.pos[key];
    saveLayout(); renderDesk();
    toast('已解散文件夹，图标回到桌面');
   }
  });
  return;
 }
 if(key.startsWith('web:')){
  const a=WEBAPPS.find(w=>'web:'+w.url===key); if(!a) return;
  const cur=a.winmode||'memory';
  const modes=[['memory','记忆'],['phone','手机'],['desktop','桌面'],['fullscreen','全屏']].map(([v,n])=>({act:'winmode',arg:v,label:n,radio:true,on:cur===v}));
  ctxOpen(x,y,[
   {act:'open',label:'打开',icon:'open'},
   {act:'rename',label:'重命名…',icon:'rename'},
   {label:'调整窗口',icon:'window',sub:modes},
   {act:'uninstall',label:'卸载',icon:'trash',danger:true},
  ],async(act,arg)=>{
   if(act==='open') openIt();
   else if(act==='rename'){
    const nn=prompt('网页应用的新名字（只改显示名，网址不变）',a.name);
    if(!nn||!nn.trim()||nn.trim()===a.name) return;
    const r=await api('/api/apps/save',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({url:a.url,name:nn.trim()})});
    if(r&&r.error){ alert('改名失败：'+r.error); return; }
    await loadWebApps(); renderDesk();
    toast('已改名为「'+nn.trim()+'」');
   }
   else if(act==='winmode'){
    const r=await api('/api/apps/save',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({url:a.url,winmode:arg})});
    if(r&&r.error){ alert(r.error); return; }
    await loadWebApps();
    toast('窗口模式已设为「'+({memory:'记忆',phone:'手机',desktop:'桌面',fullscreen:'全屏'})[arg]+'」，下次打开生效');
   }
   else if(act==='uninstall'){
    if(!confirm('卸载网站应用「'+a.name+'」？\n只是删掉这个桌面入口，网站本身和浏览器里的登录数据不受影响。')) return;
    const r=await api('/api/apps/save',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({url:a.url,delete:true})});
    if(r&&r.error){ alert(r.error); return; }
    LAYOUT.folders.forEach(fd=>{ fd.items=folderItems(fd).filter(k=>k!==key); });
    delete LAYOUT.pos[key];
    saveLayout(); await loadWebApps(); renderDesk();
   }
  });
  return;
 }
 if(key.startsWith('store:')){
  const a=STOREAPPS.find(s=>'store:'+s.id===key); if(!a) return;
  ctxOpen(x,y,[{act:'open',label:'打开',icon:'open'},{act:'uninstall',label:'卸载',icon:'trash',danger:true}],async act=>{
   if(act==='open') openIt();
   else if(act==='uninstall'){
    if(!confirm('卸载 '+a.name+'？\n便携应用的程序和数据都在同一个文件夹里，卸载会把 '+a.name+' 的本地数据（如聊天记录、配置）一起全部删掉，不可恢复。')) return;
    const r=await api('/api/apps/uninstall',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({id:a.id})});
    if(r&&r.error){ alert(r.error); return; }
    LAYOUT.folders.forEach(fd=>{ fd.items=folderItems(fd).filter(k=>k!==key); });
    saveLayout(); await loadStoreApps(); renderDesk();
   }
  });
  return;
 }
 if(key.startsWith('prod:')){
  const c=PRODAPPS.find(p=>'prod:'+p.id===key); if(!c) return;
  ctxOpen(x,y,[{act:'open',label:'打开',icon:'open'},{act:'uninstall',label:'卸载组件',icon:'trash',danger:true}],async act=>{
   if(act==='open') openIt();
   else if(act==='uninstall'){
    if(!confirm('卸载 '+c.name+'？\n程序和它在组件目录里的数据会一起删掉，不可恢复。')) return;
    const r=await api('/api/components/uninstall',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({id:c.id})});
    if(r&&r.error){ alert(r.error); return; }
    LAYOUT.folders.forEach(fd=>{ fd.items=folderItems(fd).filter(k=>k!==key); });
    saveLayout(); await loadProdApps(); renderDesk();
   }
  });
  return;
 }
 if(key==='app:firefox'){
  const items: any[]=[{act:'open',label:'打开',icon:'open'}];
  if(FFSTATE&&FFSTATE.installed) items.push({act:'uninstall',label:'卸载组件',icon:'trash',danger:true});
  ctxOpen(x,y,items,async act=>{
   if(act==='open') openIt();
   else if(act==='uninstall'){
    if(!confirm('卸载 Firefox 组件？\n程序会删掉；你的 Firefox 档案（登录、书签，在 Data/FirefoxProfile）保留，重装后还在。')) return;
    const r=await api('/api/firefox/uninstall',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({})});
    if(r&&r.error){ alert(r.error); return; }
    await loadFFState(); renderDesk();
    toast('Firefox 组件已卸载（档案保留）');
   }
  });
  return;
 }
 /* System apps and anything unlisted: 打开 only. */
 ctxOpen(x,y,[{act:'open',label:'打开',icon:'open'}],()=>openIt());
}
export function newFolder(x,y){
 const id='f'+Date.now().toString(36);
 LAYOUT.folders.push({id,name:'文件夹',items:[]});
 LAYOUT.pos['folder:'+id]={x:Math.max(4,Math.round(x-46)),y:Math.max(44,Math.round(y-20))};
 saveLayout(); renderDesk();
 const tile=document.querySelector(`.icon[data-fid="${id}"]`);
 if(tile) openFolderPop(id,tile);
}
document.addEventListener('contextmenu',ev=>{
 if(ev.target.closest('.win')||ev.target.closest('#dockwrap')||ev.target.closest('#menubar')||ev.target.closest('#folderpop')||ev.target.closest('#ctxmenu')||ev.target.closest('#launchpad')||ev.target.closest('#spotlight')||ev.target.closest('#mbdrop')||ev.target.closest('#extstrip')) return;
 ev.preventDefault(); showCtx(ev.clientX,ev.clientY);
});
document.addEventListener('dragstart',ev=>{
 if(ev.target.closest&&ev.target.closest('.icon,.fp-item')) ev.preventDefault();
});
document.addEventListener('pointerdown',ev=>{
 if(!ev.target.closest||!ev.target.closest('.icon')) document.querySelectorAll('.icon.sel').forEach(x=>x.classList.remove('sel'));
 if(!ev.target.closest('#ctxmenu')) hideCtx();
 const pop=document.getElementById('folderpop');
 if(pop&&!pop.classList.contains('hidden')&&!ev.target.closest('#folderpop')&&!ev.target.closest('.icon')) closeFolderPop();
});
document.addEventListener('keydown',ev=>{
 if(ev.key!=='Escape') return;
 /* An in-flight drag cancels first: everything slides back to its
    original slot and nothing is saved (Launchpad behavior). */
 if(dragState&&dragState.active){ const ds=dragState; __bind_dragState.set(null); __bind_suppressClickUntil.set(Date.now()+300); cancelDrag(ds); return; }
 if(fpDrag&&fpDrag.active){ const st=fpDrag; __bind_fpDrag.set(null); cancelFolderDrag(st); return; }
 hideCtx(); closeFolderPop();
});

export async function loadWebApps(){
 try{const r=await api('/api/apps');__bind_WEBAPPS.set(r.apps||[])}catch(e){__bind_WEBAPPS.set([])}
 /* v0.56: the old prune (drop folder items that are not web
    apps) IS the kind restriction item 4 removes. Now: legacy
    bare-URL items migrate to web: keys, and only web: keys
    whose app vanished are pruned -- other kinds are validated
    by their own catalogs at render time (entryMeta skips what
    it cannot resolve), never deleted from under the user here. */
 LAYOUT.folders.forEach(f=>{ folderItems(f); f.items=f.items.filter(k=>!k.startsWith('web:')||WEBAPPS.some(a=>'web:'+a.url===k)); });
}