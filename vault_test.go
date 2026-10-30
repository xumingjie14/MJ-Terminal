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
	"errors"
	"fmt"
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

// ---------- v0.53: restore hardening ----------

// An interrupted seal (meta.Sealed == false): shard-0001 already
// holds files from two top-level dirs, and one of those dirs
// still has unsealed files that exist ONLY in Data. The restore
// prepare step must not wipe that dir (the pre-v0.53 restore
// RemoveAll'd every shard-covered top-level dir and destroyed
// the unsealed remainder permanently).
func TestVaultInterruptedSealRestoreKeepsUnsealedFiles(t *testing.T) {
	base := t.TempDir()
	data := filepath.Join(base, "Data")
	vdir := filepath.Join(base, "mj-vault")
	want := buildVaultTree(t, data)
	meta, master := cheapVaultMeta(t, "pw123456")

	// TotalBytes as a real interrupted seal leaves it: the full
	// tree size, recorded before the first shard was written.
	files, err := vaultListDataFiles(data)
	if err != nil {
		t.Fatal(err)
	}
	var total int64
	for _, rel := range files {
		st, err := os.Stat(filepath.Join(data, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		total += st.Size()
	}
	meta.TotalBytes = total

	// Seal exactly two files, from two different top-level dirs
	// ("nodes" whole; "sub" only in part -- sub/deep/c.txt stays
	// plaintext and exists nowhere else).
	sealed := []string{"nodes/links.txt", "sub/b.bin"}
	sm, err := vaultWriteShard(data, vdir, "shard-0001.mjv", sealed, master, nil)
	if err != nil {
		t.Fatal(err)
	}
	meta.Shards = []vaultShardMeta{sm}
	meta.Sealed = false // the defining trait of an interrupted seal
	if err := vaultSaveMeta(vdir, meta); err != nil {
		t.Fatal(err)
	}
	for _, rel := range sealed {
		if err := os.Remove(filepath.Join(data, filepath.FromSlash(rel))); err != nil {
			t.Fatal(err)
		}
	}
	// Sanity: the unsealed survivor really is still there.
	if _, err := os.Stat(filepath.Join(data, "sub", "deep", "c.txt")); err != nil {
		t.Fatalf("fixture wrong, survivor missing before restore: %v", err)
	}

	if err := vaultRestoreTree(data, vdir, master, meta, nil); err != nil {
		t.Fatalf("vaultRestoreTree(interrupted seal): %v", err)
	}
	// Survivor byte-identical AND shard-covered files restored:
	// the whole tree, hashes equal, vault back to a clean state.
	sameTree(t, want, hashVaultTree(t, data))
	if meta.Sealed {
		t.Error("meta.Sealed = true after restore")
	}
	if len(meta.Shards) != 0 {
		t.Errorf("meta.Shards = %d after restore, want 0", len(meta.Shards))
	}
	diskMeta, err := vaultLoadMeta(vdir)
	if err != nil {
		t.Fatal(err)
	}
	if diskMeta.Sealed || len(diskMeta.Shards) != 0 {
		t.Errorf("disk meta not clean after restore: sealed=%v shards=%d", diskMeta.Sealed, len(diskMeta.Shards))
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

// Per-shard commit: corrupt the SECOND shard so the restore dies
// after shard 1. Shard 1 must already be gone from disk AND from
// the on-disk manifest, shard 2 untouched; after repairing shard
// 2 the retry resumes and finishes with the whole tree intact --
// including the files the first attempt restored (they live only
// in Data now; a retry that wiped their top-level dir via the
// prepare step would lose them for good).
func TestVaultRestoreCommitsPerShardAndResumes(t *testing.T) {
	base := t.TempDir()
	data := filepath.Join(base, "Data")
	vdir := filepath.Join(base, "mj-vault")

	// 600 small files in ONE top-level dir: the seal groups by
	// the 512-file cap, producing exactly two shards that SHARE
	// the "bulk" top-level dir -- the arrangement under which a
	// naive retry would wipe the first shard's restored files.
	want := map[string]string{}
	for i := 0; i < 600; i++ {
		rel := fmt.Sprintf("bulk/f%04d.txt", i)
		content := bytes.Repeat([]byte(fmt.Sprintf("file-%04d|", i)), 9)
		p := filepath.Join(data, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, content, 0o644); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(content)
		want[rel] = hex.EncodeToString(sum[:])
	}
	meta, master := cheapVaultMeta(t, "pw123456")
	if err := vaultSealTree(data, vdir, master, meta, nil); err != nil {
		t.Fatalf("vaultSealTree: %v", err)
	}
	if len(meta.Shards) != 2 {
		t.Fatalf("fixture: got %d shards, want 2", len(meta.Shards))
	}
	first, second := meta.Shards[0], meta.Shards[1]

	// Save shard 2 pristine, then corrupt it (its SHA must fail).
	shard2Path := filepath.Join(vdir, second.Name)
	pristine, err := os.ReadFile(shard2Path)
	if err != nil {
		t.Fatal(err)
	}
	corrupt := append([]byte(nil), pristine...)
	corrupt[len(corrupt)/2] ^= 0xff
	if err := os.WriteFile(shard2Path, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := vaultRestoreTree(data, vdir, master, meta, nil); err == nil {
		t.Fatal("restore with a corrupted shard 2 succeeded")
	}
	// Shard 1 committed: file gone, on-disk manifest down to the
	// remaining shard, its plaintext fully restored.
	if _, err := os.Stat(filepath.Join(vdir, first.Name)); !os.IsNotExist(err) {
		t.Errorf("committed shard %s still on disk (stat err=%v)", first.Name, err)
	}
	diskMeta, err := vaultLoadMeta(vdir)
	if err != nil {
		t.Fatal(err)
	}
	if len(diskMeta.Shards) != 1 || diskMeta.Shards[0].Name != second.Name {
		t.Fatalf("disk manifest after failed restore = %+v, want only %s", diskMeta.Shards, second.Name)
	}
	if !diskMeta.Sealed {
		t.Error("disk meta Sealed flipped before the restore completed")
	}
	got := hashVaultTree(t, data)
	if len(got) != first.Files {
		t.Fatalf("restored %d files before the failure, want shard 1's %d", len(got), first.Files)
	}
	for rel, h := range got {
		if want[rel] != h {
			t.Errorf("file %s restored by attempt 1 has wrong content", rel)
		}
	}

	// Repair shard 2 and retry: the resume must finish the job,
	// and the attempt-1 files must survive it byte for byte.
	if err := os.WriteFile(shard2Path, pristine, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := vaultRestoreTree(data, vdir, master, meta, nil); err != nil {
		t.Fatalf("resume restore: %v", err)
	}
	sameTree(t, want, hashVaultTree(t, data))
	if len(meta.Shards) != 0 || meta.Sealed {
		t.Errorf("meta not clean after resume: shards=%d sealed=%v", len(meta.Shards), meta.Sealed)
	}
}

// The pre-flight: with (injected) zero free space the restore
// refuses by name BEFORE writing anything back -- manifest,
// shards and the (empty) Data all stay exactly as they were.
func TestVaultRestoreSpacePrecheckRefusesCleanly(t *testing.T) {
	base := t.TempDir()
	data := filepath.Join(base, "Data")
	vdir := filepath.Join(base, "mj-vault")
	buildVaultTree(t, data)
	meta, master := cheapVaultMeta(t, "pw123456")
	if err := vaultSealTree(data, vdir, master, meta, nil); err != nil {
		t.Fatal(err)
	}
	shardsBefore := len(meta.Shards)

	old := vaultFreeSpaceBytes
	vaultFreeSpaceBytes = func(string) (uint64, error) { return 0, nil }
	defer func() { vaultFreeSpaceBytes = old }()

	err := vaultRestoreTree(data, vdir, master, meta, nil)
	if err == nil {
		t.Fatal("restore with no free space started anyway")
	}
	var se *vaultSpaceError
	if !errors.As(err, &se) {
		t.Fatalf("error is %T (%v), want *vaultSpaceError", err, err)
	}
	if se.Free != 0 || se.Need <= 0 {
		t.Errorf("space error carries need=%d free=%d", se.Need, se.Free)
	}
	if !strings.Contains(err.Error(), "还差约") {
		t.Errorf("space error does not name the shortfall: %v", err)
	}
	// Nothing written back, nothing committed.
	if left := hashVaultTree(t, data); len(left) != 0 {
		t.Errorf("refused restore wrote plaintext back: %v", left)
	}
	diskMeta, lerr := vaultLoadMeta(vdir)
	if lerr != nil {
		t.Fatal(lerr)
	}
	if len(diskMeta.Shards) != shardsBefore || !diskMeta.Sealed {
		t.Errorf("refused restore changed the manifest: shards=%d sealed=%v", len(diskMeta.Shards), diskMeta.Sealed)
	}
	for _, sh := range diskMeta.Shards {
		if _, err := os.Stat(filepath.Join(vdir, sh.Name)); err != nil {
			t.Errorf("shard %s missing after refused restore: %v", sh.Name, err)
		}
	}
	// And the user-language rendering names the cause.
	if txt := vaultRestoreErrText(err); !strings.Contains(txt, "磁盘空间不足") {
		t.Errorf("lock-screen text does not name disk space: %q", txt)
	}
	if got := vaultRestoreErrText(errors.New("分片坏了")); got != "恢复失败：分片坏了" {
		t.Errorf("generic restore error text = %q", got)
	}
}

// lastError surfaces in the status map after a failure and is
// cleared on success -- the lock screen's only window into why
// an unlock fell back to the password prompt.
func TestVaultStatusLastErrorLifecycle(t *testing.T) {
	vaultSetLastErr("")
	if got := vaultStatusMap()["lastError"]; got != "" {
		t.Fatalf("lastError = %v, want empty", got)
	}
	vaultSetLastErr("恢复失败：磁盘空间不足（测试）")
	if got := vaultStatusMap()["lastError"]; got != "恢复失败：磁盘空间不足（测试）" {
		t.Fatalf("lastError = %v, want the recorded failure", got)
	}
	vaultSetLastErr("")
	if got := vaultStatusMap()["lastError"]; got != "" {
		t.Fatalf("lastError not cleared: %v", got)
	}
}
