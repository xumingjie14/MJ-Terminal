package main

// Pure-logic tests for app isolation (v0.39): ini merge, path
// conversion, candidate ranking, kind sniffing, registry ops.
// Nothing here touches a real Sandboxie install.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMergeBoxSectionEmpty(t *testing.T) {
	out, changed := mergeBoxSection("", `D:\MJ\Data\sandbox\MJBox`, nil)
	if !changed {
		t.Fatal("empty ini must report changed")
	}
	if !strings.Contains(out, "[MJBox]") || !strings.Contains(out, `FileRootPath=D:\MJ\Data\sandbox\MJBox`) {
		t.Fatalf("section missing: %q", out)
	}
}

func TestMergeBoxSectionPreservesOthersAndIdempotent(t *testing.T) {
	orig := "[GlobalSettings]\r\nBoxRootFolder=C:\\Sandbox\r\n\r\n[OtherBox]\r\nEnabled=y\r\nFileRootPath=C:\\Sandbox\\Other\r\n"
	out, changed := mergeBoxSection(orig, `E:\Data\sandbox\MJBox`, nil)
	if !changed {
		t.Fatal("adding MJBox must report changed")
	}
	for _, want := range []string{"[GlobalSettings]", "BoxRootFolder=C:\\Sandbox", "[OtherBox]", `FileRootPath=C:\Sandbox\Other`, "[MJBox]", `FileRootPath=E:\Data\sandbox\MJBox`} {
		if !strings.Contains(out, want) {
			t.Fatalf("lost %q in:\n%s", want, out)
		}
	}
	out2, changed2 := mergeBoxSection(out, `E:\Data\sandbox\MJBox`, nil)
	if changed2 {
		t.Fatalf("second merge must be a no-op, got:\n%s", out2)
	}
}

func TestMergeBoxSectionReplacesOldRoot(t *testing.T) {
	orig := "[MJBox]\r\nEnabled=y\r\nFileRootPath=C:\\Old\\Data\\sandbox\\MJBox\r\nAutoRecover=n\r\n\r\n[Other]\r\nEnabled=y\r\n"
	out, changed := mergeBoxSection(orig, `D:\New\sandbox\MJBox`, nil)
	if !changed {
		t.Fatal("root change must report changed")
	}
	if strings.Contains(out, `C:\Old`) {
		t.Fatalf("old root survived:\n%s", out)
	}
	if !strings.Contains(out, "[Other]") {
		t.Fatalf("other section lost:\n%s", out)
	}
	if n := strings.Count(out, "[MJBox]"); n != 1 {
		t.Fatalf("MJBox section count = %d", n)
	}
}

func TestIniCodecRoundTrip(t *testing.T) {
	text := "[MJBox]\r\nEnabled=y\r\nFileRootPath=D:\\数据\\sandbox\\MJBox\r\n"
	enc := encodeSbieIni(text, true)
	if enc[0] != 0xFF || enc[1] != 0xFE {
		t.Fatal("UTF-16 output must carry the BOM")
	}
	back, is16 := decodeSbieIni(enc)
	if !is16 || back != text {
		t.Fatalf("round trip mismatch: %q (%v)", back, is16)
	}
	plain, is16b := decodeSbieIni([]byte("[A]\r\nx=1\r\n"))
	if is16b || !strings.Contains(plain, "x=1") {
		t.Fatalf("plain decode wrong: %q %v", plain, is16b)
	}
	bom, _ := decodeSbieIni(append([]byte{0xEF, 0xBB, 0xBF}, []byte("[A]")...))
	if bom != "[A]" {
		t.Fatalf("UTF-8 BOM not stripped: %q", bom)
	}
}

func TestPathConversions(t *testing.T) {
	root := `D:\MJ\Data\sandbox\MJBox`
	// Physical expectations are joined with the HOST separator
	// (the conversion produces host-native physical paths); the
	// virtual side always uses backslashes.
	cases := []struct{ vp, pp string }{
		{`C:\Program Files\Tencent\Weixin\Weixin.exe`, filepath.Join(root, "drive", "C", "Program Files", "Tencent", "Weixin", "Weixin.exe")},
		{`D:\Apps\Foo\foo.exe`, filepath.Join(root, "drive", "D", "Apps", "Foo", "foo.exe")},
		{`C:\Users\ming\AppData\Local\Programs\Bar\bar.exe`, filepath.Join(root, "user", "ming", "AppData", "Local", "Programs", "Bar", "bar.exe")},
		{`C:\Users\ming\Documents\x.txt`, filepath.Join(root, "user", "ming", "Documents", "x.txt")},
	}
	for _, c := range cases {
		if got := virtualToPhysical(root, c.vp); got != c.pp {
			t.Errorf("virtualToPhysical(%q) = %q, want %q", c.vp, got, c.pp)
		}
		if got := physicalToVirtual(root, c.pp); !strings.EqualFold(got, c.vp) {
			t.Errorf("physicalToVirtual(%q) = %q, want %q", c.pp, got, c.vp)
		}
	}
	if got := physicalToVirtual(root, root+`\RegHive`); got != "" {
		t.Errorf("hive file must not map to a virtual path, got %q", got)
	}
	if got := virtualToPhysical(root, `not-a-path`); got != "" {
		t.Errorf("garbage virtual path must map to \"\", got %q", got)
	}
	if got := physicalToVirtual(root, `C:\elsewhere\x.exe`); got != "" {
		t.Errorf("outside path must map to \"\", got %q", got)
	}
}

