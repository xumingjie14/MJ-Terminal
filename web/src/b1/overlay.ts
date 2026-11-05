/* v0.62 (detail-fix ⑤): overlay session reporting. The desktop
   view's own overlays -- Control Center, Notification Center,
   menubar dropdowns, Spotlight, Launchpad, context menus --
   report their open/close transitions to the daemon
   (POST /api/view/overlay), which arms the z-order guard for
   exactly that span (stageguard.go): while an overlay is up,
   clicking it activates this shell window and plain Windows
   z-order slides the shell over claimed windows it overlaps;
   the guard lifts them back, so an overlay leaves every
   claimed window exactly as it was. Reporting is
   transition-only per kind -- repeat opens/closes collapse
   here -- and fire-and-forget: a lost report can never break
   the UI, and the daemon clears the table when the view's
   stream disconnects, so the guard cannot stay armed. */
import { api } from './core';
const overlayState: Record<string, boolean> = {};
export function stageOverlay(kind: string, on: boolean): void {
 if (overlayState[kind] === on) return;
 overlayState[kind] = on;
 try {
  api('/api/view/overlay', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ kind, on }) }).catch(() => null);
 } catch (e) { /* the guard is best-effort; the UI is not */ }
}
