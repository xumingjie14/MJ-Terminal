/* Generated cross-module imports (v0.58 modularization):
   every name below is owned by the sibling module it is imported
   from -- the same names the pre-modular page shared through one
   script scope. */
import { WALLPAPERS, api } from './core';
import { applyWallpaper } from './menubar';
import { openApp } from './windows';
import { updateStageUI } from './stage';
import { renderDesk } from './desktop';
/* ----- unified settings (v0.30): Data/settings.json via /api/settings -----
   Wallpaper choice and the Stage Manager switch used to live in this
   view's localStorage -- which stays on the HOST PC, the opposite of
   portable. They are daemon settings now and travel with the folder.
   Until the first fetch lands, reads fall back to the old
   localStorage values so the first paint is right; when the daemon
   reports "fresh" (no settings file yet), the old local values are
   migrated up once and the local copies dropped. */
export let SETTINGS=null;
export const SETDEF={wallpaper:'tahoe',stageManager:false,idleExitMinutes:30,phoneMode:true,phoneTriggerWidth:1200,phoneWidth:400,chromeCacheMB:200,stageAutoClaimBrowser:true,filesOnDesktop:true,remoteEnabled:false,winPlaceOnClaim:true,winHideOnMinimize:true,winCloseToTray:true,immersiveMode:false,desktopFullscreen:false,dockPosition:'bottom',dockAutoHide:false,dockIconSize:52,cc_show_immersive:true,cc_show_stage:true,cc_show_fullscreen:true,cc_show_dockhide:true,cc_show_remote:true,cc_show_wallpaper:true,notify_allow_sandbox:true,notify_allow_update:true,notify_allow_servermon:true,notify_allow_node:true,notify_allow_remote:true,notify_allow_files:true,notify_banner_sandbox:true,notify_banner_update:true,notify_banner_servermon:true,notify_banner_node:true,notify_banner_remote:true,notify_banner_files:true,nc_widgets:'[{"id":"calendar","show":true},{"id":"server","show":true},{"id":"node","show":true}]',exitClosesApps:true,updateBeta:false,nodeStartMode:'last',nodeSubAuto:true,nodeSubIntervalMin:360,nodeTestURL:'http://www.gstatic.com/generate_204',nodeTestTimeoutMs:5000,nodeClashAPI:true,summonHotkey:'F8',defaultOfficeSuite:'onlyoffice',stageDisplay:'preview',deskIconSize:54,deskSort:'manual'};
export function wallKey(){ if(SETTINGS) return SETTINGS.wallpaper||'tahoe'; return localStorage.getItem('mj-wall')||'tahoe' }
/* v0.46 Dock basics: position + auto-hide are view-side behaviours
   over daemon settings (same pattern as the wallpaper). */
export function dockPos(){ const p=SETTINGS&&SETTINGS.dockPosition; return (p==='left'||p==='right')?p:'bottom' }
export function dockAutoHideOn(){ return !!(SETTINGS&&SETTINGS.dockAutoHide) }
export function saveSetting(patch){
 if(SETTINGS) Object.assign(SETTINGS,patch);
 return api('/api/settings',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(patch)})
  .then(r=>{ if(r&&!r.error) SETTINGS={...SETDEF,...r}; return r });
}
export async function loadSettings(){
 try{
  const s=await api('/api/settings');
  SETTINGS={...SETDEF,...s};
  if(s.fresh){
   const patch: any={};
   const lw=localStorage.getItem('mj-wall'); if(lw&&WALLPAPERS[lw]&&lw!==SETTINGS.wallpaper) patch.wallpaper=lw;
   const ls=localStorage.getItem('mj-stage'); if(ls!==null&&!!SETTINGS.stageManager!==(ls==='1')) patch.stageManager=(ls==='1');
   if(Object.keys(patch).length){ const r=await saveSetting(patch); if(r&&!r.error) SETTINGS={...SETDEF,...r}; }
   localStorage.removeItem('mj-wall'); localStorage.removeItem('mj-stage');
  }
 }catch(e){ if(!SETTINGS) SETTINGS={...SETDEF} }
 applyWallpaper(); updateStageUI(); applyDeskIconSize();
}
/* v0.56 desktop icon size: one settings key (deskIconSize, px),
   three surfaces （设置 slider / 控制中心 slider / desktop blank
   menu presets). The grid pitch derives from it (deskPitch), so
   the desktop re-renders at the new cell size; the Dock has its
   own size system and is untouched. The fake DOM in the boot
   test has a plain-object style, hence the guarded setProperty. */
export function deskIconSize(){ const v=SETTINGS&&+SETTINGS.deskIconSize; return (v>=36&&v<=96)?Math.round(v):54 }
export function deskPitch(){ return deskIconSize()+50 }
export let deskRenderedOnce=false;
export function applyDeskIconSize(){
 try{ const st=document.documentElement&&document.documentElement.style; if(st&&st.setProperty) st.setProperty('--deskico',deskIconSize()+'px'); }catch(e){}
 /* Only re-render once the desktop has painted for real: a
    re-render during settings load would run the order migration
    against a half-loaded entry set. The boot render picks the
    size up via deskPitch()/the CSS var on its own. */
 if(deskRenderedOnce&&typeof renderDesk==='function') renderDesk();
}
export const settingsReady=loadSettings();

export const __bind_SETTINGS: any = { get(): any { return SETTINGS; }, set(v: any): any { SETTINGS = v; return v; } };

/* gotoSys: jump to a Settings page (open the Settings app, then
   route once its window exists). v0.58: this helper used to exist
   only as a function-local copy inside the menu-bar module's
   buildHelpMenu, while launchProdApp (appsload) and openDeskFile
   (viewers) called it as a page global -- those two paths threw a
   ReferenceError at runtime (install-component toast, and the
   "open with office component" prompt). It lives here now, next
   to the settings store it serves; the menu-bar local copy keeps
   working unchanged (it shadows this one with identical code). */
export function gotoSys(pid: any): void {
  openApp('sys');
  const go = () => { if (window.sysGoto) window.sysGoto(pid); };
  go(); setTimeout(go, 150);
}

export const __bind_deskRenderedOnce: any = { get(): any { return deskRenderedOnce; }, set(v: any): any { deskRenderedOnce = v; return v; } };
