/* Generated cross-module imports (v0.58 modularization):
   every name below is owned by the sibling module it is imported
   from -- the same names the pre-modular page shared through one
   script scope. */
import { WALLPAPERS, api, customWall, esc, __bind_customWall } from './core';
import { glyph } from './glyphs';
import { SETDEF, SETTINGS, applyDeskIconSize, deskIconSize, dockAutoHideOn, saveSetting, wallKey } from './settings';
import { applyWallpaper, pollNode } from './menubar';
import { openApp } from './windows';
import { stageOn, toggleStage } from './stage';
import { applyDockLayout, dockBase, dockMagReset } from './dock';
/* ----- control center + notification center (v0.47) -----
   Built from the macOS User Guide (Use Control Center on Mac /
   Use Notification Center on Mac), mapped onto the functions
   this product actually has:
   - Control Center drops from the two-toggle menu bar icon;
     item click = toggle, chevron = expand in place (node list,
     remote tiers, wallpaper presets); one slider row (Dock icon
     size -- a real parameter); footer jumps to 设置 → 桌面与 Dock
     → 控制中心 where tiles can be hidden (the node tile cannot,
     like the manual's core controls).
   - Notification Center opens from the menu bar CLOCK (click
     desktop / Esc closes): daemon-assembled status items on
     top, recorded events stacked BY SOURCE below (click stack
     expands, Show less collapses, hover = × per item and
     全部清除 per stack, body click = jump + clear), widgets
     under them with an edit mode (show/hide + reorder).
   - A fresh event raises a banner at the top right while NC is
     closed (~6s, hover pauses, click = jump + clear).
   - The orange dot beside the CC icon is the manual's privacy
     indicator, mapped to the one thing here that watches the
     user back: a live remote-assist session.
   Every action goes through the existing feature APIs and
   settings keys -- these panels are shells, not second state. */
export const ANIM_PANEL_MS=280;   // CC dropdown fade/scale (CSS #ccpanel)
export const ANIM_NC_MS=300;      // NC column slide (CSS #ncenter)
export const ANIM_BANNER_MS=320;  // banner in/out (CSS .banner)
export const BANNER_HOLD_MS=6000; // banner dwell before auto-dismiss
export const NC_SRCNAMES={sandbox:'应用隔离',update:'软件更新',servermon:'服务器监控',node:'节点',remote:'远程协助',files:'文件'};


export const ccEl=document.getElementById('ccpanel'), ncEl=document.getElementById('ncenter'), bannersEl=document.getElementById('banners');
export let ccOpen=false, ncOpen=false, ccNodesOpen=false, ccRemoteOpen=false, ccWallOpen=false, ncEditMode=false, ccSliderDrag=false;
export const ncExpandedSrc=new Set();
export function routeJump(route){
 if(!route) return;
 closeCC(); closeNC();
 if(route.startsWith('setpage:')){
  openApp('sys');
  const pg=route.slice(8), go=()=>{ if(window.sysGoto) window.sysGoto(pg); };
  go(); setTimeout(go,150);
 } else openApp(route);
}
export function closeCC(){ if(!ccOpen) return; ccOpen=false; ccEl.classList.add('hidden'); const b=document.getElementById('mb-cc'); if(b) b.classList.remove('on'); }
export function openCC(){ closeNC(); ccOpen=true; ccEl.classList.remove('hidden'); ccEl.classList.add('pop'); requestAnimationFrame(()=>requestAnimationFrame(()=>ccEl.classList.remove('pop'))); const b=document.getElementById('mb-cc'); if(b) b.classList.add('on'); paintCC(); }
export function closeNC(){ if(!ncOpen) return; ncOpen=false; ncEl.classList.add('nc-off'); ncEditMode=false; }
export function openNC(){ closeCC(); ncOpen=true; ncEl.classList.remove('nc-off'); paintNC(); }
document.getElementById('mb-cc').onclick=e=>{ e.stopPropagation(); ccOpen?closeCC():openCC(); };
document.getElementById('mb-clock').onclick=e=>{ e.stopPropagation(); ncOpen?closeNC():openNC(); };
document.addEventListener('click',e=>{
 if(ccOpen&&!ccEl.contains(e.target as Node)) closeCC();
 if(ncOpen&&!ncEl.contains(e.target as Node)) closeNC();
});
document.addEventListener('keydown',e=>{
 if(e.key!=='Escape') return;
 if(ccOpen) closeCC(); else if(ncOpen) closeNC();
});

