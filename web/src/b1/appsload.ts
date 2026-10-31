/* Generated cross-module imports (v0.58 modularization):
   every name below is owned by the sibling module it is imported
   from -- the same names the pre-modular page shared through one
   script scope. */
import { api, esc, verQ } from './core';
import { glyph } from './glyphs';
import { gotoSys } from './settings';
import { openApp } from './windows';
import { FFSTATE, __bind_FFSTATE } from './session';
import { LAYOUT, folderById, folderItems, recOpen, saveLayout, webIconHTML, webLetter } from './entries';
import { renderDesk } from './desktop';
import { ctxEl, hideCtx } from './folders';
import { ctxItemHTML } from './ctxmenu';
import { openDeskFile } from './viewers';
/* ----- portable apps from the store (v0.24): desktop icons ----- */
export let STORECAT=[], STOREAPPS=[];
export function storeIconHTML(a){
 if(a&&a.icon) return `<div class="appico webapp"><span class="icolet">${esc(webLetter(a.name))}</span><img src="${esc(a.icon)}" alt="" draggable="false" loading="lazy" onerror="this.remove()"></div>`;
 return webIconHTML({name:a?a.name:'',url:(a&&a.homepage)||''});
}
export async function loadStoreApps(){
 try{const r=await api('/api/apps/catalog');STORECAT=r.apps||[];STOREAPPS=STORECAT.filter(a=>a.installed)}catch(e){STORECAT=[];STOREAPPS=[]}
}
export let launchStoreApp: any = async function launchStoreApp(id){
 const r=await api('/api/apps/launch',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({id})}).catch(()=>null);
 if(r&&r.error) alert(r.error);
};
/* ----- boxed apps (v0.39): real installers contained by the
   Sandboxie component, registered in Data/sandbox/apps.json. On
   the desktop they are first-class icons (key box:<id>, kind
   'native' like store apps: reorder yes, folders no). ----- */
export let BOXAPPS=[];
export async function loadBoxApps(){
 try{const r=await api('/api/sandbox/apps');BOXAPPS=r.apps||[]}catch(e){BOXAPPS=[]}
}
/* v0.51 productivity components (ONLYOFFICE / LibreOffice /
   ImageGlass / Krita / VSCodium): on-demand downloads living in
   Data/components/<id>, served by /api/components (procomp.go).
   Icons are the apps' real ones, extracted from their exes by the
   daemon; uninstalled ones render dimmed with a 「未下载」 badge
   and a click starts the download (the Firefox/OpenClaw flow). */
export let PRODAPPS=[];
export function prodBusy(c){ return c&&(c.phase==='downloading'||c.phase==='extracting'); }
export function prodPct(c){ if(!c||!c.size) return 0; return Math.max(0,Math.min(100,Math.floor((c.progress||0)*100/c.size))); }
export function prodIconHTML(c){
 const badge=!c.installed?`<span class="dlb">${prodBusy(c)?('下载 '+prodPct(c)+'%'):'未下载'}</span>`:'';
 const img=c.installed?`<img alt="" src="${verQ('/api/components/icon?id='+encodeURIComponent(c.id))}" onerror="this.remove()">`:'';
 return `<div class="appico webapp${c.installed?'':' notin'}"><span class="icolet">${esc(c.name.replace(/[^A-Za-z]/g,'').slice(0,1)||c.name.slice(0,1))}</span>${img}${badge}</div>`;
}
export async function loadProdApps(){
 try{const r=await api('/api/components');PRODAPPS=(r&&r.components)||[]}catch(e){PRODAPPS=[]}
}
/* v0.54: Firefox desktop parity. Firefox was always IN the
   built-in APPS list (desktop / Dock / Launchpad / Spotlight /
   recents included it), but as a plain emoji tile: no install
   state, and a click-when-missing only opened its page. It now
   gets the productivity components' treatment -- the real exe
   icon once installed, a state badge fed by /api/firefox, and a
   click-when-missing that starts the download (the Firefox page
   is its progress surface, like 设置 → 组件 is for PRODAPPS).
   FFSTATE itself is declared much earlier, ahead of the Dock
   builder loop (see the v0.55 note there); this section only
   loads and consumes it. */
