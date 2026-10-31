/* Generated cross-module imports (v0.58 modularization):
   every name below is owned by the sibling module it is imported
   from -- the same names the pre-modular page shared through one
   script scope. */
import { api, esc } from './core';
import { glyph } from './glyphs';
import { gotoSys } from './settings';
import { openApp } from './windows';
import { renderDesk } from './desktop';
import { DESKFILES, SUBLIST, exportDeskFile, loadDeskFiles, setWinTitle, toast, __bind_SUBLIST } from './appsload';
/* ----- open routing: everything resolves inside the system ----- */
export let openDeskFile: any = async function openDeskFile(p,kind){
 const name=p.split('/').pop();
 /* v0.51: installed productivity components get first claim on
    the kinds they own (office docs -> the office suite chosen
    in 设置, images -> ImageGlass, text/code -> VSCodium). The
    daemon decides from what is actually installed. "install"
    means an office file with no suite yet: guide the user to
    the component download, never fail silently; declining falls
    through to the built-in viewers below. */
 if(kind!=='pdf'&&kind!=='zip'&&kind!=='exe'&&kind!=='dir'){
  const cr=await api('/api/components/openfile',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:p})}).catch(()=>null);
  if(cr&&cr.error){ alert(cr.error); return; }
  if(cr&&cr.action==='launched') return;
  if(cr&&cr.action==='install'){
   if(confirm('打开「'+name+'」需要先装办公组件（'+cr.name+'）。\n现在去「设置 → 组件」下载吗？包不小，装好以后这类文件都用它打开。')){
    gotoSys('components'); return;
   }
  }
 }
 if(kind==='image') return openImageViewer(p);
 if(kind==='text'||kind==='docx') return openTextViewer(p);
 if(kind==='csv') return openGridViewer(p);
 if(kind==='dir') return openZipList(p);
 if(kind==='pdf'){
  const r=await api('/api/files/openpdf',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:p})}).catch(()=>null);
  if(r&&r.error) alert(r.error);
  return;
 }
 if(kind==='zip'){
  if(!confirm('解压「'+name+'」？\n会在终端的 Data/files 里生成一个同名文件夹，桌面上多一个文件夹图标。')) return;
  const r=await api('/api/files/unzip',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:p})}).catch(()=>null);
  if(!r||r.error){ alert('解压失败：'+((r&&r.error)||'接口没回应')); return; }
  toast('已解压到文件夹「'+r.dir+'」（'+r.count+' 个文件）');
  await loadDeskFiles(); renderDesk();
  return;
 }
 if(kind==='exe'){
  /* Deliberate friction: an imported program really runs on this
     PC. Nothing happens unless the user confirms here. */
  if(!confirm('运行「'+name+'」？\n这是从电脑导入的可执行文件，确认后会真的在这台电脑上运行它。')) return;
  const r=await api('/api/files/run',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:p})}).catch(()=>null);
  if(r&&r.error) alert(r.error);
  return;
 }
 openFilePanel(p,kind); /* xlsx / pptx / unknown */
};
/* image viewer: siblings are the other images in the same place */
export let IMGV={cur:'',fit:true};
export function imageSiblings(p){
 const pool=p.includes('/')?SUBLIST:DESKFILES;
 return pool.filter(f=>f.kind==='image').map(f=>f.path);
}
export function openImageViewer(p){
 openApp('imgview'); setWinTitle('imgview',p.split('/').pop());
 IMGV.cur=p; paintImageViewer();
}
export function paintImageViewer(){
 const img=document.getElementById('iv-img'); if(!img) return;
 const sib=imageSiblings(IMGV.cur);
 const i=sib.indexOf(IMGV.cur);
 document.getElementById('iv-name').textContent=IMGV.cur.split('/').pop()+(sib.length>1?(' · '+(i+1)+' / '+sib.length):'');
 img.classList.toggle('full',!IMGV.fit);
 img.src='/api/files/raw?path='+encodeURIComponent(IMGV.cur);
 const pv=document.getElementById('iv-prev'), nx=document.getElementById('iv-next');
 if(pv) pv.disabled=sib.length<2; if(nx) nx.disabled=sib.length<2;
}
export function stepImage(d){
 const sib=imageSiblings(IMGV.cur); if(sib.length<2) return;
 let i=sib.indexOf(IMGV.cur); i=(i+d+sib.length)%sib.length;
 IMGV.cur=sib[i]; setWinTitle('imgview',IMGV.cur.split('/').pop()); paintImageViewer();
}
export async function openTextViewer(p){
 openApp('textview'); setWinTitle('textview',p.split('/').pop());
 const pre=document.getElementById('tv-pre'); if(pre) pre.textContent='读取中…';
 const r=await api('/api/files/text?path='+encodeURIComponent(p)).catch(()=>null);
 if(!pre) return;
 if(!r||r.error){ pre.textContent='打开失败：'+((r&&r.error)||'接口没回应'); return; }
 document.getElementById('tv-name').textContent=r.name;
 document.getElementById('tv-note').textContent=(r.kind==='docx'?'文档预览（仅文字）':'')+(r.truncated?' · 仅显示前 2 MB':'');
 pre.textContent=r.text||'（空文件）';
}
export async function openGridViewer(p){
 openApp('gridview'); setWinTitle('gridview',p.split('/').pop());
 const tab=document.getElementById('gv-tab'); if(tab) tab.innerHTML='';
 const note=document.getElementById('gv-note'); if(note) note.textContent='读取中…';
 const r=await api('/api/files/csv?path='+encodeURIComponent(p)).catch(()=>null);
 if(!tab) return;
 if(!r||r.error){ if(note) note.textContent='打开失败：'+((r&&r.error)||'接口没回应'); return; }
 const rows=r.rows||[];
 tab.innerHTML=rows.map((row,i)=>'<tr>'+row.map(c=>i===0?`<th>${esc(c)}</th>`:`<td>${esc(c)}</td>`).join('')+'</tr>').join('')||'<tr><td>（空表格）</td></tr>';
 if(note) note.textContent=r.name+' · 共显示 '+rows.length+' 行'+(r.truncatedRows?'（文件更长，只显示前 200 行）':'')+(r.truncatedCols?' · 列数已截到前 50 列':'');
}
export async function openZipList(p){
 openApp('ziplist'); setWinTitle('ziplist',p.split('/').pop());
 const box=document.getElementById('zl-list'); if(box) box.innerHTML='加载中…';
 const r=await api('/api/files/sub?path='+encodeURIComponent(p)).catch(()=>null);
 if(!box) return;
 if(!r||r.error){ box.textContent='打开失败：'+((r&&r.error)||'接口没回应'); return; }
 __bind_SUBLIST.set(r.files||[]);
 box.innerHTML=SUBLIST.length?`<table>${SUBLIST.map(it=>`<tr class="file" data-p="${esc(it.path)}" data-k="${it.kind}" data-d="${it.dir}"><td>${glyph(it.dir?'folder':'file',15)}</td><td>${esc(it.name)}</td><td>${it.dir?'':(it.size/1024).toFixed(1)+' KB'}</td></tr>`).join('')}</table>`:'（空文件夹）';
 box.querySelectorAll('tr.file').forEach(tr=>tr.ondblclick=()=>{
  if(tr.dataset.d==='true'){ toast('子文件夹先不在这里展开（它在 Data/files 里）'); return; }
  openDeskFile(tr.dataset.p,tr.dataset.k);
 });
}
export function openFilePanel(p,kind){
 openApp('filepanel');
 const name=p.split('/').pop();
 setWinTitle('filepanel',name);
 const office=(kind==='xlsx'||kind==='pptx');
 document.getElementById('fp2-name').textContent=name;
 document.getElementById('fp2-desc').textContent=office
  ? '这个格式（'+(kind==='xlsx'?'Excel 表格':'PowerPoint 演示')+'）系统内暂时不能直接编辑。两个诚实的办法：传到网页版办公（腾讯文档）里打开编辑，或者导出到这台电脑、用电脑上的 Office 打开。'
  : '这个格式系统里还没有打开方式。可以先导出到这台电脑，用电脑上的软件打开。';
 document.getElementById('fp2-web').style.display=office?'':'none';
 document.getElementById('fp2-web').onclick=()=>openWebApp('https://docs.qq.com/desktop');
 document.getElementById('fp2-exp').onclick=ev=>{ ev.preventDefault(); exportDeskFile({path:p,name}); };
}

export let openWebApp: any = async function openWebApp(url){
 const r=await api('/api/appopen',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({url})}).catch(()=>null);
 if(r&&r.ok) return;
 if(r&&r.error) return; /* real error (no browser / bad url / test env): nothing more to do from a desktop icon */
 /* old daemon without /api/appopen answers non-JSON: open as a normal link */
 api('/api/open',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({url})});
};

export const __bind_openDeskFile: any = { get(): any { return openDeskFile; }, set(v: any): any { openDeskFile = v; return v; } };

export const __bind_openWebApp: any = { get(): any { return openWebApp; }, set(v: any): any { openWebApp = v; return v; } };

export const __bind_IMGV: any = { get(): any { return IMGV; }, set(v: any): any { IMGV = v; return v; } };
