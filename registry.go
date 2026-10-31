package main

// registry.go -- v0.60 architecture block 4: the feature
// registry. The project's three-piece shipping standard (every
// feature integrates the console hub, Settings, and its API
// surface) stops being a convention and becomes structure:
// every feature registers itself here with a stable id, a
// display name, its console module tag(s), its routes on the
// local and remote muxes, and the Settings keys it owns.
//
// The gate tests in registry_test.go enforce the registry
// against reality in both directions:
//
//   - every logf("tag", ...) literal in the codebase names a
//     registered feature's module tag (a new feature logging
//     under an unregistered tag fails the build);
//   - every key in the settings POST whitelist (the case labels
//     of boolSettingPtr + settingsHandler in settings.go) is
//     owned by exactly the features registered here;
//   - the local route set (the mux.HandleFunc table in
//     runServer) and the remote route set (remoteRoutes())
//     match the registered routes exactly -- a route added
//     without a registry entry, or a registry entry naming a
//     route that does not exist, fails the build.
//
// The registry is deliberately a declarative table in package
// main (the mux and the settings whitelist live here too); the
// handlers are NOT dispatched through it. It is the index the
// build checks, not a router.

type featureRoute struct {
	Path  string
	Local bool // registered on the local mux (runServer, path-only)
	// Remote lists the methods under which this path serves on
	// the remote-assist mux (remoteRoutes); empty = not
	// remote-visible. A path may serve under several methods
	// there (e.g. /api/settings is GET-read + POST-assist).
	Remote []string
}

type feature struct {
	ID      string // stable machine id
	Name    string // display name (设置/控制台里的人话名)
	Modules []string
	// console logf module tags this feature owns. Most features
	// own exactly one; a feature whose logging is dynamic
	// (procomp logs under each component's own id) declares its
	// namespace here and says so in a comment.
	Routes []featureRoute
	// Settings keys (settings POST whitelist) this feature
	// owns. The settings HUB (feature "settings") owns the
	// endpoint; the keys belong to the features they configure.
	Settings []string
	// Dynamic settings key families: POST keys formed by
	// prefix + a validated id (per-app notification toggles).
	// The gate asserts each prefix literally appears in
	// settingsHandler's dynamic-key branch.
	SettingPrefixes []string
}

func lr(path string) featureRoute { return featureRoute{Path: path, Local: true} }
func rr(method, path string) featureRoute {
	return featureRoute{Path: path, Remote: []string{method}}
}
func lrr(method, path string) featureRoute {
	return featureRoute{Path: path, Local: true, Remote: []string{method}}
}

