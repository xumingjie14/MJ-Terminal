/* Generated cross-module imports (v0.58 modularization):
   every name below is owned by the sibling module it is imported
   from -- the same names the pre-modular page shared through one
   script scope. */
import { $50 } from './fvhead';
/* ================= menu bar: the five menus + front-app menu == */
export const mbdrop=$50('mbdrop');
export let mbOpenFor=null, lastEditTarget=null;
document.addEventListener('focusin',e=>{
 const t=e.target;
 if(t&&(t.tagName==='INPUT'||t.tagName==='TEXTAREA'||t.isContentEditable)) lastEditTarget=t;
});
export function closeMbMenu(){
 mbdrop.classList.add('hidden');
 document.querySelectorAll('#menubar .mi.on').forEach(x=>x.classList.remove('on'));
 mbOpenFor=null;
}
export function mbItem(label,opts){
 opts=opts||{};
 const d=document.createElement('div');
 d.className='mbm-it'+(opts.dis?' dis':'');
 d.innerHTML='<span class="mbm-ck">'+(opts.check?'✓':'')+'</span>'+esc(label)
  +(opts.sub?' <span class="mbm-sub">'+esc(opts.sub)+'</span>':'')
  +(opts.key?'<span class="mbm-key">'+esc(opts.key)+'</span>':'');
 if(!opts.dis&&opts.fn) d.onclick=()=>{ closeMbMenu(); opts.fn(); };
 return d;
}
export function mbSep(){ const d=document.createElement('div'); d.className='mbm-sep'; return d; }
export function activeWinEl(){
 let best=null,bz=-1;
 document.querySelectorAll('.win').forEach(w=>{
  if(w.classList.contains('hidden')||w.classList.contains('staged')) return;
  const zi=parseInt(w.style.zIndex||'0',10)||0;
  if(zi>=bz){ bz=zi; best=w; }
 });
 return best;
}
export function winOfApp(id){ return document.getElementById('win-'+id); }
export function openMbMenu(spanId,builder){
 const span=$50(spanId); if(!span) return;
 document.querySelectorAll('#menubar .mi.on').forEach(x=>x.classList.remove('on'));
 span.classList.add('on');
 mbOpenFor=spanId;
 const rect=span.getBoundingClientRect();
 mbdrop.style.left=Math.max(6,Math.min(rect.left-2,innerWidth-270))+'px';
 mbdrop.innerHTML='<div class="mbm-it dis">…</div>';
 mbdrop.classList.remove('hidden');
 const fill=items=>{
  if(mbOpenFor!==spanId) return; // a faster hover already moved on
  mbdrop.innerHTML='';
  items.forEach(n=>mbdrop.appendChild(n));
 };
 const out=builder();
 if(out&&typeof out.then==='function') out.then(fill); else fill(out);
}
export function mbToggle(spanId,builder){
 if(mbOpenFor===spanId){ closeMbMenu(); return; }
 openMbMenu(spanId,builder);
}
export function buildAppMenu(){
 const a=APPS.find(x=>x.id===lastApp)||APPS[0];
 const w=winOfApp(a.id);
 const open=!!w&&!w.classList.contains('hidden');
 const items=[ mbItem('打开「'+a.n+'」',{fn:()=>openApp(a.id)}) ];
 items.push(mbSep());
 items.push(mbItem('最小化',{dis:!open,fn:()=>{ const b=w.querySelector('[data-a="min"]'); if(b) b.click(); }}));
 items.push(mbItem('关闭窗口',{dis:!w,fn:()=>{ const b=w.querySelector('[data-a="close"]'); if(b) b.click(); }}));
 return items;
}
export function buildFileMenu(){
 const aw=activeWinEl();
 return [
  mbItem('导入文件…',{fn:()=>{ const f=$50('deskimp'); if(f) f.click(); }}),
  mbItem('新建文件夹',{fn:()=>newFolder(Math.round(innerWidth/2),Math.round(innerHeight/3))}),
  mbSep(),
  mbItem('关闭窗口',{key:'⌘W',dis:!aw,fn:()=>{ const b=aw.querySelector('[data-a="close"]'); if(b) b.click(); }})
 ];
}
export function editTarget(){
 const ae=document.activeElement;
 if(ae&&(ae.tagName==='INPUT'||ae.tagName==='TEXTAREA'||ae.isContentEditable)) return ae;
 if(lastEditTarget&&document.contains(lastEditTarget)) return lastEditTarget;
 return null;
}
export function editDispatch(cmd,label){
 const t=editTarget(); if(!t) return;
 t.focus();
 let ok=false;
 try{ ok=document.execCommand(cmd); }catch(e){}
 if(ok) return;
 if(cmd==='paste'&&navigator.clipboard&&navigator.clipboard.readText){
  navigator.clipboard.readText().then(txt=>{
   if(t.setRangeText){ const s=t.selectionStart||0, e=t.selectionEnd||0; t.setRangeText(txt,s,e,'end'); t.dispatchEvent(new Event('input',{bubbles:true})); }
   else if(t.isContentEditable){ document.execCommand('insertText',false,txt); }
  }).catch(()=>toast('粘贴不可用：浏览器未授权读取剪贴板'));
  return;
 }
 toast('当前输入框不支持'+label);
}
export function buildEditMenu(){
 const has=!!editTarget();
 return [
  mbItem('剪切',{key:'⌘X',dis:!has,fn:()=>editDispatch('cut','剪切')}),
  mbItem('复制',{key:'⌘C',dis:!has,fn:()=>editDispatch('copy','复制')}),
  mbItem('粘贴',{key:'⌘V',dis:!has,fn:()=>editDispatch('paste','粘贴')}),
  mbItem('全选',{key:'⌘A',dis:!has,fn:()=>editDispatch('selectAll','全选')})
 ];
}
export function buildViewMenu(){
 const s=SETTINGS||SETDEF;
 const flip=async patch=>{ const r=await saveSetting(patch); if(r&&r.error) alert('保存失败：'+r.error); };
 return [
  mbItem('沉浸模式',{check:!!s.immersiveMode,fn:()=>flip({immersiveMode:!s.immersiveMode})}),
  mbItem('桌面全屏',{check:!!s.desktopFullscreen,fn:()=>flip({desktopFullscreen:!s.desktopFullscreen})}),
  mbItem('台前调度',{check:stageOn(),fn:()=>toggleStage(!stageOn())}),
  mbItem('Dock 自动隐藏',{check:dockAutoHideOn(),fn:async()=>{ await flip({dockAutoHide:!dockAutoHideOn()}); applyDockLayout(); }})
 ];
}
export async function buildWinMenu(){
 const aw=activeWinEl();
 const items=[
  mbItem('最小化',{dis:!aw,fn:()=>{ const b=aw.querySelector('[data-a="min"]'); if(b) b.click(); }}),
  mbItem('缩放',{dis:!aw,fn:()=>zoomWin(aw)})
 ];
 const inSys=APPS.filter(a=>{
  const w=winOfApp(a.id);
  return w&&!w.classList.contains('hidden');
 });
 let ext=[];
 try{ const r=await api('/api/extwin'); ext=(r.windows||[]).filter(x=>!x.shell); }catch(e){}
 if(inSys.length||ext.length) items.push(mbSep());
 inSys.forEach(a=>{
  const front=stageOn()?frontApp===a.id:(aw&&aw.id==='win-'+a.id);
  items.push(mbItem(a.n,{check:front,fn:()=>openApp(a.id)}));
 });
 ext.forEach(x=>{
  items.push(mbItem(x.app||'外部应用',{sub:x.title||'',fn:async()=>{
   const r=await api('/api/extwin/focus',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({hwnd:x.hwnd})}).catch(()=>null);
   if(r&&r.error) toast(r.error);
  }}));
 });
 return items;
}
export function buildHelpMenu(){
 const ver=(typeof sysInfo!=='undefined'&&sysInfo&&sysInfo.version)?('v'+sysInfo.version):'';
 const gotoSys=pid=>{ openApp('sys'); const go=()=>{ if(window.sysGoto) window.sysGoto(pid); }; go(); setTimeout(go,150); };
 return [
  mbItem('打开控制台',{fn:()=>openApp('console')}),
  mbItem('导出诊断包',{fn:()=>{ const a=document.createElement('a'); a.href='/api/diag/export'; document.body.appendChild(a); a.click(); a.remove(); }}),
  mbSep(),
  mbItem('关于铭杰终端机',{sub:ver,fn:()=>gotoSys('about')})
 ];
}
export const MB_BUILDERS={'mb-m-app':buildAppMenu,'mb-m-file':buildFileMenu,'mb-m-edit':buildEditMenu,'mb-m-view':buildViewMenu,'mb-m-win':buildWinMenu,'mb-m-help':buildHelpMenu};
Object.keys(MB_BUILDERS).forEach(id=>{
 const span=$50(id); if(!span) return;
 span.addEventListener('click',e=>{ e.stopPropagation(); mbToggle(id,MB_BUILDERS[id]); });
 span.addEventListener('mouseenter',()=>{ if(mbOpenFor&&mbOpenFor!==id) openMbMenu(id,MB_BUILDERS[id]); });
});
document.addEventListener('mousedown',e=>{
 if(!mbOpenFor) return;
 if(mbdrop.contains(e.target as Node)) return;
 if(e.target.closest&&e.target.closest('#menubar')) return;
 closeMbMenu();
});

export const __bind_mbOpenFor: any = { get(): any { return mbOpenFor; }, set(v: any): any { mbOpenFor = v; return v; } };

export const __bind_lastEditTarget: any = { get(): any { return lastEditTarget; }, set(v: any): any { lastEditTarget = v; return v; } };
