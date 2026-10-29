package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// ---------- registry sanity ----------

func TestProdRegistryShape(t *testing.T) {
	seen := map[string]bool{}
	manSeen := map[string]bool{}
	for _, c := range prodComponents {
		if c.ID == "" || c.Name == "" || c.Manifest == "" || c.ExeRel == "" {
			t.Fatalf("组件条目不完整：%+v", c)
		}
		if seen[c.ID] {
			t.Fatalf("组件 ID 重复：%s", c.ID)
		}
		seen[c.ID] = true
		if manSeen[c.Manifest] {
			t.Fatalf("清单名重复：%s", c.Manifest)
		}
		manSeen[c.Manifest] = true
		if !strings.HasSuffix(c.Manifest, ".json") {
			t.Fatalf("清单名应以 .json 结尾：%s", c.Manifest)
		}
	}
	for _, id := range []string{"onlyoffice", "libreoffice", "imageglass", "krita", "vscodium"} {
		if !seen[id] {
			t.Fatalf("缺少组件 %s", id)
		}
	}
	// The two office suites must exist as components: the router
	// and the defaultOfficeSuite setting both key off these IDs.
	for id := range officeSuites {
		if !seen[id] {
			t.Fatalf("默认办公套件 %s 不在组件表里", id)
		}
	}
}

// ---------- manifest ----------

func TestCompManifestFetchAndVerifyShape(t *testing.T) {
	good := compManifest{Version: "1.2.3", File: "MJ-Test.zip", Size: 42, SHA256: "ab"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/broken.json") {
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "x"})
			return
		}
		_ = json.NewEncoder(w).Encode(good)
	}))
	defer srv.Close()
	t.Setenv("MJ_COMP_BASE", srv.URL)

	c := prodComponent{ID: "test", Manifest: "test.json"}
	m, err := fetchCompManifest(context.Background(), c)
	if err != nil {
		t.Fatalf("fetchCompManifest: %v", err)
	}
	if m.Version != "1.2.3" || m.File != "MJ-Test.zip" || m.Size != 42 || m.SHA256 != "ab" {
		t.Fatalf("清单解析不对：%+v", m)
	}
	c2 := prodComponent{ID: "test", Manifest: "broken.json"}
	if _, err := fetchCompManifest(context.Background(), c2); err == nil {
		t.Fatal("不完整的清单应该报错")
	}
	c3 := prodComponent{ID: "test", Manifest: "missing.json"}
	srv2 := httptest.NewServer(http.NotFoundHandler())
	defer srv2.Close()
	t.Setenv("MJ_COMP_BASE", srv2.URL)
	if _, err := fetchCompManifest(context.Background(), c3); err == nil {
		t.Fatal("404 的清单应该报错")
	}
}

// ---------- install state machine ----------

func TestCompStateMachine(t *testing.T) {
	c := prodComponent{ID: "statetest", Name: "状态测试"}
	st := compState("statetest")
	if st.busy() {
		t.Fatal("初始状态不应 busy")
	}
	st.set(c, "downloading", "下载中")
	if !st.busy() {
		t.Fatal("downloading 应为 busy")
	}
	st.mu.Lock()
	st.Progress, st.Size = 50, 100
	st.mu.Unlock()
	phase, prog, size, _ := st.snapshot()
	if phase != "downloading" || prog != 50 || size != 100 {
		t.Fatalf("快照不对：%s %d %d", phase, prog, size)
	}
	st.set(c, "extracting", "释放中")
	if !st.busy() {
		t.Fatal("extracting 应为 busy")
	}
	st.set(c, "idle", "")
	if st.busy() {
		t.Fatal("idle 不应 busy")
	}
	// Distinct components keep distinct states.
	if compState("statetest") == compState("othercomp") {
		t.Fatal("不同组件的状态对象不能共用")
	}
}

// ---------- open routing (pure) ----------

