package main

// v0.54 regression tests for the data-home resolution (main.go),
// born from the field report "pictures break after moving to
// another PC". The product audit found every image reference
// (custom wallpaper, imported-file thumbnails, component icons)
// is derived from the CURRENT folder at runtime -- nothing
// persists an absolute path. The one place the derivation can
// quietly change its answer is resolveDataDir's silent fallback
// to LocalAppData when the EXE folder is unwritable. These tests
// pin: (1) the portable home wins whenever it is writable,
// (2) the fallback is taken -- and REPORTED via the bool -- when
// it is not, (3) a simulated relocation (copy the whole folder
// elsewhere, resolve there) keeps every Data-resident reference
// resolvable, because references are always Join(current home).

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDataDirPrefersPortableHome(t *testing.T) {
	exe := t.TempDir()
	local := t.TempDir()
	dir, fb := resolveDataDir(exe, local)
	if fb {
		t.Fatalf("writable EXE folder must not fall back")
	}
	if want := filepath.Join(exe, "Data"); dir != want {
		t.Fatalf("dir = %q, want %q", dir, want)
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Fatalf("portable Data dir was not created: %v", err)
	}
}

func TestResolveDataDirFallbackIsReported(t *testing.T) {
	exe := t.TempDir()
	local := t.TempDir()
	// A regular FILE named Data where the folder must go makes
	// the portable home unusable on any OS, even as root.
	if err := os.WriteFile(filepath.Join(exe, "Data"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir, fb := resolveDataDir(exe, local)
	if !fb {
		t.Fatalf("blocked EXE folder must fall back")
	}
	if want := filepath.Join(local, "MJLight"); dir != want {
		t.Fatalf("dir = %q, want %q", dir, want)
	}
}

func TestRelocationKeepsDataReferencesResolvable(t *testing.T) {
	// Machine A: portable home with the assets the field report
	// named -- custom wallpaper bytes, an imported picture and
	// its thumbnail cache entry, a component icon.
	exeA := t.TempDir()
	homeA, fb := resolveDataDir(exeA, t.TempDir())
	if fb {
		t.Fatal("machine A unexpectedly fell back")
	}
	assets := map[string]string{
		"wallpaper.bin":                "wallpaper-bytes",
		"files/pic.png":                "picture-bytes",
		"files/.thumbs/pic.png.thumb":  "thumb-bytes",
		"components/krita/mj-icon.ico": "icon-bytes",
	}
	for rel, body := range assets {
		p := filepath.Join(homeA, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The move: the whole folder is copied to machine B (a
	// different absolute base) and the daemon resolves its home
	// THERE. Every reference must resolve under B's home with
	// the same bytes -- because no reference ever stored A's
	// absolute path.
	exeB := t.TempDir()
	homeB, fb := resolveDataDir(exeB, t.TempDir())
	if fb {
		t.Fatal("machine B unexpectedly fell back")
	}
	if homeB == homeA {
		t.Fatal("test setup error: homes must differ")
	}
	for rel, body := range assets {
		src := filepath.Join(homeA, filepath.FromSlash(rel))
		dst := filepath.Join(homeB, filepath.FromSlash(rel))
		b, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(dst)
		if err != nil || string(got) != body {
			t.Errorf("after relocation, %s does not resolve under the new home (err=%v)", rel, err)
		}
	}
}
