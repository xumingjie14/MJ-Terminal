/* Generated cross-module imports (v0.58 modularization):
   every name below is owned by the sibling module it is imported
   from -- the same names the pre-modular page shared through one
   script scope. */
import { APPS, api, esc } from './core';
import { SETDEF, SETTINGS } from './settings';
import { MJEvents } from './events';
import { openApp } from './windows';
import { toast } from './appsload';
/* ----- v0.69 block 1: 任务管理器 -----
   Two surfaces over GET /api/taskmgr (the daemon's ownership-
   scoped snapshot; see taskmgr.go):
   - the menu-bar panel (#tmpanel): a quick look -- daemon row,
     process groups with aggregate CPU/memory and an end button
     per group, the open in-shell windows with a close button.
   - the 任务管理器 app window: the full table -- sortable
     columns, search filter, collapsible groups with per-process
     rows, per-window rows with the 无响应 flag, end / restart
     actions.
   Both poll once a second ONLY while visible; closing stops
   the sampling (the daemon samples per request, so a closed
   surface costs nothing). The in-shell layer is view state:
   its "end" is the window's own close light, the existing
   close path. Critical groups (core, the whole browser) ask
   for confirmation first; normal ones end immediately. The
   daemon row has no end action by design -- its only exit is
   设置 → 退出系统. */
export function tmFmtB(n){
 n=+n||0; const u=['B','KB','MB','GB','TB']; let i=0; while(n>=1024&&i<4){ n/=1024; i++; } return (n>=100||i===0?n.toFixed(0):n.toFixed(1))+' '+u[i];
}
export function tmFmtCPU(v){ return (Math.round((+v||0)*10)/10).toFixed(1)+'%'; }
export function tmFmtUp(sec){
 sec=+sec||0; const h=Math.floor(sec/3600), m=Math.floor(sec%3600/60);
 return h>0?(h+' 小时 '+m+' 分'):Math.max(1,m)+' 分';
}
export function tmShellWins(){
 /* Open in-shell windows: a window element that exists and is
    not hidden (the same test the 窗口 menu uses). */
 return APPS.filter(a=>{
  const w=document.getElementById('win-'+a.id);
  return w&&!w.classList.contains('hidden');
 });
}
export function tmCloseShellWin(id){
 const w=document.getElementById('win-'+id);
 if(!w) return;
 const b=w.querySelector('[data-a="close"]');
 if(b) (b as HTMLElement).click();
}
export async function tmKillGroup(g){
 if(g.critical && !confirm('结束「'+g.name+'」？\n'+(g.kind==='core'?'核心停下后节点会断开，需要时可以在任务管理器或节点页重新启动。':'整个浏览器和它打开的网页都会关闭，未保存的内容会丢失。'))) return false;
 const r=await api('/api/taskmgr/kill',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({group:g.id})}).catch(()=>null);
 if(r&&r.error){ toast(r.error); return false; }
 toast('已结束 '+g.name); return true;
}
export async function tmKillProc(g,p){
 const r=await api('/api/taskmgr/kill',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({pid:p.pid})}).catch(()=>null);
 if(r&&r.error){ toast(r.error); return false; }
 toast('已结束进程 '+p.name); return true;
}
export async function tmRestartGroup(g){
 if(!confirm('重启「'+g.name+'」？\n核心会用当前配置重新启动，节点短暂断开后自动接回。')) return false;
 const r=await api('/api/taskmgr/restart',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({group:g.id})}).catch(()=>null);
 if(r&&r.error){ toast(r.error); return false; }
 toast('已重启 '+g.name); return true;
}
export async function tmCloseExtWin(hwnd){
 await api('/api/extwin/close',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({hwnd})}).catch(()=>null);
}

