/* v0.59 block 3: the typed API client. Every function returns
   the contract type generated from the daemon's Go structs
   (web/src/api-types.ts -- do not edit it by hand; regenerate
   with `npm run api:gen`). New code and the event-driven
   re-pull paths (events.ts, the b2 live surfaces) go through
   here; the long-standing loaders keep their own api() calls so
   their behavior is untouched. api() itself normalizes the
   daemon's error envelope, so a failed call resolves with the
   legacy {error: string, errorCode} shape, never a throw. */
import { api } from './core';
import type {
  AccelStatusResponse, ComponentsResponse, ExtWinListResponse,
  FFStatusResponse, FilesListResponse, FilesSubListResponse,
  HotkeyStatusResponse, LayoutBlob, NotifyResponse,
  SettingsResponse, SystemInfo, UpdateSnapshot,
  VaultStatusResponse, WebAppsResponse,
} from '../api-types';

export function getSystem(): Promise<SystemInfo> { return api('/api/system'); }
export function getLayout(): Promise<LayoutBlob> { return api('/api/layout'); }
export function getWebApps(): Promise<WebAppsResponse> { return api('/api/apps'); }
/* SettingsResponse covers the fixed fields; the daemon also
   flattens per-app notification keys (notify_allow_app_<id>)
   into the same object -- read those off the raw record. */
export function getSettings(): Promise<SettingsResponse & Record<string, any>> { return api('/api/settings'); }
export function getNotify(): Promise<NotifyResponse> { return api('/api/notify'); }
export function getExtWins(): Promise<ExtWinListResponse> { return api('/api/extwin'); }
export function getFiles(): Promise<FilesListResponse> { return api('/api/files'); }
export function getFilesSub(path: string): Promise<FilesSubListResponse> { return api('/api/files/sub?path=' + encodeURIComponent(path)); }
export function getUpdate(): Promise<UpdateSnapshot> { return api('/api/update'); }
export function getVaultStatus(): Promise<VaultStatusResponse> { return api('/api/vault/status'); }
export function getFFStatus(): Promise<FFStatusResponse> { return api('/api/firefox'); }
export function getComponents(): Promise<ComponentsResponse> { return api('/api/components'); }
export function getAccel(): Promise<AccelStatusResponse> { return api('/api/accel'); }
export function getHotkeyStatus(): Promise<HotkeyStatusResponse> { return api('/api/hotkey/status'); }
