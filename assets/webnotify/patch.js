'use strict';
/* MJ 终端机 · 网页通知桥 -- 主世界补丁（document_start 注入）。
 *
 * 把页面侧的 Notification 构造路径接管下来：页面 new 出来的
 * 通知不再由 Chrome 自己弹（避免和终端机通知中心重复），而是
 * 经 postMessage 交给隔离世界的 bridge.js，再由后台 sw.js
 * 转发给本机守护进程，进终端机的通知中心。
 *
 * 对页面保持原生语义：
 *   - Notification.permission 实时读原生值；
 *   - requestPermission() 直接委托原生实现（授权弹窗、回调、
 *     Promise 都走 Chrome 自己的）；
 *   - 只有 permission === 'granted' 时才转发并触发 show 事件
 *     （没授权时原生通知本来也不会显示）；
 *   - 实例的字段（title/body/icon/tag/data…）、on* 处理器、
 *     addEventListener、close() 都在；
 *   - 用户在终端机通知中心点了这条通知时，click 事件会经
 *     daemon -> sw -> bridge 回到这个实例上触发。
 *
 * ServiceWorkerRegistration.showNotification（页面上下文里
 * 通过 registration 调的那条路）同样接管；但 Service Worker
 * 在后台自己发的推送通知不在这里 -- 那条路没有页面，属于
 * 「后台推送」，本桥管不到（如实说明，不假装）。
 *
 * 任何异常都吞掉：这个补丁绝不能把页面搞坏。
 */
(function () {
  try {
    var Native = window.Notification;
    if (!Native || window.__mjNotifyPatched) return;
    window.__mjNotifyPatched = true;

    var seq = 0;
    var byLocal = {}; // localId -> MJNotification instance

    function emit(kind, data) {
      try { window.postMessage({ __mjwn: 1, kind: kind, data: data }, '*'); } catch (e) {}
    }
    function absUrl(u) {
      try { return u ? new URL(u, location.href).href : ''; } catch (e) { return u || ''; }
    }
    function granted() {
      try { return Native.permission === 'granted'; } catch (e) { return false; }
    }

    function MJNotification(title, options) {
      options = options || {};
      this.title = String(title == null ? '' : title);
      this.body = options.body || '';
      this.icon = options.icon || '';
      this.tag = options.tag || '';
      this.dir = options.dir || 'auto';
      this.lang = options.lang || '';
      this.data = options.data;
      this.badge = options.badge || '';
      this.image = options.image || '';
      this.renotify = !!options.renotify;
      this.requireInteraction = !!options.requireInteraction;
      this.silent = !!options.silent;
      this.timestamp = Date.now();
      this.onclick = null;
      this.onshow = null;
      this.onerror = null;
      this.onclose = null;
      this._listeners = {};
      this._closed = false;
      this._localId = 'n' + (++seq) + '-' + Date.now().toString(36);
      byLocal[this._localId] = this;
      if (granted()) {
        emit('notify', {
          localId: this._localId,
          title: this.title,
          body: this.body,
          icon: absUrl(this.icon),
          tag: this.tag,
          url: location.href
        });
        var self = this;
        setTimeout(function () { if (!self._closed) self._fire('show'); }, 0);
      }
    }
    MJNotification.prototype.addEventListener = function (type, fn) {
      if (typeof fn !== 'function') return;
      (this._listeners[type] = this._listeners[type] || []).push(fn);
    };
    MJNotification.prototype.removeEventListener = function (type, fn) {
      var a = this._listeners[type];
      if (!a) return;
      var i = a.indexOf(fn);
      if (i >= 0) a.splice(i, 1);
    };
    MJNotification.prototype.dispatchEvent = function (ev) {
      if (ev && ev.type) this._fire(ev.type, ev);
      return true;
    };
    MJNotification.prototype._fire = function (type, ev) {
      var event = ev || { type: type, target: this, currentTarget: this };
      var handler = this['on' + type];
      try { if (typeof handler === 'function') handler.call(this, event); } catch (e) {}
      var list = (this._listeners[type] || []).slice();
      for (var i = 0; i < list.length; i++) {
        try { list[i].call(this, event); } catch (e) {}
      }
    };
    MJNotification.prototype.close = function () {
      if (this._closed) return;
      this._closed = true;
      emit('close', { localId: this._localId });
      this._fire('close');
    };
    Object.defineProperty(MJNotification, 'permission', {
      get: function () {
        try { return Native.permission; } catch (e) { return 'default'; }
      }
    });
    MJNotification.requestPermission = function (cb) {
      var p = Native.requestPermission();
      if (typeof cb === 'function' && p && typeof p.then === 'function') {
        p.then(function (v) { try { cb(v); } catch (e) {} }, function () {});
      }
      return p;
    };
    if ('maxActions' in Native) MJNotification.maxActions = Native.maxActions;
    window.Notification = MJNotification;

    /* 通知中心点击回传：sw -> bridge.js -> 这里 -> 实例 click。 */
    window.addEventListener('message', function (ev) {
      if (ev.source !== window) return;
      var d = ev.data;
      if (!d || d.__mjwn !== 2 || d.kind !== 'click') return;
      var inst = byLocal[d.localId];
      if (inst && !inst._closed) {
        inst._fire('click');
        try { window.focus(); } catch (e) {}
      }
    });

    /* 页面上下文的 registration.showNotification 一并接管：
       已授权时转发并吞掉（不让 Chrome 再弹一份），未授权时
       回落原生调用，保持页面看到的行为不变。 */
    try {
      var regProto = window.ServiceWorkerRegistration && ServiceWorkerRegistration.prototype;
      if (regProto && regProto.showNotification) {
        var origShow = regProto.showNotification;
        regProto.showNotification = function (title, options) {
          options = options || {};
          if (granted()) {
            emit('notify', {
              localId: '',
              title: String(title == null ? '' : title),
              body: options.body || '',
              icon: absUrl(options.icon || ''),
              tag: options.tag || '',
              url: location.href
            });
            return Promise.resolve();
          }
          return origShow.apply(this, arguments);
        };
      }
    } catch (e) {}
  } catch (e) {}
})();