export async function loadFFState(){
 try{ __bind_FFSTATE.set(await api('/api/firefox')); }catch(e){ __bind_FFSTATE.set(null); }
}
export function ffBusy(c){ return c&&(c.phase==='downloading'||c.phase==='extracting'); }
export function ffPct(c){ if(!c||!c.size) return 0; return Math.max(0,Math.min(100,Math.floor((c.progress||0)*100/c.size))); }
export function ffDeskIconHTML(){
 const c=FFSTATE||{};
 const badge=!c.installed?`<span class="dlb">${ffBusy(c)?('下载 '+ffPct(c)+'%'):(c.phase==='error'?'下载失败':'未下载')}</span>`:'';
 const img=c.installed?`<img alt="" src="${verQ('/api/firefox/icon')}" onerror="this.remove()">`:'';
 return `<div class="appico webapp${c.installed?'':' notin'}"><span class="icolet">F</span>${img}${badge}</div>`;
}
export async function launchFirefoxEntry(){
 const c=FFSTATE;
 if(c&&!c.installed){
  if(ffBusy(c)){ openApp('firefox'); return; }
  const r=await api('/api/firefox/download',{method:'POST'}).catch(()=>null);
  if(r&&r.error){ alert(r.error); return; }
  __bind_FFSTATE.set({...(c||{}),phase:'downloading'}); renderDesk();
  toast('开始下载 Firefox，在 Firefox 页看进度');
  openApp('firefox');
  return;
 }
 openApp('firefox');
}
export let launchProdApp: any = async function launchProdApp(id){
 const c=PRODAPPS.find(x=>x.id===id); if(!c) return;
 if(!c.installed){
   if(prodBusy(c)){ toast('正在下载 '+c.name+'（'+prodPct(c)+'%），下完点图标就能打开'); return; }
   const r=await api('/api/components/install',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({id})});
   if(r&&r.error){ alert(r.error); return; }
   toast('开始下载 '+c.name+'，在「设置 → 组件」里看进度');
   gotoSys('components');
   return;
 }
 const r=await api('/api/components/launch',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({id})});
 if(r&&r.error) alert(r.error);
};
export let launchBoxApp: any = async function launchBoxApp(id){
 const r=await api('/api/sandbox/launch',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({id})}).catch(()=>null);
 if(r&&r.error){
  /* v0.43: a ghost registration (the file it names is gone, or
     was never an app) gets MJ's explanation plus a one-click
     removal, never Sandboxie's raw popup. */
  if(r.ghost){
   if(confirm(r.error+'\n\n要现在移除这条登记吗？（只删这条登记，盒子里的其他东西不动）')){
    const rr=await api('/api/sandbox/remove',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({id})}).catch(()=>null);
    if(rr&&rr.error) alert(rr.error); else { await loadBoxApps(); renderDesk(); }
   }
   return;
  }
  alert(r.error);
 }
};
/* ----- imported files on the desktop (v0.32) -----
   Host files enter ONLY by the user's hand: drag them onto the
   desktop, the desktop menu's 「从电脑导入文件…」 (the OS picker),
   or paste. The daemon copies them into Data/files (originals on
   the host are never touched) and they show up here as icons mixed
   with the apps. Every open route stays inside the system; the
   only way back out is the explicit export (a browser download
   into the host's Downloads folder, labelled as such). */