/* ----- menu-bar entry + quick panel ----- */
export const tmPanelEl=document.getElementById('tmpanel'), tmBarEl=document.getElementById('mb-taskmgr');
export let tmPanelOpen=false, tmPanelTimer=null;
export function tmBarVisible(){ try{ const s=SETTINGS||SETDEF; return !s||s.taskmgrBar!==false; }catch(e){ return true; } }
export function applyTmBar(){ try{ if(tmBarEl) tmBarEl.style.display=tmBarVisible()?'':'none'; }catch(e){} }
export function closeTmPanel(){
 if(!tmPanelOpen) return;
 tmPanelOpen=false; tmPanelEl.classList.add('hidden');
 if(tmBarEl) tmBarEl.classList.remove('on');
 if(tmPanelTimer){ clearInterval(tmPanelTimer); tmPanelTimer=null; }
}
export async function paintTmPanel(){
 if(!tmPanelOpen) return;
 const snap=await api('/api/taskmgr').catch(()=>null);
 if(!tmPanelOpen||!snap) return;
 const d=snap.daemon||{};
 const groups=snap.groups||[];
 const shells=tmShellWins();
 let h='<div class="tmp-hd"><span>任务管理器</span><button class="tmp-btn gray" id="tmp-open">打开完整窗口</button></div>';
 h+='<div class="tmp-row"><span class="tmp-name">守护进程（铭杰终端机 v'+esc(d.version||'')+'）</span><span class="tmp-num">'+tmFmtCPU(d.cpu)+'</span><span class="tmp-num">'+tmFmtB(d.mem)+'</span></div>';
 h+='<div class="tmp-sub">已运行 '+tmFmtUp(d.uptimeSec)+' · 守护进程不在这里结束（出口在设置 → 退出系统）</div>';
 if(shells.length){
  h+='<div class="tmp-sec">系统内窗口</div>';
  shells.forEach(a=>{ h+='<div class="tmp-row"><span class="tmp-name">'+esc(a.n)+'</span><button class="tmp-btn gray" data-tmclose="'+a.id+'">关闭</button></div>'; });
 }
 h+='<div class="tmp-sec">终端的进程（按应用分组）</div>';
 if(!groups.length) h+='<div class="tmp-row"><span class="tmp-name" style="opacity:.6">现在没有在跑的外部进程</span></div>';
 groups.forEach(g=>{
  const hung=(g.windows||[]).some(w=>w.hung);
  h+='<div class="tmp-row"><span class="tmp-name">'+esc(g.name)+(hung?' <span class="tmp-hung">无响应</span>':'')+'</span><span class="tmp-num">'+tmFmtCPU(g.cpu)+'</span><span class="tmp-num">'+tmFmtB(g.mem)+'</span><button class="tmp-btn" data-tmkill="'+esc(g.id)+'">结束</button>'+(g.canRestart?'<button class="tmp-btn gray" data-tmrestart="'+esc(g.id)+'">重启</button>':'')+'</div>';
 });
 tmPanelEl.innerHTML=h;
 const ob=document.getElementById('tmp-open'); if(ob) ob.onclick=()=>{ closeTmPanel(); openApp('taskmgr'); };
 tmPanelEl.querySelectorAll('[data-tmclose]').forEach(b=>{ (b as HTMLElement).onclick=()=>{ tmCloseShellWin((b as HTMLElement).dataset.tmclose); setTimeout(paintTmPanel,250); }; });
 tmPanelEl.querySelectorAll('[data-tmkill]').forEach(b=>{ (b as HTMLElement).onclick=async()=>{ const g=groups.find(x=>x.id===(b as HTMLElement).dataset.tmkill); if(g&&await tmKillGroup(g)) setTimeout(paintTmPanel,250); }; });
 tmPanelEl.querySelectorAll('[data-tmrestart]').forEach(b=>{ (b as HTMLElement).onclick=async()=>{ const g=groups.find(x=>x.id===(b as HTMLElement).dataset.tmrestart); if(g&&await tmRestartGroup(g)) setTimeout(paintTmPanel,250); }; });
}
export function openTmPanel(){
 tmPanelOpen=true; tmPanelEl.classList.remove('hidden');
 if(tmBarEl) tmBarEl.classList.add('on');
 paintTmPanel();
 if(tmPanelTimer) clearInterval(tmPanelTimer);
 tmPanelTimer=setInterval(paintTmPanel,1000);
}
if(tmBarEl) tmBarEl.onclick=e=>{ e.stopPropagation(); tmPanelOpen?closeTmPanel():openTmPanel(); };
document.addEventListener('click',e=>{ if(tmPanelOpen&&!tmPanelEl.contains(e.target as Node)&&e.target!==tmBarEl&&!tmBarEl.contains(e.target as Node)) closeTmPanel(); });
document.addEventListener('keydown',e=>{ if(e.key==='Escape'&&tmPanelOpen) closeTmPanel(); });
applyTmBar();
try{ if(MJEvents&&MJEvents.on) MJEvents.on('settings',()=>applyTmBar()); }catch(e){}

