package main

// ---------- unified settings store (v0.30) ----------
//
// One file, Data/settings.json, holds the terminal's cross-cutting
// preferences. Before v0.30 they were scattered: the wallpaper
// choice and the Stage Manager switch lived in the desktop view's
// localStorage (which stays on the host PC -- exactly what a
// portable folder must not do), the idle-exit timeout was a
// hard-coded 30 minutes, and the phone-width landing numbers were
// compile-time constants in winmgr.go. They all live here now, so
// the settings travel with the folder like everything else.
//
// GET  /api/settings  -> every key, defaults merged in, plus a
//                        "fresh" flag (true when no settings file
//                        existed at load time -- the desktop uses it
//                        once to migrate its old localStorage values)
// POST /api/settings  -> a JSON object with any subset of the
//                        whitelisted keys; each value is validated,
//                        merged, persisted, and the change is logged
//                        (module "settings", key + new value only --
//                        none of these keys are secrets).
//
// Every read in the daemon goes through the accessors below, so a
// value changed in the Settings app takes effect without a restart
// (the one exception by design: a browser that is already open
// keeps the cache cap it was launched with).

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

type mjSettings struct {
	Wallpaper         string `json:"wallpaper"`         // preset key, see WALLPAPERS in index.html
	StageManager      bool   `json:"stageManager"`      // Stage Manager on/off
	IdleExitMinutes   int    `json:"idleExitMinutes"`   // daemon idle auto-exit; 0 = never
	PhoneMode         bool   `json:"phoneMode"`         // phone-width landing on/off
	PhoneTriggerWidth int    `json:"phoneTriggerWidth"` // desktop width below which phone mode triggers (px)
	PhoneWidth        int    `json:"phoneWidth"`        // landed width in phone mode (px)
	ChromeCacheMB     int    `json:"chromeCacheMB"`     // isolated Chrome --disk-cache-size cap; 0 = no cap
	// v0.31: Stage Manager auto-claim for browser-spawned windows
	// (popups, Ctrl+N, undocked DevTools). Default ON; the patrol in
	// winmgr.go reads it every tick, so toggling applies at once.
	StageAutoClaimBrowser bool `json:"stageAutoClaimBrowser"`
	// v0.32: imported files shown as desktop icons. Default ON;
	// OFF only hides the icons -- the files in Data/files and the
	// import channels are unaffected.
	FilesOnDesktop bool `json:"filesOnDesktop"`
	// v0.33: remote assistance (embedded Tailscale/tsnet) master
	// switch. Default OFF -- when off the networking never starts
	// and the terminal has exactly the attack surface it had
	// before v0.33. The remote settings POST path refuses this key:
	// the channel can never switch itself on or off.
	RemoteEnabled bool `json:"remoteEnabled"`
	// v0.37 window etiquette （开窗礼仪）, all default ON, read live
	// by winset.go on every decision:
	//   WinPlaceOnClaim   -- a window is landed once at first claim
	//                        (patrol claims included, not just
	//                        daemon launches); the desktop-backdrop
	//                        raise rides on this same switch.
	//   WinHideOnMinimize -- minimizing the desktop view hides the
	//                        whole claimed set with it; restoring
	//                        the view brings the set back.
	//   WinCloseToTray    -- closing the desktop view hides the
	//                        whole set and parks the daemon in the
	//                        host's notification area instead of
	//                        leaving the windows on the host desktop.
	WinPlaceOnClaim   bool `json:"winPlaceOnClaim"`
	WinHideOnMinimize bool `json:"winHideOnMinimize"`
	WinCloseToTray    bool `json:"winCloseToTray"`
	// v0.45 immersion （沉浸模式）, both default OFF -- the user
	// turns them on himself in 设置 → 桌面与 Dock; immerse.go
	// reads both live on every patrol tick and on a settings
	// kick, so flips apply to the whole registered set at once:
	//   ImmersiveMode    -- registered windows leave the host
	//                       taskbar and Alt-Tab (tool-window
	//                       style); Stage Manager + Dock become
	//                       the only switch entries.
	//   DesktopFullscreen -- the desktop view goes borderless
	//                       fullscreen on its own monitor.
	ImmersiveMode     bool `json:"immersiveMode"`
	DesktopFullscreen bool `json:"desktopFullscreen"`
	// v0.46 Dock basics （复刻线第一件）, read live by the desktop
	// view on every settings fetch -- the daemon itself never moves
	// the Dock, it only stores the preference:
	//   DockPosition -- "bottom" (default) / "left" / "right".
	//   DockAutoHide -- slide the Dock out until the pointer
	//                   touches its edge. Default OFF (findability
	//                   first, same philosophy as immersion).
	DockPosition string `json:"dockPosition"`
	DockAutoHide bool   `json:"dockAutoHide"`
	// v0.47: Dock icon size (the Control Center slider writes it,
	// the Dock reads it live), Control Center tile visibility,
	// per-source notification switches, and the Notification
	// Center widget layout. The node big tile has NO cc_show key:
	// like the manual's core controls it cannot be removed.
	DockIconSize        int  `json:"dockIconSize"`
	CCShowImmersive     bool `json:"cc_show_immersive"`
	CCShowStage         bool `json:"cc_show_stage"`
	CCShowFullscreen    bool `json:"cc_show_fullscreen"`
	CCShowDockHide      bool `json:"cc_show_dockhide"`
	CCShowRemote        bool `json:"cc_show_remote"`
	CCShowWallpaper     bool `json:"cc_show_wallpaper"`
	NotifyAllowSandbox  bool `json:"notify_allow_sandbox"`
	NotifyAllowUpdate   bool `json:"notify_allow_update"`
	NotifyAllowServer   bool `json:"notify_allow_servermon"`
	NotifyAllowNode     bool `json:"notify_allow_node"`
	NotifyAllowRemote   bool `json:"notify_allow_remote"`
	NotifyAllowFiles    bool `json:"notify_allow_files"`
	NotifyBannerSandbox bool `json:"notify_banner_sandbox"`
	NotifyBannerUpdate  bool `json:"notify_banner_update"`
	NotifyBannerServer  bool `json:"notify_banner_servermon"`
	NotifyBannerNode    bool `json:"notify_banner_node"`
	NotifyBannerRemote  bool `json:"notify_banner_remote"`
	NotifyBannerFiles   bool `json:"notify_banner_files"`
	// v0.54: per-app notification switches （设置 → 通知 → 应用通知）.
	// Keys are app notify ids (store-<id> / prod-<id> / firefox /
	// openclaw / web-<slug>); an ABSENT entry means the default:
	// allowed. Only explicit user choices are stored, so a newly
	// installed app is on by default without any migration. The
	// settings wire keys are notify_allow_app_<id> /
	// notify_banner_app_<id>, validated by validNotifyAppID.
	NotifyAllowApp  map[string]bool `json:"notify_allow_app,omitempty" mjapi:"-"`
	NotifyBannerApp map[string]bool `json:"notify_banner_app,omitempty" mjapi:"-"`
	// NCWidgets is a JSON array [{"id":"calendar","show":true},...]
	// in display order; ids are calendar/server/node.
	NCWidgets string `json:"nc_widgets"`
	// v0.48: a USER-initiated full exit （设置退出 / 托盘退出 /
	// 顶栏关机） also closes the apps the terminal opened
	// (polite WM_CLOSE first, then termination for stragglers).
	// Never applies to the update handover, and to the idle
	// auto-exit only via the vault (a seal needs the apps shut;
	// without the vault the idle exit leaves them alone).
	// Default ON.
	ExitClosesApps bool `json:"exitClosesApps"`
	// v0.59: the updater's beta channel. false (default) = the
	// updater checks the newest OFFICIAL release only; true =
	// checks target the newest prerelease instead (in-app test
	// builds). Local-only setting: the remote-assist settings
	// projection carries the key (it is part of settingsMap)
	// but no remote surface can flip it -- the settings POST is
	// not on the remote mux.
	UpdateBeta bool `json:"updateBeta"`
	// v0.49 node professionalization: durable preferences for the
	// node subsystem. The live state (current mode, selection,
	// subscriptions, rules, DNS choice) lives in Data/nodes.json
	// (nodes.go); these are the cross-cutting preferences the
	// spec parks in Settings:
	//   NodeStartMode      -- "last" (restore the persisted mode,
	//                         default) or a fixed rule/global/
	//                         direct applied at core autostart.
	//   NodeSubAuto        -- subscription auto-update master
	//                         switch (default ON).
	//   NodeSubIntervalMin -- default auto-update interval in
	//                         minutes (a subscription can override
	//                         with its own; 0 there = this value).
	//   NodeTestURL        -- HTTP urltest target for latency
	//                         tests through a node.
	//   NodeTestTimeoutMs  -- per-node latency test timeout.
	//   NodeClashAPI       -- enable the sing-box Clash API in the
	//                         generated config (live connections,
	//                         traffic, hot switching). Default ON;
	//                         OFF degrades those features honestly
	//                         (switching falls back to a core
	//                         restart, latency to TCP timing).
	NodeStartMode      string `json:"nodeStartMode"`
	NodeSubAuto        bool   `json:"nodeSubAuto"`
	NodeSubIntervalMin int    `json:"nodeSubIntervalMin"`
	NodeTestURL        string `json:"nodeTestURL"`
	NodeTestTimeoutMs  int    `json:"nodeTestTimeoutMs"`
	NodeClashAPI       bool   `json:"nodeClashAPI"`
	// The boss key (hotkey.go / bosskey.go): a combo string
	// registered with the host OS -- "F8" (default), "Ctrl+F8",
	// "Alt+Q", "Ctrl+Shift+F9", or "off". The v0.50 values
	// (F8 / F9 / F7 / off) are modifier-less combos and migrate
	// unchanged; sanitizeSettings canonicalizes through
	// parseHotkeyCombo. Changing it re-registers immediately; a
	// combo another program already owns leaves the daemon
	// unregistered (the setting is NOT rolled back) and the
	// Settings page shows 「被占用」 (GET /api/hotkey/status).
	SummonHotkey string `json:"summonHotkey"`
	// v0.51: which office component suite opens office documents
	// by default ("onlyoffice" / "libreoffice"). The file-open
	// router (procomp.go) falls back to the other suite when the
	// chosen one is not installed, and guides the user to the
	// component download when neither is.
	DefaultOfficeSuite string `json:"defaultOfficeSuite"`
	// v0.56: Stage Manager external-window cards -- "preview"
	// (default: a live PrintWindow thumbnail of the real window)
	// or "identity" (icon + cleaned page title + site). Read live
	// by the desktop view on every strip paint; the daemon only
	// stores the preference.
	StageDisplay string `json:"stageDisplay"`
	// v0.56: desktop icon size in px (the desktop grid's pitch
	// follows it). Written by the 设置 → 外观 slider, the Control
	// Center slider and the desktop right-click presets; read
	// live by the desktop view. The Dock has its own size and is
	// NOT governed by this key.
	DeskIconSize int `json:"deskIconSize"`
	// v0.56: desktop sort mode -- "manual" (default; the user's
	// own DESKORDER), "name", "freq" （最常用）, "kind", "added".
	// While a sort is active the view re-sorts the top-level
	// order on every render; any manual drag flips it back to
	// "manual" (the sort never fights the user for an icon).
	DeskSort string `json:"deskSort"`
}

