/* Generated cross-module imports (v0.58 modularization):
   every name below is owned by the sibling module it is imported
   from -- the same names the pre-modular page shared through one
   script scope. */
import { api, esc } from './core';
import { ANIM_BANNER_MS, BANNER_HOLD_MS, NC_SRCNAMES, bannersEl, ncDismiss, ncOpen, paintNC, routeJump } from './ccnc';
/* ----- banners (SSE from the daemon's notify module) ----- */
export function dismissBannerEl(el){ if(!el||el.classList.contains('b-out')) return; el.classList.add('b-out'); setTimeout(()=>el.remove(),ANIM_BANNER_MS); }
export function showBanner(ev){
 const el=document.createElement('div');
 el.className='banner glasspanel b-in';
 el.innerHTML=`<span class="nx">✕</span><div class="n-src" style="font-size:11.5px;opacity:.65">${esc(ev.category==='app'?(ev.appName||'应用'):(NC_SRCNAMES[ev.source]||ev.source))}</div><div class="n-title" style="font-size:13.5px;font-weight:600;margin-top:1px">${esc(ev.title)}</div>${ev.body?`<div class="n-body" style="font-size:12.5px;opacity:.8;margin-top:2px;line-height:1.45">${esc(ev.body)}</div>`:''}`;
 bannersEl.appendChild(el);
 while(bannersEl.children.length>3) bannersEl.firstElementChild.remove();
 requestAnimationFrame(()=>requestAnimationFrame(()=>el.classList.remove('b-in')));
 let timer=setTimeout(()=>dismissBannerEl(el),BANNER_HOLD_MS);
 el.addEventListener('mouseenter',()=>clearTimeout(timer));
 el.addEventListener('mouseleave',()=>{ timer=setTimeout(()=>dismissBannerEl(el),2500); });
 el.querySelector('.nx').onclick=e=>{ e.stopPropagation(); ncDismiss(ev.id); dismissBannerEl(el); };
 el.onclick=()=>{ ncDismiss(ev.id); dismissBannerEl(el); routeJump(ev.route); };
}
(function connectNotifyStream(){
 try{
  const es=new EventSource('/api/notify/stream');
  es.onmessage=e=>{ try{ const ev=JSON.parse(e.data); if(ncOpen) paintNC(); else showBanner(ev); }catch(_){} };
 }catch(e){}
})();
/* ----- v0.71 block 5: the post-update browser-restart hint -----
   After an update, some browser-side settings (the download /
   save-as landing directories) only take effect once the
   isolated Chrome has fully exited and been reopened. The
   daemon decides whether the hint is owed (version changed
   this run, not yet dismissed for this version, Chrome
   actually running -- GET /api/update/restart-hint); this
   card is the one place it is shown. It does NOT auto-expire
   like a banner: it waits for a choice. 「稍后」 dismisses for
   this version (the daemon stamps it); 「立即重启浏览器」
   asks the daemon to close and relaunch the browser. */
let restartHintDone=false;
async function pollRestartHint(){
 if(restartHintDone) return;
 let r=null;
 try{ r=await api('/api/update/restart-hint'); }catch(e){ return; }
 if(!r||!r.show) return;
 restartHintDone=true;
 const el=document.createElement('div');
 el.className='banner glasspanel';
 el.innerHTML=`<span class="nx">✕</span><div class="n-src" style="font-size:11.5px;opacity:.65">软件更新</div><div class="n-title" style="font-size:13.5px;font-weight:600;margin-top:1px">新版本 v${esc(r.version||'')} 已就绪</div><div class="n-body" style="font-size:12.5px;opacity:.8;margin-top:2px;line-height:1.45">浏览器完全退出并重新打开后，文件保存位置等设置即刻生效。</div><div style="margin-top:8px;display:flex;gap:8px"><button class="btn" data-act="restart">立即重启浏览器</button><button class="btn gray" data-act="later">稍后</button></div>`;
 bannersEl.appendChild(el);
 const done=()=>{ el.remove(); };
 el.querySelector('.nx').onclick=e=>{ e.stopPropagation(); api('/api/update/restart-hint/dismiss',{method:'POST'}).catch(()=>null); done(); };
 el.querySelector('[data-act="later"]').onclick=()=>{ api('/api/update/restart-hint/dismiss',{method:'POST'}).catch(()=>null); done(); };
 el.querySelector('[data-act="restart"]').onclick=async()=>{ await api('/api/update/restart-hint/restart',{method:'POST'}).catch(()=>null); done(); };
}
setTimeout(pollRestartHint,6000);
setInterval(pollRestartHint,45000);