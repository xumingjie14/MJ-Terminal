package main

// v0.45 pure-decision tests: the immersion state machine
// (immerse.go), the fail-visible line, the exit-restore list,
// the startup-sweep orphan verdict, and the desktop-fullscreen
// geometry round-trip. The platform layer only performs the
// outcomes these decisions pick, so pinning the tables pins the
// user-visible contract: nothing unregistered is EVER hidden.

import "testing"

func TestDecideImmerse(t *testing.T) {
	cases := []struct {
		name string
		f    immerseFacts
		want immerseAction
	}{
		{"开+在册+未沉浸→沉浸", immerseFacts{Enabled: true, Registered: true}, immApply},
		{"开+在册+已沉浸→保持", immerseFacts{Enabled: true, Registered: true, Immersed: true}, immNone},
		{"关+已沉浸→还原", immerseFacts{Enabled: false, Registered: true, Immersed: true}, immRestore},
		{"关+未沉浸→无事", immerseFacts{Enabled: false, Registered: true}, immNone},
		{"开+不在册+已沉浸→立即还原（fail-visible）", immerseFacts{Enabled: true, Registered: false, Immersed: true}, immRestore},
		{"关+不在册+已沉浸→还原", immerseFacts{Enabled: false, Registered: false, Immersed: true}, immRestore},
		{"开+不在册+未沉浸→绝不隐藏", immerseFacts{Enabled: true, Registered: false}, immNone},
		{"关+不在册+未沉浸→无事", immerseFacts{Enabled: false, Registered: false}, immNone},
		// 集合隐藏状态（v0.37 整套联动隐藏）与沉浸正交：同样的
		// 开关×在册×已沉浸组合，整套藏着与否结论必须一致——
		// 显隐归集合状态机，样式归沉浸，冲突时集合隐藏优先。
		{"开+在册+未沉浸+整套藏→照样沉浸（样式先行，显隐归集合）", immerseFacts{Enabled: true, Registered: true, SetHidden: true}, immApply},
		{"开+在册+已沉浸+整套藏→保持", immerseFacts{Enabled: true, Registered: true, Immersed: true, SetHidden: true}, immNone},
		{"关+已沉浸+整套藏→只还原样式（窗口仍由集合藏着，不抢显示）", immerseFacts{Enabled: false, Registered: true, Immersed: true, SetHidden: true}, immRestore},
		{"开+不在册+已沉浸+整套藏→fail-visible 还原样式", immerseFacts{Enabled: true, Registered: false, Immersed: true, SetHidden: true}, immRestore},
	}
	for _, c := range cases {
		if got := decideImmerse(c.f); got != c.want {
			t.Errorf("%s: decideImmerse(%+v) = %v, want %v", c.name, c.f, got, c.want)
		}
	}
}

// TestDecideImmerseFailVisibleInvariant exhausts every fact
// combination and pins the user's acceptance line directly:
// an unregistered window is never told to hide, in ANY state.
func TestDecideImmerseFailVisibleInvariant(t *testing.T) {
	for _, en := range []bool{false, true} {
		for _, reg := range []bool{false, true} {
			for _, imm := range []bool{false, true} {
				for _, hid := range []bool{false, true} {
					f := immerseFacts{Enabled: en, Registered: reg, Immersed: imm, SetHidden: hid}
					got := decideImmerse(f)
					if !reg && got == immApply {
						t.Errorf("未在册窗口被判隐藏：%+v", f)
					}
					if imm && !reg && got != immRestore {
						t.Errorf("已沉浸但已不在册，必须立即还原：%+v → %v", f, got)
					}
					if imm && !en && got != immRestore {
						t.Errorf("沉浸关闭时已沉浸窗口必须还原：%+v → %v", f, got)
					}
					// SetHidden 正交性：翻转它结论不变。
					f2 := f
					f2.SetHidden = !hid
					if decideImmerse(f2) != got {
						t.Errorf("集合隐藏状态不应改变沉浸结论：%+v vs %+v", f, f2)
					}
				}
			}
		}
	}
}

