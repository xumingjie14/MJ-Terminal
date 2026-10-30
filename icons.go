package main

// ---------- v0.56: built-in app icons ----------
//
// test1 shipped macosicons.com artwork (plan A); the user
// field-rejected the set the same evening -- borrowed art from
// a dozen uploaders never cohered as one family, and several
// entries were off-style (a full-bleed ".ssh" square, a black
// box with tiny text for the console). Since test2 the built-in
// apps wear SELF-DRAWN original artwork: one macOS-native
// family authored as SVG in assets/icons-svg/ (one superellipse
// canvas, one top-light gradient model, bold glyphs), rendered
// to PNG and embedded as base64 in icons_data.go (a text file,
// so it travels through the same Contents-API push as every
// other source file), served from the local mux at
// /api/appicon/<id>. The one exception is openclaw -- the app's
// own mascot artwork, treated as a third-party icon. The view
// falls back to the plain gradient tile whenever an id has no
// artwork, so a missing icon degrades, never breaks.
// Provenance: icon-provenance.md at the repo root.
//
// The Chrome-family icon below is a different thing: it is the
// isolated browser's OWN exe icon, extracted at runtime exactly
// like the Firefox component's (ffIconHandler), for the Stage
// Manager identity cards.

import (
	"encoding/base64"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func mustB64(s string) []byte {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil
	}
	return b
}

// appIconPNG maps a built-in APPS id to its PNG bytes. Filled
// by icons_data.go's init() from embedded base64.
var appIconPNG = map[string][]byte{}

func appIconHandler(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/appicon/")
	if id == "" || strings.ContainsAny(id, "/\\.") {
		http.NotFound(w, r)
		return
	}
	b, ok := appIconPNG[id]
	if !ok || len(b) == 0 {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "max-age=86400")
	_, _ = w.Write(b)
}

// chromeIconHandler serves the isolated browser's real exe icon
// (Chrome or Edge, whichever findChrome picked) for the Stage
// Manager identity cards, cached in Data like ff-icon.ico.
func chromeIconHandler(w http.ResponseWriter, r *http.Request) {
	browser := findChrome()
	if browser == "" {
		http.NotFound(w, r)
		return
	}
	cache := filepath.Join(dataDir(), "chrome-icon.ico")
	if b, err := os.ReadFile(cache); err == nil && len(b) > 0 {
		w.Header().Set("Content-Type", "image/x-icon")
		w.Header().Set("Cache-Control", "max-age=3600")
		_, _ = w.Write(b)
		return
	}
	ico, err := exeIconICO(browser)
	if err != nil || len(ico) == 0 {
		http.NotFound(w, r)
		return
	}
	_ = os.WriteFile(cache, ico, 0o644)
	w.Header().Set("Content-Type", "image/x-icon")
	w.Header().Set("Cache-Control", "max-age=3600")
	_, _ = w.Write(ico)
}
