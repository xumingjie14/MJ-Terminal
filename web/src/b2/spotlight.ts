/* Generated cross-module imports (v0.58 modularization):
   every name below is owned by the sibling module it is imported
   from -- the same names the pre-modular page shared through one
   script scope. */
import { SPOT_SETPAGES } from './b2core';
import { $50, fvKindLabel, vaultLockedNow } from './fvhead';
import { spotRecentGet, spotRecentPut } from './recents';
import { closeMbMenu, mbOpenFor } from './mbmenus';
import { closeLP, lpEntries, lpIsOpen, lpPageGo } from './launchpad';
/* ================= Spotlight ================= */
export const spotEl=$50('spotlight'), spotIn=$50('spot-in'), spotRes=$50('spot-res');
export let spotIsOpen=false, spotFlat=[], spotSel=0, spotNodesCache=null, spotNodesAt=0;
export function openSpot(){
 if(vaultLockedNow()) return;
 closeMbMenu(); closeLP();
 spotIsOpen=true;
 spotEl.classList.remove('hidden');
 spotEl.classList.remove('open'); void spotEl.offsetWidth; spotEl.classList.add('open');
 spotIn.value=''; spotSel=0;
 paintSpot();
 loadSpotNodes();
 setTimeout(()=>{ try{ spotIn.focus(); }catch(e){} },20);
}
export function closeSpot(){ if(!spotIsOpen) return; spotIsOpen=false; spotEl.classList.add('hidden'); }
export async function loadSpotNodes(){
 if(spotNodesCache&&Date.now()-spotNodesAt<15000) return;
 try{
  const st=await api('/api/nodepro/status');
  const rows=[];
  (st.groups||[]).forEach(g=>(g.nodes||[]).forEach(n=>{
   if(g.kind==='virtual') rows.push({target:n.id,name:n.name});
   else if(n.isWarp) rows.push({target:'warp',name:'WARP 中继'});
   else rows.push({target:n.id,name:n.name});
  }));
  spotNodesCache=rows; spotNodesAt=Date.now();
 }catch(e){ spotNodesCache=[]; spotNodesAt=Date.now(); }
 if(spotIsOpen) paintSpot();
}
export function spotCatalog(){
 const rec=spotRecentGet();
 const apps=lpEntries().filter(e=>e.key.indexOf('folder:')!==0)
  .map(e=>({key:e.key,name:e.name,icon:e.iconHTML,run:()=>e.open()}));
 const files=DESKFILES.map(f=>({key:'file:'+f.path,name:f.name,icon:fileIconHTML(f),sub:fvKindLabel(f),run:()=>openDeskFile(f.path,f.kind)}));
 const sets=SPOT_SETPAGES.map(([id,n])=>({key:'set:'+id,name:n,icon:`<div class="appico g-sys" style="color:#fff">${glyph('gear',26)}</div>`,sub:'设置',run:()=>{ openApp('sys'); const go=()=>{ if(window.sysGoto) window.sysGoto(id); }; go(); setTimeout(go,150); }}));
 const nodes=(spotNodesCache||[]).map(n=>({key:'node:'+n.target,name:n.name,icon:`<div class="appico g-core" style="color:#fff">${glyph('rocket',26)}</div>`,sub:'节点',run:async()=>{ const r=await api('/api/nodepro/select',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({target:n.target})}).catch(()=>null); if(r&&r.error) alert('切换失败：'+r.error); else { toast('已切换到 '+n.name); pollNode(); } }}));
 const acts=[];
 if(window.__vault&&window.__vault.enabled) acts.push({key:'act:lock',name:'锁定屏幕',icon:`<div class="sic">${glyph('lock',16)}</div>`,run:async()=>{ if(!confirm('锁定屏幕会关闭终端机内打开的应用，并把全部数据加密上锁。继续？')) return; const r=await api('/api/vault/lock',{method:'POST'}).catch(()=>null); if(r&&r.error){ alert(r.error); return; } if(window.__vaultShowLock) window.__vaultShowLock(); }});
 acts.push({key:'act:console',name:'打开控制台',icon:`<div class="sic">${glyph('list',16)}</div>`,run:()=>openApp('console')});
 acts.push({key:'act:update',name:'检查更新',icon:`<div class="sic">${glyph('up',16)}</div>`,run:()=>{ openApp('sys'); const go=()=>{ if(window.sysGoto) window.sysGoto('update'); }; go(); setTimeout(go,150); }});
 acts.push({key:'act:off',name:'关机…',icon:`<div class="sic">${glyph('power',16)}</div>`,run:async()=>{ if(!confirm('关机将退出终端机。继续？')) return; await api('/api/shutdown',{method:'POST'}).catch(()=>{}); document.body.innerHTML='<div style="position:fixed;inset:0;display:flex;align-items:center;justify-content:center;font-size:20px;color:#fff;text-shadow:0 2px 10px #000">已关机</div>'; }});
 return {rec,apps,files,sets,nodes,acts};
}
export function paintSpot(){
 const q=spotIn.value;
 const c=spotCatalog();
 let groups=[];
 if(!(q||'').trim()){
  const byKey={};
  [...c.apps,...c.files,...c.sets,...c.nodes,...c.acts].forEach(it=>{ byKey[it.key]=it; });
  const recItems=c.rec.map(k=>byKey[k]).filter(Boolean).slice(0,8);
  if(recItems.length) groups.push({g:'最近打开',items:recItems});
  groups.push({g:'操作',items:c.acts});
 } else {
  const cap=xs=>spotRankList(xs,q,c.rec).slice(0,6);
  const g1=cap(c.apps); if(g1.length) groups.push({g:'应用',items:g1});
  const g2=cap(c.files); if(g2.length) groups.push({g:'文件',items:g2});
  const g3=cap(c.sets); if(g3.length) groups.push({g:'设置',items:g3});
  const g4=cap(c.nodes); if(g4.length) groups.push({g:'节点',items:g4});
  const g5=cap(c.acts); if(g5.length) groups.push({g:'操作',items:g5});
 }
 spotFlat=[];
 groups.forEach(gr=>gr.items.forEach(it=>spotFlat.push(it)));
 if(spotSel>=spotFlat.length) spotSel=Math.max(0,spotFlat.length-1);
 if(!spotFlat.length){ spotRes.innerHTML='<div class="spot-none">没有找到「'+esc(q)+'」</div>'; return; }
 let idx=-1;
 spotRes.innerHTML=groups.map(gr=>'<div class="spot-gh">'+esc(gr.g)+'</div>'+gr.items.map(it=>{
  idx++;
  return '<div class="spot-it'+(idx===spotSel?' sel':'')+'" data-i="'+idx+'">'+it.icon+'<span class="snm">'+esc(it.name)+'</span>'+(it.sub?'<span class="sub">'+esc(it.sub)+'</span>':'')+'</div>';
 }).join('')).join('');
 spotRes.querySelectorAll('.spot-it').forEach(el=>{
  const it=spotFlat[+el.dataset.i];
  el.onclick=()=>runSpotItem(it);
  el.onmousemove=()=>{ const i=+el.dataset.i; if(spotSel!==i){ spotSel=i; spotPaintSel(); } };
 });
}
export function spotPaintSel(){
 spotRes.querySelectorAll('.spot-it').forEach(el=>el.classList.toggle('sel',+el.dataset.i===spotSel));
 const el=spotRes.querySelector('.spot-it[data-i="'+spotSel+'"]');
 if(el) el.scrollIntoView({block:'nearest'});
}
export function spotMove(d){
 if(!spotFlat.length) return;
 spotSel=(spotSel+d+spotFlat.length)%spotFlat.length;
 spotPaintSel();
}
export function runSpotItem(it){
 if(!it) return;
 closeSpot();
 spotRecentPut(it.key);
 it.run();
}
spotIn.addEventListener('input',()=>{ spotSel=0; paintSpot(); });
spotEl.addEventListener('click',e=>{ if(e.target===spotEl) closeSpot(); });
$50('mb-spot').addEventListener('click',e=>{ e.stopPropagation(); spotIsOpen?closeSpot():openSpot(); });

