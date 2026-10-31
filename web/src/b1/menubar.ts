/* Generated cross-module imports (v0.58 modularization):
   every name below is owned by the sibling module it is imported
   from -- the same names the pre-modular page shared through one
   script scope. */
import { WALLPAPERS, api, customWall, wallEl, __bind_customWall } from './core';
import { wallKey } from './settings';
import { openApp } from './windows';
import { stageOn, toggleStage, updateStageUI } from './stage';
import { toast } from './appsload';
/* ----- wallpaper ----- */
export function applyWallpaper(){
 if(customWall){wallEl.style.background=`center/cover no-repeat url(/api/wallpaper?v=${Date.now()})`;return}
 const key=wallKey();
 wallEl.style.background=(WALLPAPERS[key]||WALLPAPERS.tahoe).css;
}
export async function detectCustomWall(){
 try{const r=await fetch('/api/wallpaper',{method:'GET'});__bind_customWall.set(r.ok)}catch(e){__bind_customWall.set(false)}
 applyWallpaper();
}
detectCustomWall();

/* ----- menu bar clock ----- */
export function tick(){
 const d=new Date();
 const s=d.toLocaleDateString('zh-CN',{month:'numeric',day:'numeric',weekday:'short'})+' '+d.toLocaleTimeString('zh-CN',{hour:'2-digit',minute:'2-digit',hour12:false});
 document.getElementById('mb-clock').textContent=s;
}
tick();setInterval(tick,5000);

/* ----- node status in menu bar ----- */
export async function pollNode(){
 try{
  const r=await api('/api/core',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'status'})});
  document.getElementById('mb-dot').className=r.running?'on':'';
  document.getElementById('mb-dot').id='mb-dot';
  document.getElementById('mb-nodetxt').textContent=r.running?'节点已连':'节点';
 }catch(e){}
}
pollNode();setInterval(pollNode,20000);
document.getElementById('mb-node').onclick=()=>openApp('core');

/* ----- remote assistance indicator in menu bar (v0.33) -----
   Visible whenever remote assistance is switched on (the Settings
   copy promises a persistent marker). Recent operator activity
   (last 60s, reported by the daemon) strengthens it to green. */
export async function pollRemote(){
 const el=document.getElementById('mb-remote'); if(!el) return;
 const dot=document.getElementById('mb-priv');
 try{
  const s=await api('/api/remote/state');
  /* v0.47 privacy indicator (the manual's dot beside Control
     Center): orange while a remote-assist session is actually
     live -- driven by the daemon's read-only sessionActive. */
  if(dot) dot.style.display=s.sessionActive?'block':'none';
  if(!s.enabled||!s.supported||s.netState==='off'||s.netState==='unsupported'){ el.style.display='none'; return; }
  el.style.display='flex';
  el.classList.toggle('active',!!s.recentActivity);
  document.getElementById('mb-rtxt').textContent=s.netState==='online'?'远程协助中':(s.netState==='needslogin'?'远程协助待登录':'远程协助启动中');
 }catch(e){ el.style.display='none'; if(dot) dot.style.display='none'; }
}
pollRemote();setInterval(pollRemote,10000);
document.getElementById('mb-remote').onclick=()=>openApp('sys');
/* mb-logo's click is owned by the v0.48 铭杰 menu (last script). */
/* v0.38: the red light hides this view instead of letting it be
   destroyed (daemon-side SW_HIDE; the tray brings the SAME window
   back). See shellHideHandler in shellview.go. */
document.getElementById('mb-hide').onclick=async()=>{ const r=await api('/api/shell/hide',{method:'POST'}).catch(()=>null); if(r&&r.error) toast(r.error); };
document.getElementById('mb-stage').onclick=()=>toggleStage(!stageOn());
updateStageUI();
