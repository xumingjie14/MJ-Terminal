
/* ----- v0.50: menu bar completion / Launchpad / Spotlight / Finder -----
   UI replication piece #3, in the v0.47 shell pattern: every
   surface below is a shell over existing machinery -- the menus
   dispatch to the same settings keys and window buttons the rest
   of the UI uses, Launchpad reads the desktop's ONE layout list
   (DESKORDER + the folder model), Spotlight indexes only data
   the daemon already serves, and the Files app's Finder face
   sits on the unchanged Data/files endpoints and open routing.
   Nothing here bypasses the vault: while sealed, the lock screen
   covers the whole view, and Spotlight/Launchpad additionally
   refuse to open. */
export const ANIM_LP_MS=240;   // Launchpad fade + slight scale (CSS #launchpad.open)
export const ANIM_SPOT_MS=180; // Spotlight card pop (CSS #spotlight.open)
export const SPOT_RECENT_KEY='mj-spot-recent';
export const SPOT_SETPAGES=[['appearance','外观'],['desktop','桌面与 Dock'],['notify','通知'],['network','节点与网络'],['browser','浏览器'],['components','组件'],['isolation','应用隔离'],['storage','存储空间'],['security','安全与隐私'],['general','通用'],['about','关于'],['update','软件更新']];