func TestRouteFileOpen(t *testing.T) {
	all := map[string]bool{"onlyoffice": true, "libreoffice": true, "imageglass": true, "krita": true, "vscodium": true}
	none := map[string]bool{}
	cases := []struct {
		name     string
		kind     string
		ext      string
		inst     map[string]bool
		def      string
		wantComp string
		wantNeed string
	}{
		{"docx 双装 默认OO", "other", ".docx", all, "onlyoffice", "onlyoffice", ""},
		{"docx 双装 默认LO", "other", ".docx", all, "libreoffice", "libreoffice", ""},
		{"docx 只装LO 默认OO 回退", "other", ".docx", map[string]bool{"libreoffice": true}, "onlyoffice", "libreoffice", ""},
		{"docx 只装OO 默认LO 回退", "other", ".docx", map[string]bool{"onlyoffice": true}, "libreoffice", "onlyoffice", ""},
		{"docx 都没装 引导OO", "other", ".docx", none, "onlyoffice", "", "onlyoffice"},
		{"xlsx 都没装 引导LO", "other", ".xlsx", none, "libreoffice", "", "libreoffice"},
		{"odt 都没装 引导OO", "other", ".odt", none, "onlyoffice", "", "onlyoffice"},
		{"csv 是办公类", "csv", ".csv", none, "onlyoffice", "", "onlyoffice"},
		{"csv 装了OO", "csv", ".csv", all, "libreoffice", "libreoffice", ""},
		{"默认设置非法 回到OO", "other", ".pptx", none, "bogus", "", "onlyoffice"},
		{"图片 装了看图", "image", ".png", all, "onlyoffice", "imageglass", ""},
		{"图片 没装 静默回内置", "image", ".jpg", none, "onlyoffice", "", ""},
		{"文本 装了代码", "text", ".go", all, "onlyoffice", "vscodium", ""},
		{"文本 md 没装 静默回内置", "text", ".md", none, "onlyoffice", "", ""},
		{"pdf 永远回内置", "pdf", ".pdf", all, "onlyoffice", "", ""},
		{"zip 回内置", "zip", ".zip", all, "onlyoffice", "", ""},
		{"其他 回内置", "other", ".bin", all, "onlyoffice", "", ""},
	}
	for _, tc := range cases {
		comp, need := routeFileOpen(tc.kind, tc.ext, tc.inst, tc.def)
		if comp != tc.wantComp || need != tc.wantNeed {
			t.Errorf("%s：routeFileOpen(%s,%s) = (%q,%q)，想要 (%q,%q)",
				tc.name, tc.kind, tc.ext, comp, need, tc.wantComp, tc.wantNeed)
		}
	}
}

// ---------- launch containment ----------

func TestCompLaunchSpecContainment(t *testing.T) {
	oo, _ := prodByID("onlyoffice")
	lo, _ := prodByID("libreoffice")
	kr, _ := prodByID("krita")
	vs, _ := prodByID("vscodium")
	ig, _ := prodByID("imageglass")
	file := `C:\Users\x\Desktop\a.docx`

	// LibreOffice: vendor -env:UserInstallation into the component.
	args, env := compLaunchSpec(lo, file)
	if env != nil {
		t.Fatal("LibreOffice 不应改环境变量")
	}
	if len(args) != 2 || !strings.HasPrefix(args[0], "-env:UserInstallation=file:///") ||
		!strings.Contains(args[0], "components") || !strings.Contains(args[0], "libreoffice") ||
		!strings.HasSuffix(args[0], "/profile") {
		t.Fatalf("LibreOffice 参数不对：%v", args)
	}
	if args[1] != file {
		t.Fatalf("文件参数应在最后：%v", args)
	}

	// Krita: --resource-location into the component, file after it.
	args, env = compLaunchSpec(kr, "")
	if env != nil || len(args) != 2 || args[0] != "--resource-location" ||
		!strings.Contains(args[1], filepath.Join("krita", "profile", "resource")) {
		t.Fatalf("Krita 参数不对：%v", args)
	}

	// ONLYOFFICE: env redirect into the component profile.
	args, env = compLaunchSpec(oo, "")
	if len(args) != 0 {
		t.Fatalf("ONLYOFFICE 不应有额外参数：%v", args)
	}
	get := func(k string) string {
		for _, e := range env {
			if strings.HasPrefix(e, k+"=") {
				return strings.TrimPrefix(e, k+"=")
			}
		}
		return ""
	}
	for _, k := range []string{"APPDATA", "LOCALAPPDATA", "TEMP", "TMP"} {
		v := get(k)
		if v == "" || !strings.Contains(v, filepath.Join("onlyoffice", "profile")) {
			t.Fatalf("ONLYOFFICE 环境 %s 应指向组件 profile，实际 %q", k, v)
		}
	}

	// VSCodium / ImageGlass: plain launches, file still passes.
	for _, c := range []prodComponent{vs, ig} {
		args, env = compLaunchSpec(c, file)
		if env != nil || len(args) != 1 || args[0] != file {
			t.Fatalf("%s 应是纯启动+文件参数：%v", c.ID, args)
		}
	}
}

