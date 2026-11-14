package main

// addapp_test.go -- v0.69 block 2 gates: page metadata parsing,
// icon sniffing, URL normalization / duplicate rules, and the
// pin-name derivation. All pure.

import (
	"testing"
)

func TestParsePageMeta(t *testing.T) {
	page := []byte(`<html><head>
<title>  Example &amp; Co —
   Home </title>
<meta property="og:title" content="OG Should Not Win">
<link rel="stylesheet" href="/x.css">
<link rel="icon" type="image/png" href="/favicon-32.png">
<link rel="apple-touch-icon" href="/apple.png">
</head><body>hi</body></html>`)
	title, icons := parsePageMeta(page)
	if title != "Example & Co — Home" {
		t.Errorf("title = %q", title)
	}
	if len(icons) != 2 || icons[0] != "/apple.png" || icons[1] != "/favicon-32.png" {
		t.Errorf("icons = %v, want [/apple.png /favicon-32.png] (apple first)", icons)
	}
}

func TestParsePageMetaOGFallback(t *testing.T) {
	page := []byte(`<html><head><meta content="OG &quot;Title&quot;" property="og:title"></head></html>`)
	title, _ := parsePageMeta(page)
	if title != `OG "Title"` {
		t.Errorf("og title = %q", title)
	}
	// No title anywhere: empty, and the data: icon is skipped.
	title, icons := parsePageMeta([]byte(`<link rel="icon" href="data:image/png;base64,xx">`))
	if title != "" || len(icons) != 0 {
		t.Errorf("empty page: title=%q icons=%v", title, icons)
	}
}

// ---------- v0.71 test4: the scored icon chain ----------

