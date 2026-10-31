/* Generated cross-module imports (v0.58 modularization):
   every name below is owned by the sibling module it is imported
   from -- the same names the pre-modular page shared through one
   script scope. */
import { esc } from './core';
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