/* Generated cross-module imports (v0.58 modularization):
   every name below is owned by the sibling module it is imported
   from -- the same names the pre-modular page shared through one
   script scope. */
import { settingsReady } from './settings';
import { openApp } from './windows';
import { restoreSession } from './session';
import { loadLayout } from './entries';
import { renderDesk } from './desktop';
import { loadWebApps } from './ctxmenu';
import { loadBoxApps, loadDeskFiles, loadFFState, loadProdApps, loadStoreApps } from './appsload';

openApp('home');
(async()=>{ try{ await Promise.allSettled([loadLayout(),loadWebApps(),loadStoreApps(),loadBoxApps(),loadProdApps(),loadFFState(),loadDeskFiles(),settingsReady]); renderDesk(); restoreSession(); }catch(e){ console.error('[mj-boot] 启动序列失败',e); } })();