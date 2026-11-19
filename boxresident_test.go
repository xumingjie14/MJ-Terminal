package main

// v0.71 test7 specs for the native resident engine
// (boxresident.go). Everything here is a pure table: the
// review's rulings (B1 attribution, B2 alignment, B4 card
// states and the auto-relaunch gate, B6 the fuse, B3 the
// probe cache and hit-run shaping, B7 the settings pruning)
// are the kind of logic that must be PROVEN on Linux before
// a Windows machine is ever asked about it -- the platform
// primitives behind them (window style, the NCHITTEST probe)
// are the only parts a field machine can speak for.

import (
	"reflect"
	"runtime"
	"testing"
	"time"
)

func boxTestApps() []boxApp {
	return []boxApp{
		{ID: "wechat", Name: "微信", Exe: `C:\Apps\WeChat\WeChat.exe`, InstallDir: `C:\Apps\WeChat`},
		{ID: "helper", Name: "助手", Exe: `C:\Apps\WeChat\Helper\helper.exe`, InstallDir: `C:\Apps\WeChat\Helper`},
		{ID: "vlc", Name: "VLC", Exe: `C:\Apps\VLC\vlc.exe`, InstallDir: `C:\Apps\VLC`},
		{ID: "nodir", Name: "单文件", Exe: `C:\Apps\Single\tool.exe`},
	}
}

func TestAttributeWindow(t *testing.T) {
	apps := boxTestApps()
	cases := []struct {
		name  string
		image string
		inBox bool
		want  string // app id, "" = unattributed
		basis string
	}{
		{"exe exact", `C:\Apps\WeChat\WeChat.exe`, true, "wechat", "exe"},
		{"exe exact, case and slash variants", `c:/apps/wechat/wechat.exe`, true, "wechat", "exe"},
		{"exe exact, trailing spaces", `  C:\Apps\WeChat\WeChat.exe  `, true, "wechat", "exe"},
		{"same exe, NOT in the box", `C:\Apps\WeChat\WeChat.exe`, false, "", ""},
		{"empty image", ``, true, "", ""},
		{"helper exact beats the parent's dir claim", `C:\Apps\WeChat\Helper\helper.exe`, true, "helper", "exe"},
		{"unregistered binary under a registered dir", `C:\Apps\WeChat\Crashpad\crashpad.exe`, true, "wechat", "dir"},
		{"two dirs claim one binary: refused", `C:\Apps\WeChat\Helper\stray.exe`, true, "", ""},
		{"nodir app matches only its exe", `C:\Apps\Single\other.exe`, true, "", ""},
		{"nodir exe exact", `C:\Apps\Single\tool.exe`, true, "nodir", "exe"},
		{"unrelated program", `C:\Windows\explorer.exe`, true, "", ""},
	}
	for _, c := range cases {
		got, ok := attributeWindow(c.image, c.inBox, apps, "")
		if ok != (c.want != "") || got.AppID != c.want || got.Basis != c.basis {
			t.Errorf("%s: attributeWindow(%q, %v) = (%q, %q, %v), want (%q, %q)",
				c.name, c.image, c.inBox, got.AppID, got.Basis, ok, c.want, c.basis)
		}
	}
}

func TestAttributeWindowSharedExeRefused(t *testing.T) {
	apps := []boxApp{
		{ID: "a", Name: "A", Exe: `C:\Apps\Same\app.exe`, InstallDir: `C:\Apps\Same`},
		{ID: "b", Name: "B", Exe: `C:\Apps\Same\app.exe`, InstallDir: `C:\Apps\Same`},
	}
	if got, ok := attributeWindow(`C:\Apps\Same\app.exe`, true, apps, ""); ok || got.AppID != "" {
		t.Fatalf("a shared exe must attribute to nobody, got %q (ok=%v)", got.AppID, ok)
	}
}

func TestAttributeWindowPhysicalShadowPath(t *testing.T) {
	// A process snapshot that reports the PHYSICAL path under
	// the box root converts to the same virtual spelling the
	// registry holds (the sandbox.go mirror runs both ways).
	// The conversion is built on filepath.Rel, whose separator
	// semantics are the HOST's: the positive case is a Windows
	// spec (the platform lesson -- Linux cannot speak for it),
	// while the negative case below holds everywhere.
	apps := boxTestApps()
	boxRoot := `D:\MJ\Data\SbieBox`
	image := boxRoot + `\drive\C\Apps\WeChat\WeChat.exe`
	if runtime.GOOS == "windows" {
		got, ok := attributeWindow(image, true, apps, boxRoot)
		if !ok || got.AppID != "wechat" || got.Basis != "exe" {
			t.Fatalf("physical shadow path: got (%q, %q, %v), want (wechat, exe, true)", got.AppID, got.Basis, ok)
		}
	}
	// Without the box root to convert by, the physical path
	// must NOT match: it is not a spelling the registry knows.
	if _, ok := attributeWindow(image, true, apps, ""); ok {
		t.Fatal("an unconverted physical path must not attribute")
	}
}

