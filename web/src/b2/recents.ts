/* Generated cross-module imports (v0.58 modularization):
   every name below is owned by the sibling module it is imported
   from -- the same names the pre-modular page shared through one
   script scope. */
import { SPOT_RECENT_KEY } from './b2core';
import { $50 } from './fvhead';
import { paintFiles } from './filesview';
/* ----- recents （最近打开）: one ledger for Spotlight, fed by
   wrapping the same open entry points the desktop uses, so a
   launch from anywhere (desktop, Dock, Launchpad, menus) counts
   exactly like a desktop click. ----- */
export function spotRecentGet(){ try{ const a=JSON.parse(localStorage.getItem(SPOT_RECENT_KEY)||'[]'); return Array.isArray(a)?a:[]; }catch(e){ return []; } }
export function spotRecentPut(key){ if(!key) return; const a=spotRecentGet().filter(k=>k!==key); a.unshift(key); try{ localStorage.setItem(SPOT_RECENT_KEY,JSON.stringify(a.slice(0,12))); }catch(e){} }
export function recWrap(fname: any,keyOf: any){ const orig=(window as any)[fname]; if(typeof orig!=='function') return; (window as any)[fname]=function(){ const k=keyOf.apply(null,arguments); if(k) spotRecentPut(k); return orig.apply(this,arguments); }; }
recWrap('openApp',id=>'app:'+id);
recWrap('openDeskFile',p=>'file:'+p);
recWrap('openWebApp',u=>'web:'+u);
recWrap('launchStoreApp',id=>'store:'+id);
recWrap('launchBoxApp',id=>'box:'+id);
recWrap('launchProdApp',id=>'prod:'+id);
/* Reopening the Files app repaints the Finder (data may have
   changed while it sat hidden). */
(function(){ const orig=window.openApp; window.openApp=function(id){ const r=orig.apply(this,arguments); if(id==='files'&&$50('fv-body')) paintFiles(); return r; }; })();
