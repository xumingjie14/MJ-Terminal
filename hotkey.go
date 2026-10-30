package main

// ---------- global hotkey: the boss key (v0.50, redefined v0.52) ----------
//
// One key (setting summonHotkey, default "F8") registered with
// the host OS, so it reaches the daemon from anywhere -- games
// and other apps included, which is exactly why it must be an
// OS-level hotkey and not anything the desktop view could hear.
// Since v0.52 the press is a BOSS KEY (bosskey.go): our window
// foreground -> hide the whole set; anything else -> summon it
// back immediately; vault locked -> the lock screen only.
//
// The setting value is a combo string: "F8", "Ctrl+F8",
// "Alt+Q", "Ctrl+Shift+F9", or "off". The v0.50 values
// (F7 / F8 / F9 / off) are just combos with no modifiers and
// keep working unchanged -- sanitizeSettings canonicalizes
// every stored value through parseHotkeyCombo, so an old
// settings file migrates itself on load.
//
// The platform half lives in hotkey_windows.go (RegisterHotKey
// with the combo's modifiers + MOD_NOREPEAT, on a dedicated
// message thread mirroring the tray); other builds get the
// no-op stub in hotkey_other.go and report "unsupported".
// Registration is best-effort by design: if another program
// owns the combo, the daemon logs a named warning (module
// system), keeps running without the hotkey, does NOT roll the
// user's setting back and does NOT pretend the key is active --
// 设置 → 通用 shows 「被占用」 from GET /api/hotkey/status, which
// reports the combo plus a three-state status (registered /
// busy / unsupported, plus off / error). Unregistration happens
// on re-register and, failing everything, the OS drops a
// process's hotkeys when the process exits -- nothing is left
// resident.

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// ---------- combo model (pure) ----------

// Modifier bits, matching RegisterHotKey's fsModifiers values.
// MOD_NOREPEAT is NOT part of a combo; the platform layer adds
// it at registration time.
const (
	hotkeyModAlt     = 0x0001 // MOD_ALT
	hotkeyModControl = 0x0002 // MOD_CONTROL
	hotkeyModShift   = 0x0004 // MOD_SHIFT
)

// hotkeyCombo is a parsed summonHotkey value: one primary key
// (a virtual-key code) plus any Ctrl/Alt/Shift modifiers, or
// Off.
type hotkeyCombo struct {
	Off  bool
	Mods uint32 // hotkeyMod* bits
	VK   uint32 // virtual-key code of the primary key
}

// String renders the canonical setting form: "off", "F8",
// "Ctrl+F8", "Alt+Q", "Ctrl+Shift+F9" -- modifiers always in
// Ctrl / Alt / Shift order, key uppercased.
func (c hotkeyCombo) String() string {
	if c.Off {
		return "off"
	}
	parts := make([]string, 0, 4)
	if c.Mods&hotkeyModControl != 0 {
		parts = append(parts, "Ctrl")
	}
	if c.Mods&hotkeyModAlt != 0 {
		parts = append(parts, "Alt")
	}
	if c.Mods&hotkeyModShift != 0 {
		parts = append(parts, "Shift")
	}
	return strings.Join(append(parts, hotkeyKeyName(c.VK)), "+")
}

// hotkeyKeyName names a primary-key VK: F1-F12, A-Z, 0-9.
func hotkeyKeyName(vk uint32) string {
	switch {
	case vk >= 0x70 && vk <= 0x7B: // VK_F1..VK_F12
		return "F" + strconv.Itoa(int(vk-0x70)+1)
	case vk >= 0x41 && vk <= 0x5A, vk >= 0x30 && vk <= 0x39:
		return string(rune(vk))
	}
	return ""
}

// hotkeyKeyVK parses one primary-key token (case-insensitive):
// F1-F12, a letter, or a digit.
func hotkeyKeyVK(tok string) (uint32, error) {
	u := strings.ToUpper(strings.TrimSpace(tok))
	if strings.HasPrefix(u, "F") {
		if n, err := strconv.Atoi(u[1:]); err == nil && n >= 1 && n <= 12 && len(u) <= 3 {
			return 0x70 + uint32(n) - 1, nil // VK_F1 = 0x70
		}
		return 0, fmt.Errorf("功能键只支持 F1–F12（收到 %q）", tok)
	}
	if len(u) == 1 {
		if r := rune(u[0]); (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return uint32(r), nil
		}
	}
	return 0, fmt.Errorf("不支持的按键 %q（可用 F1–F12、字母、数字）", tok)
}

