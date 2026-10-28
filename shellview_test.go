package main

// v0.38 pure-decision tests: the desktop-view singleton verdict
// and the creation in-flight latch (shellview.go). The platform
// layer only collects the facts, so pinning these tables pins
// the user-visible "never two desktop pages" behaviour.

import (
	"testing"
	"time"
)

func TestDecideShellOpen(t *testing.T) {
	cases := []struct {
		name string
		f    shellFacts
		want shellOpenVerdict
	}{
		{"没有任何视图→新建", shellFacts{}, shellCreateNew},
		{"登记过但窗口已死→新建", shellFacts{Registered: true, Exists: false}, shellCreateNew},
		{"活着且在屏幕上→提到最前（最小化也算活着，先还原再提）", shellFacts{Registered: true, Exists: true, Visible: true, Iconic: true}, shellRaiseExisting},
		{"活着、可见、正常→提到最前", shellFacts{Registered: true, Exists: true, Visible: true}, shellRaiseExisting},
		{"活着但被红钮藏起（托盘态）→整套唤回", shellFacts{Registered: true, Exists: true, Visible: false, SetHidden: true}, shellRestoreTucked},
		{"活着但不可见（无论状态机）→整套唤回", shellFacts{Registered: true, Exists: true, Visible: false}, shellRestoreTucked},
		{"活着可见但整套被联动藏着→整套唤回", shellFacts{Registered: true, Exists: true, Visible: true, SetHidden: true}, shellRestoreTucked},
		{"已死+整套藏着→新建（死者优先于藏）", shellFacts{Registered: true, Exists: false, SetHidden: true}, shellCreateNew},
	}
	for _, c := range cases {
		if got := decideShellOpen(c.f); got != c.want {
			t.Errorf("%s: decideShellOpen(%+v) = %v, want %v", c.name, c.f, got, c.want)
		}
	}
}

func TestShellCreateInFlight(t *testing.T) {
	now := time.Unix(1700000000, 0)
	if shellCreateInFlightAt(time.Time{}, now) {
		t.Error("从未创建过→不该算在建")
	}
	if !shellCreateInFlightAt(now.Add(-time.Second), now) {
		t.Error("1 秒前刚开始建→应算在建（连点合并）")
	}
	if !shellCreateInFlightAt(now.Add(-shellCreateWindow+time.Second), now) {
		t.Error("窗口期内→应算在建")
	}
	if shellCreateInFlightAt(now.Add(-shellCreateWindow-time.Second), now) {
		t.Error("窗口期已过（认领迟迟没来）→必须放行再建，不能把用户锁死在没桌面的状态")
	}
}