func defaultSettings() mjSettings {
	return mjSettings{
		Wallpaper:             "tahoe",
		StageManager:          false,
		IdleExitMinutes:       30,
		PhoneMode:             true,
		PhoneTriggerWidth:     1200,
		PhoneWidth:            400,
		ChromeCacheMB:         200,
		StageAutoClaimBrowser: true,
		FilesOnDesktop:        true,
		RemoteEnabled:         false,
		WinPlaceOnClaim:       true,
		WinHideOnMinimize:     true,
		WinCloseToTray:        true,
		ImmersiveMode:         false,
		DesktopFullscreen:     false,
		DockPosition:          "bottom",
		DockAutoHide:          false,
		DockIconSize:          52,
		CCShowImmersive:       true,
		CCShowStage:           true,
		CCShowFullscreen:      true,
		CCShowDockHide:        true,
		CCShowRemote:          true,
		CCShowWallpaper:       true,
		NotifyAllowSandbox:    true,
		NotifyAllowUpdate:     true,
		NotifyAllowServer:     true,
		NotifyAllowNode:       true,
		NotifyAllowRemote:     true,
		NotifyAllowFiles:      true,
		NotifyBannerSandbox:   true,
		NotifyBannerUpdate:    true,
		NotifyBannerServer:    true,
		NotifyBannerNode:      true,
		NotifyBannerRemote:    true,
		NotifyBannerFiles:     true,
		NCWidgets:             defaultNCWidgets,
		ExitClosesApps:        true,
		NodeStartMode:         "last",
		NodeSubAuto:           true,
		NodeSubIntervalMin:    360,
		NodeTestURL:           "http://www.gstatic.com/generate_204",
		NodeTestTimeoutMs:     5000,
		NodeClashAPI:          true,
		SummonHotkey:          "F8",
		DefaultOfficeSuite:    "onlyoffice",
		StageDisplay:          "preview",
		DeskIconSize:          54,
		DeskSort:              "manual",
	}
}

