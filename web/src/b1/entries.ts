/* Generated cross-module imports (v0.58 modularization):
   every name below is owned by the sibling module it is imported
   from -- the same names the pre-modular page shared through one
   script scope. */
import { APPS, api, esc } from './core';
import { glyph } from './glyphs';
import { SETTINGS, applyDeskIconSize, deskPitch, deskRenderedOnce, saveSetting } from './settings';
import { appIcon, openApp } from './windows';
import { renderDesk } from './desktop';
import { attachDrag, effPosFor } from './drag';
import { showItemCtx } from './ctxmenu';
import { BOXAPPS, DESKFILES, PRODAPPS, STOREAPPS, fileIconHTML, launchBoxApp, launchFirefoxEntry, launchProdApp, launchStoreApp, prodIconHTML, storeIconHTML } from './appsload';
import { openDeskFile, openWebApp } from './viewers';
/* ----- website apps pinned to the desktop (v0.10) ----- */
export let WEBAPPS=[];
export function webLetter(n){return (String(n||'?').trim()[0]||'?').toUpperCase()}
export function faviconURL(u){try{return 'https://www.google.com/s2/favicons?domain='+new URL(u).hostname+'&sz=128'}catch(e){return ''}}
export function webIconHTML(a: any, mini?: any){
 const f=faviconURL(a.url);
 const img=f?`<img src="${f}" alt="" draggable="false" loading="lazy" onerror="this.remove()">`:'';
 return `<div class="appico webapp${mini?' mini':''}"><span class="icolet">${esc(webLetter(a.name))}</span>${img}</div>`;
}
/* ----- desktop layout: free icon positions + folders (v0.12) ----- */
export let LAYOUT: any={pos:{},folders:[]};
export let suppressClickUntil=0;
export async function loadLayout(){
 try{const r=await api('/api/layout'); if(r&&r.pos) LAYOUT={pos:r.pos||{},folders:r.folders||[],opens:r.opens||{},added:r.added||{},hiddenFiles:r.hiddenFiles||[]}}catch(e){}
 LAYOUT.opens=LAYOUT.opens||{}; LAYOUT.added=LAYOUT.added||{}; LAYOUT.hiddenFiles=LAYOUT.hiddenFiles||[];
}
export let _saveLayoutT=null;
export function saveLayout(){
 clearTimeout(_saveLayoutT);
 _saveLayoutT=setTimeout(()=>{api('/api/layout/save',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(LAYOUT)}).catch(()=>{})},120);
}
export function deskPos(key,slot){
 /* The pre-v0.35 position of an icon (stored free position, else
    its old default slot). Since v0.35 this is ONLY a migration
    input: reconcileDeskOrder reads it once to derive the desktop
    order from an old layout; painting itself uses grid cells. */
 const p=LAYOUT.pos[key];
 const stored=(p&&typeof p.x==='number'&&typeof p.y==='number')?p:null;
 return effPosFor(stored,slot,innerWidth,innerHeight,deskPitch());
}
export function folderById(id){return LAYOUT.folders.find(f=>f.id===id)}
/* ----- v0.56: kind-agnostic desktop entries -----
   A desktop entry is a canonical key: app:<id> / web:<url> /
   store:<id> / box:<id> / prod:<id> / file:<name> (folders are
   folder:<id>). entryMeta resolves a key to {name, icon(),
   open()} from whichever catalog owns it, so the desktop, the
   Launchpad, Spotlight and folder contents all render and
   launch a member exactly the same way. Folder membership used
   to store bare web URLs (pre-v0.56); folderItems migrates them
   to web: keys in place, once per folder object. */
