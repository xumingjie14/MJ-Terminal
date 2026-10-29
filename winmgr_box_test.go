package main

// Tests for the v0.40 explicit PID-set window claiming (boxed
// apps): PID difference selection, window picking, the
// second-launch (empty difference) route, registration, and the
// patrol's box-list warning throttle. Platform-neutral: the
// platform layer is stubbed out off Windows, so these pin the
// pure decision logic the real launch path is built from.

import (
	"testing"
	"time"
)

func TestDiffPIDs(t *testing.T) {
	before := map[uint32]bool{10: true, 20: true}
	after := map[uint32]bool{20: true, 30: true, 40: true}
	diff := diffPIDs(before, after)
	if len(diff) != 2 || !diff[30] || !diff[40] {
		t.Fatalf("diff = %v, want {30,40}", sortedPIDList(diff))
	}
	if d := diffPIDs(before, before); len(d) != 0 {
		t.Fatalf("identical sets must diff empty, got %v", sortedPIDList(d))
	}
	if d := diffPIDs(nil, nil); len(d) != 0 {
		t.Fatalf("nil sets must diff empty, got %v", sortedPIDList(d))
	}
	if !pidSetsOverlap(before, after) || pidSetsOverlap(before, map[uint32]bool{99: true}) {
		t.Fatal("pidSetsOverlap wrong")
	}
	if got := sortedPIDList(map[uint32]bool{40: true, 10: true, 30: true}); len(got) != 3 || got[0] != 10 || got[2] != 40 {
		t.Fatalf("sortedPIDList = %v", got)
	}
	if s := pidSetFromInts([]int{5, 5, 0, -3, 7}); len(s) != 2 || !s[5] || !s[7] {
		t.Fatalf("pidSetFromInts = %v", sortedPIDList(s))
	}
}

func TestPickWindowForPIDs(t *testing.T) {
	wins := []windowFact{
		{HWND: 101, PID: 300, Class: "Qt6QWindow", Title: "微信", Visible: true, W: 900, H: 700},
		{HWND: 102, PID: 301, Class: "Qt6QWindow", Title: "helper", Visible: true, W: 300, H: 200},
		{HWND: 103, PID: 302, Class: "Qt6QWindow", Title: "宿主微信", Visible: true, W: 900, H: 700},
		{HWND: 104, PID: 300, Class: "#32770", Title: "dialog", Visible: true, Owned: true, W: 400, H: 300},
		{HWND: 105, PID: 300, Class: "Qt6QWindow", Title: "hidden", Visible: false, W: 900, H: 700},
	}
	images := map[uint32]string{
		300: `C:\Program Files\Tencent\Weixin\Weixin.exe`,
		301: `C:\Program Files\Tencent\Weixin\WeChatAppEx.exe`,
		302: `C:\Program Files\Tencent\Weixin\Weixin.exe`, // host twin: same image, different PID
	}
	// The box PID set names 300+301 only: the host twin (302) must
	// never be picked even though its image matches exactly.
	got, ok := pickWindowForPIDs(wins, map[uint32]bool{300: true, 301: true}, nil, images, "weixin.exe")
	if !ok || got.HWND != 101 {
		t.Fatalf("pick = %+v (%v), want hwnd 101", got, ok)
	}
	// Image preference: with only the helper's window qualifying
	// by size it still wins when it is the image match.
	got2, ok2 := pickWindowForPIDs(wins, map[uint32]bool{301: true}, nil, images, "wechatappex.exe")
	if !ok2 || got2.HWND != 102 {
		t.Fatalf("helper pick = %+v (%v), want hwnd 102", got2, ok2)
	}
	// No image match anywhere: fall back to the PID set alone
	// (largest candidate) rather than claiming nothing.
	got3, ok3 := pickWindowForPIDs(wins, map[uint32]bool{300: true, 301: true}, nil, images, "other.exe")
	if !ok3 || got3.HWND != 101 {
		t.Fatalf("fallback pick = %+v (%v), want hwnd 101", got3, ok3)
	}
	// Excluded windows (pre-launch picture / already registered)
	// are skipped.
	if _, ok4 := pickWindowForPIDs(wins, map[uint32]bool{300: true}, map[uint64]bool{101: true}, images, "weixin.exe"); ok4 {
		t.Fatal("excluded window must not be picked")
	}
	// Empty PID set never picks anything.
	if _, ok5 := pickWindowForPIDs(wins, nil, nil, images, "weixin.exe"); ok5 {
		t.Fatal("empty PID set must not pick")
	}
	// A PID outside the set (the host twin) is not picked even
	// when it is the only window offered.
	if _, ok6 := pickWindowForPIDs(wins[2:3], map[uint32]bool{300: true}, nil, images, "weixin.exe"); ok6 {
		t.Fatal("host twin window must not be picked for a box PID set")
	}
}

