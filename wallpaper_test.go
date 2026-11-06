package main

import (
	"bytes"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// v0.63 (detail-fix ②): the wallpaper fit modes -- the valid
// set, the log labels, the default, and the sanitize fallback
// for a hand-edited or damaged settings.json.
func TestWallpaperFitValidation(t *testing.T) {
	for _, ok := range []string{"fill", "fit", "center", "stretch"} {
		if !validWallpaperFit(ok) {
			t.Errorf("fit %q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "cover", "contain", "FILL", "tile", "auto"} {
		if validWallpaperFit(bad) {
			t.Errorf("fit %q should be invalid", bad)
		}
	}
	labels := map[string]string{"fill": "填充", "fit": "适合", "center": "居中", "stretch": "拉伸", "bogus": "填充"}
	for in, want := range labels {
		if got := wallpaperFitLabel(in); got != want {
			t.Errorf("wallpaperFitLabel(%q) = %q, want %q", in, got, want)
		}
	}
	if got := defaultSettings().WallpaperFit; got != "fill" {
		t.Errorf("default wallpaperFit = %q, want fill (the pre-v0.63 cover behaviour)", got)
	}
	s := sanitizeSettings(mjSettings{WallpaperFit: "fit", SummonHotkey: "F8"})
	if s.WallpaperFit != "fit" {
		t.Errorf("valid fit should survive sanitize, got %q", s.WallpaperFit)
	}
	s = sanitizeSettings(mjSettings{WallpaperFit: "cover", SummonHotkey: "F8"})
	if s.WallpaperFit != "fill" {
		t.Errorf("invalid fit should fall back to fill, got %q", s.WallpaperFit)
	}
}

// v0.63: wallpaperFit rides the settings whitelist like every
// other key: a valid POST persists (and shows in settingsMap),
// an invalid one is rejected without changing the value.
func TestWallpaperFitSettingsWhitelist(t *testing.T) {
	dsTestRoot(t)
	setState.Lock()
	oldCur, oldLoaded, oldFresh := setState.cur, setState.loaded, setState.fresh
	setState.cur, setState.loaded = defaultSettings(), true
	setState.Unlock()
	defer func() {
		setState.Lock()
		setState.cur, setState.loaded, setState.fresh = oldCur, oldLoaded, oldFresh
		setState.Unlock()
	}()
	if got := settingsGet().WallpaperFit; got != "fill" {
		t.Fatalf("wallpaperFit default = %q, want fill", got)
	}
	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(body))
		rec := httptest.NewRecorder()
		settingsHandler(rec, req)
		return rec
	}
	rec := post(`{"wallpaperFit": "center"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST wallpaperFit=center: status %d, body %s", rec.Code, rec.Body.String())
	}
	if got := settingsGet().WallpaperFit; got != "center" {
		t.Fatalf("wallpaperFit=center did not persist, got %q", got)
	}
	if got := settingsMap()["wallpaperFit"]; got != "center" {
		t.Fatalf("settingsMap wallpaperFit = %v, want center", got)
	}
	rec = post(`{"wallpaperFit": "cover"}`)
	if !strings.Contains(rec.Body.String(), `"code"`) || settingsGet().WallpaperFit != "center" {
		t.Fatalf("invalid wallpaperFit must be rejected without changing the value: %s", rec.Body.String())
	}
	rec = post(`{"wallpaperFit": 3}`)
	if !strings.Contains(rec.Body.String(), `"code"`) || settingsGet().WallpaperFit != "center" {
		t.Fatalf("non-string wallpaperFit must be rejected without changing the value: %s", rec.Body.String())
	}
}

// v0.63: persistWallpaperFit (the fromfile endpoint's settings
// write) lands the value in the store and on disk, so a fresh
// load sees it.
func TestPersistWallpaperFit(t *testing.T) {
	dsTestRoot(t)
	setState.Lock()
	oldCur, oldLoaded, oldFresh := setState.cur, setState.loaded, setState.fresh
	setState.cur, setState.loaded = defaultSettings(), true
	setState.Unlock()
	defer func() {
		setState.Lock()
		setState.cur, setState.loaded, setState.fresh = oldCur, oldLoaded, oldFresh
		setState.Unlock()
	}()
	persistWallpaperFit("stretch")
	if got := settingsGet().WallpaperFit; got != "stretch" {
		t.Fatalf("persistWallpaperFit: store = %q, want stretch", got)
	}
	setState.Lock()
	setState.cur, setState.loaded = mjSettings{}, false
	setState.Unlock()
	if got := settingsGet().WallpaperFit; got != "stretch" {
		t.Fatalf("persistWallpaperFit: reloaded = %q, want stretch (the save must reach settings.json)", got)
	}
}

// v0.63: the fromfile endpoint's source validation -- a real
// image passes with its bytes and content type; a corrupt
// image, a non-image and a missing file are all refused.
func TestWallpaperImageBytes(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	good := filepath.Join(dir, "pic.png")
	if err := os.WriteFile(good, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	b, ct, err := wallpaperImageBytes(good)
	if err != nil {
		t.Fatalf("valid png refused: %v", err)
	}
	if !bytes.Equal(b, buf.Bytes()) || ct != "image/png" {
		t.Errorf("valid png: bytes equal=%v, ct=%q, want image/png", bytes.Equal(b, buf.Bytes()), ct)
	}
	corrupt := filepath.Join(dir, "bad.png")
	if err := os.WriteFile(corrupt, []byte("this is not a png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := wallpaperImageBytes(corrupt); err == nil {
		t.Error("corrupt png should be refused")
	}
	txt := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(txt, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := wallpaperImageBytes(txt); err == nil {
		t.Error("a text file should be refused as not-an-image")
	}
	if _, _, err := wallpaperImageBytes(filepath.Join(dir, "missing.png")); err == nil {
		t.Error("a missing file should be refused")
	}
	if _, _, err := wallpaperImageBytes(dir); err == nil {
		t.Error("a directory should be refused")
	}
	// webp has no stdlib decoder: it passes the byte-level
	// checks (the dialog's <img> probe is its decode gate).
	webp := filepath.Join(dir, "pic.webp")
	if err := os.WriteFile(webp, []byte("RIFF\x00\x00\x00\x00WEBP"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := wallpaperImageBytes(webp); err != nil {
		t.Errorf("webp should pass byte-level validation, got %v", err)
	}
}
