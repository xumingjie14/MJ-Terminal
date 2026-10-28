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
	"os"
	"path/filepath"
	"strconv"
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
	}
}

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
	b, err := os.ReadFile(settingsPath())
	if err != nil {
		setState.fresh = true
		return
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
	return s
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

// settingsSaveLocked persists atomically (tmp + rename). Caller
// holds the lock.
func settingsSaveLocked() error {
	b, err := json.MarshalIndent(setState.cur, "", "  ")
	if err != nil {
		return err
	}
	tmp := settingsPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, settingsPath()); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	setState.fresh = false
	return nil
}

func settingsMap() map[string]any {
	s := settingsGet()
	return map[string]any{
		"wallpaper":             s.Wallpaper,
		"stageManager":          s.StageManager,
		"idleExitMinutes":       s.IdleExitMinutes,
		"phoneMode":             s.PhoneMode,
		"phoneTriggerWidth":     s.PhoneTriggerWidth,
		"phoneWidth":            s.PhoneWidth,
		"chromeCacheMB":         s.ChromeCacheMB,
		"stageAutoClaimBrowser": s.StageAutoClaimBrowser,
		"filesOnDesktop":        s.FilesOnDesktop,
		"fresh":                 settingsFresh(),
	}
}

// ---------- accessors used across the daemon ----------

func settingIdleExitMinutes() int { return settingsGet().IdleExitMinutes }

func settingPhone() (enabled bool, triggerWidth, width int) {
	s := settingsGet()
	return s.PhoneMode, s.PhoneTriggerWidth, s.PhoneWidth
}

func settingChromeCacheMB() int { return settingsGet().ChromeCacheMB }

func settingStageAutoClaimBrowser() bool { return settingsGet().StageAutoClaimBrowser }

func settingFilesOnDesktop() bool { return settingsGet().FilesOnDesktop }

// ---------- HTTP ----------

func settingsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, settingsMap())
	case http.MethodPost:
		var raw map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			writeJSON(w, map[string]any{"error": "请求内容不对"})
			return
		}
		setState.Lock()
		settingsLoadLocked()
		next := setState.cur
		var changed []string
		fail := ""
		for key, val := range raw {
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
			case "chromeCacheMB":
				var v int
				if json.Unmarshal(val, &v) != nil || !(v == 0 || (v >= 50 && v <= 2000)) {
					fail = "chromeCacheMB 须为 0（不限制）或 50–2000"
				} else if v != next.ChromeCacheMB {
					next.ChromeCacheMB = v
					changed = append(changed, intLog("chromeCacheMB", v))
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
			writeJSON(w, map[string]any{"error": fail})
			return
		}
		setState.cur = next
		err := settingsSaveLocked()
		setState.Unlock()
		if err != nil {
			logf("settings", "error", "设置保存失败：%v", err)
			writeJSON(w, map[string]any{"error": "保存失败：" + err.Error()})
			return
		}
		for _, c := range changed {
			logf("settings", "info", "设置已更新：%s", c)
		}
		writeJSON(w, settingsMap())
	default:
		writeJSON(w, map[string]any{"error": "method not allowed"})
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