func TestChooseAlignedWindow(t *testing.T) {
	// Stickiness beats a better basis; a launch claim beats
	// exe; exe beats dir; the lowest hwnd breaks every tie
	// deterministically, whatever the candidate order.
	cands := []alignedCand{
		{HWND: 30, Basis: "dir"},
		{HWND: 10, Basis: "exe"},
		{HWND: 20, Basis: "exe"},
	}
	if got := chooseAlignedWindow(0, cands); got != 10 {
		t.Fatalf("exe tie should pick the lowest hwnd, got %d", got)
	}
	if got := chooseAlignedWindow(30, cands); got != 30 {
		t.Fatalf("a live sticky window must win over better candidates, got %d", got)
	}
	if got := chooseAlignedWindow(99, cands); got != 10 {
		t.Fatalf("a dead sticky window falls back to the ladder, got %d", got)
	}
	claimed := []alignedCand{
		{HWND: 30, Basis: "dir", LaunchClaimed: true},
		{HWND: 10, Basis: "exe"},
	}
	if got := chooseAlignedWindow(0, claimed); got != 30 {
		t.Fatalf("the launch claim must beat an exe candidate, got %d", got)
	}
	if got := chooseAlignedWindow(0, nil); got != 0 {
		t.Fatalf("no candidates = no aligned window, got %d", got)
	}
	// Candidate order must not matter.
	rev := []alignedCand{cands[2], cands[1], cands[0]}
	if got := chooseAlignedWindow(0, rev); got != 10 {
		t.Fatalf("nondeterministic alignment: got %d", got)
	}
}