export const _normFolders=new WeakSet();
export function folderItems(f){
 if(!f) return [];
 if(!_normFolders.has(f)){
  f.items=(Array.isArray(f.items)?f.items:[]).map(u=>{
   if(typeof u!=='string') return '';
   if(/^(app|web|store|box|prod|file):/.test(u)) return u;
   if(/^https?:\/\//.test(u)) return 'web:'+u;
   return u;
  }).filter(Boolean);
  _normFolders.add(f);
 }
 return f.items;
}
export function folderedKeys(){ const s=new Set(); LAYOUT.folders.forEach(f=>folderItems(f).forEach(k=>s.add(k))); return s; }
export function entryMeta(key){
 try{
  if(key.startsWith('app:')){ const a=APPS.find(x=>'app:'+x.id===key); if(!a) return null; return {key,name:a.n,icon:()=>appIcon(a),open:()=>{ if(a.id==='firefox') launchFirefoxEntry(); else openApp(a.id); }}; }
  if(key.startsWith('web:')){ const a=WEBAPPS.find(x=>'web:'+x.url===key); if(!a) return null; return {key,name:a.name,icon:()=>webIconHTML(a),open:()=>openWebApp(a.url)}; }
  if(key.startsWith('store:')){ const a=STOREAPPS.find(x=>'store:'+x.id===key); if(!a) return null; return {key,name:a.name,icon:()=>storeIconHTML(a),open:()=>launchStoreApp(a.id)}; }
  if(key.startsWith('box:')){ const a=BOXAPPS.find(x=>'box:'+x.id===key); if(!a) return null; return {key,name:a.name,icon:()=>storeIconHTML(a),open:()=>launchBoxApp(a.id)}; }
  if(key.startsWith('prod:')){ const c=PRODAPPS.find(x=>'prod:'+x.id===key); if(!c) return null; return {key,name:c.name,icon:()=>prodIconHTML(c),open:()=>launchProdApp(c.id)}; }
  if(key.startsWith('file:')){ const f=DESKFILES.find(x=>'file:'+x.name===key); if(!f) return null; return {key,name:f.name,icon:()=>fileIconHTML(f),open:()=>openDeskFile(f.path,f.kind)}; }
 }catch(e){ console.error('[mj-desk] 条目解析失败，已跳过：'+key,e); }
 return null;
}
/* Open counters + first-seen stamps, both inside the layout
   blob (they travel with the folder): opens feeds the 最常用
   sort, added feeds 添加日期. Stamps are assigned lazily the
   first time an entry is enumerated after v0.56. */
export function recOpen(key){
 if(!key||key.startsWith('folder:')) return;
 LAYOUT.opens=LAYOUT.opens||{};
 LAYOUT.opens[key]=(LAYOUT.opens[key]||0)+1;
 saveLayout();
}
export function stampAdded(keys){
 LAYOUT.added=LAYOUT.added||{};
 let dirty=false; const now=Date.now();
 keys.forEach(k=>{ if(!LAYOUT.added[k]){ LAYOUT.added[k]=now; dirty=true; } });
 if(dirty) saveLayout();
}
/* Desktop sorting (item 7): DESKORDER stays the single truth;
   with a non-manual mode active the top-level order IS the
   sorted order (persisted by the normal render path), so every
   surface that reads DESKORDER agrees. A manual drag switches
   the mode back to 手动 (finishDrag). */
export function deskSortMode(){ const m=SETTINGS&&SETTINGS.deskSort; return ['name','freq','kind','added'].includes(m)?m:'manual' }
export function setDeskSort(m){ if(SETTINGS) SETTINGS.deskSort=m; saveSetting({deskSort:m}); if(typeof renderDesk==='function'&&deskRenderedOnce) renderDesk(); }
export function setDeskIconSize(px){ px=Math.max(36,Math.min(96,Math.round(+px||54))); if(SETTINGS) SETTINGS.deskIconSize=px; saveSetting({deskIconSize:px}); applyDeskIconSize(); }
export const KIND_RANK={app:0,web:1,store:2,box:3,prod:4,file:5,folder:6};
export function sortedDeskOrder(keys){
 const mode=deskSortMode(); if(mode==='manual') return keys;
 const nameOf=k=>{ if(k.startsWith('folder:')){ const f=folderById(k.slice(7)); return f?f.name:''; } const m=entryMeta(k); return m?m.name:''; };
 const arr=keys.slice();
 if(mode==='name') arr.sort((a,b)=>nameOf(a).localeCompare(nameOf(b),'zh'));
 else if(mode==='freq'){ const o=LAYOUT.opens||{}; arr.sort((a,b)=>((o[b]||0)-(o[a]||0))||nameOf(a).localeCompare(nameOf(b),'zh')); }
 else if(mode==='kind') arr.sort((a,b)=>((KIND_RANK[a.split(':')[0]]??9)-(KIND_RANK[b.split(':')[0]]??9))||nameOf(a).localeCompare(nameOf(b),'zh'));
 else if(mode==='added'){ const ad=LAYOUT.added||{}; arr.sort((a,b)=>((ad[a]||0)-(ad[b]||0))||nameOf(a).localeCompare(nameOf(b),'zh')); }
 return arr;
}
/* Stage Manager identity (item 1): a browser window's cleaned
   page title -- the browser's own suffix is noise there. */
export function cleanWinTitle(t){
 let s=String(t||'').trim();
 s=s.replace(/\s*[—–-]\s*Mozilla Firefox$/,'').replace(/\s*-\s*Google Chrome$/,'').replace(/\s*-\s*Microsoft[​]? Edge$/,'').replace(/\s*[—–-]\s*Chromium$/,'');
 return s;
}
export function folderIconHTML(f){
 const items=folderItems(f).map(k=>entryMeta(k)).filter(Boolean).slice(0,4);
 if(!items.length) return `<div class="appico folderico"><span class="fempty" style="color:#fff">${glyph('folder',26)}</span></div>`;
 return `<div class="appico folderico">${items.map(m=>`<span class="fmini">${m.icon()}</span>`).join('')}</div>`;
}
export function mkDeskIcon(key: any,kind: any,iconHTML: any,name: any,onOpen: any,fid?: any){
 const d=document.createElement('button');
 d.className='icon'; d.dataset.key=key; d.dataset.kind=kind; if(fid) d.dataset.fid=fid;
 d.innerHTML=iconHTML+`<div class="n">${esc(name)}</div>`;
 d.title=name;
 d.onclick=()=>{ document.querySelectorAll('.icon.sel').forEach(x=>{if(x!==d)x.classList.remove('sel')}); d.classList.add('sel'); };
 d.ondblclick=()=>{ if(Date.now()<suppressClickUntil) return; recOpen(key); onOpen(); };
 /* v0.56 item 5: right-clicking an icon gets ITS menu, not the
    desktop's. Imported files override this below with their own
    (showFileCtx, which grew the same item actions). */
 d.oncontextmenu=ev=>{ ev.preventDefault(); ev.stopPropagation(); showItemCtx(ev.clientX,ev.clientY,key,kind,fid); };
 attachDrag(d,key,kind);
 return d;
}
export const __bind_WEBAPPS: any = { get(): any { return WEBAPPS; }, set(v: any): any { WEBAPPS = v; return v; } };

export const __bind_LAYOUT: any = { get(): any { return LAYOUT; }, set(v: any): any { LAYOUT = v; return v; } };

export const __bind_suppressClickUntil: any = { get(): any { return suppressClickUntil; }, set(v: any): any { suppressClickUntil = v; return v; } };

export const __bind__saveLayoutT: any = { get(): any { return _saveLayoutT; }, set(v: any): any { _saveLayoutT = v; return v; } };
