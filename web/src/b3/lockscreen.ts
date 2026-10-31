
/* v0.48: the 铭杰 menu (top-left, macOS Apple-menu position) and the
   vault lock screen. Self-contained: it talks only to the vault and
   shutdown endpoints and shares `api`/`SETTINGS` with the desktop
   script. */
(function(){
 var LK={shown:false,busy:false,ackErr:''};
 function $(id){ return document.getElementById(id); }
 function vapi(path: any, opts?: any){ return api(path,opts).catch(function(){ return null; }); }
 function phaseText(st){
   if(st.phase==='sealing') return '正在上锁：加密整个 Data 目录…';
   if(st.phase==='unlocking') return '正在解锁：逐片解密还原…';
   if(st.partial) return '上次上锁被中断，输入密码恢复全部数据';
   return '输入密码解锁';
 }
 function paintLock(st){
   var sub=$('lk-sub'), form=$('lk-form'), prog=$('lk-prog');
   sub.textContent=phaseText(st);
   var working=(st.phase==='sealing'||st.phase==='unlocking');
   form.style.display=working?'none':'';
   prog.style.display=working?'':'none';
   /* v0.53: a failed unlock restore explains itself right here
      (status.lastError) -- while locked there is no other place
      the reason could be seen. It stays up until the next unlock
      attempt (submit acknowledges the standing text), and a new
      attempt resets the acknowledgement when its job starts, so
      even an identical new failure is shown again. */
   var le=$('lk-lasterr');
   if(working) LK.ackErr='';
   var msg=st.lastError||'';
   if(msg&&msg!==LK.ackErr){ le.textContent=msg; le.style.display='block'; }
   else { le.textContent=''; le.style.display='none'; }
   if(working&&st.progressTotal>0){
     var pct=Math.min(100,Math.round((st.progress||0)*100/st.progressTotal));
     $('lk-bar-i').style.width=pct+'%';
     $('lk-progtxt').textContent=pct+'%（'+((st.progress||0)/1048576).toFixed(0)+' / '+(st.progressTotal/1048576).toFixed(0)+' MB）';
   }
 }
 function showLock(st){ LK.shown=true; $('lockscreen').classList.add('show'); if(st) paintLock(st); var pw=$('lk-pw'); if(pw&&st&&st.phase!=='sealing'&&st.phase!=='unlocking') setTimeout(function(){ pw.focus(); },60); }
 function hideLock(){ LK.shown=false; $('lockscreen').classList.remove('show'); }
 window.__vaultShowLock=function(){ vapi('/api/vault/status').then(function(st){ if(st) showLock(st); }); };
 async function vaultSync(){
   var st=await vapi('/api/vault/status'); if(!st) return;
   window.__vault=st;
   if(st.locked){ if(!LK.shown) showLock(st); else paintLock(st); }
   else if(LK.shown){ hideLock(); location.reload(); }
 }
 $('lk-form').addEventListener('submit',async function(ev){
   ev.preventDefault(); if(LK.busy) return;
   var pw=$('lk-pw').value; if(!pw) return;
   LK.busy=true; $('lk-err').textContent='';
   /* Clear the standing restore error for this attempt: the new
      result (success, or a fresh lastError) replaces it. */
   LK.ackErr=(window.__vault&&window.__vault.lastError)||'';
   var le0=$('lk-lasterr'); le0.textContent=''; le0.style.display='none';
   var r=await vapi('/api/vault/unlock',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({password:pw})});
   LK.busy=false;
   if(!r||r.error){
     var card=$('lk-card'); card.classList.remove('lk-shake'); void card.offsetWidth; card.classList.add('lk-shake');
     $('lk-err').textContent=(r&&r.error)?r.error:'解锁失败';
     $('lk-pw').value=''; $('lk-pw').focus();
     return;
   }
   $('lk-pw').value='';
   vaultSync();
 });
 $('lk-off').onclick=async function(){
   if(!confirm('关机将退出终端机。继续？')) return;
   await vapi('/api/shutdown',{method:'POST'});
   $('lk-sub').textContent='已关机';
   $('lk-form').style.display='none';
 };
 /* --- the 铭杰 menu --- */
 var menu=$('mbmenu'), logo=$('mb-logo');
 function closeMenu(){ menu.classList.add('hidden'); }
 async function openMenu(){
   var st=await vapi('/api/vault/status'); if(st) window.__vault=st;
   var vaultOn=!!(window.__vault&&window.__vault.enabled);
   var html='<div class="mbm-it" id="mbm-about">关于铭杰终端机</div><div class="mbm-sep"></div>';
   /* v0.50: 设置… sits where macOS puts it (right under 关于）,
      with its ⌘, shortcut shown and really bound (see below). */
   html+='<div class="mbm-it" id="mbm-settings">设置…<span style="float:right;opacity:.6;margin-left:30px;font-size:12.5px">⌘,</span></div><div class="mbm-sep"></div>';
   if(vaultOn) html+='<div class="mbm-it" id="mbm-lock">锁定屏幕</div><div class="mbm-sep"></div>';
   html+='<div class="mbm-it" id="mbm-off">关机…</div>';
   menu.innerHTML=html;
   menu.classList.remove('hidden');
   $('mbm-about').onclick=function(){ closeMenu(); openApp('home'); };
   $('mbm-settings').onclick=function(){ closeMenu(); openApp('sys'); };
   $('mbm-off').onclick=async function(){
     closeMenu();
     var closeApps=!(window.SETTINGS&&SETTINGS.exitClosesApps===false);
     var msg=closeApps?'关机将退出终端机，并关闭它打开的应用。继续？':'关机将退出终端机（打开的应用会留在宿主上继续运行）。继续？';
     if(window.__vault&&window.__vault.enabled) msg='关机将退出终端机，并把整个 Data 目录加密上锁。继续？';
     if(!confirm(msg)) return;
     await vapi('/api/shutdown',{method:'POST'});
     document.body.innerHTML='<div style="position:fixed;inset:0;display:flex;align-items:center;justify-content:center;font-size:20px;color:#fff;text-shadow:0 2px 10px #000">已关机</div>';
   };
   var lk=$('mbm-lock');
   if(lk) lk.onclick=async function(){
     closeMenu();
     if(!confirm('锁定屏幕会关闭终端机内打开的应用，并把全部数据加密上锁。继续？')) return;
     var r=await vapi('/api/vault/lock',{method:'POST'});
     if(r&&r.error){ alert(r.error); return; }
     window.__vaultShowLock();
   };
 }
 logo.onclick=function(ev){ ev.stopPropagation(); if(menu.classList.contains('hidden')) openMenu(); else closeMenu(); };
 document.addEventListener('mousedown',function(ev){ if(!menu.classList.contains('hidden')&&!menu.contains(ev.target as Node)&&ev.target!==logo) closeMenu(); });
 document.addEventListener('keydown',function(ev){ if(ev.key==='Escape') closeMenu(); });
 /* v0.50: ⌘, opens Settings, like macOS (Ctrl+, accepted too --
    Windows keyboards have no ⌘ key; the menu shows ⌘, because
    that is the macOS grammar this shell replicates). While the
    vault is locked the key does nothing: the lock screen owns
    the keyboard. */
 document.addEventListener('keydown',function(ev){
   if((ev.metaKey||ev.ctrlKey)&&ev.key===','&&!ev.altKey){
     if(window.__vault&&window.__vault.locked) return;
     ev.preventDefault(); closeMenu(); openApp('sys');
   }
 });
 vaultSync();
 setInterval(vaultSync,3000);
})();