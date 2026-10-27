//go:build !windows

package main

// No-op stubs for the unified-window-management hooks (winmgr.go):
// outside Windows there are no host windows to register, focus or
// stamp, so the registry simply stays empty and the frontend hides
// the external-windows group. This keeps the Linux build (used for
// API testing) compiling and behaving exactly as before.

func platformSnapshotWindows(browserPath string) map[uint64]bool {
	return map[uint64]bool{}
}

func platformClaimWindow(browserPath string, before, claimed map[uint64]bool) (uint64, uint32, bool) {
	return 0, 0, false
}

func platformWindowTitle(hwnd uint64) (string, bool) {
	return "", false
}

func platformFocusWindow(hwnd uint64) bool {
	return false
}

func platformSetAppID(hwnd uint64) {}
