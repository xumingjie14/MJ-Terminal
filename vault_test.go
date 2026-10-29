package main

// v0.48 vault tests. The crypto core is exercised end-to-end on
// synthetic Data trees: seal -> plaintext gone and unfindable ->
// unlock -> per-file hash equality; wrong password; interrupted
// (partial) seal recovery; password change; and the rule that
// password verification never touches a data shard.
//
// Tests use cheap Argon2id parameters via vaultNewMetaParams --
// the meta format stores the parameters, so this is the same
// code path with a smaller work factor (production: time=3,
// memory=64 MiB, threads=4).

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const testMarker = "ALPHA-PLAINTEXT-MARKER-中文标记-987654321"

func cheapVaultMeta(t *testing.T, password string) (*vaultMeta, []byte) {
	t.Helper()
	master := make([]byte, vaultKeyLen)
	if _, err := rand.Read(master); err != nil {
		t.Fatal(err)
	}
	m, err := vaultNewMetaParams(password, master, vaultKDF{Time: 1, Memory: 1024, Threads: 1})
	if err != nil {
		t.Fatal(err)
	}
	return m, master
}

// buildVaultTree creates the synthetic Data tree and returns
// slash-relative path -> sha256 hex for every file.
func buildVaultTree(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string][]byte{
		"a.txt":           []byte("hello " + testMarker),
		"sub/b.bin":       bytes.Repeat([]byte{0x5a, 0x11, 0x00, 0xff}, 1500),
		"sub/deep/c.txt":  []byte("deep file\n" + testMarker),
		"空文件.txt":         {},
		"中文名/说明.txt":      []byte("中文内容 " + testMarker),
		"nodes/links.txt": []byte("vless://example.invalid/0000 # node"),
		"日志/运行日志.log":     []byte("log line\n"),
	}
	big := make([]byte, 5<<20+123)
	if _, err := rand.Read(big); err != nil {
		t.Fatal(err)
	}
	files["大文件.bin"] = big
	out := map[string]string{}
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, content, 0o644); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(content)
		out[rel] = hex.EncodeToString(sum[:])
	}
	return out
}

func hashVaultTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		sum := sha256.Sum256(b)
		out[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sameTree(t *testing.T, want, got map[string]string) {
	t.Helper()
	if len(want) != len(got) {
		wk := make([]string, 0, len(want))
		for k := range want {
			wk = append(wk, k)
		}
		gk := make([]string, 0, len(got))
		for k := range got {
			gk = append(gk, k)
		}
		sort.Strings(wk)
		sort.Strings(gk)
		t.Fatalf("file count differs: want %d %v, got %d %v", len(want), wk, len(got), gk)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("file %s: hash differs (want %s, got %s)", k, v, got[k])
		}
	}
}

