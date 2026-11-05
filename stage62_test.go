package main

// v0.62 detail-fix trio tests: the raise verdict (①), the
// stage-focus hide list + solo book (③), the z-order guard
// verdict (⑤), the set-hide reasons (⑤), and the multi-window
// regression the user reported -- ONE Chrome process with THREE
// top-level windows must yield three independent claims, three
// independent registry entries, and per-window picking that
// survives one window going away. Pure decisions and books
// only, in the house style: the platform layer is not stubbed,
// so what is pinned here is the logic the field behaviour
// stands on.

import (
	"reflect"
	"testing"
	"time"
)

func TestDecideRaise(t *testing.T) {
	cases := []struct {
		name string
		f    raiseFacts
		want raiseOutcome
	}{
		{"活窗口+置前成功→OK", raiseFacts{Alive: true, FocusOK: true}, raiseOK},
		{"活窗口+置前失败→Failed（保留登记，记日志）", raiseFacts{Alive: true, FocusOK: false}, raiseFailed},
		{"死句柄→Gone（也保留登记，修剪归巡逻）", raiseFacts{Alive: false, FocusOK: false}, raiseGone},
		{"死句柄即使误报成功→Gone 优先", raiseFacts{Alive: false, FocusOK: true}, raiseGone},
	}
	for _, c := range cases {
		if got := decideRaise(c.f); got != c.want {
			t.Errorf("%s: decideRaise(%+v) = %v, want %v", c.name, c.f, got, c.want)
		}
	}
}

func TestDecideRaiseRetry(t *testing.T) {
	// v0.62 test2: the field case is (1, false) -- one user
	// click in ~15 failed outright. It must buy exactly one
	// retry; the second failure is final (the caller's WARN
	// path, registration kept per decideRaise's rule).
	cases := []struct {
		name     string
		attempts int
		ok       bool
		want     raiseRetryAction
	}{
		{"第一次成功→完成", 1, true, raiseActOK},
		{"第一次失败→重试一次", 1, false, raiseActRetry},
		{"重试成功→完成（记置前重试成功）", 2, true, raiseActOKAfterRetry},
		{"两次都失败→放弃（警告路径）", 2, false, raiseActGiveUp},
		{"预算外失败→保持放弃", 3, false, raiseActGiveUp},
		{"预算外成功→仍是重试成功", 3, true, raiseActOKAfterRetry},
	}
	for _, c := range cases {
		if got := decideRaiseRetry(c.attempts, c.ok); got != c.want {
			t.Errorf("%s: decideRaiseRetry(%d, %v) = %v, want %v", c.name, c.attempts, c.ok, got, c.want)
		}
	}
}

func TestFocusClaimedWindowDeadHandleGivesUp(t *testing.T) {
	// HWND 0 is never a window on any platform (IsWindow(0)
	// fails before any other call), so both raise attempts
	// fail deterministically: the verdict must be raiseFailed
	// -- never raiseGone, because removal is the patrol's
	// business, not a failed raise's.
	if got := focusClaimedWindow(0, "测试窗口"); got != raiseFailed {
		t.Errorf("focusClaimedWindow(0) = %v, want raiseFailed", got)
	}
}