func TestDecideBoxCardState(t *testing.T) {
	cases := []struct {
		name  string
		facts boxCardFacts
		want  string
	}{
		{"front", boxCardFacts{HasWindow: true, WindowVisible: true}, "front"},
		{"back: hidden window", boxCardFacts{HasWindow: true}, "back"},
		{"back: process alive, no window", boxCardFacts{ProcAlive: true}, "back"},
		{"exited: nothing at all", boxCardFacts{}, "exited"},
		{"restoring while summoning", boxCardFacts{Summoning: true}, "restoring"},
		{"auto-relaunch pending reads restoring", boxCardFacts{AutoRelaunch: true}, "restoring"},
		{"auto-relaunch failing reads unstable", boxCardFacts{AutoRelaunch: true, Attempts: residentUnstableAttempts}, "unstable"},
		{"no auto-relaunch: attempts alone never read unstable", boxCardFacts{Attempts: residentUnstableAttempts + 1}, "exited"},
	}
	for _, c := range cases {
		if got := decideBoxCardState(c.facts); got != c.want {
			t.Errorf("%s: decideBoxCardState = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDecideHideFuse(t *testing.T) {
	if decideHideFuse(boxHideFuseThreshold - 1) {
		t.Fatal("one failure short of the threshold must not fuse")
	}
	if !decideHideFuse(boxHideFuseThreshold) {
		t.Fatal("the threshold itself must fuse")
	}
	if !decideHideFuse(boxHideFuseThreshold + 5) {
		t.Fatal("past the threshold stays fused")
	}
}

func TestPruneBoxAppIDMap(t *testing.T) {
	reg := map[string]bool{"wechat": true, "vlc": true}
	in := map[string]bool{"wechat": true, "vlc": false, "gone": true}
	if got := pruneBoxAppIDMap(in, reg); !reflect.DeepEqual(got, map[string]bool{"wechat": true}) {
		t.Fatalf("prune = %v, want only live true entries", got)
	}
	if got := pruneBoxAppIDMap(map[string]bool{"gone": true}, reg); got != nil {
		t.Fatalf("an all-stale map must prune to nil, got %v", got)
	}
	if got := pruneBoxAppIDMap(nil, reg); got != nil {
		t.Fatalf("nil in, nil out, got %v", got)
	}
	// An empty valid set prunes everything -- which is why
	// the settings caller only invokes this when the registry
	// was actually READ (boxRegistryIDSet's ok): an unreadable
	// registry must never reach here and wipe the lock set.
	if got := pruneBoxAppIDMap(map[string]bool{"wechat": true}, map[string]bool{}); got != nil {
		t.Fatalf("an empty valid set must prune to nil, got %v", got)
	}
}

func TestProbeCacheValid(t *testing.T) {
	now := time.Now()
	good := boxProbeEntry{RectL: 0, RectT: 0, RectR: 800, RectB: 600, DPI: 96, OK: true, At: now}
	if !probeCacheValid(good, 0, 0, 800, 600, 96, now) {
		t.Fatal("a fresh success entry must be valid")
	}
	if probeCacheValid(good, 0, 0, 900, 600, 96, now) {
		t.Fatal("a moved/resized window invalidates the entry")
	}
	if probeCacheValid(good, 0, 0, 800, 600, 144, now) {
		t.Fatal("a DPI change invalidates the entry")
	}
	bad := boxProbeEntry{RectL: 0, RectT: 0, RectR: 800, RectB: 600, DPI: 96, OK: false, At: now}
	if !probeCacheValid(bad, 0, 0, 800, 600, 96, now.Add(boxProbeRetryDelay/2)) {
		t.Fatal("a fresh failure entry is valid (do not re-probe every tick)")
	}
	if probeCacheValid(bad, 0, 0, 800, 600, 96, now.Add(boxProbeRetryDelay+time.Second)) {
		t.Fatal("a stale failure entry must be retried")
	}
}

func TestZoneFromHitRun(t *testing.T) {
	mk := func(xs ...bool) []hitSample {
		out := make([]hitSample, len(xs))
		for i, c := range xs {
			out[i] = hitSample{X: int32(100 + i*4), Close: c}
		}
		return out
	}
	// A run against the band's right edge (the usual shape);
	// runs narrower than 16 px are not buttons (see the
	// too-narrow case below).
	z, ok := zoneFromHitRun(mk(false, false, true, true, true, true, true), 4, 10, 30)
	if !ok || z.X1 != 100+7*4 || z.Y0 != 10 || z.Y1 != 40 {
		t.Fatalf("right-edge run: got %+v ok=%v", z, ok)
	}
	if z.X0 > 100+2*4 || z.X0 < 100+2*4-3 {
		t.Fatalf("zone left edge should hug the run start within a step: %+v", z)
	}
	// Two runs: the rightmost wins (the left one is another
	// caption button the window also hit-tests, e.g. help).
	z, ok = zoneFromHitRun(mk(true, true, true, true, false, true, true, true, true), 4, 0, 28)
	if !ok || z.X1 != 100+9*4 || z.X0 < 100+5*4-3 || z.X0 > 100+5*4 {
		t.Fatalf("the rightmost run must win: %+v ok=%v", z, ok)
	}
	// A three-sample run at step 4 spans under 16 px: refused.
	if _, ok := zoneFromHitRun(mk(false, true, true, true), 4, 0, 28); ok {
		t.Fatal("a sub-16px run must not shape a zone")
	}
	// A lone sample is not a button.
	if _, ok := zoneFromHitRun(mk(false, true, false), 4, 0, 28); ok {
		t.Fatal("a single hit sample must not shape a zone")
	}
	// No hits at all.
	if _, ok := zoneFromHitRun(mk(false, false), 4, 0, 28); ok {
		t.Fatal("no hits = no zone")
	}
}

func TestBoxAutoRelaunchDue(t *testing.T) {
	now := time.Now()
	booted := now.Add(-time.Hour)
	base := boxResidentAppState{MissingSince: now.Add(-residentConfirm - time.Second)}
	cases := []struct {
		name      string
		st        boxResidentAppState
		procAlive bool
		f         func(*boxResidentAppState)
		want      bool
	}{
		{"due", base, false, nil, true},
		{"process alive blocks", base, true, nil, false},
		{"still inside the confirm window", base, false, func(s *boxResidentAppState) {
			s.MissingSince = now.Add(-time.Second)
		}, false},
		{"no disappearance recorded", boxResidentAppState{}, false, nil, false},
		{"backoff not yet reached", base, false, func(s *boxResidentAppState) {
			s.NextTry = now.Add(time.Minute)
		}, false},
		{"summoning blocks", base, false, func(s *boxResidentAppState) { s.Summoning = true }, false},
		{"a launch in flight blocks", base, false, func(s *boxResidentAppState) { s.Launching = true }, false},
	}
	for _, c := range cases {
		st := c.st
		if c.f != nil {
			c.f(&st)
		}
		if got := boxAutoRelaunchDue(st, c.procAlive, now, booted); got != c.want {
			t.Errorf("%s: boxAutoRelaunchDue = %v, want %v", c.name, got, c.want)
		}
	}
	// The boot grace: right after the daemon starts, nothing
	// is "missing" yet, however old MissingSince looks.
	st := boxResidentAppState{MissingSince: now.Add(-time.Hour)}
	if boxAutoRelaunchDue(st, false, now, now.Add(-time.Second)) {
		t.Fatal("the boot grace must block auto-relaunch")
	}
}

// The native rows of the close-intercept truth table: same
// pure decision, native facts (intercept_test.go covers the
// web rows).
func TestDecideCloseInterceptNative(t *testing.T) {
	// The same proven geometry shape as intercept_test.go's
	// web rows: frame (8,0)-(792,592), BtnW 45 + FrameXW 8
	// gives a formula zone of [739,792) x [0,35).
	geo := interceptFacts{
		Enabled: true, ServicesActive: true,
		RectL: 0, RectT: 0, RectR: 800, RectB: 600,
		FrameL: 8, FrameT: 0, FrameR: 792, FrameB: 592,
		FrameOK: true, DpiWindow: 96,
		BtnW: 45, FrameXW: 8, FrameYW: 8, CaptionH: 23, PaddedB: 4,
	}
	nativeDown := func(x, y int32, extra func(*interceptFacts)) interceptFacts {
		f := geo
		f.Kind = interceptMouseDown
		f.X, f.Y = x, y
		f.TargetHWND = 42
		f.TargetBoxApp = "wechat"
		if extra != nil {
			extra(&f)
		}
		return f
	}
	// Standard frame: the formula zone is the band's right
	// end (46 px wide at 96 DPI) -- a press inside swallows.
	if d := decideCloseIntercept(nativeDown(770, 10, nil)); d.Action != interceptSwallow {
		t.Fatalf("native standard-frame close press: got %v (%s)", d.Action, d.Reason)
	}
	if d := decideCloseIntercept(nativeDown(400, 10, nil)); d.Action != interceptPass {
		t.Fatalf("native press mid-caption must pass, got %v", d.Action)
	}
	// Probed zone: the press is judged against the PROBED
	// rect, not the formula (a self-drawn button can sit
	// anywhere in the band).
	probe := func(f *interceptFacts) {
		f.ProbeOK = true
		f.ProbeL, f.ProbeT, f.ProbeR, f.ProbeB = 700, 0, 740, 28
	}
	if d := decideCloseIntercept(nativeDown(720, 10, probe)); d.Action != interceptSwallow {
		t.Fatalf("press inside the probed zone must swallow, got %v (%s)", d.Action, d.Reason)
	}
	if d := decideCloseIntercept(nativeDown(770, 10, probe)); d.Action != interceptPass {
		t.Fatalf("press outside the probed zone (but inside the formula zone) must pass, got %v", d.Action)
	}
	// Unprobable self-drawn window: the mouse path passes,
	// honestly -- Alt+F4 still hides (below).
	if d := decideCloseIntercept(nativeDown(770, 10, func(f *interceptFacts) { f.NativeNoZone = true })); d.Action != interceptPass {
		t.Fatalf("an unprobable window's press must pass, got %v", d.Action)
	}
	// The armed release inside the probed zone hides.
	up := nativeDown(720, 10, probe)
	up.Kind = interceptMouseUp
	up.ArmedHWND = 42
	if d := decideCloseIntercept(up); d.Action != interceptSwallowHide || d.HideHWND != 42 {
		t.Fatalf("armed release in the probed zone: got %v hwnd=%d", d.Action, d.HideHWND)
	}
	// Alt+F4 on a locked native window hides; on an unlocked
	// one it passes.
	key := interceptFacts{Kind: interceptKeyDown, Enabled: true, ServicesActive: true, AltF4: true, ForegroundHWND: 42, ForegroundBoxApp: "wechat"}
	if d := decideCloseIntercept(key); d.Action != interceptSwallowHide {
		t.Fatalf("Alt+F4 on a locked native window must hide, got %v", d.Action)
	}
	key.ForegroundBoxApp = ""
	if d := decideCloseIntercept(key); d.Action != interceptPass {
		t.Fatalf("Alt+F4 on an unlocked native window must pass, got %v", d.Action)
	}
}