func TestImmerseExitList(t *testing.T) {
	book := map[uint64]immerseRec{
		300: {OrigStyle: 0x100},
		100: {OrigStyle: 0x200},
		200: {OrigStyle: 0x300},
	}
	got := immerseExitList(book)
	want := []uint64{100, 200, 300}
	if len(got) != len(want) {
		t.Fatalf("退出还原清单长度 = %v, want %v（全部在册沉浸窗口一个不缺）", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("退出还原清单 = %v, want %v（排序确定）", got, want)
		}
	}
	if got := immerseExitList(map[uint64]immerseRec{}); len(got) != 0 {
		t.Errorf("空账本应给出空清单，got %v", got)
	}
}

func TestIsOrphanMark(t *testing.T) {
	cases := []struct {
		name       string
		marked     bool
		registered bool
		want       bool
	}{
		{"有标记+不在册→孤儿，清扫还原", true, false, true},
		{"有标记+已在册→本守护进程的活窗口，不动", true, true, false},
		{"无标记+不在册→与沉浸无关，不动", false, false, false},
		{"无标记+在册→普通窗口，不动", false, true, false},
	}
	for _, c := range cases {
		if got := isOrphanMark(c.marked, c.registered); got != c.want {
			t.Errorf("%s: isOrphanMark(%v, %v) = %v, want %v", c.name, c.marked, c.registered, got, c.want)
		}
	}
}

func TestDecideFullscreen(t *testing.T) {
	cases := []struct {
		name string
		f    fsFacts
		want fsAction
	}{
		{"开+未全屏+有桌面→进入", fsFacts{Want: true, ShellPresent: true}, fsEnter},
		{"开+已全屏+同一桌面→保持", fsFacts{Want: true, Active: true, ShellPresent: true, SameShell: true}, fsNone},
		{"开+已全屏+桌面已重建→退出旧的再进新的", fsFacts{Want: true, Active: true, ShellPresent: true, SameShell: false}, fsReenter},
		{"开+无桌面→等桌面出现", fsFacts{Want: true, ShellPresent: false}, fsNone},
		{"关+已全屏→退出还原", fsFacts{Want: false, Active: true, ShellPresent: true, SameShell: true}, fsExit},
		{"关+已全屏+桌面已关→照样退出（清状态）", fsFacts{Want: false, Active: true, ShellPresent: false}, fsExit},
		{"关+未全屏→无事", fsFacts{Want: false}, fsNone},
	}
	for _, c := range cases {
		if got := decideFullscreen(c.f); got != c.want {
			t.Errorf("%s: decideFullscreen(%+v) = %v, want %v", c.name, c.f, got, c.want)
		}
	}
}

// TestFsStateRoundTrip pins the geometry promise: whatever
// frame was saved when fullscreen began comes back verbatim
// when it ends, and a second end finds nothing.
func TestFsStateRoundTrip(t *testing.T) {
	var s fsState
	if _, _, _, _, ok := s.end(); ok {
		t.Fatal("未进入全屏时 end 不应有东西可还原")
	}
	rect := [4]int32{120, 80, 1320, 880}
	s.begin(4242, 0x16cf0000, rect, true)
	if !s.Active || !shellFsStateMatches(s, 4242, 0x16cf0000, rect, true) {
		t.Fatalf("begin 后状态不对：%+v", s)
	}
	hwnd, style, gotRect, zoomed, ok := s.end()
	if !ok || hwnd != 4242 || style != 0x16cf0000 || gotRect != rect || !zoomed {
		t.Fatalf("end 往返不一致：hwnd=%d style=%#x rect=%v zoomed=%v ok=%v",
			hwnd, style, gotRect, zoomed, ok)
	}
	if s.Active {
		t.Fatal("end 后状态应已清空")
	}
	if _, _, _, _, ok := s.end(); ok {
		t.Fatal("清空后再 end 不应重复还原")
	}
	// 浮动窗口（未最大化）往返同样原样。
	s.begin(7, 0x14c80000, [4]int32{0, 0, 800, 600}, false)
	_, _, r2, z2, ok2 := s.end()
	if !ok2 || r2 != [4]int32{0, 0, 800, 600} || z2 {
		t.Fatalf("浮动窗口往返不一致：rect=%v zoomed=%v ok=%v", r2, z2, ok2)
	}
}

func shellFsStateMatches(s fsState, hwnd uint64, style int32, rect [4]int32, zoomed bool) bool {
	return s.HWND == hwnd && s.Style == style && s.Rect == rect && s.Zoomed == zoomed
}