func TestDecideRaiseVerify(t *testing.T) {
	// v0.62 test3: the field case is (FocusOK, BELOW the
	// shell): the test2 build logged ~50 raise "successes"
	// while the window sat under the full-size desktop view
	// and the user's clicks did nothing -- SetForegroundWindow
	// reported focus, not stage presence. The table below is
	// the fix: only focus + above-shell + not-iconic counts as
	// done; anything else spends the ONE escalation lift; a
	// second bad verdict fails, which focusClaimedWindow maps
	// to raiseFailed -- where decideRaise's standing rule
	// keeps the registration (removal is the patrol's alone,
	// pinned end-to-end by TestFocusClaimedWindowDeadHandle-
	// GivesUp above: even the fully-dead path never answers
	// raiseGone).
	cases := []struct {
		name   string
		checks int
		f      raiseStageFacts
		want   raiseVerifyAction
	}{
		{"接口成功+在桌面之上+未最小化→核验通过", 1, raiseStageFacts{FocusOK: true, AboveShell: true}, raiseVerifyDone},
		{"补提后的第二次核验同样通过", 2, raiseStageFacts{FocusOK: true, AboveShell: true}, raiseVerifyDone},
		{"接口成功却压在桌面之下→补提（真机原案）", 1, raiseStageFacts{FocusOK: true}, raiseVerifyLift},
		{"补提后仍在桌面之下→判失败", 2, raiseStageFacts{FocusOK: true}, raiseVerifyFail},
		{"接口成功但窗口还最小化着→补提", 1, raiseStageFacts{FocusOK: true, AboveShell: true, Iconic: true}, raiseVerifyLift},
		{"补提后仍最小化→判失败", 2, raiseStageFacts{FocusOK: true, AboveShell: true, Iconic: true}, raiseVerifyFail},
		{"接口失败但已在台上→补提时再问一次前台", 1, raiseStageFacts{AboveShell: true}, raiseVerifyLift},
		{"接口失败且补提额度已花掉→判失败", 2, raiseStageFacts{AboveShell: true}, raiseVerifyFail},
		{"全灭→第一次仍先补提", 1, raiseStageFacts{}, raiseVerifyLift},
		{"全灭→第二次判失败", 2, raiseStageFacts{}, raiseVerifyFail},
	}
	for _, c := range cases {
		if got := decideRaiseVerify(c.checks, c.f); got != c.want {
			t.Errorf("%s: decideRaiseVerify(%d, %+v) = %v, want %v", c.name, c.checks, c.f, got, c.want)
		}
	}
	if raiseStageMaxChecks != 2 {
		t.Errorf("raiseStageMaxChecks = %d, want 2 (judge the dance, then judge it once after the lift)", raiseStageMaxChecks)
	}
}

func TestDecideRaiseSweep(t *testing.T) {
	// v0.62 test3: a sweep tick re-lifts ONLY a live window
	// that has actually fallen back below the shell while its
	// set is supposed to be up. Everything else stands down
	// silently: the window is already up, it is gone, the user
	// minimized it themselves, or the set machine tucked the
	// whole set away (a sweep must never fight it).
	cases := []struct {
		name string
		f    raiseSweepFacts
		want raiseSweepAction
	}{
		{"在册+被盖回桌面之下+整套在场→再提上台", raiseSweepFacts{Registered: true}, raiseSweepLift},
		{"仍在桌面之上→收兵", raiseSweepFacts{Registered: true, AboveShell: true}, raiseSweepStop},
		{"窗口已关/已被巡逻修剪→收兵", raiseSweepFacts{}, raiseSweepStop},
		{"用户自己最小化了→收兵", raiseSweepFacts{Registered: true, Iconic: true}, raiseSweepStop},
		{"整套已被藏起→收兵（不和整套状态机打架）", raiseSweepFacts{Registered: true, SetHidden: true}, raiseSweepStop},
	}
	for _, c := range cases {
		if got := decideRaiseSweep(c.f); got != c.want {
			t.Errorf("%s: decideRaiseSweep(%+v) = %v, want %v", c.name, c.f, got, c.want)
		}
	}
}

