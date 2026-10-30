package main

// v0.37 pure-decision tests: birth placement, backdrop, and the
// whole-set visibility state machine (winset.go). The platform
// layer only collects the facts and performs the outcomes, so
// pinning these tables pins the user-visible behaviour.

import "testing"

func TestDecideBirthPlacement(t *testing.T) {
	base := birthFacts{Enabled: true, ShellReady: true}
	cases := []struct {
		name string
		f    birthFacts
		want birthAction
	}{
		{"普通新生窗口→落位", base, birthPlaceIt},
		{"出生即最大化→照样落位（本版主治症状）", birthFacts{Enabled: true, ShellReady: true, BornMaximized: true}, birthPlaceIt},
		{"已落位过→永不再摆", birthFacts{Enabled: true, ShellReady: true, Placed: true}, birthResolve},
		{"有属主的对话框→跳过且不再重试", birthFacts{Enabled: true, ShellReady: true, Owned: true}, birthResolve},
		{"对话框且整套藏着→对话框优先（resolve）", birthFacts{Enabled: true, ShellReady: true, Owned: true, SetHidden: true}, birthResolve},
		{"开关关闭→不落位", birthFacts{Enabled: false, ShellReady: true}, birthResolve},
		{"整套藏着→暂缓（跟随隐藏，恢复后再摆）", birthFacts{Enabled: true, ShellReady: true, SetHidden: true}, birthDefer},
		{"桌面不存在→暂缓", birthFacts{Enabled: true, ShellReady: false}, birthDefer},
		{"桌面最小化中（无框可落）→暂缓", birthFacts{Enabled: true, ShellReady: false, BornMaximized: true}, birthDefer},
	}
	for _, c := range cases {
		if got := decideBirthPlacement(c.f); got != c.want {
			t.Errorf("%s: decideBirthPlacement(%+v) = %v, want %v", c.name, c.f, got, c.want)
		}
	}
}

func TestDecideBackdrop(t *testing.T) {
	cases := []struct {
		name string
		f    backdropFacts
		want bool
	}{
		{"守护进程刚从桌面打开→垫背", backdropFacts{Enabled: true, ShellUsable: true, LaunchContext: true, Foreground: fgHost}, true},
		{"前台是桌面→垫背", backdropFacts{Enabled: true, ShellUsable: true, Foreground: fgShell}, true},
		{"前台是已认领窗口（弹窗刚抢到前台也算）→垫背", backdropFacts{Enabled: true, ShellUsable: true, Foreground: fgClaimed}, true},
		{"前台是宿主应用→只登记不垫背", backdropFacts{Enabled: true, ShellUsable: true, Foreground: fgHost}, false},
		{"前台未知→不垫背", backdropFacts{Enabled: true, ShellUsable: true, Foreground: fgUnknown}, false},
		{"开关关闭→不垫背（垫背跟随落位开关）", backdropFacts{Enabled: false, ShellUsable: true, LaunchContext: true}, false},
		{"对话框→不垫背", backdropFacts{Enabled: true, ShellUsable: true, Owned: true, LaunchContext: true}, false},
		{"整套藏着→不垫背", backdropFacts{Enabled: true, ShellUsable: true, SetHidden: true, LaunchContext: true}, false},
		{"桌面最小化中（框不可用）→不垫背", backdropFacts{Enabled: true, ShellUsable: false, LaunchContext: true}, false},
	}
	for _, c := range cases {
		if got := decideBackdrop(c.f); got != c.want {
			t.Errorf("%s: decideBackdrop(%+v) = %v, want %v", c.name, c.f, got, c.want)
		}
	}
}