export let DESKFILES=[], SUBLIST=[];
export async function loadDeskFiles(){
 try{ const r=await api('/api/files'); DESKFILES=r.files||[]; }catch(e){ DESKFILES=[]; }
}
export function fileIconHTML(f){
 if(f.dir) return `<div class="appico fileico g-f-dir" style="color:#fff">${glyph('folder',26)}</div>`;
 const ext=(f.ext||'').toUpperCase();
 if(f.kind==='image'&&['jpg','jpeg','png','gif'].includes(f.ext)){
  /* Real thumbnail from the daemon; if it cannot render one
     (webp/bmp), the img removes itself and the type tile shows. */
  return `<div class="appico fileico g-f-img"><span class="ext">${esc(ext)}</span><img src="/api/files/thumb?name=${encodeURIComponent(f.name)}" alt="" draggable="false" loading="lazy" onerror="this.remove()"></div>`;
 }
 /* v0.54: media tiles -- a music note for audio, a play glyph
    for video (the daemon's thumb endpoint only decodes still
    images, so a video never has a thumbnail to reuse; if one
    ever exists it would come through the image path above). */
 const mg=fvMediaGroup(f);
 if(mg==='music') return `<div class="appico fileico g-f-aud">♪</div>`;
 if(mg==='video') return `<div class="appico fileico g-f-vid">▶</div>`;
 const g={pdf:'g-f-pdf',image:'g-f-img',text:'g-f-txt',csv:'g-f-csv',docx:'g-f-doc',xlsx:'g-f-xls',pptx:'g-f-ppt',zip:'g-f-zip',exe:'g-f-exe'}[f.kind]||'g-f-other';
 return `<div class="appico fileico ${g}"><span class="ext">${esc(ext||'文件')}</span></div>`;
}
export let toastT=null;
export function toast(msg){
 const t=document.getElementById('toast'); if(!t) return;
 t.textContent=msg; t.classList.remove('hidden');
 clearTimeout(toastT); toastT=setTimeout(()=>t.classList.add('hidden'),2600);
}
export let importBusy=false;
export async function importFiles(fileList,via){
 const files=[...fileList].filter(f=>f&&f.name);
 if(!files.length||importBusy) return;
 importBusy=true;
 toast('正在导入 '+files.length+' 个文件…');
 let ok=0; const fails=[];
 for(const f of files){
  try{
   const fd=new FormData(); fd.append('via',via); fd.append('file',f,f.name);
   const r=await (await fetch('/api/files/import',{method:'POST',body:fd})).json();
   ok+=(r.imported||[]).length;
   (r.errors||[]).forEach(e=>fails.push(e.name+'：'+e.error));
  }catch(e){ fails.push(f.name+'：上传失败'); }
 }
 importBusy=false;
 await loadDeskFiles();
 if(LAYOUT.hiddenFiles&&LAYOUT.hiddenFiles.length){ const before=LAYOUT.hiddenFiles.length; LAYOUT.hiddenFiles=LAYOUT.hiddenFiles.filter(n=>!files.some(fl=>fl.name===n)); if(LAYOUT.hiddenFiles.length!==before) saveLayout(); }
 /* v0.56: an import started from inside a folder （导入文件 in
    the folder popup) lands the new files in THAT folder. */
 const intoFid=window.__mjFolderImport; window.__mjFolderImport=null;
 if(intoFid&&ok){
  const f=folderById(intoFid);
  if(f){ files.forEach(fl=>{ const k='file:'+fl.name; if(DESKFILES.some(d=>d.name===fl.name)&&!folderItems(f).includes(k)) f.items.push(k); }); saveLayout(); }
 }
 renderDesk();
 if(window.__filesRefresh) window.__filesRefresh(); /* v0.50: an open Finder window repaints too */
 if(fails.length) alert('有 '+fails.length+' 个文件没导入成功：\n'+fails.join('\n'));
 else if(ok) toast('已导入 '+ok+' 个文件到桌面');
}
/* drag-in: the veil is pointer-events:none; the document-level
   handlers both catch the drop and stop Edge from navigating to
   the dropped file (the desktop view is an Edge app window). */
export let dragFilesDepth=0;
document.addEventListener('dragenter',ev=>{
 if(ev.dataTransfer&&[...ev.dataTransfer.types].includes('Files')){ dragFilesDepth++; document.getElementById('dropveil').classList.remove('hidden'); }
});
document.addEventListener('dragleave',ev=>{
 if(ev.dataTransfer&&[...ev.dataTransfer.types].includes('Files')&&--dragFilesDepth<=0){ dragFilesDepth=0; document.getElementById('dropveil').classList.add('hidden'); }
});
document.addEventListener('dragover',ev=>{ ev.preventDefault(); });
document.addEventListener('drop',ev=>{
 ev.preventDefault(); dragFilesDepth=0; document.getElementById('dropveil').classList.add('hidden');
 if(ev.dataTransfer&&ev.dataTransfer.files&&ev.dataTransfer.files.length) importFiles(ev.dataTransfer.files,'drag');
});
document.addEventListener('paste',ev=>{
 const t=ev.target;
 if(t&&(t.tagName==='INPUT'||t.tagName==='TEXTAREA'||t.isContentEditable)) return;
 if(ev.clipboardData&&ev.clipboardData.files&&ev.clipboardData.files.length){ ev.preventDefault(); importFiles(ev.clipboardData.files,'paste'); }
});
document.getElementById('deskimp').addEventListener('change',e=>{ importFiles(e.target.files,'picker'); e.target.value=''; });
/* file icon right-click menu: open / reveal / rename / export /
   remove-icon / delete. v0.56: 从桌面移除 (icon only, the file
   stays in Data/files and in the Files app) and 删除文件 (the
   real delete, confirmed) are deliberately separate items. */
