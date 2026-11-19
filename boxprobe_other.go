//go:build !windows

package main

// ---------- v0.71 test7: close-zone probe (non-Windows) ----------
//
// No host windows exist off Windows, so there is nothing to
// probe: the native resident engine is inert there and this
// stub only keeps the package building.

func platformProbeCloseZone(hwnd uint64) (closeZone, bool) { return closeZone{}, false }
func platformNCHitTest(hwnd uint64, x, y int32) (int32, bool) {
	return 0, false
}