// The sweep driver at the seam level (the TestHotkeyRetry-
// Driver pattern): arming books exactly one generation with
// two ticks at the pinned delays; a newer verified raise --
// any window -- retires the older sweep's claim, so its ticks
// fire into nothing and can never yank a window back over a
// stage the user has re-cast. The per-tick fact verdict is
// TestDecideRaiseSweep's; in this test process there is no
// shell and no host window, so a live tick stands down at the
// shell check and what is pinned here is the generation
// bookkeeping the anti-fight guard stands on (the same
// observable surface TestSoloReconcile pins).
func TestRaiseSweepDriver(t *testing.T) {
	raiseSweepState.Lock()
	raiseSweepState.gen, raiseSweepState.hwnd, raiseSweepState.label = 0, 0, ""
	raiseSweepState.Unlock()

	type tick struct {
		d time.Duration
		f func()
	}
	var ticks []tick
	origSchedule := raiseSweepSchedule
	raiseSweepSchedule = func(d time.Duration, f func()) *time.Timer {
		ticks = append(ticks, tick{d, f})
		return time.NewTimer(time.Hour) // captured, never fires on its own
	}
	defer func() { raiseSweepSchedule = origSchedule }()

	raiseSweepArm(101, "窗口甲")
	if len(ticks) != 2 {
		t.Fatalf("one arm scheduled %d ticks, want 2", len(ticks))
	}
	if ticks[0].d != raiseSweepTick1 || ticks[1].d != raiseSweepTick2 {
		t.Fatalf("tick delays = %v, %v; want %v, %v", ticks[0].d, ticks[1].d, raiseSweepTick1, raiseSweepTick2)
	}
	if raiseSweepTick1 != 300*time.Millisecond || raiseSweepTick2 != 800*time.Millisecond {
		t.Fatalf("sweep cadence = %v/%v, want ~300ms/~800ms (inside the click's activation tail, gone within a second)", raiseSweepTick1, raiseSweepTick2)
	}
	raiseSweepState.Lock()
	genA := raiseSweepState.gen
	raiseSweepState.Unlock()
	if label, live := raiseSweepClaim(genA, 101); !live || label != "窗口甲" {
		t.Fatalf("fresh arm: claim = %q, %v; want 窗口甲, true", label, live)
	}
	if _, live := raiseSweepClaim(genA, 999); live {
		t.Fatal("claim granted for a window the sweep does not serve")
	}

	// A newer verified raise retires sweep A. Its captured
	// ticks fire and must be inert: no claim, no panic, and
	// sweep B's bookkeeping untouched.
	raiseSweepArm(102, "窗口乙")
	raiseSweepState.Lock()
	genB := raiseSweepState.gen
	raiseSweepState.Unlock()
	if genB == genA {
		t.Fatal("second arm did not bump the generation")
	}
	if len(ticks) != 4 {
		t.Fatalf("two arms scheduled %d ticks, want 4", len(ticks))
	}
	for _, tk := range ticks[:2] {
		tk.f()
	}
	if _, live := raiseSweepClaim(genA, 101); live {
		t.Fatal("stale generation still claims the stage after a newer raise")
	}
	if label, live := raiseSweepClaim(genB, 102); !live || label != "窗口乙" {
		t.Fatalf("newer arm after stale fires: claim = %q, %v; want 窗口乙, true", label, live)
	}
	// The live ticks run their course quietly: with no shell
	// in this process each stands down at the shell check.
	ticks[2].f()
	ticks[3].f()
	if label, live := raiseSweepClaim(genB, 102); !live || label != "窗口乙" {
		t.Fatalf("live ticks disturbed the book: claim = %q, %v; want 窗口乙, true", label, live)
	}
}

func TestDecideHideOthers(t *testing.T) {
	cands := []soloCandidate{
		{HWND: 101, Visible: true},               // the anchor itself
		{HWND: 102, Visible: true},               // up on the desktop → hide
		{HWND: 103, Visible: true},               // up on the desktop → hide
		{HWND: 104, Visible: true, Iconic: true}, // user-minimized → never touched
		{HWND: 105, Visible: false},              // already invisible → not ours to restore
		{HWND: 0, Visible: true},                 // junk handle → skipped
	}
	if got, want := decideHideOthers(101, cands), []uint64{102, 103}; !reflect.DeepEqual(got, want) {
		t.Errorf("decideHideOthers = %v, want %v", got, want)
	}
	if got := decideHideOthers(101, nil); len(got) != 0 {
		t.Errorf("decideHideOthers(nil) = %v, want empty", got)
	}
	// Only the anchor exists: nothing hides, the action is a
	// plain raise.
	if got := decideHideOthers(101, cands[:1]); len(got) != 0 {
		t.Errorf("decideHideOthers(anchor only) = %v, want empty", got)
	}
}

func TestDecideZGuard(t *testing.T) {
	on := zGuardFacts{OverlayLive: true, ForegroundIsShell: true, ShellUsable: true}
	cases := []struct {
		name string
		f    zGuardFacts
		want bool
	}{
		{"浮层开+桌面在前台+桌面可用→守护", on, true},
		{"没有浮层会话→不守护（显式舞台动作时桌面可以正当前台）", zGuardFacts{ForegroundIsShell: true, ShellUsable: true}, false},
		{"前台是已认领窗口→不守护", zGuardFacts{OverlayLive: true, ShellUsable: true}, false},
		{"前台是宿主程序→不守护", zGuardFacts{OverlayLive: true, ForegroundIsShell: false, ShellUsable: true}, false},
		{"桌面最小化中→不守护", zGuardFacts{OverlayLive: true, ForegroundIsShell: true, ShellUsable: false}, false},
		{"整套藏着→不守护（绝不和整套状态机打架）", zGuardFacts{OverlayLive: true, ForegroundIsShell: true, ShellUsable: true, SetHidden: true}, false},
	}
	for _, c := range cases {
		if got := decideZGuard(c.f); got != c.want {
			t.Errorf("%s: decideZGuard(%+v) = %v, want %v", c.name, c.f, got, c.want)
		}
	}
}

