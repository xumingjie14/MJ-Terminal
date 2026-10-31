/* Generated cross-module imports (v0.58 modularization):
   every name below is owned by the sibling module it is imported
   from -- the same names the pre-modular page shared through one
   script scope. */
import { WALLPAPERS, api, customWall, esc, sysInfo, __bind_customWall, __bind_sysInfo } from './core';
import { glyph } from './glyphs';
import { SETDEF, SETTINGS, applyDeskIconSize, deskIconSize, dockPos, loadSettings, saveSetting, wallKey, __bind_SETTINGS } from './settings';
import { applyWallpaper, pollNode } from './menubar';
import { openApp } from './windows';
import { stageOn, toggleStage } from './stage';
import { refreshExtStrip } from './extstrip';
import { FFSTATE, __bind_FFSTATE } from './session';
import { WEBAPPS, deskSortMode, setDeskSort, webIconHTML } from './entries';
import { renderDesk } from './desktop';
import { loadWebApps } from './ctxmenu';
import { PRODAPPS, STOREAPPS, launchBoxApp, launchStoreApp, loadBoxApps, loadProdApps, loadStoreApps, prodBusy, prodPct, storeIconHTML, toast } from './appsload';
import { IMGV, openWebApp, paintImageViewer, stepImage } from './viewers';
import { applyDockLayout } from './dock';
/* ----- app contents (same real functions as before) ----- */
export function render(id){
 const b=document.getElementById('body-'+id);
 if(id==='home') b.innerHTML=`<h2 style="margin-bottom:8px">铭杰工作站</h2><p class="mut">真功能版：终端执行的是这台电脑的真实命令，文件打开的是真实目录，浏览器是独立档案的 Chrome，节点在后台常驻。<br>关掉桌面窗口只是隐藏，后台和节点继续跑；再双击 EXE 就能回来，彻底退出在「设置」里。</p><p style="margin-top:14px"><span class="tag" id="home-sys">正在读取系统信息…</span></p><div class="row" style="margin-top:16px"><button class="btn" onclick="openApp('term')">打开终端</button><button class="btn gray" onclick="openApp('files')">打开文件</button><button class="btn gray" onclick="openApp('sys')">换壁纸</button></div>`;
 if(id==='term') b.innerHTML=`<pre class="term" id="tout">铭杰终端（真实命令）\r\n试试：dir、ipconfig、ping 223.5.5.5、systeminfo、tasklist\r\n</pre><div class="row" style="margin-top:10px"><span id="prompt" class="tag">…</span><input class="txt" id="tin" placeholder="输入命令，回车执行" autocomplete="off"><button class="btn" id="trun">执行</button></div><div class="row" style="margin-top:8px"><button class="btn gray" data-cmd="systeminfo">系统信息</button><button class="btn gray" data-cmd="ipconfig">IP 配置</button><button class="btn gray" data-cmd="dir">当前目录</button><button class="btn gray" data-cmd="tasklist">进程</button></div><p style="margin-top:8px"><span class="tag" id="term-accel">节点加速：…</span></p>`;
 if(id==='files'){ b.style.padding='0';
  /* v0.50 Finder face (bind in the v0.50 script): sidebar smart
     groups / toolbar / icon-list-column views / preview pane,
     all over the SAME Data/files library and open routing the
     desktop icons use. The window title area carries the honest
     virtual path: 文稿库 is a single-layer library. */
  b.innerHTML=`<div class="fv"><div class="fv-path">文稿库 <span style="opacity:.6">›</span> <b id="fv-crumb">全部文件</b><span style="opacity:.55;font-size:12px">　· 单层文稿库（Data/files），不是电脑上的文件夹</span></div><div class="fv-main"><div class="fv-side" id="fv-side"></div><div class="fv-center"><div class="fv-tools"><span class="seg" id="fv-viewseg"><button data-v="icon">图标</button><button data-v="list">列表</button><button data-v="cols">分栏</button></span><select class="fv-sorts" id="fv-sort" title="排序"><option value="name">名称</option><option value="size">大小</option><option value="kind">类型</option><option value="mtime">导入时间</option></select><button class="btn gray" id="fv-import">导入…</button><input class="fv-q" id="fv-q" type="text" placeholder="搜索当前分组" autocomplete="off"></div><div class="fv-body" id="fv-body"></div></div><div class="fv-prev" id="fv-prev"></div></div></div>`;
 }
 if(id==='browser') b.innerHTML=`<h3>谷歌浏览器（独立窗口）</h3><p class="mut" style="margin-top:8px">这个浏览器和公用电脑自己的浏览器是分开的：独立档案存在 EXE 旁边的 Data 文件夹，历史、Cookie、登录都在你自己的档案里。<br><b style="color:#fff">走节点打开时，只有这个窗口的流量走节点</b>，不会改这台电脑的系统代理。</p><div class="row" style="margin-top:14px"><input class="txt" id="burl" value="https://www.google.com"></div><div class="row" style="margin-top:10px"><button class="btn" id="bproxy">走节点打开 Chrome</button><button class="btn gray" id="bdirect">直连打开 Chrome</button><button class="btn gray" id="btest">代理自检</button></div><p style="margin-top:12px"><span class="tag" id="bstatus">连不上时先点「代理自检」，它会告诉你卡在哪一步</span></p><hr style="border-color:rgba(255,255,255,.14);margin:18px 0"><h3>网站应用（钉到桌面）</h3><p class="mut" style="margin-top:6px">下面的网站已经钉在桌面右侧，点开就是<b style="color:#fff">应用化的干净窗口</b>：没有地址栏、没有标签页，像打开一个 App，自动走节点（全局加速开着时）。适合 X、YouTube 这种固定要去的网站。</p><div id="walist" class="mut" style="margin-top:10px">加载中…</div><div class="row" style="margin-top:12px"><input class="txt" id="waname" placeholder="名字，如 GitHub" style="max-width:150px"><input class="txt" id="waurl" placeholder="网址，如 https://github.com"></div><div class="row" style="margin-top:8px"><button class="btn" id="waadd">＋ 钉到桌面</button><span class="tag" id="wastatus"></span></div><hr style="border-color:rgba(255,255,255,.14);margin:18px 0"><h3>谷歌云同步（换电脑不丢登录）</h3><p class="mut" style="margin-top:6px">Chrome 的登录搬不了家（被那台电脑的 Windows 账户锁着），但<b style="color:#fff">开启谷歌同步</b>后，换一台电脑打开终端机，在隔离 Chrome 里登一次谷歌，密码、书签、历史就会同步回来，各网站的登录基本点一下就恢复。同步的开关在 Chrome 里面，下面这个按钮会直接把同步设置页在隔离 Chrome 中打开。</p><div class="row" style="margin-top:10px"><button class="btn" id="bsync">打开同步设置</button><span class="tag" id="bsyncst">打开后在 Chrome 里点「开启同步」并登录谷歌账户</span></div>`;
 if(id==='firefox') b.innerHTML=`<h3>Firefox（系统内浏览器 · 登录可搬家）</h3><p class="mut" style="margin-top:8px">这是装在咱们系统里面的 Firefox，用自己的档案（EXE 旁 Data/FirefoxProfile）。和 Chrome 不一样，<b style="color:#fff">Firefox 的登录、Cookie、历史都锁在档案自己身上</b>：把文件夹带到另一台电脑打开，登录状态样，这是专门用来装需要登录的网站的浏览器。建议进 Firefox 后给档案设一个<b style="color:#fff">主密码</b>（设置 → 隐私与安全 → 主密码），文件夹在别人手里也解不开。宿主机自己的 Firefox 与它互不相干。</p><p style="margin-top:12px"><span class="tag" id="ffinfo">组件状态读取中…</span></p><div class="row" style="margin-top:10px"><button class="btn" id="ffdl">下载 Firefox 组件</button><span class="tag" id="ffprog"></span></div><div class="row" style="margin-top:14px"><input class="txt" id="ffurl" placeholder="网址（留空开主页），如 https://github.com"></div><div class="row" style="margin-top:10px"><button class="btn" id="ffopen">走节点打开</button><button class="btn gray" id="ffopenapp">应用化打开（走节点）</button><button class="btn gray" id="ffopend">直连打开</button></div><div id="ffconflict" style="display:none;margin-top:12px;padding:10px 12px;border-radius:10px;background:rgba(255,159,10,.14);border:1px solid rgba(255,159,10,.45)"><p style="font-size:13px;line-height:1.6" id="ffconflict-t"></p><div class="row" style="margin-top:8px"><button class="btn" id="ffrestart">重启并切换</button><button class="btn" id="ffforce" style="display:none;background:#ff453a">强制结束并切换</button><span class="mut" style="font-size:12px">会关掉现在的 Firefox 窗口再重开；标签页按 Firefox 的会话恢复回来。强制结束只在温和关闭退不干净时才出现：它会结束本组件的 Firefox 进程树（按进程路径认人，不碰宿主机的 Firefox），未保存的表单内容可能丢失。</span></div></div><p class="mut" style="margin-top:10px">应用化＝隐藏标签栏和地址栏的干净窗口。Firefox 是单进程浏览器：<b style="color:#fff">普通/应用化在进程启动那一刻就定死了</b>，开着的时候再点另一种打开不会生效——页面会提示冲突，点「重启并切换」一键处理。</p><p style="margin-top:8px"><span class="tag" id="ffstatus"></span></p>`;
 if(id==='ssh') b.innerHTML=`<h3>SSH 远程终端</h3><p class="mut" style="margin-top:6px">连接、私钥、服务器指纹都存在 EXE 旁的 <b style="color:#fff">Data/ssh</b> 里，跟着文件夹走。密码和私钥口令只在连接时输入、<b style="color:#fff">不保存</b>。第一次连某台服务器会记下它的指纹，之后每次校验，对不上会拦住。</p><div class="row" style="margin-top:10px"><select id="sshconn" class="txt" style="max-width:250px"></select><button class="btn gray" id="sshnew">＋ 新建</button><button class="btn gray" id="sshedit">编辑</button><button class="btn gray" id="sshdel">删除</button></div><div id="sshform" style="display:none;margin-top:10px"><div class="row"><input class="txt" id="sf_name" placeholder="名字，如 新加坡服务器" style="max-width:170px"><input class="txt" id="sf_host" placeholder="主机 / IP"><input class="txt" id="sf_port" placeholder="端口" value="22" style="max-width:80px"><input class="txt" id="sf_user" placeholder="用户名" style="max-width:120px"></div><div class="row" style="margin-top:8px"><label class="mut">登录方式 <select id="sf_auth" class="txt"><option value="key">密钥</option><option value="password">密码</option></select></label><select id="sf_key" class="txt" style="max-width:170px"></select><label class="mut"><input type="checkbox" id="sf_proxy"> 走节点连接</label></div><div class="row" style="margin-top:8px"><input class="txt" id="sf_keyname" placeholder="新密钥名（导入时填，如 id_sg）" style="max-width:210px"></div><textarea id="sf_keydata" rows="3" style="width:100%;margin-top:6px" placeholder="粘贴私钥内容导入（可选，存进 Data/ssh/keys；已存的密钥在上面选）"></textarea><div class="row" style="margin-top:8px"><button class="btn" id="sfsave">保存连接</button><span class="tag" id="sfst"></span></div></div><div class="row" style="margin-top:12px"><input class="txt" id="sshpass" type="password" placeholder="密码 / 私钥口令（连接时输，不保存）" style="max-width:250px" autocomplete="off"><button class="btn" id="sshgo">连接</button><button class="btn gray" id="sshstop">断开</button><span class="tag" id="sshst">未连接</span></div><div id="sshterm" style="margin-top:10px;height:300px;background:#0b0e14;border-radius:10px;padding:6px;overflow:hidden"></div><hr style="border-color:rgba(255,255,255,.14);margin:16px 0"><h3>文件（SFTP）</h3><p class="mut" style="margin-top:4px">共用上面的终端连接传文件，不用再输一次密码。</p><div class="row" style="margin-top:8px"><span class="tag" id="sfpath">未连接</span><button class="btn gray" id="sfup">↑ 上级</button><button class="btn gray" id="sfrefresh">刷新</button><button class="btn gray" id="sfmkdir">新建文件夹</button><button class="btn gray" id="sfupload">上传文件</button><input type="file" id="sffile" style="display:none"></div><div id="sflist" class="mut" style="margin-top:8px">连上服务器后，这里能浏览、上传、下载、删除远程文件。</div>`;
 if(id==='servermon') b.innerHTML=`<div class="sm-wrap"><div class="sm-side"><div class="sm-hd">服务器</div><div id="sm-list"></div><p class="mut" style="font-size:11.5px;padding:10px 8px 0;line-height:1.6">监控走 SSH 通道读服务器数据，不用在服务器上装任何东西。密钥连接可自动采样；密码连接先在 SSH 里连上，或在详情里输一次密码手动采样。</p></div><div class="sm-main" id="sm-main"><p class="mut">加载中…</p></div></div>`;
 if(id==='console'){ b.style.padding='0'; b.style.background='#0b0e14'; b.style.display='flex'; b.style.flexDirection='column';
 b.innerHTML=`<div style="padding:12px 14px 10px;display:flex;gap:8px;flex-wrap:wrap;align-items:center;border-bottom:1px solid rgba(255,255,255,.08)"><span class="cschip">版本 <b id="cs-ver">…</b></span><span class="cschip"><span class="csdot" id="cs-core-dot"></span>核心 <span id="cs-core">…</span></span><span class="cschip"><span class="csdot" id="cs-node-dot"></span>节点 <span id="cs-node">…</span></span><span class="cschip">运行时长 <span id="cs-up">…</span></span><span class="cschip">日志 <span id="cs-cnt">…</span></span></div><div style="padding:8px 14px;display:flex;gap:8px;flex-wrap:wrap;align-items:center;border-bottom:1px solid rgba(255,255,255,.08)"><select id="cs-level" class="csin"><option value="">全部级别</option><option value="info">信息及以上</option><option value="warn">警告及以上</option><option value="error">只看错误</option></select><select id="cs-module" class="csin"><option value="">全部模块</option><option value="system">系统</option><option value="core">核心</option><option value="node">节点</option><option value="browser">浏览器</option><option value="winmgr">窗口管理</option><option value="ssh">SSH</option><option value="servermon">服务器监控</option><option value="update">更新</option><option value="firefox">Firefox</option><option value="apps">应用商店</option><option value="openclaw">小龙虾</option><option value="onlyoffice">ONLYOFFICE</option><option value="libreoffice">LibreOffice</option><option value="imageglass">ImageGlass</option><option value="krita">Krita</option><option value="vscodium">VSCodium</option><option value="sandbox">应用隔离</option><option value="settings">设置</option><option value="storage">存储</option><option value="files">文件导入</option><option value="remote">远程协助</option><option value="diag">诊断</option><option value="api">接口</option></select><input id="cs-q" class="csin" placeholder="搜索日志…" style="flex:1;min-width:100px"><label style="font-size:12.5px;opacity:.9"><input type="checkbox" id="cs-auto" checked> 自动滚动</label><button class="btn gray" id="cs-copy">复制诊断摘要</button><button class="btn gray" id="cs-export">导出日志</button></div><div id="cs-log" style="flex:1;overflow:auto;padding:10px 14px;font-family:ui-monospace,Consolas,monospace;font-size:12px;line-height:1.7;color:#d6e2f0"></div><div style="padding:7px 14px;font-size:12px;opacity:.6;border-top:1px solid rgba(255,255,255,.08)">日志同时存在 EXE 旁 Data/logs/mj.log。出问题时先看这里的红色报错，或点「导出日志」把诊断包发出来定位。</div>`;
 }
 if(id==='appstore') b.innerHTML=`<h3>应用商店 · 便携应用</h3><p class="mut" style="margin-top:6px">这里的都是<b style="color:#fff">免安装的便携版</b>：从官方源直下、装进 EXE 旁的 <b style="color:#fff">Data/apps</b>，程序和数据都跟着文件夹走，宿主电脑不留东西。每个应用上架前都验过包，并钉死了校验和，下载对不上会自动拒绝。</p><div class="row" style="margin-top:10px"><input class="txt" id="st-q" placeholder="搜索应用…"><span class="tag" id="st-count"></span></div><div style="display:flex;gap:12px;margin-top:12px;align-items:flex-start"><div id="st-cats" style="width:102px;flex:none;display:flex;flex-direction:column;gap:4px"></div><div id="st-grid" style="flex:1;display:grid;grid-template-columns:repeat(auto-fill,minmax(220px,1fr));gap:10px;align-content:start"></div></div><p class="mut" id="st-foot" style="margin-top:12px"></p><hr style="border-color:rgba(255,255,255,.14);margin:18px 0"><h3>隔离安装 · 只有安装版的软件</h3><p class="mut" style="margin-top:6px">微信这种<b style="color:#fff">只有安装版</b>的软件，可以装进<b style="color:#fff">隔离盒子</b>：整个安装过程被隔离组件接住，程序和数据都落在 Data/sandbox 里，跟着文件夹走，宿主电脑不留东西。要先装好隔离组件（设置 → 应用隔离，过一次管理员确认）。</p><div id="st-box" style="margin-top:10px"></div>`;
 if(id==='openclaw') b.innerHTML=`<h3>小龙虾 OpenClaw</h3><div class="sm-card" style="margin-top:10px"><div class="sm-row"><span>状态</span><span id="oc-st-state">读取中…</span></div><div class="sm-row"><span>版本</span><span id="oc-st-ver">—</span></div><div class="sm-row"><span>运行时长</span><span id="oc-st-up">—</span></div><div class="sm-row"><span>内存占用（整个进程树）</span><span id="oc-st-rss">—</span></div><div class="sm-row"><span>组件占用磁盘</span><span id="oc-st-disk">—</span></div><div class="sm-row"><span>模型出口</span><span id="oc-st-route">—</span></div></div><div class="row" style="margin-top:10px"><button class="btn" id="oc-dl">下载组件</button><button class="btn gray" id="oc-check">检查更新</button><span class="tag" id="oc-prog"></span></div><div id="oc-main" style="display:none"><div class="row" style="margin-top:12px"><button class="btn" id="oc-start">启动</button><button class="btn red" id="oc-stop">停止</button><button class="btn gray" id="oc-console">打开控制台</button></div><p class="mut" style="margin-top:8px">网关令牌 <span class="tag" id="oc-token">…</span> <button class="btn gray" id="oc-tokcopy">复制令牌</button> <span style="font-size:12px">打开控制台要令牌时粘贴它；令牌存在组件自己的配置里，跟着文件夹走，不用记。</span></p><div class="sm-sec">设置</div><div class="sm-card"><div class="sm-row"><span>模型流量走本地节点</span><label class="sm-sw"><input type="checkbox" id="oc-p-vianode"><i></i></label></div><div class="sm-row"><span>打开终端机时自动启动</span><label class="sm-sw"><input type="checkbox" id="oc-p-autostart"><i></i></label></div><div class="sm-row"><span>崩溃后自动重启（10 分钟内最多 3 次）</span><label class="sm-sw"><input type="checkbox" id="oc-p-crash"><i></i></label></div></div><p class="mut" style="font-size:12px;margin-top:6px">走节点：节点在跑时模型请求经节点出去；节点起不来会明示本次直连，绝不假装走了节点。</p><div class="sm-sec">配置向导 · 模型</div><div class="sm-card"><div class="sm-row"><span>当前配置</span><span id="oc-wiz-cur">读取中…</span></div><div class="row" style="margin-top:8px" id="oc-wiz-provs"><button class="btn" data-p="openai">OpenAI</button><button class="btn gray" data-p="anthropic">Anthropic</button><button class="btn gray" data-p="deepseek">DeepSeek</button><button class="btn gray" data-p="custom">自定义接口</button></div><div class="row" style="margin-top:8px;display:none" id="oc-wiz-idrow"><input class="txt" id="oc-wiz-id" placeholder="厂商标识（小写字母开头，如 myapi）" style="max-width:240px"></div><div class="row" style="margin-top:8px"><input class="txt" id="oc-wiz-base" placeholder="接口地址（留空用官方地址）"></div><div class="row" style="margin-top:8px"><input class="txt" id="oc-wiz-key" type="password" placeholder="API Key（留空 = 不修改已保存的）" autocomplete="off"></div><div class="row" style="margin-top:8px"><input class="txt" id="oc-wiz-model" list="oc-wiz-models" placeholder="模型名，如 gpt-5.1"><datalist id="oc-wiz-models"></datalist></div><div class="row" style="margin-top:8px"><button class="btn" id="oc-wiz-save">保存配置</button><button class="btn gray" id="oc-wiz-test">测试连接</button><span class="tag" id="oc-wiz-msg"></span></div><p class="mut" style="font-size:12px;margin-top:6px">保存走小龙虾自己的配置命令：立即生效、不用重启网关，改前它会自动备份旧配置。Key 只写进组件配置，不进日志。自定义接口按 OpenAI 兼容协议。Telegram 等消息渠道去小龙虾控制台里配。</p></div><div class="sm-sec">运行日志</div><div class="sm-card"><pre id="oc-log" style="max-height:220px;overflow:auto;font-size:11.5px;line-height:1.55;white-space:pre-wrap;word-break:break-all;margin:0">（暂无日志）</pre><div class="row" style="margin-top:6px"><button class="btn gray" id="oc-log-refresh">刷新日志</button><span class="mut" style="font-size:12px">网关自己的输出；终端机的系统事件在「控制台」应用里看（模块：小龙虾）</span></div></div><div class="sm-sec">数据与组件</div><div class="sm-card"><div class="sm-row"><span>数据导出（配置 + 它记住的东西，换电脑用）</span><button class="btn gray" id="oc-export">导出</button></div><div class="sm-row"><span>数据导入（导入前自动备份现有数据）</span><span><input type="file" id="oc-import-file" accept=".zip" style="display:none"><button class="btn gray" id="oc-import">导入…</button></span></div><div class="sm-row"><span>卸载组件（程序、配置、数据全删）</span><button class="btn red" id="oc-uninst">卸载</button></div><p class="mut" style="font-size:12px;margin-top:6px" id="oc-dir"></p></div></div><details style="margin-top:14px"><summary class="mut" style="cursor:pointer;font-size:12.5px">安装须知（重型组件，先看一眼）</summary><p class="mut" style="margin-top:6px;line-height:1.7">小龙虾是一个能<b style="color:#fff">操作电脑、收发消息</b>的 AI 助手。它是<b style="color:#fff">按需下载的重型组件</b>：<br>· <b style="color:#fff">体积约 630 MB</b>，装在 EXE 旁的 Data/openclaw，跟着文件夹走；<br>· 运行时<b style="color:#fff">内存约 1 GB</b> 量级，老电脑会吃力（上面的状态卡能实时看）；<br>· 它权限很大（能跑命令、动文件、替你发消息），<b style="color:#fff">只建议装在自己的电脑上，不要装到公用电脑</b>；<br>· 模型 API Key 自备：在上面的配置向导里填，或去小龙虾自己的控制台里配。</p></details>`;
 if(id==='imgview') b.innerHTML=`<div style="display:flex;flex-direction:column;height:100%"><div class="row" style="padding-bottom:10px"><button class="btn gray" id="iv-prev">‹ 上一张</button><button class="btn gray" id="iv-next">下一张 ›</button><button class="btn gray" id="iv-fit">适应窗口 / 100%</button><span class="tag" id="iv-name">图片</span></div><div class="imgstage"><img id="iv-img" alt=""></div><p class="mut" style="margin-top:8px;font-size:12px">同目录的其他图片可以用按钮或键盘 ← → 切换。</p></div>`;
 if(id==='textview') b.innerHTML=`<div class="row" style="padding-bottom:10px"><span class="tag" id="tv-name">文本</span><label style="font-size:12.5px"><input type="checkbox" id="tv-wrap" checked> 自动换行</label><span class="tag" id="tv-note"></span></div><pre class="textview" id="tv-pre">读取中…</pre>`;
 if(id==='gridview') b.innerHTML=`<p class="mut" id="gv-note" style="margin-bottom:8px">读取中…</p><table class="gridtab" id="gv-tab"></table>`;
 if(id==='ziplist') b.innerHTML=`<h3 id="zl-name">文件夹</h3><div id="zl-list" class="mut" style="margin-top:10px">加载中…</div><p class="mut" style="margin-top:8px;font-size:12px">这是解压出来的文件夹（在 Data/files 里）。双击文件用系统内的方式打开；子文件夹暂不在这里展开。</p>`;
 if(id==='filepanel') b.innerHTML=`<h3 id="fp2-name">文件</h3><p class="mut" id="fp2-desc" style="margin-top:8px"></p><div class="row" style="margin-top:14px"><button class="btn" id="fp2-web">在网页版办公中打开（腾讯文档）</button><button class="btn gray" id="fp2-exp">导出到电脑（下载文件夹）</button></div>`;
 if(id==='core') b.innerHTML=`<h3>节点</h3>
<div class="sm-card" style="margin-top:10px">
 <div class="sm-row"><span>模式</span><span class="seg" id="np-mode"><button data-m="rule">规则</button><button data-m="global">全局</button><button data-m="direct">直连</button></span></div>
 <div class="sm-row"><span>核心状态</span><span class="row"><span class="tag" id="core-status">状态：读取中…</span><button class="btn" id="core-start">启动</button><button class="btn red" id="core-stop">停止</button><button class="btn gray" id="core-refresh">刷新</button></span></div>
 <div class="sm-row"><span>当前出口</span><span id="np-exit" style="text-align:right">—</span></div>
 <div class="sm-row"><span>实时流量</span><span class="set-val" id="np-rates">—</span></div>
 <div style="position:relative;margin-top:6px"><canvas id="np-chart" style="width:100%;height:110px;display:block"></canvas></div>
 <div class="sm-row"><span>本次会话累计</span><span class="set-val" id="np-sess">↓ 0 B · ↑ 0 B</span></div>
 <p class="mut" style="margin-top:4px">规则 = 绕过大陆（国内直连、其余走代理）；全局 = 全部走当前出口；直连 = 全部直连。点任意节点即切换出口（核心在跑时优先热切换，不重启）。TUN 与系统代理接管不做——这是便携版的红线，宿主电脑的网络设置永远不动。</p>
</div>
<div id="np-groups"></div>
<div class="set-sec" style="margin-top:14px">添加</div>
<div class="sm-card">
 <div class="sm-row" style="align-items:flex-start"><span style="padding-top:6px">节点链接</span><span style="flex:1;margin-left:12px"><textarea id="nlink" rows="2" style="width:100%" placeholder="粘贴节点链接，如 vless://... 或 ss://..."></textarea></span></div>
 <div class="sm-row"><span></span><button class="btn" id="nadd">添加这个节点</button></div>
 <div class="sm-row" style="align-items:flex-start"><span style="padding-top:6px">订阅</span><span style="flex:1;margin-left:12px"><input class="txt" id="nsubname" style="width:100%;margin-bottom:6px" placeholder="订阅名称（可空，默认用域名）"><input class="txt" id="nsub" style="width:100%" placeholder="订阅地址 https://...（支持分享链接 / base64 / Clash YAML）"></span></div>
 <div class="sm-row"><span></span><span class="row"><button class="btn gray" id="nsubbtn">添加订阅</button><button class="btn gray" id="np-updall">更新全部订阅</button></span></div>
</div>
<div class="set-sec" style="margin-top:14px">WARP 中继</div>
<div class="sm-card" id="np-warp"><div class="sm-row"><span>状态</span><span class="set-val">读取中…</span></div></div>
<div class="set-sec" style="margin-top:14px">DNS</div>
<div class="sm-card">
 <div class="sm-row"><span>方案</span><span class="seg" id="np-dns"><button data-d="anti">防污染</button><button data-d="direct">直连</button><button data-d="custom">自定义</button></span></div>
 <div class="sm-row" id="np-dnsrow" style="display:none"><span>DoH 地址</span><input class="txt" id="np-dnscustom" style="width:280px" placeholder="https://dns.google/dns-query"></div>
 <div class="sm-row"><span></span><button class="btn gray" id="np-dnssave">保存 DNS 设置</button></div>
 <p class="mut" style="margin-top:4px">防污染：远程 DoH 经代理查询，规则模式下国内域名走本地解析；直连：用系统 DNS；自定义：填一个 DoH 地址（经代理查询）。</p>
</div>
<div class="set-sec" style="margin-top:14px">规则</div>
<div class="sm-card">
 <div id="np-rules"></div>
 <div class="sm-row"><span>新增</span><span class="row"><select id="np-rtype" class="txt" style="width:96px"><option value="domain">域名</option><option value="suffix">域名后缀</option><option value="ipcidr">IP 段</option></select><input class="txt" id="np-rvalue" style="width:190px" placeholder="example.com 或 203.0.113.0/24"><select id="np-raction" class="txt" style="width:84px"><option value="proxy">代理</option><option value="direct">直连</option><option value="block">拒绝</option></select><button class="btn gray" id="np-radd">添加规则</button></span></div>
 <div class="sm-row"><span></span><button class="btn" id="np-rsave">保存规则</button></div>
 <p class="mut" style="margin-top:4px">自定义规则优先级高于默认集，规则模式下生效。默认规则集（只读）：</p>
 <div id="np-defrules" class="mut"></div>
</div>
<div class="set-sec" style="margin-top:14px">实时连接</div>
<div class="sm-card">
 <div class="sm-row"><span>连接</span><span class="row"><span class="set-val" id="np-conn-note">—</span><button class="btn gray" id="np-conn-refresh">刷新</button><button class="btn gray" id="np-conn-closeall">关闭全部</button></span></div>
 <div style="overflow-x:auto"><div id="np-conns"></div></div>
</div>
<div class="set-sec" style="margin-top:14px">其他</div>
<div class="sm-card">
 <div class="sm-row"><span>软件内全局加速</span><span class="row"><span class="tag" id="accel-state">…</span><button class="btn gray" id="accel-toggle">切换</button></span></div>
 <div class="sm-row"><span>维护</span><span class="row"><button class="btn gray" id="ptest">代理自检</button><button class="btn gray" id="corelogbtn">看 sing-box 日志</button><button class="btn red" id="nclear">清空全部节点与订阅</button></span></div>
</div>
<pre class="term" id="corelog" style="min-height:90px;margin-top:10px;display:none"></pre>
<p class="mut" style="margin-top:8px">全局加速是终端内路由的总闸（桌面窗口、点开的链接、终端命令走不走节点）；模式管核心内部的分流。两者不冲突，浏览器始终经 127.0.0.1:2080。</p>`;
 if(id==='sys'){ b.style.padding='0'; b.style.display='flex'; b.style.flexDirection='column';
  b.innerHTML=`<div class="set-wrap"><div class="set-side"><input class="set-search" id="set-q" placeholder="搜索设置"><div class="set-mecard" id="set-mecard"><div class="set-meico g-sys">铭</div><div style="min-width:0"><div style="font-weight:650;font-size:14px">铭杰终端机</div><div style="font-size:11.5px;opacity:.65" id="set-me-ver">版本 …</div></div></div><div class="set-nav" id="set-nav"></div></div><div class="set-main" id="set-main"><p class="mut">加载中…</p></div></div>`;
 }
 bind(id);
}

