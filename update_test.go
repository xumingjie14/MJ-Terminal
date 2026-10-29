package main

// v0.48 updater-hardening tests: the rename retry helper (with an
// injected fail-then-succeed rename) and the .part reuse
// predicate (size + SHA-256) -- the two pure pieces behind the
// field failure "58 MB downloads fine, rename dies on a lock".

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func stubRenameSleep(t *testing.T) {
	t.Helper()
	old := renameRetrySleep
	renameRetrySleep = func(time.Duration) {}
	t.Cleanup(func() { renameRetrySleep = old })
}

func TestRenameWithRetrySucceedsAfterFailures(t *testing.T) {
	stubRenameSleep(t)
	attempts := 0
	logged := 0
	err := renameWithRetry(func() error {
		attempts++
		if attempts < 3 {
			return errors.New("文件正被另一进程使用")
		}
		return nil
	}, func(attempt int, err error) { logged++ })
	if err != nil {
		t.Fatalf("renameWithRetry = %v, want nil", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	if logged != 2 {
		t.Fatalf("logAttempt calls = %d, want 2 (one per failure)", logged)
	}
}

func TestRenameWithRetryExhausts(t *testing.T) {
	stubRenameSleep(t)
	attempts := 0
	err := renameWithRetry(func() error {
		attempts++
		return errors.New("始终被占用")
	}, nil)
	if err == nil {
		t.Fatal("renameWithRetry = nil, want the last error")
	}
	if attempts != 12 {
		t.Fatalf("attempts = %d, want 12 (the spec floor is >=10 over ~30-60s)", attempts)
	}
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func writeTemp(t *testing.T, dir, name string, b []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPartFileStatus(t *testing.T) {
	dir := t.TempDir()
	content := []byte("the quick brown fox jumps over the lazy dog")
	sum := sha256Hex(content)

	// Complete: size and hash both match.
	p := writeTemp(t, dir, "pkg.zip.part", content)
	if complete, from := partFileStatus(p, int64(len(content)), sum); !complete || from != int64(len(content)) {
		t.Errorf("complete case = (%v, %d), want (true, %d)", complete, from, len(content))
	}
	// Complete by size alone when the manifest has no hash.
	if complete, _ := partFileStatus(p, int64(len(content)), ""); !complete {
		t.Error("size-only case: complete = false, want true")
	}
	// Partial: usable as a Range prefix.
	part := writeTemp(t, dir, "partial.part", content[:10])
	if complete, from := partFileStatus(part, int64(len(content)), sum); complete || from != 10 {
		t.Errorf("partial case = (%v, %d), want (false, 10)", complete, from)
	}
	// Oversized: garbage, restart.
	over := writeTemp(t, dir, "over.part", append(append([]byte{}, content...), 'x'))
	if complete, from := partFileStatus(over, int64(len(content)), sum); complete || from != 0 {
		t.Errorf("oversized case = (%v, %d), want (false, 0)", complete, from)
	}
	// Full size, wrong bytes: restart, never resume.
	wrong := writeTemp(t, dir, "wrong.part", []byte("the quick brown fox jumps over the lazy cog"))
	if complete, from := partFileStatus(wrong, int64(len(content)), sum); complete || from != 0 {
		t.Errorf("wrong-content case = (%v, %d), want (false, 0)", complete, from)
	}
	// Missing file.
	if complete, from := partFileStatus(filepath.Join(dir, "nope.part"), 100, sum); complete || from != 0 {
		t.Errorf("missing case = (%v, %d), want (false, 0)", complete, from)
	}
}

func TestFinalizePartFileHappyPath(t *testing.T) {
	stubRenameSleep(t)
	dir := t.TempDir()
	content := []byte("package-bytes")
	part := writeTemp(t, dir, "pkg.zip.part", content)
	dest := filepath.Join(dir, "pkg.zip")
	if err := finalizePartFile(part, dest); err != nil {
		t.Fatalf("finalizePartFile = %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != string(content) {
		t.Fatalf("dest = %q, %v", got, err)
	}
	if _, err := os.Stat(part); !os.IsNotExist(err) {
		t.Errorf(".part should be gone after finalize, stat err = %v", err)
	}
}