func TestReasonForSetEvent(t *testing.T) {
	// ⑤'s diagnostics bar: every set-vis event that can hide
	// the set names its reason; no event maps to an empty one.
	for ev, want := range map[setVisEvent]string{
		evShellMinimized: "shell-minimized 桌面视图最小化联动",
		evShellClosed:    "shell-closed 桌面视图关闭收进托盘",
		evBossKeyHide:    "boss-key 老板键",
	} {
		if got := reasonForSetEvent(ev); got != want {
			t.Errorf("reasonForSetEvent(%v) = %q, want %q", ev, got, want)
		}
	}
	if got := reasonForSetEvent(evRestoreAll); got == "" {
		t.Error("reasonForSetEvent(evRestoreAll) is empty: a hide must never be reason-less")
	}
}

// The ① field report, modelled end to end at the decision
// level: one isolated-Chrome process (PID 4242) with three
// top-level windows, plus a HOST Chrome window that must never
// be claimed. The three terminal windows claim independently;
// the registry then holds three independent entries for the
// same PID; picking one for a second-launch presentation
// touches none of the others; and when the picked window goes
// away the remaining two are still pickable.
func TestStageMultiWindowOneProcess(t *testing.T) {
	procs := []procFact{
		{PID: 4242, PPID: 4, ImagePath: tChromeExe,
			CmdLine: `"C:\Program Files\Google\Chrome\Application\chrome.exe" --user-data-dir=C:\MJ-Terminal\Data\ChromeProfile`},
		{PID: 7777, PPID: 4, ImagePath: tChromeExe,
			CmdLine: `"C:\Program Files\Google\Chrome\Application\chrome.exe"`}, // host Chrome: default profile
	}
	owned := classifyOwnership(procs, nil, tCtx())
	if owned[4242] != ownProfile {
		t.Fatalf("classifyOwnership: isolated Chrome PID = %q, want %q", owned[4242], ownProfile)
	}
	if owned[7777] != "" {
		t.Fatalf("classifyOwnership: host Chrome PID claimed as %q, want unowned", owned[7777])
	}
	opts := claimOpts{AutoClaimBrowser: true, BrowserPIDs: map[uint32]bool{}, ShellPIDs: map[uint32]bool{}}
	reg := map[uint64]bool{}
	wins := []windowFact{
		{HWND: 101, PID: 4242, Class: "Chrome_WidgetWin_1", Title: "文档一 - Google Chrome", Visible: true, W: 1200, H: 800},
		{HWND: 102, PID: 4242, Class: "Chrome_WidgetWin_1", Title: "文档二 - Google Chrome", Visible: true, W: 1200, H: 800},
		{HWND: 103, PID: 4242, Class: "Chrome_WidgetWin_1", Title: "文档三 - Google Chrome", Visible: true, W: 1200, H: 800},
		{HWND: 201, PID: 7777, Class: "Chrome_WidgetWin_1", Title: "宿主窗口 - Google Chrome", Visible: true, W: 1200, H: 800},
	}
	claimed := map[uint64]bool{}
	for _, w := range wins {
		v := decideClaim(w, owned, reg, opts)
		if w.HWND == 201 {
			if v.Claim {
				t.Errorf("host Chrome window claimed (%+v): the patrol must never pull a host window in", v)
			}
			continue
		}
		if !v.Claim || !v.Browser {
			t.Errorf("window %d of the claimed process: verdict %+v, want a browser claim", w.HWND, v)
			continue
		}
		claimed[w.HWND] = true
		reg[w.HWND] = true // the patrol registers it; next tick it is in reg
	}
	if len(claimed) != 3 {
		t.Fatalf("claimed %d windows of one process, want 3 independent claims", len(claimed))
	}
	// A window born later from the same process (Chrome's
	// New Window) claims the same way -- nothing about the
	// first three registrations gates it.
	v := decideClaim(windowFact{HWND: 104, PID: 4242, Class: "Chrome_WidgetWin_1", Title: "新窗口 - Google Chrome", Visible: true, W: 1200, H: 800}, owned, reg, opts)
	if !v.Claim || !v.Browser {
		t.Errorf("late-born fourth window: verdict %+v, want a browser claim", v)
	}

	// Registry level: three entries, one PID, independent.
	extWinsTest := map[uint64]*extWindow{
		101: {HWND: 101, App: "Chrome", pid: 4242},
		102: {HWND: 102, App: "Chrome", pid: 4242},
		103: {HWND: 103, App: "Chrome", pid: 4242},
	}
	pids := map[uint32]bool{4242: true}
	first, ok := pickRegisteredForPIDs(extWinsTest, "Chrome", pids)
	if !ok || !claimed[first] {
		t.Fatalf("pickRegisteredForPIDs = %d, %v; want one of the three windows", first, ok)
	}
	if len(extWinsTest) != 3 {
		t.Fatalf("picking mutated the registry: %d entries, want 3", len(extWinsTest))
	}
	// The picked window closes (the ONLY removal path is the
	// prune's): the other two stay registered and pickable.
	delete(extWinsTest, first)
	second, ok := pickRegisteredForPIDs(extWinsTest, "Chrome", pids)
	if !ok || second == first || !claimed[second] {
		t.Fatalf("after one window closed: pick = %d, %v; want another of the remaining windows", second, ok)
	}
	if _, ok := pickRegisteredForPIDs(extWinsTest, "Chrome", map[uint32]bool{9999: true}); ok {
		t.Error("pick matched a PID set the windows do not belong to")
	}
}

