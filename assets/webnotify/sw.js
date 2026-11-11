'use strict';
/* MJ 终端机 · 网页通知桥 -- 后台 Service Worker。
 *
 * 职责：
 *   1. 读守护进程在释放扩展时盖进来的 config.json（守护进程
 *      的本机地址，端口随实例走），向 /api/webnotify/ping 报到
 *      （守护进程据此在控制台确认桥活着，并回告总开关状态）；
 *   2. 把内容脚本转来的通知 POST 给 /api/webnotify，记住
 *      「通知中心条目 id -> 发出它的标签页」；
 *   3. 长轮询 /api/webnotify/poll 等点击回传：用户在终端机
 *      通知中心点了某条网页通知时，把对应标签页切到前台，
 *      并让页面里那个 Notification 实例收到 click 事件。
 *
 * 总开关在守护进程侧（设置 → 通知 → 网页通知）：ping 回告
 * enabled=false 后本脚本停止转发；守护进程收到转发时还会再
 * 校验一次，开关以守护进程为准。
 */
var base = '';
var enabled = true;
var byId = {}; // daemon event id -> { tabId, localId }

function loadBase() {
  return fetch(chrome.runtime.getURL('config.json'))
    .then(function (r) { return r.json(); })
    .then(function (c) { if (c && c.base) base = String(c.base).replace(/\/+$/, ''); })
    .catch(function () {});
}

function ping() {
  if (!base) return;
  fetch(base + '/api/webnotify/ping', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ version: chrome.runtime.getManifest().version })
  })
    .then(function (r) { return r.json(); })
    .then(function (j) { if (j && typeof j.enabled === 'boolean') enabled = j.enabled; })
    .catch(function () {});
}

function handleClick(c) {
  var rec = byId[c.id];
  if (!rec) return;
  delete byId[c.id];
  try {
    chrome.tabs.get(rec.tabId, function (tab) {
      if (chrome.runtime.lastError || !tab) return;
      try { chrome.windows.update(tab.windowId, { focused: true }); } catch (e) {}
      try { chrome.tabs.update(rec.tabId, { active: true }); } catch (e) {}
      try {
        chrome.tabs.sendMessage(rec.tabId, { kind: 'click', localId: rec.localId }, function () {
          void chrome.runtime.lastError;
        });
      } catch (e) {}
    });
  } catch (e) {}
}

function poll() {
  if (!base) { setTimeout(poll, 5000); return; }
  fetch(base + '/api/webnotify/poll')
    .then(function (r) { return r.json(); })
    .then(function (j) {
      var clicks = (j && j.clicks) || [];
      for (var i = 0; i < clicks.length; i++) handleClick(clicks[i]);
      poll();
    })
    .catch(function () { setTimeout(poll, 3000); });
}

chrome.runtime.onMessage.addListener(function (msg, sender) {
  if (!msg || msg.kind !== 'notify') return; // 'close' 等其余种类：通知中心的条目由用户在中心里处理，这里不转发
  if (!enabled || !base) return;
  var d = msg.data || {};
  var payload = {
    title: d.title || '',
    body: d.body || '',
    icon: d.icon || '',
    tag: d.tag || '',
    url: d.url || (sender && sender.tab && sender.tab.url) || ''
  };
  fetch(base + '/api/webnotify', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload)
  })
    .then(function (r) { return r.json(); })
    .then(function (j) {
      if (j && j.disabled) { enabled = false; return; }
      if (j && j.id && sender && sender.tab && sender.tab.id != null) {
        byId[j.id] = { tabId: sender.tab.id, localId: d.localId || '' };
      }
    })
    .catch(function () {});
});

loadBase().then(function () { ping(); poll(); });
chrome.runtime.onStartup.addListener(function () { loadBase().then(ping); });
chrome.runtime.onInstalled.addListener(function () { loadBase().then(ping); });