// parseHotkeyCombo validates a summonHotkey value and returns
// its combo. The rules are the v0.52 contract, and every
// rejection names its reason -- the settings POST surfaces it
// verbatim:
//
//   - "off" (any case) turns the hotkey off;
//   - a single F1-F12 needs no modifier;
//   - Ctrl / Alt / Shift may combine with a letter, digit or
//     F-key;
//   - a bare letter or digit is REJECTED (it would eat normal
//     typing system-wide);
//   - any Win-key combination is REJECTED (OS-reserved, and
//     the desktop view cannot even record it);
//   - empty values, modifier-only combos and two primary keys
//     are REJECTED.
func parseHotkeyCombo(s string) (hotkeyCombo, error) {
	t := strings.TrimSpace(s)
	if t == "" {
		return hotkeyCombo{}, errors.New("组合为空，请选一个按键或组合")
	}
	if strings.EqualFold(t, "off") {
		return hotkeyCombo{Off: true}, nil
	}
	var c hotkeyCombo
	keySeen := false
	for _, p := range strings.Split(t, "+") {
		p = strings.TrimSpace(p)
		if p == "" {
			return hotkeyCombo{}, fmt.Errorf("组合 %q 里有空的键位", s)
		}
		switch strings.ToLower(p) {
		case "ctrl", "control":
			c.Mods |= hotkeyModControl
		case "alt":
			c.Mods |= hotkeyModAlt
		case "shift":
			c.Mods |= hotkeyModShift
		case "win", "windows", "meta", "super", "cmd", "command":
			return hotkeyCombo{}, errors.New("不能用 Win 键组合（系统保留，视图也录不到）")
		default:
			if keySeen {
				return hotkeyCombo{}, fmt.Errorf("组合 %q 里有多个主键，只能留一个", s)
			}
			vk, err := hotkeyKeyVK(p)
			if err != nil {
				return hotkeyCombo{}, err
			}
			c.VK = vk
			keySeen = true
		}
	}
	if !keySeen {
		return hotkeyCombo{}, errors.New("只有 Ctrl/Alt/Shift 不够，要再加一个主键")
	}
	if c.Mods == 0 && (c.VK < 0x70 || c.VK > 0x7B) {
		return hotkeyCombo{}, errors.New("裸字母/数字键会吃掉正常打字，请加 Ctrl、Alt 或 Shift 一起用（单个 F1–F12 可以）")
	}
	return c, nil
}

// canonicalHotkey returns the canonical setting string for a
// value, or the validation error. It is the single gate used by
// sanitizeSettings (migration + fallback) and by the settings
// POST (named rejection).
func canonicalHotkey(s string) (string, error) {
	c, err := parseHotkeyCombo(s)
	if err != nil {
		return "", err
	}
	return c.String(), nil
}

// ---------- registration state ----------

var hotkeyState = struct {
	sync.Mutex
	started    bool
	key        string // canonical combo we currently WANT registered
	registered bool   // the OS confirmed registration
	note       string // human-readable state for the Settings row
}{}

// hotkeyConflict is set by the platform layer when registration
// failed because another program owns the combo (Windows error
// 1409). It is what makes /api/hotkey/status say 「被占用」
// instead of a generic failure -- the setting itself is left
// exactly as the user chose it.
var hotkeyConflict atomic.Bool

// hotkeyStart begins the platform registration for the
// configured combo. Called once from runServer (in locked boot
// too: the boss key is how a sealed terminal gets its password
// screen back). Idempotent.
func hotkeyStart() {
	hotkeyState.Lock()
	if hotkeyState.started {
		hotkeyState.Unlock()
		return
	}
	hotkeyState.started = true
	hotkeyState.Unlock()
	hotkeyApplySetting()
}

// hotkeyKick re-applies the configured combo after a settings
// change: unregister the old, register the new (or stay
// unregistered for "off").
func hotkeyKick() {
	hotkeyState.Lock()
	hotkeyState.started = true
	hotkeyState.Unlock()
	hotkeyApplySetting()
}

// hotkeyApplySetting resolves the configured value to a combo
// and hands it to the platform layer. sanitizeSettings keeps
// the stored value canonical, so the parse cannot fail in
// practice; the fallback is loud, never silent.
func hotkeyApplySetting() {
	key := settingSummonHotkey()
	combo, err := parseHotkeyCombo(key)
	if err != nil {
		logf("system", "warn", "老板键设置值不可用（%q：%v），回退 F8", key, err)
		combo, _ = parseHotkeyCombo("F8")
		key = "F8"
	}
	hotkeyState.Lock()
	hotkeyState.key = key
	hotkeyState.Unlock()
	hotkeyPlatformApply(combo)
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
		logf("system", "info", "老板键已注册：%s（终端机在前台按＝整套隐藏，在别处按＝立刻召唤）", key)
	} else if key == "off" {
		logf("system", "info", "老板键已关闭")
	} else if note != "" {
		logf("system", "warn", "老板键 %s 未能注册：%s", key, note)
	}
}

// hotkeyStatusHandler reports the registration state for
// 设置 → 通用. Read-only; the key itself changes through the
// settings POST like every other preference. The status field
// is the three-state truth the Settings row renders:
// registered / busy / unsupported (plus off / error).
func hotkeyStatusHandler(w http.ResponseWriter, r *http.Request) {
	hotkeyState.Lock()
	key, registered, note := hotkeyState.key, hotkeyState.registered, hotkeyState.note
	hotkeyState.Unlock()
	if key == "" {
		key = settingSummonHotkey()
	}
	status := "error"
	switch {
	case !hotkeyPlatformSupported:
		status = "unsupported"
	case key == "off":
		status = "off"
	case registered:
		status = "registered"
	case hotkeyConflict.Load():
		status = "busy"
	}
	writeJSON(w, map[string]any{
		"key":        key,
		"registered": registered,
		"supported":  hotkeyPlatformSupported,
		"note":       note,
		"status":     status,
	})
}