func TestDecidePresentRetry(t *testing.T) {
	onStage := presentFacts{Pending: true, ExpectPresent: true, Foreground: true, AboveShell: true}
	cases := []struct {
		name string
		f    presentFacts
		want presentAction
	}{
		{"没打过待呈现标记→无事", presentFacts{}, presentDone},
		{"出生不期待呈现（宿主语境悄悄认领）→无事", presentFacts{Pending: true, ExpectPresent: false, Foreground: false}, presentDone},
		{"期待呈现且在前台又在桌面之上→验货通过", onStage, presentDone},
		{"期待呈现但不在前台→补提", presentFacts{Pending: true, ExpectPresent: true, Foreground: false, AboveShell: true}, presentRetry},
		{"在前台但被桌面盖住（v0.54 头条根因）→补提", presentFacts{Pending: true, ExpectPresent: true, Foreground: true, AboveShell: false}, presentRetry},
		{"在前台、在桌面之上、但最小化着→补提（还原）", presentFacts{Pending: true, ExpectPresent: true, Foreground: true, AboveShell: true, Iconic: true}, presentRetry},
		{"补提一次仍未露面→还有第二次", presentFacts{Pending: true, ExpectPresent: true, Foreground: false, AboveShell: true, RetriesUsed: 1}, presentRetry},
		{"补提预算用尽、仍不在前台→收手（归用户）", presentFacts{Pending: true, ExpectPresent: true, Foreground: false, AboveShell: true, RetriesUsed: 2}, presentDone},
		{"补提预算用尽、被桌面盖住→收手（归用户）", presentFacts{Pending: true, ExpectPresent: true, Foreground: true, AboveShell: false, RetriesUsed: 2}, presentDone},
		{"整套藏着出生的→等整套恢复再验货", presentFacts{Pending: true, ExpectPresent: true, Foreground: false, SetHidden: true}, presentWait},
		{"整套藏着且已在前台（不可能态）→藏优先，继续等", presentFacts{Pending: true, ExpectPresent: true, Foreground: true, AboveShell: true, SetHidden: true}, presentWait},
		{"老板键藏着→新窗口绝不自己跳出来，等整套恢复", presentFacts{Pending: true, ExpectPresent: true, Foreground: false, BossHidden: true}, presentWait},
		{"老板键藏着且事实称在台上（不可能态）→藏优先", presentFacts{Pending: true, ExpectPresent: true, Foreground: true, AboveShell: true, BossHidden: true}, presentWait},
		// v0.54: immersion must NOT change the verdict either
		// way -- presentation is owed the same with it on/off.
		{"沉浸开+在台上→验货通过（与沉浸关同判）", presentFacts{Pending: true, ExpectPresent: true, Foreground: true, AboveShell: true, Immersed: true}, presentDone},
		{"沉浸开+被盖住→照样补提（与沉浸关同判）", presentFacts{Pending: true, ExpectPresent: true, Foreground: true, AboveShell: false, Immersed: true}, presentRetry},
		{"沉浸开+老板藏→照样等（与沉浸关同判）", presentFacts{Pending: true, ExpectPresent: true, Foreground: false, BossHidden: true, Immersed: true}, presentWait},
	}
	for _, c := range cases {
		if got := decidePresentRetry(c.f); got != c.want {
			t.Errorf("%s: decidePresentRetry(%+v) = %v, want %v", c.name, c.f, got, c.want)
		}
	}
}

