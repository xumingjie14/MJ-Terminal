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
	}
	for _, c := range cases {
		if got := stepSetVis(c.cur, c.ev, c.sw); got != c.want {
			t.Errorf("%s: stepSetVis(%v, %v, %+v) = %+v, want %+v", c.name, c.cur, c.ev, c.sw, got, c.want)
		}
	}
}