export async function loadSys(){
 __bind_sysInfo.set(await api('/api/system'));
 const el=document.getElementById('home-sys'); if(el) el.textContent=`${sysInfo.os}/${sysInfo.arch} · ${sysInfo.hostname} · CPU ${sysInfo.numCPU} 核 · 主目录 ${sysInfo.home}`;
 const kv=document.getElementById('syskv'); if(kv) kv.innerHTML=Object.entries({电脑名:sysInfo.hostname,系统:sysInfo.os,架构:sysInfo.arch,CPU核数:sysInfo.numCPU,主目录:sysInfo.home,程序目录:sysInfo.exeDir,数据目录:sysInfo.dataDir,当前时间:sysInfo.time}).map(([k,v])=>`<span class="mut">${k}</span><span>${esc(v)}</span>`).join('');
 const pr=document.getElementById('prompt'); if(pr) pr.textContent=sysInfo.home||'>';
}

export let history=[],hi=-1;
export async function runCmd(cmd){
 const out=document.getElementById('tout'), pr=document.getElementById('prompt');
 if(!cmd.trim())return; history.unshift(cmd);hi=-1;
 out.textContent+=`\r\n${pr.textContent}> ${cmd}\r\n运行中…`;
 const r=await api('/api/run',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({cmd})});
 out.textContent=out.textContent.replace(/运行中…$/,'');
 out.textContent+=(r.output||'')+(r.error?('\r\n[错误] '+r.error):'')+'\r\n';
 if(r.cwd) pr.textContent=r.cwd;
 out.scrollTop=out.scrollHeight;
}
export async function coreAct(action){
 const r=await api('/api/core',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action})});
 const s=document.getElementById('core-status'); if(s) s.textContent='状态：'+(r.message||JSON.stringify(r));
 pollNode();
}