export function showFileCtx(x,y,f){
 /* test2: painted by the shared ctxItemHTML (one component,
    unified glyphs) instead of hand-rolled rows. */
 ctxEl.innerHTML=[
  {act:'fc-open',label:'打开',icon:'open'},
  {act:'fc-reveal',label:'在「文件」中显示',icon:'folder'},
  {act:'fc-rename',label:'重命名…',icon:'rename'},
  {act:'fc-export',label:'导出到电脑（下载文件夹）',icon:'export'},
  {act:'fc-unpin',label:'从桌面移除',icon:'eyeoff'},
  {act:'fc-del',label:'删除文件',icon:'trash',danger:true},
 ].map(ctxItemHTML).join('');
 ctxEl.classList.remove('hidden');
 ctxEl.style.left=Math.min(x,innerWidth-220)+'px';
 ctxEl.style.top=Math.min(y,innerHeight-260)+'px';
 const fc=a=>ctxEl.querySelector(`[data-act="${a}"]`);
 fc('fc-open').onclick=()=>{ hideCtx(); recOpen('file:'+f.name); openDeskFile(f.path,f.kind); };
 fc('fc-reveal').onclick=()=>{ hideCtx(); openApp('files'); let n=0; const t=setInterval(()=>{ if((window.__filesReveal&&window.__filesReveal(f.path))||++n>12) clearInterval(t); },120); };
 fc('fc-unpin').onclick=()=>{
  hideCtx();
  /* Icon only: the name goes on the layout's hidden list; the
     file itself stays in Data/files and in the Files app, and
     re-importing the same name brings the icon back. */
  LAYOUT.hiddenFiles=LAYOUT.hiddenFiles||[];
  if(!LAYOUT.hiddenFiles.includes(f.name)) LAYOUT.hiddenFiles.push(f.name);
  delete LAYOUT.pos['file:'+f.name];
  saveLayout(); renderDesk();
  toast('已从桌面移除（文件还在「文件」里）');
 };
 fc('fc-export').onclick=()=>{ hideCtx(); exportDeskFile(f); };
 fc('fc-rename').onclick=async()=>{
  hideCtx();
  const nn=prompt('新名字（不能带路径）',f.name);
  if(!nn||nn.trim()===f.name) return;
  const r=await api('/api/files/rename',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({old:f.name,new:nn.trim()})});
  if(r.error){ alert('改名失败：'+r.error); return; }
  if(LAYOUT.pos['file:'+f.name]){ LAYOUT.pos['file:'+r.name]=LAYOUT.pos['file:'+f.name]; delete LAYOUT.pos['file:'+f.name]; }
  saveLayout(); await loadDeskFiles(); renderDesk();
 };
 fc('fc-del').onclick=async()=>{
  hideCtx();
  if(!confirm('删除终端桌面上的「'+f.name+'」？\n删的是终端里的副本（在 Data/files 里），电脑上的原文件不受影响。')) return;
  const r=await api('/api/files/delete',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({name:f.name})});
  if(r.error){ alert('删除失败：'+r.error); return; }
  delete LAYOUT.pos['file:'+f.name];
  /* v0.56: a deleted file also leaves every folder and the
     hidden list -- no dangling membership. */
  LAYOUT.folders.forEach(fd=>{ fd.items=folderItems(fd).filter(k=>k!=='file:'+f.name); });
  LAYOUT.hiddenFiles=(LAYOUT.hiddenFiles||[]).filter(n=>n!==f.name);
  saveLayout(); await loadDeskFiles(); renderDesk();
 };
}
export function exportDeskFile(f){
 const a=document.createElement('a');
 a.href='/api/files/raw?path='+encodeURIComponent(f.path)+'&dl=1';
 document.body.appendChild(a); a.click(); a.remove();
}
export function setWinTitle(id,t){ const b=document.querySelector('#win-'+id+' .title b'); if(b) b.textContent=t; }
export const __bind_STOREAPPS: any = { get(): any { return STOREAPPS; }, set(v: any): any { STOREAPPS = v; return v; } };

export const __bind_launchStoreApp: any = { get(): any { return launchStoreApp; }, set(v: any): any { launchStoreApp = v; return v; } };

export const __bind_BOXAPPS: any = { get(): any { return BOXAPPS; }, set(v: any): any { BOXAPPS = v; return v; } };

export const __bind_PRODAPPS: any = { get(): any { return PRODAPPS; }, set(v: any): any { PRODAPPS = v; return v; } };

export const __bind_launchProdApp: any = { get(): any { return launchProdApp; }, set(v: any): any { launchProdApp = v; return v; } };

export const __bind_launchBoxApp: any = { get(): any { return launchBoxApp; }, set(v: any): any { launchBoxApp = v; return v; } };

export const __bind_DESKFILES: any = { get(): any { return DESKFILES; }, set(v: any): any { DESKFILES = v; return v; } };

export const __bind_STORECAT: any = { get(): any { return STORECAT; }, set(v: any): any { STORECAT = v; return v; } };

export const __bind_SUBLIST: any = { get(): any { return SUBLIST; }, set(v: any): any { SUBLIST = v; return v; } };

export const __bind_toastT: any = { get(): any { return toastT; }, set(v: any): any { toastT = v; return v; } };

export const __bind_importBusy: any = { get(): any { return importBusy; }, set(v: any): any { importBusy = v; return v; } };

export const __bind_dragFilesDepth: any = { get(): any { return dragFilesDepth; }, set(v: any): any { dragFilesDepth = v; return v; } };