/* ---- invocation: F5 / Ctrl+Space inside this view, the menu-bar
   magnifier (above), and the daemon's view channel for F8. The
   capture phase + preventDefault is what keeps F5 from reloading
   the shell; F5 inside claimed EXTERNAL windows never reaches
   this view and is not ours to touch. ---- */
document.addEventListener('keydown',e=>{
 if(e.key==='F5'){
  e.preventDefault();
  if(!vaultLockedNow()){ spotIsOpen?closeSpot():openSpot(); }
  return;
 }
 if(e.code==='Space'&&e.ctrlKey&&!e.metaKey&&!e.altKey&&!e.shiftKey){
  e.preventDefault();
  if(!vaultLockedNow()){ spotIsOpen?closeSpot():openSpot(); }
  return;
 }
 if(spotIsOpen){
  if(e.key==='Escape'){ e.preventDefault(); e.stopPropagation(); closeSpot(); }
  else if(e.key==='ArrowDown'){ e.preventDefault(); spotMove(1); }
  else if(e.key==='ArrowUp'){ e.preventDefault(); spotMove(-1); }
  else if(e.key==='Enter'){ e.preventDefault(); runSpotItem(spotFlat[spotSel]); }
  return;
 }
 if(lpIsOpen){
  if(e.key==='Escape'){ e.preventDefault(); e.stopPropagation(); closeLP(); }
  else if(e.key==='ArrowRight'){ e.preventDefault(); lpPageGo(1); }
  else if(e.key==='ArrowLeft'){ e.preventDefault(); lpPageGo(-1); }
  return;
 }
 if(mbOpenFor&&e.key==='Escape'){ closeMbMenu(); }
},true);
document.addEventListener('keyup',e=>{ if(e.key==='F5') e.preventDefault(); },true);
/* v0.59 block 3: this was the stream's only consumer; it now
   rides the shared event client (MJEvents.onCmd) instead of
   holding its own EventSource. The direct-EventSource path
   remains as a fallback if the client is somehow absent. */
try{
 const onViewCmd=(m)=>{
  try{ if(m&&m.cmd==='spotlight'&&!vaultLockedNow()) openSpot(); }catch(e){}
 };
 if(typeof MJEvents!=='undefined'&&MJEvents&&MJEvents.onCmd){ MJEvents.onCmd(onViewCmd); }
 else{
  const es=new EventSource('/api/view/stream');
  es.onmessage=ev=>{ try{ onViewCmd(JSON.parse(ev.data)); }catch(e){} };
 }
}catch(e){}

export const __bind_spotIsOpen: any = { get(): any { return spotIsOpen; }, set(v: any): any { spotIsOpen = v; return v; } };

export const __bind_spotFlat: any = { get(): any { return spotFlat; }, set(v: any): any { spotFlat = v; return v; } };

export const __bind_spotSel: any = { get(): any { return spotSel; }, set(v: any): any { spotSel = v; return v; } };

export const __bind_spotNodesCache: any = { get(): any { return spotNodesCache; }, set(v: any): any { spotNodesCache = v; return v; } };

export const __bind_spotNodesAt: any = { get(): any { return spotNodesAt; }, set(v: any): any { spotNodesAt = v; return v; } };