func TestStepSetVis(t *testing.T) {
	on := setVisSwitches{MinimizeLink: true, CloseToTray: true}
	cases := []struct {
		name string
		cur  setVisState
		ev   setVisEvent
		sw   setVisSwitches
		want setVisStep
	}{
		{"显示中+桌面最小化（联动开）→随最小化藏", visShown, evShellMinimized, on,
			setVisStep{Next: visHiddenMinimize, HideSet: true}},
		{"显示中+桌面最小化（联动关）→不动", visShown, evShellMinimized, setVisSwitches{MinimizeLink: false, CloseToTray: true},
			setVisStep{Next: visShown}},
		{"显示中+桌面关闭（托盘开）→退托盘", visShown, evShellClosed, on,
			setVisStep{Next: visHiddenTray, HideSet: true, TrayDelta: 1}},
		{"显示中+桌面关闭（托盘关）→不动（老行为）", visShown, evShellClosed, setVisSwitches{MinimizeLink: true, CloseToTray: false},
			setVisStep{Next: visShown}},
		{"最小化藏+桌面恢复→整套回来", visHiddenMinimize, evShellRestored, on,
			setVisStep{Next: visShown, RestoreSet: true}},
		{"最小化藏+名单点击恢复→整套回来", visHiddenMinimize, evRestoreAll, on,
			setVisStep{Next: visShown, RestoreSet: true}},
		{"最小化藏+桌面被关（托盘开）→转托盘藏（不再重复藏）", visHiddenMinimize, evShellClosed, on,
			setVisStep{Next: visHiddenTray, TrayDelta: 1}},
		{"最小化藏+桌面被关（托盘关）→整套回来", visHiddenMinimize, evShellClosed, setVisSwitches{MinimizeLink: true, CloseToTray: false},
			setVisStep{Next: visShown, RestoreSet: true}},
		{"最小化藏+再次最小化事件→保持", visHiddenMinimize, evShellMinimized, on,
			setVisStep{Next: visHiddenMinimize}},
		{"最小化藏+新桌面打开→整套回来", visHiddenMinimize, evShellOpened, on,
			setVisStep{Next: visShown, RestoreSet: true}},
		{"托盘藏+托盘点击恢复→整套回来且撤图标", visHiddenTray, evRestoreAll, on,
			setVisStep{Next: visShown, RestoreSet: true, TrayDelta: -1}},
		{"托盘藏+双击EXE开出新桌面→整套回来且撤图标", visHiddenTray, evShellOpened, on,
			setVisStep{Next: visShown, RestoreSet: true, TrayDelta: -1}},
		{"托盘藏+最小化事件（无桌面）→保持", visHiddenTray, evShellMinimized, on,
			setVisStep{Next: visHiddenTray}},
		{"托盘藏+关闭事件（无桌面）→保持", visHiddenTray, evShellClosed, on,
			setVisStep{Next: visHiddenTray}},
		{"显示中+恢复请求→无操作", visShown, evRestoreAll, on,
			setVisStep{Next: visShown}},
		{"显示中+新桌面打开→无操作", visShown, evShellOpened, on,
			setVisStep{Next: visShown}},
		// v0.52 boss key: its own gesture -- independent of the
		// minimize / close-to-tray switches, parks the tray icon
		// as the return anchor, and leaves via the same restore
		// events as the tray state.
		{"显示中+老板键→老板藏且停靠托盘", visShown, evBossKeyHide, on,
			setVisStep{Next: visHiddenBoss, HideSet: true, TrayDelta: 1}},
		{"显示中+老板键（两个联动开关全关）→照样老板藏", visShown, evBossKeyHide, setVisSwitches{},
			setVisStep{Next: visHiddenBoss, HideSet: true, TrayDelta: 1}},
		{"老板藏+恢复请求（热键/托盘）→整套回来且撤图标", visHiddenBoss, evRestoreAll, on,
			setVisStep{Next: visShown, RestoreSet: true, TrayDelta: -1}},
		{"老板藏+双击EXE开出新桌面→整套回来且撤图标", visHiddenBoss, evShellOpened, on,
			setVisStep{Next: visShown, RestoreSet: true, TrayDelta: -1}},
		{"老板藏+桌面死亡（崩溃）→转托盘藏（集合不恢复、图标不重复停靠）", visHiddenBoss, evShellClosed, on,
			setVisStep{Next: visHiddenTray}},
		{"老板藏+最小化事件（无意义）→保持", visHiddenBoss, evShellMinimized, on,
			setVisStep{Next: visHiddenBoss}},
		{"老板藏+再次老板键事件（决策层不会发）→保持", visHiddenBoss, evBossKeyHide, on,
			setVisStep{Next: visHiddenBoss}},
		{"最小化藏+老板键（单窗被手动恢复且在前台）→转老板藏且停靠托盘", visHiddenMinimize, evBossKeyHide, on,
			setVisStep{Next: visHiddenBoss, HideSet: true, TrayDelta: 1}},
		{"托盘藏+老板键事件（决策层不会发）→保持", visHiddenTray, evBossKeyHide, on,
			setVisStep{Next: visHiddenTray}},
	}
	for _, c := range cases {
		if got := stepSetVis(c.cur, c.ev, c.sw); got != c.want {
			t.Errorf("%s: stepSetVis(%v, %v, %+v) = %+v, want %+v", c.name, c.cur, c.ev, c.sw, got, c.want)
		}
	}
}

// v0.48: the exit kill set -- downward expansion from OUR roots
// only, self and kernel PIDs excluded even when offered as roots,
// dead roots ignored, unrelated host processes never included.
func TestComputeKillSet(t *testing.T) {
	procs := []procFact{
		{PID: 1, PPID: 0},
		{PID: 100, PPID: 1, ImagePath: `C:\apps\Telegram.exe`}, // root: a tracked launch
		{PID: 101, PPID: 100, ImagePath: `C:\apps\helper.exe`}, // child
		{PID: 102, PPID: 101, ImagePath: `C:\apps\gc.exe`},     // grandchild
		{PID: 2, PPID: 100}, // kernel-ish PID: excluded even as a child
		{PID: 200, PPID: 1, ImagePath: `C:\host\notepad.exe`}, // unrelated host app
		{PID: 201, PPID: 200, ImagePath: `C:\host\child.exe`}, // its child: also not ours
		{PID: 999, PPID: 1, ImagePath: `C:\MJ\MJ-System.exe`}, // the daemon itself
	}
	roots := map[uint32]bool{100: true, 999: true, 4242: true} // self offered; 4242 is dead
	got := computeKillSet(procs, roots, 999)
	want := map[uint32]bool{100: true, 101: true, 102: true}
	if len(got) != len(want) {
		t.Fatalf("kill set = %v, want %v", got, want)
	}
	for pid := range want {
		if !got[pid] {
			t.Errorf("pid %d missing from kill set", pid)
		}
	}
	for _, pid := range []uint32{2, 200, 201, 999, 4242} {
		if got[pid] {
			t.Errorf("pid %d must never be in the kill set", pid)
		}
	}
}