var stageDisplays = map[string]bool{"preview": true, "identity": true}

var deskSorts = map[string]bool{"manual": true, "name": true, "freq": true, "kind": true, "added": true}

var officeSuites = map[string]bool{"onlyoffice": true, "libreoffice": true}

var nodeStartModes = map[string]bool{"last": true, "rule": true, "global": true, "direct": true}

// defaultNCWidgets: Notification Center widgets in display order
// (calendar, server, node -- all shown). The frontend editor
// rewrites this; sanitizeSettings keeps it well-formed.
const defaultNCWidgets = `[{"id":"calendar","show":true},{"id":"server","show":true},{"id":"node","show":true}]`

var ncWidgetIDs = map[string]bool{"calendar": true, "server": true, "node": true}

// ncWidgetsValid reports whether v is a well-formed widget
// layout: a non-empty array of {id, show} with known, unique ids.
func ncWidgetsValid(v string) bool {
	if strings.TrimSpace(v) == "" {
		return false
	}
	var arr []struct {
		ID   string `json:"id"`
		Show bool   `json:"show"`
	}
	if json.Unmarshal([]byte(v), &arr) != nil || len(arr) == 0 {
		return false
	}
	seen := map[string]bool{}
	for _, w := range arr {
		if !ncWidgetIDs[w.ID] || seen[w.ID] {
			return false
		}
		seen[w.ID] = true
	}
	return true
}

// sanitizeNCWidgets canonicalizes the widget layout JSON.
// Anything malformed resets to the default layout (a broken
// editor state must never hide the whole widget area).
func sanitizeNCWidgets(v string) string {
	if !ncWidgetsValid(v) {
		return defaultNCWidgets
	}
	var arr []struct {
		ID   string `json:"id"`
		Show bool   `json:"show"`
	}
	_ = json.Unmarshal([]byte(v), &arr)
	out, err := json.Marshal(arr)
	if err != nil {
		return defaultNCWidgets
	}
	return string(out)
}

// boolSettingPtr resolves the v0.47 boolean key families
// (cc_show_*, notify_allow_*, notify_banner_*) to their struct
// field. It is the whitelist for those families: an unknown key
// returns nil and the POST handler rejects it like any other
// unknown key.
func boolSettingPtr(s *mjSettings, key string) *bool {
	switch key {
	case "cc_show_immersive":
		return &s.CCShowImmersive
	case "cc_show_stage":
		return &s.CCShowStage
	case "cc_show_fullscreen":
		return &s.CCShowFullscreen
	case "cc_show_dockhide":
		return &s.CCShowDockHide
	case "cc_show_remote":
		return &s.CCShowRemote
	case "cc_show_wallpaper":
		return &s.CCShowWallpaper
	case "notify_allow_sandbox":
		return &s.NotifyAllowSandbox
	case "notify_allow_update":
		return &s.NotifyAllowUpdate
	case "notify_allow_servermon":
		return &s.NotifyAllowServer
	case "notify_allow_node":
		return &s.NotifyAllowNode
	case "notify_allow_remote":
		return &s.NotifyAllowRemote
	case "notify_allow_files":
		return &s.NotifyAllowFiles
	case "notify_banner_sandbox":
		return &s.NotifyBannerSandbox
	case "notify_banner_update":
		return &s.NotifyBannerUpdate
	case "notify_banner_servermon":
		return &s.NotifyBannerServer
	case "notify_banner_node":
		return &s.NotifyBannerNode
	case "notify_banner_remote":
		return &s.NotifyBannerRemote
	case "notify_banner_files":
		return &s.NotifyBannerFiles
	}
	return nil
}