func TestValidThemeColor(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
		ok   bool
	}{
		{"#FF5722", "#ff5722", true},
		{" #abc ", "#aabbcc", true},
		{"#123456", "#123456", true},
		{"red", "", false},
		{"#12345", "", false},
		{"#1234567", "", false},
		{"rgb(1,2,3)", "", false},
		{"", "", false},
	} {
		got, ok := validThemeColor(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("validThemeColor(%q) = %q,%v want %q,%v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestDeclaredIconSize(t *testing.T) {
	for _, tc := range []struct {
		sizes string
		def   int
		want  int
	}{
		{"", 180, 180},
		{"any", 0, iconSizeAny},
		{"48x48", 0, 48},
		{"16x16 192x192 32x32", 0, 192},
		{"garbage", 42, 42},
		{"192x192", 0, 192},
	} {
		if got := declaredIconSize(tc.sizes, tc.def); got != tc.want {
			t.Errorf("declaredIconSize(%q,%d) = %d, want %d", tc.sizes, tc.def, got, tc.want)
		}
	}
}

func TestParsePageMetaFull(t *testing.T) {
	page := []byte(`<html><head>
<title>Example</title>
<meta name="theme-color" content="#FF5722">
<link rel="manifest" href="/manifest.webmanifest">
<link href="/favicon-32.png" sizes="32x32" rel="icon">
<link rel="apple-touch-icon" href="/apple.png">
<link rel="icon" type="image/svg+xml" sizes="any" href="/icon.svg">
</head><body>hi</body></html>`)
	m := parsePageMetaFull(page)
	if m.ThemeColor != "#ff5722" {
		t.Errorf("themeColor = %q, want #ff5722", m.ThemeColor)
	}
	if m.Manifest != "/manifest.webmanifest" {
		t.Errorf("manifest = %q", m.Manifest)
	}
	if len(m.Icons) != 3 {
		t.Fatalf("icons = %v, want 3", m.Icons)
	}
	// Apple class first (v0.69 order), undeclared = 180 by convention.
	if m.Icons[0].Href != "/apple.png" || !m.Icons[0].Apple || m.Icons[0].Declared != 180 {
		t.Errorf("icons[0] = %+v", m.Icons[0])
	}
	byHref := map[string]iconLink{}
	for _, ic := range m.Icons {
		byHref[ic.Href] = ic
	}
	if byHref["/favicon-32.png"].Declared != 32 {
		t.Errorf("favicon declared = %d, want 32", byHref["/favicon-32.png"].Declared)
	}
	if byHref["/icon.svg"].Declared != iconSizeAny {
		t.Errorf("svg declared = %d, want iconSizeAny", byHref["/icon.svg"].Declared)
	}
	// A page with an unusable theme color keeps "" (never garbage).
	m2 := parsePageMetaFull([]byte(`<meta name="theme-color" content="rebeccapurple">`))
	if m2.ThemeColor != "" {
		t.Errorf("named theme color accepted: %q", m2.ThemeColor)
	}
}

func TestParseManifestIcons(t *testing.T) {
	doc := []byte(`{"name":"X","icons":[
		{"src":"/icon-192.png","sizes":"192x192","type":"image/png"},
		{"src":"/icon-512.png","sizes":"512x512","purpose":"any maskable"},
		{"src":"/badge.png","sizes":"96x96","purpose":"monochrome"},
		{"src":"","sizes":"64x64"}
	]}`)
	got := parseManifestIcons(doc)
	if len(got) != 2 {
		t.Fatalf("manifest icons = %v, want 2 (monochrome + empty dropped)", got)
	}
	if got[0].Declared != 192 || got[1].Declared != 512 {
		t.Errorf("declared = %d,%d want 192,512", got[0].Declared, got[1].Declared)
	}
	if bad := parseManifestIcons([]byte(`{not json`)); bad != nil {
		t.Errorf("bad json: %v", bad)
	}
}

func TestIconRealSize(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 13, 'I', 'H', 'D', 'R', 0, 0, 1, 0, 0, 0, 0, 128}  // 256x128
	ico := []byte{0, 0, 1, 0, 2, 0, 32, 32, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 48, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0} // frames 32x32 + 256x48
	gif := append([]byte("GIF89a"), 48, 0, 32, 0)
	jpg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 16, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xFF, 0xC0, 0, 17, 8, 0, 64, 0, 128}         // SOF0: 128x64
	webp := []byte{'R', 'I', 'F', 'F', 0, 0, 0, 0, 'W', 'E', 'B', 'P', 'V', 'P', '8', 'X', 0, 0, 0, 0, 0, 0, 0, 0, 43, 1, 0, 199, 0, 0} // VP8X 300x200
	cases := []struct {
		name string
		b    []byte
		want int
	}{
		{"png", png, 256},
		{"ico", ico, 256},
		{"gif", gif, 48},
		{"jpg", jpg, 128},
		{"webp", webp, 300},
		{"svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), iconSizeAny},
		{"junk", []byte("<html>no</html>"), 0},
		{"short png", []byte{0x89, 'P', 'N', 'G'}, 0},
	}
	for _, tc := range cases {
		if got := iconRealSize(tc.b); got != tc.want {
			t.Errorf("%s: iconRealSize = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestDecideIconWinner(t *testing.T) {
	fs := []fetchedIcon{
		{Cand: iconCandidate{Declared: 180}, Real: 180},
		{Cand: iconCandidate{Declared: 512}, Real: 32}, // declared lies: real 32 loses
		{Cand: iconCandidate{Declared: 0}, Real: 256},  // undeclared but really 256: wins
		{Cand: iconCandidate{Declared: 256}, Real: 0},  // unmeasurable: competes at declared, ties -> earlier wins
	}
	if got := decideIconWinner(fs); got != 2 {
		t.Errorf("winner = %d, want 2 (real size rules)", got)
	}
	svg := []fetchedIcon{
		{Cand: iconCandidate{Declared: 512}, Real: 512},
		{Cand: iconCandidate{Declared: iconSizeAny}, Real: iconSizeAny},
	}
	if got := decideIconWinner(svg); got != 1 {
		t.Errorf("svg winner = %d, want 1", got)
	}
}

func TestWebAppLedgerThemeColor(t *testing.T) {
	dsTestRoot(t)
	raw := "旧条目\thttps://old.example.com\n" +
		"全字段\thttps://full.example.com\tphone\t0123456789abcdef.png\t#FF5722\n" +
		"只有颜色\thttps://color.example.com\t\t\t#abc\n" +
		"坏颜色\thttps://bad.example.com\t\t\tred\n"
	if err := dsSave("webapps", []byte(raw)); err != nil {
		t.Fatal(err)
	}
	byURL := map[string]webApp{}
	for _, a := range loadWebApps() {
		byURL[a.URL] = a
	}
	if a := byURL["https://old.example.com"]; a.ThemeColor != "" || a.Icon != "" {
		t.Errorf("old entry grew fields: %+v", a)
	}
	if a := byURL["https://full.example.com"]; a.ThemeColor != "#ff5722" || a.Icon != "0123456789abcdef.png" || a.WinMode != "phone" {
		t.Errorf("full entry: %+v", a)
	}
	if a := byURL["https://color.example.com"]; a.ThemeColor != "#aabbcc" {
		t.Errorf("color-only entry: %+v", a)
	}
	if a := byURL["https://bad.example.com"]; a.ThemeColor != "" {
		t.Errorf("bad color accepted: %+v", a)
	}
	// Save shape: a theme-color entry writes all five columns
	// with the middle ones padded empty; a bare entry stays the
	// exact old two-field line.
	saveWebApps([]webApp{
		{Name: "甲", URL: "https://a.example.com", ThemeColor: "#112233"},
		{Name: "乙", URL: "https://b.example.com"},
		{Name: "丙", URL: "https://c.example.com", Icon: "0123456789abcdef.png"},
	})
	b, _, err := dsLoad("webapps")
	if err != nil {
		t.Fatal(err)
	}
	want := "甲\thttps://a.example.com\t\t\t#112233\n" +
		"乙\thttps://b.example.com\n" +
		"丙\thttps://c.example.com\t\t0123456789abcdef.png\n"
	if string(b) != want {
		t.Errorf("saved ledger:\n%q\nwant:\n%q", b, want)
	}
}

func TestSniffIconExt(t *testing.T) {
	cases := []struct {
		b    []byte
		want string
	}{
		{[]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, ".png"},
		{[]byte{0, 0, 1, 0, 1, 0}, ".ico"},
		{[]byte{0xFF, 0xD8, 0xFF, 0xE0}, ".jpg"},
		{[]byte("GIF89a...."), ".gif"},
		{[]byte("RIFF\x00\x00\x00\x00WEBP"), ".webp"},
		{[]byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), ".svg"},
		{[]byte("<html>not an icon</html>"), ""},
		{[]byte{}, ""},
	}
	for _, tc := range cases {
		if got := sniffIconExt(tc.b); got != tc.want {
			t.Errorf("sniff(%q...) = %q, want %q", tc.b[:min(len(tc.b), 8)], got, tc.want)
		}
	}
}

func TestNormalizeWebURL(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
		ok   bool
	}{
		{"example.com", "https://example.com", true},
		{" https://x.com/a ", "https://x.com/a", true},
		{"http://a.b", "http://a.b", true},
		{"ftp://a.b", "", false},
		{"", "", false},
		{"https://", "", false},
		{"javascript:alert(1)", "", false},
	} {
		got, ok := normalizeWebURL(tc.in)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("normalizeWebURL(%q) = %q,%v want %q,%v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestWebAppExists(t *testing.T) {
	list := []webApp{{Name: "YT", URL: "https://www.youtube.com"}}
	if name, ok := webAppExists(list, "https://www.youtube.com/"); !ok || name != "YT" {
		t.Errorf("trailing-slash dup: %q %v", name, ok)
	}
	if _, ok := webAppExists(list, "https://vimeo.com"); ok {
		t.Error("non-entry reported as existing")
	}
}

func TestPinNameFromTitle(t *testing.T) {
	for _, tc := range []struct {
		title, url, want string
	}{
		{"YouTube - Google Chrome", "https://www.youtube.com", "YouTube"},
		{"文档 - Microsoft Edge", "https://docs.example.com", "文档"},
		{"", "https://example.com/x", "example.com"},
		{"", "not a url", "网站应用"},
	} {
		if got := pinNameFromTitle(tc.title, tc.url); got != tc.want {
			t.Errorf("pinNameFromTitle(%q,%q) = %q, want %q", tc.title, tc.url, got, tc.want)
		}
	}
	if len(webIconName("https://x.com")) != 16 {
		t.Error("icon name must be a 16-char FNV hex")
	}
}
