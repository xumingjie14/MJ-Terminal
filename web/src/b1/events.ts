/* v0.59 block 3: the change-feed client + the item-#8 wiring.
 *
 * One EventSource on /api/view/stream carries both families the
 * daemon sends there: view commands ({"cmd"}) -- the pre-bus
 * Spotlight channel, whose consumer migrated onto onCmd -- and
 * bus events ({"topic","id"}) from the daemon event bus
 * (events.go). The routing/dedupe core is the node-tested pure
 * module (web/src/pure/events.ts, a page global); this module
 * owns the connection (reconnect with capped exponential
 * backoff) and the surface wiring:
 *
 *   files     -> desktop grid (+ open folder pop) re-pull
 *   layout    -> desktop grid (+ open folder pop) re-pull
 *   webapps   -> desktop grid (+ open folder pop) re-pull
 *   storeapps -> desktop grid re-pull (store + sandbox apps)
 *   settings  -> settings re-pull + apply (wallpaper, Dock...)
 *   windows   -> Stage strip immediate refresh (its 1s/4s poll
 *                stays as the liveness channel)
 *   nodes     -> menu-bar node state re-pull (traffic and the
 *                connections list keep their own live channels)
 *   vault     -> vault status mirror refresh (+ lock screen)
 *
 * Deliberately NOT wired: the notification center (it has its
 * own /api/notify/stream), node traffic/connections (own live
 * polling), server monitor sampling. A surface re-pulls via the
 * typed API and re-renders in place -- never a page reload. */
import type { BusEvent } from '../api-types';
import { getVaultStatus } from './apiclient';
import { loadLayout } from './entries';
import { renderDesk } from './desktop';
import { loadWebApps } from './ctxmenu';
import { loadBoxApps, loadDeskFiles, loadStoreApps } from './appsload';
import { loadSettings } from './settings';
import { refreshExtStrip } from './extstrip';
import { pollNode } from './menubar';
import { openFolderEl, openFolderId, openFolderPop } from './folders';

const hub = createEvHub();

export const MJEvents = {
  on(topic: string, fn: (ev: BusEvent) => void): () => void { return hub.on(topic, fn); },
  onCmd(fn: (m: any) => void): () => void { return hub.onCmd(fn); },
};

/* Coalescing refresher: bursts (an import landing several
   events, a prune sweep) collapse into one re-pull ~140 ms
   after the first event; an event arriving mid-refresh queues
   exactly one more run. */
function refresher(fn: () => Promise<void>): () => void {
  let timer: any = null, running = false, again = false;
  const kick = () => {
    if (running) { again = true; return; }
    if (timer) return;
    timer = setTimeout(async () => {
      timer = null; running = true;
      try { await fn(); } catch (e) {}
      running = false;
      if (again) { again = false; kick(); }
    }, 140);
  };
  return kick;
}

function refreshFolderPop(): void {
  try { if (openFolderId && openFolderEl) openFolderPop(openFolderId, openFolderEl); } catch (e) {}
}

MJEvents.on('files', refresher(async () => { await loadDeskFiles(); renderDesk(); refreshFolderPop(); }));
MJEvents.on('layout', refresher(async () => { await loadLayout(); renderDesk(); refreshFolderPop(); }));
MJEvents.on('webapps', refresher(async () => { await loadWebApps(); renderDesk(); refreshFolderPop(); }));
MJEvents.on('storeapps', refresher(async () => { await loadStoreApps(); await loadBoxApps(); renderDesk(); }));
MJEvents.on('settings', refresher(async () => { await loadSettings(); }));
MJEvents.on('windows', refresher(async () => { await refreshExtStrip(); }));
MJEvents.on('nodes', refresher(async () => { await pollNode(); }));
MJEvents.on('vault', refresher(async () => {
  const s = await getVaultStatus();
  (window as any).__vault = s;
  if (s && s.locked && (window as any).__vaultShowLock) {
    try { (window as any).__vaultShowLock(); } catch (e) {}
  }
}));

/* ---- connection management ---- */
let es: any = null, attempt = 0, retryTimer: any = null, started = false;

function scheduleReconnect(): void {
  if (retryTimer) return;
  const delay = evBackoffDelay(attempt);
  attempt++;
  retryTimer = setTimeout(() => { retryTimer = null; connectEvents(); }, delay);
}

function connectEvents(): void {
  try {
    if (typeof EventSource === 'undefined') { scheduleReconnect(); return; }
    es = new EventSource('/api/view/stream');
    es.onopen = () => { attempt = 0; };
    es.onmessage = (ev: any) => { try { hub.dispatch(ev.data); } catch (e) {} };
    es.onerror = () => {
      try { if (es) es.close(); } catch (e) {}
      es = null;
      scheduleReconnect();
    };
  } catch (e) { scheduleReconnect(); }
}

export function startEvents(): void {
  if (started) return;
  started = true;
  connectEvents();
}

startEvents();