func TestVaultSealUnlockRoundTrip(t *testing.T) {
	base := t.TempDir()
	data := filepath.Join(base, "Data")
	vdir := filepath.Join(base, "mj-vault")
	want := buildVaultTree(t, data)
	meta, master := cheapVaultMeta(t, "correct horse battery")

	if err := vaultSealTree(data, vdir, master, meta, nil); err != nil {
		t.Fatalf("vaultSealTree: %v", err)
	}
	if !meta.Sealed {
		t.Error("meta.Sealed = false after a completed seal")
	}
	if len(meta.Shards) == 0 {
		t.Fatal("no shards recorded")
	}
	// Plaintext paths all gone: Data holds no files at all.
	if left := hashVaultTree(t, data); len(left) != 0 {
		t.Fatalf("plaintext remains after seal: %v", left)
	}
	// The known plaintext marker must not be findable in any
	// shard, and each shard's recorded hash must match its file.
	for _, sh := range meta.Shards {
		b, err := os.ReadFile(filepath.Join(vdir, sh.Name))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(b, []byte(testMarker)) {
			t.Errorf("shard %s contains the plaintext marker", sh.Name)
		}
		if bytes.Contains(b, []byte("vless://")) {
			t.Errorf("shard %s contains a plaintext node link", sh.Name)
		}
		sum := sha256.Sum256(b)
		if hex.EncodeToString(sum[:]) != sh.SHA256 {
			t.Errorf("shard %s: recorded SHA-256 does not match the file", sh.Name)
		}
	}
	// The meta on DISK must carry the sealed state + shards (a
	// boot reads only this).
	diskMeta, err := vaultLoadMeta(vdir)
	if err != nil {
		t.Fatalf("vaultLoadMeta: %v", err)
	}
	if !diskMeta.Sealed || len(diskMeta.Shards) != len(meta.Shards) {
		t.Fatalf("disk meta: sealed=%v shards=%d, want true/%d", diskMeta.Sealed, len(diskMeta.Shards), len(meta.Shards))
	}

	if err := vaultRestoreTree(data, vdir, master, meta, nil); err != nil {
		t.Fatalf("vaultRestoreTree: %v", err)
	}
	sameTree(t, want, hashVaultTree(t, data))
	if meta.Sealed {
		t.Error("meta.Sealed = true after restore")
	}
	if len(meta.Shards) != 0 {
		t.Errorf("meta.Shards = %d after restore, want 0", len(meta.Shards))
	}
	entries, err := os.ReadDir(vdir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".mjv") {
			t.Errorf("shard file %s left behind after restore", e.Name())
		}
	}
}

