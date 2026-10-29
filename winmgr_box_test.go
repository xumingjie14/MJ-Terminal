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
	got, ok := pickWindowForPIDs(wins, map[uint32]bool{300: true, 301: true}, nil, nil, images, "weixin.exe")
	if !ok || got.HWND != 101 {
		t.Fatalf("pick = %+v (%v), want hwnd 101", got, ok)
	}
	// Image preference: with only the helper's window qualifying
	// by size it still wins when it is the image match.
	got2, ok2 := pickWindowForPIDs(wins, map[uint32]bool{301: true}, nil, nil, images, "wechatappex.exe")
	if !ok2 || got2.HWND != 102 {
		t.Fatalf("helper pick = %+v (%v), want hwnd 102", got2, ok2)
	}
	// No image match anywhere: fall back to the PID set alone
	// (largest candidate) rather than claiming nothing.
	got3, ok3 := pickWindowForPIDs(wins, map[uint32]bool{300: true, 301: true}, nil, nil, images, "other.exe")
	if !ok3 || got3.HWND != 101 {
		t.Fatalf("fallback pick = %+v (%v), want hwnd 101", got3, ok3)
	}
	// Excluded windows (pre-launch picture / already registered)
	// are skipped. v0.41: with the visible main window excluded,
	// the pick falls to the same process's HIDDEN app window
	// (hwnd 105) -- that fall is the whole point of the release;
	// excluding both leaves nothing.
	got4, ok4 := pickWindowForPIDs(wins, map[uint32]bool{300: true}, nil, map[uint64]bool{101: true}, images, "weixin.exe")
	if !ok4 || got4.HWND != 105 {
		t.Fatalf("excluded-visible pick = %+v (%v), want the hidden hwnd 105", got4, ok4)
	}
	if _, ok4b := pickWindowForPIDs(wins, map[uint32]bool{300: true}, nil, map[uint64]bool{101: true, 105: true}, images, "weixin.exe"); ok4b {
		t.Fatal("all candidates excluded must not pick")
	}
	// Empty PID set never picks anything.
	if _, ok5 := pickWindowForPIDs(wins, nil, nil, nil, images, "weixin.exe"); ok5 {
		t.Fatal("empty PID set must not pick")
	}
	// A PID outside the set (the host twin) is not picked even
	// when it is the only window offered.
	if _, ok6 := pickWindowForPIDs(wins[2:3], map[uint32]bool{300: true}, nil, nil, images, "weixin.exe"); ok6 {
		t.Fatal("host twin window must not be picked for a box PID set")
	}
	// The prefer set (this launch's PID difference) outranks an
	// image match on an older process.
	got7, ok7 := pickWindowForPIDs(wins, map[uint32]bool{300: true, 301: true}, map[uint32]bool{301: true}, nil, images, "weixin.exe")
	if !ok7 || got7.HWND != 102 {
		t.Fatalf("prefer pick = %+v (%v), want hwnd 102 (the launch's own new PID)", got7, ok7)
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
	if hwnd, ok := claimWindowByPIDs("Weixin", nil, nil, nil, "weixin.exe", ownBox); ok || hwnd != 0 {
		t.Fatalf("empty PID set claim = %d (%v)", hwnd, ok)
	}
	if hwnd, ok := claimWindowByPIDs("Weixin", map[uint32]bool{300: true}, nil, nil, "weixin.exe", ownBox); ok || hwnd != 0 {
		t.Fatalf("no-inventory claim = %d (%v)", hwnd, ok)
	}
}

