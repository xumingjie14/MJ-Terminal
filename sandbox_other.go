//go:build !windows

package main

// Non-Windows stubs for the app-isolation platform layer: the
// Sandboxie component is Windows-only, so detection reports
// nothing and every privileged operation refuses honestly. The
// Linux build exists for API testing; the settings page shows
// 「当前平台不支持」 there (sandboxStatusMap's supported flag).

import "errors"

var errSbieUnsupported = errors.New("应用隔离只支持 Windows")

// errSbieUACCanceled mirrors the Windows sentinel so shared flow
// code can errors.Is against it on every platform.
var errSbieUACCanceled = errors.New("管理员确认被取消了")

func sbiePlatformStatus() (string, string, string) { return "", "", "" }

func sbieRunElevated(exe string, args ...string) (uint32, error) {
	return 0, errSbieUnsupported
}

func sbieProcImagePath(pid uint32) string { return "" }

func sbieKillPid(pid int) error { return errSbieUnsupported }