var featureRegistry = []feature{
	{
		ID: "core", Name: "系统核心",
		Modules: []string{"system"},
		Routes: []featureRoute{
			lr("/api/shutdown"),
			lrr("GET", "/api/system"),
		},
		Settings: []string{"idleExitMinutes", "exitClosesApps"},
	},
	{
		ID: "diag", Name: "控制台与诊断",
		Modules: []string{"diag", "api"},
		Routes: []featureRoute{
			lr("/api/diag/status"),
			lrr("GET", "/api/diag/logs"),
			lr("/api/diag/export"),
			rr("GET", "/api/remote/diagstatus"),
			rr("GET", "/api/remote/diagexport"),
		},
	},
	{
		ID: "datastore", Name: "数据层",
		Modules: []string{"data"},
		// No routes and no settings of its own: it is the
		// persistence layer every other feature writes through
		// (internal/datastore since v0.60).
	},
	{
		ID: "events", Name: "变更总线",
		Modules: []string{"bus"},
		Routes: []featureRoute{
			lr("/api/view/stream"),
		},
	},
	{
		ID: "desktop", Name: "桌面与 Web 应用",
		Modules: []string{"layout", "icons", "browser"},
		Routes: []featureRoute{
			lr("/"),
			lr("/api/layout"), lr("/api/layout/save"),
			lr("/api/apps"), lr("/api/apps/save"), lr("/api/appopen"),
			lr("/api/browser"), lr("/api/wallpaper"),
			lr("/api/appicon/"), lr("/api/chrome/icon"),
			// legacy workstation action endpoints (command
			// results, not view state -- see apicontract.go)
			lr("/api/list"), lr("/api/run"), lr("/api/open"), lr("/api/openurl"),
		},
		Settings: []string{
			"wallpaper", "filesOnDesktop", "deskIconSize", "deskSort",
			"dockPosition", "dockAutoHide", "dockIconSize",
			"cc_show_wallpaper", "cc_show_dockhide", "nc_widgets",
			"chromeCacheMB",
		},
	},
	{
		ID: "windows", Name: "窗口管理",
		Modules: []string{"winmgr", "thumb"},
		Routes: []featureRoute{
			lrr("GET", "/api/extwin"),
			lr("/api/extwin/focus"), lr("/api/extwin/minimize"),
			lr("/api/extwin/close"), lr("/api/extwin/thumb"),
			lr("/api/wingeom/clear"),
			lr("/api/openshell"), lr("/api/shell/hide"), lr("/api/shell/restored"),
			lr("/api/hotkey/status"),
		},
		Settings: []string{
			"stageManager", "stageDisplay", "stageAutoClaimBrowser",
			"winPlaceOnClaim", "winHideOnMinimize", "winCloseToTray",
			"immersiveMode", "desktopFullscreen",
			"phoneMode", "phoneTriggerWidth", "phoneWidth",
			"summonHotkey",
			"cc_show_immersive", "cc_show_stage", "cc_show_fullscreen",
		},
	},
	{
		ID: "nodes", Name: "节点与代理核心",
		Modules: []string{"node", "core"},
		Routes: []featureRoute{
			lrr("POST", "/api/core"), lr("/api/core/log"),
			lr("/api/proxytest"),
			lrr("GET", "/api/node"),
			featureRoute{Path: "/api/accel", Local: true, Remote: []string{"GET", "POST"}},
			lr("/api/nodepro/status"), lr("/api/nodepro/mode"),
			lr("/api/nodepro/select"), lr("/api/nodepro/test"),
			lr("/api/nodepro/detour"), lr("/api/nodepro/subs"),
			lr("/api/nodepro/rules"), lr("/api/nodepro/dns"),
			lr("/api/nodepro/warp"), lr("/api/nodepro/connections"),
			lr("/api/nodepro/connections/close"),
		},
		Settings: []string{
			"nodeStartMode", "nodeSubAuto", "nodeSubIntervalMin",
			"nodeTestURL", "nodeTestTimeoutMs", "nodeClashAPI",
		},
	},
	{
		ID: "settings", Name: "设置",
		Modules: []string{"settings"},
		Routes: []featureRoute{
			featureRoute{Path: "/api/settings", Local: true, Remote: []string{"GET", "POST"}},
		},
		// The hub owns the endpoint; the KEYS belong to the
		// features they configure (see their entries).
	},
	{
		ID: "vault", Name: "保险库",
		Modules: []string{"vault"},
		Routes: []featureRoute{
			lr("/api/vault/status"), lr("/api/vault/enable"),
			lr("/api/vault/unlock"), lr("/api/vault/lock"),
			lr("/api/vault/changepw"), lr("/api/vault/disable"),
		},
	},
	{
		ID: "notify", Name: "通知中心",
		Modules: []string{"notify"},
		Routes: []featureRoute{
			lr("/api/notify"), lr("/api/notify/dismiss"),
			lr("/api/notify/clear-source"), lr("/api/notify/stream"),
		},
		Settings: []string{
			"notify_allow_sandbox", "notify_allow_update", "notify_allow_servermon",
			"notify_allow_node", "notify_allow_remote", "notify_allow_files",
			"notify_banner_sandbox", "notify_banner_update", "notify_banner_servermon",
			"notify_banner_node", "notify_banner_remote", "notify_banner_files",
		},
		SettingPrefixes: []string{"notify_allow_app_", "notify_banner_app_"},
	},
	{
		ID: "files", Name: "文件",
		Modules: []string{"files"},
		Routes: []featureRoute{
			lr("/api/files"), lr("/api/files/sub"), lr("/api/files/import"),
			lr("/api/files/rename"), lr("/api/files/delete"), lr("/api/files/raw"),
			lr("/api/files/thumb"), lr("/api/files/text"), lr("/api/files/csv"),
			lr("/api/files/openpdf"), lr("/api/files/run"), lr("/api/files/unzip"),
		},
	},
	{
		ID: "store", Name: "应用商店",
		Modules: []string{"apps"},
		Routes: []featureRoute{
			lr("/api/apps/catalog"), lr("/api/apps/install"),
			lr("/api/apps/launch"), lr("/api/apps/uninstall"),
		},
	},
	{
		ID: "sandbox", Name: "隔离安装",
		Modules: []string{"sandbox"},
		Routes: []featureRoute{
			lr("/api/sandbox/status"), lr("/api/sandbox/apps"),
			lr("/api/sandbox/component/install"), lr("/api/sandbox/component/uninstall"),
			lr("/api/sandbox/component/repair"), lr("/api/sandbox/import"),
			lr("/api/sandbox/box/install"), lr("/api/sandbox/box/register"),
			lr("/api/sandbox/box/discard"), lr("/api/sandbox/box/stop"),
			lr("/api/sandbox/launch"), lr("/api/sandbox/remove"), lr("/api/sandbox/clear"),
		},
	},
	{
		ID: "procomp", Name: "生产力组件",
		// Components log under each component's own id
		// (logf(c.ID, ...) in procomp.go) -- the console module
		// for a component event is the component itself. The
		// declared tag is the family's namespace, documented
		// here because the literal-scan gate cannot cover
		// dynamic ids.
		Modules: []string{"components"},
		Routes: []featureRoute{
			lr("/api/components"), lr("/api/components/install"),
			lr("/api/components/uninstall"), lr("/api/components/launch"),
			lr("/api/components/openfile"), lr("/api/components/icon"),
		},
		Settings: []string{"defaultOfficeSuite"},
	},
	{
		ID: "firefox", Name: "Firefox 组件",
		Modules: []string{"firefox"},
		Routes: []featureRoute{
			lrr("GET", "/api/firefox"),
			lr("/api/firefox/icon"), lr("/api/firefox/download"),
			lr("/api/firefox/open"), lr("/api/firefox/restart"),
			lr("/api/firefox/uninstall"),
		},
	},
	{
		ID: "ssh", Name: "SSH 与 SFTP",
		Modules: []string{"ssh"},
		Routes: []featureRoute{
			lr("/api/ssh"), lr("/api/ssh/save"), lr("/api/ssh/keydel"),
			lr("/api/ssh/sftp/list"), lr("/api/ssh/sftp/mkdir"),
			lr("/api/ssh/sftp/delete"), lr("/api/ssh/sftp/upload"),
			lr("/api/ssh/sftp/download"),
			lr("/ws/ssh"),
			// the SSH terminal's xterm assets ride this feature
			lr("/assets/xterm.js"), lr("/assets/xterm.css"), lr("/assets/xterm-fit.js"),
		},
	},
	{
		ID: "servermon", Name: "服务器监控",
		Modules: []string{"servermon"},
		Routes: []featureRoute{
			lrr("GET", "/api/servermon"),
			lr("/api/servermon/watch"), lr("/api/servermon/config"),
			lr("/api/servermon/sample"), lr("/api/servermon/history"),
			lr("/api/servermon/exec"), lr("/api/servermon/agent"),
		},
	},
	{
		ID: "openclaw", Name: "小龙虾 (OpenClaw)",
		Modules: []string{"openclaw"},
		Routes: []featureRoute{
			lrr("GET", "/api/openclaw/status"),
			lr("/api/openclaw/install"),
			lrr("POST", "/api/openclaw/start"),
			lrr("POST", "/api/openclaw/stop"),
			lr("/api/openclaw/uninstall"), lr("/api/openclaw/console"),
			lr("/api/openclaw/prefs"), lr("/api/openclaw/wizard"),
			lr("/api/openclaw/wizard/apply"), lr("/api/openclaw/wizard/test"),
			lr("/api/openclaw/logtail"), lr("/api/openclaw/checkupdate"),
			lr("/api/openclaw/export"), lr("/api/openclaw/import"),
		},
	},
	{
		ID: "remote", Name: "远程协助",
		Modules: []string{"remote"},
		Routes: []featureRoute{
			lrr("GET", "/api/remote/state"),
			lr("/api/remote/pairing"), lr("/api/remote/openlogin"),
			rr("GET", "/api/remote/capabilities"),
		},
		Settings: []string{"remoteEnabled", "cc_show_remote"},
	},
	{
		ID: "storage", Name: "存储空间",
		Modules: []string{"storage"},
		Routes: []featureRoute{
			lrr("GET", "/api/storage/usage"),
			lr("/api/storage/cleanup"),
		},
	},
	{
		ID: "update", Name: "系统更新",
		Modules: []string{"update"},
		Routes: []featureRoute{
			lr("/api/update"), lr("/api/update/token"),
			lrr("POST", "/api/update/check"),
			lrr("POST", "/api/update/download"),
			lrr("POST", "/api/update/apply"),
		},
		Settings: []string{"updateBeta"},
	},
}

// featureByID returns the registered feature with the given
// stable id, or nil.
func featureByID(id string) *feature {
	for i := range featureRegistry {
		if featureRegistry[i].ID == id {
			return &featureRegistry[i]
		}
	}
	return nil
}

// registeredModuleTags is the union of console module tags the
// registry vouches for (the gate test compares the codebase's
// logf literals against it).
func registeredModuleTags() map[string]string {
	out := map[string]string{}
	for _, f := range featureRegistry {
		for _, m := range f.Modules {
			out[m] = f.ID
		}
	}
	return out
}
