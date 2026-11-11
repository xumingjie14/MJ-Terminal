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
