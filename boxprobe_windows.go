//go:build windows

package main

// ---------- v0.71 test7: patrol-side close-zone probe (B3) ----------
//
// A self-drawn window's close button is the app's private
// geometry; the only system-level witness is the window
// itself, via WM_NCHITTEST. Asking from inside the low-level
// hook callback is forbidden (review B3: a cross-process
// synchronous SendMessage there can eat the whole
// LowLevelHooksTimeout against a busy target -- precisely the
// window the user most wants to close -- and a timed-out
// callback gets the hook silently removed, test5's death).
// So the PATROL asks, on its own goroutine with no such
// budget: scan the caption band's right end, collect which
// points the window itself calls HTCLOSE, and cache the run
// (boxresident.go owns the cache and its invalidation). The
// callback later does a pure range check against the cache.

import "unsafe"

var procSendMessageTimeoutW = user32.NewProc("SendMessageTimeoutW")

const (
	wmNCHitTestIC   = 0x0084 // WM_NCHITTEST
	htCloseValue    = 20     // HTCLOSE
	smtoAbortIfHung = 0x0002 // SMTO_ABORTIFHUNG
	ncProbeTimeout  = 200    // ms per point; a hung window fails the point, not the patrol
	ncProbeStep     = 4      // px between coarse samples
	ncProbeSpan     = 240    // px of the band's right end worth scanning
)

// platformNCHitTest asks one point of one window what lives
// there. ok=false when the window did not answer in time --
// an unanswering window simply has no probe data this round.
func platformNCHitTest(hwnd uint64, x, y int32) (int32, bool) {
	var res uintptr
	lp := uintptr(uint64(uint32(y))<<32 | uint64(uint32(x)))
	r, _, _ := procSendMessageTimeoutW.Call(uintptr(hwnd), wmNCHitTestIC, 0, lp,
		smtoAbortIfHung, ncProbeTimeout, uintptr(unsafe.Pointer(&res)))
	if r == 0 {
		return 0, false
	}
	return int32(res), true
}

// platformProbeCloseZone probes one self-drawn window's
// caption band for its close button: a coarse scan at
// ncProbeStep across the band's right end at mid-band height,
// the pure run-finder (zoneFromHitRun) to shape the zone,
// then a 1 px edge refinement so the cached zone hugs the
// button the window itself reported. Patrol goroutine only.
func platformProbeCloseZone(hwnd uint64) (closeZone, bool) {
	fl, ft, fr, _, fok := icFrameBounds(hwnd)
	if !fok {
		l, t, r, _, rok := platformWindowRect(hwnd)
		if !rok {
			return closeZone{}, false
		}
		fl, ft, fr = l, t, r
	}
	_, _, frameY, captionH, padded := icCaptionMetrics(icWindowDPI(hwnd))
	zoneH := captionH + frameY + padded
	if zoneH <= 0 || fr <= fl {
		return closeZone{}, false
	}
	bandY := ft + zoneH/2
	minX := fr - ncProbeSpan
	if minX < fl {
		minX = fl
	}
	var samples []hitSample
	for x := minX; x < fr; x += ncProbeStep {
		h, ok := platformNCHitTest(hwnd, x, bandY)
		samples = append(samples, hitSample{X: x, Close: ok && h == htCloseValue})
	}
	z, ok := zoneFromHitRun(samples, ncProbeStep, ft, zoneH)
	if !ok {
		return closeZone{}, false
	}
	// Refine the coarse edges at 1 px (bounded: the coarse
	// zone is never more than a step off).
	for n := 0; n <= ncProbeStep && z.X0 > fl; n++ {
		h, ok := platformNCHitTest(hwnd, z.X0-1, bandY)
		if !ok || h != htCloseValue {
			break
		}
		z.X0--
	}
	for n := 0; n <= ncProbeStep && z.X1 < fr; n++ {
		h, ok := platformNCHitTest(hwnd, z.X1, bandY)
		if !ok || h != htCloseValue {
			break
		}
		z.X1++
	}
	return z, true
}