/* ----- control center ----- */
export function ccTile(id: any,on: any,ico: any,name: any,sub: any,chev?: any){
 /* test2: ico is a GLYPHS name -- one drawn set in the CC too
    (test1 mixed blue filled circles, gray circles and emoji). */
 return `<div class="cc-tile ${on?'on':''}" id="cc-${id}"><span class="cc-ico">${glyph(ico,16)}</span><span class="cc-tx"><div class="cc-name">${name}</div>${sub?`<div class="cc-sub">${sub}</div>`:''}</span>${chev?'<span class="cc-chev">›</span>':''}</div>`;
}
export async function paintCC(){
 if(!ccEl.innerHTML) ccEl.innerHTML='<div class="nc-empty">读取中…</div>';
 const s=SETTINGS||SETDEF;
 const [core,node,remote,pairing,npst]=await Promise.all([
  api('/api/core',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'status'})}).catch(()=>({})),
  api('/api/node').catch(()=>({nodes:[]})),
  api('/api/remote/state').catch(()=>({})),
  api('/api/remote/pairing').catch(()=>({operators:[]})),
  api('/api/nodepro/status').catch(()=>null)
 ]);
 const running=!!core.running, nodes=node.nodes||[];
 /* v0.49: the node tile reads the nodepro status -- the exit
    shown is the REAL current exit, and the expanded list is a
    real switcher (click = switch), repaying the v0.47 debt
    where rows could only jump to the node page. */
 const npExit=(npst&&npst.exit)||null;
 const curNode=npExit?{name:npExit.currentName?((npExit.name)+' → '+npExit.currentName):npExit.name}:(nodes.length?nodes[0]:null);
 const rsum=remoteSummary(remote,pairing);
 const wallName=customWall?'自定义壁纸':((WALLPAPERS[wallKey()]||{}).n||wallKey());
 let h='<div class="cc-grid">';
 /* node big tile: always present (core control, not hideable) */
 h+=`<div class="cc-tile wide ${running?'on':''}" id="cc-node"><span class="cc-ico">${glyph('rocket',16)}</span><span class="cc-tx"><div class="cc-name">节点 ${running?'已连接':'未连接'}</div><div class="cc-sub">${running?(curNode?esc(curNode.name):'—'):(npExit?('当前选择：'+esc(npExit.name)):(nodes.length?('默认节点：'+esc(nodes[0].name)):'还没有节点'))}</div></span><span class="cc-chev" id="cc-node-chev">${ccNodesOpen?'⌄':'›'}</span></div>`;
 if(s.cc_show_immersive!==false) h+=ccTile('immersive',!!s.immersiveMode,'half','沉浸模式',s.immersiveMode?'开':'关');
 if(s.cc_show_stage!==false) h+=ccTile('stage',stageOn(),'stage','台前调度',stageOn()?'开':'关');
 if(s.cc_show_fullscreen!==false) h+=ccTile('fullscreen',!!s.desktopFullscreen,'size','桌面全屏',s.desktopFullscreen?'开':'关');
 if(s.cc_show_dockhide!==false) h+=ccTile('dockhide',dockAutoHideOn(),'dock','Dock 自动隐藏',dockAutoHideOn()?'开':'关');
 if(s.cc_show_remote!==false) h+=ccTile('remote',!!rsum.on,'link','远程协助',rsum.label,true);
 if(s.cc_show_wallpaper!==false) h+=ccTile('wall',false,'image','壁纸',wallName,true);
 h+='</div>';
 if(ccNodesOpen){
  let ccRows=[];
  if(npst&&npst.groups){
   (npst.groups||[]).forEach(g=>{ (g.nodes||[]).forEach(n=>{
     if(g.kind==='virtual') ccRows.push({target:n.id,label:n.name,sub:n.id==='auto'?'urltest 自动测速选最快':'按顺序探活顺次切换',cur:npst.selected===n.id});
     else if(n.isWarp) ccRows.push({target:'warp',label:'WARP 中继',sub:'wireguard',cur:npst.selected==='warp'});
     else ccRows.push({target:n.id,label:n.name,sub:n.type+' '+n.server+':'+n.port,cur:npst.selected===n.id});
   }); });
  }
  if(!ccRows.length&&nodes.length) ccRows=nodes.map((n,i)=>({target:'',label:n.name,sub:n.type+' '+n.server+':'+n.port,cur:i===0}));
  h+='<div class="cc-list">'+(ccRows.length?ccRows.map(r0=>`<div class="cc-li" data-ccnode="${esc(r0.target)}"><span class="cc-dot ${running&&r0.cur?'on':''}"></span><span>${esc(r0.label)}<span style="opacity:.6"> · ${esc(r0.sub)}</span></span>${r0.cur?'<span class="cc-cur">当前出口</span>':''}</div>`).join(''):'<div class="cc-li">还没有节点</div>')+'<div style="font-size:12px;opacity:.6;padding:6px 8px 2px">点节点即切换出口（核心在跑时热切换，不重启）；节点的增删在「节点」页。</div></div>';
 }
 if(ccRemoteOpen&&s.cc_show_remote!==false){
  const rows=[['off','关闭'],['read','只读诊断'],['assist','协助']];
  const curMode=!rsum.on?'off':(rsum.mode||'mixed');
  h+='<div class="cc-list">'+rows.map(([m,n])=>`<div class="cc-li" data-ccremote="${m}"><span>${n}</span>${curMode===m?'<span class="cc-cur">✓ 当前</span>':''}</div>`).join('')+(rsum.mode==='mixed'?'<div style="font-size:12px;opacity:.6;padding:6px 8px 2px">现在各位操作者的档位不一样，点上面任一档会统一到该档；按人分别设在 设置 → 安全与隐私。</div>':'')+'</div>';
 }
 if(ccWallOpen&&s.cc_show_wallpaper!==false){
  h+='<div class="cc-list">'+Object.entries(WALLPAPERS).map(([k,v])=>`<div class="cc-li" data-ccwall="${k}"><span>${v.n}</span>${!customWall&&wallKey()===k?'<span class="cc-cur">✓ 当前</span>':''}</div>`).join('')+(customWall?'<div style="font-size:12px;opacity:.6;padding:6px 8px 2px">现在是自定义壁纸，点任一预设即换回预设。</div>':'')+'</div>';
 }
 h+=`<div class="cc-slider"><span>Dock 图标大小</span><input type="range" id="cc-docksize" min="44" max="64" step="1" value="${dockBase()}"><span id="cc-docksize-v" style="opacity:.7;font-variant-numeric:tabular-nums">${dockBase()}</span></div>`;
 h+=`<div class="cc-slider"><span>桌面图标大小</span><input type="range" id="cc-desksize" min="36" max="96" step="2" value="${deskIconSize()}"><span id="cc-desksize-v" style="opacity:.7;font-variant-numeric:tabular-nums">${deskIconSize()}</span></div>`;
 h+='<div class="cc-foot"><button id="cc-edit">编辑控制中心</button></div>';
 ccEl.innerHTML=h;
 const nodeTile=ccEl.querySelector('#cc-node');
 if(nodeTile) nodeTile.onclick=async e=>{
  if(e.target.closest('#cc-node-chev')||e.target.closest('.cc-li')) return;
  nodeTile.style.opacity='.55';
  await api('/api/core',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:running?'stop':'start'})}).catch(()=>{});
  pollNode(); paintCC();
 };
 const chev=ccEl.querySelector('#cc-node-chev');
 if(chev) chev.onclick=e=>{ e.stopPropagation(); ccNodesOpen=!ccNodesOpen; paintCC(); };
 ccEl.querySelectorAll('[data-ccnode]').forEach(el=>el.onclick=async()=>{
  const target=el.dataset.ccnode;
  if(!target){ closeCC(); openApp('core'); return; }
  el.style.opacity='.55';
  const r=await api('/api/nodepro/select',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({target})}).catch(()=>null);
  if(r&&r.error){ el.style.opacity=''; alert('切换失败：'+r.error); return; }
  pollNode(); paintCC();
 });
 const tgl=(id,fn)=>{ const el=ccEl.querySelector('#cc-'+id); if(el) el.onclick=fn; };
 tgl('immersive',async()=>{ const r=await saveSetting({immersiveMode:!((SETTINGS||SETDEF).immersiveMode)}); if(r&&r.error) alert('保存失败：'+r.error); paintCC(); });
 tgl('stage',()=>{ toggleStage(!stageOn()); paintCC(); });
 tgl('fullscreen',async()=>{ const r=await saveSetting({desktopFullscreen:!((SETTINGS||SETDEF).desktopFullscreen)}); if(r&&r.error) alert('保存失败：'+r.error); paintCC(); });
 tgl('dockhide',async()=>{ const r=await saveSetting({dockAutoHide:!dockAutoHideOn()}); if(r&&r.error){ alert('保存失败：'+r.error); return; } applyDockLayout(); paintCC(); });
 tgl('remote',async e=>{
  if(e.target.closest('.cc-chev')||e.target.closest('.cc-li')) return;
  if(ccRemoteOpen){ ccRemoteOpen=false; paintCC(); return; }
  const r=await saveSetting({remoteEnabled:!rsum.on}); if(r&&r.error){ alert('保存失败：'+r.error); return; } paintCC();
 });
 const remoteTile=ccEl.querySelector('#cc-remote');
 if(remoteTile){ const cv=remoteTile.querySelector('.cc-chev'); if(cv) cv.onclick=e=>{ e.stopPropagation(); ccRemoteOpen=!ccRemoteOpen; paintCC(); }; }
 ccEl.querySelectorAll('[data-ccremote]').forEach(el=>el.onclick=()=>ccSetRemote(el.dataset.ccremote,remote,pairing));
 tgl('wall',async e=>{
  if(e.target.closest('.cc-chev')||e.target.closest('.cc-li')) return;
  await ccNextWallpaper(); paintCC();
 });
 const wallTile=ccEl.querySelector('#cc-wall');
 if(wallTile){ const cv=wallTile.querySelector('.cc-chev'); if(cv) cv.onclick=e=>{ e.stopPropagation(); ccWallOpen=!ccWallOpen; paintCC(); }; }
 ccEl.querySelectorAll('[data-ccwall]').forEach(el=>el.onclick=async()=>{ await ccSetWallpaper(el.dataset.ccwall); paintCC(); });
 const slider=ccEl.querySelector('#cc-docksize');
 if(slider){
  slider.addEventListener('pointerdown',()=>{ ccSliderDrag=true; });
  slider.addEventListener('pointerup',()=>{ ccSliderDrag=false; });
  slider.oninput=()=>{ if(SETTINGS) SETTINGS.dockIconSize=+slider.value; const v=ccEl.querySelector('#cc-docksize-v'); if(v) v.textContent=slider.value; dockMagReset(); };
  slider.onchange=async()=>{ ccSliderDrag=false; const r=await saveSetting({dockIconSize:+slider.value}); if(r&&r.error) alert('保存失败：'+r.error); };
 }
 const dslider=ccEl.querySelector('#cc-desksize');
 if(dslider){
  dslider.addEventListener('pointerdown',()=>{ ccSliderDrag=true; });
  dslider.addEventListener('pointerup',()=>{ ccSliderDrag=false; });
  dslider.oninput=()=>{ if(SETTINGS) SETTINGS.deskIconSize=+dslider.value; const v=ccEl.querySelector('#cc-desksize-v'); if(v) v.textContent=dslider.value; applyDeskIconSize(); };
  dslider.onchange=async()=>{ ccSliderDrag=false; const r=await saveSetting({deskIconSize:+dslider.value}); if(r&&r.error) alert('保存失败：'+r.error); };
 }
 const edit=ccEl.querySelector('#cc-edit');
 if(edit) edit.onclick=()=>routeJump('setpage:desktop');
}
export function remoteSummary(state,pairing){
 if(!state||!state.enabled) return {on:false,mode:'off',label:'关'};
 const ops=(pairing&&pairing.operators)||[];
 if(!ops.length) return {on:true,mode:'',label:'已开启 · 无操作者'};
 const tiers=new Set(ops.map(o=>o.tier));
 if(tiers.size>1) return {on:true,mode:'mixed',label:'分别设置'};
 const t=ops[0].tier;
 return {on:true,mode:t,label:t==='assist'?'协助':'只读诊断'};
}
export async function ccSetRemote(mode: any, ..._rest: any[]){
 if(mode==='off'){ await saveSetting({remoteEnabled:false}); paintCC(); return; }
 if(!(SETTINGS&&SETTINGS.remoteEnabled)) await saveSetting({remoteEnabled:true});
 const pr=await api('/api/remote/pairing').catch(()=>({operators:[]}));
 for(const o of pr.operators||[]){
  if(o.tier!==mode) await api('/api/remote/pairing',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'tier',id:o.id,tier:mode})}).catch(()=>{});
 }
 ccRemoteOpen=false; paintCC();
}
export async function ccSetWallpaper(key){
 if(customWall){ await api('/api/wallpaper',{method:'DELETE'}).catch(()=>{}); __bind_customWall.set(false); }
 await saveSetting({wallpaper:key}); applyWallpaper();
}
export async function ccNextWallpaper(){
 const keys=Object.keys(WALLPAPERS);
 const idx=customWall?-1:keys.indexOf(wallKey());
 await ccSetWallpaper(keys[(idx+1)%keys.length]);
}

