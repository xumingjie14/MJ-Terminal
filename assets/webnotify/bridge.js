'use strict';
/* MJ 终端机 · 网页通知桥 -- 隔离世界中继（document_start）。
 *
 * 只做两件事，把主世界补丁（patch.js）和后台 sw.js 接起来：
 *   页面 -> 守护进程：监听 patch.js 的 postMessage 标记消息，
 *     用 chrome.runtime.sendMessage 转给 sw.js；
 *   守护进程 -> 页面：sw.js 收到「通知中心点了这条」后发来
 *     runtime 消息，这里 postMessage 回主世界。
 * 本脚本不碰页面 DOM、不读页面变量，和页面互不干扰。
 */
(function () {
  if (window.__mjNotifyBridge) return;
  window.__mjNotifyBridge = true;

  window.addEventListener('message', function (ev) {
    if (ev.source !== window) return;
    var d = ev.data;
    if (!d || d.__mjwn !== 1) return;
    try {
      chrome.runtime.sendMessage({ kind: d.kind, data: d.data }, function () {
        void chrome.runtime.lastError;
      });
    } catch (e) {}
  });

  try {
    chrome.runtime.onMessage.addListener(function (msg) {
      if (msg && msg.kind === 'click') {
        window.postMessage({ __mjwn: 2, kind: 'click', localId: msg.localId }, '*');
      }
    });
  } catch (e) {}
})();
