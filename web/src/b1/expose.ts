/* Page-global exposure (v0.58): in the pre-modular page these
   names were top-level declarations of the first script block --
   i.e. real page globals. Two later script blocks (now bundles 2
   and 3), the string-built inline handlers (onclick="openApp(...)"),
   and the node boot suites still reach them as bare globals, so
   bundle 1 re-publishes exactly the names they consume -- no more.
   Mutable bindings go through the owner's __bind accessor so a
   later bundle REPLACING one (the recents/files wrappers replace
   window.openApp and five launch functions) updates the very
   binding bundle 1 itself calls, exactly like the classic page. */
import { APPS, api, dockEl, esc, iconsEl, thumbBad } from './core';
import { GLYPHS, glyph } from './glyphs';
import { SETDEF, deskPitch, dockAutoHideOn, gotoSys, saveSetting } from './settings';
import { pollNode } from './menubar';
import { MJEvents } from './events';
import { appIcon, zoomWin } from './windows';
import { stageOn, toggleStage } from './stage';
import { extCardHTML } from './extstrip';
import { cleanWinTitle, entryMeta, folderIconHTML, folderItems, folderedKeys, saveLayout, sortedDeskOrder, webIconHTML } from './entries';
import { persistDeskPos, renderDesk } from './desktop';
import { dragMovedArray } from './drag';
import { ctxEl, hideCtx, openFolderPop } from './folders';
import { ctxItemHTML, newFolder, showItemCtx } from './ctxmenu';
import { exportDeskFile, fileIconHTML, launchFirefoxEntry, loadDeskFiles, prodIconHTML, showFileCtx, storeIconHTML, toast } from './appsload';
import { applyDockLayout } from './dock';
import { ccTile } from './ccnc';
import { __bind_sysInfo } from './core';
import { __bind_SETTINGS } from './settings';
import { __bind_openApp } from './windows';
import { __bind_frontApp, __bind_lastApp } from './stage';
import { __bind_LAYOUT, __bind_WEBAPPS, __bind_suppressClickUntil } from './entries';
import { __bind_DESKORDER } from './desktop';
import { __bind_BOXAPPS, __bind_DESKFILES, __bind_PRODAPPS, __bind_STOREAPPS, __bind_launchBoxApp, __bind_launchProdApp, __bind_launchStoreApp } from './appsload';
import { __bind_openDeskFile, __bind_openWebApp } from './viewers';
function pub(name: string, d: PropertyDescriptor): void { Object.defineProperty(window, name, Object.assign({ configurable: true, enumerable: true }, d)); }
pub('APPS', { get: () => APPS });
pub('api', { get: () => api });
pub('MJEvents', { get: () => MJEvents });
pub('dockEl', { get: () => dockEl });
pub('esc', { get: () => esc });
pub('iconsEl', { get: () => iconsEl });
pub('thumbBad', { get: () => thumbBad });
pub('GLYPHS', { get: () => GLYPHS });
pub('glyph', { get: () => glyph });
pub('SETDEF', { get: () => SETDEF });
pub('deskPitch', { get: () => deskPitch });
pub('dockAutoHideOn', { get: () => dockAutoHideOn });
pub('gotoSys', { get: () => gotoSys });
pub('saveSetting', { get: () => saveSetting });
pub('pollNode', { get: () => pollNode });
pub('appIcon', { get: () => appIcon });
pub('zoomWin', { get: () => zoomWin });
pub('stageOn', { get: () => stageOn });
pub('toggleStage', { get: () => toggleStage });
pub('extCardHTML', { get: () => extCardHTML });
pub('cleanWinTitle', { get: () => cleanWinTitle });
pub('entryMeta', { get: () => entryMeta });
pub('folderIconHTML', { get: () => folderIconHTML });
pub('folderItems', { get: () => folderItems });
pub('folderedKeys', { get: () => folderedKeys });
pub('saveLayout', { get: () => saveLayout });
pub('sortedDeskOrder', { get: () => sortedDeskOrder });
pub('webIconHTML', { get: () => webIconHTML });
pub('persistDeskPos', { get: () => persistDeskPos });
pub('renderDesk', { get: () => renderDesk });
pub('dragMovedArray', { get: () => dragMovedArray });
pub('ctxEl', { get: () => ctxEl });
pub('hideCtx', { get: () => hideCtx });
pub('openFolderPop', { get: () => openFolderPop });
pub('ctxItemHTML', { get: () => ctxItemHTML });
pub('newFolder', { get: () => newFolder });
pub('showItemCtx', { get: () => showItemCtx });
pub('exportDeskFile', { get: () => exportDeskFile });
pub('fileIconHTML', { get: () => fileIconHTML });
pub('launchFirefoxEntry', { get: () => launchFirefoxEntry });
pub('loadDeskFiles', { get: () => loadDeskFiles });
pub('prodIconHTML', { get: () => prodIconHTML });
pub('showFileCtx', { get: () => showFileCtx });
pub('storeIconHTML', { get: () => storeIconHTML });
pub('toast', { get: () => toast });
pub('applyDockLayout', { get: () => applyDockLayout });
pub('ccTile', { get: () => ccTile });
pub('sysInfo', { get: __bind_sysInfo.get, set: __bind_sysInfo.set });
pub('SETTINGS', { get: __bind_SETTINGS.get, set: __bind_SETTINGS.set });
pub('openApp', { get: __bind_openApp.get, set: __bind_openApp.set });
pub('frontApp', { get: __bind_frontApp.get, set: __bind_frontApp.set });
pub('lastApp', { get: __bind_lastApp.get, set: __bind_lastApp.set });
pub('LAYOUT', { get: __bind_LAYOUT.get, set: __bind_LAYOUT.set });
pub('WEBAPPS', { get: __bind_WEBAPPS.get, set: __bind_WEBAPPS.set });
pub('suppressClickUntil', { get: __bind_suppressClickUntil.get, set: __bind_suppressClickUntil.set });
pub('DESKORDER', { get: __bind_DESKORDER.get, set: __bind_DESKORDER.set });
pub('BOXAPPS', { get: __bind_BOXAPPS.get, set: __bind_BOXAPPS.set });
pub('DESKFILES', { get: __bind_DESKFILES.get, set: __bind_DESKFILES.set });
pub('PRODAPPS', { get: __bind_PRODAPPS.get, set: __bind_PRODAPPS.set });
pub('STOREAPPS', { get: __bind_STOREAPPS.get, set: __bind_STOREAPPS.set });
pub('launchBoxApp', { get: __bind_launchBoxApp.get, set: __bind_launchBoxApp.set });
pub('launchProdApp', { get: __bind_launchProdApp.get, set: __bind_launchProdApp.set });
pub('launchStoreApp', { get: __bind_launchStoreApp.get, set: __bind_launchStoreApp.set });
pub('openDeskFile', { get: __bind_openDeskFile.get, set: __bind_openDeskFile.set });
pub('openWebApp', { get: __bind_openWebApp.get, set: __bind_openWebApp.set });
