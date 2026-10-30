package main

// v0.52: the boss-key press decision (bosskey.go's
// decideBossAction). Pure over three facts -- locked, already
// boss-hidden, whose window is foreground -- so the whole press
// contract is pinned here without a window system.

import "testing"

func TestDecideBossAction(t *testing.T) {
	cases := []struct {
		name string
		f    bossFacts
		want bossAction
	}{
		// Locked always wins: the lock screen is the only thing
		// a press may produce while sealed.
		{"锁定（前台是桌面）→只唤锁屏", bossFacts{Locked: true, Foreground: fgShell}, bossActLock},
		{"锁定（前台是宿主）→只唤锁屏", bossFacts{Locked: true, Foreground: fgHost}, bossActLock},
		{"锁定且老板藏中（不可达态）→仍只唤锁屏", bossFacts{Locked: true, BossHidden: true, Foreground: fgHost}, bossActLock},
		// Our window in front: the press hides the whole set.
		{"前台是桌面视图→整套隐藏", bossFacts{Foreground: fgShell}, bossActHide},
		{"前台是已认领窗口→整套隐藏", bossFacts{Foreground: fgClaimed}, bossActHide},
		// Anything else in front: summon.
		{"前台是宿主应用→召唤", bossFacts{Foreground: fgHost}, bossActSummon},
		{"前台未知→召唤", bossFacts{Foreground: fgUnknown}, bossActSummon},
		// Already boss-hidden: a press is always a summon, even
		// if a re-shown window briefly reads as ours.
		{"老板藏中（前台宿主）→召唤", bossFacts{BossHidden: true, Foreground: fgHost}, bossActSummon},
		{"老板藏中（前台竟是已认领）→召唤不二次隐藏", bossFacts{BossHidden: true, Foreground: fgClaimed}, bossActSummon},
	}
	for _, c := range cases {
		if got := decideBossAction(c.f); got != c.want {
			t.Errorf("%s: decideBossAction(%+v) = %v, want %v", c.name, c.f, got, c.want)
		}
	}
}
