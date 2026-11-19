package main

// intercept_test.go -- v0.71 test3, layer 2: the close
// interception rulebook is pinned here, on any OS, because
// the mechanism it drives (the project's first low-level
// hooks) is exactly the Windows execution surface Linux
// cannot prove (the v0.71-test1 lesson). The truth table is
// the review's HS-8 list: non-locked windows, injected input,
// Alt+F4 under a host foreground, the minimize cell, a
// caption drag start, and the drag-away cancel.

import "testing"

func icMouseFacts() interceptFacts {
	// A locked window at (100,100)-(900,700) whose visible
	// frame is inset by the usual 8px resize overhang on the
	// left/right/bottom: frame (108,100)-(892,692). Metrics at
	// 96 DPI (BtnW 45, frames 8, caption 23, padded 4) anchor
	// the close zone on the frame: x in [839,892), y in
	// [100,135). The click point (880,110) is the close
	// button's heart.
	return interceptFacts{
		Kind:           interceptMouseDown,
		TargetHWND:     42,
		TargetLockURL:  "https://web.telegram.org/",
		RectL:          100,
		RectT:          100,
		RectR:          900,
		RectB:          700,
		FrameL:         108,
		FrameT:         100,
		FrameR:         892,
		FrameB:         692,
		FrameOK:        true,
		DpiWindow:      96,
		BtnW:           45,
		FrameXW:        8,
		FrameYW:        8,
		CaptionH:       23,
		PaddedB:        4,
		X:              880,
		Y:              110,
		ServicesActive: true,
		Enabled:        true,
	}
}

func TestDecideCloseInterceptMouse(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*interceptFacts)
		want   interceptAction
		hide   uint64
	}{
		{"DOWN落在关闭区→吞下武装", func(f *interceptFacts) {}, interceptSwallow, 0},
		{"DOWN落在最小化格→放行", func(f *interceptFacts) { f.X = 770 }, interceptPass, 0},
		{"DOWN在标题栏中段（拖动起手）→放行", func(f *interceptFacts) { f.X = 400 }, interceptPass, 0},
		{"DOWN在内容区右上角→放行", func(f *interceptFacts) { f.Y = 200 }, interceptPass, 0},
		{"未锁窗口的关闭区→放行", func(f *interceptFacts) { f.TargetLockURL = "" }, interceptPass, 0},
		{"无目标窗口→放行", func(f *interceptFacts) { f.TargetHWND = 0 }, interceptPass, 0},
		{"注入事件→放行", func(f *interceptFacts) { f.Injected = true }, interceptPass, 0},
		{"保险库锁定（服务未启）→放行", func(f *interceptFacts) { f.ServicesActive = false }, interceptPass, 0},
		{"钩子未生效→放行", func(f *interceptFacts) { f.Enabled = false }, interceptPass, 0},
		{"度量缺失→放行", func(f *interceptFacts) { f.BtnW = 0 }, interceptPass, 0},
		{"UP同窗区内→吞下并藏", func(f *interceptFacts) {
			f.Kind = interceptMouseUp
			f.ArmedHWND = 42
		}, interceptSwallowHide, 42},
		{"UP拖出区外→吞下取消不藏", func(f *interceptFacts) {
			f.Kind = interceptMouseUp
			f.ArmedHWND = 42
			f.X, f.Y = 300, 400
		}, interceptSwallow, 0},
		{"UP落在别的窗口→吞下取消不藏", func(f *interceptFacts) {
			f.Kind = interceptMouseUp
			f.ArmedHWND = 42
			f.TargetHWND = 77
		}, interceptSwallow, 0},
		{"UP无武装→放行", func(f *interceptFacts) { f.Kind = interceptMouseUp }, interceptPass, 0},
		{"UP武装的是已解锁窗口→吞下取消", func(f *interceptFacts) {
			f.Kind = interceptMouseUp
			f.ArmedHWND = 42
			f.TargetLockURL = ""
		}, interceptSwallow, 0},
	}
	for _, c := range cases {
		f := icMouseFacts()
		c.mutate(&f)
		dec := decideCloseIntercept(f)
		if dec.Action != c.want || dec.HideHWND != c.hide {
			t.Errorf("%s: decideCloseIntercept = (%v, hide %d), want (%v, hide %d)",
				c.name, dec.Action, dec.HideHWND, c.want, c.hide)
		}
	}
}