/* ----- notification center ----- */
export function fmtNTime(ms){
 const d=new Date(ms), now=new Date();
 const hm=d.toLocaleTimeString('zh-CN',{hour:'2-digit',minute:'2-digit',hour12:false});
 return (d.toDateString()===now.toDateString())?hm:(d.getMonth()+1)+'/'+d.getDate()+' '+hm;
}
export async function paintNC(){
 const scroll=document.getElementById('nc-scroll');
 if(!scroll.innerHTML) scroll.innerHTML='<div class="nc-empty">读取中…</div>';
 const [data,nodeR,coreR]=await Promise.all([
  api('/api/notify').catch(()=>({status:[],events:[],widgets:{}})),
  api('/api/node').catch(()=>({nodes:[]})),
  api('/api/core',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'status'})}).catch(()=>({}))
 ]);
 const now=new Date();
 let h=`<div id="nc-date">${now.toLocaleDateString('zh-CN',{weekday:'long',month:'long',day:'numeric'})}</div>`;
 const status=data.status||[], events=data.events||[];
 /* v0.54: the panel splits into 系统通知 (status items + the
    six built-in sources' stacks) and 应用通知 (events about
    user-installed apps, stacked per app). */
 const evCard=(e0,srcName)=>`<div class="ncard" data-nev="${e0.id}" data-nroute="${esc(e0.route||'')}"><span class="nx" data-nx="${e0.id}">✕</span><div class="n-src">${esc(srcName)}</div><div class="n-title">${esc(e0.title)}</div>${e0.body?`<div class="n-body">${esc(e0.body)}</div>`:''}<span class="n-time">${fmtNTime(e0.time)}</span></div>`;
 const groupHTML=g=>{
  const srcName=g.category==='app'?(g.name||'应用'):(NC_SRCNAMES[g.source]||g.source);
  if(g.items.length===1) return evCard(g.items[0],srcName);
  if(ncExpandedSrc.has(g.key)){
   return `<div class="nstack"><button class="n-clear" data-nclear="${esc(g.key)}">全部清除</button>${g.items.map(e0=>evCard(e0,srcName)).join('')}<div class="nc-showless" data-nless="${esc(g.key)}">Show less</div></div>`;
  }
  const e0=g.items[0];
  return `<div class="nstack"><button class="n-clear" data-nclear="${esc(g.key)}">全部清除</button><div class="npeek"></div><div class="ncard" data-nexpand="${esc(g.key)}"><div class="n-src">${esc(srcName)}<span class="n-count">${g.items.length}</span></div><div class="n-title">${esc(e0.title)}</div>${e0.body?`<div class="n-body">${esc(e0.body)}</div>`:''}<span class="n-time">${fmtNTime(e0.time)}</span></div></div>`;
 };
 const sysEvents=events.filter(e=>ncEventCategory(e)!=='app'), appEvents=events.filter(e=>ncEventCategory(e)==='app');
 if(ncEditMode){
  h+='<div class="nc-sec">编辑小部件</div>'+ncWidgetsEditHTML();
 } else {
  if(!status.length&&!events.length) h+='<div class="nc-empty">没有通知</div>';
  if(status.length||sysEvents.length){
   h+='<div class="nc-sec">系统通知</div>';
   for(const it of status){
    h+=`<div class="ncard" data-nroute="${esc(it.route||'')}" style="cursor:pointer"><div class="n-src">${esc(NC_SRCNAMES[it.source]||it.source)}</div><div class="n-title">${esc(it.title)}</div>${it.body?`<div class="n-body">${esc(it.body)}</div>`:''}<div class="n-act"><button data-nact="${esc(it.route||'')}">${esc(it.action||'打开')}</button></div></div>`;
   }
   for(const g of ncGroupEvents(sysEvents)) h+=groupHTML(g);
  }
  if(appEvents.length){
   h+='<div class="nc-sec">应用通知</div>';
   for(const g of ncGroupEvents(appEvents)) h+=groupHTML(g);
  }
  h+='<div class="nc-sec">小部件</div>'+ncWidgetsHTML(data,nodeR,coreR);
 }
 scroll.innerHTML=h;
 const eb=document.getElementById('nc-editbtn'); if(eb) eb.textContent=ncEditMode?'完成':'编辑小部件';
 scroll.querySelectorAll('[data-nact]').forEach(b=>b.onclick=e=>{ e.stopPropagation(); routeJump(b.dataset.nact); });
 scroll.querySelectorAll('.ncard[data-nroute]').forEach(c=>{
  if(c.dataset.nexpand!==undefined&&c.dataset.nexpand!=='') return;
  c.onclick=()=>{ const id=c.dataset.nev; if(id) ncDismiss(id); routeJump(c.dataset.nroute); };
 });
 scroll.querySelectorAll('[data-nexpand]').forEach(c=>c.onclick=()=>{ ncExpandedSrc.add(c.dataset.nexpand); paintNC(); });
 scroll.querySelectorAll('[data-nless]').forEach(el=>el.onclick=()=>{ ncExpandedSrc.delete(el.dataset.nless); paintNC(); });
 scroll.querySelectorAll('[data-nx]').forEach(x=>x.onclick=e=>{ e.stopPropagation(); ncDismiss(x.dataset.nx); });
 scroll.querySelectorAll('[data-nclear]').forEach(b=>b.onclick=async e=>{ e.stopPropagation(); const k=b.dataset.nclear; const body=k.startsWith('app:')?{source:'app',app:k.slice(4)}:{source:k}; await api('/api/notify/clear-source',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)}).catch(()=>{}); ncExpandedSrc.delete(k); paintNC(); });
 scroll.querySelectorAll('[data-wdg]').forEach(el=>el.onclick=()=>{ const r=el.dataset.wdg; if(r) routeJump(r); });
 scroll.querySelectorAll('[data-wshow]').forEach(el=>el.onchange=()=>ncWidgetEdit(el.dataset.wshow,'show',el.checked));
 scroll.querySelectorAll('[data-wmove]').forEach(b=>b.onclick=()=>ncWidgetEdit(b.dataset.wmove,'move',+b.dataset.dir));
}
export async function ncDismiss(id){
 await api('/api/notify/dismiss',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({id})}).catch(()=>{});
 if(ncOpen) paintNC();
}
export const NC_WNAMES={calendar:'日历',server:'服务器',node:'节点'};
export function ncWidgetsEditHTML(){
 const layout=ncLayoutWidgets(SETTINGS&&SETTINGS.nc_widgets);
 return layout.map((w,i)=>`<div class="wdg-edit"><span>${NC_WNAMES[w.id]||w.id}</span><span class="row"><button data-wmove="${w.id}" data-dir="-1" ${i===0?'disabled style="opacity:.35"':''}>↑</button><button data-wmove="${w.id}" data-dir="1" ${i===layout.length-1?'disabled style="opacity:.35"':''}>↓</button><label class="sm-sw"><input type="checkbox" data-wshow="${w.id}" ${w.show?'checked':''}><i></i></label></span></div>`).join('')+'<p class="set-note" style="margin:4px 2px">开关 = 在通知中心显示这个小部件；↑↓ 调整上下顺序。改动即时保存。</p>';
}
export async function ncWidgetEdit(id,kind,val){
 const layout=ncLayoutWidgets(SETTINGS&&SETTINGS.nc_widgets);
 const i=layout.findIndex(w=>w.id===id); if(i<0) return;
 if(kind==='show') layout[i].show=!!val;
 else { const j=i+val; if(j<0||j>=layout.length) return; const t=layout[i]; layout[i]=layout[j]; layout[j]=t; }
 const r=await saveSetting({nc_widgets:JSON.stringify(layout)});
 if(r&&r.error){ alert('保存失败：'+r.error); return; }
 paintNC();
}
export function ncWidgetsHTML(data,nodeR,coreR){
 const layout=ncLayoutWidgets(SETTINGS&&SETTINGS.nc_widgets).filter(w=>w.show);
 if(!layout.length) return '<div class="nc-empty">小部件都隐藏了，点下方「编辑小部件」恢复</div>';
 return layout.map(w=>{
  if(w.id==='calendar'){
   const now=new Date(), y=now.getFullYear(), m=now.getMonth(), today=now.getDate();
   const first=new Date(y,m,1).getDay(), days=new Date(y,m+1,0).getDate();
   let cells=['日','一','二','三','四','五','六'].map(d=>`<span class="cal-h">${d}</span>`).join('');
   for(let i=0;i<first;i++) cells+='<span class="cal-d dim"></span>';
   for(let d=1;d<=days;d++) cells+=`<span class="cal-d${d===today?' today':''}">${d}</span>`;
   return `<div class="wdg" style="cursor:default"><div class="w-name">日历 · ${y}年${m+1}月</div><div class="cal-grid">${cells}</div></div>`;
  }
  if(w.id==='server'){
   const sv=(data.widgets&&data.widgets.server)||{};
   if(!sv.name) return `<div class="wdg" data-wdg="servermon"><div class="w-name">服务器</div><div class="w-sub">还没有配置服务器</div></div>`;
   const bars=sv.hasSnap?`<div class="w-sub">CPU ${Math.round(sv.cpuPct||0)}%</div><div class="w-bar"><i style="width:${Math.min(100,Math.max(0,sv.cpuPct||0))}%"></i></div><div class="w-sub">内存 ${Math.round(sv.memPct||0)}%</div><div class="w-bar"><i style="width:${Math.min(100,Math.max(0,sv.memPct||0))}%"></i></div>`:'<div class="w-sub">暂无采样数据</div>';
   return `<div class="wdg" data-wdg="servermon"><div class="w-name">服务器</div><div class="w-big">${esc(sv.name)} <span style="font-size:12px;font-weight:400;opacity:.8">${sv.online?'● 在线':'○ 离线'}</span></div>${bars}</div>`;
  }
  const nodes=(nodeR&&nodeR.nodes)||[], cur=nodes.length?nodes[0]:null, running=!!(coreR&&coreR.running);
  return `<div class="wdg" data-wdg="core"><div class="w-name">节点</div><div class="w-big">${cur?esc(cur.name):'还没有节点'}</div><div class="w-sub">${running?'已连接':'未连接'}${cur?(' · '+esc(cur.type)+' '+esc(cur.server)+':'+cur.port):''}</div></div>`;
 }).join('');
}
document.getElementById('nc-editbtn').onclick=()=>{ ncEditMode=!ncEditMode; paintNC(); };
setInterval(()=>{ if(ncOpen) paintNC(); if(ccOpen&&!ccSliderDrag) paintCC(); },20000);

export const __bind_ccOpen: any = { get(): any { return ccOpen; }, set(v: any): any { ccOpen = v; return v; } };

export const __bind_ncOpen: any = { get(): any { return ncOpen; }, set(v: any): any { ncOpen = v; return v; } };

export const __bind_ccNodesOpen: any = { get(): any { return ccNodesOpen; }, set(v: any): any { ccNodesOpen = v; return v; } };

export const __bind_ccRemoteOpen: any = { get(): any { return ccRemoteOpen; }, set(v: any): any { ccRemoteOpen = v; return v; } };

export const __bind_ccWallOpen: any = { get(): any { return ccWallOpen; }, set(v: any): any { ccWallOpen = v; return v; } };

export const __bind_ncEditMode: any = { get(): any { return ncEditMode; }, set(v: any): any { ncEditMode = v; return v; } };

export const __bind_ccSliderDrag: any = { get(): any { return ccSliderDrag; }, set(v: any): any { ccSliderDrag = v; return v; } };
