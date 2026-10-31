/* Generated cross-module imports (v0.58 modularization):
   every name below is owned by the sibling module it is imported
   from -- the same names the pre-modular page shared through one
   script scope. */
import { APPS, esc, verQ, winsEl, z, __bind_z } from './core';
import { glyph } from './glyphs';
import { lastApp, onWinClosed, openSet, setFront, stageOn, __bind_lastApp } from './stage';
import { showStageCtx } from './extstrip';
import { saveSession } from './session';
import { ffDeskIconHTML } from './appsload';
import { render } from './pages';
/* ----- windows ----- */
export function appIcon(a: any, size?: any){
 if(a.id==='firefox')return ffDeskIconHTML();
 /* Built-in art (art:1): the SELF-DRAWN set the daemon serves
    at /api/appicon/ (test2; test1's macosicons set was
    field-rejected as incoherent). The artwork fills the tile
    edge-to-edge; if the image fails it removes itself and the
    plain gradient tile underneath shows -- the icon degrades,
    never breaks. Provenance: icon-provenance.md. */
 if(a.art)return `<div class="appico ${a.g}"><img class="appArt" alt="" draggable="false" src="${verQ('/api/appicon/'+a.id)}" onerror="this.remove()"></div>`;
 /* Chrome is a third-party brand icon: the daemon serves the
    real thing at /api/chrome/icon; the drawn dot stays
    underneath as the degradation path. */
 if(a.id==='browser')return `<div class="appico g-browser"><div class="chrome-dot"></div><img class="appArt" alt="" draggable="false" src="${verQ('/api/chrome/icon')}" onerror="this.remove()"></div>`;
 if(a.gkey)return `<div class="appico ${a.g}" style="color:#fff">${glyph(a.gkey,27)}</div>`;
 return `<div class="appico ${a.g}">${esc(a.glyph)}</div>`;
}
export function makeWin(app,i){
 const w=document.createElement('section');w.className='win hidden';w.id='win-'+app.id;
 w.style.left=(90+i*40)+'px';w.style.top=(86+i*30)+'px';
 w.innerHTML=`<div class="title"><div class="lights"><button class="l-close" data-a="close"><span>✕</span></button><button class="l-min" data-a="min"><span>–</span></button><button class="l-max"><span>＋</span></button></div><b>${app.n}</b></div><div class="body" id="body-${app.id}"></div>`;
 const title=w.querySelector('.title');
 title.addEventListener('pointerdown',(ev: any)=>{
   if(ev.target.closest('button'))return;bring(w);
   const sx=ev.clientX-w.offsetLeft, sy=ev.clientY-w.offsetTop;
   function mv(e){w.style.left=Math.max(0,e.clientX-sx)+'px';w.style.top=Math.max(44,e.clientY-sy)+'px'}
   function up(){removeEventListener('pointermove',mv);removeEventListener('pointerup',up);saveSession()}
   addEventListener('pointermove',mv);addEventListener('pointerup',up);
 });
 title.addEventListener('dblclick',()=>zoomWin(w));
 /* v0.50: the green light was decorative until now -- it runs
    the same zoom as the title double-click and the 窗口 menu. */
 w.querySelector('.l-max').onclick=()=>zoomWin(w);
 w.querySelector('[data-a="close"]').onclick=()=>{w.classList.add('hidden');onWinClosed(app.id);syncDots()};
 w.addEventListener('pointerdown',()=>{ if(w.classList.contains('staged')) openApp(app.id); });
 /* v0.56 test2 item 3: a STAGED window's right-click is the
    stage menu (switch / close). A front window's right-click
    stays the app's own -- never hijacked. */
 w.addEventListener('contextmenu',ev=>{ if(!w.classList.contains('staged')) return; ev.preventDefault(); ev.stopPropagation(); showStageCtx(ev.clientX,ev.clientY,app); });
 w.querySelector('[data-a="min"]').onclick=()=>{w.classList.add('hidden');onWinClosed(app.id);syncDots()};
 winsEl.appendChild(w);return w;
}
export function bring(w){w.style.zIndex=__bind_z.set(z + 1);saveSession()}
/* v0.50: the one zoom behaviour (title double-click, green
   traffic light, 窗口 → 缩放 all call this). */
export function zoomWin(w){ if(!w) return; w.style.width=w.style.width==='96vw'?'':'96vw'; w.style.left=w.style.width?'2vw':'90px'; saveSession(); }
export let openApp: any = function openApp(id){
 let w=document.getElementById('win-'+id);
 if(!w){w=makeWin(APPS.find(a=>a.id===id),APPS.findIndex(a=>a.id===id));render(id)}
 __bind_lastApp.set(id);
 if(stageOn()){ openSet.add(id); setFront(id); }
 else { w.classList.remove('hidden'); bring(w); }
 document.getElementById('mb-appname').textContent=APPS.find(a=>a.id===id).n;
 document.querySelectorAll('.icon').forEach(x=>x.classList.toggle('sel',x.dataset.key==='app:'+id));
 syncDots();
 saveSession();
 /* Settings re-fetches its live rows every time it is opened
    (render/bind only run on first creation). */
 if(id==='sys'&&window.sysRefresh) window.sysRefresh();
};
export function syncDots(){
 APPS.forEach(a=>{const d=document.getElementById('ddot-'+a.id);if(!d)return;const w=document.getElementById('win-'+a.id);d.classList.toggle('on',!!w&&!w.classList.contains('hidden'))});
}

export const __bind_openApp: any = { get(): any { return openApp; }, set(v: any): any { openApp = v; return v; } };
