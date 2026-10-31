
export const $50=id=>document.getElementById(id);
export function vaultLockedNow(){ return !!(window.__vault&&window.__vault.locked); }
export function fvFmtSize(n){ n=+n||0; if(n>=1073741824) return (n/1073741824).toFixed(2)+' GB'; if(n>=1048576) return (n/1048576).toFixed(1)+' MB'; if(n>=1024) return (n/1024).toFixed(1)+' KB'; return n+' B'; }
export function fvFmtTime(ms){ if(!ms) return '—'; const d=new Date(ms); return d.toLocaleDateString('zh-CN')+' '+d.toLocaleTimeString('zh-CN',{hour:'2-digit',minute:'2-digit',hour12:false}); }
export function fvKindName(kind){ return {image:'图片',pdf:'PDF 文档',text:'文本',csv:'表格',docx:'Word 文档',xlsx:'Excel 表格',pptx:'PPT 演示',zip:'压缩包',exe:'程序',dir:'文件夹',other:'文件'}[kind]||'文件'; }
/* v0.54: the kind label for a FILE (media classify by extension,
   the daemon kind stays 'other' for them). */
export function fvKindLabel(f){ const mg=fvMediaGroup(f); if(mg==='music') return '音乐'; if(mg==='video') return '视频'; return fvKindName(f&&f.kind); }