// notifyAllowed / notifyBanner read the per-source notification
// switches (used by notify.go; unknown sources default to allowed
// so a new source can never be silently muted).
func notifyAllowed(s mjSettings, source string) bool {
	if p := boolSettingPtr(&s, "notify_allow_"+source); p != nil {
		return *p
	}
	return true
}

func notifyBanner(s mjSettings, source string) bool {
	if p := boolSettingPtr(&s, "notify_banner_"+source); p != nil {
		return *p
	}
	return true
}

// validNotifyAppID is the whitelist rule for the dynamic part of
// the v0.54 per-app notification keys: a short slug the daemon
// can safely store and the frontend can safely render into an
// element id -- lowercase letters, digits and dashes, starting
// with a letter or digit, at most 64 chars. Anything else in a
// settings POST is rejected, and sanitizeSettings prunes stored
// entries that fail it.
func validNotifyAppID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case c == '-' && i > 0:
		default:
			return false
		}
	}
	return true
}

// notifyAppAllowed / notifyAppBanner read an app's switches.
// Absent = default ON (a newly installed app notifies until the
// user says otherwise) -- the mirror image of the source
// switches, whose fields default true in defaultSettings.
func notifyAppAllowed(s mjSettings, appID string) bool {
	if v, ok := s.NotifyAllowApp[appID]; ok {
		return v
	}
	return true
}

func notifyAppBanner(s mjSettings, appID string) bool {
	if v, ok := s.NotifyBannerApp[appID]; ok {
		return v
	}
	return true
}

var dockPositions = map[string]bool{"bottom": true, "left": true, "right": true}

var wallpaperKeys = map[string]bool{
	"tahoe": true, "sonoma": true, "sequoia": true,
	"ventura": true, "aurora": true, "graphite": true,
}

var setState struct {
	sync.Mutex
	loaded bool
	fresh  bool // no settings.json existed when first loaded
	cur    mjSettings
}

func settingsPath() string { return filepath.Join(dataDir(), "settings.json") }

// settingsLoadLocked reads the file onto a defaults-initialized
// struct (missing keys keep their defaults) and clamps every field
// back into its valid range, so a hand-edited or damaged file can
// never push the daemon into a weird state. Caller holds the lock.
func settingsLoadLocked() {
	if setState.loaded {
		return
	}
	setState.loaded = true
	setState.cur = defaultSettings()
	// v0.57: through the data layer (datastore.go) -- a damaged
	// settings.json is restored from its keep-last-good backup
	// there and the event logged under [data]; only a genuinely
	// absent file counts as a fresh install.
	b, status, err := dsLoad("settings")
	if status == dsMissing {
		setState.fresh = true
		return
	}
	if err != nil {
		return // corrupt with no usable backup: defaults, [data] has the loud record
	}
	var s mjSettings = setState.cur
	if err := json.Unmarshal(b, &s); err != nil {
		logf("settings", "warn", "settings.json 解析失败，已回退默认值：%v", err)
		return
	}
	setState.cur = sanitizeSettings(s)
}

// sanitizeSettings forces every field into its valid domain,
// substituting the default for anything out of range.
func sanitizeSettings(s mjSettings) mjSettings {
	d := defaultSettings()
	if !wallpaperKeys[s.Wallpaper] {
		s.Wallpaper = d.Wallpaper
	}
	if s.IdleExitMinutes < 0 || s.IdleExitMinutes > 240 {
		s.IdleExitMinutes = d.IdleExitMinutes
	}
	if s.PhoneTriggerWidth < 600 || s.PhoneTriggerWidth > 4000 {
		s.PhoneTriggerWidth = d.PhoneTriggerWidth
	}
	if s.PhoneWidth < 280 || s.PhoneWidth > 800 {
		s.PhoneWidth = d.PhoneWidth
	}
	if !(s.ChromeCacheMB == 0 || (s.ChromeCacheMB >= 50 && s.ChromeCacheMB <= 2000)) {
		s.ChromeCacheMB = d.ChromeCacheMB
	}
	if !dockPositions[s.DockPosition] {
		s.DockPosition = d.DockPosition
	}
	if s.DockIconSize < 44 || s.DockIconSize > 64 {
		s.DockIconSize = d.DockIconSize
	}
	s.NCWidgets = sanitizeNCWidgets(s.NCWidgets)
	if !nodeStartModes[s.NodeStartMode] {
		s.NodeStartMode = d.NodeStartMode
	}
	if s.NodeSubIntervalMin < 15 || s.NodeSubIntervalMin > 10080 {
		s.NodeSubIntervalMin = d.NodeSubIntervalMin
	}
	if !validTestURL(s.NodeTestURL) {
		s.NodeTestURL = d.NodeTestURL
	}
	if s.NodeTestTimeoutMs < 1000 || s.NodeTestTimeoutMs > 15000 {
		s.NodeTestTimeoutMs = d.NodeTestTimeoutMs
	}
	// v0.52: the boss-key value canonicalizes through the combo
	// parser -- legacy F7 / F8 / F9 / off pass through as-is,
	// "ctrl+f8" becomes "Ctrl+F8", and anything the parser
	// rejects falls back to the F8 default.
	if c, err := canonicalHotkey(s.SummonHotkey); err == nil {
		s.SummonHotkey = c
	} else {
		s.SummonHotkey = d.SummonHotkey
	}
	if !officeSuites[s.DefaultOfficeSuite] {
		s.DefaultOfficeSuite = d.DefaultOfficeSuite
	}
	if !stageDisplays[s.StageDisplay] {
		s.StageDisplay = d.StageDisplay
	}
	if s.DeskIconSize < 36 || s.DeskIconSize > 96 {
		s.DeskIconSize = d.DeskIconSize
	}
	if !deskSorts[s.DeskSort] {
		s.DeskSort = d.DeskSort
	}
	// v0.54: prune per-app notification entries whose ids fail
	// the whitelist (a hand-edited settings.json must not smuggle
	// arbitrary keys into the store). Valid entries pass through.
	s.NotifyAllowApp = pruneNotifyAppMap(s.NotifyAllowApp)
	s.NotifyBannerApp = pruneNotifyAppMap(s.NotifyBannerApp)
	return s
}