func TestSoloBookLifecycle(t *testing.T) {
	soloTakeAll() // drain any state another test left behind
	defer soloTakeAll()

	soloBook(101, []uint64{102, 103})
	if !soloForget(102) {
		t.Fatal("soloForget(102) = false, want true (it was booked)")
	}
	if soloForget(102) {
		t.Fatal("soloForget(102) twice = true, want false")
	}
	// A second focus action with a new anchor merges: window
	// 103, hidden by the first action and invisible since, must
	// stay booked -- dropping it would strand it.
	soloBook(104, []uint64{105})
	anchor, hidden := soloTakeAll()
	if anchor != 104 {
		t.Errorf("solo anchor = %d, want 104 (the newest focus action)", anchor)
	}
	got := map[uint64]bool{}
	for _, h := range hidden {
		got[h] = true
	}
	if !got[103] || !got[105] || len(got) != 2 {
		t.Errorf("solo book after merge = %v, want {103, 105}", hidden)
	}
	// Drained means drained.
	if _, rest := soloTakeAll(); len(rest) != 0 {
		t.Errorf("solo book not empty after drain: %v", rest)
	}
	// Booking nothing clears the anchor: a focus action that
	// hid nothing must not leave a stale anchor behind.
	soloBook(101, nil)
	if anchor, _ := soloTakeAll(); anchor != 0 {
		t.Errorf("solo anchor after empty book = %d, want 0", anchor)
	}
}

func TestSoloReconcile(t *testing.T) {
	soloTakeAll()
	defer soloTakeAll()

	// Booked window died while the anchor lives: it just
	// leaves the book (on Linux the platform restores are
	// no-ops, so only the bookkeeping is observable here).
	soloBook(101, []uint64{102, 103})
	soloReconcile(map[uint64]bool{101: true, 102: true})
	anchor, hidden := soloTakeAll()
	if anchor != 101 || len(hidden) != 1 || hidden[0] != 102 {
		t.Fatalf("reconcile with dead booking: anchor=%d hidden=%v, want 101/[102]", anchor, hidden)
	}

	// Anchor died: the book empties (its windows are restored
	// by the platform calls inside soloReconcile).
	soloBook(101, []uint64{102, 103})
	soloReconcile(map[uint64]bool{102: true, 103: true})
	if anchor, hidden := soloTakeAll(); anchor != 0 || len(hidden) != 0 {
		t.Fatalf("reconcile with dead anchor: anchor=%d hidden=%v, want an empty book", anchor, hidden)
	}
}

func TestOverlaySessions(t *testing.T) {
	overlayClearAll()
	if overlayLive() {
		t.Fatal("overlay live after clear")
	}
	overlaySession("cc", true)
	overlaySession("cc", true) // repeat report: still one session
	if !overlayLive() {
		t.Fatal("overlay not live after open")
	}
	overlaySession("nc", true)
	overlaySession("cc", false)
	if !overlayLive() {
		t.Fatal("overlay died with cc while nc is still open")
	}
	overlaySession("nc", false)
	overlaySession("nc", false) // closing what is not open: no-op
	if overlayLive() {
		t.Fatal("overlay still live after all sessions closed")
	}
	overlaySession("menu", true)
	overlaySession("*", false) // the view's reset
	if overlayLive() {
		t.Fatal("overlay still live after reset")
	}
	overlaySession("", true) // junk kind: ignored
	if overlayLive() {
		t.Fatal("empty overlay kind armed the guard")
	}
}