func TestSetEnvVarReplace(t *testing.T) {
	env := []string{"PATH=C:\\x", "APPDATA=C:\\old"}
	env = setEnvVar(env, "APPDATA", "D:\\new")
	if len(env) != 2 {
		t.Fatalf("替换不应新增条目：%v", env)
	}
	found := false
	for _, e := range env {
		if e == "APPDATA=D:\\new" {
			found = true
		}
	}
	if !found {
		t.Fatalf("APPDATA 没有被替换：%v", env)
	}
	env = setEnvVar(env, "NEWKEY", "v")
	if len(env) != 3 {
		t.Fatalf("新键应追加：%v", env)
	}
}

// ---------- icon extraction ----------

func grpIcon(t *testing.T, id uint16, payload []byte, w, h byte) []byte {
	t.Helper()
	g := make([]byte, 6+14)
	binary.LittleEndian.PutUint16(g[2:], 1) // type icon
	binary.LittleEndian.PutUint16(g[4:], 1) // count
	e := g[6:]
	e[0], e[1] = w, h
	binary.LittleEndian.PutUint16(e[4:], 1)  // planes
	binary.LittleEndian.PutUint16(e[6:], 32) // bit count
	binary.LittleEndian.PutUint32(e[8:], uint32(len(payload)))
	binary.LittleEndian.PutUint16(e[12:], id)
	return g
}

func TestBuildICOPicksLargest(t *testing.T) {
	small := []byte("small-icon")
	big := []byte("big-icon-payload-256px-png-bytes")
	// Manually build a 2-entry group: entry A id=1 (small), B id=2 (big).
	g := make([]byte, 6+28)
	binary.LittleEndian.PutUint16(g[2:], 1)
	binary.LittleEndian.PutUint16(g[4:], 2)
	mk := func(e []byte, id uint16, payload []byte) {
		e[0], e[1] = 32, 32
		binary.LittleEndian.PutUint16(e[4:], 1)
		binary.LittleEndian.PutUint16(e[6:], 32)
		binary.LittleEndian.PutUint32(e[8:], uint32(len(payload)))
		binary.LittleEndian.PutUint16(e[12:], id)
	}
	mk(g[6:20], 1, small)
	mk(g[20:34], 2, big)
	icons := map[uint32][]byte{1: small, 2: big}
	ico, err := buildICO(g, func(id uint32) []byte { return icons[id] })
	if err != nil {
		t.Fatalf("buildICO: %v", err)
	}
	if len(ico) != 22+len(big) {
		t.Fatalf("ICO 大小不对：%d", len(ico))
	}
	if binary.LittleEndian.Uint16(ico[0:]) != 0 || binary.LittleEndian.Uint16(ico[2:]) != 1 ||
		binary.LittleEndian.Uint16(ico[4:]) != 1 {
		t.Fatal("ICO 头不对")
	}
	if binary.LittleEndian.Uint32(ico[6+8:]) != uint32(len(big)) ||
		binary.LittleEndian.Uint32(ico[6+12:]) != 22 {
		t.Fatal("ICO 条目的大小/偏移不对")
	}
	if string(ico[22:]) != string(big) {
		t.Fatal("应选中最大的图标载荷")
	}
}

