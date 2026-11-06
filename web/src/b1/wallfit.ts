/* v0.63 （细节修复 ②): the 设为壁纸 fit dialog. Opened from the
   file context menu (desktop icons and the Files view share
   showFileCtx, which is the dialog's only caller) for image
   files only. macOS grammar, following the folder-pop sheet
   pattern: a centered glass panel over a dimmed backdrop, the
   image previewed against a desktop-shaped frame, a four-way
   segmented mode selector, 取消 / 设定.
 *
 * The preview is the applied rendering, not an approximation:
 * the frame's background is wpFitCSS(mode, rawURL) -- the very
 * string applyWallpaper puts on the desktop -- so 填充 crops,
 * 适合 letterboxes over the base tone, 居中 shows natural size
 * and 拉伸 distorts, exactly as the desktop will.
 *
 * Nothing is applied until 设定： the daemon endpoint
 * (POST /api/wallpaper/fromfile) copies the image into the
 * wallpaper store and persists the fit mode. An image the
 * browser cannot decode never gets that far -- the probe
 * <img> fails, the error shows in the dialog and 设定 stays
 * disabled; a daemon refusal (file gone, damaged) surfaces the
 * same way, in-dialog, with the desktop untouched. */
import { api, esc, __bind_customWall } from './core';
import { applyWallpaper } from './menubar';
import { wallFit } from './settings';
import { toast } from './appsload';
import { stageOverlay } from './overlay';

let wfEl: any = null;
let wfEsc: any = null;

export function closeWallFitDialog(): void {
  if (!wfEl) return;
  wfEl.remove();
  wfEl = null;
  stageOverlay('wallfit', false);
  if (wfEsc) { document.removeEventListener('keydown', wfEsc); wfEsc = null; }
}

export function openWallFitDialog(f: any): void {
  closeWallFitDialog();
  const rawURL = '/api/files/raw?path=' + encodeURIComponent(f.path);
  let mode = wallFit(); // the dialog opens on the persisted fit mode
  let ready = false;
  const el = document.createElement('div');
  el.id = 'wallfit';
  el.innerHTML = `<div class="wf-panel glasspanel">
    <div class="wf-title">设为壁纸</div>
    <div class="wf-name">${esc(f.name)}</div>
    <div class="wf-frame"></div>
    <div class="seg wf-seg">${WPFIT_MODES.map((m: any) => `<button data-m="${m.id}">${m.n}</button>`).join('')}</div>
    <div class="wf-err" style="display:none"></div>
    <div class="wf-btns"><button class="btn gray" data-wf="cancel">取消</button><button class="btn" data-wf="ok" disabled>设定</button></div>
  </div>`;
  document.body.appendChild(el);
  wfEl = el;
  stageOverlay('wallfit', true);
  const frame = el.querySelector('.wf-frame');
  const errEl = el.querySelector('.wf-err');
  const okBtn: any = el.querySelector('[data-wf="ok"]');
  const paint = () => {
    frame.style.background = wpFitCSS(mode, rawURL);
    el.querySelectorAll('.wf-seg button').forEach((b: any) => b.classList.toggle('on', b.dataset.m === mode));
  };
  paint();
  el.querySelectorAll('.wf-seg button').forEach((b: any) => b.onclick = () => { mode = b.dataset.m; paint(); });
  /* Decode probe: the preview background would silently show
     the base tone for an unreadable image, so decodability is
     proven separately -- no decode, no 设定. */
  const probe = new Image();
  probe.onload = () => { ready = true; okBtn.disabled = false; };
  probe.onerror = () => {
    errEl.textContent = '这张图片读不出来，无法设为壁纸。';
    errEl.style.display = '';
  };
  probe.src = rawURL;
  el.addEventListener('mousedown', (ev: any) => { if (ev.target === el) closeWallFitDialog(); });
  el.querySelector('[data-wf="cancel"]').onclick = () => closeWallFitDialog();
  okBtn.onclick = async () => {
    if (!ready) return;
    okBtn.disabled = true;
    const r = await api('/api/wallpaper/fromfile', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ path: f.path, fit: mode }) }).catch(() => null);
    if (!r || r.error) {
      errEl.textContent = '设定失败：' + ((r && r.error) || '接口没回应');
      errEl.style.display = '';
      okBtn.disabled = false;
      return;
    }
    __bind_customWall.set(true);
    applyWallpaper();
    closeWallFitDialog();
    toast('已设为壁纸');
  };
  wfEsc = (ev: any) => { if (ev.key === 'Escape') closeWallFitDialog(); };
  document.addEventListener('keydown', wfEsc);
}