// pruneNotifyAppMap returns a copy of m holding only entries
// whose keys are valid app notify ids (nil stays nil).
func pruneNotifyAppMap(m map[string]bool) map[string]bool {
	if m == nil {
		return nil
	}
	out := make(map[string]bool, len(m))
	for k, v := range m {
		if validNotifyAppID(k) {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// validTestURL: the urltest target must be an absolute http(s)
// URL -- it is handed to sing-box / the Clash API verbatim.
func validTestURL(v string) bool {
	if len(v) < 10 || len(v) > 300 {
		return false
	}
	return strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://")
}

func settingsGet() mjSettings {
	setState.Lock()
	defer setState.Unlock()
	settingsLoadLocked()
	return setState.cur
}

func settingsFresh() bool {
	setState.Lock()
	defer setState.Unlock()
	settingsLoadLocked()
	return setState.fresh
}

// settingsSaveLocked persists through the data layer (atomic +
// keep-last-good backup). Caller holds the lock.
func settingsSaveLocked() error {
	b, err := json.MarshalIndent(setState.cur, "", "  ")
	if err != nil {
		return err
	}
	if err := dsSave("settings", b); err != nil {
		return err
	}
	setState.fresh = false
	return nil
}

func settingsMap() map[string]any {
	s := settingsGet()
	// v0.59 block 3: the fixed shape is the API contract struct
	// (settingsResponse = mjSettings + fresh, apicontract.go). It
	// is round-tripped into a map ONLY so the dynamic per-app
	// keys below can be merged in; the emitted keys and values
	// are byte-identical to the old hand-built literal, which the
	// remote-assist settings projection relies on.
	sb, _ := json.Marshal(settingsResponse{mjSettings: s, Fresh: settingsFresh()})
	out := map[string]any{}
	_ = json.Unmarshal(sb, &out)
	// The per-app maps ride the wire ONLY in their flattened
	// notify_allow_app_<id> form (the loops below); the object
	// form is the on-disk representation and was never a key of
	// this map before the contract refactor either.
	delete(out, "notify_allow_app")
	delete(out, "notify_banner_app")
	// v0.54: per-app notification switches are emitted flattened
	// (notify_allow_app_<id>: bool) for the entries the user has
	// explicitly set; an absent key reads as the default (on) on
	// the view side, exactly like the daemon's own default.
	for id, v := range s.NotifyAllowApp {
		out["notify_allow_app_"+id] = v
	}
	for id, v := range s.NotifyBannerApp {
		out["notify_banner_app_"+id] = v
	}
	return out
}

// ---------- accessors used across the daemon ----------

func settingIdleExitMinutes() int { return settingsGet().IdleExitMinutes }

func settingStageDisplay() string { return settingsGet().StageDisplay }

func settingPhone() (enabled bool, triggerWidth, width int) {
	s := settingsGet()
	return s.PhoneMode, s.PhoneTriggerWidth, s.PhoneWidth
}

func settingChromeCacheMB() int { return settingsGet().ChromeCacheMB }

func settingStageAutoClaimBrowser() bool { return settingsGet().StageAutoClaimBrowser }

func settingFilesOnDesktop() bool { return settingsGet().FilesOnDesktop }

func settingRemoteEnabled() bool { return settingsGet().RemoteEnabled }

func settingWinPlaceOnClaim() bool { return settingsGet().WinPlaceOnClaim }

func settingWinHideOnMinimize() bool { return settingsGet().WinHideOnMinimize }

func settingWinCloseToTray() bool { return settingsGet().WinCloseToTray }

func settingImmersiveMode() bool { return settingsGet().ImmersiveMode }

func settingDesktopFullscreen() bool { return settingsGet().DesktopFullscreen }

func settingExitClosesApps() bool { return settingsGet().ExitClosesApps }

func settingNodeStartMode() string { return settingsGet().NodeStartMode }

func settingNodeSubAuto() bool { return settingsGet().NodeSubAuto }

func settingNodeSubIntervalMin() int { return settingsGet().NodeSubIntervalMin }

func settingNodeTestURL() string { return settingsGet().NodeTestURL }

func settingNodeTestTimeoutMs() int { return settingsGet().NodeTestTimeoutMs }

func settingNodeClashAPI() bool { return settingsGet().NodeClashAPI }

func settingSummonHotkey() string { return settingsGet().SummonHotkey }

func settingDefaultOfficeSuite() string { return settingsGet().DefaultOfficeSuite }

// settingsSetRemoteEnabledInternal flips the remote-assistance
// master switch from inside the daemon (the pairing page's
// "disconnect and clear pairings" action), going through the same
// persistence + logging as a settings POST. The remote mux can
// never reach this: its settings POST refuses the key outright.
func settingsSetRemoteEnabledInternal(v bool) {
	setState.Lock()
	settingsLoadLocked()
	changed := setState.cur.RemoteEnabled != v
	setState.cur.RemoteEnabled = v
	err := settingsSaveLocked()
	setState.Unlock()
	if err != nil {
		logf("settings", "error", "设置保存失败：%v", err)
		return
	}
	if changed {
		logf("settings", "info", "设置已更新：%s", boolLog("remoteEnabled", v))
	}
	remoteKick()
}

// ---------- HTTP ----------

func settingsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, settingsMap())
	case http.MethodPost:
		var raw map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			apiErrBadRequest(w, errCodeBadJSON, "请求内容不对")
			return
		}
		setState.Lock()
		settingsLoadLocked()
		next := setState.cur
		var changed []string
		fail := ""
		appMapsCloned := false
		for key, val := range raw {
			// v0.54: the per-app notification switches are
			// dynamic keys (notify_allow_app_<id> /
			// notify_banner_app_<id>) resolving into the
			// NotifyAllowApp / NotifyBannerApp maps. The id
			// must pass the whitelist; the maps on `next` are
			// cloned before the first mutation (next is a
			// shallow copy of the live settings -- mutating its
			// maps in place would leak an unsaved change if a
			// later key in the same POST failed).
			if strings.HasPrefix(key, "notify_allow_app_") || strings.HasPrefix(key, "notify_banner_app_") {
				allow := strings.HasPrefix(key, "notify_allow_app_")
				id := strings.TrimPrefix(key, "notify_allow_app_")
				id = strings.TrimPrefix(id, "notify_banner_app_")
				var v bool
				switch {
				case !validNotifyAppID(id):
					fail = "应用通知键不对：" + key
				case json.Unmarshal(val, &v) != nil:
					fail = key + " 必须是 true/false"
				default:
					if !appMapsCloned {
						appMapsCloned = true
						next.NotifyAllowApp = pruneNotifyAppMap(next.NotifyAllowApp)
						if next.NotifyAllowApp == nil {
							next.NotifyAllowApp = map[string]bool{}
						}
						next.NotifyBannerApp = pruneNotifyAppMap(next.NotifyBannerApp)
						if next.NotifyBannerApp == nil {
							next.NotifyBannerApp = map[string]bool{}
						}
					}
					m := next.NotifyBannerApp
					if allow {
						m = next.NotifyAllowApp
					}
					if cur, ok := m[id]; !ok || cur != v {
						m[id] = v
						changed = append(changed, boolLog(key, v))
					}
				}
				if fail != "" {
					break
				}
				continue
			}
			switch key {
			case "wallpaper":
				var v string
				if json.Unmarshal(val, &v) != nil || !wallpaperKeys[v] {
					fail = "wallpaper 不是有效的预设名"
				} else if v != next.Wallpaper {
					next.Wallpaper = v
					changed = append(changed, "wallpaper = "+v)
				}
			case "stageManager":
				var v bool
				if json.Unmarshal(val, &v) != nil {
					fail = "stageManager 必须是 true/false"
				} else if v != next.StageManager {
					next.StageManager = v
					changed = append(changed, boolLog("stageManager", v))
				}
			case "idleExitMinutes":
				var v int
				if json.Unmarshal(val, &v) != nil || v < 0 || v > 240 {
					fail = "idleExitMinutes 须在 0–240 之间（0 = 不自动退出）"
				} else if v != next.IdleExitMinutes {
					next.IdleExitMinutes = v
					changed = append(changed, intLog("idleExitMinutes", v))
				}
			case "phoneMode":
				var v bool
				if json.Unmarshal(val, &v) != nil {
					fail = "phoneMode 必须是 true/false"
				} else if v != next.PhoneMode {
					next.PhoneMode = v
					changed = append(changed, boolLog("phoneMode", v))
				}
			case "phoneTriggerWidth":
				var v int
				if json.Unmarshal(val, &v) != nil || v < 600 || v > 4000 {
					fail = "phoneTriggerWidth 须在 600–4000 之间"
				} else if v != next.PhoneTriggerWidth {
					next.PhoneTriggerWidth = v
					changed = append(changed, intLog("phoneTriggerWidth", v))
				}
			case "phoneWidth":
				var v int
				if json.Unmarshal(val, &v) != nil || v < 280 || v > 800 {
					fail = "phoneWidth 须在 280–800 之间"
				} else if v != next.PhoneWidth {
					next.PhoneWidth = v
					changed = append(changed, intLog("phoneWidth", v))
				}
			case "stageAutoClaimBrowser":
				var v bool
				if json.Unmarshal(val, &v) != nil {
					fail = "stageAutoClaimBrowser 必须是 true/false"
				} else if v != next.StageAutoClaimBrowser {
					next.StageAutoClaimBrowser = v
					changed = append(changed, boolLog("stageAutoClaimBrowser", v))
				}
			case "filesOnDesktop":
				var v bool
				if json.Unmarshal(val, &v) != nil {
					fail = "filesOnDesktop 必须是 true/false"
				} else if v != next.FilesOnDesktop {
					next.FilesOnDesktop = v
					changed = append(changed, boolLog("filesOnDesktop", v))
				}
			case "winPlaceOnClaim":
				var v bool
				if json.Unmarshal(val, &v) != nil {
					fail = "winPlaceOnClaim 必须是 true/false"
				} else if v != next.WinPlaceOnClaim {
					next.WinPlaceOnClaim = v
					changed = append(changed, boolLog("winPlaceOnClaim", v))
				}
			case "winHideOnMinimize":
				var v bool
				if json.Unmarshal(val, &v) != nil {
					fail = "winHideOnMinimize 必须是 true/false"
				} else if v != next.WinHideOnMinimize {
					next.WinHideOnMinimize = v
					changed = append(changed, boolLog("winHideOnMinimize", v))
				}
			case "winCloseToTray":
				var v bool
				if json.Unmarshal(val, &v) != nil {
					fail = "winCloseToTray 必须是 true/false"
				} else if v != next.WinCloseToTray {
					next.WinCloseToTray = v
					changed = append(changed, boolLog("winCloseToTray", v))
				}
			case "immersiveMode":
				var v bool
				if json.Unmarshal(val, &v) != nil {
					fail = "immersiveMode 必须是 true/false"
				} else if v != next.ImmersiveMode {
					next.ImmersiveMode = v
					changed = append(changed, boolLog("immersiveMode", v))
				}
			case "desktopFullscreen":
				var v bool
				if json.Unmarshal(val, &v) != nil {
					fail = "desktopFullscreen 必须是 true/false"
				} else if v != next.DesktopFullscreen {
					next.DesktopFullscreen = v
					changed = append(changed, boolLog("desktopFullscreen", v))
				}
			case "dockPosition":
				var v string
				if json.Unmarshal(val, &v) != nil || !dockPositions[v] {
					fail = "dockPosition 须为 bottom / left / right"
				} else if v != next.DockPosition {
					next.DockPosition = v
					changed = append(changed, "dockPosition = "+v)
				}
			case "dockAutoHide":
				var v bool
				if json.Unmarshal(val, &v) != nil {
					fail = "dockAutoHide 必须是 true/false"
				} else if v != next.DockAutoHide {
					next.DockAutoHide = v
					changed = append(changed, boolLog("dockAutoHide", v))
				}
			case "dockIconSize":
				var v int
				if json.Unmarshal(val, &v) != nil || v < 44 || v > 64 {
					fail = "dockIconSize 须在 44–64 之间"
				} else if v != next.DockIconSize {
					next.DockIconSize = v
					changed = append(changed, intLog("dockIconSize", v))
				}
			case "nc_widgets":
				var v string
				if json.Unmarshal(val, &v) != nil {
					fail = "nc_widgets 必须是字符串"
				} else if !ncWidgetsValid(v) {
					fail = "nc_widgets 格式不对（小部件 id 只能是 calendar/server/node，不可重复）"
				} else if canon := sanitizeNCWidgets(v); canon != next.NCWidgets {
					next.NCWidgets = canon
					changed = append(changed, "nc_widgets 已更新")
				}
			case "cc_show_immersive", "cc_show_stage", "cc_show_fullscreen",
				"cc_show_dockhide", "cc_show_remote", "cc_show_wallpaper",
				"notify_allow_sandbox", "notify_allow_update", "notify_allow_servermon",
				"notify_allow_node", "notify_allow_remote", "notify_allow_files",
				"notify_banner_sandbox", "notify_banner_update", "notify_banner_servermon",
				"notify_banner_node", "notify_banner_remote", "notify_banner_files":
				// v0.47 boolean families, resolved through the
				// boolSettingPtr whitelist.
				var v bool
				if json.Unmarshal(val, &v) != nil {
					fail = key + " 必须是 true/false"
				} else if p := boolSettingPtr(&next, key); p == nil {
					fail = "不支持的设置项：" + key
				} else if v != *p {
					*p = v
					changed = append(changed, boolLog(key, v))
				}
			case "exitClosesApps":
				var v bool
				if json.Unmarshal(val, &v) != nil {
					fail = "exitClosesApps 必须是 true/false"
				} else if v != next.ExitClosesApps {
					next.ExitClosesApps = v
					changed = append(changed, boolLog("exitClosesApps", v))
				}
			case "updateBeta":
				var v bool
				if json.Unmarshal(val, &v) != nil {
					fail = "updateBeta 必须是 true/false"
				} else if v != next.UpdateBeta {
					next.UpdateBeta = v
					changed = append(changed, boolLog("updateBeta", v))
				}
			case "remoteEnabled":
				var v bool
				if json.Unmarshal(val, &v) != nil {
					fail = "remoteEnabled 必须是 true/false"
				} else if v != next.RemoteEnabled {
					next.RemoteEnabled = v
					changed = append(changed, boolLog("remoteEnabled", v))
				}
			case "nodeStartMode":
				var v string
				if json.Unmarshal(val, &v) != nil || !nodeStartModes[v] {
					fail = "nodeStartMode 须为 last / rule / global / direct"
				} else if v != next.NodeStartMode {
					next.NodeStartMode = v
					changed = append(changed, "nodeStartMode = "+v)
				}
			case "nodeSubAuto":
				var v bool
				if json.Unmarshal(val, &v) != nil {
					fail = "nodeSubAuto 必须是 true/false"
				} else if v != next.NodeSubAuto {
					next.NodeSubAuto = v
					changed = append(changed, boolLog("nodeSubAuto", v))
				}
			case "nodeSubIntervalMin":
				var v int
				if json.Unmarshal(val, &v) != nil || v < 15 || v > 10080 {
					fail = "nodeSubIntervalMin 须在 15–10080 分钟之间"
				} else if v != next.NodeSubIntervalMin {
					next.NodeSubIntervalMin = v
					changed = append(changed, intLog("nodeSubIntervalMin", v))
				}
			case "nodeTestURL":
				var v string
				if json.Unmarshal(val, &v) != nil || !validTestURL(v) {
					fail = "nodeTestURL 须是 http(s) 地址"
				} else if v != next.NodeTestURL {
					next.NodeTestURL = v
					changed = append(changed, "nodeTestURL = "+v)
				}
			case "nodeTestTimeoutMs":
				var v int
				if json.Unmarshal(val, &v) != nil || v < 1000 || v > 15000 {
					fail = "nodeTestTimeoutMs 须在 1000–15000 之间"
				} else if v != next.NodeTestTimeoutMs {
					next.NodeTestTimeoutMs = v
					changed = append(changed, intLog("nodeTestTimeoutMs", v))
				}
			case "nodeClashAPI":
				var v bool
				if json.Unmarshal(val, &v) != nil {
					fail = "nodeClashAPI 必须是 true/false"
				} else if v != next.NodeClashAPI {
					next.NodeClashAPI = v
					changed = append(changed, boolLog("nodeClashAPI", v))
				}
			case "summonHotkey":
				// v0.52: any valid boss-key combo (hotkey.go's
				// parser is the single rulebook); a rejection
				// carries the parser's named reason verbatim.
				var v string
				if json.Unmarshal(val, &v) != nil {
					fail = "summonHotkey 必须是字符串"
				} else if c, err := canonicalHotkey(v); err != nil {
					fail = "summonHotkey 不可用：" + err.Error()
				} else if c != next.SummonHotkey {
					next.SummonHotkey = c
					changed = append(changed, "summonHotkey = "+c)
				}
			case "defaultOfficeSuite":
				var v string
				if json.Unmarshal(val, &v) != nil || !officeSuites[v] {
					fail = "defaultOfficeSuite 须为 onlyoffice / libreoffice"
				} else if v != next.DefaultOfficeSuite {
					next.DefaultOfficeSuite = v
					changed = append(changed, "defaultOfficeSuite = "+v)
				}
			case "chromeCacheMB":
				var v int
				if json.Unmarshal(val, &v) != nil || !(v == 0 || (v >= 50 && v <= 2000)) {
					fail = "chromeCacheMB 须为 0（不限制）或 50–2000"
				} else if v != next.ChromeCacheMB {
					next.ChromeCacheMB = v
					changed = append(changed, intLog("chromeCacheMB", v))
				}
			case "stageDisplay":
				var v string
				if json.Unmarshal(val, &v) != nil || !stageDisplays[v] {
					fail = "stageDisplay 须为 preview / identity"
				} else if v != next.StageDisplay {
					next.StageDisplay = v
					changed = append(changed, "stageDisplay = "+v)
				}
			case "deskIconSize":
				var v int
				if json.Unmarshal(val, &v) != nil || v < 36 || v > 96 {
					fail = "deskIconSize 须在 36–96 之间"
				} else if v != next.DeskIconSize {
					next.DeskIconSize = v
					changed = append(changed, intLog("deskIconSize", v))
				}
			case "deskSort":
				var v string
				if json.Unmarshal(val, &v) != nil || !deskSorts[v] {
					fail = "deskSort 须为 manual / name / freq / kind / added"
				} else if v != next.DeskSort {
					next.DeskSort = v
					changed = append(changed, "deskSort = "+v)
				}
			default:
				// Unknown keys are ignored: an older desktop talking
				// to a newer daemon (or vice versa) must not fail.
			}
			if fail != "" {
				break
			}
		}
		if fail != "" {
			setState.Unlock()
			logf("settings", "warn", "设置更新被拒绝：%s", fail)
			apiErrBadRequest(w, errCodeInvalid, fail)
			return
		}
		setState.cur = next
		err := settingsSaveLocked()
		setState.Unlock()
		if err != nil {
			logf("settings", "error", "设置保存失败：%v", err)
			writeAPIError(w, http.StatusInternalServerError, errCodeInternal, "保存失败："+err.Error())
			return
		}
		for _, c := range changed {
			logf("settings", "info", "设置已更新：%s", c)
			if strings.HasPrefix(c, "remoteEnabled") {
				// v0.33: start/stop the tailnet side right away
				// instead of waiting for the reconcile tick.
				remoteKick()
			}
			if strings.HasPrefix(c, "immersiveMode") || strings.HasPrefix(c, "desktopFullscreen") {
				// v0.45: immersion flips apply to the whole set
				// right away (the patrol would catch up within a
				// tick anyway; the kick removes the wait).
				immerseKick()
			}
			if strings.HasPrefix(c, "summonHotkey") {
				// v0.50: a new summon key re-registers with the OS
				// right away (unregister old, register new).
				hotkeyKick()
			}
		}
		writeJSON(w, settingsMap())
	default:
		writeAPIError(w, http.StatusMethodNotAllowed, errCodeMethod, "method not allowed")
	}
}

func boolLog(key string, v bool) string {
	if v {
		return key + " = true"
	}
	return key + " = false"
}

func intLog(key string, v int) string {
	return key + " = " + strconv.Itoa(v)
}