func TestBuildICOErrors(t *testing.T) {
	if _, err := buildICO([]byte{1, 2}, func(uint32) []byte { return nil }); err == nil {
		t.Fatal("过短的图标组应报错")
	}
	g := grpIcon(t, 7, []byte("x"), 16, 16)
	if _, err := buildICO(g, func(uint32) []byte { return nil }); err == nil {
		t.Fatal("图标数据取不到应报错")
	}
	bad := grpIcon(t, 7, []byte("x"), 16, 16)
	bad[2] = 2 // type must be 1
	if _, err := buildICO(bad, func(uint32) []byte { return []byte("x") }); err == nil {
		t.Fatal("类型不对的图标组应报错")
	}
}

// buildRsrcFixture lays out a synthetic .rsrc section:
//
//	root(2 entries: type 3, type 14) -> name dirs -> language dirs
//	-> data entries -> payloads. base is the section RVA.
func buildRsrcFixture(base uint32, groupID, iconID uint32, group, icon []byte) []byte {
	const (
		rootOff  = 0
		gNameOff = 32
		gLangOff = 56
		gDataOff = 80
		iNameOff = 96
		iLangOff = 120
		iDataOff = 144
		groupOff = 160
	)
	iconOff := groupOff + len(group)
	total := iconOff + len(icon)
	buf := make([]byte, total)
	dir := func(off int, entries ...[2]uint32) {
		binary.LittleEndian.PutUint16(buf[off+14:], uint16(len(entries)))
		for i, e := range entries {
			p := off + 16 + i*8
			binary.LittleEndian.PutUint32(buf[p:], e[0])
			binary.LittleEndian.PutUint32(buf[p+4:], e[1])
		}
	}
	dataEntry := func(off int, payloadOff int, payload []byte) {
		binary.LittleEndian.PutUint32(buf[off:], base+uint32(payloadOff))
		binary.LittleEndian.PutUint32(buf[off+4:], uint32(len(payload)))
	}
	const dirFlag = 0x80000000
	dir(rootOff, [2]uint32{3, dirFlag | iNameOff}, [2]uint32{14, dirFlag | gNameOff})
	dir(gNameOff, [2]uint32{groupID, dirFlag | gLangOff})
	dir(gLangOff, [2]uint32{0, gDataOff})
	dir(iNameOff, [2]uint32{iconID, dirFlag | iLangOff})
	dir(iLangOff, [2]uint32{0, iDataOff})
	dataEntry(gDataOff, groupOff, group)
	dataEntry(iDataOff, iconOff, icon)
	copy(buf[groupOff:], group)
	copy(buf[iconOff:], icon)
	return buf
}

func TestRsrcLookupEndToEnd(t *testing.T) {
	icon := []byte("\x89PNG fake 256 icon bytes")
	group := grpIcon(t, 9, icon, 0, 0)
	const base = 0x1000
	data := buildRsrcFixture(base, 5, 9, group, icon)

	gotGroup, err := rsrcLookup(data, base, 14, 0)
	if err != nil {
		t.Fatalf("rsrcLookup group: %v", err)
	}
	if string(gotGroup) != string(group) {
		t.Fatal("取到的图标组和放进去的不一致")
	}
	gotIcon, err := rsrcLookup(data, base, 3, 9)
	if err != nil {
		t.Fatalf("rsrcLookup icon: %v", err)
	}
	if string(gotIcon) != string(icon) {
		t.Fatal("取到的图标数据不一致")
	}
	if _, err := rsrcLookup(data, base, 3, 42); err == nil {
		t.Fatal("不存在的图标 ID 应报错")
	}
	if _, err := rsrcLookup(data, base, 99, 0); err == nil {
		t.Fatal("不存在的资源类型应报错")
	}
	ico, err := buildICO(gotGroup, func(id uint32) []byte {
		b, err := rsrcLookup(data, base, 3, id)
		if err != nil {
			return nil
		}
		return b
	})
	if err != nil {
		t.Fatalf("buildICO: %v", err)
	}
	if string(ico[22:]) != string(icon) {
		t.Fatal("组装出的 ICO 载荷不对")
	}
	// Truncated data must error, never panic.
	if _, err := rsrcLookup(data[:20], base, 14, 0); err == nil {
		t.Fatal("截断的资源数据应报错")
	}
}
