package main

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// v0.56 test3: icon endpoints must never let the view's HTTP
// cache strand an update. test1 -> test2 field failure: the
// appicon handler sent max-age=86400, so the Edge view served
// the old build's artwork for a day while the new daemon had
// the new art all along. These tests pin no-cache on the icon
// handlers, the exe-stamp keying of the Data-side icon caches,
// and the served page's version token.

func TestAppIconServedNoCache(t *testing.T) {
	const id = "unittest-icon"
	appIconPNG[id] = []byte{0x89, 'P', 'N', 'G'}
	defer delete(appIconPNG, id)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/appicon/"+id, nil)
	appIconHandler(rec, req)
	if rec.Code != 200 {
		t.Fatalf("appicon: got status %d, want 200", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("appicon: Cache-Control = %q, want %q", cc, "no-cache")
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("appicon: Content-Type = %q, want image/png", ct)
	}
}

func TestServeIconBytesNoCache(t *testing.T) {
	rec := httptest.NewRecorder()
	serveIconBytes(rec, []byte("ICO"), "image/x-icon")
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("serveIconBytes: Cache-Control = %q, want %q", cc, "no-cache")
	}
	if rec.Body.String() != "ICO" {
		t.Fatalf("serveIconBytes: body = %q, want the icon bytes", rec.Body.String())
	}
}

// The Data-side exe icon caches (chrome-icon.ico / ff-icon.ico
// / per-component mj-icon.ico) are keyed by the source exe's
// path+mtime+size: the same exe hits, a changed exe (browser
// swap, component update) misses, a different exe path misses.
func TestExeIconCacheKeyedByExe(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "browser.exe")
	if err := os.WriteFile(exe, []byte("fake-exe-v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(dir, "icon.ico")
	writeExeIconCache(cache, exe, []byte("ICO-BYTES"))
	b, ok := readExeIconCache(cache, exe)
	if !ok || !bytes.Equal(b, []byte("ICO-BYTES")) {
		t.Fatalf("same exe: cache read = %q / %v, want the cached bytes", b, ok)
	}
	// The exe changes (update / different browser): new content
	// AND a moved mtime -- the stamp must no longer match.
	if err := os.WriteFile(exe, []byte("fake-exe-v2-longer"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(exe, future, future); err != nil {
		t.Fatal(err)
	}
	if _, ok := readExeIconCache(cache, exe); ok {
		t.Fatal("cache survived an exe change -- a stale icon would serve forever")
	}
	// A different exe path never matches, even with same bytes.
	exe2 := filepath.Join(dir, "other.exe")
	if err := os.WriteFile(exe2, []byte("fake-exe-v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := readExeIconCache(cache, exe2); ok {
		t.Fatal("cache matched a different exe path")
	}
	// And a cache without its sidecar (pre-test3 Data) is a
	// miss: it re-extracts once and re-keys.
	cache2 := filepath.Join(dir, "legacy.ico")
	if err := os.WriteFile(cache2, []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := readExeIconCache(cache2, exe2); ok {
		t.Fatal("sidecar-less legacy cache must count as a miss")
	}
}

// The served shell page must carry the running AppVersion in
// its APPVER constant: index.html on disk holds the token
// exactly once, and what the daemon serves has it substituted.
func TestServedIndexCarriesAppVersion(t *testing.T) {
	src, err := os.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(src), "__MJ_APPVER__"); n != 1 {
		t.Fatalf("index.html carries the __MJ_APPVER__ token %d times, want exactly 1", n)
	}
	if bytes.Contains(servedIndexHTML, []byte("__MJ_APPVER__")) {
		t.Fatal("served page still contains the raw version token")
	}
	if !bytes.Contains(servedIndexHTML, []byte("'"+AppVersion+"'")) {
		t.Fatalf("served page does not carry AppVersion %q in its APPVER constant", AppVersion)
	}
}