func TestDecideCloseInterceptKeyboard(t *testing.T) {
	base := interceptFacts{
		Kind:              interceptKeyDown,
		AltF4:             true,
		ForegroundHWND:    42,
		ForegroundLockURL: "https://web.telegram.org/",
		ServicesActive:    true,
		Enabled:           true,
	}
	cases := []struct {
		name   string
		mutate func(*interceptFacts)
		want   interceptAction
		hide   uint64
	}{
		{"已锁窗前台 Alt+F4 keydown→吞下并藏", func(f *interceptFacts) {}, interceptSwallowHide, 42},
		{"宿主程序前台 Alt+F4→放行（归宿主自己关）", func(f *interceptFacts) { f.ForegroundLockURL = "" }, interceptPass, 0},
		{"无前台窗→放行", func(f *interceptFacts) { f.ForegroundHWND = 0 }, interceptPass, 0},
		{"注入的 Alt+F4→放行", func(f *interceptFacts) { f.Injected = true }, interceptPass, 0},
		{"不是 Alt+F4（普通 F4）→放行", func(f *interceptFacts) { f.AltF4 = false }, interceptPass, 0},
		{"Alt+F4 的 keyup→永远放行", func(f *interceptFacts) { f.Kind = interceptKeyUp }, interceptPass, 0},
		{"服务未启→放行", func(f *interceptFacts) { f.ServicesActive = false }, interceptPass, 0},
	}
	for _, c := range cases {
		f := base
		c.mutate(&f)
		dec := decideCloseIntercept(f)
		if dec.Action != c.want || dec.HideHWND != c.hide {
			t.Errorf("%s: decideCloseIntercept = (%v, hide %d), want (%v, hide %d)",
				c.name, dec.Action, dec.HideHWND, c.want, c.hide)
		}
	}
}

func TestCloseZoneFor(t *testing.T) {
	zoneOf := func(f interceptFacts) closeZone {
		z, ok := closeZoneFor(f)
		if !ok {
			t.Fatalf("closeZoneFor(%+v) not ok", f)
		}
		return z
	}
	// Negative coordinates (left-hand monitor): the formula is
	// purely relative, no branches.
	f := icMouseFacts()
	f.RectL, f.RectT, f.RectR, f.RectB = -1928, 0, -28, 1040
	f.FrameL, f.FrameT, f.FrameR, f.FrameB = -1920, 0, -36, 1032
	z := zoneOf(f)
	if !z.contains(-40, 10) {
		t.Error("negative-coordinate rect: point in close zone not detected")
	}
	if z.contains(-100, 10) {
		t.Error("negative-coordinate rect: caption middle wrongly in close zone")
	}
	// Maximized: the rect runs past the visible screen edge;
	// the visible frame is the screen itself and anchors.
	f = icMouseFacts()
	f.RectL, f.RectT, f.RectR, f.RectB = -8, -8, 1928, 1048
	f.FrameL, f.FrameT, f.FrameR, f.FrameB = 0, 0, 1920, 1040
	if z := zoneOf(f); !z.contains(1905, 5) {
		t.Error("maximized: X at the screen's top-right corner not detected")
	}
	// No DWM frame answer: the rect anchors and the zone grows
	// by the second frame width, so it still ends at the
	// rect's right edge (the invisible border strip included);
	// WITH a frame, that same strip is outside the zone.
	f = icMouseFacts()
	f.FrameOK = false
	if z := zoneOf(f); !z.contains(895, 110) {
		t.Error("rect-anchored zone should cover the invisible border strip")
	}
	f.FrameOK = true
	if z := zoneOf(f); z.contains(895, 110) {
		t.Error("frame-anchored zone must end at the visible frame edge")
	}
	// Missing metrics never hit.
	f = icMouseFacts()
	f.BtnW = 0
	if _, ok := closeZoneFor(f); ok {
		t.Error("BtnW=0 must yield no zone")
	}
	f = icMouseFacts()
	f.CaptionH, f.FrameYW, f.PaddedB = 0, 0, 0
	if _, ok := closeZoneFor(f); ok {
		t.Error("zero caption height must yield no zone")
	}
}

