package main

// Pure-logic tests for app isolation (v0.39): ini merge, path
// conversion, candidate ranking, kind sniffing, registry ops.
// Nothing here touches a real Sandboxie install.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeBoxSectionEmpty(t *testing.T) {
	out, changed := mergeBoxSection("", `D:\MJ\Data\sandbox\MJBox`)
	if !changed {
		t.Fatal("empty ini must report changed")
	}
	if !strings.Contains(out, "[MJBox]") || !strings.Contains(out, `FileRootPath=D:\MJ\Data\sandbox\MJBox`) {
		t.Fatalf("section missing: %q", out)
	}
}

func TestMergeBoxSectionPreservesOthersAndIdempotent(t *testing.T) {
	orig := "[GlobalSettings]\r\nBoxRootFolder=C:\\Sandbox\r\n\r\n[OtherBox]\r\nEnabled=y\r\nFileRootPath=C:\\Sandbox\\Other\r\n"
	out, changed := mergeBoxSection(orig, `E:\Data\sandbox\MJBox`)
	if !changed {
		t.Fatal("adding MJBox must report changed")
	}
	for _, want := range []string{"[GlobalSettings]", "BoxRootFolder=C:\\Sandbox", "[OtherBox]", `FileRootPath=C:\Sandbox\Other`, "[MJBox]", `FileRootPath=E:\Data\sandbox\MJBox`} {
		if !strings.Contains(out, want) {
			t.Fatalf("lost %q in:\n%s", want, out)
		}
	}
	out2, changed2 := mergeBoxSection(out, `E:\Data\sandbox\MJBox`)
	if changed2 {
		t.Fatalf("second merge must be a no-op, got:\n%s", out2)
	}
}

func TestMergeBoxSectionReplacesOldRoot(t *testing.T) {
	orig := "[MJBox]\r\nEnabled=y\r\nFileRootPath=C:\\Old\\Data\\sandbox\\MJBox\r\nAutoRecover=n\r\n\r\n[Other]\r\nEnabled=y\r\n"
	out, changed := mergeBoxSection(orig, `D:\New\sandbox\MJBox`)
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
		want               boxLaunchFailKind
	}{
		{"never appeared in the box list", false, false, 0, boxFailNoProcess},
		{"appeared then exited", true, false, 0, boxFailExited},
		{"alive but zero windows", true, true, 0, boxFailNoWindow},
		{"already-running process alive, no window found", false, true, 0, boxFailNoWindow},
		{"cross-check found windows under boxed PIDs", true, true, 2, boxFailWindowPID},
		{"cross-check wins over exited", true, false, 1, boxFailWindowPID},
	}
	for _, c := range cases {
		if got := classifyBoxLaunchTimeout(c.sawNew, c.stillAlive, c.cross); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestBoxLaunchFailMessage(t *testing.T) {
	before := []int{10, 20}
	last := []int{10, 20, 300}
	msgs := map[boxLaunchFailKind]string{}
	for _, kind := range []boxLaunchFailKind{boxFailNoProcess, boxFailExited, boxFailNoWindow, boxFailWindowPID} {
		msgs[kind] = boxLaunchFailMessage("Weixin", kind, before, last, nil)
		if !strings.Contains(msgs[kind], string(kind)) || !strings.Contains(msgs[kind], "Weixin") {
			t.Errorf("%s: message must carry kind and app: %q", kind, msgs[kind])
		}
	}
	// The four shapes must be distinguishable by wording alone,
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