export function bind(id){
 if(id==='home'||id==='sys') loadSys();
 if(id==='sys'){
  /* ----- 设置 (v0.30): macOS System Settings grammar -----
     Left rail: search + colored section list, topped by the system
     card (click = 软件更新）. Right rail: grouped lists -- section
     title, grey description, rows with the control right-aligned
     (reuses the .sm-* grouped-list grammar). Every control below
     reads/writes REAL daemon state: preferences live in
     Data/settings.json (/api/settings), and the per-domain rows hit
     the same endpoints their own app pages use. */
  const G=(x: any): any=>document.getElementById(x);
  const fmtB=n=>{ n=+n||0; if(n>=1073741824) return (n/1073741824).toFixed(2)+' GB'; if(n>=1048576) return (n/1048576).toFixed(1)+' MB'; if(n>=1024) return (n/1024).toFixed(1)+' KB'; return n+' B' };
  const PAGES=[
   {id:'appearance',n:'外观',g:'linear-gradient(180deg,#64d2ff,#bf5af2)',glyph:'image',kw:'壁纸 背景 个性化 wallpaper'},
   {id:'desktop',n:'桌面与 Dock',g:'linear-gradient(180deg,#2dd4bf,#0e7490)',glyph:'stage',kw:'台前调度 手机宽 落位 dock 位置 左侧 右侧 自动隐藏 窗口 导入 文件 桌面图标 沉浸 全屏 任务栏 控制中心'},
   {id:'notify',n:'通知',g:'linear-gradient(180deg,#ff453a,#ff9f0a)',glyph:'bell',kw:'通知 横幅 提醒 通知中心 banner 允许'},
   {id:'network',n:'节点与网络',g:'linear-gradient(180deg,#bf5af2,#7d2ae8)',glyph:'rocket',kw:'节点 加速 代理 下载 网络'},
   {id:'browser',n:'浏览器',g:'linear-gradient(180deg,#5ec2ff,#0a62d6)',glyph:'globe',kw:'chrome firefox 缓存 同步 谷歌 浏览器'},
   {id:'components',n:'组件',g:'linear-gradient(180deg,#ffb340,#ff7139)',glyph:'shapes',kw:'firefox 小龙虾 openclaw 组件'},
   {id:'isolation',n:'应用隔离',g:'linear-gradient(180deg,#64d2ff,#5856d6)',glyph:'box',kw:'隔离 沙盒 sandboxie 驱动 管理员 微信 安装 盒子'},
   {id:'storage',n:'存储空间',g:'linear-gradient(180deg,#a7b0be,#5b6470)',glyph:'stack',kw:'存储 空间 清理 缓存 瘦身 占用 磁盘'},
   {id:'security',n:'安全与隐私',g:'linear-gradient(180deg,#ffd60a,#ff9f0a)',glyph:'lock',kw:'远程 协助 安全 隐私 配对 接管 tailscale 操作者 保险库 加密 密码 锁屏 vault lock'},
   {id:'general',n:'通用',g:'linear-gradient(180deg,#d1d1d6,#636366)',glyph:'gear',kw:'退出 自动 时长 通用 关机 关闭 应用'},
   {id:'about',n:'关于',g:'linear-gradient(180deg,#64d2ff,#0a84ff)',glyph:'info',kw:'关于 版本 控制台 诊断 数据目录'},
   {id:'update',n:'软件更新',g:'linear-gradient(180deg,#8e8e93,#48484a)',glyph:'up',kw:'更新 升级 版本 令牌'}
  ];
  let curPage='appearance', usageCache=null, updIv=null, secIv=null, sbxIv=null, prodIv=null;
  const storageUsageOnce=async()=>{ if(!usageCache) usageCache=await api('/api/storage/usage'); return usageCache };
  const navPaint=()=>{ const q=(G('set-q').value||'').trim().toLowerCase();
   G('set-nav').innerHTML=PAGES.filter(p=>!q||(p.n+' '+p.kw).toLowerCase().includes(q)).map(p=>`<div class="set-it${p.id===curPage?' on':''}" data-page="${p.id}"><span class="set-ico" style="background:${p.g}">${glyph(p.glyph,14)}</span><span>${p.n}</span></div>`).join('')||'<p class="mut" style="padding:8px">没有匹配的设置项</p>';
   G('set-nav').querySelectorAll('[data-page]').forEach(el=>el.onclick=()=>goto(el.dataset.page));
   const mc=G('set-mecard'); if(mc) mc.classList.toggle('on',curPage==='update');
  };
  const goto=pid=>{ curPage=pid; navPaint(); paintPage(pid) };
  const paintAppearance=()=>{
   G('set-main').innerHTML=`<div class="set-page"><div class="set-sec">壁纸</div><div class="set-desc">点一张换壁纸，立刻生效。也可以上传你自己的图片当壁纸（存在 EXE 旁的 Data 文件夹，跟着程序走）。</div><div class="wp-grid" id="wp-grid">${Object.entries(WALLPAPERS).map(([k,v])=>`<div class="wp" data-wp="${k}" style="background:${v.css}"><span>${v.n}</span></div>`).join('')}</div><div class="sm-card" style="margin-top:14px"><div class="sm-row"><span>自定义壁纸</span><span class="row"><label class="btn gray" for="wp-file" style="cursor:pointer">上传我的图片…</label><input type="file" id="wp-file" accept="image/*" style="display:none"><button class="btn gray" id="wp-remove">删除自定义壁纸</button></span></div><div class="sm-row"><span>当前</span><span class="set-val" id="wp-status">…</span></div></div><div class="set-sec">桌面图标</div><div class="set-desc">桌面图标的大小和排列方式。大小在桌面空白处右键菜单、控制中心里也能调；手动拖动任何图标，排序会切回「手动」。</div><div class="sm-card"><div class="sm-row"><span>图标大小</span><span class="row"><input type="range" id="ap-size" min="36" max="96" step="2" value="${deskIconSize()}" style="accent-color:#0a84ff"><span class="set-val" id="ap-size-v">${deskIconSize()}</span></span></div><div class="sm-row"><span>排序方式</span><span class="seg" id="ap-sort"><button data-m="manual">手动</button><button data-m="name">名称</button><button data-m="freq">最常用</button><button data-m="kind">种类</button><button data-m="added">添加日期</button></span></div></div><p class="set-note">壁纸选择存在 Data/settings.json，跟着文件夹走，换电脑不丢。</p></div>`;
   const markSel=()=>{ const cur=customWall?'':wallKey(); document.querySelectorAll('.wp').forEach(x=>x.classList.toggle('sel',x.dataset.wp===cur)); const st=G('wp-status'); if(st) st.textContent=customWall?'自定义壁纸':(WALLPAPERS[cur]?.n||cur); };
   markSel();
   document.querySelectorAll('.wp').forEach(x=>x.onclick=async()=>{ if(customWall){ await api('/api/wallpaper',{method:'DELETE'}); __bind_customWall.set(false); } await saveSetting({wallpaper:x.dataset.wp}); applyWallpaper(); markSel(); });
   G('wp-file').onchange=async e=>{ const f=e.target.files[0]; if(!f) return; const st=G('wp-status'); st.textContent='上传中…'; const buf=await f.arrayBuffer(); const r=await fetch('/api/wallpaper',{method:'POST',headers:{'Content-Type':f.type||'image/jpeg'},body:buf}); const j=await r.json().catch(()=>({})); if(j.ok){ __bind_customWall.set(true); applyWallpaper(); markSel(); } else st.textContent='失败：'+(j.error||'未知错误'); };
   G('wp-remove').onclick=async()=>{ await api('/api/wallpaper',{method:'DELETE'}); __bind_customWall.set(false); applyWallpaper(); markSel(); };
   const apSize=G('ap-size');
   if(apSize){
    apSize.oninput=()=>{ const v=+apSize.value; G('ap-size-v').textContent=v; if(SETTINGS) SETTINGS.deskIconSize=v; applyDeskIconSize(); };
    apSize.onchange=()=>{ saveSetting({deskIconSize:+apSize.value}); };
    const seg=G('ap-sort');
    const paintSort=()=>seg.querySelectorAll('button').forEach(b=>b.classList.toggle('on',b.dataset.m===deskSortMode()||(b.dataset.m==='manual'&&deskSortMode()==='manual')));
    paintSort();
    seg.querySelectorAll('button').forEach(b=>b.onclick=()=>{ setDeskSort(b.dataset.m); paintSort(); });
   }
  };
  const paintDesktop=()=>{
   const s=SETTINGS||SETDEF;
   G('set-main').innerHTML=`<div class="set-page"><div class="set-sec">台前调度</div><div class="set-desc">开启后，桌面只显示当前应用，其他打开的应用缩到左侧成一排实时缩略图，点缩略图就切换过来。菜单栏右侧的 ▦ 也可以随开随关。</div><div class="sm-card"><div class="sm-row"><span>台前调度</span><label class="sm-sw"><input type="checkbox" id="ds-stage" ${stageOn()?'checked':''}><i></i></label></div><div class="sm-row"><span>浏览器新窗口自动纳入台前调度</span><label class="sm-sw"><input type="checkbox" id="ds-autoclaim" ${s.stageAutoClaimBrowser!==false?'checked':''}><i></i></label></div><div class="sm-row"><span>外部窗口卡片</span><span class="seg" id="ds-stagedisp"><button data-m="preview">窗口预览</button><button data-m="identity">应用标识</button></span></div></div><p class="set-note">窗口预览＝卡片上是窗口的真实画面缩略图（默认）；应用标识＝只显示应用图标和窗口标题，画面抓不到的窗口会自动用这种。自动纳入：隔离 Chrome / Firefox 自己新开的窗口（网页弹窗、新窗口、拖出的标签、独立的开发者工具）会自动登记进台前调度，方便切换；登记后是否摆位由下方「开窗礼仪」里的开关决定。已经登记的窗口不受这个开关影响。</p><div class="set-sec">窗口记忆</div><div class="set-desc">每个应用上次关窗时的大小和位置会被记住，下次打开优先按记忆落位（超出当前桌面范围会自动收回）。网站应用在右键菜单「调整窗口」里选了手机/桌面/全屏时，那个选择优先于记忆。</div><div class="sm-card"><div class="sm-row"><span>已记住的窗口</span><span class="set-val" id="ds-geom-n">…</span></div><div class="sm-row"><span>清除窗口记忆</span><button class="btn gray" id="ds-geom-clear">全部清除</button></div></div><div class="set-sec">Dock</div><div class="set-desc">终端机自己的 Dock，和 macOS 一样可以换边、可以自动隐藏；图标放大跟随指针连续缩放。改动即时生效，不用重启。</div><div class="sm-card"><div class="sm-row"><span>位置</span><span class="seg" id="ds-dockpos"><button data-p="bottom">底部</button><button data-p="left">左侧</button><button data-p="right">右侧</button></span></div><div class="sm-row"><span>自动隐藏 Dock</span><label class="sm-sw"><input type="checkbox" id="ds-dockhide" ${s.dockAutoHide?'checked':''}><i></i></label></div></div><p class="set-note">自动隐藏后，Dock 沿它所在的那条边滑出，只留一条 3px 的边：指针贴到那条边停约 0.3 秒（或快速甩到边沿）它就滑进来，移开约 0.4 秒又滑回去。台前调度开着时把 Dock 放到左侧，会和台前调度条带抢左缘——条带（系统内缩略图和外部应用卡片）会临时镜像到右缘，Dock 改回来它也跟着回来，只是位置镜像，行为不变。</p><div class="set-sec">小窗手机宽</div><div class="set-desc">桌面窗口没最大化、或窄于触发宽度时，新打开的网站应用按手机宽度落位，网站多会显示手机版页面。只管打开时摆好，之后随你拖动。</div><div class="sm-card"><div class="sm-row"><span>手机宽模式</span><label class="sm-sw"><input type="checkbox" id="ds-pm" ${s.phoneMode?'checked':''}><i></i></label></div><div class="sm-row"><span>触发宽度（桌面窄于此值，px）</span><input class="set-num" id="ds-ptw" type="number" min="600" max="4000" step="50" value="${s.phoneTriggerWidth}"></div><div class="sm-row"><span>手机宽度（落位宽度，px）</span><input class="set-num" id="ds-pw" type="number" min="280" max="800" step="10" value="${s.phoneWidth}"></div></div><div class="set-sec">开窗礼仪</div><div class="set-desc">从终端机打开、或被台前调度认领的窗口，第一次出现时自动摆进桌面区域（约 62% × 85%，菜单栏和 Dock 不会被盖住），你正在终端里操作时桌面还会垫在新窗口后面，就像在自己的系统里开应用；摆好之后完全归你，拖动、最大化都不再被拉回。你在宿主的其他软件里忙时，新窗口只登记、不抢屏。</div><div class="sm-card"><div class="sm-row"><span>新窗口自动落位（桌面垫背）</span><label class="sm-sw"><input type="checkbox" id="ds-place" ${s.winPlaceOnClaim!==false?'checked':''}><i></i></label></div><div class="sm-row"><span>最小化时联动隐藏</span><label class="sm-sw"><input type="checkbox" id="ds-hidemin" ${s.winHideOnMinimize!==false?'checked':''}><i></i></label></div><div class="sm-row"><span>关闭桌面时隐藏到托盘</span><label class="sm-sw"><input type="checkbox" id="ds-tray" ${s.winCloseToTray!==false?'checked':''}><i></i></label></div></div><p class="set-note">联动隐藏：最小化桌面时已登记的窗口整套跟着藏起，桌面自己留在宿主任务栏里找得到，恢复桌面时整套按原样回来（你自己单独最小化的窗口不会被动）。关闭桌面则藏得更彻底：任务栏不留按钮，宿主右下角系统托盘会出现终端机图标，点它（或照旧双击程序）整套恢复。无论哪种藏法，退出系统前所有被藏的窗口都会先恢复显示。</p><div class="set-sec">沉浸模式</div><div class="set-desc">开启后，终端机打开的一切只留在终端机里：已经登记进台前调度的窗口（包括桌面自己）会从宿主任务栏和 Alt-Tab 里消失，切换入口只剩台前调度与 Dock；宿主右下角的托盘图标是回宿主的锚，点它整套回来。没有被台前调度认领的窗口永远不会被隐藏，一直留在宿主任务栏上找得到。</div><div class="sm-card"><div class="sm-row"><span>沉浸模式</span><label class="sm-sw"><input type="checkbox" id="ds-immersive" ${s.immersiveMode?'checked':''}><i></i></label></div><div class="sm-row"><span>桌面全屏</span><label class="sm-sw"><input type="checkbox" id="ds-fullscreen" ${s.desktopFullscreen?'checked':''}><i></i></label></div></div><p class="set-note">桌面全屏：桌面视图以无边框全屏占据它所在的显示器，关掉这个开关就恢复原来的窗口大小与位置；两个开关都即时生效，不用重启。一句实话：万一终端机异常退出（被强杀或崩溃），个别已沉浸的窗口可能暂时留在隐藏状态——把那个窗口关掉即自愈，重新打开终端机时启动清扫也会把它们还原回任务栏。</p><div class="set-sec">导入的文件</div><div class="set-desc">从电脑导入到终端桌面的文件存在 Data/files 里，和应用图标混排在桌面上。关掉只是不在桌面上显示这些图标，文件本身、导入通道（拖入 / 桌面菜单导入 / 粘贴）都不受影响。</div><div class="sm-card"><div class="sm-row"><span>在桌面显示导入的文件</span><label class="sm-sw"><input type="checkbox" id="ds-files" ${s.filesOnDesktop!==false?'checked':''}><i></i></label></div></div><div class="set-sec">控制中心</div><div class="set-desc">菜单栏右边双开关图标打开的控制中心里放哪些格子，改动即时生效。节点大格是核心控制，和 macOS 的核心控件一样不能关。</div><div class="sm-card"><div class="sm-row"><span>节点（核心控制）</span><span class="set-val">始终显示</span></div>${[['immersive','沉浸模式'],['stage','台前调度'],['fullscreen','桌面全屏'],['dockhide','Dock 自动隐藏'],['remote','远程协助'],['wallpaper','壁纸']].map(([k,n])=>`<div class="sm-row"><span>${n}</span><label class="sm-sw"><input type="checkbox" id="ds-cc-${k}" ${s['cc_show_'+k]!==false?'checked':''}><i></i></label></div>`).join('')}</div></div>`;
   G('ds-stage').onchange=e=>toggleStage(e.target.checked);
   const sdSeg=G('ds-stagedisp');
   if(sdSeg){
    const paintSD=()=>sdSeg.querySelectorAll('button').forEach(b=>b.classList.toggle('on',b.dataset.m===((SETTINGS&&SETTINGS.stageDisplay)||'preview')));
    paintSD();
    sdSeg.querySelectorAll('button').forEach(b=>b.onclick=async()=>{ const r=await saveSetting({stageDisplay:b.dataset.m}); if(r&&r.error){ alert('保存失败：'+r.error); return; } paintSD(); refreshExtStrip(); });
   }
   const geomN=G('ds-geom-n');
   if(geomN){
    const paintN=async()=>{ try{ const s=await api('/api/wingeom/clear'); geomN.textContent=(s.count||0)+' 个窗口'; }catch(e){ geomN.textContent='—'; } };
    paintN();
    G('ds-geom-clear').onclick=async()=>{ const r=await api('/api/wingeom/clear',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({})}); if(r&&r.error){ alert('清除失败：'+r.error); return; } geomN.textContent='已清除 '+(r.cleared||0)+' 条'; toast('窗口记忆已清除，下次打开用默认大小'); };
   }
   G('ds-files').onchange=async e=>{ const r=await saveSetting({filesOnDesktop:e.target.checked}); if(r&&r.error){ alert('保存失败：'+r.error); e.target.checked=!e.target.checked; return; } renderDesk(); };
   G('ds-autoclaim').onchange=async e=>{ const r=await saveSetting({stageAutoClaimBrowser:e.target.checked}); if(r&&r.error){ alert('保存失败：'+r.error); e.target.checked=!e.target.checked; } };
   const bindWinTgl=(id,key)=>{ const el=G(id); if(el) el.onchange=async e=>{ const r=await saveSetting({[key]:e.target.checked}); if(r&&r.error){ alert('保存失败：'+r.error); e.target.checked=!e.target.checked; } }; };
   bindWinTgl('ds-place','winPlaceOnClaim'); bindWinTgl('ds-hidemin','winHideOnMinimize'); bindWinTgl('ds-tray','winCloseToTray');
   bindWinTgl('ds-immersive','immersiveMode'); bindWinTgl('ds-fullscreen','desktopFullscreen');
   ['immersive','stage','fullscreen','dockhide','remote','wallpaper'].forEach(k=>bindWinTgl('ds-cc-'+k,'cc_show_'+k));
   const dockPosPaint=()=>{ const seg=G('ds-dockpos'); if(seg) seg.querySelectorAll('button').forEach(b=>b.classList.toggle('on',b.dataset.p===dockPos())); };
   dockPosPaint();
   G('ds-dockpos').querySelectorAll('button').forEach(b=>b.onclick=async()=>{ const prev=dockPos(); if(b.dataset.p===prev) return; if(!SETTINGS) __bind_SETTINGS.set({...SETDEF}); SETTINGS.dockPosition=b.dataset.p; dockPosPaint(); applyDockLayout(); const r=await saveSetting({dockPosition:b.dataset.p}); if(r&&r.error){ alert('保存失败：'+r.error); SETTINGS.dockPosition=prev; dockPosPaint(); applyDockLayout(); } });
   G('ds-dockhide').onchange=async e=>{ const want=e.target.checked; const r=await saveSetting({dockAutoHide:want}); if(r&&r.error){ alert('保存失败：'+r.error); e.target.checked=!want; if(SETTINGS) SETTINGS.dockAutoHide=!want; return; } applyDockLayout(); };
   const savePhone=async()=>{ const r=await saveSetting({phoneMode:G('ds-pm').checked,phoneTriggerWidth:+G('ds-ptw').value||1200,phoneWidth:+G('ds-pw').value||400}); if(r&&r.error) alert('保存失败：'+r.error); };
   G('ds-pm').onchange=savePhone; G('ds-ptw').onchange=savePhone; G('ds-pw').onchange=savePhone;
  };
  const paintNotify=async()=>{
   /* v0.47 设置 → 通知：macOS Notifications grammar -- one row
      per source, 允许通知 + 横幅 switches, both daemon settings
      (notify_allow_<src> / notify_banner_<src>).
      v0.54: a second section, 应用通知 -- one row per installed
      app (store apps, components incl. Firefox/OpenClaw, and the
      desktop's web apps), keys notify_allow_app_<id> /
      notify_banner_app_<id>, default on (absent = on). */
   const s=SETTINGS||SETDEF;
   const SRCS=[['sandbox','应用隔离'],['update','软件更新'],['servermon','服务器监控'],['node','节点'],['remote','远程协助'],['files','文件']];
   const rowHTML=(idAttr,keyBase,name)=>`<div class="sm-row"><span>${esc(name)}</span><span class="row"><span style="font-size:12px;opacity:.65">允许通知</span><label class="sm-sw"><input type="checkbox" id="nt-allow-${idAttr}" ${s['notify_allow_'+keyBase]!==false?'checked':''}><i></i></label><span style="font-size:12px;opacity:.65">横幅</span><label class="sm-sw"><input type="checkbox" id="nt-banner-${idAttr}" ${s['notify_banner_'+keyBase]!==false?'checked':''}><i></i></label></span></div>`;
   G('set-main').innerHTML=`<div class="set-page"><div class="set-sec">系统通知</div><div class="set-desc">通知中心（点菜单栏右边的日期时间打开）里有两类东西：状态型的（隔离安装待登记、更新可用、服务器告警——状态在就一直在）和事件型的（装完了、断开了这类刚发生的事，留下来等你处理）。这里按来源逐个管：关掉「允许通知」，这个来源的事件不再记录、状态型也不再出现在通知中心（功能本身在各自页面照常）；关掉「横幅」只是不弹横幅，通知照样进通知中心。</div><div class="sm-card">${SRCS.map(([id,n])=>rowHTML(id,id,n)).join('')}</div><p class="set-note">横幅是新事件发生时在桌面右上角滑入的小卡片，约 6 秒后自动消失，鼠标停在上面会等你。沉浸模式不拦横幅——它不是专注模式。</p><div class="set-sec">应用通知</div><div class="set-desc">你装的应用由终端机产生的事件（目前是安装完成）。默认都开；关掉某个应用，它的事就不再进通知中心。如实说明：网页自己在 Chrome 里发的通知走 Windows 通知中心，不在这里管，也管不到。</div><div class="sm-card" id="nt-apps"><div class="sm-row"><span>读取中…</span></div></div></div>`;
   const bindN=(elid,key)=>{ const el=G(elid); if(el) el.onchange=async e=>{ const r=await saveSetting({[key]:e.target.checked}); if(r&&r.error){ alert('保存失败：'+r.error); e.target.checked=!e.target.checked; } }; };
   SRCS.forEach(([id])=>{ bindN('nt-allow-'+id,'notify_allow_'+id); bindN('nt-banner-'+id,'notify_banner_'+id); });
   /* The installed-app list: store apps + components (incl.
      Firefox and OpenClaw) + the desktop's web apps. Web-app
      notify ids are a slug of the site URL (daemon validation
      only allows [a-z0-9-]); collisions get a numeric suffix. */
   const rows=[], usedIds=new Set();
   const pushRow=(id,name)=>{ let k=id,i=2; while(usedIds.has(k)) k=id+'-'+(i++); usedIds.add(k); rows.push({id:k,name}); };
   (typeof STOREAPPS!=='undefined'?STOREAPPS:[]).forEach(a=>pushRow('store-'+a.id,a.name));
   (typeof PRODAPPS!=='undefined'?PRODAPPS:[]).filter(c=>c.installed).forEach(c=>pushRow('prod-'+c.id,c.name));
   try{ const ff=await api('/api/firefox'); if(ff&&ff.installed) pushRow('firefox','Firefox'); }catch(e){}
   try{ const oc=await api('/api/openclaw/status'); if(oc&&oc.installed) pushRow('openclaw','小龙虾 OpenClaw'); }catch(e){}
   const webSlug=a=>{ let h=''; try{ const u=new URL(a.url); h=(u.hostname+u.pathname).toLowerCase(); }catch(e){ h=(a.name||'').toLowerCase(); } return 'web-'+(h.replace(/[^a-z0-9]+/g,'-').replace(/^-+|-+$/g,'').slice(0,44)||'app'); };
   (typeof WEBAPPS!=='undefined'?WEBAPPS:[]).forEach(a=>pushRow(webSlug(a),a.name));
   const card=G('nt-apps'); if(!card) return;
   card.innerHTML=rows.length?rows.map(r=>rowHTML('app-'+r.id,'app_'+r.id,r.name)).join(''):'<div class="sm-row"><span>还没有已安装的应用</span></div>';
   rows.forEach(r=>{ bindN('nt-allow-app-'+r.id,'notify_allow_app_'+r.id); bindN('nt-banner-app-'+r.id,'notify_banner_app_'+r.id); });
  };
  const paintNetwork=async()=>{
   const s=SETTINGS||SETDEF;
   G('set-main').innerHTML=`<div class="set-page"><div class="set-sec">软件内全局加速</div><div class="set-desc">开着时：桌面窗口、从软件里点开的链接、终端里跑的命令都走节点；宿主电脑自己的浏览器和程序完全不受影响。它是终端内路由的总闸；下面的「模式」管的是核心内部怎么分流（规则 / 全局 / 直连），两边不冲突。</div><div class="sm-card"><div class="sm-row"><span>全局加速</span><label class="sm-sw"><input type="checkbox" id="nw-accel"><i></i></label></div><div class="sm-row"><span>当前状态</span><span class="set-val" id="nw-state">读取中…</span></div><div class="sm-row"><span>守护进程下载（更新、组件）</span><span class="set-val" id="nw-route">…</span></div></div><div class="set-sec">节点专业化</div><div class="set-desc">耐久偏好放这里；日常操作（切换出口、测速、订阅更新、规则编辑）在「节点」应用里，两边读同一份数据。</div><div class="sm-card"><div class="sm-row"><span>核心启动时的模式</span><span class="seg" id="nw-startmode"><button data-m="last" class="${s.nodeStartMode==='last'?'on':''}">跟随上次</button><button data-m="rule" class="${s.nodeStartMode==='rule'?'on':''}">规则</button><button data-m="global" class="${s.nodeStartMode==='global'?'on':''}">全局</button><button data-m="direct" class="${s.nodeStartMode==='direct'?'on':''}">直连</button></span></div><div class="sm-row"><span>订阅自动更新</span><label class="sm-sw"><input type="checkbox" id="nw-subauto" ${s.nodeSubAuto!==false?'checked':''}><i></i></label></div><div class="sm-row"><span>订阅默认更新间隔（分钟）</span><input class="set-num" id="nw-subint" type="number" min="15" max="10080" step="15" value="${s.nodeSubIntervalMin}"></div><div class="sm-row"><span>测速目标</span><input class="txt" id="nw-testurl" style="width:280px;text-align:right" value="${esc(s.nodeTestURL||'')}"></div><div class="sm-row"><span>测速超时（毫秒）</span><input class="set-num" id="nw-testto" type="number" min="1000" max="15000" step="500" value="${s.nodeTestTimeoutMs}"></div><div class="sm-row"><span>Clash API（实时连接 / 流量图 / 热切换）</span><label class="sm-sw"><input type="checkbox" id="nw-clash" ${s.nodeClashAPI!==false?'checked':''}><i></i></label></div></div><p class="set-note">Clash API 只监听 127.0.0.1，密钥在本地随机生成、不进日志。关掉它，节点页的实时连接与流量图会如实显示不可用，切换出口改走重启核心。测速目标与超时改完后，节点页的下一次测速即生效。</p><div class="set-sec">节点</div><div class="set-desc">节点链接、订阅导入和节点启停在「节点」应用里。</div><div class="row"><button class="btn gray" id="nw-open">打开节点页</button></div></div>`;
   const paint=async()=>{ const r=await api('/api/accel'); const sw=G('nw-accel'); if(sw) sw.checked=!!r.enabled; const st=G('nw-state'); if(st) st.textContent=r.active?'生效中（节点运行中）':(r.enabled?'已开启，节点启动后生效':'已关闭'); const rt=G('nw-route'); if(rt) rt.textContent=r.active?'走节点':'直连（节点没在跑或加速已关）'; };
   G('nw-accel').onchange=async e=>{ await api('/api/accel',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({enabled:e.target.checked})}); paint(); pollNode(); };
   G('nw-open').onclick=()=>openApp('core');
   const bindSave=(elid,key,conv)=>{ const el=G(elid); if(el) el.onchange=async()=>{ const r=await saveSetting({[key]:conv?conv(el):el.value}); if(r&&r.error) alert('保存失败：'+r.error); }; };
   G('nw-startmode').querySelectorAll('button').forEach(b=>b.onclick=async()=>{ const r=await saveSetting({nodeStartMode:b.dataset.m}); if(r&&r.error){ alert('保存失败：'+r.error); return; } G('nw-startmode').querySelectorAll('button').forEach(x=>x.classList.toggle('on',x===b)); });
   bindSave('nw-subauto','nodeSubAuto',el=>el.checked);
   bindSave('nw-subint','nodeSubIntervalMin',el=>+el.value||360);
   bindSave('nw-testurl','nodeTestURL',el=>el.value.trim());
   bindSave('nw-testto','nodeTestTimeoutMs',el=>+el.value||5000);
   bindSave('nw-clash','nodeClashAPI',el=>el.checked);
   paint();
  };
  const paintBrowser=async()=>{
   const s=SETTINGS||SETDEF;
   G('set-main').innerHTML=`<div class="set-page"><div class="set-sec">隔离 Chrome</div><div class="set-desc">独立档案存在 Data/ChromeProfile，历史、Cookie、登录都在里面，跟着文件夹走。</div><div class="sm-card"><div class="sm-row"><span>档案大小</span><span class="set-val" id="br-chrome-size">读取中…</span></div><div class="sm-row"><span>磁盘缓存上限（MB，0 = 不限制）</span><input class="set-num" id="br-cache" type="number" min="0" max="2000" step="50" value="${s.chromeCacheMB}"></div></div><p class="set-note">缓存是档案长胖的主因。上限在下次打开隔离 Chrome 时生效（启动参数 --disk-cache-size）；已经开着的窗口不受影响。要立刻瘦身去「存储空间」一键清理。</p><div class="set-sec">Firefox</div><div class="sm-card"><div class="sm-row"><span>组件状态</span><span class="set-val" id="br-ff">读取中…</span></div><div class="sm-row"><span>管理</span><button class="btn gray" id="br-ff-open">打开 Firefox 页</button></div></div><div class="set-sec">谷歌云同步</div><div class="set-desc">Chrome 的登录搬不了家（被那台电脑的 Windows 账户锁着），但开启谷歌同步后，换一台电脑在隔离 Chrome 里登一次谷歌，密码、书签、历史就会同步回来。同步开关在谷歌浏览器页里。</div><div class="row"><button class="btn gray" id="br-sync">去谷歌浏览器页开同步</button></div></div>`;
   G('br-cache').onchange=async()=>{ const r=await saveSetting({chromeCacheMB:+G('br-cache').value||0}); if(r&&r.error){ alert('保存失败：'+r.error); G('br-cache').value=(SETTINGS||SETDEF).chromeCacheMB; } };
   G('br-ff-open').onclick=()=>openApp('firefox');
   G('br-sync').onclick=()=>openApp('browser');
   try{ const ff=await api('/api/firefox'); const el=G('br-ff'); if(el) el.textContent=ff.installed?('已安装 · v'+(ff.version||'?')):'未安装（去 Firefox 页下载组件）'; }catch(e){}
   try{ const u=await storageUsageOnce(); const el=G('br-chrome-size'); if(el){ const g=(u.groups||[]).find(x=>x.key==='chrome'); el.textContent=g?fmtB(g.bytes):'—'; } }catch(e){}
  };
  const paintComponents=async()=>{
   if(prodIv){ clearInterval(prodIv); prodIv=null; }
   G('set-main').innerHTML=`<div class="set-page"><div class="set-sec">组件</div><div class="set-desc">按需下载的重型组件，装在 EXE 旁，跟着文件夹走。点「管理」去各自的应用页下载、启停或卸载。</div><div class="sm-card"><div class="sm-row"><span>Firefox</span><span class="row"><span class="set-val" id="cp-ff">读取中…</span><button class="btn gray" data-openapp="firefox">管理</button></span></div><div class="sm-row"><span>小龙虾 OpenClaw</span><span class="row"><span class="set-val" id="cp-oc">读取中…</span><button class="btn gray" data-openapp="openclaw">管理</button></span></div><div class="sm-row"><span>应用隔离 Sandboxie</span><span class="row"><span class="set-val" id="cp-sbie">读取中…</span><button class="btn gray" id="cp-sbie-go">管理</button></span></div></div><p class="set-note">应用隔离的组件包已内置在程序里（装的时候不用联网），驱动装在当前这台电脑上；细节和诚实说明在「应用隔离」页。</p><div class="set-sec">生产力组件</div><div class="set-desc">五个开源软件做成的便携组件，按需逐个下载（合计约 1.3 GB），装进 EXE 旁的 <b style="color:#fff">Data/components</b>；每个组件的数据都收在自己的组件目录里，跟着文件夹走。没装的在桌面上是「未下载」图标，点它也会开始下载。</div><div class="sm-card" id="cp-prod"><div class="sm-row"><span>读取中…</span></div></div><div class="set-sec">打开方式</div><div class="sm-card"><div class="sm-row"><span>默认办公套件</span><span class="sm-seg" id="cp-office">${['onlyoffice','libreoffice'].map(v=>`<button data-v="${v}" class="${(SETTINGS.defaultOfficeSuite||'onlyoffice')===v?'on':''}">${v==='onlyoffice'?'ONLYOFFICE':'LibreOffice'}</button>`).join('')}</span></div></div><p class="set-note">打开文档 / 表格 / 演示文件时优先用哪个套件；只装了一个就用那个，都没装会先请你下载。图片装了 ImageGlass 就用它打开，代码和文本装了 VSCodium 就用它打开，没装走内置查看器。</p></div>`;
   G('set-main').querySelectorAll('[data-openapp]').forEach(b=>b.onclick=()=>openApp(b.dataset.openapp));
   G('cp-sbie-go').onclick=()=>goto('isolation');
   try{ const sb=await api('/api/sandbox/status'); const el=G('cp-sbie'); if(el) el.textContent=!sb.supported?'当前平台不支持':(sb.installed?('已安装 · v'+(sb.version||'?')+(sb.serviceState==='RUNNING'?' · 运行中':' · 服务异常')):'未安装'); }catch(e){}
   try{ const ff=await api('/api/firefox'); const el=G('cp-ff'); if(el) el.textContent=ff.installed?('已安装 · v'+(ff.version||'?')):'未安装'; }catch(e){}
   try{ const oc=await api('/api/openclaw/status'); const el=G('cp-oc'); if(el) el.textContent=oc.installed?('已安装 · v'+(oc.version||'?')+(oc.running?' · 运行中':'')+(oc.diskBytes?' · '+fmtB(oc.diskBytes):'')):'未安装'; }catch(e){}
   /* v0.51 productivity components: full management rows live on
      this page (these five have no app page of their own). */
   const paintProd=()=>{
    const card=G('cp-prod'); if(!card) return;
    card.innerHTML=PRODAPPS.length?PRODAPPS.map(c=>{
     let st,btns='';
     if(c.installed){ st='已安装 · v'+(c.version||'?')+(c.diskBytes?' · '+fmtB(c.diskBytes):'')+(c.updateAvailable?' · 可更新到 v'+c.available:'');
      btns=`<button class="btn blue" data-prod-open="${c.id}">打开</button>`+(c.updateAvailable?`<button class="btn gray" data-prod-in="${c.id}">更新</button>`:'')+`<button class="btn gray" data-prod-un="${c.id}">卸载</button>`;
     } else if(prodBusy(c)){ const p=prodPct(c); st=c.phase==='extracting'?'正在释放…':('下载中 '+p+'%'+(c.size?' · '+fmtB(c.progress||0)+' / '+fmtB(c.size):'')); btns=`<span class="smProg"><i style="width:${p}%"></i></span>`; }
     else if(c.phase==='error'){ st='⚠ '+(c.message||'上次没装成'); btns=`<button class="btn blue" data-prod-in="${c.id}">重试下载</button>`; }
     else { st='未下载'+(c.available?(' · v'+c.available+' · '+fmtB(c.availableSize||0)):''); btns=`<button class="btn blue" data-prod-in="${c.id}">下载</button>`; }
     return `<div class="sm-row"><span>${esc(c.name)}<div style="color:#9a9aa2;font-size:11.5px">${esc(c.blurb||'')}</div></span><span class="row"><span class="set-val">${st}</span>${btns}</span></div>`;
    }).join(''):'<div class="sm-row"><span>读取不到组件列表（守护进程没回应）</span></div>';
    card.querySelectorAll('[data-prod-in]').forEach(b=>b.onclick=async()=>{ await api('/api/components/install',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({id:b.dataset.prodIn})}); kickProd(); });
    card.querySelectorAll('[data-prod-open]').forEach(b=>b.onclick=async()=>{ const r=await api('/api/components/launch',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({id:b.dataset.prodOpen})}); if(r&&r.error) alert(r.error); });
    card.querySelectorAll('[data-prod-un]').forEach(b=>b.onclick=async()=>{ const c=PRODAPPS.find(x=>x.id===b.dataset.prodUn); if(!c) return; if(!confirm('卸载 '+c.name+'？\n程序和它在组件目录里的数据会一起删掉，不可恢复。')) return; const r=await api('/api/components/uninstall',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({id:c.id})}); if(r&&r.error){ alert(r.error); return; } await loadProdApps(); renderDesk(); paintProd(); });
   };
   const kickProd=()=>{ if(prodIv) return; prodIv=setInterval(async()=>{ if(curPage!=='components'||!G('cp-prod')){ clearInterval(prodIv); prodIv=null; return; } const wasBusy=PRODAPPS.some(prodBusy); await loadProdApps(); paintProd(); if(!PRODAPPS.some(prodBusy)){ clearInterval(prodIv); prodIv=null; if(wasBusy) renderDesk(); } },2000); };
   await loadProdApps(); paintProd();
   if(PRODAPPS.some(prodBusy)) kickProd();
   const seg=G('cp-office'); if(seg) seg.querySelectorAll('button').forEach(b=>b.onclick=async()=>{ const r=await saveSetting({defaultOfficeSuite:b.dataset.v}); if(r&&r.error){ alert('保存失败：'+r.error); return; } seg.querySelectorAll('button').forEach(x=>x.classList.toggle('on',x===b)); });
  };
  const paintIsolation=()=>{
   if(sbxIv){ clearInterval(sbxIv); sbxIv=null; }
   G('set-main').innerHTML=`<div class="set-page"><div class="set-sec">应用隔离</div><div class="set-desc">没有便携版的软件（比如微信），可以装进一个<b style="color:#fff">隔离盒子</b>：安装时它往 Program Files、注册表、AppData 写的东西，全被隔离组件（Sandboxie Plus）截进 EXE 旁的 <b style="color:#fff">Data/sandbox</b>。程序和数据跟着文件夹走，删掉盒子就是真卸载，宿主电脑不留东西。</div><div class="sm-card" id="sbx-card"><div class="sm-row"><span>读取中…</span></div></div><div class="set-sec">先说清楚</div><div class="set-desc">① 安装组件要过<b style="color:#fff">一次管理员确认</b>（驱动必须进系统），驱动和服务会常驻在这台电脑上，直到你在这里卸载组件为止——所以这个功能适合<b style="color:#fff">你自己的电脑</b>，不要在公用、共享电脑上装。<br>② 盒内应用<b style="color:#fff">默认直连网络</b>。应用自己支持填代理的（如 Telegram），在应用里把代理指向 127.0.0.1:2080 就能走终端的节点；把不认代理的应用的全部流量强制掰进节点，是 Sandboxie 的付费功能，本产品不提供。<br>③ 要往系统里装服务/驱动的软件、带强 DRM 或反作弊的软件，可能在盒子里装不上或跑不起来，这是这类软件的性质，装的时候会如实报错。<br>④ 换电脑时盒子跟着文件夹走，新电脑上把组件装一次就能接着用；个别软件有机器绑定授权或账号风控，可能要重新激活或扫码登录，那是软件自己的规矩。<br>⑤ 组件安装包（约 24 MB）已经<b style="color:#fff">内置在程序里</b>，没网也能装；只有内置包释放失败时才联网下载，下载后照样验 SHA-256。<br>⑥ 沙盒默认只隔离「写」、不隔离「读」：盒子里没有的文件，应用会<b style="color:#fff">读到宿主机上的真实文件</b>。所以对<b style="color:#fff">数据封堵名单</b>内的应用（当前：微信家族），它们的数据目录被设为「只认盒内」：宿主机那份数据对它们不存在，盒内从一份全新数据开始、之后也只长在盒子里，换电脑跟着文件夹走。名单外的应用写入始终隔离，但读取仍可能看到宿主机上的同名文件，装之前心里有数。</div><div class="set-sec">盒子</div><div class="sm-card" id="sbx-boxcard"><div class="sm-row"><span>读取中…</span></div></div><div class="set-sec">盒内应用</div><div class="set-desc">在「应用商店」里用隔离安装添加；登记后图标直接上桌面，双击打开。在这里也能启动和移除。</div><div class="sm-card" id="sbx-apps"><div class="sm-row"><span>读取中…</span></div></div></div>`;
   const paint=async()=>{
    let st; try{ st=await api('/api/sandbox/status'); }catch(e){ return; }
    const card=G('sbx-card'); if(!card) return;
    const comp=st.component||{};
    let h='';
    if(!st.supported){
     h=`<div class="sm-row"><span>当前平台</span><span class="set-val">不支持（应用隔离需要 Windows）</span></div>`;
    } else if(comp.busy){
     const pc=comp.size?Math.min(99,Math.floor((comp.progress||0)/comp.size*100)):0;
     h=`<div class="sm-row"><span>组件状态</span><span class="set-val">${esc(comp.message||'处理中…')}</span></div>`+(comp.size?`<div class="sm-row"><span>进度</span><span class="set-val">${pc}%</span></div>`:'')+`<p class="set-note">如果屏幕上弹出了系统的管理员确认窗口，点「是」才会继续。</p>`;
    } else if(!st.installed){
     h=`<div class="sm-row"><span>组件状态</span><span class="set-val">未安装</span></div><div class="sm-row"><span>组件</span><span class="set-val">Sandboxie Plus v${esc(st.pinnedVersion||'')}</span></div><div class="sm-row"><span>安装</span><span class="row"><button class="btn" id="sbx-install">安装组件</button></span></div><p class="set-note">点安装后会弹出一次系统管理员确认（UAC），同意后全程自动，不用再点别的。</p>`;
     if(comp.phase==='error') h+=`<p class="set-note" style="color:#ff9d97">${esc(comp.message||'上次安装没成功')}</p>`;
    } else {
     const ready=st.serviceState==='RUNNING'&&st.iniPlaced;
     h=`<div class="sm-row"><span>组件状态</span><span class="set-val">${ready?'已就绪':(st.serviceState!=='RUNNING'?'服务异常':'盒子待落位')}</span></div>`
      +`<div class="sm-row"><span>版本</span><span class="set-val">Sandboxie Plus v${esc(st.version||'?')}</span></div>`
      +`<div class="sm-row"><span>服务 / 驱动</span><span class="set-val">${esc(st.serviceState||'—')} / ${esc(st.driverState||'—')}</span></div>`
      +(st.metaSource?`<div class="sm-row"><span>组件来源</span><span class="set-val">${st.metaSource==='embedded'?'内置组件包':'在线下载'}${st.metaAt?(' · '+esc(st.metaAt)):''}</span></div>`:'')
      +`<div class="sm-row"><span>管理</span><span class="row"><button class="btn gray" id="sbx-repair">修复</button><button class="btn red" id="sbx-uninstall">卸载组件</button></span></div>`
      +(ready?'':`<p class="set-note">修复会重新启动服务、把盒子按当前 Data 位置落位（可能要再过一次管理员确认）。</p>`);
     if(comp.phase==='error') h+=`<p class="set-note" style="color:#ff9d97">${esc(comp.message||'')}</p>`;
     if(comp.phase==='done'&&comp.message) h+=`<p class="set-note">${esc(comp.message)}</p>`;
    }
    card.innerHTML=h;
    const bi=G('sbx-install'); if(bi) bi.onclick=async()=>{ bi.disabled=true; const r=await api('/api/sandbox/component/install',{method:'POST'}); if(r&&r.error) alert(r.error); paint(); };
    const br=G('sbx-repair'); if(br) br.onclick=async()=>{ br.disabled=true; br.textContent='修复中…'; const r=await api('/api/sandbox/component/repair',{method:'POST'}); if(r&&r.error) alert('修复没成功：'+r.error); else alert('修复完成：'+(r.message||'')); paint(); };
    const bu=G('sbx-uninstall'); if(bu) bu.onclick=async()=>{ if(!confirm('卸载隔离组件？\n驱动和服务会从这台电脑移除（要过一次管理员确认）。盒子里已装的应用和数据保留在 Data/sandbox，以后重装组件还能接着用。')) return; const r=await api('/api/sandbox/component/uninstall',{method:'POST'}); if(r&&r.error) alert(r.error); paint(); };
    const bc=G('sbx-boxcard'); if(bc){
     bc.innerHTML=`<div class="sm-row"><span>盒子占用</span><span class="set-val">${fmtB(st.boxBytes||0)}</span></div><div class="sm-row"><span>盒子位置</span><span class="set-val" style="font-size:11.5px">${esc(st.boxRoot||'')}</span></div><div class="sm-row"><span>清空盒子</span><span class="row"><button class="btn red" id="sbx-clear">清空盒子</button></span></div><p class="set-note">清空 = 盒内全部应用和数据一起删掉，等于把它们全部卸载。组件本身不卸载。</p>`;
     const cl=G('sbx-clear'); if(cl) cl.onclick=async()=>{ if(!confirm('真的要清空盒子？盒内全部应用和数据都会删掉，不可恢复。')) return; if(!confirm('再确认一次：盒子里装的软件（比如微信和它的聊天数据）全没了。继续？')) return; const r=await api('/api/sandbox/clear',{method:'POST'}); if(r&&r.error) alert(r.error); await loadBoxApps(); renderDesk(); paint(); };
    }
    const ac=G('sbx-apps'); if(ac){
     const apps=st.apps||[];
     ac.innerHTML=apps.length?apps.map(a=>`<div class="sm-row"><span>${esc(a.name)}${a.sealed?' <span class="tag">全新数据 · 读不到宿主那份</span>':''}${a.ghost?' <span class="tag">登记已失效（程序文件不在了）</span>':''}<br><span style="font-size:12px;opacity:.65">${esc(a.exe)}</span></span><span class="row"><button class="btn gray" data-sbx-open="${esc(a.id)}">打开</button><button class="btn gray" data-sbx-del="${esc(a.id)}">移除</button></span></div>`).join(''):'<div class="sm-row"><span>还没有隔离应用</span></div>';
     ac.querySelectorAll('[data-sbx-open]').forEach(b=>b.onclick=()=>launchBoxApp(b.dataset.sbxOpen));
     ac.querySelectorAll('[data-sbx-del]').forEach(b=>b.onclick=async()=>{ const a=apps.find(x=>x.id===b.dataset.sbxDel); if(!a) return; if(!confirm('移除 '+a.name+'？\n它的程序和数据（在盒子里的那一份）会一起删掉，不可恢复。')) return; const r=await api('/api/sandbox/remove',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({id:a.id})}); if(r&&r.error) alert(r.error); await loadBoxApps(); renderDesk(); paint(); });
    }
   };
   paint();
   sbxIv=setInterval(()=>{ if(curPage!=='isolation'||!document.getElementById('sbx-card')){ clearInterval(sbxIv); sbxIv=null; return; } paint(); },2500);
  };
  const STCOLORS={program:'#8e8e93',chrome:'#0a84ff',firefoxProfile:'#ff7139',openclaw:'#ff453a',apps:'#30d158',files:'#ff9f0a',logs:'#ffd60a',wallpaper:'#bf5af2',other:'#48484a'};
  const paintStorage=async()=>{
   G('set-main').innerHTML=`<div class="set-page"><div class="set-sec">存储空间</div><div class="set-desc" id="st-total">正在盘点…</div><div id="st-body"></div><div class="set-sec">一键清理缓存</div><div class="set-desc">只清纯缓存：Chrome 档案里的 Cache / Code Cache / GPUCache 等缓存目录、Firefox 的 cache2、已完成更新的暂存包、核心的旧日志文件。<b style="color:#fff">绝不碰</b>登录、Cookie、书签、历史和任何配置。浏览器开着时不会执行。</div><div class="row"><button class="btn" id="st-clean">一键清理缓存</button><span class="tag" id="st-clean-msg"></span></div></div>`;
   const paintUsage=async()=>{ const u=await api('/api/storage/usage'); usageCache=u; const body=G('st-body'); if(!body) return; const tot=G('st-total'); if(tot) tot.textContent=`这个文件夹共占用 ${fmtB(u.totalBytes)}（其中 Data ${fmtB(u.dataBytes)}）。盘点用时 ${u.tookMs} ms。`; const segs=(u.groups||[]).filter(g=>g.bytes>0).map(g=>`<i style="background:${STCOLORS[g.key]||'#666'};width:${Math.max(0.4,(g.bytes/Math.max(1,u.totalBytes)*100))}%"></i>`).join(''); body.innerHTML=`<div class="set-stack">${segs}</div><div class="set-legend">${(u.groups||[]).map(g=>`<span><i style="background:${STCOLORS[g.key]||'#666'}"></i>${esc(g.name)}</span>`).join('')}</div><div class="sm-card">${(u.groups||[]).map(g=>`<div class="sm-row"><span>${esc(g.name)}</span><span class="set-val">${fmtB(g.bytes)}</span></div>`).join('')}</div>`; };
   G('st-clean').onclick=async()=>{ const msg=G('st-clean-msg'); if(!confirm('清理纯缓存？登录、书签、历史都不会动。')) return; msg.textContent='清理中…'; const r=await api('/api/storage/cleanup',{method:'POST'}); if(r.error){ msg.textContent='没清成：'+r.error; return; } msg.textContent=`完成，释放 ${fmtB(r.freedBytes)}（${(r.removed||[]).length} 项）`; usageCache=null; paintUsage(); };
   paintUsage();
  };
  const paintSecurity=()=>{
   if(secIv){ clearInterval(secIv); secIv=null; }
   const s=SETTINGS||SETDEF;
   G('set-main').innerHTML=`<div class="set-page"><div class="set-sec">远程协助</div><div class="set-desc">让被你配对的操作者（比如你的 AI 助手）通过你自己的私有加密网络连进来，远程看状态、查日志，替你定位问题。通道是内嵌在守护进程里的 Tailscale 组网：不装任何软件、不动系统，节点身份存在 Data 里跟着文件夹走。对方发出的<b style="color:#fff">每一个请求都会记入控制台</b>（模块「远程协助」），配对、改档、撤销只能在这一页操作。</div><div class="sm-card"><div class="sm-row"><span>远程协助</span><label class="sm-sw"><input type="checkbox" id="sec-remote" ${s.remoteEnabled?'checked':''}><i></i></label></div><div class="sm-row"><span>组网状态</span><span class="set-val" id="sec-state">读取中…</span></div><div id="sec-netrows"></div></div><div id="sec-pendwrap"></div><div class="set-sec">操作者</div><div class="set-desc">只有名单里的人能连进来，按人单独授权、单独撤销。新批准的默认是<b style="color:#fff">只读档</b>：只能看状态和日志；升到<b style="color:#fff">协助档</b>后，还能改设置（白名单内）、启停节点与小龙虾、下载并应用更新。</div><div class="sm-card" id="sec-ops"><div class="sm-row"><span>读取中…</span></div></div><div class="set-sec">安全边界</div><div class="set-desc">任何档位都<b style="color:#fff">不会</b>对远程提供：你的文件内容、SSH 私钥与密码、节点分享链接与订阅地址、小龙虾令牌原文、更新令牌。退出系统、安装/卸载应用、配对管理也不对远程开放——这些入口在远程侧根本不存在。</div><div class="row"><button class="btn red" id="sec-clear">断开并清除全部配对</button></div><div class="set-sec">保险库</div><div class="set-desc">把<b style="color:#fff">整个 Data 目录</b>加密：终端机退出或锁屏时，导入的文件、浏览器资料、节点与订阅、SSH 密钥、商店应用本体、盒子里的一切都变成密文，拷走也只是乱码；下次打开终端机，输对密码才解锁。锁屏会先关闭终端机内打开的应用再上锁（和 Mac 不一样——免驱动方案保不住活应用，如实说明）。还有一句实话：<b style="color:#fff">解锁运行期间，数据在磁盘上是明文</b>，这是免安装、免驱动方案的边界；密码<b style="color:#fff">忘记无法找回，没有后门</b>。</div><div class="sm-card" id="sec-vault"><div class="sm-row"><span>读取中…</span></div></div></div>`;
   const NETTXT={off:'已关闭',starting:'组网启动中…',needslogin:'等待登录（见下方）',online:'已组网，可被配对的操作者访问',error:'组网出错',unsupported:'当前平台不支持（需要 64 位构建）'};
   const paint=async()=>{
    let st, pr;
    try{ st=await api('/api/remote/state'); pr=await api('/api/remote/pairing'); }catch(e){ return; }
    const sw=G('sec-remote'); if(!sw) return;
    if(document.activeElement!==sw) sw.checked=!!st.enabled;
    const stEl=G('sec-state'); if(stEl) stEl.textContent=!st.supported?'当前平台不支持':(st.enabled?(NETTXT[st.netState]||st.netState):'已关闭');
    const nr=G('sec-netrows'); if(nr){
     let h='';
     if(st.enabled&&st.netState==='needslogin'&&st.authURL) h+=`<div class="sm-row"><span>登录 Tailscale</span><span class="row"><button class="btn" id="sec-openlogin">打开登录页</button></span></div><p class="set-note">用你自己的 Tailscale 账号登录一次，终端会以设备名「mj-terminal」加入你的网络。登录页在终端自己的隔离浏览器里打开；也可以手动复制下面的链接到隔离浏览器里打开：<span style="word-break:break-all">${esc(st.authURL)}</span></p>`;
     if(st.enabled&&st.netState==='online') h+=`<div class="sm-row"><span>设备名</span><span class="set-val">${esc(st.device||'—')}</span></div><div class="sm-row"><span>网络地址</span><span class="set-val">${esc((st.ips||[]).join(' · ')||'—')}</span></div><div class="sm-row"><span>远程入口</span><span class="set-val">:${st.port}（仅你的私有网络内可达）</span></div>`;
     if(st.enabled&&st.err) h+=`<p class="set-note">${esc(st.err)}</p>`;
     nr.innerHTML=h;
     const ol=G('sec-openlogin'); if(ol) ol.onclick=async()=>{ ol.disabled=true; const r=await api('/api/remote/openlogin',{method:'POST'}).catch(()=>null); if(r&&r.error) alert(r.error); ol.disabled=false; };
    }
    const pw=G('sec-pendwrap'); if(pw){
     const pend=pr.pending||[];
     pw.innerHTML=pend.length?`<div class="set-sec">待配对</div><div class="set-desc">下面这些设备刚刚敲过远程入口的门。批准前先<b style="color:#fff">核对配对码</b>：把这串数字告诉对方，对方确认一致，才是本人在敲门。名字可以改成你认得的称呼。</div><div class="sm-card">${pend.map(p=>`<div class="sm-row"><span>${esc(p.device||p.name)}<br><span style="font-size:12px;opacity:.65">配对码</span> <b style="font-size:16px;letter-spacing:2px">${esc(p.code)}</b></span><span class="row"><input class="txt" data-pend-name="${esc(p.id)}" value="${esc(p.name)}" style="min-width:110px;flex:none"><button class="btn" data-pend-ok="${esc(p.id)}">批准</button><button class="btn gray" data-pend-no="${esc(p.id)}">拒绝</button></span></div>`).join('')}</div>`:'';
     pw.querySelectorAll('[data-pend-ok]').forEach(b=>b.onclick=async()=>{ const inp=pw.querySelector(`[data-pend-name="${CSS.escape(b.dataset.pendOk)}"]`); const r=await api('/api/remote/pairing',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'approve',id:b.dataset.pendOk,name:inp?inp.value:''})}); if(r&&r.error) alert(r.error); paint(); });
     pw.querySelectorAll('[data-pend-no]').forEach(b=>b.onclick=async()=>{ await api('/api/remote/pairing',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'reject',id:b.dataset.pendNo})}); paint(); });
    }
    const oc=G('sec-ops'); if(oc){
     const ops=pr.operators||[];
     oc.innerHTML=ops.length?ops.map(o=>`<div class="sm-row"><span>${esc(o.name)}<br><span style="font-size:12px;opacity:.65">${esc(o.device||'')} · 配对于 ${esc(o.pairedAt||'')}${pr.lastSeen&&pr.lastSeen[o.id]?(' · 最近活动 '+esc(pr.lastSeen[o.id])):''}</span></span><span class="row"><span class="sm-seg"><button data-tier="read" data-op="${esc(o.id)}" class="${o.tier!=='assist'?'on':''}">只读</button><button data-tier="assist" data-op="${esc(o.id)}" class="${o.tier==='assist'?'on':''}">协助</button></span><button class="btn gray" data-revoke="${esc(o.id)}">撤销</button></span></div>`).join(''):'<div class="sm-row"><span>还没有配对的操作者</span></div>';
     oc.querySelectorAll('[data-tier]').forEach(b=>b.onclick=async()=>{ const r=await api('/api/remote/pairing',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'tier',id:b.dataset.op,tier:b.dataset.tier})}); if(r&&r.error) alert(r.error); paint(); });
     oc.querySelectorAll('[data-revoke]').forEach(b=>b.onclick=async()=>{ if(!confirm('撤销这位操作者的远程资格？对方立刻就连不进来了，之后想再给要重新配对。')) return; await api('/api/remote/pairing',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'revoke',id:b.dataset.revoke})}); paint(); });
    }
   };
   const paintVault=async()=>{
    const box=G('sec-vault'); if(!box) return;
    let st; try{ st=await api('/api/vault/status'); }catch(e){ box.innerHTML='<div class="sm-row"><span>读取失败</span></div>'; return; }
    if(!G('sec-vault')) return;
    if(!st.enabled){
     box.innerHTML=`<div class="sm-row"><span>保险库</span><span class="set-val">未启用</span></div><div class="sm-row"><span>启用后，退出 / 锁屏即全量加密整个 Data</span><button class="btn" id="vlt-enable-btn">启用保险库…</button></div><div id="vlt-form"></div>`;
     G('vlt-enable-btn').onclick=()=>{
      G('vlt-form').innerHTML=`<div class="sm-row"><span>设置密码（至少 6 位）</span><input class="txt" type="password" id="vlt-pw1" style="min-width:170px;flex:none" autocomplete="new-password"></div><div class="sm-row"><span>再输一遍</span><input class="txt" type="password" id="vlt-pw2" style="min-width:170px;flex:none" autocomplete="new-password"></div><div class="sm-row"><span style="opacity:.75;font-size:12.5px">建议用一句好记又长的口令。忘记密码 = 数据无法找回，没有后门。启用后数据先保持明文，第一次退出或点「立即上锁」时才加密。</span><button class="btn red" id="vlt-enable-go">确认启用</button></div><div class="sm-row" id="vlt-msg"></div>`;
      G('vlt-enable-go').onclick=async()=>{
       const p1=G('vlt-pw1').value, p2=G('vlt-pw2').value, msg=G('vlt-msg');
       if(p1.length<6){ msg.innerHTML='<span style="color:#ff9f0a">密码至少 6 位</span>'; return; }
       if(p1!==p2){ msg.innerHTML='<span style="color:#ff9f0a">两次输入不一致</span>'; return; }
       const r=await api('/api/vault/enable',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({password:p1})});
       if(r&&r.error){ msg.innerHTML='<span style="color:#ff453a">'+esc(r.error)+'</span>'; return; }
       paintVault();
      };
     };
     return;
    }
    box.innerHTML=`<div class="sm-row"><span>保险库</span><span class="set-val">已启用</span></div><div class="sm-row"><span>立即上锁（关闭打开的应用并加密全部数据）</span><button class="btn" id="vlt-lock">立即上锁</button></div><div class="sm-row"><span>修改密码（只重包裹主密钥，秒级完成，不重加密数据）</span><button class="btn gray" id="vlt-chpw-btn">修改密码…</button></div><div id="vlt-form"></div><div class="sm-row"><span>停用保险库（需输密码；之后退出不再加密）</span><span class="row"><input class="txt" type="password" id="vlt-dis-pw" placeholder="当前密码" style="min-width:140px;flex:none" autocomplete="off"><button class="btn gray" id="vlt-disable">停用</button></span></div><div class="sm-row" id="vlt-msg"></div>`;
    G('vlt-lock').onclick=async()=>{ const r=await api('/api/vault/lock',{method:'POST'}); if(r&&r.error){ alert(r.error); return; } if(window.__vaultShowLock) window.__vaultShowLock(); };
    G('vlt-chpw-btn').onclick=()=>{
     G('vlt-form').innerHTML=`<div class="sm-row"><span>旧密码</span><input class="txt" type="password" id="vlt-old" style="min-width:150px;flex:none" autocomplete="off"></div><div class="sm-row"><span>新密码（至少 6 位）</span><input class="txt" type="password" id="vlt-new" style="min-width:150px;flex:none" autocomplete="new-password"></div><div class="sm-row"><span></span><button class="btn" id="vlt-chpw-go">确认修改</button></div>`;
     G('vlt-chpw-go').onclick=async()=>{
      const r=await api('/api/vault/changepw',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({old:G('vlt-old').value,new:G('vlt-new').value})});
      const msg=G('vlt-msg'); if(msg) msg.innerHTML=r&&r.error?('<span style="color:#ff453a">'+esc(r.error)+'</span>'):'<span style="color:#30d158">密码已修改</span>';
      if(!(r&&r.error)) G('vlt-form').innerHTML='';
     };
    };
    G('vlt-disable').onclick=async()=>{ if(!confirm('停用保险库？当前数据保持明文，之后的退出 / 锁屏不再加密。')) return; const r=await api('/api/vault/disable',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({password:G('vlt-dis-pw').value})}); if(r&&r.error){ alert(r.error); return; } paintVault(); };
   };
   paintVault();
   G('sec-remote').onchange=async e=>{ const r=await saveSetting({remoteEnabled:e.target.checked}); if(r&&r.error){ alert('保存失败：'+r.error); e.target.checked=!e.target.checked; return; } setTimeout(paint,700); };
   G('sec-clear').onclick=async()=>{ if(!confirm('断开并清除全部配对？所有操作者立刻失去资格，远程协助同时关闭。')) return; await api('/api/remote/pairing',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'clear'})}); await loadSettings(); const sw=G('sec-remote'); if(sw) sw.checked=false; paint(); };
   paint();
   secIv=setInterval(()=>{ if(curPage!=='security'||!document.getElementById('sec-state')){ clearInterval(secIv); secIv=null; return; } paint(); },3000);
  };
  const paintGeneral=()=>{
   const s=SETTINGS||SETDEF;
   const opts=[[0,'不自动'],[15,'15 分钟'],[30,'30 分钟'],[60,'60 分钟']];
   G('set-main').innerHTML=`<div class="set-page"><div class="set-sec">守护进程</div><div class="set-desc">节点没在运行、也没人操作时，守护进程多久后自动退出。节点或小龙虾在跑时不会自动退出。改完立刻生效。</div><div class="sm-card"><div class="sm-row"><span>空闲自动退出</span><span class="sm-seg" id="gn-idle">${opts.map(([v,n])=>`<button data-v="${v}" class="${s.idleExitMinutes===v?'on':''}">${n}</button>`).join('')}</span></div></div><div class="set-sec">老板键（全局热键）</div><div class="set-desc">终端机的窗口在最前面时按一下，整套窗口立刻隐藏（守护进程和节点继续在后台运行）；在其他任何界面（包括游戏里）按一下，终端机马上回到最前面并打开聚焦搜索。隐藏之后，再按一次这个键、或点系统托盘里的终端机图标，就能回来。保险库锁定时唤出的是密码屏。热键由守护进程向系统注册，退出终端机即注销，不留常驻。</div><div class="sm-card"><div class="sm-row"><span>老板键</span><span class="row"><span class="sm-seg" id="gn-hotkey">${['F8','F9','F7','F6','F12','off'].map(k=>`<button data-k="${k}" class="${(s.summonHotkey||'F8')===k?'on':''}">${k==='off'?'关闭':k}</button>`).join('')}</span><button class="btn gray" id="gn-hk-rec">自定义…</button></span></div><div class="sm-row"><span>当前按键</span><span class="set-val" id="gn-hk-cur">${esc(s.summonHotkey||'F8')}</span></div><div class="sm-row"><span>注册状态</span><span class="set-val" id="gn-hk-state">读取中…</span></div></div><p class="set-note">「自定义…」点开后直接按想要的新组合：单个 F1–F12 可以，字母/数字要加 Ctrl / Alt / Shift（裸字母会吃掉正常打字），Win 键组合不能用。状态显示「被占用」说明这个组合已被别的程序抢注（游戏、录屏、输入法常见），设置不会回滚，但这个键暂时按不动，换一个即可；改键即时重新注册，不用重启。</p><div class="set-sec">退出</div><div class="set-desc">关掉桌面窗口只是隐藏，后台和节点继续跑；再双击 MJ-System.exe 回来。真正断开只有点这个「退出系统」。个人数据都在 EXE 旁的 Data 文件夹，用完带走或删掉即可。</div><div class="sm-card"><div class="sm-row"><span>退出时关闭打开的应用</span><label class="sm-sw"><input type="checkbox" id="gn-closeapps" ${s.exitClosesApps!==false?'checked':''}><i></i></label></div></div><p class="set-note">开启时，从设置退出、托盘退出或顶栏「关机」，会先请终端机打开的应用（隔离浏览器、商店应用、盒内应用）自己关，等几秒还不退的才结束进程，不留后台。更新交接不关应用；空闲自动退出也不关——除非启用了保险库（上锁前必须先关应用）。</p><div class="row"><button class="btn red" id="gn-quit">退出系统（停节点 + 关后台）</button></div></div>`;
   G('gn-idle').querySelectorAll('button').forEach(b=>b.onclick=async()=>{ const r=await saveSetting({idleExitMinutes:+b.dataset.v}); if(r&&r.error){ alert('保存失败：'+r.error); return; } G('gn-idle').querySelectorAll('button').forEach(x=>x.classList.toggle('on',x===b)); });
   /* v0.50/v0.52: boss-key row -- preset keys + a "自定义…"
      recorder + the daemon's live registration state
      (GET /api/hotkey/status, three-state since v0.52). */
   const hkPaint=async()=>{ const el=G('gn-hk-state'); if(!el) return; try{ const st=await api('/api/hotkey/status'); if(!document.getElementById('gn-hk-state')) return; const cur=G('gn-hk-cur'); if(cur&&st.key) cur.textContent=st.key; el.textContent=st.status==='off'?'已关闭':(st.status==='unsupported'?'当前平台不支持':(st.status==='registered'?('已注册（'+st.key+'）'):(st.status==='busy'?('被占用（'+st.key+'）：'+(st.note||'已被其他程序注册')):('未注册：'+(st.note||'未知原因'))))); }catch(e){ el.textContent='读取失败'; } };
   hkPaint(); setTimeout(hkPaint,600);
   const hkMarkSeg=()=>{ const cur=(SETTINGS&&SETTINGS.summonHotkey)||'F8'; G('gn-hotkey').querySelectorAll('button').forEach(x=>x.classList.toggle('on',x.dataset.k===cur)); const c=G('gn-hk-cur'); if(c) c.textContent=cur; };
   G('gn-hotkey').querySelectorAll('button').forEach(b=>b.onclick=async()=>{ const r=await saveSetting({summonHotkey:b.dataset.k}); if(r&&r.error){ alert('保存失败：'+r.error); return; } hkMarkSeg(); setTimeout(hkPaint,350); });
   /* Recorder: capture the next keydown as the new combo. The
      daemon remains the authority (its parser validates and
      names any rejection); the checks mirrored here only save
      a round trip for the obvious cases. Esc cancels. */
   const hkRec=G('gn-hk-rec');
   hkRec.onclick=()=>{
    if(hkRec.dataset.rec==='1') return;
    hkRec.dataset.rec='1'; const oldLabel=hkRec.textContent; hkRec.textContent='按下组合键…（Esc 取消）';
    const done=()=>{ hkRec.dataset.rec=''; hkRec.textContent=oldLabel; document.removeEventListener('keydown',cap,true); };
    const cap=async e=>{
     if(e.key==='Escape'){ e.preventDefault(); done(); return; }
     if(['Control','Shift','Alt','Meta'].includes(e.key)) return; /* wait for the primary key */
     e.preventDefault(); e.stopPropagation();
     if(e.metaKey){ alert('不能用 Win 键组合（系统保留，视图也录不到）'); done(); return; }
     let key='';
     if(/^F([1-9]|1[0-2])$/.test(e.key)) key=e.key;
     else if(e.key&&e.key.length===1&&/[a-zA-Z0-9]/.test(e.key)) key=e.key.toUpperCase();
     else { alert('这个键不能用作老板键（可用 F1–F12、字母、数字）'); done(); return; }
     const mods=[]; if(e.ctrlKey) mods.push('Ctrl'); if(e.altKey) mods.push('Alt'); if(e.shiftKey) mods.push('Shift');
     if(!mods.length&&!/^F/.test(key)){ alert('裸字母/数字键会吃掉正常打字，请加 Ctrl / Alt / Shift 一起按'); done(); return; }
     const combo=mods.concat([key]).join('+');
     done();
     const r=await saveSetting({summonHotkey:combo});
     if(r&&r.error){ alert('保存失败：'+r.error); return; }
     hkMarkSeg(); setTimeout(hkPaint,350);
    };
    document.addEventListener('keydown',cap,true);
   };
   G('gn-quit').onclick=async()=>{ await api('/api/shutdown',{method:'POST'}); document.body.innerHTML='<div style="position:fixed;inset:0;display:flex;align-items:center;justify-content:center;font-size:20px;color:#fff;text-shadow:0 2px 10px #000">已退出，节点和后台都停了</div>'; };
   G('gn-closeapps').onchange=async e=>{ const r=await saveSetting({exitClosesApps:e.target.checked}); if(r&&r.error){ alert('保存失败：'+r.error); e.target.checked=!e.target.checked; } };
  };
  const paintAbout=async()=>{
   G('set-main').innerHTML=`<div class="set-page"><div class="set-sec">关于</div><div class="sm-card" id="ab-card"><div class="sm-row"><span>读取中…</span></div></div><div class="set-sec">出问题了？</div><div class="set-desc">先去「控制台」看红色报错，或导出诊断包发出来定位。</div><div class="row"><button class="btn gray" id="ab-console">打开控制台</button><a class="btn gray" href="/api/diag/export" style="text-decoration:none">导出诊断包</a></div></div>`;
   G('ab-console').onclick=()=>openApp('console');
   try{ const si=await api('/api/system'); const u=await storageUsageOnce(); const card=G('ab-card'); if(card){ const rows={版本:'v'+si.version,系统:si.os+' / '+si.arch,电脑名:si.hostname,'CPU 核数':si.numCPU,程序目录:si.exeDir,数据目录:si.dataDir,文件夹总占用:fmtB(u.totalBytes)}; if(si.dataFallback) rows['⚠ 数据不在程序文件夹']='程序文件夹不可写，数据暂存在这台电脑的 LocalAppData 里——壁纸、导入的图片、组件图标等搬家时不会跟随。把程序文件夹放到可写位置（如桌面）再打开即可恢复便携。'; card.innerHTML=Object.entries(rows).map(([k,v])=>`<div class="sm-row"><span>${k}</span><span class="set-val">${esc(String(v))}</span></div>`).join(''); } }catch(e){}
  };
  const paintUpdate=()=>{
   if(updIv){ clearInterval(updIv); updIv=null; }
   G('set-main').innerHTML=`<div class="set-page"><div class="set-sec">软件更新</div><div class="set-desc">当前版本 <b style="color:#fff" id="upd-cur">…</b>。后台启动时会自动检查新版并预下载，你也可以手动检查；点「重启并更新」完成升级（更新时节点断开几秒，自动重连，节点和数据都不动）。仓库是公开的，<b style="color:#fff">不需要令牌</b>；下面这个令牌框只在仓库改回私有后才用得上，平时留空即可。</div><div class="sm-card"><div class="sm-row"><span>更新令牌（公开仓库不用填）</span><span class="row"><input class="txt" id="upd-token" type="password" placeholder="令牌" autocomplete="off" style="min-width:170px;flex:none"><button class="btn gray" id="upd-token-save">保存令牌</button></span></div><div class="sm-row"><span id="upd-status">…</span><span class="row"><button class="btn" id="upd-check">检查更新</button><button class="btn gray" id="upd-dl" style="display:none">下载更新</button><button class="btn red" id="upd-apply" style="display:none">重启并更新</button></span></div></div></div>`;
   const updStatus=G('upd-status'), updDl=G('upd-dl'), updApply=G('upd-apply');
   const updPaint=r=>{ const cur=G('upd-cur'); if(cur) cur.textContent='v'+(r.current||'?'); let m=r.message||''; if(r.phase==='downloading'&&r.size) m=`正在下载… ${((r.progress||0)/1048576).toFixed(1)} / ${(r.size/1048576).toFixed(1)} MB`; if(!m) m='点「检查更新」看看有没有新版'; updStatus.textContent=m; updDl.style.display=r.phase==='available'?'':'none'; updApply.style.display=r.phase==='ready'?'':'none'; const tk=G('upd-token'); if(r.token&&tk&&document.activeElement!==tk) tk.placeholder='令牌已配置（粘贴新令牌可更换，留空保存 = 删除）'; };
   const updRefresh=async()=>{ try{ updPaint(await api('/api/update')) }catch(e){} };
   updRefresh();
   updIv=setInterval(()=>{ if(curPage!=='update'||!document.getElementById('upd-status')){ clearInterval(updIv); updIv=null; return; } updRefresh(); },3000);
   G('upd-token-save').onclick=async()=>{ const v=G('upd-token').value; const r=await api('/api/update/token',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({token:v})}); G('upd-token').value=''; updStatus.textContent=r.error?('保存失败：'+r.error):(r.token?'令牌已保存，可以检查更新了':'令牌已删除'); updRefresh(); };
   G('upd-check').onclick=async()=>{ updStatus.textContent='检查中…'; updPaint(await api('/api/update/check',{method:'POST'})); };
   updDl.onclick=async()=>{ updPaint(await api('/api/update/download',{method:'POST'})); updRefresh(); };
   updApply.onclick=async()=>{ if(!confirm('现在重启并更新到新版本？节点会断开几秒，之后自动重连。')) return; const r=await api('/api/update/apply',{method:'POST'}); if(r.error){ updStatus.textContent='更新失败：'+r.error; return; } document.body.innerHTML='<div style="position:fixed;inset:0;display:flex;align-items:center;justify-content:center;font-size:20px;color:#fff;text-shadow:0 2px 10px #000">正在更新到新版本，桌面马上自动重开…</div>'; };
  };
  const paintPage=pid=>{ ({appearance:paintAppearance,desktop:paintDesktop,notify:paintNotify,network:paintNetwork,browser:paintBrowser,components:paintComponents,isolation:paintIsolation,storage:paintStorage,security:paintSecurity,general:paintGeneral,about:paintAbout,update:paintUpdate})[pid]?.(); };
  G('set-q').oninput=navPaint;
  G('set-mecard').onclick=()=>goto('update');
  window.sysRefresh=()=>{ navPaint(); paintPage(curPage); };
  /* v0.47: Control Center / Notification Center jump into a
     specific settings page through here (the only sanctioned
     cross-scope handle, next to sysRefresh). */
  window.sysGoto=pid=>{ if(PAGES.some(p=>p.id===pid)) goto(pid); };
  window.sysGoto=pid=>{ goto(pid); };
  (async()=>{ try{ const si=await api('/api/system'); const el=G('set-me-ver'); if(el) el.textContent='v'+si.version+' · 点这里看软件更新'; }catch(e){} })();
  if(!SETTINGS){ loadSettings().then(()=>paintPage(curPage)); }
  goto(window.__mjSetPage||'appearance'); window.__mjSetPage=null;
 }
 if(id==='term'){
   const tin=document.getElementById('tin');
   document.getElementById('trun').onclick=()=>{runCmd(tin.value);tin.value=''};
   tin.onkeydown=e=>{if(e.key==='Enter'){runCmd(tin.value);tin.value=''} if(e.key==='ArrowUp'){e.preventDefault();if(hi<history.length-1)tin.value=history[++hi]||''} if(e.key==='ArrowDown'){e.preventDefault();if(hi>0)tin.value=history[--hi]||''}};
   document.querySelectorAll('[data-cmd]').forEach(x=>x.onclick=()=>runCmd(x.dataset.cmd));
   (async()=>{const r=await api('/api/accel');const el=document.getElementById('term-accel');if(el)el.textContent=r.enabled?(r.coreRunning?'节点加速中：curl / git 等命令走节点':'全局加速已开：节点启动后命令走节点'):'全局加速已关：命令直连（在「节点」里打开）'})();
   loadSys();
 }
 if(id==='files'){
   /* v0.50: the Finder face is bound by the v0.50 script block
      (window.__bindFiles); the pre-v0.50 host-directory browser
      that lived here is gone -- its /api/list endpoint stays in
      the daemon untouched, the window simply no longer browses
      host folders (that was never the terminal's model). */
   if(window.__bindFiles) window.__bindFiles();
 }
 if(id==='browser'){
   const openB=async(proxy)=>{
     const s=document.getElementById('bstatus'); s.textContent=proxy?'正在启动节点并打开 Chrome…':'正在打开 Chrome…';
     const r=await api('/api/browser',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({proxy,url:document.getElementById('burl').value})});
     s.textContent=r.error?('失败：'+r.error):(`已打开：${r.browser} · ${r.mode} · 档案在 ${r.profile}`);
   };
   document.getElementById('bproxy').onclick=()=>openB(true);
   document.getElementById('bdirect').onclick=()=>openB(false);
   const renderWa=()=>{
     const l=document.getElementById('walist'); if(!l) return;
     l.innerHTML=WEBAPPS.length?WEBAPPS.map(a=>`<div class="row" style="margin-top:6px">${webIconHTML(a,true)}<b style="color:#fff">${esc(a.name)}</b><span style="word-break:break-all">${esc(a.url)}</span><button class="btn gray" data-waopen="${esc(a.url)}">打开</button><button class="btn gray" data-wadel="${esc(a.url)}">删除</button></div>`).join(''):'还没有钉住的网站，在下面添加第一个。';
     l.querySelectorAll('[data-waopen]').forEach(x=>x.onclick=()=>openWebApp(x.dataset.waopen));
     l.querySelectorAll('[data-wadel]').forEach(x=>x.onclick=async()=>{
       await api('/api/apps/save',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({url:x.dataset.wadel,delete:true})});
       await loadWebApps(); renderWa(); renderDesk();
     });
   };
   renderWa();
   document.getElementById('waadd').onclick=async()=>{
     const s=document.getElementById('wastatus');
     const name=document.getElementById('waname').value, url=document.getElementById('waurl').value;
     const r=await api('/api/apps/save',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({name,url})});
     if(r.error){s.textContent='失败：'+r.error;return}
     s.textContent='已钉到桌面 ✓'; document.getElementById('waname').value=''; document.getElementById('waurl').value='';
     await loadWebApps(); renderWa(); renderDesk();
   };
   document.getElementById('btest').onclick=async()=>{
     const s=document.getElementById('bstatus'); s.textContent='自检中（经节点连接 Google，最多约 15 秒）…';
     const r=await api('/api/proxytest',{method:'POST'});
     s.textContent=(r.ok?'✓ ':'✕ ')+(r.message||'');
   };
   document.getElementById('bsync').onclick=async()=>{
     const s=document.getElementById('bsyncst'); s.textContent='正在打开同步设置…';
     const r=await api('/api/browser',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({proxy:true,url:'chrome://settings/syncSetup'})});
     s.textContent=r.error?('失败：'+r.error):'已在隔离 Chrome 中打开同步设置，在那边点「开启同步」登录谷歌账户';
   };
 }
 if(id==='firefox'){
   let ffTimer=null;
   const paintFF=async()=>{
     const r=await api('/api/firefox');
     /* v0.54: keep the desktop tile's state source in sync, and
        repaint the desktop when the installed bit flips (the
        tile gains its real icon + loses the 未下载 badge). */
     const wasIn=!!(FFSTATE&&FFSTATE.installed); __bind_FFSTATE.set(r);
     if(!!r.installed!==wasIn&&typeof renderDesk==='function') renderDesk();
     const info=document.getElementById('ffinfo'), prog=document.getElementById('ffprog'), dl=document.getElementById('ffdl');
     if(!info) { if(ffTimer) clearInterval(ffTimer); return; }
     let t;
     if(r.installed) t='已安装 Firefox '+(r.version||'')+(r.updateAvailable?(' · 有新版 '+(r.available||'')+' 可更新'):' · 已是最新');
     else t='未安装组件'+(r.available?(' · 可下载版本 '+(r.available||'')+'（约 '+Math.round((r.availableSize||0)/1048576)+' MB）'):'');
     info.textContent=t;
     if(dl) dl.textContent=r.installed?(r.updateAvailable?'更新 Firefox 组件':'重新下载组件'):('下载 Firefox 组件（约 '+Math.round((r.availableSize||0)/1048576)+' MB，走节点下载）');
     if(r.phase==='downloading'){ prog.textContent='下载中… '+((r.progress||0)/1048576).toFixed(1)+' / '+((r.size||0)/1048576).toFixed(1)+' MB'; if(!ffTimer) ffTimer=setInterval(paintFF,800); }
     else if(r.phase==='extracting'){ prog.textContent='正在释放组件…'; if(!ffTimer) ffTimer=setInterval(paintFF,800); }
     else { prog.textContent=r.message||''; if(ffTimer){ clearInterval(ffTimer); ffTimer=null; } }
   };
   paintFF();
   document.getElementById('ffdl').onclick=async()=>{ await api('/api/firefox/download',{method:'POST'}); paintFF(); };
   let ffWant=null; // last open request, reused by 重启并切换
   const openFF=async(proxy,appmode)=>{
     const s=document.getElementById('ffstatus'); s.textContent='正在打开 Firefox…';
     ffWant={proxy,appmode,url:document.getElementById('ffurl').value};
     const r=await api('/api/firefox/open',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(ffWant)});
     const cf=document.getElementById('ffconflict');
     if(r.conflict){
       document.getElementById('ffconflict-t').textContent='Firefox 正以'+(r.runningMode||'未知')+'模式运行，本次想开的是'+(appmode?'应用化':'普通')+'模式。模式在进程启动时定死，不重启切换不了。';
       const fb0=document.getElementById('ffforce'); if(fb0) fb0.style.display='none';
       cf.style.display='block'; s.textContent='模式冲突，等你决定';
       return;
     }
     cf.style.display='none';
     s.textContent=r.error?('失败：'+r.error):('已打开 · '+(r.mode||''));
   };
   const rb=document.getElementById('ffrestart');
   const fb=document.getElementById('ffforce');
   /* test2 item 1: the restart first tries the gentle path
      (close EVERY window of the component's processes, not
      just claimed ones). If the daemon answers stillRunning,
      the explicit 强制结束并切换 button appears -- the force
      kill (this profile's process tree only) happens ONLY on
      that second, deliberate click. */
   const doRestart=async force=>{
     if(!ffWant) return;
     const s=document.getElementById('ffstatus'); s.textContent=force?'正在强制结束 Firefox 并切换模式…':'正在关闭 Firefox 并切换模式…';
     rb.disabled=true; if(fb) fb.disabled=true;
     const r=await api('/api/firefox/restart',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({...ffWant,force:!!force})});
     rb.disabled=false; if(fb) fb.disabled=false;
     if(r.error){
       s.textContent='失败：'+r.error;
       if(r.stillRunning&&fb) fb.style.display='';
       return;
     }
     if(fb) fb.style.display='none';
     document.getElementById('ffconflict').style.display='none';
     s.textContent='已切换 · '+(r.mode||'');
   };
   if(rb) rb.onclick=()=>doRestart(false);
   if(fb) fb.onclick=()=>doRestart(true);
   document.getElementById('ffopen').onclick=()=>openFF(true,false);
   document.getElementById('ffopenapp').onclick=()=>openFF(true,true);
   document.getElementById('ffopend').onclick=()=>openFF(false,false);
 }
 if(id==='servermon'){
   const G=(x: any): any=>document.getElementById(x);
   const post=(p,body)=>api(p,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});
   const fmtB=n=>{if(n==null||isNaN(n))return '—';const u=['B','KB','MB','GB','TB'];let i=0;n=+n;while(n>=1024&&i<4){n/=1024;i++}return (n>=100||i===0?n.toFixed(0):n.toFixed(1))+' '+u[i]};
   const fmtRate=n=>fmtB(n)+'/s';
   const fmtUp=s=>{if(s==null)return '—';s=+s;const d=Math.floor(s/86400),h=Math.floor(s%86400/3600),m=Math.floor(s%3600/60);return d?d+' 天 '+h+' 小时':(h?h+' 小时 '+m+' 分':m+' 分钟')};
   const pctCls=p=>p>=90?'bad':(p>=75?'warn':'');
   let servers=[], cur='all', builtFor=null, range='live', livePts=[], histPts=[], timer=null, watchTimer=null, ovSig='';
   let cmdHist=[]; try{cmdHist=JSON.parse(localStorage.getItem('mj-sm-cmdhist')||'[]')}catch(e){}
   const byId=id2=>servers.find(x=>x.conn.id===id2);
   const dotOf=s=>!s?'<span class="sdot idle"></span>':(s.ok?'<span class="sdot on"></span>':'<span class="sdot off"></span>');
   function paintSide(){
     const anyOn=servers.some(x=>x.snap&&x.snap.ok);
     G('sm-list').innerHTML=`<div class="sm-it ${cur==='all'?'on':''}" data-id="all"><span class="sdot ${anyOn?'on':'idle'}"></span><span class="nm">全部服务器<span class="sub">${servers.length} 台</span></span></div>`+servers.map(sv=>`<div class="sm-it ${cur===sv.conn.id?'on':''}" data-id="${sv.conn.id}">${dotOf(sv.snap)}<span class="nm">${esc(sv.conn.name||sv.conn.host)}<span class="sub">${esc(sv.conn.user)}@${esc(sv.conn.host)}:${sv.conn.port||22}</span></span></div>`).join('');
     G('sm-list').querySelectorAll('.sm-it').forEach(el=>el.onclick=()=>{ selectServer(el.dataset.id); });
   }
   function selectServer(id2){
     if(cur!=='all'&&cur!==id2) post('/api/servermon/watch',{id:cur,on:false});
     cur=id2; builtFor=null; livePts=[]; ovSig='';
     if(cur!=='all'){ post('/api/servermon/watch',{id:cur,on:true}); }
     paintSide(); paintMain();
   }
   function barHTML(p){return `<div class="sm-bar"><i class="${pctCls(p)}" style="width:${Math.min(100,p||0)}%"></i></div>`}
   function overviewHTML(){
     if(!servers.length) return `<h3>服务器监控</h3><p class="mut" style="margin-top:8px">还没有服务器连接。先去 <b style="color:#fff">SSH</b> 应用里新建一个连接（推荐用密钥登录，监控就能自动采样），这里会自动出现。</p><div class="row" style="margin-top:12px"><button class="btn" onclick="openApp('ssh')">打开 SSH 应用</button></div>`;
     return `<div class="sm-sec" style="margin-top:0">全部服务器<span class="mut" style="font-weight:400;font-size:12px">${servers.filter(x=>x.snap&&x.snap.ok).length} / ${servers.length} 在线</span></div><div class="sm-ov">`+servers.map(sv=>{const s=sv.snap,u=sv.usage||{};return `<div class="sm-ovc" data-id="${sv.conn.id}"><div style="display:flex;align-items:center;gap:7px;font-weight:600;font-size:13.5px">${dotOf(s)}<span style="white-space:nowrap;overflow:hidden;text-overflow:ellipsis">${esc(sv.conn.name||sv.conn.host)}</span></div><div class="mut" style="font-size:11.5px;margin-top:2px">${esc(sv.conn.user)}@${esc(sv.conn.host)}${sv.engine?' · 后台采样中':''}${s&&s.agent?' · 服务器采集':''}</div>${s&&s.ok?`<div style="display:grid;grid-template-columns:1fr 1fr;gap:4px 12px;margin-top:10px;font-size:12.5px;font-variant-numeric:tabular-nums"><span>CPU <b>${s.cpuPct.toFixed(0)}%</b></span><span>内存 <b>${s.memPct.toFixed(0)}%</b></span><span>↓ <b>${fmtRate(s.rxRate)}</b></span><span>↑ <b>${fmtRate(s.txRate)}</b></span><span>本月 <b>${fmtB((u.monthRx||0)+(u.monthTx||0))}</b></span><span>运行 <b>${fmtUp(s.uptimeSec)}</b></span></div>`:`<p class="mut" style="margin-top:10px;font-size:12.5px">${s?esc(s.err||'离线'):'等待采样…点开查看'}</p>`}</div>`}).join('')+`</div><p class="mut" style="margin-top:12px;font-size:12px">点一台服务器看详情：历史曲线、流量统计、服务状态、命令行都在详情里。</p>`;
   }
   function detailHTML(sv){
     const c=sv.conn;
     return `<div style="display:flex;align-items:center;gap:8px;flex-wrap:wrap"><button class="btn gray" id="sm-back">‹ 全部</button><h3 style="margin:0">${esc(c.name||c.host)}</h3><span id="sm-status"></span><span class="mut" style="font-size:12.5px">${esc(c.user)}@${esc(c.host)}:${c.port||22}${c.proxy?' · 走节点':''}</span></div>
     <div class="row" style="margin-top:10px"><input class="txt" id="sm-cred" type="password" placeholder="密码 / 私钥口令（手动采样和执行命令用，不保存）" style="max-width:300px" autocomplete="off"><button class="btn gray" id="sm-sample">手动采样一次</button><button class="btn gray" id="sm-openssh">在 SSH 中打开</button><span class="tag" id="sm-msg"></span></div>
     <div class="sm-tiles" style="margin-top:12px">
       <div class="sm-tile"><div class="k">CPU</div><div class="v" id="sm-t-cpu">—</div><div class="s" id="sm-t-load">负载 —</div></div>
       <div class="sm-tile"><div class="k">内存</div><div class="v" id="sm-t-mem">—</div><div id="sm-t-memb"></div><div class="s" id="sm-t-mems"></div></div>
       <div class="sm-tile"><div class="k">磁盘 /</div><div class="v" id="sm-t-disk">—</div><div id="sm-t-diskb"></div><div class="s" id="sm-t-disks"></div></div>
       <div class="sm-tile"><div class="k">下载速率</div><div class="v" id="sm-t-rx">—</div><div class="s">入站</div></div>
       <div class="sm-tile"><div class="k">上传速率</div><div class="v" id="sm-t-tx">—</div><div class="s">出站</div></div>
       <div class="sm-tile"><div class="k">运行时间</div><div class="v" id="sm-t-up" style="font-size:16px">—</div><div class="s" id="sm-t-ports"></div></div>
     </div>
     <div class="sm-sec">网络流量 <span class="sm-seg" id="sm-ranges"><button data-r="live" class="on">实时</button><button data-r="1h">1 小时</button><button data-r="24h">24 小时</button><button data-r="7d">7 天</button></span></div>
     <div class="sm-card" style="position:relative"><canvas class="sm-chart" id="sm-chart"></canvas><div class="sm-tip" id="sm-tip"></div><div style="display:flex;gap:16px;margin-top:6px;font-size:12px"><span><span style="color:#30d158">━</span> 下载</span><span><span style="color:#0a84ff">━</span> 上传</span><span class="mut" id="sm-chart-note"></span></div></div>
     <div class="sm-sec">流量统计</div>
     <div class="sm-card" id="sm-usage"></div>
     <div class="sm-sec">服务与进程</div>
     <div class="sm-card" id="sm-svc"></div>
     <div class="sm-sec">命令行</div>
     <div class="sm-card"><div class="row"><button class="btn gray" data-cmd="systemctl restart sing-box">重启 sing-box</button><button class="btn gray" data-cmd="journalctl -u sing-box -n 50 --no-pager">sing-box 日志</button><button class="btn gray" data-cmd="df -h">磁盘占用</button><button class="btn gray" data-cmd="free -m">内存</button><button class="btn gray" data-cmd="uptime">运行时间</button></div><div class="row" style="margin-top:8px"><input class="txt" id="sm-cmd" placeholder="输入命令回车执行（一次性命令；交互程序请去 SSH 终端）" autocomplete="off"><button class="btn" id="sm-run">执行</button></div><div class="sm-cmdout" id="sm-out" style="margin-top:8px;display:none"></div></div>
     <div class="sm-sec">服务器采集（电脑关机也记录）</div>
     <div class="sm-card" id="sm-agent"></div>
     <div class="sm-sec">监控设置</div>
     <div class="sm-card">
       <div class="sm-row"><span>后台采样（面板关着也每分钟采）</span><label class="sm-sw"><input type="checkbox" id="sm-c-bg"><i></i></label></div>
       <div class="sm-row"><span>月流量配额（GB，0 = 不设）</span><input class="sm-num" id="sm-c-quota" type="number" min="0" step="1"></div>
       <div class="sm-row"><span>磁盘告警（用到百分之多少，0 = 关）</span><input class="sm-num" id="sm-c-disk" type="number" min="0" max="99" step="1"></div>
       <div class="sm-row"><span>流量告警（用到配额百分之多少，0 = 关）</span><input class="sm-num" id="sm-c-traf" type="number" min="0" max="99" step="1"></div>
       <div class="sm-row"><span>服务停止时告警（进控制台）</span><label class="sm-sw"><input type="checkbox" id="sm-c-svc"><i></i></label></div>
       <div class="row" style="margin-top:10px"><button class="btn" id="sm-c-save">保存设置</button><span class="tag" id="sm-c-msg"></span></div>
       <p class="mut" style="font-size:12px;margin-top:8px">流量统计是终端机按采样累计的，和服务器商账单会有小出入；历史曲线保留 30 天。装了服务器采集后，关机时段的数据会从服务器自动补回来。</p>
     </div>`;
   }
   let agMsg='';
   async function agentAct(action){
     agMsg='处理中…'; paintAgent(byId(cur));
     const r=await post('/api/servermon/agent',{id:cur,action:action,password:G('sm-cred').value,passphrase:G('sm-cred').value});
     if(r.error){ agMsg=r.error; }
     else if(action==='install'){ const ml={'systemd':'systemd 定时（开机自启已确认）','systemd+cron':'systemd + crontab 双保险（开机自启）','cron':'crontab 定时（开机自启）','systemd-nolinger':'systemd 定时（linger 未开，重启后需登录才恢复）'}; agMsg='已安装（'+(ml[r.method]||'已写入')+'）：服务器开始每分钟记录'; }
     else if(action==='sync'){ agMsg='已同步：补入历史 '+(r.points||0)+' 点，补记流量 ↓ '+fmtB(r.addIn||0)+' · ↑ '+fmtB(r.addOut||0); if(range!=='live') loadHist(); }
     else if(action==='uninstall'){ agMsg='已卸载，服务器上的采集脚本和记录已删除'; }
     refresh();
   }
   function paintAgent(sv){
     const el=G('sm-agent'); if(!el||!sv) return;
     const s=sv.snap||{};
     if(!s.agent){
       el.innerHTML=`<p style="font-size:12.5px;line-height:1.65">把一个小采集脚本装到服务器上：每分钟把 CPU、内存、流量记一条到服务器本地（<code>~/.mjmon</code>），<b>采集本身不联网、不耗服务器流量</b>。这样电脑关机、退出终端机的时段也不会断档，下次打开自动把缺失的历史和流量补回来。安装方式：写入脚本并设定时任务（优先 systemd 用户定时器，没有就用 crontab），不需要 root。</p><div class="row" style="margin-top:8px"><button class="btn" id="sm-ag-install">安装服务器采集</button><span class="tag" id="sm-ag-msg">${esc(agMsg)}</span></div>`;
       const b=G('sm-ag-install'); if(b) b.onclick=()=>agentAct('install');
       return;
     }
     const lastTxt=s.agentLast?new Date(s.agentLast*1000).toLocaleString():'暂无';
     const syncTxt=s.agentSync?new Date(s.agentSync*1000).toLocaleString():'还没同步过';
     el.innerHTML=`<div class="sm-row"><span>状态</span><span><span class="sdot on" style="display:inline-block;vertical-align:1px;margin-right:7px"></span>已安装 · 服务器最近样本 ${lastTxt}</span></div><div class="sm-row"><span>上次同步</span><span>${syncTxt}（面板开着时每 5 分钟自动同步）</span></div><div class="row" style="margin-top:8px"><button class="btn gray" id="sm-ag-sync">立即同步</button><button class="btn gray" id="sm-ag-reinstall">重新安装/加固自启</button><button class="btn gray" id="sm-ag-uninstall">卸载</button><span class="tag" id="sm-ag-msg">${esc(agMsg)}</span></div>`;
     const sb=G('sm-ag-sync'); if(sb) sb.onclick=()=>agentAct('sync');
     const ub=G('sm-ag-uninstall'); if(ub) ub.onclick=()=>agentAct('uninstall');
     const rb=G('sm-ag-reinstall'); if(rb) rb.onclick=()=>agentAct('install');
   }
   function paintMain(){
     const M=G('sm-main'); if(!M) return;
     if(cur==='all'){
       const sig=servers.map(x=>x.conn.id+':'+(x.snap?x.snap.ok+Math.round(x.snap.cpuPct)+Math.round((x.usage||{}).monthRx/1e6):'n')).join('|');
       if(sig===ovSig&&builtFor==='all') return;
       ovSig=sig; builtFor='all';
       M.innerHTML=overviewHTML(); M.querySelectorAll('.sm-ovc').forEach(el=>el.onclick=()=>selectServer(el.dataset.id)); return;
     }
     const sv=byId(cur); if(!sv){ cur='all'; paintMain(); return; }
     if(builtFor!==cur){ M.innerHTML=detailHTML(sv); builtFor=cur; bindDetail(sv); }
     updateDetail(sv);
   }
   function bindDetail(sv){
     G('sm-back').onclick=()=>selectServer('all');
     G('sm-openssh').onclick=()=>openApp('ssh');
     G('sm-sample').onclick=async()=>{ G('sm-msg').textContent='采样中…'; const r=await post('/api/servermon/sample',{id:cur,password:G('sm-cred').value,passphrase:G('sm-cred').value}); G('sm-msg').textContent=r.ok?'已更新':(r.error||'采样失败'); refresh(); };
     G('sm-ranges').querySelectorAll('button').forEach(b=>b.onclick=()=>{ range=b.dataset.r; G('sm-ranges').querySelectorAll('button').forEach(x=>x.classList.toggle('on',x===b)); if(range==='live'){drawChart(livePts);} else loadHist(); });
     const runCmd=async(cmd)=>{ const out=G('sm-out'); if(!out) return; out.style.display='block'; out.textContent='执行中…'; const r=await post('/api/servermon/exec',{id:cur,cmd:cmd,password:G('sm-cred').value,passphrase:G('sm-cred').value}); out.textContent=r.error?('错误：'+r.error):((r.output||'（无输出）')); if(cmd&&cmdHist[0]!==cmd){cmdHist.unshift(cmd); cmdHist=cmdHist.slice(0,30); try{localStorage.setItem('mj-sm-cmdhist',JSON.stringify(cmdHist))}catch(e){}} };
     G('sm-run').onclick=()=>{ const v=G('sm-cmd').value.trim(); if(v) runCmd(v); };
     G('sm-cmd').addEventListener('keydown',e=>{ if(e.key==='Enter'){ const v=e.target.value.trim(); if(v) runCmd(v); } if(e.key==='ArrowUp'){ e.preventDefault(); if(cmdHist[0]) e.target.value=cmdHist[0]; } });
     G('sm-main').querySelectorAll('[data-cmd]').forEach(b=>b.onclick=()=>runCmd(b.dataset.cmd));
     const cfg=sv.cfg||{};
     G('sm-c-bg').checked=!!cfg.bg; G('sm-c-quota').value=cfg.quotaGB||0; G('sm-c-disk').value=(cfg.alertDisk==null?90:cfg.alertDisk); G('sm-c-traf').value=(cfg.alertTraffic==null?90:cfg.alertTraffic); G('sm-c-svc').checked=cfg.alertSvc!==false;
     G('sm-c-save').onclick=async()=>{ const nc={bg:G('sm-c-bg').checked,quotaGB:+G('sm-c-quota').value||0,alertDisk:+G('sm-c-disk').value||0,alertTraffic:+G('sm-c-traf').value||0,alertSvc:G('sm-c-svc').checked}; const r=await post('/api/servermon/config',{id:cur,cfg:nc}); G('sm-c-msg').textContent=r.ok?'已保存':(r.error||'保存失败'); if(r.ok){ sv.cfg=nc; } };
     drawChart(livePts);
   }
   function updateDetail(sv){
     const s=sv.snap, u=sv.usage||{}, cfg=sv.cfg||{};
     const st=G('sm-status'); if(!st) return;
     st.innerHTML=s?(s.ok?'<span class="tag" style="color:#30d158">● 在线</span>':'<span class="tag" style="color:#ff453a">● 离线</span>'):'<span class="tag">未采样</span>';
     paintAgent(sv);
     if(!s){ G('sm-msg').textContent='等待采样…（密钥连接会自动开始）'; return; }
     if(!s.ok){ G('sm-msg').textContent=s.err||'采样失败'; return; }
     G('sm-msg').textContent=s.liveClient?'（借用 SSH 终端的连接采样）':'';
     G('sm-t-cpu').textContent=s.cpuPct.toFixed(0)+'%';
     G('sm-t-load').textContent='负载 '+(s.load1||0).toFixed(2);
     G('sm-t-mem').textContent=s.memPct.toFixed(0)+'%';
     G('sm-t-memb').innerHTML=barHTML(s.memPct);
     G('sm-t-mems').textContent=fmtB(s.memUsed)+' / '+fmtB(s.memTotal);
     G('sm-t-disk').textContent=s.diskPct.toFixed(0)+'%';
     G('sm-t-diskb').innerHTML=barHTML(s.diskPct);
     G('sm-t-disks').textContent=fmtB(s.diskUsed)+' / '+fmtB(s.diskTotal);
     G('sm-t-rx').textContent=fmtRate(s.rxRate);
     G('sm-t-tx').textContent=fmtRate(s.txRate);
     G('sm-t-up').textContent=fmtUp(s.uptimeSec);
     G('sm-t-ports').textContent=(s.ports||[]).length?('监听端口 '+(s.ports||[]).join('、')):'';
     const monthUsed=(u.monthRx||0)+(u.monthTx||0);
     let quotaHTML='';
     if(cfg.quotaGB>0){ const qpct=Math.min(100, monthUsed/(cfg.quotaGB*1e9)*100); quotaHTML=`<div class="sm-row"><span>本月配额进度</span><span>${fmtB(monthUsed)} / ${cfg.quotaGB} GB（${qpct.toFixed(0)}%）</span></div><div class="sm-bar"><i class="${qpct>=90?'bad':(qpct>=75?'warn':'')}" style="width:${qpct}%"></i></div>`; }
     G('sm-usage').innerHTML=`<div class="sm-row"><span>今日</span><span>↓ ${fmtB(u.dayRx||0)} · ↑ ${fmtB(u.dayTx||0)}</span></div><div class="sm-row"><span>本月</span><span>↓ ${fmtB(u.monthRx||0)} · ↑ ${fmtB(u.monthTx||0)}</span></div>${quotaHTML}<div class="sm-row"><span>累计（终端机记录）</span><span>↓ ${fmtB(u.totalRx||0)} · ↑ ${fmtB(u.totalTx||0)}</span></div>`;
     const svc=(s.services||[]).map(x=>`<div class="sm-row"><span><span class="sdot ${x.on?'on':'off'}" style="display:inline-block;vertical-align:1px;margin-right:7px"></span>${esc(x.name)}</span><span class="mut">${x.on?'运行中':'未运行'}</span></div>`).join('');
     const top=(s.top||[]).map(p=>`<div class="sm-row"><span>${esc(p.name)}</span><span class="mut">CPU ${p.cpu}% · 内存 ${p.mem}%</span></div>`).join('');
     G('sm-svc').innerHTML=(svc||'<p class="mut">无服务数据</p>')+(top?`<div style="margin-top:6px;border-top:1px solid rgba(255,255,255,.06);padding-top:4px"><div class="mut" style="font-size:12px;padding:4px 2px">进程占用 Top（按内存）</div>${top}</div>`:'');
     if(range==='live'){ const last=livePts[livePts.length-1]; if(!last||last.ts!==s.ts){ livePts.push({ts:s.ts,rx:s.rxRate,tx:s.txRate}); if(livePts.length>90) livePts.splice(0,livePts.length-90); } drawChart(livePts); }
   }
   async function loadHist(){ const r=await api('/api/servermon/history?id='+encodeURIComponent(cur)+'&range='+range); histPts=(r.points||[]).map(p=>({ts:p.ts,rx:p.rx,tx:p.tx})); drawChart(histPts); const n=G('sm-chart-note'); if(n) n.textContent=histPts.length?'':'暂无历史（开启后台采样后会持续记录）'; }
   function drawChart(pts){
     const cv=G('sm-chart'); if(!cv) return;
     const dpr=window.devicePixelRatio||1, W=cv.clientWidth, H=cv.clientHeight; if(!W) return;
     if(cv.width!==Math.round(W*dpr)||cv.height!==Math.round(H*dpr)){ cv.width=W*dpr; cv.height=H*dpr; }
     const ctx=cv.getContext('2d'); ctx.setTransform(dpr,0,0,dpr,0,0); ctx.clearRect(0,0,W,H);
     const pad={l:4,r:4,t:10,b:16};
     ctx.strokeStyle='rgba(255,255,255,.08)'; ctx.fillStyle='rgba(255,255,255,.45)'; ctx.font='10px system-ui'; ctx.lineWidth=1;
     for(let i=0;i<3;i++){ const y=pad.t+(H-pad.t-pad.b)*i/2; ctx.beginPath(); ctx.moveTo(pad.l,y); ctx.lineTo(W-pad.r,y); ctx.stroke(); }
     if(!pts||pts.length<2){ ctx.fillText('等待数据…',pad.l+2,H/2); cv.onmousemove=null; return; }
     let max=0; pts.forEach(p=>{ max=Math.max(max,p.rx,p.tx); }); if(max<=0) max=1; max*=1.15;
     const X=i=>pad.l+(W-pad.l-pad.r)*i/(pts.length-1), Y=v=>pad.t+(H-pad.t-pad.b)*(1-v/max);
     const line=(key,color,fill)=>{ ctx.beginPath(); pts.forEach((p,i)=>{ const x=X(i),y=Y(p[key]); i?ctx.lineTo(x,y):ctx.moveTo(x,y); }); ctx.strokeStyle=color; ctx.lineWidth=1.6; ctx.stroke(); if(fill){ ctx.lineTo(X(pts.length-1),H-pad.b); ctx.lineTo(X(0),H-pad.b); ctx.closePath(); const g=ctx.createLinearGradient(0,pad.t,0,H-pad.b); g.addColorStop(0,fill); g.addColorStop(1,'rgba(0,0,0,0)'); ctx.fillStyle=g; ctx.fill(); ctx.lineWidth=1; } };
     line('rx','#30d158','rgba(48,209,88,.20)');
     line('tx','#0a84ff',null);
     ctx.fillStyle='rgba(255,255,255,.45)';
     const tf=d=>(range==='7d')?(d.getMonth()+1)+'/'+d.getDate()+' '+d.toTimeString().slice(0,5):d.toTimeString().slice(0,8);
     const t0=tf(new Date(pts[0].ts*1000)), t1=tf(new Date(pts[pts.length-1].ts*1000));
     ctx.fillText(t0,pad.l,H-4); ctx.fillText(t1,W-pad.r-ctx.measureText(t1).width,H-4);
     const tip=G('sm-tip');
     cv.onmousemove=e=>{ const r=cv.getBoundingClientRect(); const fx=(e.clientX-r.left-pad.l)/(W-pad.l-pad.r); let i=Math.round(fx*(pts.length-1)); i=Math.max(0,Math.min(pts.length-1,i)); const p=pts[i]; drawChart(pts); const c2=cv.getContext('2d'); const x=X(i); c2.strokeStyle='rgba(255,255,255,.35)'; c2.lineWidth=1; c2.beginPath(); c2.moveTo(x,pad.t); c2.lineTo(x,H-pad.b); c2.stroke(); [['rx','#30d158'],['tx','#0a84ff']].forEach(kv=>{ c2.fillStyle=kv[1]; c2.beginPath(); c2.arc(x,Y(p[kv[0]]),3,0,7); c2.fill(); }); if(tip){ tip.style.display='block'; tip.style.left=Math.min(W-120,x+16)+'px'; tip.style.top='6px'; tip.innerHTML=tf(new Date(p.ts*1000))+'<br>↓ '+fmtRate(p.rx)+'<br>↑ '+fmtRate(p.tx); } };
     cv.onmouseleave=()=>{ if(tip) tip.style.display='none'; drawChart(pts); };
   }
   async function refresh(){ try{ const r=await api('/api/servermon'); servers=r.servers||[]; paintSide(); paintMain(); }catch(e){} }
   refresh();
   timer=setInterval(()=>{ if(!document.hidden) refresh(); },3000);
   watchTimer=setInterval(()=>{ if(cur!=='all') post('/api/servermon/watch',{id:cur,on:true}); },30000);
   const winEl=document.getElementById('body-servermon')?.closest('.win');
   if(winEl){ const mo=new MutationObserver(()=>{ if(!document.body.contains(winEl)){ clearInterval(timer); clearInterval(watchTimer); if(cur!=='all') post('/api/servermon/watch',{id:cur,on:false}); mo.disconnect(); } }); mo.observe(document.body,{childList:true,subtree:true}); }
 }
 if(id==='ssh'){
   let conns=[], keys=[], cur=null, ws=null, term=null, fit=null, curPath='';
   const G=(x: any): any=>document.getElementById(x);
   const loadAssets=()=>new Promise<void>(res=>{
     if(window.Terminal) return res();
     const css=document.createElement('link'); css.rel='stylesheet'; css.href='/assets/xterm.css'; document.head.appendChild(css);
     const s1=document.createElement('script'); s1.src='/assets/xterm.js';
     s1.onload=()=>{ const s2=document.createElement('script'); s2.src='/assets/xterm-fit.js'; s2.onload=()=>res(); document.head.appendChild(s2); };
     document.head.appendChild(s1);
   });
   const paintConns=()=>{
     G('sshconn').innerHTML=conns.map(c=>`<option value="${c.id}">${esc(c.name||c.host)}（${esc(c.user)}@${esc(c.host)}:${c.port||22}${c.proxy?' · 走节点':''}）</option>`).join('')||'<option value="">（暂无连接，先点新建）</option>';
     G('sf_key').innerHTML=keys.map(k=>`<option value="${esc(k)}">${esc(k)}</option>`).join('')||'<option value="">（暂无密钥，可在下面粘贴导入）</option>';
   };
   const loadSSH=async()=>{ const r=await api('/api/ssh'); conns=r.connections||[]; keys=r.keys||[]; paintConns(); };
   loadSSH();
   G('sshnew').onclick=()=>{ G('sshform').style.display='block'; ['sf_name','sf_host','sf_user','sf_keydata','sf_keyname'].forEach(x=>G(x).value=''); G('sf_port').value='22'; G('sshconn').dataset.edit=''; G('sfst').textContent='填好点保存'; };
   G('sshedit').onclick=()=>{ const c=conns.find(x=>x.id===G('sshconn').value); if(!c) return; G('sshform').style.display='block'; G('sf_name').value=c.name||''; G('sf_host').value=c.host||''; G('sf_port').value=c.port||22; G('sf_user').value=c.user||''; G('sf_auth').value=c.auth||'password'; G('sf_key').value=c.keyfile||''; G('sf_proxy').checked=!!c.proxy; G('sshconn').dataset.edit=c.id; G('sfst').textContent='改完点保存'; };
   G('sshdel').onclick=async()=>{ const cid=G('sshconn').value; if(!cid) return; if(!confirm('删掉这个连接？')) return; await api('/api/ssh/save',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({conn:{id:cid},delete:true})}); loadSSH(); };
   G('sfsave').onclick=async()=>{ const c={id:G('sshconn').dataset.edit||'',name:G('sf_name').value.trim(),host:G('sf_host').value.trim(),port:parseInt(G('sf_port').value||'22',10),user:G('sf_user').value.trim(),auth:G('sf_auth').value,keyfile:G('sf_key').value,proxy:G('sf_proxy').checked}; if(!c.host||!c.user){G('sfst').textContent='主机和用户名要填';return} const r=await api('/api/ssh/save',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({conn:c,keyname:G('sf_keyname').value.trim(),keydata:G('sf_keydata').value})}); if(r.error){G('sfst').textContent='失败：'+r.error;return} G('sfst').textContent='已保存 ✓'; G('sf_keydata').value=''; conns=r.connections||conns; keys=r.keys||keys; paintConns(); };
   const ensureTerm=async()=>{ await loadAssets(); if(term) return; term=new Terminal({fontSize:13,cursorBlink:true,theme:{background:'#0b0e14',foreground:'#d6e2f0'}}); fit=new FitAddon.FitAddon(); term.loadAddon(fit); term.open(G('sshterm')); try{fit.fit()}catch(e){} term.onData(d=>{ if(ws&&ws.readyState===1) ws.send(new TextEncoder().encode(d)); }); term.onResize(({cols,rows})=>{ if(ws&&ws.readyState===1) ws.send(JSON.stringify({t:'resize',cols,rows})); }); };
   G('sshgo').onclick=async()=>{
     const cid=G('sshconn').value; if(!cid){G('sshst').textContent='先选一个连接（没有就点新建）';return}
     await ensureTerm(); if(ws){ws.close();ws=null} term.clear();
     const c=conns.find(x=>x.id===cid); const pw=G('sshpass').value;
     const url=`ws://${location.host}/ws/ssh?conn=${encodeURIComponent(cid)}&password=${encodeURIComponent(c&&c.auth==='password'?pw:'')}&passphrase=${encodeURIComponent(c&&c.auth==='key'?pw:'')}&cols=${term.cols}&rows=${term.rows}`;
     G('sshst').textContent='连接中…';
     ws=new WebSocket(url); ws.binaryType='arraybuffer';
     ws.onopen=()=>{ G('sshst').textContent='已连接 · '+(c?(c.host+':'+(c.port||22)):''); cur=cid; curPath=''; sfList(''); };
     ws.onmessage=e=>{ if(!term) return; if(typeof e.data==='string') term.write(e.data); else term.write(new Uint8Array(e.data)); };
     ws.onclose=()=>{ G('sshst').textContent='已断开'; if(cur===cid) cur=null; };
     ws.onerror=()=>{ G('sshst').textContent='连接出错（看终端里的提示）'; };
   };
   G('sshstop').onclick=()=>{ if(ws) ws.close(); };
   const winEl=G('body-ssh')?G('body-ssh').closest('.win'):null;
   if(winEl){ const mo=new MutationObserver(()=>{ if(!document.body.contains(winEl)){ if(ws) ws.close(); mo.disconnect(); } }); mo.observe(document.body,{childList:true,subtree:true}); }
   const fmtSize=n=>n>1048576?(n/1048576).toFixed(1)+' MB':n>1024?(n/1024).toFixed(1)+' KB':n+' B';
   const sfList=async p=>{
     if(!cur){ G('sflist').textContent='先建立终端连接'; return; }
     const r=await api('/api/ssh/sftp/list',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({conn:cur,path:p||curPath})});
     if(r.error){ G('sflist').textContent='失败：'+r.error; return; }
     curPath=r.path; G('sfpath').textContent=r.path;
     G('sflist').innerHTML=r.entries.map(e=>{
       const full=(r.path.endsWith('/')?r.path:r.path+'/')+e.name;
       return `<div class="row" style="justify-content:space-between;padding:3px 0"><span>${glyph(e.dir?'folder':'file',14)} ${e.dir?`<a href="#" data-sfgo="${esc(full)}" style="color:#7cc7ff">${esc(e.name)}</a>`:esc(e.name)}${e.dir?'':` <span class="mut">${fmtSize(e.size)}</span>`}</span><span>${e.dir?'':`<a href="/api/ssh/sftp/download?conn=${encodeURIComponent(cur)}&path=${encodeURIComponent(full)}" style="color:#7cc7ff">下载</a> `}<a href="#" data-sfdel="${esc(full)}" data-dir="${e.dir?1:0}" style="color:#ff8a80">删除</a></span></div>`;
     }).join('')||'（空目录）';
     G('sflist').querySelectorAll('[data-sfgo]').forEach(a=>a.onclick=ev=>{ ev.preventDefault(); sfList(a.dataset.sfgo); });
     G('sflist').querySelectorAll('[data-sfdel]').forEach(a=>a.onclick=async ev=>{ ev.preventDefault(); if(!confirm('删除 '+a.dataset.sfdel+' ？')) return; const rr=await api('/api/ssh/sftp/delete',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({conn:cur,path:a.dataset.sfdel,dir:a.dataset.dir==='1'})}); if(rr.error) alert(rr.error); else sfList(curPath); });
   };
   G('sfup').onclick=()=>{ if(!curPath) return; sfList(curPath.replace(/\/[^/]+\/?$/,'')||'/'); };
   G('sfrefresh').onclick=()=>sfList(curPath);
   G('sfmkdir').onclick=async()=>{ const n=prompt('新文件夹名'); if(!n) return; const r=await api('/api/ssh/sftp/mkdir',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({conn:cur,path:(curPath.endsWith('/')?curPath:curPath+'/')+n})}); if(r.error) alert(r.error); else sfList(curPath); };
   G('sfupload').onclick=()=>G('sffile').click();
   G('sffile').onchange=async()=>{ const f=G('sffile').files[0]; if(!f||!cur) return; const fd=new FormData(); fd.append('conn',cur); fd.append('path',curPath); fd.append('file',f); try{ const r=await (await fetch('/api/ssh/sftp/upload',{method:'POST',body:fd})).json(); if(r.error) alert('上传失败：'+r.error); else sfList(curPath); }catch(e){ alert('上传失败'); } G('sffile').value=''; };
 }
 if(id==='console'){
   const G=(x: any): any=>document.getElementById(x);
   let lastSeq=0, entries=[], stat=null;
   const rank={info:1,warn:2,error:3};
   const vis=()=>{const w=G('win-console');return !!w&&!w.classList.contains('hidden')};
   const filtered=()=>{const lv=G('cs-level').value, md=G('cs-module').value, q=G('cs-q').value.trim().toLowerCase();
     return entries.filter(e=>(!lv||rank[e.level]>=rank[lv])&&(!md||e.module===md)&&(!q||(e.msg+' '+e.module+' '+e.level).toLowerCase().includes(q)))};
   const paint=()=>{const list=filtered();
     G('cs-log').innerHTML=list.length?list.map(e=>`<div class="cl ${e.level}"><span class="ct">${esc((e.time||'').slice(11))}</span><span class="cm">[${esc(e.module)}]</span><span class="cmsg">${esc(e.msg)}</span></div>`).join(''):'<div style="opacity:.55">（暂无符合条件的日志）</div>';
     if(G('cs-auto').checked) G('cs-log').scrollTop=G('cs-log').scrollHeight;};
   const paintStat=s=>{ if(!s||s.error) return; stat=s;
     G('cs-ver').textContent='v'+s.version;
     G('cs-core').textContent=s.coreRunning?'运行中':'已停止'; G('cs-core-dot').className='csdot '+(s.coreRunning?'on':'off');
     G('cs-node').textContent=s.nodeConnected?('已连（'+s.nodeCount+' 个）'):'未连'; G('cs-node-dot').className='csdot '+(s.nodeConnected?'on':'off');
     G('cs-up').textContent=s.uptimeText||''; G('cs-cnt').textContent=(s.logCount??0)+' 条'; };
   const fetchStat=async()=>{ try{ paintStat(await api('/api/diag/status')); }catch(e){} };
   const tick=async()=>{ if(!vis()) return;
     try{ const r=await api('/api/diag/logs?after='+lastSeq); const es=r.entries||[];
       if(es.length){ es.forEach(e=>{ entries.push(e); if(e.seq>lastSeq) lastSeq=e.seq; }); if(entries.length>2000) entries=entries.slice(-2000); paint(); }
       else if(!entries.length) paint();
     }catch(e){} };
   G('cs-level').onchange=paint; G('cs-module').onchange=paint; G('cs-q').oninput=paint;
   G('cs-export').onclick=()=>{ const a=document.createElement('a'); a.href='/api/diag/export'; document.body.appendChild(a); a.click(); a.remove(); };
   G('cs-copy').onclick=async()=>{
     const btn=G('cs-copy');
     await fetchStat();
     const s=stat||{};
     const errs=entries.filter(e=>e.level==='error').slice(-10);
     const txt=['MJ Terminal 诊断摘要','时间：'+new Date().toLocaleString('zh-CN'),
       '版本：v'+(s.version||'?')+' · '+(s.os||'')+'/'+(s.arch||'')+' · 守护进程端口 '+(s.port||'?')+' · 已运行 '+(s.uptimeText||'?'),
       '核心：'+(s.coreRunning?'运行中':'已停止')+' · 节点：'+(s.nodeConnected?'已连':'未连')+'（共 '+(s.nodeCount??0)+' 个） · 外部窗口 '+(s.extWindows??0)+' 个 · SSH 连接 '+(s.sshConnections??0)+' 条（在线 '+(s.sshLive??0)+'）',
       '数据目录：'+(s.dataDir||''),'日志共 '+(s.logCount??entries.length)+' 条',
       '--- 最近报错 ---',
       ...(errs.length?errs.map(e=>'['+e.time+'] ['+e.module+'] '+e.msg):['（没有错误记录）'])].join('\n');
     let ok=false;
     try{ await navigator.clipboard.writeText(txt); ok=true; }catch(e){
       const ta=document.createElement('textarea'); ta.value=txt; ta.style.position='fixed'; ta.style.opacity='0'; document.body.appendChild(ta); ta.select(); try{ ok=document.execCommand('copy'); }catch(e2){} ta.remove(); }
     btn.textContent=ok?'已复制 ✓':'复制失败'; setTimeout(()=>{btn.textContent='复制诊断摘要'},1600);
   };
   fetchStat(); tick();
   setInterval(tick,1000);
   setInterval(()=>{ if(vis()) fetchStat(); },5000);
 }
 if(id==='appstore'){
   const G=(x: any): any=>document.getElementById(x);
   const CATS=['全部','网络通讯','办公文档','开发运维','影音图像','系统工具'];
   let cat='全部', apps=[], wasInstalled={}, boxUi='';
   const vis=()=>{const w=G('win-appstore');return !!w&&!w.classList.contains('hidden')};
   const fmtMB=n=>n>1048576?(n/1048576).toFixed(1)+' MB':Math.max(1,Math.round(n/1024))+' KB';
   const paintCats=()=>{ G('st-cats').innerHTML=CATS.map(c=>`<button class="stcat${c===cat?' on':''}" data-cat="${c}">${c}</button>`).join('');
     G('st-cats').querySelectorAll('[data-cat]').forEach(b=>b.onclick=()=>{cat=b.dataset.cat;paintCats();paintGrid();}); };
   const paintGrid=()=>{
     const q=(G('st-q').value||'').trim().toLowerCase();
     const list=apps.filter(a=>(cat==='全部'||a.category===cat)&&(!q||(a.name+' '+a.desc).toLowerCase().includes(q)));
     G('st-count').textContent=apps.length?('共 '+apps.length+' 个应用 · 装好后图标会出现在桌面上'):'目录加载中…';
     G('st-grid').innerHTML=list.length?list.map(a=>{
       let btn='';
       if(a.installing){ const pc=a.size?Math.min(99,Math.floor((a.progress||0)/a.size*100)):0;
         btn=`<div class="stbar"><i style="width:${a.phase==='extracting'?100:pc}%"></i></div><span class="mut">${a.phase==='extracting'?'正在释放…':('下载中 '+pc+'%')}</span>`;
       } else if(a.installed){ btn=`<div class="row"><button class="btn" data-open="${a.id}">打开</button><button class="btn gray" data-un="${a.id}">卸载</button></div>`;
       } else if(a.phase==='error'){ btn=`<div class="row"><button class="btn" data-dl="${a.id}">重试</button></div><span style="color:#ff9d97;font-size:12px">${esc(a.stateMsg||'安装失败')}</span>`;
       } else { btn=`<button class="btn" data-dl="${a.id}">下载</button>`; }
       return `<div class="stcard"><div class="sthead">${storeIconHTML(a)}<div style="min-width:0"><b style="font-size:14.5px">${esc(a.name)}</b> <span class="mut" style="font-size:12px">v${esc(a.version)}</span><div class="mut" style="font-size:12px">${fmtMB(a.size)} · ${esc(a.category)}</div></div></div><div class="mut" style="font-size:12.5px;line-height:1.55;flex:1">${esc(a.desc)}</div><div><span class="stbadge">${esc(a.source)}</span></div><div>${btn}</div></div>`;
     }).join(''):'<p class="mut">这个分类下还没有应用。</p>';
     G('st-grid').querySelectorAll('[data-dl]').forEach(b=>b.onclick=async()=>{ b.disabled=true; b.textContent='开始…'; const r=await api('/api/apps/install',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({id:b.dataset.dl})}); if(r&&r.error) alert(r.error); tick(true); });
     G('st-grid').querySelectorAll('[data-open]').forEach(b=>b.onclick=()=>launchStoreApp(b.dataset.open));
     G('st-grid').querySelectorAll('[data-un]').forEach(b=>b.onclick=async()=>{ const a=apps.find(x=>x.id===b.dataset.un); if(!a) return;
       if(!confirm('卸载 '+a.name+'？\n便携应用的程序和数据都在同一个文件夹里，卸载会把 '+a.name+' 的本地数据（如聊天记录、配置）一起全部删掉，不可恢复。')) return;
       const r=await api('/api/apps/uninstall',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({id:a.id})});
       if(r&&r.error) alert(r.error); await loadStoreApps(); renderDesk(); tick(true); });
   };
   const tick=async(force)=>{
     if(!force&&!vis()) return;
     try{ const r=await api('/api/apps/catalog'); apps=r.apps||[];
       const nowIn={}; apps.forEach(a=>nowIn[a.id]=!!a.installed);
       let changed=false; apps.forEach(a=>{ if(wasInstalled[a.id]===undefined) return; if(wasInstalled[a.id]!==nowIn[a.id]) changed=true; });
       wasInstalled=nowIn;
       if(changed){ await loadStoreApps(); renderDesk(); }
       G('st-foot').textContent='目录来源：'+(r.source==='remote'?'在线目录（仓库实时更新，加应用不用升级）':'内置目录（在线目录暂时拉不到时的兜底）')+' · 安装位置：Data/apps · 下载走节点加速（全局加速开着时）';
       paintGrid(); paintBox();
     }catch(e){}
   };
   /* ----- 隔离安装 (v0.39): drive the Sandboxie box-install state
      machine. The section re-renders only when the UI STATE changes
      (boxUi key), so a half-typed URL is never clobbered by the
      1.2 s tick; progress text updates in place. ----- */
   const boxRender=(key,html)=>{ if(boxUi===key) return false; boxUi=key; const el=G('st-box'); if(el) el.innerHTML=html; return true; };
   const paintBox=async()=>{
     const boxEl=G('st-box'); if(!boxEl) return;
     let st; try{ st=await api('/api/sandbox/status'); }catch(e){ return; }
     const bi=st.boxInstall||{};
     if(!st.installed){
       if(boxRender('guide',`<div class="row"><span class="tag">隔离组件还没装，隔离安装用不了</span><button class="btn gray" id="stbox-go">去装组件</button></div>`)){
         G('stbox-go').onclick=()=>{ window.__mjSetPage='isolation'; openApp('sys'); setTimeout(()=>{ if(window.sysGoto) window.sysGoto('isolation'); },80); };
       }
       return;
     }
     if(bi.phase==='choosing'){
       const cands=bi.candidates||[];
       /* v0.43: the render key carries each candidate's liveness,
          so a file that vanishes while this dialog is open is
          struck through on the next tick instead of being
          registered into a ghost. The top ALIVE candidate is
          preselected as a suggestion only — registration still
          takes the explicit button click below. */
       const ckey='choosing:'+cands.map(c=>(c.exists===false?'0':'1')+(c.existing?'E':'')).join('');
       const firstAlive=cands.findIndex(c=>c.exists!==false);
       const anyForeign=cands.some(c=>c.foreign&&c.exists!==false);
       const anyExisting=cands.some(c=>c.existing&&c.exists!==false);
       if(boxRender(ckey,`<p style="font-size:13.5px">${glyph('checkcircle',14)} ${esc(bi.message||'装完了')}</p><div style="margin-top:6px">${cands.map((c,i)=> c.exists===false
         ? `<label style="display:block;margin-top:5px;font-size:13px;opacity:.55"><input type="radio" disabled> <s>${esc(c.virtual)}</s> <span class="tag">文件已消失，登记不了（多半是别的程序的临时文件）</span></label>`
         : `<label style="display:block;margin-top:5px;font-size:13px"><input type="radio" name="stbox-exe" value="${esc(c.virtual)}" data-sealed="${c.sealed?1:0}" ${i===firstAlive?'checked':''}> ${esc(c.virtual)} <span class="mut">${(c.size/1048576).toFixed(1)} MB</span>${c.existing?' <span class="tag">盒内已有</span>':''}${c.foreign?' <span class="tag">与安装包看起来无关</span>':''}</label>`).join('')}</div>${anyExisting?`<p class="mut" style="margin-top:8px;font-size:12.5px;line-height:1.6">标了「盒内已有」的不是这次新装进来的：本次安装没有新增文件——这个程序应该是已经装在盒子里了（同版本重装时安装器会跳过已存在的文件）。下面是从盒内找到的已有程序，确认是它，登记即可。</p>`:''}${anyForeign?`<p class="mut" style="margin-top:8px;font-size:12.5px;line-height:1.6">注意：标了「与安装包看起来无关」的候选，可能是安装期间盒子里其他程序活动留下的文件，认准你要装的那个再登记。</p>`:''}<p class="mut" id="stbox-sealnote" style="margin-top:8px;font-size:12.5px;line-height:1.6"></p><p class="mut" style="margin-top:4px;font-size:12.5px">这一步已经存盘：关掉页面、重启终端都不会丢，什么时候回来确认都行。</p><div class="row" style="margin-top:10px"><input class="txt" id="stbox-name" placeholder="应用名字（可改）" style="max-width:200px;flex:none"><button class="btn" id="stbox-reg">登记到桌面</button><button class="btn gray" id="stbox-skip">先不登记</button></div>`)){
         const nm=G('stbox-name');
         const topC=firstAlive>=0?cands[firstAlive]:null;
         if(nm&&topC){ const b=(topC.virtual||'').split('\\').pop()||''; nm.value=b.replace(/\.exe$/i,''); }
         const sealNote=()=>{ const sel=boxEl.querySelector('input[name="stbox-exe"]:checked'); const sn=G('stbox-sealnote'); if(sn) sn.textContent=(sel&&sel.dataset.sealed==='1')?'这个主程序在数据封堵名单里（微信家族）：登记后它在盒内使用一份全新数据，和宿主机上的同名软件互不相通；宿主机那份数据不会被读到、也不会被改动。':''; };
         boxEl.querySelectorAll('input[name="stbox-exe"]').forEach(r=>r.onchange=sealNote); sealNote();
         G('stbox-reg').onclick=async()=>{ const sel=boxEl.querySelector('input[name="stbox-exe"]:checked'); if(!sel){ alert('先选一个主程序'); return; } const r=await api('/api/sandbox/box/register',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({name:G('stbox-name').value,exe:sel.value})}); if(r&&r.error){ alert(r.error); return; } boxUi=''; await loadBoxApps(); renderDesk(); paintBox(); };
         G('stbox-skip').onclick=async()=>{ await api('/api/sandbox/box/discard',{method:'POST'}); boxUi=''; paintBox(); };
       }
       return;
     }
     if(bi.busy){
       if(boxRender('busy',`<div class="row"><span class="tag" id="stbox-prog">处理中…</span><button class="btn gray" id="stbox-stop">终止本次安装</button></div><p class="mut" style="margin-top:6px;font-size:12.5px">终止只会结束这次安装的进程，盒子里其他应用不受影响。交互式安装的话，安装窗口已经在盒子里打开了，在那个窗口里点完就行。</p>`)){
         G('stbox-stop').onclick=async()=>{ await api('/api/sandbox/box/stop',{method:'POST'}); boxUi=''; };
       }
       const p=G('stbox-prog');
       if(p){
         const PL={preparing:'准备中',downloading:'下载 / 导入',installing:'盒内安装中',scanning:'扫描登记中'};
         let el=''; if(bi.startedAt){ const s=Math.max(0,Math.floor((Date.now()-bi.startedAt)/1000)); el=' · 已用时 '+Math.floor(s/60)+':'+String(s%60).padStart(2,'0'); }
         p.textContent=(PL[bi.phase]||'处理中')+el+' · '+(bi.message||'处理中…')+(bi.phase==='downloading'&&bi.size?(' · '+Math.min(99,Math.floor((bi.progress||0)/bi.size*100))+'%'):'');
       }
       return;
     }
     const key='form:'+bi.phase+':'+(bi.message||'');
     if(boxRender(key,`${bi.phase==='done'?`<p style="font-size:13px">${glyph('checkcircle',13)} ${esc(bi.message||'')}</p>`:''}${bi.phase==='error'?`<p style="font-size:13px;color:#ff9d97">${glyph('warn',13)} ${esc(bi.message||'没装成')}</p>`:''}${bi.phase==='idle'&&bi.message?`<p style="font-size:13px">${glyph('info',13)} ${esc(bi.message)}</p>`:''}<div class="row"><input class="txt" id="stbox-url" placeholder="安装包下载地址（https://… 以 .exe 结尾）"><input class="txt" id="stbox-sha" placeholder="SHA-256（可选）" style="max-width:200px;flex:none"></div><div class="row" style="margin-top:8px"><button class="btn" id="stbox-dl">下载并隔离安装</button><button class="btn gray" id="stbox-local">导入本地安装包…</button><input type="file" id="stbox-file" accept=".exe,.msi" style="display:none"><span class="tag" id="stbox-fmsg"></span></div><p class="mut" style="margin-top:8px;font-size:12.5px;line-height:1.6">认得出类型的安装器（NSIS / Inno）会自动静默安装；认不出的会在盒子里打开安装界面，你照常点完就行。MSI 安装包的安装服务在盒子外，隔离不住，不接。</p>`)){
       G('stbox-dl').onclick=async()=>{ const u=G('stbox-url').value.trim(); if(!u){ alert('先填安装包地址'); return; } const r=await api('/api/sandbox/box/install',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({url:u,sha256:G('stbox-sha').value.trim()})}); if(r&&r.error){ alert(r.error); return; } boxUi=''; paintBox(); };
       G('stbox-local').onclick=()=>G('stbox-file').click();
       G('stbox-file').onchange=async e=>{ const f=e.target.files[0]; if(!f) return; const msg=G('stbox-fmsg'); if(msg) msg.textContent='导入中…'; const fd=new FormData(); fd.append('file',f); let r=null; try{ r=await (await fetch('/api/sandbox/import',{method:'POST',body:fd})).json(); }catch(err){} if(!r||r.error){ if(msg) msg.textContent='导入失败：'+((r&&r.error)||'网络错误'); return; } const rr=await api('/api/sandbox/box/install',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({file:r.name})}); if(rr&&rr.error){ alert(rr.error); return; } boxUi=''; paintBox(); };
     }
   };
   G('st-q').oninput=paintGrid;
   paintCats(); tick(true);
   setInterval(()=>tick(false),1200);
 }
 if(id==='openclaw'){
   const G=(x: any): any=>document.getElementById(x);
   let ocToken='', ocTick=0, ocProv='openai', ocWizLoaded=false;
   const vis=()=>{const w=G('win-openclaw');return !!w&&!w.classList.contains('hidden')};
   const fmtB=v=>{ v=+v||0; if(v>=1e9) return (v/1e9).toFixed(2)+' GB'; if(v>=1e6) return (v/1e6).toFixed(1)+' MB'; if(v>=1e3) return (v/1e3).toFixed(1)+' KB'; return v+' B'; };
   const maskTok=t=>t?(t.slice(0,4)+'••••••••'+t.slice(-4)):'';
   const OC_PROV={openai:{base:'https://api.openai.com/v1',models:['gpt-5.1','gpt-5.1-mini','gpt-5.6-luna']},anthropic:{base:'https://api.anthropic.com/v1',models:['claude-sonnet-4-5','claude-opus-4-1']},deepseek:{base:'https://api.deepseek.com/v1',models:['deepseek-chat','deepseek-reasoner']},custom:{base:'',models:[]}};
   const wizBody=()=>({provider: ocProv==='custom'?(G('oc-wiz-id').value.trim()||'custom'):ocProv, apiKey:G('oc-wiz-key').value.trim(), model:G('oc-wiz-model').value.trim(), baseUrl:G('oc-wiz-base').value.trim()});
   const paintWiz=async()=>{
     let r; try{ r=await api('/api/openclaw/wizard'); }catch(e){ return; }
     const cur=G('oc-wiz-cur'); if(!cur) return;
     if(r.error){ cur.textContent=r.error; return; }
     ocWizLoaded=true;
     const ps=r.providers||[];
     if(!ps.length){ cur.textContent='还没配置模型，在下面选厂商填 Key'; return; }
     const names=ps.map(p=>p.id+(p.hasKey?('（Key …'+(p.keyTail||'')+'）'):'（没 Key）')).join('、');
     cur.textContent=names+(r.primary?(' · 默认模型 '+r.primary):' · 还没设默认模型');
     if(r.primary&&!G('oc-wiz-model').value){ const i=r.primary.indexOf('/'); if(i>0) G('oc-wiz-model').value=r.primary.slice(i+1); }
   };
   const paintLog=async()=>{
     const el=G('oc-log'); if(!el) return;
     let r; try{ r=await api('/api/openclaw/logtail'); }catch(e){ return; }
     const lines=(r.lines||[]); el.textContent=lines.length?lines.join('\n'):'（暂无日志）';
     el.scrollTop=el.scrollHeight;
   };
   const paintOC=async()=>{
     let r; try{ r=await api('/api/openclaw/status'); }catch(e){ return; }
     if(!G('oc-st-state')) return;
     const dot=c=>'<span class="sdot '+(c?'on':'off')+'" style="display:inline-block;vertical-align:1px;margin-right:6px"></span>';
     let state;
     if(!r.installed) state=dot(false)+'未安装组件';
     else if(r.phase==='downloading'||r.phase==='extracting') state=dot(false)+'正在安装…';
     else if(r.healthy) state=dot(true)+'运行中';
     else if(r.starting) state=dot(false)+'启动中…（网关起来要十几秒）';
     else if(r.running) state=dot(false)+'已启动（健康检查中）';
     else state=dot(false)+(r.runMsg||'已停止');
     if(r.installed&&r.restarts) state+=' · 累计自动重启 '+r.restarts+' 次';
     G('oc-st-state').innerHTML=state;
     G('oc-st-ver').textContent=r.installed?('小龙虾 '+(r.version||'')):(r.available?('可下载 '+(r.available||'')):'—');
     G('oc-st-up').textContent=(r.running&&r.uptimeText)?r.uptimeText:'—';
     G('oc-st-rss').textContent=r.running?(r.rssBytes?fmtB(r.rssBytes):'读取中…'):'—';
     G('oc-st-disk').textContent=r.installed?(r.diskBytes?fmtB(r.diskBytes):'计算中…'):'—';
     G('oc-st-route').textContent=r.running?(r.viaNode?'经本地节点出去':'直连'+(r.viaNodePref?'（开关是走节点，本次没走成）':'')):'—';
     const dl=G('oc-dl'), prog=G('oc-prog');
     if(r.phase==='downloading'){ prog.textContent='下载中… '+fmtB(r.progress||0)+' / '+fmtB(r.size||0); }
     else if(r.phase==='extracting'){ prog.textContent='正在释放组件…'; }
     else if(r.updateAvailable&&r.installed){ prog.textContent='有新版 '+(r.available||'')+'，点「更新组件」下载'; }
     else { prog.textContent=r.stateMsg||''; }
     dl.style.display=(!r.installed||r.updateAvailable)?'':'none';
     dl.textContent=r.installed?('更新组件到 '+(r.available||'')):('下载组件'+(r.availableSize?('（约 '+fmtB(r.availableSize)+'）'):''));
     dl.disabled=(r.phase==='downloading'||r.phase==='extracting');
     G('oc-main').style.display=r.installed?'':'none';
     if(r.installed){
       const setSw=(id,v)=>{ const el=G(id); if(el&&document.activeElement!==el) el.checked=!!v; };
       setSw('oc-p-vianode', r.viaNodePref!==false); setSw('oc-p-autostart', r.autoStart); setSw('oc-p-crash', r.crashRestart!==false);
       G('oc-start').disabled=!!(r.running||r.starting);
       G('oc-stop').disabled=!r.running;
       G('oc-console').disabled=!r.healthy;
       ocToken=r.token||'';
       G('oc-token').textContent=r.hasToken?maskTok(ocToken):'（未设置）';
       G('oc-tokcopy').disabled=!r.hasToken;
       G('oc-dir').textContent='组件目录：'+(r.componentDir||'')+'（数据导出不含日志；卸载会连数据一起删除）';
       if(!ocWizLoaded) paintWiz();
       ocTick++;
       if(ocTick%3===0) paintLog();
     }
   };
   paintOC();
   setInterval(()=>{ if(vis()) paintOC(); },1800);
   G('oc-dl').onclick=async()=>{ const r=await api('/api/openclaw/install',{method:'POST'}); if(r&&r.error) alert(r.error); paintOC(); };
   G('oc-check').onclick=async()=>{ G('oc-prog').textContent='检查中…'; const r=await api('/api/openclaw/checkupdate',{method:'POST'}); if(r&&r.error){ G('oc-prog').textContent=r.error; return; } G('oc-prog').textContent=r.updateAvailable?('有新版 '+(r.available||'')+' 可更新'):('已是最新（'+(r.current||'')+'）'); paintOC(); };
   G('oc-start').onclick=async()=>{ const r=await api('/api/openclaw/start',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({viaNode:G('oc-p-vianode').checked})}); if(r&&r.error) alert(r.error); paintOC(); };
   G('oc-stop').onclick=async()=>{ const r=await api('/api/openclaw/stop',{method:'POST'}); if(r&&r.error) alert(r.error); paintOC(); };
   G('oc-console').onclick=async()=>{ const r=await api('/api/openclaw/console',{method:'POST'}); if(r&&r.error) alert(r.error); };
   const prefSend=()=>{ const body={viaNode:G('oc-p-vianode').checked,autoStart:G('oc-p-autostart').checked,crashRestart:G('oc-p-crash').checked}; api('/api/openclaw/prefs',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)}); };
   G('oc-p-vianode').onchange=prefSend; G('oc-p-autostart').onchange=prefSend; G('oc-p-crash').onchange=prefSend;
   G('oc-wiz-provs').querySelectorAll('button').forEach(b=>b.onclick=()=>{
     ocProv=b.dataset.p;
     G('oc-wiz-provs').querySelectorAll('button').forEach(x=>x.className=(x===b?'btn':'btn gray'));
     G('oc-wiz-idrow').style.display=ocProv==='custom'?'':'none';
     const P=OC_PROV[ocProv]||{base:'',models:[]};
     G('oc-wiz-base').placeholder=ocProv==='custom'?'接口地址，如 https://example.com/v1（必填）':('接口地址（留空用官方 '+P.base+'）');
     G('oc-wiz-models').innerHTML=P.models.map(m=>'<option value="'+m+'">').join('');
     G('oc-wiz-model').placeholder=ocProv==='custom'?'模型名（必填）':('模型名，如 '+P.models[0]);
   });
   G('oc-wiz-save').onclick=async()=>{ const m=G('oc-wiz-msg'); m.textContent='保存中（走小龙虾配置命令，要几秒）…'; const r=await api('/api/openclaw/wizard/apply',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(wizBody())}); if(r&&r.error){ m.textContent=r.error; return; } m.textContent='已保存，立即生效'; G('oc-wiz-key').value=''; ocWizLoaded=false; paintWiz(); };
   G('oc-wiz-test').onclick=async()=>{ const m=G('oc-wiz-msg'); m.textContent='测试中…'; const r=await api('/api/openclaw/wizard/test',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(wizBody())}); m.textContent=r.error?r.error:(r.message||''); };
   G('oc-log-refresh').onclick=paintLog;
   G('oc-export').onclick=()=>{ const a=document.createElement('a'); a.href='/api/openclaw/export'; document.body.appendChild(a); a.click(); a.remove(); };
   G('oc-import').onclick=()=>G('oc-import-file').click();
   G('oc-import-file').onchange=async(e)=>{ const f=e.target.files[0]; if(!f) return; if(!confirm('导入这份小龙虾数据？\\n现在的配置和数据会先自动备份（不会删），然后被导入的数据替换。')){ e.target.value=''; return; } const fd=new FormData(); fd.append('file',f); try{ const resp=await fetch('/api/openclaw/import',{method:'POST',body:fd}); const r=await resp.json(); alert(r.error||r.message||'导入完成'); }catch(err){ alert('导入失败：'+err); } e.target.value=''; ocWizLoaded=false; paintOC(); paintWiz(); };
   G('oc-uninst').onclick=async()=>{ if(!confirm('卸载小龙虾？\\n组件程序、它记住的配置和数据都在 Data/openclaw 里，卸载会一并全部删掉，不可恢复。')) return;
     const r=await api('/api/openclaw/uninstall',{method:'POST'}); if(r&&r.error) alert(r.error); ocWizLoaded=false; paintOC(); };
   G('oc-tokcopy').onclick=async()=>{ if(!ocToken) return; const btn=G('oc-tokcopy'); let ok=false;
     try{ await navigator.clipboard.writeText(ocToken); ok=true; }catch(e){
       const ta=document.createElement('textarea'); ta.value=ocToken; ta.style.position='fixed'; ta.style.opacity='0'; document.body.appendChild(ta); ta.select(); try{ ok=document.execCommand('copy'); }catch(e2){} ta.remove(); }
     btn.textContent=ok?'已复制 ✓':'复制失败'; setTimeout(()=>{btn.textContent='复制令牌'},1600); };
 }
 if(id==='imgview'){
   document.getElementById('iv-prev').onclick=()=>stepImage(-1);
   document.getElementById('iv-next').onclick=()=>stepImage(1);
   document.getElementById('iv-fit').onclick=()=>{ IMGV.fit=!IMGV.fit; paintImageViewer(); };
   document.addEventListener('keydown',ev=>{
     const w=document.getElementById('win-imgview');
     if(!w||w.classList.contains('hidden')) return;
     if(ev.key==='ArrowLeft') stepImage(-1);
     else if(ev.key==='ArrowRight') stepImage(1);
   });
 }
 if(id==='textview'){
   document.getElementById('tv-wrap').onchange=e=>{
     const pre=document.getElementById('tv-pre');
     if(pre) pre.style.whiteSpace=e.target.checked?'pre-wrap':'pre';
   };
 }
 if(id==='core'){
   /* v0.49 节点专业化：数据全部来自 /api/nodepro/status（分组、
      选择、延迟、流量、规则同源）；写操作走 /api/nodepro/*。
      /api/node 只剩旧读口（远程只读档与菜单栏点灯在用）。 */
   const post=(path,body)=>api(path,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body||{})});
   const fmtB=v=>{ v=+v||0; if(v>=1e9) return (v/1e9).toFixed(2)+' GB'; if(v>=1e6) return (v/1e6).toFixed(1)+' MB'; if(v>=1e3) return (v/1e3).toFixed(1)+' KB'; return v+' B'; };
   const fmtRate=v=>fmtB(v)+'/s';
   const fmtT=ts=>ts?new Date(ts*1000).toLocaleString():'从未';
   let NP=null, rulesDraft=null; const trafPts=[];
   const statusEl=()=>document.getElementById('core-status');
   const busy=t=>{ const s=statusEl(); if(s) s.textContent='状态：'+t; };
   const latHTML=n=>{
     if(n.latencyErr) return '<span style="color:#ff453a" title="'+esc(n.latencyErr)+'">测速失败</span>';
     if(!n.latencyAt) return '<span class="mut">未测</span>';
     const ms=n.latencyMs, c=ms<150?'#30d158':(ms<400?'#ffd60a':'#ff9f0a');
     return '<span style="color:'+c+'">'+ms+' ms</span>'+(n.latencyKind==='tcp'?'<span class="mut"> · TCP</span>':'');
   };
   const allRealNodes=()=>{ const out=[]; (NP.groups||[]).forEach(g=>{ if(g.kind!=='virtual') (g.nodes||[]).forEach(n=>{ if(!n.isWarp) out.push(n); }); }); return out; };
   const detourName=d=>{ if(!d) return ''; if(d==='warp') return 'WARP 中继'; const t=allRealNodes().find(n=>n.id===d); return t?t.name:d; };
   const nodeRow=n=>{
     const others=allRealNodes().filter(x=>x.id!==n.id);
     const opts=['<option value="">前置：无</option>']
       .concat(NP.warp&&NP.warp.registered?['<option value="warp"'+(n.detour==='warp'?' selected':'')+'>前置：WARP</option>']:[])
       .concat(others.map(o=>'<option value="'+o.id+'"'+(n.detour===o.id?' selected':'')+'>前置：'+esc(o.name)+'</option>')).join('');
     let h='<div class="sm-row" style="'+(n.selected?'background:rgba(48,209,88,.10);border-radius:8px':'')+'"><span style="min-width:0"><span class="sdot '+(n.selected?'on':'idle')+'" style="display:inline-block;vertical-align:1px;margin-right:7px"></span><b>'+esc(n.name)+'</b> <span class="tag">'+esc(n.type)+'</span>'+(n.unreachable6?' <span class="tag" style="color:#ff9f0a">不可直达 · IPv6</span>':'')+'<div class="mut" style="font-size:12px;margin-top:2px">'+esc(n.server)+':'+n.port+(n.detour?' · 经 '+esc(detourName(n.detour)):'')+'</div></span>'
       +'<span class="row" style="flex:none">'+latHTML(n)
       +'<select class="txt" data-detour="'+n.id+'" style="width:128px">'+opts+'</select>'
       +'<button class="btn gray" data-test="'+n.id+'">测速</button>'
       +(n.selected?'<span class="tag" style="color:#30d158">当前出口</span>':'<button class="btn" data-sel="'+n.id+'">切换</button>')
       +'</span></div>';
     if(n.unreachable6){
       const cands=allRealNodes().filter(x=>x.id!==n.id&&!x.ipv6);
       h+='<div class="sm-row"><span class="mut" style="font-size:12px">本地没有 IPv6 出口，这个节点的服务器是 IPv6 地址，直连到不了。给它套一个前置就能用：</span><span class="row"><select class="txt" id="v6fix-'+n.id+'" style="width:170px">'+(NP.warp&&NP.warp.registered?'<option value="warp">WARP 中继</option>':'')+cands.map(o=>'<option value="'+o.id+'">'+esc(o.name)+'</option>').join('')+'</select><button class="btn gray" data-v6fix="'+n.id+'">套前置</button></span></div>';
     }
     return h;
   };
   const groupHTML=g=>{
     if(g.kind==='virtual'){
       const rows=(g.nodes||[]).map(v=>{
         const cur=v.id==='auto'?(v.currentTag?'':''):(v.currentId?(allRealNodes().find(n=>n.id===v.currentId)||{}).name||'':'');
         const hit=v.id==='auto'?(v.currentTag?('当前命中 '+esc(v.currentTag.replace(/^node-/,'').slice(0,10)+'…')):'核心启动后显示当前命中'):('当前命中 '+(cur||'—'));
         return '<div class="sm-row" style="'+(v.selected?'background:rgba(48,209,88,.10);border-radius:8px':'')+'"><span><b>'+esc(v.name)+'</b> <span class="tag">'+(v.id==='auto'?'urltest':'fallback')+'</span><div class="mut" style="font-size:12px;margin-top:2px">'+v.members+' 个成员 · '+hit+'</div></span><span class="row">'+(v.selected?'<span class="tag" style="color:#30d158">当前出口</span>':'<button class="btn" data-sel="'+v.id+'">选为出口</button>')+'<button class="btn gray" data-test="'+v.id+'">测速</button></span></div>';
       }).join('');
       return '<div class="set-sec" style="margin-top:14px">虚拟组</div><div class="sm-card">'+rows+'<p class="mut" style="margin-top:4px">自动选择：核心按间隔测速、自动用最快的（sing-box urltest）。故障转移：守护进程按成员顺序探活，当前出口挂了就顺次切到第一个活着的（sing-box 没有 fallback 出站类型，由守护进程看门狗实现）。</p></div>';
     }
     let head='';
     if(g.kind==='sub'&&g.sub){
       const s=g.sub, ui=s.userInfo;
       let uiHTML='';
       if(ui&&ui.total>0){ const used=(+ui.upload||0)+(+ui.download||0), pct=Math.min(100,used/ui.total*100);
         uiHTML='<div class="sm-row"><span>流量</span><span>'+fmtB(used)+' / '+fmtB(ui.total)+'（'+pct.toFixed(0)+'%）'+(ui.expire?' · 到期 '+new Date(ui.expire*1000).toLocaleDateString():'')+'</span></div><div class="sm-bar"><i class="'+(pct>=90?'bad':(pct>=75?'warn':''))+'" style="width:'+pct+'%"></i></div>';
       }
       head='<div class="sm-row"><span><b>'+esc(s.name)+'</b><div class="mut" style="font-size:12px;margin-top:2px">'+esc(s.urlHost||'')+' · '+s.nodeCount+' 个节点 · 更新于 '+fmtT(s.updatedAt)+'</div></span><span class="row"><label class="sm-sw"><input type="checkbox" data-suben="'+s.id+'" '+(s.enabled?'checked':'')+'><i></i></label><button class="btn gray" data-subupd="'+s.id+'">更新</button><button class="btn gray" data-subren="'+s.id+'">改名</button><button class="btn gray" data-subdel="'+s.id+'">删除</button></span></div>'
         +'<div class="sm-row"><span>自动更新间隔（分钟，0 = 用全局默认）</span><input class="set-num" type="number" min="0" max="10080" step="15" value="'+(s.autoMins||0)+'" data-submins="'+s.id+'"></div>'+uiHTML;
     } else {
       head='<div class="sm-row"><span class="mut">链接手动添加的节点（'+(g.nodes||[]).filter(n=>!n.isWarp).length+' 个）</span><span class="row"><button class="btn gray" data-test="group:manual">整组测速</button></span></div>';
     }
     if(g.kind==='sub') head+='<div class="sm-row"><span></span><button class="btn gray" data-test="group:'+g.id+'">整组测速</button></div>';
     const rows=(g.nodes||[]).map(n=>{
       if(n.isWarp) return '<div class="sm-row" style="'+(n.selected?'background:rgba(48,209,88,.10);border-radius:8px':'')+'"><span><b>WARP 中继</b> <span class="tag">wireguard</span><div class="mut" style="font-size:12px;margin-top:2px">'+esc(n.server||'')+'</div></span><span class="row"><span class="mut">未测</span>'+(n.selected?'<span class="tag" style="color:#30d158">当前出口</span>':'<button class="btn" data-sel="warp">选为出口</button>')+'</span></div>';
       return nodeRow(n);
     }).join('');
     return '<div class="set-sec" style="margin-top:14px">'+esc(g.name)+'</div><div class="sm-card">'+head+(rows||'<p class="mut">这个组里还没有节点。</p>')+'</div>';
   };
   const drawNpChart=()=>{
     const cv=document.getElementById('np-chart'); if(!cv) return;
     const dpr=window.devicePixelRatio||1, W=cv.clientWidth, H=cv.clientHeight; if(!W) return;
     if(cv.width!==Math.round(W*dpr)||cv.height!==Math.round(H*dpr)){ cv.width=W*dpr; cv.height=H*dpr; }
     const ctx=cv.getContext('2d'); ctx.setTransform(dpr,0,0,dpr,0,0); ctx.clearRect(0,0,W,H);
     const pad={l:4,r:4,t:8,b:6};
     ctx.strokeStyle='rgba(255,255,255,.08)'; ctx.lineWidth=1;
     for(let i=0;i<3;i++){ const y=pad.t+(H-pad.t-pad.b)*i/2; ctx.beginPath(); ctx.moveTo(pad.l,y); ctx.lineTo(W-pad.r,y); ctx.stroke(); }
     const pts=trafPts; if(!pts||pts.length<2){ ctx.fillStyle='rgba(255,255,255,.45)'; ctx.font='11px system-ui'; ctx.fillText(NP&&NP.coreRunning?(NP.clashOK?'等待流量数据…':'Clash API 不可用，暂无流量数据'):'核心未运行',pad.l+2,H/2); return; }
     let max=0; pts.forEach(p=>{ max=Math.max(max,p.rx,p.tx); }); if(max<=0) max=1; max*=1.15;
     const X=i=>pad.l+(W-pad.l-pad.r)*i/(pts.length-1), Y=v=>pad.t+(H-pad.t-pad.b)*(1-v/max);
     const line=(key,color,fill)=>{ ctx.beginPath(); pts.forEach((p,i)=>{ const x=X(i),y=Y(p[key]); i?ctx.lineTo(x,y):ctx.moveTo(x,y); }); ctx.strokeStyle=color; ctx.lineWidth=1.6; ctx.stroke(); if(fill){ ctx.lineTo(X(pts.length-1),H-pad.b); ctx.lineTo(X(0),H-pad.b); ctx.closePath(); const gr=ctx.createLinearGradient(0,pad.t,0,H-pad.b); gr.addColorStop(0,fill); gr.addColorStop(1,'rgba(0,0,0,0)'); ctx.fillStyle=gr; ctx.fill(); ctx.lineWidth=1; } };
     line('rx','#30d158','rgba(48,209,88,.20)'); line('tx','#0a84ff',null);
   };
   const paintNP=()=>{
     if(!NP) return;
     document.querySelectorAll('#np-mode button').forEach(x=>x.classList.toggle('on',x.dataset.m===NP.mode));
     document.querySelectorAll('#np-dns button').forEach(x=>x.classList.toggle('on',x.dataset.d===NP.dnsMode));
     const dr=document.getElementById('np-dnsrow'); if(dr) dr.style.display=NP.dnsMode==='custom'?'':'none';
     const dc=document.getElementById('np-dnscustom'); if(dc&&document.activeElement!==dc) dc.value=NP.dnsCustom||'';
     const ex=NP.exit||{};
     const exEl=document.getElementById('np-exit');
     if(exEl) exEl.innerHTML='<b>'+esc(ex.name||'—')+'</b>'+(ex.currentName?' <span class="mut">→ '+esc(ex.currentName)+'</span>':'')+(ex.type?' <span class="tag">'+esc(ex.type)+'</span>':'');
     const wEl=document.getElementById('np-warp');
     if(wEl) wEl.innerHTML=NP.warp&&NP.warp.registered
       ? '<div class="sm-row"><span>状态</span><span class="set-val">已注册 · 端点 '+esc(NP.warp.endpoint||'')+' · '+fmtT(NP.warp.registeredAt)+'</span></div><div class="sm-row"><span></span><span class="row"><button class="btn gray" id="np-warp-reg">重新注册</button><button class="btn red" id="np-warp-del">移除</button></span></div><p class="mut" style="margin-top:4px">可作任意节点的前置，也可在手动组里直接选为出口。</p>'
       : '<div class="sm-row"><span>状态</span><span class="set-val">未注册</span></div><div class="sm-row"><span></span><button class="btn" id="np-warp-reg">注册 WARP 中继</button></div><p class="mut" style="margin-top:4px">用 Cloudflare 的公开注册在本机生成一套 WireGuard 身份存进 Data，可给 IPv6 节点或普通节点当「前置」（链式代理）。注册走当前网络，失败会具名报错。</p>';
     const wr=document.getElementById('np-warp-reg'); if(wr) wr.onclick=async()=>{ busy('正在注册 WARP…'); const r=await post('/api/nodepro/warp',{action:'register'}); if(r.error){ alert('WARP 注册失败：'+r.error); busy('WARP 注册失败'); } else { NP=r; paintNP(); busy('WARP 已注册'); } };
     const wd=document.getElementById('np-warp-del'); if(wd) wd.onclick=async()=>{ if(!confirm('移除 WARP 中继？注册信息会从 Data 删除，用它当前置的节点会回到无前置。')) return; const r=await post('/api/nodepro/warp',{action:'remove'}); if(r.error) alert(r.error); else { NP=r; paintNP(); } };
     const gEl=document.getElementById('np-groups'); if(gEl) gEl.innerHTML=(NP.groups||[]).map(groupHTML).join('');
     const dEl=document.getElementById('np-defrules'); if(dEl) dEl.innerHTML=(NP.defaultRules||[]).map(r0=>'• '+esc(r0.label)+' → '+esc(r0.action)).join('<br>');
     if(!rulesDraft) rulesDraft=(NP.rules||[]).map(r0=>({...r0}));
     paintRules();
     const t=NP.traffic||{};
     const sess=document.getElementById('np-sess'); if(sess) sess.textContent='↓ '+fmtB(t.sessDown)+' · ↑ '+fmtB(t.sessUp);
     const rates=document.getElementById('np-rates');
     if(rates) rates.textContent=t.available?('↓ '+fmtRate(t.down)+' · ↑ '+fmtRate(t.up)):((NP.coreRunning?'Clash API 不可用':'核心未运行'));
   };
   const paintRules=()=>{
     const el=document.getElementById('np-rules'); if(!el) return;
     const tn={domain:'域名',suffix:'后缀',ipcidr:'IP 段'}, an={proxy:'代理',direct:'直连',block:'拒绝'};
     el.innerHTML=(rulesDraft&&rulesDraft.length)?rulesDraft.map((r0,i)=>'<div class="sm-row"><span><label class="sm-sw" style="vertical-align:-3px;margin-right:8px"><input type="checkbox" data-ren="'+i+'" '+(r0.enabled?'checked':'')+'><i></i></label>'+esc(tn[r0.type]||r0.type)+' · <b>'+esc(r0.value)+'</b> → '+esc(an[r0.action]||r0.action)+'</span><button class="btn gray" data-rdel="'+i+'">删除</button></div>').join(''):'<p class="mut">还没有自定义规则。</p>';
     el.querySelectorAll('[data-rdel]').forEach(x=>x.onclick=()=>{ rulesDraft.splice(+x.dataset.rdel,1); paintRules(); });
     el.querySelectorAll('[data-ren]').forEach(x=>x.onchange=()=>{ rulesDraft[+x.dataset.ren].enabled=x.checked; });
   };
   const loadNP=async()=>{ const r=await api('/api/nodepro/status'); if(r&&!r.error){ NP=r; paintNP(); } };
   const act=async(fn,t)=>{ busy(t||'处理中…'); const r=await fn(); if(r&&r.error){ alert(r.error); busy('出错：'+r.error); return null; } if(r) { NP=r; paintNP(); } busy('就绪'); return r; };
   // mode + dns segmented
   document.querySelectorAll('#np-mode button').forEach(x=>x.onclick=()=>act(async()=>post('/api/nodepro/mode',{mode:x.dataset.m}),'切换模式中…'));
   document.querySelectorAll('#np-dns button').forEach(x=>x.onclick=()=>{ const mode=x.dataset.d; if(mode==='custom'){ NP.dnsMode='custom'; paintNP(); const i=document.getElementById('np-dnscustom'); if(i) i.focus(); return; } act(async()=>post('/api/nodepro/dns',{mode}),'切换 DNS 中…'); });
   document.getElementById('np-dnssave').onclick=()=>act(async()=>post('/api/nodepro/dns',{mode:'custom',custom:document.getElementById('np-dnscustom').value}),'保存 DNS 中…');
   // groups: delegated actions
   const gWrap=document.getElementById('np-groups');
   gWrap.addEventListener('click',async e=>{
     const sel=e.target.closest('[data-sel]'); const tst=e.target.closest('[data-test]');
     const su=e.target.closest('[data-subupd]'), sr=e.target.closest('[data-subren]'), sd=e.target.closest('[data-subdel]');
     const vf=e.target.closest('[data-v6fix]');
     if(sel){ act(async()=>post('/api/nodepro/select',{target:sel.dataset.sel}),'切换出口中…'); return; }
     if(tst){ busy('测速中（整组会逐个测）…'); const r=await post('/api/nodepro/test',{target:tst.dataset.test}); if(r.error){ alert('测速失败：'+r.error); busy('测速失败'); } else { await loadNP(); busy('测速完成'); } return; }
     if(su){ busy('更新订阅中…'); const r=await post('/api/nodepro/subs',{action:'update',id:su.dataset.subupd}); if(r.error){ alert('更新失败：'+r.error); busy('订阅更新失败'); } else { NP=r; paintNP(); busy('订阅已更新'); } return; }
     if(sr){ const name=prompt('订阅新名字：'); if(name) { const r=await post('/api/nodepro/subs',{action:'rename',id:sr.dataset.subren,name}); if(r.error) alert(r.error); else { NP=r; paintNP(); } } return; }
     if(sd){ if(!confirm('删除这个订阅？它下面的节点会一并移除。')) return; const r=await post('/api/nodepro/subs',{action:'remove',id:sd.dataset.subdel}); if(r.error) alert(r.error); else { NP=r; paintNP(); } return; }
     if(vf){ const s2=document.getElementById('v6fix-'+vf.dataset.v6fix); if(s2) act(async()=>post('/api/nodepro/detour',{node:vf.dataset.v6fix,detour:s2.value}),'设置前置中…'); return; }
   });
   gWrap.addEventListener('change',async e=>{
     const dt=e.target.closest('[data-detour]');
     if(dt){ act(async()=>post('/api/nodepro/detour',{node:dt.dataset.detour,detour:dt.value}),'设置前置中…'); return; }
     const se=e.target.closest('[data-suben]');
     if(se){ const r=await post('/api/nodepro/subs',{action:'enable',id:se.dataset.suben,enabled:se.checked}); if(r.error){ alert(r.error); se.checked=!se.checked; } else { NP=r; paintNP(); } return; }
     const sm=e.target.closest('[data-submins]');
     if(sm){ const r=await post('/api/nodepro/subs',{action:'automins',id:sm.dataset.submins,autoMins:+sm.value||0}); if(r.error) alert(r.error); else NP=r; return; }
   });
   // add forms
   document.getElementById('nadd').onclick=async()=>{
     const v=document.getElementById('nlink').value.trim(); if(!v) return;
     busy('添加节点中…');
     const r=await api('/api/node',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({link:v})});
     if(r.error){ alert(r.error); busy('添加失败'); } else { document.getElementById('nlink').value=''; await loadNP(); busy(r.message||'已添加'); coreAct('status'); }
   };
   document.getElementById('nsubbtn').onclick=async()=>{
     const u=document.getElementById('nsub').value.trim(); if(!u) return;
     busy('拉取订阅中…');
     const r=await post('/api/nodepro/subs',{action:'add',name:document.getElementById('nsubname').value.trim(),url:u});
     if(r.error){ alert(r.error); busy('订阅添加失败'); if(r.groups){ NP=r; paintNP(); } }
     else { document.getElementById('nsub').value=''; document.getElementById('nsubname').value=''; NP=r; paintNP(); busy('订阅已添加'); }
   };
   document.getElementById('np-updall').onclick=async()=>{ busy('更新全部订阅中…'); const r=await post('/api/nodepro/subs',{action:'updateAll'}); if(r.error) alert('部分订阅更新失败：'+r.error); if(r.groups){ NP=r; paintNP(); } busy('订阅更新完成'); };
   // rules editor
   document.getElementById('np-radd').onclick=()=>{
     const v=document.getElementById('np-rvalue').value.trim(); if(!v) return;
     rulesDraft=rulesDraft||[]; rulesDraft.push({id:'',type:document.getElementById('np-rtype').value,value:v,action:document.getElementById('np-raction').value,enabled:true});
     document.getElementById('np-rvalue').value=''; paintRules();
   };
   document.getElementById('np-rsave').onclick=()=>act(async()=>post('/api/nodepro/rules',{rules:rulesDraft||[]}),'保存规则中…');
   // connections
   const paintConns=async()=>{
     const box=document.getElementById('np-conns'); if(!box) return;
     const r=await api('/api/nodepro/connections');
     const note=document.getElementById('np-conn-note'); if(note) note.textContent=r.available?((r.connections||[]).length+' 条'):(r.reason||'不可用');
     if(!r.available){ box.innerHTML='<p class="mut">'+(esc(r.reason||'Clash API 不可用'))+'——如实显示，不造数据。</p>'; return; }
     const cc2=r.connections||[];
     box.innerHTML=cc2.length?('<table style="width:100%;border-collapse:collapse;font-size:12.5px"><tr style="text-align:left;opacity:.6"><th style="padding:4px 6px">目标</th><th>网络</th><th>出站链</th><th>↓ / ↑</th><th>开始</th><th></th></tr>'+cc2.map(c=>'<tr style="border-top:1px solid rgba(255,255,255,.06)"><td style="padding:5px 6px;word-break:break-all">'+esc(c.host)+(c.port?':'+esc(c.port):'')+'</td><td>'+esc(c.network||'')+'</td><td>'+esc((c.chains||[]).join(' → '))+'</td><td style="white-space:nowrap">'+fmtB(c.download)+' / '+fmtB(c.upload)+'</td><td style="white-space:nowrap">'+(c.start?new Date(c.start*1000).toLocaleTimeString():'')+'</td><td><button class="btn gray" data-connclose="'+esc(c.id)+'">关闭</button></td></tr>').join('')+'</table>'):'<p class="mut">当前没有连接。</p>';
     box.querySelectorAll('[data-connclose]').forEach(x=>x.onclick=async()=>{ await post('/api/nodepro/connections/close',{id:x.dataset.connclose}); paintConns(); });
   };
   document.getElementById('np-conn-refresh').onclick=paintConns;
   document.getElementById('np-conn-closeall').onclick=async()=>{ const r=await post('/api/nodepro/connections/close',{all:true}); if(r.error) alert(r.error); paintConns(); };
   // core buttons + accel + misc (ids kept from the old page)
   coreAct('status'); loadNP(); paintConns();
   const loadAccel=async()=>{
     const r=await api('/api/accel');
     const s=document.getElementById('accel-state'); if(s) s.textContent=r.enabled?(r.coreRunning?'加速开 · 生效中':'加速开 · 节点启动后生效'):'加速关 · 软件内直连';
   };
   loadAccel();
   document.getElementById('accel-toggle').onclick=async()=>{
     const cur=await api('/api/accel');
     await api('/api/accel',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({enabled:!cur.enabled})});
     loadAccel(); coreAct('status');
   };
   document.getElementById('nclear').onclick=async()=>{ if(!confirm('清空全部节点与订阅？（WARP、规则、DNS 与模式保留）')) return; await api('/api/node',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'clear'})}); rulesDraft=null; await loadNP(); coreAct('status'); busy('已清空'); };
   document.getElementById('core-start').onclick=async()=>{ await coreAct('start'); setTimeout(()=>{ loadNP(); paintConns(); },600); };
   document.getElementById('core-stop').onclick=async()=>{ await coreAct('stop'); setTimeout(loadNP,600); };
   document.getElementById('core-refresh').onclick=()=>{ coreAct('status'); loadNP(); paintConns(); };
   document.getElementById('ptest').onclick=async()=>{
     busy('自检中（经节点连接 Google，最多约 15 秒）…');
     const r=await api('/api/proxytest',{method:'POST'});
     busy((r.ok?'✓ ':'✕ ')+(r.message||''));
   };
   document.getElementById('corelogbtn').onclick=async()=>{
     const r=await api('/api/core/log');
     const el=document.getElementById('corelog'); el.style.display='block'; el.textContent=r.log||'(暂无日志)';
   };
   // polling: traffic 1s (rates + chart only), connections
   // 2.5s, full status 15s (skipped while the user is editing
   // a control inside the page). Cleaned up on window close.
   const ivT=setInterval(async()=>{
     if(document.hidden||!document.getElementById('np-chart')) return;
     const r=await api('/api/nodepro/status'); if(!r||r.error) return;
     const t=r.traffic||{};
     if(t.available){ trafPts.push({rx:t.down||0,tx:t.up||0}); if(trafPts.length>90) trafPts.splice(0,trafPts.length-90); }
     const rates=document.getElementById('np-rates'); if(rates) rates.textContent=t.available?('↓ '+fmtRate(t.down)+' · ↑ '+fmtRate(t.up)):((r.coreRunning?'Clash API 不可用':'核心未运行'));
     const sess=document.getElementById('np-sess'); if(sess) sess.textContent='↓ '+fmtB(t.sessDown)+' · ↑ '+fmtB(t.sessUp);
     drawNpChart();
   },1000);
   const ivC=setInterval(()=>{ if(!document.hidden&&document.getElementById('np-conns')) paintConns(); },2500);
   const ivS=setInterval(()=>{ if(document.hidden) return; const ae=document.activeElement; if(ae&&(ae.tagName==='SELECT'||ae.tagName==='INPUT'||ae.tagName==='TEXTAREA')&&ae.closest&&ae.closest('.win')) return; loadNP(); },15000);
   const winEl=document.getElementById('win-core');
   if(winEl){ const mo=new MutationObserver(()=>{ if(!document.body.contains(winEl)){ clearInterval(ivT); clearInterval(ivC); clearInterval(ivS); mo.disconnect(); } }); mo.observe(document.body,{childList:true,subtree:true}); }
 }
}
export const __bind_history: any = { get(): any { return history; }, set(v: any): any { history = v; return v; } };

export const __bind_hi: any = { get(): any { return hi; }, set(v: any): any { hi = v; return v; } };
