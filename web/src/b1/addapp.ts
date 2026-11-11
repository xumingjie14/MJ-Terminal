/* Generated cross-module imports (v0.58 modularization):
   every name below is owned by the sibling module it is imported
   from -- the same names the pre-modular page shared through one
   script scope. */
import { api } from './core';
import { loadWebApps } from './ctxmenu';
import { renderDesk } from './desktop';
import { toast } from './appsload';
/* ----- v0.69 block 2, entry B: the 添加应用 dialog -----
   Opened from the desktop right-click menu and the Launchpad
   tile. Paste a URL -> 识别 asks the daemon to fetch the page
   through the node (title prefill + duplicate check against
   the WEBAPPS ledger) -> save goes through the ledger's own
   /api/apps/save, which also fetches the site icon through
   the node at add time (addapp.go). A duplicate URL is
   reported, never silently renamed over. */
export const aaEl=document.getElementById('addapp');
export let aaExists=false;
export function aaLetter(){
 const n=(document.getElementById('aa-name') as HTMLInputElement).value.trim();
 const ico=document.getElementById('aa-ico');
 if(ico) ico.textContent=(n||'?').slice(0,1).toUpperCase();
}
export function openAddApp(){
 if(!aaEl) return;
 aaExists=false;
 (document.getElementById('aa-url') as HTMLInputElement).value='';
 (document.getElementById('aa-name') as HTMLInputElement).value='';
 const note=document.getElementById('aa-note'); if(note){ note.textContent=''; note.className='aa-note'; }
 aaLetter();
 aaEl.classList.remove('hidden');
 setTimeout(()=>{ const u=document.getElementById('aa-url'); if(u) u.focus(); },30);
}
export function closeAddApp(){ if(aaEl) aaEl.classList.add('hidden'); }
export async function aaProbe(){
 const urlEl=document.getElementById('aa-url') as HTMLInputElement;
 const nameEl=document.getElementById('aa-name') as HTMLInputElement;
 const note=document.getElementById('aa-note');
 const url=urlEl.value.trim();
 if(!url){ if(note){ note.textContent='先填网址。'; note.className='aa-note'; } return; }
 if(note){ note.textContent='正在走节点识别…'; note.className='aa-note'; }
 const r=await api('/api/webapps/probe',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({url})}).catch(()=>null);
 if(!r){ if(note) note.textContent='识别失败，检查一下网址。'; return; }
 if(r.error){ if(note){ note.textContent=r.error; note.className='aa-note'; } return; }
 urlEl.value=r.url||url;
 if(r.exists){
  aaExists=true;
  if(note){ note.textContent='这个网页已经在桌面上了（「'+(r.name||'')+'」），不用重复添加。'; note.className='aa-note'; }
  return;
 }
 aaExists=false;
 if(r.name) nameEl.value=r.name;
 aaLetter();
 if(note){ note.textContent='已识别，名称和图标会在保存时抓好。'; note.className='aa-note ok'; }
}
export async function aaSave(){
 const url=(document.getElementById('aa-url') as HTMLInputElement).value.trim();
 const name=(document.getElementById('aa-name') as HTMLInputElement).value.trim();
 const note=document.getElementById('aa-note');
 if(aaExists){ if(note){ note.textContent='已存在的应用不能重复添加。'; note.className='aa-note'; } return; }
 if(!url){ if(note) note.textContent='先填网址。'; return; }
 if(!name){ if(note) note.textContent='给它起个名字（或先点「识别」自动填）。'; return; }
 const r=await api('/api/apps/save',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({name,url})}).catch(()=>null);
 if(!r||r.error){ if(note){ note.textContent='保存失败：'+((r&&r.error)||'网络问题'); note.className='aa-note'; } return; }
 closeAddApp();
 await loadWebApps();
 renderDesk();
 toast('已添加「'+name+'」到桌面');
}
if(aaEl){
 document.getElementById('aa-probe').onclick=()=>aaProbe();
 document.getElementById('aa-save').onclick=()=>aaSave();
 document.getElementById('aa-cancel').onclick=()=>closeAddApp();
 (document.getElementById('aa-name') as HTMLInputElement).oninput=()=>aaLetter();
 (document.getElementById('aa-url') as HTMLInputElement).onkeydown=e=>{ if(e.key==='Enter') aaProbe(); };
 (document.getElementById('aa-name') as HTMLInputElement).onkeydown=e=>{ if(e.key==='Enter') aaSave(); };
 aaEl.addEventListener('click',e=>{ if(e.target===aaEl) closeAddApp(); });
 document.addEventListener('keydown',e=>{ if(e.key==='Escape'&&!aaEl.classList.contains('hidden')) closeAddApp(); });
}