// fieldBoxWindows46 is the v0.40 real-machine cross-check,
// transcribed verbatim from the user's console log (2026-10-29
// 12:38): at the launch timeout, EnumWindows found these 46
// top-level windows under the box's 18 PIDs -- EVERY one of them
// invisible. Six are WeChat's Qt main windows (the app, born
// hidden); the other forty are the standard invisible plumbing.
// The log carried no sizes, so the app windows are given a
// uniform plausible size here (ties then break on HWND, as the
// picker promises) and the plumbing zero size.
func fieldBoxWindows46() []windowFact {
	qt := "Sandbox:MJBox:Qt51514QWindowIcon"
	app := func(hwnd uint64, pid uint32) windowFact {
		return windowFact{HWND: hwnd, PID: pid, Title: "WeChat", Class: qt, Visible: false, W: 1046, H: 820}
	}
	junk := func(hwnd uint64, pid uint32, title, class string) windowFact {
		return windowFact{HWND: hwnd, PID: pid, Title: title, Class: class, Visible: false}
	}
	return []windowFact{
		app(67616, 15320),
		junk(67622, 15320, "", "Sandbox:MJBox:DisplayICC_SystemMessageWindow"),
		junk(132956, 16512, "", "Sandbox:MJBox:Chrome_SystemMessageWindow"),
		junk(132966, 16512, "Default IME", "IME"),
		junk(133070, 16512, "", "Sandbox:MJBox:DisplayICC_SystemMessageWindow"),
		junk(133090, 16512, "MSCTFIME UI", "MSCTFIME UI"),
		junk(133188, 15320, "MSCTFIME UI", "MSCTFIME UI"),
		junk(198402, 10356, "MSCTFIME UI", "MSCTFIME UI"),
		junk(198576, 1352, "", "Sandbox:MJBox:crashpad_SessionEndWatcher"),
		app(198604, 16512),
		junk(199252, 13332, "", "Sandbox:MJBox:DisplayICC_SystemMessageWindow"),
		junk(263868, 10356, "", "Sandbox:MJBox:Chrome_SystemMessageWindow"),
		junk(263870, 10356, "Default IME", "IME"),
		app(264192, 15376),
		junk(264678, 13332, "MSCTFIME UI", "MSCTFIME UI"),
		junk(264704, 12676, "Default IME", "IME"),
		junk(329500, 1352, "Default IME", "IME"),
		junk(395094, 15376, "", "Sandbox:MJBox:Chrome_SystemMessageWindow"),
		junk(395612, 13940, "Default IME", "IME"),
		app(395724, 12676),
		junk(395862, 12676, "", "Sandbox:MJBox:Chrome_SystemMessageWindow"),
		junk(460490, 10356, "", "Sandbox:MJBox:DisplayICC_SystemMessageWindow"),
		junk(461152, 13332, "", "Sandbox:MJBox:Chrome_SystemMessageWindow"),
		junk(525864, 17648, "Default IME", "IME"),
		junk(526706, 12676, "", "Sandbox:MJBox:DisplayICC_SystemMessageWindow"),
		junk(592086, 12484, "Default IME", "IME"),
		junk(656848, 16344, "Default IME", "IME"),
		app(656994, 10356),
		junk(657214, 15376, "", "Sandbox:MJBox:DisplayICC_SystemMessageWindow"),
		junk(657650, 17496, "", "Sandbox:MJBox:RunDLL"),
		app(658024, 13332),
		junk(722906, 16344, "", "Sandbox:MJBox:crashpad_SessionEndWatcher"),
		junk(788294, 10540, "", "Sandbox:MJBox:crashpad_SessionEndWatcher"),
		junk(854224, 17496, "Default IME", "IME"),
		junk(854330, 13332, "Default IME", "IME"),
		junk(1051108, 12484, "", "Sandbox:MJBox:crashpad_SessionEndWatcher"),
		junk(1116412, 12676, "MSCTFIME UI", "MSCTFIME UI"),
		junk(1313314, 13940, "", "Sandbox:MJBox:crashpad_SessionEndWatcher"),
		junk(2098424, 15376, "Default IME", "IME"),
		junk(2557036, 10540, "Default IME", "IME"),
		junk(3408016, 12384, "", "Sandbox:MJBox:Sandboxie_DDE_ProxyClass1"),
		junk(4392284, 15320, "", "Sandbox:MJBox:Chrome_SystemMessageWindow"),
		junk(5899288, 15320, "Default IME", "IME"),
		junk(6882894, 12384, "Default IME", "IME"),
		junk(8521164, 17648, "", "Sandbox:MJBox:crashpad_SessionEndWatcher"),
		junk(13174024, 15376, "MSCTFIME UI", "MSCTFIME UI"),
	}
}

// fieldBoxPIDs18 is the box PID set from the same log line.
func fieldBoxPIDs18() map[uint32]bool {
	return pidSetFromInts([]int{17, 1352, 5764, 10356, 10540, 12384, 12484, 12676, 13332, 13356, 13940, 15320, 15376, 16296, 16344, 16512, 17496, 17648})
}

func TestFieldFixture46Windows(t *testing.T) {
	wins := fieldBoxWindows46()
	if len(wins) != 46 {
		t.Fatalf("fixture transcribed wrong: %d windows, want 46", len(wins))
	}
	// The shape test must admit EXACTLY the six WeChat Qt main
	// windows and reject every plumbing window.
	var shaped []uint64
	for _, w := range wins {
		if isAppWindowShape(w) {
			shaped = append(shaped, w.HWND)
		}
		if w.Title == "WeChat" && isSystemWindowClass(w.Class) {
			t.Errorf("app window hwnd=%d misclassified as system class %q", w.HWND, w.Class)
		}
	}
	wantShaped := []uint64{67616, 198604, 264192, 395724, 656994, 658024}
	if len(shaped) != len(wantShaped) {
		t.Fatalf("app-shaped windows = %v, want %v", shaped, wantShaped)
	}
	for i := range wantShaped {
		if shaped[i] != wantShaped[i] {
			t.Fatalf("app-shaped windows = %v, want %v", shaped, wantShaped)
		}
	}
	// The whole list is the cross-check the v0.40 run produced:
	// it contains a born-hidden app window.
	if !hasHiddenAppWindow(wins) {
		t.Fatal("fixture must read as containing a hidden app window")
	}
	box := fieldBoxPIDs18()
	// No preference, no image evidence: uniform sizes make the
	// deterministic tie-break the lowest HWND among the six.
	got, ok := pickWindowForPIDs(wins, box, nil, nil, nil, "")
	if !ok || got.HWND != 67616 {
		t.Fatalf("fixture pick = %+v (%v), want hwnd 67616", got, ok)
	}
	// The launch difference wins: prefer the PID this launch
	// brought in (12676) and its WeChat window is picked.
	got2, ok2 := pickWindowForPIDs(wins, box, map[uint32]bool{12676: true}, nil, nil, "")
	if !ok2 || got2.HWND != 395724 || got2.PID != 12676 {
		t.Fatalf("fixture prefer pick = %+v (%v), want hwnd 395724 (pid 12676)", got2, ok2)
	}
	// With the first two picked windows excluded (already
	// registered), the next app window in line is chosen --
	// plumbing never surfaces at any point.
	got3, ok3 := pickWindowForPIDs(wins, box, map[uint32]bool{12676: true}, map[uint64]bool{67616: true, 395724: true}, nil, "")
	if !ok3 || got3.HWND != 198604 {
		t.Fatalf("fixture exclusion pick = %+v (%v), want hwnd 198604", got3, ok3)
	}
	// Restrict the PID set to a plumbing-only process and the
	// picker must find NOTHING (its windows are all junk or
	// untitled-hidden): pid 1352 holds only crashpad/IME windows.
	if _, ok4 := pickWindowForPIDs(wins, map[uint32]bool{1352: true}, nil, nil, nil, ""); ok4 {
		t.Fatal("plumbing-only PID must not yield a pick")
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
