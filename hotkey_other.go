//go:build !windows

package main

// ---------- v0.50: global summon hotkey, non-Windows stub ----------
//
// The summon hotkey is a Windows facility (RegisterHotKey); the
// product ships for Windows only. Other builds (dev/test on
// Linux) compile this stub: the setting exists, the status
// endpoint answers honestly, and nothing is registered.

const hotkeyPlatformSupported = false

func hotkeyPlatformApply() {
	if settingSummonHotkey() == "off" {
		hotkeySetResult(false, "")
		return
	}
	hotkeySetResult(false, "当前平台不支持全局热键")
}