func TestWindowsOwnedByPIDsCrossCheck(t *testing.T) {
	wins := []windowFact{
		{HWND: 201, PID: 300, Title: "微信", Class: "Qt6QWindow", Visible: true},
		{HWND: 202, PID: 999, Title: "宿主应用", Class: "Other", Visible: true},
		{HWND: 203, PID: 301, Title: "", Class: "Qt6Tray", Visible: false},
	}
	cross := windowsOwnedByPIDs(wins, map[uint32]bool{300: true, 301: true})
	if len(cross) != 2 || cross[0].HWND != 201 || cross[1].HWND != 203 {
		t.Fatalf("cross = %+v, want hwnds 201,203 (any shape, incl. invisible)", cross)
	}
	if none := windowsOwnedByPIDs(wins, map[uint32]bool{555: true}); len(none) != 0 {
		t.Fatalf("cross for unrelated set = %+v, want none", none)
	}
	if none := windowsOwnedByPIDs(wins, nil); len(none) != 0 {
		t.Fatalf("cross for empty set = %+v, want none", none)
	}
}

// TestBoxSecondLaunchEmptyDiff covers the second-launch
// route's decision pieces: when the app already runs in the box,
// the post-launch PID difference is empty (not a failure), and
// the existing PID set must locate the already-registered window
// to present.
func TestBoxSecondLaunchEmptyDiff(t *testing.T) {
	before := pidSetFromInts([]int{300, 301})
	after := pidSetFromInts([]int{300, 301}) // box list unchanged by the second launch
	if diff := diffPIDs(before, after); len(diff) != 0 {
		t.Fatalf("second launch diff = %v, want empty", sortedPIDList(diff))
	}
	reg := map[uint64]*extWindow{
		401: {HWND: 401, App: "Weixin", pid: 300, origin: ownBox},
		402: {HWND: 402, App: "别的盒内应用", pid: 999, origin: ownBox},
		403: {HWND: 403, App: "shell", pid: 300, Shell: true},
	}
	hwnd, ok := pickRegisteredForPIDs(reg, "Weixin", before)
	if !ok || hwnd != 401 {
		t.Fatalf("pickRegistered = %d (%v), want 401", hwnd, ok)
	}
	// A patrol-claimed boxed window is labelled by title, not by
	// the registry name: the box origin still matches it.
	reg2 := map[uint64]*extWindow{
		411: {HWND: 411, App: "微信", pid: 301, origin: ownBox},
	}
	if hwnd, ok := pickRegisteredForPIDs(reg2, "Weixin", before); !ok || hwnd != 411 {
		t.Fatalf("pickRegistered (patrol-labelled) = %d (%v), want 411", hwnd, ok)
	}
	// A registered window of a DIFFERENT app whose PID is not in
	// the set never matches.
	reg3 := map[uint64]*extWindow{
		421: {HWND: 421, App: "别的", pid: 999, origin: "launch"},
	}
	if _, ok := pickRegisteredForPIDs(reg3, "Weixin", before); ok {
		t.Fatal("unrelated registered window must not match the existing PID set")
	}
}

// TestRegisterPIDClaimedWindow exercises the registration core
// that claimWindowByPIDs (the external-PID-source variant of
// trackLaunch) is built on, including the duplicate guard.
func TestRegisterPIDClaimedWindow(t *testing.T) {
	extMu.Lock()
	saved := extWins
	extWins = map[uint64]*extWindow{}
	extMu.Unlock()
	defer func() {
		extMu.Lock()
		extWins = saved
		extMu.Unlock()
	}()
	wf := windowFact{HWND: 501, PID: 300, Title: "微信", Class: "Qt6QWindow", Visible: true, W: 900, H: 700}
	if !registerPIDClaimedWindow("Weixin", wf, ownBox) {
		t.Fatal("first registration must succeed")
	}
	extMu.Lock()
	e := extWins[501]
	extMu.Unlock()
	if e == nil || e.App != "Weixin" || e.pid != 300 || e.origin != ownBox {
		t.Fatalf("registered entry = %+v", e)
	}
	if registerPIDClaimedWindow("Weixin", wf, ownBox) {
		t.Fatal("duplicate registration must be refused")
	}
	// claimWindowByPIDs with an empty set is a clean no-claim,
	// and off Windows (no window inventory) it claims nothing.
	if hwnd, ok := claimWindowByPIDs("Weixin", nil, nil, "weixin.exe", ownBox); ok || hwnd != 0 {
		t.Fatalf("empty PID set claim = %d (%v)", hwnd, ok)
	}
	if hwnd, ok := claimWindowByPIDs("Weixin", map[uint32]bool{300: true}, nil, "weixin.exe", ownBox); ok || hwnd != 0 {
		t.Fatalf("no-inventory claim = %d (%v)", hwnd, ok)
	}
}

func TestDecideBoxWarnThrottle(t *testing.T) {
	now := time.Now()
	if !decideBoxWarn(time.Time{}, false, now) {
		t.Fatal("first failure must log")
	}
	if decideBoxWarn(now, true, now.Add(30*time.Second)) {
		t.Fatal("repeat within a minute must be silent")
	}
	if !decideBoxWarn(now, true, now.Add(61*time.Second)) {
		t.Fatal("repeat after a minute must log again")
	}
}
