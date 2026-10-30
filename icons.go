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
// test3 (field report: "icons still the old ones after the
// update"): test1's handler sent Cache-Control: max-age=86400,
// so the Edge view cached test1's artwork for a day and in-app
// refreshes never re-asked the server -- the new daemon served
// the new art and nobody ever saw it. Icon bytes are program
// assets whose URL does not change across versions, so they
// are now served no-cache (revalidate every load) AND the view
// version-tokens their URLs with the running AppVersion
// (verQ() in index.html) -- the token is what unsticks machines
// already holding a max-age entry. serveIconBytes is the one
// place every icon handler gets its headers from.
//
// The Chrome-family icon below is a different thing: it is the
// isolated browser's OWN exe icon, extracted at runtime exactly
// like the Firefox component's (ffIconHandler), for the Stage
// Manager identity cards. Both are cached in Data -- and since
// test3 the cache is keyed by the source exe's path+mtime+size
// (a sidecar stamp), so swapping the underlying exe (Chrome
// <-> Edge, a Firefox update) can never serve a stale icon
// forever.

import (
	"encoding/base64"
	"fmt"
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

// serveIconBytes writes icon bytes with the one cache policy
// every icon endpoint shares: no-cache. Icon URLs are stable
// across versions while their content is a program asset, so a
// reusable cache entry is exactly the test1->test2 failure.
func serveIconBytes(w http.ResponseWriter, b []byte, contentType string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(b)
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
	serveIconBytes(w, b, "image/png")
}

// ---------- exe icon Data caches, keyed by the source exe ----------

// exeIconStamp identifies the exe an extracted icon belongs
// to: absolute path + mtime + size. Any exe swap or update
// changes the stamp and invalidates the cached icon.
func exeIconStamp(exePath string) (string, error) {
	fi, err := os.Stat(exePath)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s\n%d\n%d", exePath, fi.ModTime().UnixNano(), fi.Size()), nil
}

func exeIconSidecar(cachePath string) string { return cachePath + ".src" }

// readExeIconCache returns the cached icon only when its
// sidecar stamp still matches the exe's current stamp.
func readExeIconCache(cachePath, exePath string) ([]byte, bool) {
	stamp, err := exeIconStamp(exePath)
	if err != nil {
		return nil, false
	}
	side, err := os.ReadFile(exeIconSidecar(cachePath))
	if err != nil || string(side) != stamp {
		return nil, false
	}
	b, err := os.ReadFile(cachePath)
	if err != nil || len(b) == 0 {
		return nil, false
	}
	return b, true
}

func writeExeIconCache(cachePath, exePath string, ico []byte) {
	if stamp, err := exeIconStamp(exePath); err == nil {
		_ = os.WriteFile(exeIconSidecar(cachePath), []byte(stamp), 0o644)
	}
	_ = os.WriteFile(cachePath, ico, 0o644)
}

// exeIconServed resolves the icon for exePath: the keyed Data
// cache when it still matches the exe, otherwise a fresh
// extraction (which re-keys the cache). origin is "cache" or
// "extract" so callers can log cache rebuilds without logging
// every hit.
func exeIconServed(cachePath, exePath string) (ico []byte, origin string, err error) {
	if b, ok := readExeIconCache(cachePath, exePath); ok {
		return b, "cache", nil
	}
	ico, err = exeIconICO(exePath)
	if err != nil {
		return nil, "", err
	}
	if len(ico) == 0 {
		return nil, "", fmt.Errorf("从 %s 提取到的图标为空", exePath)
	}
	writeExeIconCache(cachePath, exePath, ico)
	return ico, "extract", nil
}

// chromeIconHandler serves the isolated browser's real exe icon
// (Chrome or Edge, whichever findChrome picked) for the Stage
// Manager identity cards, cached in Data keyed by the exe.
func chromeIconHandler(w http.ResponseWriter, r *http.Request) {
	browser := findChrome()
	if browser == "" {
		http.NotFound(w, r)
		return
	}
	cache := filepath.Join(dataDir(), "chrome-icon.ico")
	ico, origin, err := exeIconServed(cache, browser)
	if err != nil || len(ico) == 0 {
		http.NotFound(w, r)
		return
	}
	if origin == "extract" {
		logf("icons", "info", "浏览器图标已从 %s 提取并缓存（缓存缺失或来源 exe 已变化）", browser)
	}
	serveIconBytes(w, ico, "image/x-icon")
}
