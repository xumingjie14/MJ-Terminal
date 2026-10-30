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

func platformProcessSnapshot() []procFact {
	return nil
}

func platformAllTopWindows() []windowFact {
	return nil
}

func platformClaimWindow(browserPath string, before, claimed map[uint64]bool) (uint64, uint32, bool) {
	return 0, 0, false
}

func platformWindowTitle(hwnd uint64) (string, bool) {
	return "", false
}

func platformWindowRect(hwnd uint64) (int32, int32, int32, int32, bool) {
	return 0, 0, 0, 0, false
}

func platformWindowZoomed(hwnd uint64) bool {
	return false
}

func platformPlaceWindow(hwnd uint64, l, t, r, b int32) bool {
	return false
}

func platformFocusWindow(hwnd uint64) bool {
	return false
}

func platformSetAppID(hwnd uint64) {}

// v0.37 window-etiquette ops (winset.go): no host windows exist off
// Windows, so every fact is "no" and every action is a no-op.

func platformWindowExists(hwnd uint64) bool  { return false }
func platformWindowIconic(hwnd uint64) bool  { return false }
func platformWindowVisible(hwnd uint64) bool { return false }
func platformShowAppWindow(hwnd uint64) bool { return false }
func platformWindowOwned(hwnd uint64) bool   { return false }
func platformForegroundHWND() uint64         { return 0 }
func platformHideWindow(hwnd uint64) bool    { return false }
func platformShowBackWindow(hwnd uint64) bool {
	return false
}
func platformRestoreShellWindow(hwnd uint64)  {}
func platformCloseWindow(hwnd uint64) bool    { return false }
func platformMinimizeWindow(hwnd uint64) bool { return false }
func platformRaiseBackdrop(hwnd uint64, shell uint64) bool {
	return false
}

// v0.54: z-order verification (presentSweep) and the isolated-
// Chrome DWM frame: off Windows there is no z-order to check, so
// "above" reads true (verification falls back to foreground
// alone) and the frame ops are inert.
func platformWindowAbove(hwnd uint64, other uint64) bool { return true }
func platformApplyChromeFrame(hwnd uint64) uint          { return 0 }
func platformRestoreChromeFrame(hwnd uint64)             {}

// v0.45 immersion ops (immerse.go): no host windows off Windows,
// so nothing is ever immersed, marked or fullscreened.

func platformImmerseWindow(hwnd uint64) (uint32, bool) { return 0, false }
func platformUnimmerseWindow(hwnd uint64, orig uint32, hasOrig bool) bool {
	return false
}
func platformWindowHasImmerseMark(hwnd uint64) bool { return false }
func platformEnterShellFullscreen(hwnd uint64) (int32, [4]int32, bool, bool) {
	return 0, [4]int32{}, false, false
}
func platformExitShellFullscreen(hwnd uint64, style int32, rect [4]int32, zoomed bool) bool {
	return false
}

// v0.56: Stage Manager preview capture (thumb.go) -- no host
// windows off Windows, so captures always fail and the strip
// uses identity cards.
const thumbSupported = false

func platformCaptureWindowFrame(hwnd uint64, maxW int) ([]byte, error) {
	return nil, errNoCapture
}

func platformProcessImageBase(pid uint32) string { return "" }