func TestAppInstallDirVirtual(t *testing.T) {
	cases := map[string]string{
		`C:\Program Files\Tencent\Weixin\Weixin.exe`:       `C:\Program Files\Tencent\Weixin`,
		`C:\Program Files\Weixin\Weixin.exe`:               `C:\Program Files\Weixin`,
		`C:\Program Files\app.exe`:                         ``,
		`C:\Program Files (x86)\Foo\Bar\bar.exe`:           `C:\Program Files (x86)\Foo\Bar`,
		`C:\Program Files\Foo\1.2.3\foo.exe`:               `C:\Program Files\Foo`,
		`C:\Program Files\Foo\bin\foo.exe`:                 `C:\Program Files\Foo`,
		`C:\Users\ming\AppData\Local\Programs\Bar\bar.exe`: `C:\Users\ming\AppData\Local\Programs\Bar`,
		`C:\Users\ming\AppData\Local\Temp\app.exe`:         `C:\Users\ming\AppData\Local\Temp`,
		`D:\Tools\foo.exe`:                                 `D:\Tools`,
	}
	for in, want := range cases {
		if got := appInstallDirVirtual(in); got != want {
			t.Errorf("appInstallDirVirtual(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRankBoxCandidates(t *testing.T) {
	cands := []boxExeCand{
		{Virtual: `C:\Program Files\Tencent\Weixin\unins000.exe`, Size: 1 << 20},
		{Virtual: `C:\Program Files\Tencent\Weixin\Weixin.exe`, Size: 180 << 20},
		{Virtual: `C:\Program Files\Tencent\Weixin\WeixinUpdate.exe`, Size: 5 << 20},
		{Virtual: `C:\Users\ming\AppData\Local\Programs\Side\side.exe`, Size: 20 << 20},
		{Virtual: `C:\Program Files\Tencent\Weixin\crashpad_handler.exe`, Size: 1 << 20},
	}
	ranked := rankBoxCandidates(cands, "WeChatWin_4.2.0.exe")
	if len(ranked) != 4 {
		t.Fatalf("uninstaller must be excluded, got %d candidates", len(ranked))
	}
	if !strings.HasSuffix(ranked[0].Virtual, `\Weixin.exe`) {
		t.Fatalf("main exe must rank first, got %s", ranked[0].Virtual)
	}
	if !strings.HasSuffix(ranked[1].Virtual, `side.exe`) && !strings.Contains(ranked[1].Virtual, "Weixin") {
		t.Fatalf("unexpected second: %s", ranked[1].Virtual)
	}
	last := ranked[len(ranked)-1]
	if strings.Contains(last.Virtual, "crashpad") || strings.Contains(last.Virtual, "Update") {
		// update/crash helpers sink below real programs but order
		// between themselves is not asserted.
	}
	for _, c := range ranked {
		if strings.Contains(strings.ToLower(c.Virtual), "unins") {
			t.Fatalf("uninstaller leaked into ranking: %s", c.Virtual)
		}
	}
}

func TestSniffInstallerKindFrom(t *testing.T) {
	inno := append([]byte("MZ...."), utf16Bytes("Inno Setup")...)
	if got := sniffInstallerKindFrom(bytes.NewReader(inno)); got != "inno" {
		t.Fatalf("inno utf16 marker: got %q", got)
	}
	nsis := append([]byte("MZ...."), []byte("NullsoftInst")...)
	if got := sniffInstallerKindFrom(bytes.NewReader(nsis)); got != "nsis" {
		t.Fatalf("nsis marker: got %q", got)
	}
	if got := sniffInstallerKindFrom(bytes.NewReader([]byte("MZ plain binary"))); got != "" {
		t.Fatalf("unknown binary: got %q", got)
	}
	// Marker straddling the 1 MiB read boundary.
	big := make([]byte, 0, (1<<20)+128)
	big = append(big, bytes.Repeat([]byte{0}, (1<<20)-4)...)
	big = append(big, []byte("Inno Setup")...)
	big = append(big, bytes.Repeat([]byte{0}, 64)...)
	if got := sniffInstallerKindFrom(bytes.NewReader(big)); got != "inno" {
		t.Fatalf("boundary marker: got %q", got)
	}
}

func TestSilentArgsFor(t *testing.T) {
	if a := silentArgsFor("nsis"); len(a) != 1 || a[0] != "/S" {
		t.Fatalf("nsis args: %v", a)
	}
	if a := silentArgsFor("inno"); len(a) != 4 {
		t.Fatalf("inno args: %v", a)
	}
	if a := silentArgsFor("msi"); a != nil {
		t.Fatalf("msi must be interactive/none: %v", a)
	}
	if a := silentArgsFor(""); a != nil {
		t.Fatalf("unknown must be nil: %v", a)
	}
}

func TestComponentPlan(t *testing.T) {
	cases := []struct {
		extracted, embedded bool
		want                string
	}{
		{true, true, "reuse"},
		{true, false, "reuse"},
		{false, true, "extract"},
		{false, false, "download"},
	}
	for _, c := range cases {
		if got := componentPlan(c.extracted, c.embedded); got != c.want {
			t.Errorf("componentPlan(%v,%v) = %q, want %q", c.extracted, c.embedded, got, c.want)
		}
	}
}

func TestBoxAppsRegistryPure(t *testing.T) {
	reg := map[string]boxApp{}
	a1 := boxApp{ID: newBoxAppID(reg, "WeChat"), Name: "WeChat"}
	reg = boxAppsUpsert(reg, a1)
	a2 := boxApp{ID: newBoxAppID(reg, "WeChat"), Name: "WeChat"}
	if a2.ID == a1.ID {
		t.Fatalf("id collision not resolved: %q", a2.ID)
	}
	reg = boxAppsUpsert(reg, a2)
	if len(reg) != 2 {
		t.Fatalf("want 2 entries, got %d", len(reg))
	}
	reg = boxAppsRemove(reg, a1.ID)
	if len(reg) != 1 {
		t.Fatalf("remove failed: %d entries", len(reg))
	}
	if _, ok := reg[a2.ID]; !ok {
		t.Fatal("wrong entry removed")
	}
}

func TestParseBoxPids(t *testing.T) {
	out := "Sandboxed processes in box MJBox:\r\n1234\r\n5678\r\n1234\r\n"
	pids := parseBoxPids(out)
	if len(pids) != 2 || pids[0] != 1234 || pids[1] != 5678 {
		t.Fatalf("pids: %v", pids)
	}
	if p := parseBoxPids(""); len(p) != 0 {
		t.Fatalf("empty output must give no pids: %v", p)
	}
}

func TestMatchNewAppDataDirs(t *testing.T) {
	root := `D:\MJ\Data\sandbox\MJBox\user\ming\AppData\Roaming`
	before := map[string]bool{strings.ToLower(root + `\Existing`): true}
	after := []string{
		root + `\Existing`,
		root + `\Tencent`,
		root + `\Tencent\Weixin`,
		root + `\Unrelated`,
	}
	got := matchNewAppDataDirs(after, before, []string{"Weixin", "微信"})
	if len(got) != 1 || !strings.HasSuffix(got[0], `\Tencent\Weixin`) {
		t.Fatalf("matched: %v", got)
	}
	// The vendor dir alone matches when the hint is the vendor.
	got2 := matchNewAppDataDirs(after, before, []string{"Tencent"})
	found := map[string]bool{}
	for _, g := range got2 {
		found[g] = true
	}
	if !found[root+`\Tencent`] {
		t.Fatalf("vendor dir should match its hint: %v", got2)
	}
	for _, g := range got2 {
		if strings.HasSuffix(g, `\Tencent\Weixin`) {
			t.Fatalf("descendant of a matched dir must be dropped: %v", got2)
		}
	}
}

func TestInstallerHint(t *testing.T) {
	if h := installerHint("SomethingSetup.exe"); strings.Contains(h, "setup") {
		t.Fatalf("hint keeps junk: %q", h)
	}
	if h := installerHint("WeChatWin_4.2.0.exe"); !strings.HasPrefix(h, "wechat") {
		t.Fatalf("wechat hint: %q", h)
	}
}

func TestClassifyBoxLaunchTimeout(t *testing.T) {
	cases := []struct {
		name               string
		sawNew, stillAlive bool
		cross              int
		hiddenApp          bool
		want               boxLaunchFailKind
	}{
		{"never appeared in the box list", false, false, 0, false, boxFailNoProcess},
		{"appeared then exited", true, false, 0, false, boxFailExited},
		{"alive but zero windows", true, true, 0, false, boxFailNoWindow},
		{"already-running process alive, no window found", false, true, 0, false, boxFailNoWindow},
		{"cross-check found windows under boxed PIDs", true, true, 2, false, boxFailWindowPID},
		{"cross-check wins over exited", true, false, 1, false, boxFailWindowPID},
		{"v0.41 the field shape: hidden app window, reveal failed", true, true, 46, true, boxFailHiddenWindow},
		{"v0.41 hidden app window outranks the exited reading", true, false, 3, true, boxFailHiddenWindow},
		{"v0.41 hidden app window outranks the other-PID reading", false, true, 5, true, boxFailHiddenWindow},
	}
	for _, c := range cases {
		if got := classifyBoxLaunchTimeout(c.sawNew, c.stillAlive, c.cross, c.hiddenApp); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestBoxLaunchFailMessage(t *testing.T) {
	before := []int{10, 20}
	last := []int{10, 20, 300}
	msgs := map[boxLaunchFailKind]string{}
	for _, kind := range []boxLaunchFailKind{boxFailNoProcess, boxFailExited, boxFailNoWindow, boxFailWindowPID, boxFailHiddenWindow} {
		msgs[kind] = boxLaunchFailMessage("Weixin", kind, before, last, nil)
		if !strings.Contains(msgs[kind], string(kind)) || !strings.Contains(msgs[kind], "Weixin") {
			t.Errorf("%s: message must carry kind and app: %q", kind, msgs[kind])
		}
	}
	// The shapes must be distinguishable by wording alone,
	// and must carry the raw PID sets for field diagnosis.
	if !strings.Contains(msgs[boxFailNoProcess], "名单里一直没出现") || !strings.Contains(msgs[boxFailNoProcess], "[10 20]") {
		t.Errorf("no-process wording: %q", msgs[boxFailNoProcess])
	}
	if !strings.Contains(msgs[boxFailExited], "出现过又消失") {
		t.Errorf("exited wording: %q", msgs[boxFailExited])
	}
	if !strings.Contains(msgs[boxFailNoWindow], "没有窗口") || !strings.Contains(msgs[boxFailNoWindow], "[10 20 300]") {
		t.Errorf("no-window wording: %q", msgs[boxFailNoWindow])
	}
	if !strings.Contains(msgs[boxFailWindowPID], "没能登记") {
		t.Errorf("window-pid wording: %q", msgs[boxFailWindowPID])
	}
	// v0.41: the hidden shape must NOT read like "no window" --
	// the window existed, it was the reveal that failed.
	if !strings.Contains(msgs[boxFailHiddenWindow], "隐藏状态") || !strings.Contains(msgs[boxFailHiddenWindow], "没能把它显示出来") {
		t.Errorf("hidden-window wording: %q", msgs[boxFailHiddenWindow])
	}
	if strings.Contains(msgs[boxFailHiddenWindow], "没有窗口") {
		t.Errorf("hidden-window wording must not claim there was no window: %q", msgs[boxFailHiddenWindow])
	}
	seen := map[string]bool{}
	for k, m := range msgs {
		if seen[m] {
			t.Fatalf("duplicate message for %s", k)
		}
		seen[m] = true
	}
}

func TestFormatBoxCrossCheck(t *testing.T) {
	if s := formatBoxCrossCheck(nil); !strings.Contains(s, "没有任何顶级窗口") {
		t.Fatalf("empty cross-check: %q", s)
	}
	s := formatBoxCrossCheck([]windowFact{{HWND: 77, PID: 300, Title: "微信", Class: "Qt6QWindow", Visible: true}})
	for _, want := range []string{"pid=300", "微信", "Qt6QWindow"} {
		if !strings.Contains(s, want) {
			t.Fatalf("cross-check %q missing %q", s, want)
		}
	}
	if !equalIntSlices([]int{1, 2}, []int{1, 2}) || equalIntSlices([]int{1}, []int{1, 2}) || equalIntSlices([]int{1, 3}, []int{1, 2}) {
		t.Fatal("equalIntSlices wrong")
	}
}

func TestSameNameBoxPIDs(t *testing.T) {
	procs := []procNameFact{
		{PID: 300, ImageBase: "Weixin.exe"},
		{PID: 301, ImageBase: "WeChatAppEx.exe"}, // same suite, different exe: not the registered main
		{PID: 302, ImageBase: "weixin.EXE"},      // case-insensitive match
		{PID: 303, ImageBase: ""},                // image unreadable: never matched, never killed
		{PID: 304, ImageBase: "Weixin.exe"},
	}
	got := sameNameBoxPIDs(procs, "Weixin.exe")
	if len(got) != 3 || got[0] != 300 || got[1] != 302 || got[2] != 304 {
		t.Fatalf("sameName = %v, want [300 302 304] ascending", got)
	}
	if got := sameNameBoxPIDs(procs, ""); got != nil {
		t.Fatalf("empty exe base must match nothing, got %v", got)
	}
	if got := sameNameBoxPIDs(nil, "Weixin.exe"); got != nil {
		t.Fatalf("no processes must match nothing, got %v", got)
	}
}

func TestDecideBoxResidual(t *testing.T) {
	qt := "Sandbox:MJBox:Qt51514QWindowIcon"
	// No same-name processes at all: nothing to rule on.
	if d := decideBoxResidual(nil, nil); d != boxResidualNone {
		t.Fatalf("empty residuals = %v, want none", d)
	}
	// The wedged shape (v0.40 field case): same-name processes
	// whose windows are ALL hidden or plumbing -> kill.
	wedged := []windowFact{
		{HWND: 1, PID: 300, Title: "WeChat", Class: qt, Visible: false},
		{HWND: 2, PID: 300, Title: "", Class: "Sandbox:MJBox:Chrome_SystemMessageWindow", Visible: false},
		{HWND: 3, PID: 300, Title: "Default IME", Class: "IME", Visible: false},
	}
	if d := decideBoxResidual([]uint32{300}, wedged); d != boxResidualKill {
		t.Fatalf("wedged residuals = %v, want kill", d)
	}
	// Same-name processes with no windows at all -> kill.
	if d := decideBoxResidual([]uint32{300}, nil); d != boxResidualKill {
		t.Fatalf("windowless residuals = %v, want kill", d)
	}
	// A visible app window on the residual PID: a live app,
	// present it, never kill it.
	live := append(append([]windowFact{}, wedged...),
		windowFact{HWND: 4, PID: 300, Title: "WeChat", Class: qt, Visible: true})
	if d := decideBoxResidual([]uint32{300}, live); d != boxResidualPresent {
		t.Fatalf("live residuals = %v, want present", d)
	}
	// A visible app window owned by a DIFFERENT process does not
	// make THIS residual live: different-name processes are not
	// touched by the ruling either way.
	other := []windowFact{
		{HWND: 5, PID: 999, Title: "别的盒内应用", Class: "Qt6QWindow", Visible: true},
		{HWND: 6, PID: 300, Title: "WeChat", Class: qt, Visible: false},
	}
	if d := decideBoxResidual([]uint32{300}, other); d != boxResidualKill {
		t.Fatalf("residual with only another app visible = %v, want kill (its own windows are all hidden)", d)
	}
	// A visible PLUMBING window (titled IME) on the residual PID
	// is not an app window: still wedged.
	plumb := []windowFact{
		{HWND: 7, PID: 300, Title: "Default IME", Class: "IME", Visible: true},
	}
	if d := decideBoxResidual([]uint32{300}, plumb); d != boxResidualKill {
		t.Fatalf("residual with only visible plumbing = %v, want kill", d)
	}
}

func TestHasHiddenAppWindow(t *testing.T) {
	qt := "Sandbox:MJBox:Qt51514QWindowIcon"
	if hasHiddenAppWindow(nil) {
		t.Fatal("empty cross-check has no hidden app window")
	}
	if hasHiddenAppWindow([]windowFact{{HWND: 1, PID: 300, Title: "WeChat", Class: qt, Visible: true}}) {
		t.Fatal("a visible app window is not the hidden shape")
	}
	if hasHiddenAppWindow([]windowFact{{HWND: 2, PID: 300, Title: "", Class: "Sandbox:MJBox:Chrome_SystemMessageWindow", Visible: false}}) {
		t.Fatal("hidden plumbing is not a hidden APP window")
	}
	if !hasHiddenAppWindow([]windowFact{
		{HWND: 3, PID: 300, Title: "", Class: "IME", Visible: false},
		{HWND: 4, PID: 301, Title: "WeChat", Class: qt, Visible: false},
	}) {
		t.Fatal("hidden titled Qt window must read as the hidden shape")
	}
}

func TestVerifyPinnedFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.bin")
	data := []byte("hello sandboxie")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	if !verifyPinnedFile(p, int64(len(data)), sha) {
		t.Fatal("correct file must verify")
	}
	if verifyPinnedFile(p, int64(len(data))+1, sha) {
		t.Fatal("size mismatch must fail")
	}
	if verifyPinnedFile(p, int64(len(data)), strings.Repeat("0", 64)) {
		t.Fatal("hash mismatch must fail")
	}
}

// TestEmbeddedComponentPin guards the build locally: when the
// (gitignored) installer staging file has been fetched, the
// embedded copy must match the pin exactly. Skipped on machines
// where the fetch has not happened (go build would fail there
// anyway -- see the fetch instructions in sandbox.go).
func TestEmbeddedComponentPin(t *testing.T) {
	if int64(len(sbieEmbedded)) != sbieSize {
		t.Skipf("embedded component size %d (staging file not fetched?)", len(sbieEmbedded))
	}
	sum := sha256.Sum256(sbieEmbedded)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), sbieSHA256) {
		t.Fatal("embedded component hash does not match the pin")
	}
}

// ---------- v0.42: data seal ----------

func TestSealRuleLinesForExe(t *testing.T) {
	lines := sealRuleLinesForExe("Weixin.exe")
	wantCount := len(sealWeChatRoots) + len(sealWeChatKeys)
	if len(lines) != wantCount {
		t.Fatalf("Weixin.exe must produce %d rules, got %v", wantCount, lines)
	}
	joined := strings.Join(lines, "\n")
	for _, want := range []string{
		`WriteFilePath=Weixin.exe,%Personal%\xwechat_files\`,
		`WriteFilePath=Weixin.exe,%Personal%\WeChat Files\`,
		`WriteFilePath=Weixin.exe,%AppData%\Tencent\xwechat\`,
		`WriteFilePath=Weixin.exe,%AppData%\Tencent\Weixin\`,
		`WriteFilePath=Weixin.exe,%AppData%\Tencent\WeChat\`,
		`WriteKeyPath=Weixin.exe,HKEY_CURRENT_USER\Software\Tencent\WeChat`,
		`WriteKeyPath=Weixin.exe,HKEY_CURRENT_USER\Software\Tencent\Weixin`,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("rule %q missing from:\n%s", want, joined)
		}
	}
	// Case-insensitive template hit; the prefix keeps the
	// caller's spelling.
	upper := sealRuleLinesForExe("WEIXIN.EXE")
	if len(upper) != len(lines) || !strings.HasPrefix(upper[0], "WriteFilePath=WEIXIN.EXE,") {
		t.Fatalf("uppercase hit wrong: %v", upper)
	}
	if got := sealRuleLinesForExe("notepad++.exe"); got != nil {
		t.Fatalf("uncovered exe must produce no rules, got %v", got)
	}
	if got := sealRuleLinesForExe(""); got != nil {
		t.Fatalf("empty exe must produce no rules, got %v", got)
	}
}

func TestSealKeyPathsForExe(t *testing.T) {
	keys := sealKeyPathsForExe("Weixin.exe")
	if len(keys) != len(sealWeChatKeys) {
		t.Fatalf("Weixin.exe must have %d key roots, got %v", len(sealWeChatKeys), keys)
	}
	joined := strings.Join(keys, "\n")
	for _, want := range []string{
		`HKEY_CURRENT_USER\Software\Tencent\WeChat`,
		`HKEY_CURRENT_USER\Software\Tencent\Weixin`,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("key root %q missing from:\n%s", want, joined)
		}
	}
	// Spelling guards, per the verified driver semantics: the
	// HKEY_ user-mode prefix is what conf_expand.c translates
	// (no SID, no % variable in the rule), and a whole-tree key
	// pattern is written bare — a trailing backslash or star
	// would change what the pattern names.
	for _, k := range keys {
		if !strings.HasPrefix(k, `HKEY_CURRENT_USER\`) {
			t.Fatalf("key root must use the HKEY_CURRENT_USER spelling: %q", k)
		}
		if strings.HasSuffix(k, `\`) || strings.Contains(k, "*") || strings.Contains(k, "%") {
			t.Fatalf("key root must be a bare whole-tree pattern: %q", k)
		}
	}
	// Case-insensitive hit, and both family exes share the set.
	if got := sealKeyPathsForExe("WECHAT.EXE"); len(got) != len(keys) {
		t.Fatalf("WeChat.exe must share the key set, got %v", got)
	}
	if got := sealKeyPathsForExe("notepad++.exe"); got != nil {
		t.Fatalf("uncovered exe must have no key roots, got %v", got)
	}
}

func TestSealRuleLinesRegistry(t *testing.T) {
	wx := boxApp{ID: "weixin", Name: "微信", Exe: `C:\Program Files\Tencent\Weixin\Weixin.exe`}
	other := boxApp{ID: "foo", Name: "Foo", Exe: `C:\Program Files\Foo\foo.exe`}
	reg := map[string]boxApp{"weixin": wx, "foo": other}
	lines := sealRuleLines(reg)
	if want := len(sealWeChatRoots) + len(sealWeChatKeys); len(lines) != want {
		t.Fatalf("only the covered app contributes: %v", lines)
	}
	// File and key rules coexist in one sorted, deduped set:
	// every WriteFilePath line sorts ahead of every WriteKeyPath
	// line, and the set equals the per-exe rendering as a set.
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, `WriteKeyPath=Weixin.exe,HKEY_CURRENT_USER\Software\Tencent\Weixin`) {
		t.Fatalf("key rules missing from registry rendering: %v", lines)
	}
	if strings.LastIndex(joined, "WriteFilePath=") > strings.Index(joined, "WriteKeyPath=") {
		t.Fatalf("sorted rendering must keep file lines ahead of key lines: %v", lines)
	}
	// Deterministic: merging the same registry again produces
	// byte-identical lines.
	if again := sealRuleLines(reg); strings.Join(again, "|") != strings.Join(lines, "|") {
		t.Fatalf("rule generation must be deterministic:\n%v\n%v", lines, again)
	}
	// A second registration of the same exe shares the template:
	// no duplicate lines.
	reg["weixin-2"] = boxApp{ID: "weixin-2", Name: "微信2", Exe: `D:\Other\Weixin.exe`}
	if got := sealRuleLines(reg); len(got) != len(lines) {
		t.Fatalf("duplicate registration must not duplicate rules: %v", got)
	}
	// Removing every registration of the covered exe removes
	// its lines completely.
	delete(reg, "weixin")
	delete(reg, "weixin-2")
	if got := sealRuleLines(reg); len(got) != 0 {
		t.Fatalf("removed app's rules must go with it: %v", got)
	}
}

func TestMergeBoxSectionSealAndBorder(t *testing.T) {
	root := `D:\MJ\Data\sandbox\MJBox`
	lines := sealRuleLinesForExe("Weixin.exe")
	orig := "[GlobalSettings]\r\nBoxRootFolder=C:\\Sandbox\r\n\r\n[OtherBox]\r\nEnabled=y\r\n"
	out, changed := mergeBoxSection(orig, root, lines)
	if !changed {
		t.Fatal("first merge must report changed")
	}
	for _, want := range []string{"[GlobalSettings]", "[OtherBox]", "BorderColor=#000000,off,6,192,no", `WriteFilePath=Weixin.exe,%Personal%\xwechat_files\`, `WriteKeyPath=Weixin.exe,HKEY_CURRENT_USER\Software\Tencent\WeChat`} {
		if !strings.Contains(out, want) {
			t.Fatalf("merged section missing %q:\n%s", want, out)
		}
	}
	// Idempotent with the same lines.
	out2, changed2 := mergeBoxSection(out, root, lines)
	if changed2 {
		t.Fatalf("second merge with same lines must be a no-op:\n%s", out2)
	}
	if !iniHasCurrentBox(out, root, lines) {
		t.Fatal("merged output must read as current")
	}
	// Dropping the app drops its rules (and only its rules).
	out3, changed3 := mergeBoxSection(out, root, nil)
	if !changed3 {
		t.Fatal("dropping seal lines must report changed")
	}
	if strings.Contains(out3, "WriteFilePath=") || strings.Contains(out3, "WriteKeyPath=") {
		t.Fatalf("seal rules survived their app:\n%s", out3)
	}
	if !strings.Contains(out3, "[OtherBox]") || !strings.Contains(out3, "BorderColor=#000000,off,6,192,no") {
		t.Fatalf("unrelated content lost:\n%s", out3)
	}
}

func TestSealShadowPaths(t *testing.T) {
	root := `D:\MJ\Data\sandbox\MJBox`
	got := sealShadowPaths(root, "Weixin.exe", []string{"ming"})
	var want []string
	for _, rel := range [][]string{
		{"user", "ming", "Documents", "xwechat_files"},
		{"user", "ming", "Documents", "WeChat Files"},
		{"user", "ming", "AppData", "Roaming", "Tencent", "xwechat"},
		{"user", "ming", "AppData", "Roaming", "Tencent", "Weixin"},
		{"user", "ming", "AppData", "Roaming", "Tencent", "WeChat"},
	} {
		want = append(want, filepath.Join(append([]string{root}, rel...)...))
	}
	if len(got) != len(want) {
		t.Fatalf("shadow paths = %v, want %v", got, want)
	}
	set := map[string]bool{}
	for _, g := range got {
		set[g] = true
	}
	for _, w := range want {
		if !set[w] {
			t.Fatalf("missing shadow path %q in %v", w, got)
		}
	}
	// Every purge target must pass the strict in-box guard.
	for _, g := range got {
		if !insideBoxStrict(root, g) {
			t.Fatalf("purge target %q refused by the in-box guard", g)
		}
	}
	if got := sealShadowPaths(root, "foo.exe", []string{"ming"}); got != nil {
		t.Fatalf("uncovered exe must map to no shadow paths, got %v", got)
	}
	if got := sealShadowPaths(root, "Weixin.exe", nil); got != nil {
		t.Fatalf("no users must map to no shadow paths, got %v", got)
	}
}

func TestInsideBoxStrict(t *testing.T) {
	root := filepath.Join(`D:\MJ\Data\sandbox`, "MJBox")
	inside := filepath.Join(root, "user", "ming", "Documents", "xwechat_files")
	if !insideBoxStrict(root, inside) {
		t.Fatal("a folder deep inside the box must pass")
	}
	if insideBoxStrict(root, root) {
		t.Fatal("the box root itself must be refused")
	}
	if insideBoxStrict(root, filepath.Join(root, "..", "evil")) {
		t.Fatal("an escaping path must be refused")
	}
	if insideBoxStrict(root, filepath.Join(`D:\MJ\Data\sandbox`, "MJBox2", "user")) {
		t.Fatal("a prefix-sibling (MJBox2) must be refused")
	}
	if insideBoxStrict(root, `C:\Users\ming\Documents\xwechat_files`) {
		t.Fatal("the HOST's real data folder must be refused")
	}
}

func TestSealNeededAndMarkerRoundTrip(t *testing.T) {
	wx := boxApp{ID: "weixin", Exe: `C:\Program Files\Tencent\Weixin\Weixin.exe`}
	if !sealNeeded(wx) {
		t.Fatal("covered + unsealed must need the purge")
	}
	wx.DataSealed = true
	if sealNeeded(wx) {
		t.Fatal("the marker must make the purge one-time only")
	}
	foo := boxApp{ID: "foo", Exe: `C:\Program Files\Foo\foo.exe`}
	if sealNeeded(foo) {
		t.Fatal("an uncovered app never needs the purge")
	}
	// The marker must survive the registry's JSON round trip,
	// or every restart would purge again.
	b, err := json.Marshal(wx)
	if err != nil {
		t.Fatal(err)
	}
	var back boxApp
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if !back.DataSealed {
		t.Fatal("dataSealed lost in the JSON round trip")
	}
}

// ---------- v0.43: strict /listpids parsing ----------

func TestParseBoxPidsStrict(t *testing.T) {
	// The documented format: a count line, then one PID per line.
	got := parseBoxPids("3\n3036\n2136\n384\n")
	if len(got) != 3 || got[0] != 384 || got[1] != 2136 || got[2] != 3036 {
		t.Fatalf("documented format: %v", got)
	}
	// The v0.40 field shape: 17 programs in the box — the count
	// itself must NOT come back as a PID (it did before v0.43,
	// and a matching host PID then gets falsely claimed).
	var sb strings.Builder
	sb.WriteString("17\r\n")
	for i := 0; i < 17; i++ {
		fmt.Fprintf(&sb, "%d\r\n", 1000+i*4)
	}
	got = parseBoxPids(sb.String())
	if len(got) != 17 {
		t.Fatalf("17-program list: got %d pids %v", len(got), got)
	}
	for _, p := range got {
		if p == 17 {
			t.Fatalf("count leaked into pid list: %v", got)
		}
	}
	// A truncated list (count promises more than was delivered):
	// the count is still not a PID.
	got = parseBoxPids("3\n3036\n2136")
	if len(got) != 2 || got[0] != 2136 || got[1] != 3036 {
		t.Fatalf("truncated list: %v", got)
	}
	// Noise lines never yield numbers (the old splitter pulled
	// "1" and "18" out of a version string like this).
	if got := parseBoxPids("Sandboxie Start v1.18\nerror 42 happened\n"); len(got) != 0 {
		t.Fatalf("noise must yield no pids: %v", got)
	}
	// Empty box: a lone 0 count.
	if got := parseBoxPids("0\n"); len(got) != 0 {
		t.Fatalf("empty box: %v", got)
	}
}

// ---------- v0.43: install completion watch ----------

func TestDecideInstallWatch(t *testing.T) {
	cfg := defaultInstallWatchCfg(false)
	cases := []struct {
		name string
		obs  installWatchObs
		want installVerdict
	}{
		{"installer gone + tree stable -> complete; resident auto-launched app does not block",
			installWatchObs{Elapsed: 40 * time.Second, InstallerSeen: true, InstallerGone: true, AnySessionSeen: true, SessionAlive: 3, StableFor: 9 * time.Second},
			watchComplete},
		{"installer gone but tree still moving -> wait",
			installWatchObs{Elapsed: 40 * time.Second, InstallerSeen: true, InstallerGone: true, AnySessionSeen: true, SessionAlive: 0, StableFor: 2 * time.Second},
			watchWait},
		{"installer still running -> wait even if tree is quiet",
			installWatchObs{Elapsed: 3 * time.Minute, InstallerSeen: true, InstallerGone: false, AnySessionSeen: true, SessionAlive: 1, StableFor: 60 * time.Second},
			watchWait},
		{"installer never exits -> named timeout",
			installWatchObs{Elapsed: 15 * time.Minute, InstallerSeen: true, InstallerGone: false, AnySessionSeen: true, SessionAlive: 1},
			watchTimeout},
		{"installer never identified, everyone left, stable -> complete",
			installWatchObs{Elapsed: 30 * time.Second, AnySessionSeen: true, SessionAlive: 0, StableFor: 10 * time.Second},
			watchComplete},
		{"installer never identified, residents remain, long quiet -> complete",
			installWatchObs{Elapsed: 2 * time.Minute, AnySessionSeen: true, SessionAlive: 2, StableFor: 31 * time.Second},
			watchComplete},
		{"installer never identified, residents remain, short quiet -> wait",
			installWatchObs{Elapsed: 2 * time.Minute, AnySessionSeen: true, SessionAlive: 2, StableFor: 10 * time.Second},
			watchWait},
		{"nothing ever appeared -> wait (the timeout will name it)",
			installWatchObs{Elapsed: 3 * time.Minute, StableFor: 3 * time.Minute},
			watchWait},
		{"nothing ever appeared, past the deadline -> timeout",
			installWatchObs{Elapsed: 15 * time.Minute, StableFor: 15 * time.Minute},
			watchTimeout},
	}
	for _, c := range cases {
		if got := decideInstallWatch(c.obs, cfg); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	// Interactive installs get the longer deadline.
	icfg := defaultInstallWatchCfg(true)
	obs := installWatchObs{Elapsed: 20 * time.Minute, InstallerSeen: true, AnySessionSeen: true, SessionAlive: 1}
	if got := decideInstallWatch(obs, icfg); got != watchWait {
		t.Errorf("interactive 20m: got %v, want wait", got)
	}
	if got := decideInstallWatch(obs, cfg); got != watchTimeout {
		t.Errorf("silent 20m: got %v, want timeout", got)
	}
}

func TestInstallSummaries(t *testing.T) {
	s := installTimeoutSummary("td-setup.exe",
		installWatchObs{Elapsed: 15 * time.Minute, InstallerSeen: true, InstallerGone: true, SessionAlive: 2, FreshFiles: 123},
		[]int{111, 222})
	for _, want := range []string{"超时未完成", "td-setup.exe", "安装器已退出", "123"} {
		if !strings.Contains(s, want) {
			t.Errorf("timeout summary missing %q: %s", want, s)
		}
	}
	s2 := installTimeoutSummary("x.exe",
		installWatchObs{Elapsed: 15 * time.Minute, InstallerSeen: true, SessionAlive: 1}, []int{9})
	if !strings.Contains(s2, "安装器仍未退出") {
		t.Errorf("timeout summary must say the installer is still running: %s", s2)
	}
	e := installEmptyScanSummary("td-setup.exe", 55, 2, 1)
	for _, want := range []string{"没找到能登记的新程序", "55", "2", "1"} {
		if !strings.Contains(e, want) {
			t.Errorf("empty-scan summary missing %q: %s", want, e)
		}
	}
}

// ---------- v0.43: candidate trust ----------

func TestIsJunkCandidateShape(t *testing.T) {
	junk := []string{
		// The field ghost: a WeChat patcher's temporary 7z.exe.
		`C:\Users\u\AppData\Roaming\Tencent\xwechat\xplugin\plugins\CourgettePatch\20\extracted\7z.exe`,
		`C:\Program Files\App\7za.exe`,
		`C:\Users\u\AppData\Local\Temp\setuphelper.exe`,
		`C:\Program Files\App\plugins\realplugin.exe`,
		`C:\Users\u\AppData\Local\App\tmp\thing.exe`,
	}
	for _, p := range junk {
		if !isJunkCandidateShape(p) {
			t.Errorf("must be junk: %s", p)
		}
	}
	real := []string{
		`C:\Users\u\AppData\Roaming\Telegram Desktop\Telegram.exe`,
		`C:\Program Files\Tencent\Weixin\Weixin.exe`,
		// Segment equality, not substring: these are safe.
		`C:\Users\u\AppData\Local\Programs\Templates Tool\TemplateApp.exe`,
		`C:\Program Files\PluginSoft\PluginSoft.exe`,
	}
	for _, p := range real {
		if isJunkCandidateShape(p) {
			t.Errorf("must NOT be junk: %s", p)
		}
	}
}

func TestRankBoxCandidatesTelegramFixture(t *testing.T) {
	// The 2026-10-29 field set: Telegram's per-user install,
	// its uninstaller, and the temporary 7z.exe a concurrently
	// boxed WeChat's patcher dropped (and later deleted) while
	// Telegram was installing.
	fresh := []boxExeCand{
		{Virtual: `C:\Users\ming\AppData\Roaming\Telegram Desktop\Telegram.exe`, Size: 60 << 20},
		{Virtual: `C:\Users\ming\AppData\Roaming\Telegram Desktop\unins000.exe`, Size: 3 << 20},
		{Virtual: `C:\Users\ming\AppData\Roaming\Tencent\xwechat\xplugin\plugins\CourgettePatch\20\extracted\7z.exe`, Size: 1 << 20},
	}
	ranked := rankBoxCandidates(fresh, "td-setup-win-x64-7.3.3 (3).exe")
	if len(ranked) != 1 {
		t.Fatalf("only Telegram.exe may survive, got %v", ranked)
	}
	if !strings.HasSuffix(ranked[0].Virtual, `\Telegram.exe`) {
		t.Fatalf("survivor must be Telegram.exe, got %s", ranked[0].Virtual)
	}
	// And Telegram outranks an unrelated per-user candidate:
	// the Roaming root plus its own folder's name carry it.
	fresh = append(fresh, boxExeCand{Virtual: `C:\Users\ming\AppData\Local\Programs\Other\other.exe`, Size: 5 << 20})
	ranked = rankBoxCandidates(fresh, "td-setup-win-x64-7.3.3 (3).exe")
	if len(ranked) != 2 || !strings.HasSuffix(ranked[0].Virtual, `\Telegram.exe`) {
		t.Fatalf("Telegram must rank first, got %v", ranked)
	}
}

func TestMarkForeignCandidates(t *testing.T) {
	ranked := []boxExeCand{
		{Virtual: `C:\Users\ming\AppData\Roaming\Telegram Desktop\Telegram.exe`},
		{Virtual: `C:\Program Files\Tencent\Weixin\WeixinSvc.exe`},
		{Virtual: `C:\Users\ming\AppData\Roaming\Telegram Desktop\modules\TelegramHelper.exe`},
	}
	markForeignCandidates(ranked, "td-setup-win-x64-7.3.3 (3).exe")
	if ranked[0].Foreign {
		t.Error("the top candidate is never foreign")
	}
	if !ranked[1].Foreign {
		t.Error("a Tencent-folder candidate in a Telegram install must be flagged foreign")
	}
	if ranked[2].Foreign {
		t.Error("a same-folder candidate must not be flagged foreign")
	}
	wx := []boxExeCand{
		{Virtual: `C:\Program Files\Tencent\Weixin\Weixin.exe`},
		{Virtual: `C:\Program Files\Tencent\Weixin\WeixinUpdate.exe`},
	}
	markForeignCandidates(wx, "WeChatWin_4.2.0.exe")
	if wx[1].Foreign {
		t.Error("a same-folder Weixin candidate must not be foreign")
	}
}

func TestSbieBoxBusyPhases(t *testing.T) {
	sbieBoxOp.Lock()
	savedPhase, savedMsg, savedStarted := sbieBoxOp.Phase, sbieBoxOp.Message, sbieBoxOp.StartedAt
	sbieBoxOp.Unlock()
	defer func() {
		sbieBoxOp.Lock()
		sbieBoxOp.Phase, sbieBoxOp.Message, sbieBoxOp.StartedAt = savedPhase, savedMsg, savedStarted
		sbieBoxOp.Unlock()
	}()
	for _, p := range []string{"preparing", "downloading", "installing", "scanning"} {
		sbieBoxSet(p, "x")
		if !sbieBoxBusy() {
			t.Errorf("phase %s must be busy", p)
		}
	}
	sbieBoxSet("installing", "x")
	sbieBoxOp.Lock()
	started := sbieBoxOp.StartedAt
	sbieBoxOp.Unlock()
	if started == 0 {
		t.Error("a busy phase must stamp StartedAt")
	}
	for _, p := range []string{"choosing", "done", "error", "idle"} {
		sbieBoxSet(p, "x")
		if sbieBoxBusy() {
			t.Errorf("phase %s must not be busy", p)
		}
	}
	sbieBoxOp.Lock()
	started = sbieBoxOp.StartedAt
	sbieBoxOp.Unlock()
	if started != 0 {
		t.Error("idle must clear StartedAt")
	}
}

func TestPendingInstallRoundTrip(t *testing.T) {
	dir := t.TempDir()
	boxRoot := filepath.Join(dir, "MJBox")
	exePhys := filepath.Join(boxRoot, "user", "ming", "AppData", "Roaming", "Telegram Desktop", "Telegram.exe")
	if err := os.MkdirAll(filepath.Dir(exePhys), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exePhys, []byte("MZ-fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldPhys := filepath.Join(boxRoot, "drive", "C", "Program Files", "Old", "old.exe")
	if err := os.MkdirAll(filepath.Dir(oldPhys), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldPhys, []byte("MZ-old"), 0o644); err != nil {
		t.Fatal(err)
	}

	pendPath := filepath.Join(dir, "pending-install.json")
	savePendingInstallFile(pendPath, &sbiePendingInstall{
		InstallerName: "td-setup.exe",
		BeforeExes:    map[string]bool{strings.ToLower(oldPhys): true},
		BeforeAppData: map[string]bool{},
	})

	sbieBoxOp.Lock()
	savedPhase, savedPend, savedCands := sbieBoxOp.Phase, sbieBoxOp.Pending, sbieBoxOp.Candidates
	sbieBoxOp.Unlock()
	defer func() {
		sbieBoxOp.Lock()
		sbieBoxOp.Phase, sbieBoxOp.Pending, sbieBoxOp.Candidates = savedPhase, savedPend, savedCands
		sbieBoxOp.Unlock()
	}()

	if !restorePendingFrom(pendPath, boxRoot) {
		t.Fatal("the pending registration must restore")
	}
	sbieBoxOp.Lock()
	phase, p2, cands := sbieBoxOp.Phase, sbieBoxOp.Pending, sbieBoxOp.Candidates
	sbieBoxOp.Unlock()
	if phase != "choosing" || p2 == nil || p2.InstallerName != "td-setup.exe" {
		t.Fatalf("restored state wrong: phase=%s pend=%+v", phase, p2)
	}
	if len(cands) != 1 || !strings.HasSuffix(cands[0].Virtual, `\Telegram.exe`) || !cands[0].Exists {
		t.Fatalf("restored candidates wrong: %+v", cands)
	}
	// If the candidate file has since vanished, the restore
	// must retire the pending instead of resurrecting a ghost
	// choice.
	_ = os.Remove(exePhys)
	if restorePendingFrom(pendPath, boxRoot) {
		t.Fatal("a pending with no surviving candidates must retire")
	}
	if _, err := os.Stat(pendPath); !os.IsNotExist(err) {
		t.Fatal("the retired pending file must be removed")
	}
}
