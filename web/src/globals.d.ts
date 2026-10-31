/* Ambient page globals (v0.58 modularization).
 *
 * The page still loads as THREE classic script blocks (now three
 * esbuild bundles). Inside a bundle, cross-module names are real
 * ES imports; ACROSS bundles, the later bundles keep reading the
 * first bundle's names as bare page globals -- exactly how the
 * three hand-written script blocks shared one global scope. Those
 * names are declared here so tsc can check the consumers. Bundle 1
 * publishes them at runtime through web/src/b1/expose.ts (live
 * accessor properties on window); its own modules never use these
 * declarations -- they import the real bindings, which shadow the
 * ambient ones.
 *
 * The pure-function files under web/src/pure are NOT declared
 * here: they are part of the program as global scripts, so their
 * declarations are the ambient ones.
 */
declare function esc(...args: any[]): any;
declare const APPS: any;
declare var lastApp: any;
declare var openApp: any;
declare function newFolder(...args: any[]): any;
declare function toast(...args: any[]): any;
declare var SETTINGS: any;
declare const SETDEF: any;
declare function saveSetting(...args: any[]): any;
declare function stageOn(...args: any[]): any;
declare function toggleStage(...args: any[]): any;
declare function dockAutoHideOn(...args: any[]): any;
declare function applyDockLayout(...args: any[]): any;
declare function zoomWin(...args: any[]): any;
declare function api(...args: any[]): any;
declare var frontApp: any;
declare var sysInfo: any;
declare function folderedKeys(...args: any[]): any;
declare function appIcon(...args: any[]): any;
declare function launchFirefoxEntry(...args: any[]): any;
declare var WEBAPPS: any;
declare function webIconHTML(...args: any[]): any;
declare var openWebApp: any;
declare var LAYOUT: any;
declare function folderIconHTML(...args: any[]): any;
declare function openFolderPop(...args: any[]): any;
declare var STOREAPPS: any;
declare function storeIconHTML(...args: any[]): any;
declare var launchStoreApp: any;
declare var BOXAPPS: any;
declare var launchBoxApp: any;
declare var PRODAPPS: any;
declare function prodIconHTML(...args: any[]): any;
declare var launchProdApp: any;
declare var DESKORDER: any;
declare var suppressClickUntil: any;
declare function dragMovedArray(...args: any[]): any;
declare function persistDeskPos(...args: any[]): any;
declare function renderDesk(...args: any[]): any;
declare const dockEl: any;
declare var DESKFILES: any;
declare function fileIconHTML(...args: any[]): any;
declare var openDeskFile: any;
declare function glyph(...args: any[]): any;
declare function pollNode(...args: any[]): any;
declare function loadDeskFiles(...args: any[]): any;
declare function saveLayout(...args: any[]): any;
declare const ctxEl: any;
declare function hideCtx(...args: any[]): any;
declare function exportDeskFile(...args: any[]): any;
declare function showFileCtx(...args: any[]): any;

/* APPVER is NOT TypeScript: it is a verbatim line in web/shell.html
   at the top of script block 1, because the daemon substitutes its
   __MJ_APPVER__ placeholder byte-exact at serve time (main.go) and
   the Go test pins its single-quoted shape. verQ() in b1/core.ts
   spends it as the cache-busting token on asset URLs. */
declare const APPVER: string;

/* The SSH window loads xterm.js from the daemon at runtime. */
declare const Terminal: any;
declare const FitAddon: any;

/* The page's DOM code is dynamically typed legacy code: elements
   flow around as Element/HTMLElement/EventTarget and gain ad-hoc
   properties. Index signatures keep tsc focused on what this
   migration bought it for -- undefined names, scope and module
   wiring -- instead of re-litigating DOM property types the
   fake-DOM suites already cover behaviourally. */
interface Element { [key: string]: any; }
interface HTMLElement { [key: string]: any; }
interface EventTarget { [key: string]: any; }
interface Node { [key: string]: any; }

/* Cross-bundle handles the page code deliberately parks on
   window (the pre-modular page did the same), plus the wrapped
   openApp slot and the settings mirror bundle 3 reads. */
interface Window {
  __vault: any;
  __vaultShowLock: any;
  __filesRefresh: any;
  __filesReveal: any;
  __bindFiles: any;
  __mjFolderImport: any;
  __lpClose: any;
  __mjSetPage: any;
  __dockMenuOpen: any;
  sysRefresh: any;
  sysGoto: any;
  openApp: any;
  SETTINGS: any;
  Terminal: any;
}