func TestVaultWrongPasswordRefusedAndTreeUntouched(t *testing.T) {
	base := t.TempDir()
	data := filepath.Join(base, "Data")
	vdir := filepath.Join(base, "mj-vault")
	want := buildVaultTree(t, data)
	meta, master := cheapVaultMeta(t, "right-password")
	if err := vaultSealTree(data, vdir, master, meta, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := vaultVerifyPassword(meta, "wrong-password"); err == nil {
		t.Fatal("vaultVerifyPassword(wrong) = nil error")
	}
	// The sealed state is exactly as before the failed attempt.
	diskMeta, err := vaultLoadMeta(vdir)
	if err != nil {
		t.Fatal(err)
	}
	if !diskMeta.Sealed || len(diskMeta.Shards) != len(meta.Shards) {
		t.Error("failed unlock changed the sealed state")
	}
	if left := hashVaultTree(t, data); len(left) != 0 {
		t.Errorf("failed unlock wrote plaintext: %v", left)
	}
	// And the right password still opens everything.
	key, err := vaultVerifyPassword(meta, "right-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := vaultRestoreTree(data, vdir, key, meta, nil); err != nil {
		t.Fatal(err)
	}
	sameTree(t, want, hashVaultTree(t, data))
}

func TestVaultVerifyNeverTouchesShards(t *testing.T) {
	base := t.TempDir()
	data := filepath.Join(base, "Data")
	vdir := filepath.Join(base, "mj-vault")
	buildVaultTree(t, data)
	meta, master := cheapVaultMeta(t, "pw123456")
	if err := vaultSealTree(data, vdir, master, meta, nil); err != nil {
		t.Fatal(err)
	}
	mtimes := map[string]int64{}
	for _, sh := range meta.Shards {
		st, err := os.Stat(filepath.Join(vdir, sh.Name))
		if err != nil {
			t.Fatal(err)
		}
		mtimes[sh.Name] = st.ModTime().UnixNano()
	}
	for i := 0; i < 3; i++ {
		if _, err := vaultVerifyPassword(meta, "pw123456"); err != nil {
			t.Fatal(err)
		}
		if _, err := vaultVerifyPassword(meta, "nope-nope"); err == nil {
			t.Fatal("wrong password accepted")
		}
	}
	for name, mt := range mtimes {
		st, err := os.Stat(filepath.Join(vdir, name))
		if err != nil {
			t.Fatal(err)
		}
		if st.ModTime().UnixNano() != mt {
			t.Errorf("shard %s mtime changed during password verification", name)
		}
	}
}

// A half-finished seal = some shards recorded + their sources
// deleted, the rest still plaintext, Sealed marker possibly set.
// The runtime recovery is restore-first; this test constructs
// exactly that state and proves the restore loses nothing.
func TestVaultPartialSealRecovery(t *testing.T) {
	base := t.TempDir()
	data := filepath.Join(base, "Data")
	vdir := filepath.Join(base, "mj-vault")
	want := buildVaultTree(t, data)
	meta, master := cheapVaultMeta(t, "pw123456")

	// Seal ONLY the first two files as one shard, delete their
	// sources, record the shard -- an interrupted seal's shape.
	files, err := vaultListDataFiles(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 3 {
		t.Fatalf("tree too small: %d files", len(files))
	}
	sm, err := vaultWriteShard(data, vdir, "shard-0001.mjv", files[:2], master, nil)
	if err != nil {
		t.Fatal(err)
	}
	meta.Shards = []vaultShardMeta{sm}
	meta.Sealed = true // marker may or may not be set; both must recover
	meta.TotalBytes = sm.Bytes
	if err := vaultSaveMeta(vdir, meta); err != nil {
		t.Fatal(err)
	}
	for _, rel := range files[:2] {
		if err := os.Remove(filepath.Join(data, filepath.FromSlash(rel))); err != nil {
			t.Fatal(err)
		}
	}
	vaultPruneEmptyDirs(data)

	// Sanity: we really are in a partial state.
	left := hashVaultTree(t, data)
	if len(left) != len(want)-2 {
		t.Fatalf("partial state has %d plaintext files, want %d", len(left), len(want)-2)
	}

	// Recovery = restore what is sealed; the plaintext remainder
	// was never touched. Result: the whole tree, hashes intact.
	if err := vaultRestoreTree(data, vdir, master, meta, nil); err != nil {
		t.Fatalf("vaultRestoreTree(partial): %v", err)
	}
	sameTree(t, want, hashVaultTree(t, data))
}

func TestVaultChangePassword(t *testing.T) {
	base := t.TempDir()
	data := filepath.Join(base, "Data")
	vdir := filepath.Join(base, "mj-vault")
	want := buildVaultTree(t, data)
	meta, master := cheapVaultMeta(t, "old-password")
	if err := vaultSealTree(data, vdir, master, meta, nil); err != nil {
		t.Fatal(err)
	}
	fresh, err := vaultRewrap(meta, master, "new-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vaultVerifyPassword(fresh, "old-password"); err == nil {
		t.Error("old password still works after rewrap")
	}
	key, err := vaultVerifyPassword(fresh, "new-password")
	if err != nil {
		t.Fatalf("new password does not verify: %v", err)
	}
	if !bytes.Equal(key, master) {
		t.Error("rewrap changed the master key (data would be lost)")
	}
	// Data sealed under the old header opens identically under
	// the rewrapped one (same key, same shards).
	if err := vaultRestoreTree(data, vdir, key, fresh, nil); err != nil {
		t.Fatal(err)
	}
	sameTree(t, want, hashVaultTree(t, data))
}

func TestVaultSealRefusesNonemptyManifest(t *testing.T) {
	base := t.TempDir()
	data := filepath.Join(base, "Data")
	vdir := filepath.Join(base, "mj-vault")
	buildVaultTree(t, data)
	meta, master := cheapVaultMeta(t, "pw123456")
	meta.Shards = []vaultShardMeta{{Name: "shard-0001.mjv"}}
	if err := vaultSealTree(data, vdir, master, meta, nil); err == nil {
		t.Fatal("sealing over a non-empty manifest was allowed (would corrupt it)")
	}
	if left := hashVaultTree(t, data); len(left) == 0 {
		t.Error("refused seal still deleted plaintext")
	}
}

func TestVaultSealRefusesVaultInsideData(t *testing.T) {
	base := t.TempDir()
	data := filepath.Join(base, "Data")
	buildVaultTree(t, data)
	meta, master := cheapVaultMeta(t, "pw123456")
	if err := vaultSealTree(data, filepath.Join(data, "vault-in"), master, meta, nil); err == nil {
		t.Fatal("vault root inside Data was allowed")
	}
}