/* ----- the full window (pages.ts render/bind plug in here) ----- */
export const tmPage={ q:'', sort:'mem', dir:-1, collapsed:new Set(), timer:null, snap:null };
export function tmPageHTML(){
 return '<div class="tm-wrap">'
 +'<div class="tm-daemon" id="tm-daemon">读取中…</div>'
 +'<div class="tm-tools"><input class="txt" type="text" id="tm-q" placeholder="搜索进程 / 应用"><span class="tag" id="tm-note"></span><button class="btn gray" id="tm-open-panel">顶栏速览</button></div>'
 +'<div class="tm-table" id="tm-table"></div>'
 +'<div class="tm-foot">只列终端机自己的进程：守护进程拉起的浏览器、核心、组件、商店与沙盒应用，加上系统内窗口。电脑上其他软件的进程不在这里，也不会被动到。</div>'
 +'</div>';
}
export function tmSortedGroups(snap){
 const gs=((snap&&snap.groups)||[]).slice();
 const k=tmPage.sort, d=tmPage.dir;
 gs.sort((a,b)=>{
  let r=0;
  if(k==='name') r=String(a.name).localeCompare(String(b.name));
  else if(k==='cpu') r=(a.cpu||0)-(b.cpu||0);
  else if(k==='mem') r=(a.mem||0)-(b.mem||0);
  else if(k==='status') r=((a.windows||[]).some(w=>w.hung)?1:0)-((b.windows||[]).some(w=>w.hung)?1:0);
  return r*d;
 });
 return gs;
}
export function paintTmPage(){
 const table=document.getElementById('tm-table'); if(!table) return;
 const snap=tmPage.snap;
 const dEl=document.getElementById('tm-daemon');
 if(dEl&&snap&&snap.daemon){
  const d=snap.daemon;
  dEl.innerHTML='守护进程 <b>铭杰终端机 v'+esc(d.version||'')+'</b> · 已运行 '+tmFmtUp(d.uptimeSec)+' · CPU '+tmFmtCPU(d.cpu)+' · 内存 '+tmFmtB(d.mem)+' · PID '+d.pid+'<br><span style="opacity:.6">守护进程不能在这里结束——彻底退出在「设置 → 通用 → 退出系统」。</span>';
 }
 const q=tmPage.q.trim().toLowerCase();
 const shells=tmShellWins().filter(a=>!q||a.n.toLowerCase().includes(q));
 const arrow=k=>tmPage.sort===k?(tmPage.dir<0?' ▾':' ▴'):'';
 let h='<div class="tm-th"><span class="tm-c-name" data-sort="name">名称'+arrow('name')+'</span><span class="tm-c-cpu" data-sort="cpu">CPU'+arrow('cpu')+'</span><span class="tm-c-mem" data-sort="mem">内存'+arrow('mem')+'</span><span class="tm-c-st" data-sort="status">状态'+arrow('status')+'</span><span class="tm-c-act"></span></div>';
 if(shells.length){
  h+='<div class="tm-grow" style="cursor:default"><span class="tm-c-name">系统内窗口</span><span class="tm-c-cpu"></span><span class="tm-c-mem"></span><span class="tm-c-st"></span><span class="tm-c-act"></span></div>';
  shells.forEach(a=>{ h+='<div class="tm-prow"><span class="tm-c-name">'+esc(a.n)+'</span><span class="tm-c-cpu">—</span><span class="tm-c-mem">—</span><span class="tm-c-st">运行中</span><span class="tm-c-act"><button class="tmp-btn gray" data-tmclose="'+a.id+'">关闭窗口</button></span></div>'; });
 }
 const groups=tmSortedGroups(snap).filter(g=>{
  if(!q) return true;
  if(g.name.toLowerCase().includes(q)) return true;
  return (g.procs||[]).some(p=>p.name.toLowerCase().includes(q));
 });
 if(!groups.length&&!shells.length) h+='<div class="tm-prow"><span class="tm-c-name" style="opacity:.6">没有匹配的进程</span></div>';
 groups.forEach(g=>{
  const hung=(g.windows||[]).some(w=>w.hung);
  const open=!tmPage.collapsed.has(g.id);
  h+='<div class="tm-grow" data-tmtoggle="'+esc(g.id)+'"><span class="tm-c-name">'+(open?'▾ ':'▸ ')+esc(g.name)+' <span style="opacity:.5;font-weight:400">（'+(g.procs||[]).length+' 个进程）</span></span><span class="tm-c-cpu">'+tmFmtCPU(g.cpu)+'</span><span class="tm-c-mem">'+tmFmtB(g.mem)+'</span><span class="tm-c-st">'+(hung?'<span class="tmp-hung">无响应</span>':'运行中')+'</span><span class="tm-c-act"><button class="tmp-btn" data-tmkill="'+esc(g.id)+'">结束</button>'+(g.canRestart?' <button class="tmp-btn gray" data-tmrestart="'+esc(g.id)+'">重启</button>':'')+'</span></div>';
  if(open){
   (g.windows||[]).forEach(w=>{
    h+='<div class="tm-prow"><span class="tm-c-name">窗口 · '+esc(w.title||w.app||'')+(w.hung?' <span class="tmp-hung">无响应</span>':'')+'</span><span class="tm-c-cpu"></span><span class="tm-c-mem"></span><span class="tm-c-st">'+(w.hung?'无响应':'')+'</span><span class="tm-c-act"><button class="tmp-btn gray" data-tmwinclose="'+w.hwnd+'">关闭窗口</button></span></div>';
   });
   (g.procs||[]).filter(p=>!q||g.name.toLowerCase().includes(q)||p.name.toLowerCase().includes(q)).forEach(p=>{
    h+='<div class="tm-prow"><span class="tm-c-name">'+esc(p.name)+' <span style="opacity:.5">PID '+p.pid+'</span></span><span class="tm-c-cpu">'+tmFmtCPU(p.cpu)+'</span><span class="tm-c-mem">'+tmFmtB(p.mem)+'</span><span class="tm-c-st"></span><span class="tm-c-act"><button class="tmp-btn" data-tmkillpid="'+p.pid+'">结束进程</button></span></div>';
   });
  }
 });
 table.innerHTML=h;
 table.querySelectorAll('[data-sort]').forEach(el=>{ (el as HTMLElement).onclick=()=>{ const k=(el as HTMLElement).dataset.sort; if(tmPage.sort===k) tmPage.dir=-tmPage.dir; else { tmPage.sort=k; tmPage.dir=k==='name'?1:-1; } paintTmPage(); }; });
 table.querySelectorAll('[data-tmtoggle]').forEach(el=>{ (el as HTMLElement).onclick=e=>{ if((e.target as HTMLElement).closest('button')) return; const id=(el as HTMLElement).dataset.tmtoggle; tmPage.collapsed.has(id)?tmPage.collapsed.delete(id):tmPage.collapsed.add(id); paintTmPage(); }; });
 table.querySelectorAll('[data-tmclose]').forEach(b=>{ (b as HTMLElement).onclick=()=>{ tmCloseShellWin((b as HTMLElement).dataset.tmclose); setTimeout(tmPageTick,250); }; });
 table.querySelectorAll('[data-tmkill]').forEach(b=>{ (b as HTMLElement).onclick=async()=>{ const g=groups.find(x=>x.id===(b as HTMLElement).dataset.tmkill); if(g&&await tmKillGroup(g)) setTimeout(tmPageTick,250); }; });
 table.querySelectorAll('[data-tmrestart]').forEach(b=>{ (b as HTMLElement).onclick=async()=>{ const g=groups.find(x=>x.id===(b as HTMLElement).dataset.tmrestart); if(g&&await tmRestartGroup(g)) setTimeout(tmPageTick,250); }; });
 table.querySelectorAll('[data-tmkillpid]').forEach(b=>{ (b as HTMLElement).onclick=async()=>{ const pid=+(b as HTMLElement).dataset.tmkillpid; for(const g of groups){ const p=(g.procs||[]).find(x=>x.pid===pid); if(p){ if(await tmKillProc(g,p)) setTimeout(tmPageTick,250); break; } } }; });
 table.querySelectorAll('[data-tmwinclose]').forEach(b=>{ (b as HTMLElement).onclick=async()=>{ await tmCloseExtWin(+(b as HTMLElement).dataset.tmwinclose); setTimeout(tmPageTick,350); }; });
 const note=document.getElementById('tm-note');
 if(note&&snap) note.textContent='共 '+groups.reduce((n,g)=>n+(g.procs||[]).length,0)+' 个进程 · '+new Date((snap.at||0)*1000).toLocaleTimeString('zh-CN',{hour12:false});
}
export async function tmPageTick(){
 const win=document.getElementById('win-taskmgr');
 if(!win||win.classList.contains('hidden')){ tmPageStop(); return; }
 const snap=await api('/api/taskmgr').catch(()=>null);
 if(snap){ tmPage.snap=snap; paintTmPage(); }
}
export function tmPageStop(){ if(tmPage.timer){ clearInterval(tmPage.timer); tmPage.timer=null; } }
export function bindTmPage(){
 const q=document.getElementById('tm-q');
 if(q) q.oninput=()=>{ tmPage.q=(q as HTMLInputElement).value; paintTmPage(); };
 const op=document.getElementById('tm-open-panel');
 if(op) op.onclick=()=>openTmPanel();
 tmPageTick();
 tmPageStop();
 tmPage.timer=setInterval(tmPageTick,1000);
}
