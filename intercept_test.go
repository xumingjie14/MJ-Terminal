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
	// A locked window at (100,100)-(900,700); close zone with
	// closeW=46, captionH=32: x in [854,900), y in [100,132).
	return interceptFacts{
		Kind:           interceptMouseDown,
		TargetHWND:     42,
		TargetLockURL:  "https://web.telegram.org/",
		RectL:          100,
		RectT:          100,
		RectR:          900,
		RectB:          700,
		CloseW:         46,
		CaptionH:       32,
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
		{"度量缺失→放行", func(f *interceptFacts) { f.CloseW = 0 }, interceptPass, 0},
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

func TestInterceptInCloseZone(t *testing.T) {
	// Window rect (-1920,0)-(-20,1040) on a left-hand monitor:
	// negative coordinates must work relatively, no branches.
	if !interceptInCloseZone(-40, 10, -1920, 0, -20, 1040, 46, 32) {
		t.Error("negative-coordinate rect: point in close zone not detected")
	}
	if interceptInCloseZone(-100, 10, -1920, 0, -20, 1040, 46, 32) {
		t.Error("negative-coordinate rect: caption middle wrongly in close zone")
	}
	// Maximized: the rect runs past the visible screen edge;
	// the formula uses the rect as-is.
	if !interceptInCloseZone(1905, 5, -8, -8, 1928, 1048, 46, 32) {
		t.Error("maximized rect: X at the screen's top-right corner not detected")
	}
	if interceptInCloseZone(880, 110, 100, 100, 900, 700, 0, 32) {
		t.Error("closeW=0 must never hit")
	}
	if interceptInCloseZone(880, 110, 100, 100, 900, 700, 46, 0) {
		t.Error("captionH=0 must never hit")
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