// TestCloseZoneDPITable pins the geometry across the three
// scaling tiers against a Chrome app window's real button
// placement: the close button's centre sits about 23*scale px
// left of the visible frame's right edge and 14*scale px
// below its top, the button spans the caption band's full
// height, and the minimize cell sits one full button further
// left. Metrics are the system quantities AT THE WINDOW'S
// DPI (what the platform layer now measures); the window rect
// is physical pixels, identical at every tier.
func TestCloseZoneDPITable(t *testing.T) {
	for _, dpi := range []int32{96, 120, 144} {
		s := float64(dpi) / 96
		px := func(v float64) int32 { return int32(v*s + 0.5) }
		f := icMouseFacts()
		f.RectL, f.RectT, f.RectR, f.RectB = 100, 100, 1300, 900
		f.FrameL, f.FrameT = 100+px(8), 100
		f.FrameR, f.FrameB = 1300-px(8), 900-px(8)
		f.DpiWindow = dpi
		f.BtnW, f.FrameXW, f.FrameYW = px(45), px(8), px(8)
		f.CaptionH, f.PaddedB = px(23), px(4)
		z, ok := closeZoneFor(f)
		if !ok {
			t.Fatalf("dpi %d: no zone", dpi)
		}
		xCenter, xTop := f.FrameR-px(23), f.FrameT+px(14)
		if !z.contains(xCenter, xTop) {
			t.Errorf("dpi %d: close centre (%d,%d) outside zone %+v", dpi, xCenter, xTop, z)
		}
		// The button's lower half: its band is ~32*scale tall
		// (centre at +14*scale), so a click at +26*scale is
		// still on the button and must hit.
		if !z.contains(xCenter, f.FrameT+px(26)) {
			t.Errorf("dpi %d: close lower half (%d,%d) outside zone %+v", dpi, xCenter, f.FrameT+px(26), z)
		}
		// Minimize centre: one full button plus a half further
		// left -- never in the zone.
		if z.contains(f.FrameR-px(112), xTop) {
			t.Errorf("dpi %d: minimize centre wrongly in close zone %+v", dpi, z)
		}
		// Content area below the caption band: never in.
		if z.contains(xCenter, z.Y1+px(30)) {
			t.Errorf("dpi %d: content point wrongly in close zone %+v", dpi, z)
		}
		// The full gesture at this tier: DOWN arms, UP hides.
		f.X, f.Y = xCenter, xTop
		if dec := decideCloseIntercept(f); dec.Action != interceptSwallow {
			t.Errorf("dpi %d: DOWN on X = %v, want swallow", dpi, dec.Action)
		}
		f.Kind, f.ArmedHWND = interceptMouseUp, f.TargetHWND
		if dec := decideCloseIntercept(f); dec.Action != interceptSwallowHide || dec.HideHWND != f.TargetHWND {
			t.Errorf("dpi %d: UP on X = (%v, %d), want swallow+hide", dpi, dec.Action, dec.HideHWND)
		}
	}
}

// TestCloseZoneMetricDrift is the test5 field geometry in
// miniature: the window lives at 150% (DPI 144) but the
// metrics were measured at the logon system DPI (96) -- the
// old formula's zone. A click on the close button's lower
// half misses that zone; measured at the window's own DPI
// (the test6 rule) the same click hits.
func TestCloseZoneMetricDrift(t *testing.T) {
	f := icMouseFacts()
	f.RectL, f.RectT, f.RectR, f.RectB = 100, 100, 1300, 900
	f.FrameL, f.FrameT, f.FrameR, f.FrameB = 112, 100, 1288, 888
	clickX, clickY := f.FrameR-34, f.FrameT+39 // 23*1.5 left, 26*1.5 down (the button's lower half)
	// Old way: metrics at 96 while the window is at 144.
	f.DpiWindow = 96
	f.BtnW, f.FrameXW, f.FrameYW, f.CaptionH, f.PaddedB = 45, 8, 8, 23, 4
	if z, _ := closeZoneFor(f); z.contains(clickX, clickY) {
		t.Fatalf("drift case unexpectedly hits with system-DPI metrics; the case no longer discriminates")
	}
	// New way: the same facts with metrics at the window DPI.
	f.DpiWindow = 144
	f.BtnW, f.FrameXW, f.FrameYW, f.CaptionH, f.PaddedB = 68, 12, 12, 34, 6
	z, ok := closeZoneFor(f)
	if !ok || !z.contains(clickX, clickY) {
		t.Errorf("window-DPI metrics: click (%d,%d) outside zone %+v", clickX, clickY, z)
	}
}

// The GOOS gate, pinned like bridgeRouteFor: hooks are a
// Windows-only surface, and an empty lock set means the risk
// surface does not exist at all.
func TestInterceptDesiredFor(t *testing.T) {
	cases := []struct {
		goos     string
		locks    int
		services bool
		want     bool
	}{
		{"windows", 1, true, true},
		{"windows", 3, true, true},
		{"windows", 0, true, false},
		{"windows", 1, false, false},
		{"linux", 1, true, false},
		{"darwin", 2, true, false},
	}
	for _, c := range cases {
		if got := interceptDesiredFor(c.goos, c.locks, c.services); got != c.want {
			t.Errorf("interceptDesiredFor(%q, %d, %v) = %v, want %v", c.goos, c.locks, c.services, got, c.want)
		}
	}
}
