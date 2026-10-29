package main

// ---------- global summon hotkey (v0.50) ----------
//
// One unmodified function key (setting summonHotkey: F8 default,
// F9 / F7 / off) registered with the host OS, so the user can
// recall the terminal from anywhere -- games and other apps
// included, which is exactly why it must be an OS-level hotkey
// and not anything the desktop view could hear. On a press:
//
//   - vault locked  -> summon the lock-screen view (the same
//     recall path the tray icon uses; the view that comes up IS
//     the password screen while sealed);
//   - otherwise     -> recall the whole view set (ditto) AND ask
//     the desktop view to open Spotlight, via the view command
//     channel (viewcmd.go).
//
// The platform half lives in hotkey_windows.go (RegisterHotKey on
// a dedicated message thread, mirroring the tray); other builds
// get the no-op stub in hotkey_other.go and report "unsupported".
// Registration is best-effort by design: if another program owns
// the key, the daemon logs a named warning (module system), keeps
// running without the hotkey, and 设置 → 通用 shows the conflict
// with the F9 / F7 / off alternatives. Unregistration happens on
// re-register and, failing everything, the OS drops a process's
// hotkeys when the process exits -- nothing is left resident.
//
// GET /api/hotkey/status -> {key, registered, supported, note}
// backs the Settings row.

import (
	"net/http"
	"sync"
)

var hotkeyState = struct {
	sync.Mutex
	started    bool
	key        string // the key we currently WANT registered
	registered bool   // the OS confirmed registration
	note       string // human-readable state for the Settings row
}{}

// hotkeyVK maps a setting value to its virtual-key code. The
// Windows half uses the codes; the map itself is platform-neutral
// so the whitelist logic is testable everywhere.
var hotkeyVK = map[string]uint32{
	"F7": 0x76, // VK_F7
	"F8": 0x77, // VK_F8
	"F9": 0x78, // VK_F9
}

// hotkeyStart begins the platform registration for the configured
// key. Called once from runServer (in locked boot too: F8 is how
// a sealed terminal gets its password screen back). Idempotent.
func hotkeyStart() {
	hotkeyState.Lock()
	if hotkeyState.started {
		hotkeyState.Unlock()
		return
	}
	hotkeyState.started = true
	hotkeyState.key = settingSummonHotkey()
	hotkeyState.Unlock()
	hotkeyPlatformApply()
}

// hotkeyKick re-applies the configured key after a settings
// change: unregister the old, register the new (or stay
// unregistered for "off").
func hotkeyKick() {
	hotkeyState.Lock()
	hotkeyState.started = true
	hotkeyState.key = settingSummonHotkey()
	hotkeyState.Unlock()
	hotkeyPlatformApply()
}

// hotkeySetResult records the platform layer's registration
// outcome and logs transitions (module system, per the console
// rule: the hotkey's lifecycle is a system facility).
func hotkeySetResult(registered bool, note string) {
	hotkeyState.Lock()
	changed := hotkeyState.registered != registered || hotkeyState.note != note
	hotkeyState.registered = registered
	hotkeyState.note = note
	key := hotkeyState.key
	hotkeyState.Unlock()
	if !changed {
		return
	}
	if registered {
		logf("system", "info", "全局召唤热键已注册：%s（任意界面按下即唤回终端机）", key)
	} else if key == "off" {
		logf("system", "info", "全局召唤热键已关闭")
	} else if note != "" {
		logf("system", "warn", "全局召唤热键 %s 未能注册：%s", key, note)
	}
}

// hotkeySummon is the press action, run on its own goroutine by
// the platform layer (it performs an HTTP self-call and must
// never block a message pump).
func hotkeySummon() {
	if vaultLocked() {
		// Sealed: the recall path brings up the shell view, which
		// is the password screen while locked -- nothing else may
		// be summoned, and Spotlight stays shut.
		logf("system", "info", "召唤热键：保险库锁定中，唤出锁定屏")
		onTrayIconRestore()
		return
	}
	logf("system", "info", "召唤热键：唤回终端机并打开聚焦搜索")
	onTrayIconRestore()
	viewCmdBroadcast("spotlight")
}

// hotkeyStatusHandler reports the registration state for
// 设置 → 通用. Read-only; the key itself changes through the
// settings POST like every other preference.
func hotkeyStatusHandler(w http.ResponseWriter, r *http.Request) {
	hotkeyState.Lock()
	st := map[string]any{
		"key":        hotkeyState.key,
		"registered": hotkeyState.registered,
		"supported":  hotkeyPlatformSupported,
		"note":       hotkeyState.note,
	}
	hotkeyState.Unlock()
	if st["key"] == "" {
		st["key"] = settingSummonHotkey()
	}
	writeJSON(w, st)
}
