
export function verQ(u){ return u+(u.indexOf('?')>=0?'&':'?')+'v='+APPVER; }
/* Stage Manager strip state (test3): declared HERE, at the
   very top of the block, because updateStageUI() ->
   refreshExtStrip() can run during initial evaluation, long
   before the strip section below executes -- the v0.54 TDZ
   lesson (the same reason FFSTATE is hoisted further down).
   The strip logic itself lives in the extwin section. */
export const thumbBad=new Set();
export const extCards=new Map(); /* hwnd -> Stage card record */
export let extLabelEl=null, extLive=false;
export const APPS=[
 {id:'home',e:'',g:'g-home',glyph:'⌂',n:'工作站',art:1},
 {id:'term',e:'>_',g:'g-term',glyph:'>_',n:'终端',art:1},
 {id:'files',e:'',g:'g-files',glyph:'',n:'文件',art:1},
 {id:'browser',e:'',g:'g-browser',glyph:'',n:'谷歌浏览器'},
 {id:'firefox',e:'',g:'g-firefox',glyph:'',n:'Firefox'},
 {id:'ssh',e:'',g:'g-ssh',glyph:'',n:'SSH',art:1},
 {id:'servermon',e:'',g:'g-servermon',glyph:'',n:'服务器',art:1},
 {id:'console',e:'≡',g:'g-console',glyph:'≡',n:'控制台',art:1},
 {id:'appstore',e:'',g:'g-store',glyph:'',n:'应用商店',art:1},
 {id:'openclaw',e:'',g:'g-openclaw',glyph:'',n:'小龙虾',art:1},
 {id:'core',e:'',g:'g-core',glyph:'',n:'节点',art:1},
 {id:'sys',e:'',g:'g-sys',glyph:'',n:'设置',art:1},
 /* file viewers (v0.32): real windows in the same openApp system
    (so they join the Stage Manager like everything else), but they
    are viewers, not resident apps -- nodesk keeps them off the
    desktop icon grid and the Dock; file icons wake them. */
 {id:'imgview',e:'',g:'g-files',glyph:'',n:'图片',nodesk:true,gkey:'image'},
 {id:'textview',e:'',g:'g-sys',glyph:'',n:'文本',nodesk:true,gkey:'list'},
 {id:'gridview',e:'',g:'g-ssh',glyph:'',n:'表格',nodesk:true,gkey:'grid'},
 {id:'ziplist',e:'',g:'g-files',glyph:'',n:'文件夹',nodesk:true,gkey:'folder'},
 {id:'filepanel',e:'',g:'g-sys',glyph:'',n:'打开文件',nodesk:true,gkey:'file'},
];
export const WALLPAPERS={
 tahoe:{n:'大湖',css:'radial-gradient(1100px 600px at 78% -10%,#ffb35c 0%,transparent 55%),radial-gradient(900px 700px at 12% 110%,#3b1d8f 0%,transparent 60%),linear-gradient(160deg,#1c6dd9 0%,#5b2ee5 52%,#b01e9e 100%)'},
 sonoma:{n:'索诺玛',css:'radial-gradient(900px 550px at 85% 0%,#ffd29d 0%,transparent 55%),radial-gradient(800px 600px at 0% 100%,#7a1e5e 0%,transparent 60%),linear-gradient(150deg,#f2709c 0%,#ff9472 55%,#b24592 100%)'},
 sequoia:{n:'红杉',css:'radial-gradient(900px 650px at 80% 110%,#0f5132 0%,transparent 60%),radial-gradient(700px 500px at 10% -10%,#9be15d22 0%,transparent 50%),linear-gradient(165deg,#06131f 0%,#0b2e2a 60%,#083344 100%)'},
 ventura:{n:'文图拉',css:'radial-gradient(900px 600px at 15% -10%,#f0f 0%,transparent 45%),radial-gradient(900px 700px at 90% 110%,#302b63 0%,transparent 60%),linear-gradient(140deg,#6441a5 0%,#2a0845 90%)'},
 aurora:{n:'极光',css:'radial-gradient(1000px 500px at 60% -20%,#43e97b55 0%,transparent 55%),radial-gradient(800px 600px at 20% 120%,#38f9d755 0%,transparent 55%),linear-gradient(170deg,#050a1f 0%,#0b2740 55%,#12082e 100%)'},
 graphite:{n:'石墨',css:'radial-gradient(900px 600px at 80% 0%,#ffffff14 0%,transparent 50%),linear-gradient(160deg,#232329 0%,#101014 70%,#050507 100%)'},
};
export const winsEl=document.getElementById('wins'), iconsEl=document.getElementById('icons'), dockEl=document.getElementById('dock'), wallEl=document.getElementById('wall');
export let z=10, sysInfo=null, customWall=false;

export function esc(s){return String(s??'').replace(/[&<>"]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]))}
export async function api(p: any, opt?: any){const r=await fetch(p,opt);let t=await r.text();try{return JSON.parse(t)}catch(e){return{raw:t}}}

export const __bind_sysInfo: any = { get(): any { return sysInfo; }, set(v: any): any { sysInfo = v; return v; } };

export const __bind_extLabelEl: any = { get(): any { return extLabelEl; }, set(v: any): any { extLabelEl = v; return v; } };

export const __bind_extLive: any = { get(): any { return extLive; }, set(v: any): any { extLive = v; return v; } };

export const __bind_z: any = { get(): any { return z; }, set(v: any): any { z = v; return v; } };

export const __bind_customWall: any = { get(): any { return customWall; }, set(v: any): any { customWall = v; return v; } };
