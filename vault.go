package main

// ---------- data vault: password + full-Data encryption + lock (v0.48) ----------
//
// The user's demand, verbatim: EVERY file under Data is encrypted
// while the terminal is closed or locked -- copy the folder away
// and all you get is ciphertext; only typing the right password
// inside the terminal unlocks it. Scope is the whole Data tree
// (imported files, browser profiles, nodes & subscriptions, SSH
// keys, store-app binaries, the whole sandbox box, components,
// OpenClaw config), no picking and choosing. Outside the vault:
// only the EXE itself (it has to run to ask for the password) and
// the public sing-box core next to it.
//
// Model (no home-grown protocol):
//   * A random 32-byte master key encrypts the data. The password
//     never encrypts data directly: Argon2id (time=3, memory=64MiB,
//     threads=4, 16-byte random salt) derives a KEK that WRAPS the
//     master key (AES-256-GCM). Changing the password re-wraps --
//     a second's work, the data is never re-encrypted.
//   * Password verification touches only a verify token (the
//     master key GCM-sealed over a fixed constant) in the small
//     header -- never a data shard.
//   * Data is sealed into shards (target ~128 MiB of plaintext
//     each), stored in a SIBLING directory of Data (mj-vault/), so
//     Data itself can be emptied completely. Each shard is a
//     stream of 1 MiB AES-256-GCM chunks (per-shard random nonce
//     base + chunk counter), independently SHA-256'd and listed in
//     the header manifest.
//   * Crash rule, absolute: a shard's source files are deleted
//     ONLY after the shard is durable on disk (fsync + rename)
//     AND recorded in the header. An interrupted seal therefore
//     leaves every file either as plaintext or inside exactly one
//     recorded shard -- the next unlock restores everything
//     (rollback to plaintext) with zero loss.
//   * The header (mj-vault/meta.json) lives OUTSIDE Data: KDF
//     salt, wrapped key, verify token, shard manifest. The daemon
//     boots to the password screen from the header alone.
//
// Honest boundaries (also written in Settings + the release notes):
//   * While the terminal is UNLOCKED and running, the data exists
//     as plaintext on the disk -- live transparent encryption
//     needs a driver, which the portable no-install model forbids.
//     The vault protects the closed/locked state, which is the
//     scenario the user asked about (folder copied while away).
//   * A forgotten password means the data is unrecoverable. There
//     is no backdoor, by design.
//   * If the daemon is killed mid-seal, the not-yet-sealed
//     remainder stays plaintext until the next unlock finishes
//     the rollback -- the key exists only in RAM during a session,
//     so no boot-time code could seal it without the password.

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/argon2"
)

// ---------- parameters ----------

const (
	vaultKDFTime    = 3
	vaultKDFMemory  = 64 * 1024 // KiB = 64 MiB
	vaultKDFThreads = 4
	vaultSaltLen    = 16
	vaultKeyLen     = 32

	vaultChunkSize  = 1 << 20   // 1 MiB plaintext per GCM chunk
	vaultShardBytes = 128 << 20 // target plaintext per shard
	vaultShardFiles = 512       // ... or this many files, whichever first
	vaultMagic      = "MJV1"    // shard file magic
	vaultVerifyText = "MJ-VAULT-VERIFY\x01"
)

// ---------- header (meta.json, outside Data) ----------

type vaultKDF struct {
	Salt    string `json:"salt"` // base64, 16 bytes
	Time    uint32 `json:"time"`
	Memory  uint32 `json:"memory"` // KiB
	Threads uint8  `json:"threads"`
}

type vaultShardMeta struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`   // ciphertext bytes on disk
	SHA256 string `json:"sha256"` // over the whole shard file
	Files  int    `json:"files"`
	Bytes  int64  `json:"bytes"` // plaintext bytes inside
}

type vaultMeta struct {
	Version    int              `json:"version"`
	KDF        vaultKDF         `json:"kdf"`
	WrappedKey string           `json:"wrappedKey"` // base64: nonce || GCM(KEK, masterKey)
	Verify     string           `json:"verify"`     // base64: nonce || GCM(masterKey, vaultVerifyText)
	Sealed     bool             `json:"sealed"`     // a seal run completed at least once / data currently in shards
	Shards     []vaultShardMeta `json:"shards"`
	TotalBytes int64            `json:"totalBytes"`
	UpdatedAt  string           `json:"updatedAt"`
}

// vaultDir sits NEXT TO the Data directory so sealing can empty
// Data completely while the header and shards stay put.
func vaultDir() string { return filepath.Join(filepath.Dir(dataDir()), "mj-vault") }

func vaultMetaPathIn(vaultRoot string) string { return filepath.Join(vaultRoot, "meta.json") }

func vaultLoadMeta(vaultRoot string) (*vaultMeta, error) {
	b, err := os.ReadFile(vaultMetaPathIn(vaultRoot))
	if err != nil {
		return nil, err
	}
	var m vaultMeta
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("保险库元数据解析失败：%w", err)
	}
	return &m, nil
}

func vaultSaveMeta(vaultRoot string, m *vaultMeta) error {
	if err := os.MkdirAll(vaultRoot, 0o755); err != nil {
		return err
	}
	m.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
	b, err := json.MarshalIndent(m, "", " ")
	if err != nil {
		return err
	}
	tmp := vaultMetaPathIn(vaultRoot) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, vaultMetaPathIn(vaultRoot)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// ---------- crypto primitives ----------

func vaultDeriveKEK(password string, kdf vaultKDF) ([]byte, error) {
	salt, err := base64.StdEncoding.DecodeString(kdf.Salt)
	if err != nil {
		return nil, fmt.Errorf("保险库盐值损坏：%w", err)
	}
	return argon2.IDKey([]byte(password), salt, kdf.Time, kdf.Memory, kdf.Threads, vaultKeyLen), nil
}

func vaultGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// vaultSealWithKey GCM-seals plaintext under key with a fresh
// random nonce; output = nonce || ciphertext.
func vaultSealWithKey(key, plaintext []byte) (string, error) {
	g, err := vaultGCM(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := g.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(out), nil
}

func vaultOpenWithKey(key []byte, encoded string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	g, err := vaultGCM(key)
	if err != nil {
		return nil, err
	}
	ns := g.NonceSize()
	if len(raw) < ns {
		return nil, errors.New("密文太短")
	}
	return g.Open(nil, raw[:ns], raw[ns:], nil)
}

// vaultNewMeta creates the header for a fresh master key wrapped
// under password, using the production KDF parameters.
func vaultNewMeta(password string, masterKey []byte) (*vaultMeta, error) {
	return vaultNewMetaParams(password, masterKey, vaultKDF{
		Time: vaultKDFTime, Memory: vaultKDFMemory, Threads: vaultKDFThreads,
	})
}

func vaultNewMetaParams(password string, masterKey []byte, kdf vaultKDF) (*vaultMeta, error) {
	salt := make([]byte, vaultSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	kdf.Salt = base64.StdEncoding.EncodeToString(salt)
	kek, err := vaultDeriveKEK(password, kdf)
	if err != nil {
		return nil, err
	}
	wrapped, err := vaultSealWithKey(kek, masterKey)
	if err != nil {
		return nil, err
	}
	verify, err := vaultSealWithKey(masterKey, []byte(vaultVerifyText))
	if err != nil {
		return nil, err
	}
	return &vaultMeta{Version: 1, KDF: kdf, WrappedKey: wrapped, Verify: verify}, nil
}

// vaultVerifyPassword checks the password against the header
// ONLY (unwrap + verify token) and returns the master key. It
// never reads a data shard -- the tests pin shard mtimes across
// verification.
func vaultVerifyPassword(m *vaultMeta, password string) ([]byte, error) {
	kek, err := vaultDeriveKEK(password, m.KDF)
	if err != nil {
		return nil, err
	}
	master, err := vaultOpenWithKey(kek, m.WrappedKey)
	if err != nil {
		return nil, errors.New("密码不对")
	}
	plain, err := vaultOpenWithKey(master, m.Verify)
	if err != nil || string(plain) != vaultVerifyText {
		return nil, errors.New("密码不对")
	}
	return master, nil
}

// vaultRewrap produces a header for the SAME master key under a
// new password (fresh salt): password change in O(1), data never
// re-encrypted.
func vaultRewrap(m *vaultMeta, masterKey []byte, newPassword string) (*vaultMeta, error) {
	fresh, err := vaultNewMeta(newPassword, masterKey)
	if err != nil {
		return nil, err
	}
	fresh.Sealed = m.Sealed
	fresh.Shards = m.Shards
	fresh.TotalBytes = m.TotalBytes
	return fresh, nil
}

// ---------- shard format ----------
//
// Layout: magic(4) | headerLen uint32be | headerJSON |
//         per file, per 1MiB chunk: ctLen uint32be | ciphertext.
// Chunk nonce = shardNonceBase(8) || chunkCounter uint32be,
// the counter running across the whole shard, so every chunk in
// a shard has a unique nonce under the master key.

type vaultShardHeader struct {
	Nonce string `json:"nonce"` // base64, 8 bytes
	Files []struct {
		Path string `json:"p"`
		Size int64  `json:"s"`
	} `json:"files"`
}

func vaultChunkNonce(base []byte, counter uint32) []byte {
	n := make([]byte, 12)
	copy(n, base)
	binary.BigEndian.PutUint32(n[8:], counter)
	return n
}

// vaultWriteShard encrypts files (slash-relative paths under
// dataRoot) into vaultRoot/name and returns its manifest entry.
// The write is tmp + fsync + rename: a shard file under its final
// name is always complete.
func vaultWriteShard(dataRoot, vaultRoot, name string, files []string, key []byte, progress func(int64)) (vaultShardMeta, error) {
	out := vaultShardMeta{Name: name}
	if err := os.MkdirAll(vaultRoot, 0o755); err != nil {
		return out, err
	}
	g, err := vaultGCM(key)
	if err != nil {
		return out, err
	}
	base := make([]byte, 8)
	if _, err := rand.Read(base); err != nil {
		return out, err
	}
	hdr := vaultShardHeader{Nonce: base64.StdEncoding.EncodeToString(base)}
	var sizes []int64
	for _, rel := range files {
		st, err := os.Stat(filepath.Join(dataRoot, filepath.FromSlash(rel)))
		if err != nil {
			return out, fmt.Errorf("读取待加密文件失败 %s：%w", rel, err)
		}
		hdr.Files = append(hdr.Files, struct {
			Path string `json:"p"`
			Size int64  `json:"s"`
		}{rel, st.Size()})
		sizes = append(sizes, st.Size())
	}
	hdrJSON, err := json.Marshal(hdr)
	if err != nil {
		return out, err
	}
	tmp := filepath.Join(vaultRoot, name+".tmp")
	f, err := os.Create(tmp)
	if err != nil {
		return out, err
	}
	h := sha256.New()
	w := io.MultiWriter(f, h)
	writeAll := func(b []byte) error {
		_, err := w.Write(b)
		return err
	}
	fail := func(err error) (vaultShardMeta, error) {
		_ = f.Close()
		_ = os.Remove(tmp)
		return out, err
	}
	if err := writeAll([]byte(vaultMagic)); err != nil {
		return fail(err)
	}
	var lb [4]byte
	binary.BigEndian.PutUint32(lb[:], uint32(len(hdrJSON)))
	if err := writeAll(lb[:]); err != nil {
		return fail(err)
	}
	if err := writeAll(hdrJSON); err != nil {
		return fail(err)
	}
	buf := make([]byte, vaultChunkSize)
	var counter uint32
	var plainTotal int64
	for i, rel := range files {
		in, err := os.Open(filepath.Join(dataRoot, filepath.FromSlash(rel)))
		if err != nil {
			return fail(fmt.Errorf("打开待加密文件失败 %s：%w", rel, err))
		}
		remaining := sizes[i]
		for remaining > 0 {
			want := int64(len(buf))
			if remaining < want {
				want = remaining
			}
			n, rerr := io.ReadFull(in, buf[:want])
			if rerr != nil && rerr != io.ErrUnexpectedEOF {
				_ = in.Close()
				return fail(fmt.Errorf("读取待加密文件失败 %s：%w", rel, rerr))
			}
			ct := g.Seal(nil, vaultChunkNonce(base, counter), buf[:n], nil)
			counter++
			binary.BigEndian.PutUint32(lb[:], uint32(len(ct)))
			if err := writeAll(lb[:]); err != nil {
				_ = in.Close()
				return fail(err)
			}
			if err := writeAll(ct); err != nil {
				_ = in.Close()
				return fail(err)
			}
			plainTotal += int64(n)
			remaining -= int64(n)
			if progress != nil {
				progress(int64(n))
			}
			if int64(n) < want {
				break
			}
		}
		_ = in.Close()
	}
	if err := f.Sync(); err != nil {
		return fail(err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return out, err
	}
	final := filepath.Join(vaultRoot, name)
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return out, err
	}
	st, err := os.Stat(final)
	if err != nil {
		return out, err
	}
	out.Size = st.Size()
	out.SHA256 = hex.EncodeToString(h.Sum(nil))
	out.Files = len(files)
	out.Bytes = plainTotal
	return out, nil
}

// vaultReadShardHeader parses only the header of a shard file.
func vaultReadShardHeader(path string) (*vaultShardHeader, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	prefix := make([]byte, 8)
	if _, err := io.ReadFull(f, prefix); err != nil {
		return nil, 0, err
	}
	if string(prefix[:4]) != vaultMagic {
		return nil, 0, errors.New("不是保险库分片文件")
	}
	hl := int64(binary.BigEndian.Uint32(prefix[4:]))
	if hl <= 0 || hl > 64*1024*1024 {
		return nil, 0, errors.New("分片头长度异常")
	}
	hb := make([]byte, hl)
	if _, err := io.ReadFull(f, hb); err != nil {
		return nil, 0, err
	}
	var hdr vaultShardHeader
	if err := json.Unmarshal(hb, &hdr); err != nil {
		return nil, 0, err
	}
	return &hdr, 8 + hl, nil
}

// vaultHashFile streams a SHA-256 over a file (shard integrity is
// verified BEFORE any decryption happens).
func vaultHashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// vaultDecryptShard verifies the shard's SHA-256, then streams
// every file back under dataRoot (overwriting what is there).
func vaultDecryptShard(dataRoot, vaultRoot string, sm vaultShardMeta, key []byte, progress func(int64)) error {
	path := filepath.Join(vaultRoot, sm.Name)
	sum, err := vaultHashFile(path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(sum, sm.SHA256) {
		return fmt.Errorf("分片 %s 校验失败（SHA-256 对不上），已停止解锁，数据未动", sm.Name)
	}
	hdr, hdrLen, err := vaultReadShardHeader(path)
	if err != nil {
		return fmt.Errorf("分片 %s 头损坏：%w", sm.Name, err)
	}
	base, err := base64.StdEncoding.DecodeString(hdr.Nonce)
	if err != nil || len(base) != 8 {
		return fmt.Errorf("分片 %s 的 nonce 损坏", sm.Name)
	}
	g, err := vaultGCM(key)
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Seek(hdrLen, io.SeekStart); err != nil {
		return err
	}
	var counter uint32
	var lb [4]byte
	for _, fe := range hdr.Files {
		dst := filepath.Join(dataRoot, filepath.FromSlash(fe.Path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		remaining := fe.Size
		for remaining > 0 {
			if _, err := io.ReadFull(f, lb[:]); err != nil {
				_ = out.Close()
				return fmt.Errorf("分片 %s 数据不完整：%w", sm.Name, err)
			}
			cl := int64(binary.BigEndian.Uint32(lb[:]))
			if cl <= 0 || cl > vaultChunkSize+64 {
				_ = out.Close()
				return fmt.Errorf("分片 %s 块长度异常", sm.Name)
			}
			ct := make([]byte, cl)
			if _, err := io.ReadFull(f, ct); err != nil {
				_ = out.Close()
				return fmt.Errorf("分片 %s 数据不完整：%w", sm.Name, err)
			}
			pt, err := g.Open(nil, vaultChunkNonce(base, counter), ct, nil)
			counter++
			if err != nil {
				_ = out.Close()
				return fmt.Errorf("分片 %s 解密失败：%w", sm.Name, err)
			}
			if _, err := out.Write(pt); err != nil {
				_ = out.Close()
				return err
			}
			remaining -= int64(len(pt))
			if progress != nil {
				progress(int64(len(pt)))
			}
		}
		if err := out.Close(); err != nil {
			return err
		}
	}
	return nil
}

// ---------- whole-tree seal / restore ----------

// vaultListDataFiles enumerates every regular file under root as
// sorted slash-relative paths. Symlinks to files are followed
// (their content is what gets sealed; restore writes a regular
// file). Dangling symlinks seal as empty files.
func vaultListDataFiles(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, serr := os.Stat(p) // follows symlinks on purpose
		if serr == nil && !info.Mode().IsRegular() {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

func vaultCountDataFiles(root string) int {
	files, err := vaultListDataFiles(root)
	if err != nil {
		return -1
	}
	return len(files)
}

// vaultSealTree encrypts the whole dataRoot into shards under
// vaultRoot, updating and persisting meta after EVERY shard: a
// shard's sources are deleted only once the shard is durable and
// recorded. meta.Shards must be empty when called (a previous
// partial state is restored first, never sealed over).
func vaultSealTree(dataRoot, vaultRoot string, key []byte, meta *vaultMeta, progress func(done, total int64)) error {
	if strings.HasPrefix(filepath.Clean(vaultRoot), filepath.Clean(dataRoot)+string(filepath.Separator)) {
		return errors.New("保险库目录不能在 Data 里面")
	}
	if len(meta.Shards) > 0 {
		// Shard files are numbered from 1 per seal run: sealing
		// over a half-sealed state would overwrite recorded
		// shards and corrupt the manifest. The one legal move
		// from a partial state is restore (unlock) first.
		return errors.New("已存在密封分片（半密封状态），必须先解锁恢复，不能直接续封")
	}
	files, err := vaultListDataFiles(dataRoot)
	if err != nil {
		return err
	}
	var total int64
	sizes := make(map[string]int64, len(files))
	for _, rel := range files {
		if st, err := os.Stat(filepath.Join(dataRoot, filepath.FromSlash(rel))); err == nil {
			sizes[rel] = st.Size()
			total += st.Size()
		}
	}
	meta.TotalBytes = total
	var done int64
	report := func() {
		if progress != nil {
			progress(done, total)
		}
	}
	report()
	// Group into shards: ~vaultShardBytes of plaintext or
	// vaultShardFiles files, whichever comes first.
	var groups [][]string
	var cur []string
	var curBytes int64
	flush := func() {
		if len(cur) > 0 {
			groups = append(groups, cur)
			cur, curBytes = nil, 0
		}
	}
	for _, rel := range files {
		if len(cur) > 0 && (curBytes+sizes[rel] > vaultShardBytes || len(cur) >= vaultShardFiles) {
			flush()
		}
		cur = append(cur, rel)
		curBytes += sizes[rel]
	}
	flush()
	for i, group := range groups {
		name := fmt.Sprintf("shard-%04d.mjv", i+1)
		sm, err := vaultWriteShard(dataRoot, vaultRoot, name, group, key, func(n int64) {
			done += n
			report()
		})
		if err != nil {
			return fmt.Errorf("加密分片 %s 失败：%w", name, err)
		}
		meta.Shards = append(meta.Shards, sm)
		if err := vaultSaveMeta(vaultRoot, meta); err != nil {
			return fmt.Errorf("记录保险库清单失败：%w", err)
		}
		// The shard is durable AND recorded: only now may its
		// sources go away.
		for _, rel := range group {
			_ = os.Remove(filepath.Join(dataRoot, filepath.FromSlash(rel)))
		}
		vaultPruneEmptyDirs(dataRoot)
	}
	meta.Sealed = true
	return vaultSaveMeta(vaultRoot, meta)
}

// vaultPruneEmptyDirs removes empty directories under root
// (deepest first), never root itself. Cosmetic -- empty dirs hold
// no data -- but a sealed Data should LOOK empty too.
func vaultPruneEmptyDirs(root string) {
	var dirs []string
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() && p != root {
			dirs = append(dirs, p)
		}
		return nil
	})
	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
	for _, d := range dirs {
		_ = os.Remove(d) // fails silently when not empty: fine
	}
}

// vaultRestoreTree decrypts every shard in meta back into
// dataRoot. Before writing, each top-level entry covered by the
// shards has its live counterpart removed: anything sitting in
// Data while sealed can only be post-seal residue (e.g. the
// throwaway lock-screen browser profile), and the sealed copy is
// the truth. Shards are deleted only after ALL of them restored
// cleanly; a failure leaves meta untouched so unlock can retry.
func vaultRestoreTree(dataRoot, vaultRoot string, key []byte, meta *vaultMeta, progress func(done, total int64)) error {
	var total int64
	for _, sm := range meta.Shards {
		total += sm.Bytes
	}
	// Prepare top-level counterparts.
	prepared := map[string]bool{}
	for _, sm := range meta.Shards {
		hdr, _, err := vaultReadShardHeader(filepath.Join(vaultRoot, sm.Name))
		if err != nil {
			return fmt.Errorf("读分片 %s 的目录失败：%w", sm.Name, err)
		}
		for _, fe := range hdr.Files {
			top := strings.SplitN(fe.Path, "/", 2)[0]
			if prepared[top] {
				continue
			}
			prepared[top] = true
			live := filepath.Join(dataRoot, filepath.FromSlash(top))
			if live != dataRoot {
				_ = os.RemoveAll(live)
			}
		}
	}
	var done int64
	report := func() {
		if progress != nil {
			progress(done, total)
		}
	}
	report()
	for _, sm := range meta.Shards {
		if err := vaultDecryptShard(dataRoot, vaultRoot, sm, key, func(n int64) {
			done += n
			report()
		}); err != nil {
			return err
		}
	}
	for _, sm := range meta.Shards {
		_ = os.Remove(filepath.Join(vaultRoot, sm.Name))
	}
	meta.Shards = nil
	meta.Sealed = false
	return vaultSaveMeta(vaultRoot, meta)
}

// ---------- runtime state ----------

const (
	vaultPhaseOff       = "off"       // never enabled
	vaultPhaseUnlocked  = "unlocked"  // enabled, session open, key in RAM
	vaultPhaseGate      = "gate"      // enabled, booted with plaintext data: password required before any service starts
	vaultPhaseSealed    = "sealed"    // locked: data lives in shards only
	vaultPhaseSealing   = "sealing"   // a seal job is running
	vaultPhaseUnlocking = "unlocking" // an unlock/restore job is running
)

var vaultSt = struct {
	sync.Mutex
	phase    string
	key      []byte // master key, RAM only, wiped on seal
	meta     *vaultMeta
	done     int64 // job progress bytes
	total    int64
	partial  bool // shards exist AND plaintext files remain (interrupted seal/restore)
	detected bool
}{
	phase: vaultPhaseOff,
}

// vaultFailedUnlocks counts wrong-password attempts during the
// current locked period. Those attempts are logged live, but the
// log FILE is sealed away at that moment -- so the count is
// re-stated into the log right after a successful unlock: "someone
// tried passwords while you were away" must survive in the record.
var vaultFailedUnlocks atomic.Int64

// vaultLocked reports the states in which NOTHING may touch Data:
// gate (password first), sealed, and the two transient jobs.
func vaultLocked() bool {
	vaultSt.Lock()
	defer vaultSt.Unlock()
	switch vaultSt.phase {
	case vaultPhaseGate, vaultPhaseSealed, vaultPhaseSealing, vaultPhaseUnlocking:
		return true
	}
	return false
}

// vaultDataInaccessible gates best-effort background writers
// (notify persistence, ...) while Data is sealed or moving.
func vaultDataInaccessible() bool { return vaultLocked() }

func vaultPhase() string {
	vaultSt.Lock()
	defer vaultSt.Unlock()
	return vaultSt.phase
}

func vaultSetPhase(p string) {
	vaultSt.Lock()
	vaultSt.phase = p
	vaultSt.Unlock()
}

func vaultEnabled() bool {
	vaultSt.Lock()
	defer vaultSt.Unlock()
	return vaultSt.meta != nil
}

func vaultSetKey(key []byte) {
	vaultSt.Lock()
	vaultSt.key = key
	vaultSt.Unlock()
}

func vaultWipeKey() {
	vaultSt.Lock()
	for i := range vaultSt.key {
		vaultSt.key[i] = 0
	}
	vaultSt.key = nil
	vaultSt.Unlock()
}

func vaultKey() []byte {
	vaultSt.Lock()
	defer vaultSt.Unlock()
	return vaultSt.key
}

// vaultDetect runs FIRST in runServer, before anything reads or
// writes Data (and before the first logf can open Data/logs):
// it classifies the on-disk state from the header alone.
func vaultDetect() {
	meta, err := vaultLoadMeta(vaultDir())
	vaultSt.Lock()
	defer vaultSt.Unlock()
	vaultSt.detected = true
	if err != nil {
		vaultSt.phase = vaultPhaseOff
		return
	}
	vaultSt.meta = meta
	files := vaultCountDataFiles(dataDir())
	switch {
	case len(meta.Shards) == 0:
		vaultSt.phase = vaultPhaseGate
	case files == 0:
		vaultSt.phase = vaultPhaseSealed
	default:
		// Shards exist AND plaintext remains: an interrupted seal
		// or restore. Locked either way; the next successful
		// unlock rolls everything back to plaintext.
		vaultSt.phase = vaultPhaseSealed
		vaultSt.partial = true
	}
	if vaultSt.phase == vaultPhaseSealed {
		// The on-disk log must not be created inside a sealed
		// Data: logging is memory-only until unlock.
		diagHoldFile.Store(true)
	}
}

// vaultStatusMap is the lock screen / settings view of the vault.
func vaultStatusMap() map[string]any {
	vaultSt.Lock()
	defer vaultSt.Unlock()
	m := map[string]any{
		"enabled": vaultSt.meta != nil,
		"phase":   vaultSt.phase,
		"locked": vaultSt.phase == vaultPhaseGate || vaultSt.phase == vaultPhaseSealed ||
			vaultSt.phase == vaultPhaseSealing || vaultSt.phase == vaultPhaseUnlocking,
		"partial": vaultSt.partial,
		"hasKey":  vaultSt.key != nil,
	}
	if vaultSt.meta != nil {
		m["shards"] = len(vaultSt.meta.Shards)
		m["totalBytes"] = vaultSt.meta.TotalBytes
		m["sealed"] = vaultSt.meta.Sealed
	}
	if vaultSt.phase == vaultPhaseSealing || vaultSt.phase == vaultPhaseUnlocking {
		m["progress"] = vaultSt.done
		m["progressTotal"] = vaultSt.total
	}
	return m
}

func vaultSetProgress(done, total int64) {
	vaultSt.Lock()
	vaultSt.done, vaultSt.total = done, total
	vaultSt.Unlock()
}

// ---------- busy blockers ----------

// vaultBusyBlockers names long-running writers into Data that
// must finish before a seal may start (an isolation install
// mid-flight, a component download, ...). Sealing over the top
// of one would corrupt its state, so lock/exit-seal refuse and
// say who is in the way.
func vaultBusyBlockers() []string {
	var out []string
	if sbieCompBusy() {
		out = append(out, "隔离组件正在安装/卸载")
	}
	if sbieBoxBusy() {
		out = append(out, "隔离安装正在进行")
	}
	upd.mu.Lock()
	if upd.Phase == "downloading" {
		out = append(out, "软件更新正在下载")
	}
	upd.mu.Unlock()
	if ocBusy() {
		out = append(out, "小龙虾组件正在下载/安装")
	}
	if ph, _, _, _ := ffSt.get(); ph == "downloading" {
		out = append(out, "Firefox 组件正在下载")
	}
	return out
}

// ---------- quiesce + seal (shared by lock screen and exits) ----------

// vaultQuiesceForSeal makes the daemon stop touching Data, in an
// order that keeps the console log complete for as long as
// possible: gates first, window/app shutdown, subsystems, core,
// and the log file handle last.
func vaultQuiesceForSeal() {
	servicesSetActive(false)
	monHold.Store(true)
	// Wait (bounded) for an in-flight server sample to land its
	// writes before its directories are sealed away.
	for i := 0; i < 100 && monInFlight.Load() > 0; i++ {
		time.Sleep(100 * time.Millisecond)
	}
	immerseRestoreAllForExit()
	grpApplyEvent(evRestoreAll)
	closeAppsForExit()
	remoteShutdown()
	ocStopForShutdown()
	coreMu.Lock()
	stopCoreLocked()
	coreMu.Unlock()
	// The log file lives in Data/logs: from here to unlock,
	// logging is memory-only.
	diagHoldFile.Store(true)
	diagCloseFileForVault()
}

// vaultSealData runs blockers -> quiesce -> seal for the LOCK
// path (daemon stays alive, locked). The caller flips the phase
// to sealing first so the endpoint gate engages immediately.
func vaultSealData() error {
	if blockers := vaultBusyBlockers(); len(blockers) > 0 {
		return fmt.Errorf("暂时不能上锁：%s，等它完成再试", strings.Join(blockers, "、"))
	}
	key := vaultKey()
	if key == nil {
		return errors.New("保险库密钥不在内存里（本会话还没解锁过），无法上锁")
	}
	vaultQuiesceForSeal()
	vaultSt.Lock()
	meta := vaultSt.meta
	vaultSt.Unlock()
	if meta == nil {
		return errors.New("保险库未启用")
	}
	logf("vault", "info", "保险库开始上锁（整个 Data 目录）")
	err := vaultSealTree(dataDir(), vaultDir(), key, meta, vaultSetProgress)
	vaultSt.Lock()
	vaultSt.meta = meta
	vaultSt.Unlock()
	if err != nil {
		logf("vault", "error", "保险库上锁未完成：%v（已密封的部分保留，解锁后自动回滚恢复）", err)
		return err
	}
	vaultWipeKey()
	vaultSetPhase(vaultPhaseSealed)
	logf("vault", "info", "保险库已上锁：%d 个分片，共 %s，Data 已清空", len(meta.Shards), fmtBytes(meta.TotalBytes))
	return nil
}

// vaultSealAtExit is the seal half of requestDaemonExit: the
// exit sequence has already closed the apps and stopped the
// core/subsystems, so only the seal-specific tail remains
// (sampling hold, log file, then the shards). A blocker or a
// missing in-RAM key is an error the caller logs -- the exit
// itself always proceeds.
func vaultSealAtExit() error {
	if blockers := vaultBusyBlockers(); len(blockers) > 0 {
		return fmt.Errorf("有任务正在写 Data：%s", strings.Join(blockers, "、"))
	}
	key := vaultKey()
	if key == nil {
		return errors.New("主密钥不在内存（本会话未解锁），无法上锁")
	}
	servicesSetActive(false)
	monHold.Store(true)
	for i := 0; i < 100 && monInFlight.Load() > 0; i++ {
		time.Sleep(100 * time.Millisecond)
	}
	diagHoldFile.Store(true)
	diagCloseFileForVault()
	vaultSetPhase(vaultPhaseSealing)
	vaultSt.Lock()
	meta := vaultSt.meta
	vaultSt.Unlock()
	if meta == nil {
		return errors.New("保险库未启用")
	}
	logf("vault", "info", "保险库开始上锁（整个 Data 目录）")
	err := vaultSealTree(dataDir(), vaultDir(), key, meta, vaultSetProgress)
	vaultSt.Lock()
	vaultSt.meta = meta
	vaultSt.Unlock()
	if err != nil {
		logf("vault", "error", "保险库上锁未完成：%v（已密封的部分保留，解锁后自动回滚恢复）", err)
		return err
	}
	vaultWipeKey()
	vaultSetPhase(vaultPhaseSealed)
	logf("vault", "info", "保险库已上锁：%d 个分片，共 %s，Data 已清空", len(meta.Shards), fmtBytes(meta.TotalBytes))
	return nil
}

// ---------- local-mux gate ----------

// vaultGateMiddleware: while the vault is locked, the local mux
// serves ONLY what the lock screen needs (the shell page, the
// vault status/unlock endpoints, the shell opener, a settings
// GET for the page's own boot, and shutdown). Everything else
// is refused before it can touch Data.
func vaultGateMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if vaultLocked() && !vaultGateAllowed(r) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "终端机已锁定，输入密码解锁后继续"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func vaultGateAllowed(r *http.Request) bool {
	switch r.URL.Path {
	case "/", "/api/system", "/api/vault/status", "/api/vault/unlock", "/api/shutdown", "/api/openshell":
		return true
	case "/api/settings":
		return r.Method == http.MethodGet
	}
	return false
}

func fmtBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.2f GB", float64(n)/float64(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// ---------- HTTP handlers (local mux only -- never remote) ----------

func vaultStatusHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, vaultStatusMap())
}

func vaultEnableHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if len([]rune(body.Password)) < 6 {
		writeJSON(w, map[string]any{"error": "密码至少 6 位"})
		return
	}
	vaultSt.Lock()
	if vaultSt.meta != nil {
		vaultSt.Unlock()
		writeJSON(w, map[string]any{"error": "保险库已经启用了"})
		return
	}
	vaultSt.Unlock()
	master := make([]byte, vaultKeyLen)
	if _, err := rand.Read(master); err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	meta, err := vaultNewMeta(body.Password, master)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	if err := vaultSaveMeta(vaultDir(), meta); err != nil {
		logf("vault", "error", "保险库启用失败（写元数据）：%v", err)
		writeJSON(w, map[string]any{"error": "启用失败：" + err.Error()})
		return
	}
	vaultSt.Lock()
	vaultSt.meta = meta
	vaultSt.key = master
	vaultSt.phase = vaultPhaseUnlocked
	vaultSt.Unlock()
	logf("vault", "info", "保险库已启用（整个 Data 目录将在退出/锁屏时加密；忘记密码无法找回）")
	writeJSON(w, map[string]any{"ok": true})
}

func vaultUnlockHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	vaultSt.Lock()
	meta := vaultSt.meta
	phase := vaultSt.phase
	vaultSt.Unlock()
	if meta == nil {
		writeJSON(w, map[string]any{"error": "保险库未启用"})
		return
	}
	if phase != vaultPhaseGate && phase != vaultPhaseSealed {
		writeJSON(w, map[string]any{"error": "当前不在锁定状态"})
		return
	}
	// Verification reads ONLY the header (meta.json) -- never a
	// data shard. A wrong password changes nothing on disk.
	master, err := vaultVerifyPassword(meta, body.Password)
	if err != nil {
		vaultFailedUnlocks.Add(1)
		logf("vault", "info", "保险库解锁失败：密码不对")
		writeJSON(w, map[string]any{"error": "密码不对"})
		return
	}
	vaultSetKey(master)
	if len(meta.Shards) == 0 {
		// Gate mode: data is plaintext, nothing to decrypt.
		vaultSetPhase(vaultPhaseUnlocked)
		logf("vault", "info", "保险库已解锁（密码验证通过）")
		if n := vaultFailedUnlocks.Swap(0); n > 0 {
			logf("vault", "warn", "锁定期间共有 %d 次密码输错", n)
		}
		onVaultUnlocked()
		writeJSON(w, map[string]any{"ok": true})
		return
	}
	// Sealed (or interrupted): decrypt everything back. Async:
	// a multi-GB Data takes a while and the lock screen polls
	// the status endpoint for progress.
	vaultSetPhase(vaultPhaseUnlocking)
	writeJSON(w, map[string]any{"ok": true, "unlocking": true})
	go func() {
		metaCopy := *meta
		err := vaultRestoreTree(dataDir(), vaultDir(), master, &metaCopy, vaultSetProgress)
		if err != nil {
			logf("vault", "error", "保险库解锁恢复失败：%v", err)
			vaultWipeKey()
			vaultSetPhase(vaultPhaseSealed)
			return
		}
		vaultSt.Lock()
		vaultSt.meta = &metaCopy
		vaultSt.partial = false
		vaultSt.Unlock()
		vaultSetPhase(vaultPhaseUnlocked)
		diagHoldFile.Store(false)
		logf("vault", "info", "保险库已解锁：数据恢复完成（%s）", fmtBytes(metaCopy.TotalBytes))
		if n := vaultFailedUnlocks.Swap(0); n > 0 {
			logf("vault", "warn", "锁定期间共有 %d 次密码输错（明细只在当时控制台里，日志文件当时已上锁）", n)
		}
		onVaultUnlocked()
	}()
}

func vaultLockHandler(w http.ResponseWriter, r *http.Request) {
	vaultSt.Lock()
	phase := vaultSt.phase
	hasKey := vaultSt.key != nil
	meta := vaultSt.meta
	vaultSt.Unlock()
	if meta == nil {
		writeJSON(w, map[string]any{"error": "保险库未启用"})
		return
	}
	if phase != vaultPhaseUnlocked || !hasKey {
		writeJSON(w, map[string]any{"error": "当前状态不能上锁"})
		return
	}
	if blockers := vaultBusyBlockers(); len(blockers) > 0 {
		writeJSON(w, map[string]any{"error": "暂时不能上锁：" + strings.Join(blockers, "、") + "，等它完成再试"})
		return
	}
	// Flip the phase first: the local endpoint gate engages at
	// once, the seal itself runs in the background (the lock
	// screen polls status for progress).
	vaultSetPhase(vaultPhaseSealing)
	logf("vault", "info", "用户锁定屏幕，开始上锁")
	writeJSON(w, map[string]any{"ok": true, "message": "正在上锁"})
	go func() {
		if err := vaultSealData(); err != nil {
			// vaultSealData has already moved what it could; any
			// state it leaves is consistent and recoverable, and
			// the safest posture is to STAY locked (services off,
			// unlock rolls back). The error is in the console.
			vaultSetPhase(vaultPhaseSealed)
		}
	}()
}

func vaultChangePwHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Old string `json:"old"`
		New string `json:"new"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if len([]rune(body.New)) < 6 {
		writeJSON(w, map[string]any{"error": "新密码至少 6 位"})
		return
	}
	vaultSt.Lock()
	meta := vaultSt.meta
	vaultSt.Unlock()
	if meta == nil {
		writeJSON(w, map[string]any{"error": "保险库未启用"})
		return
	}
	master, err := vaultVerifyPassword(meta, body.Old)
	if err != nil {
		writeJSON(w, map[string]any{"error": "旧密码不对"})
		return
	}
	fresh, err := vaultRewrap(meta, master, body.New)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	if err := vaultSaveMeta(vaultDir(), fresh); err != nil {
		writeJSON(w, map[string]any{"error": "保存失败：" + err.Error()})
		return
	}
	vaultSt.Lock()
	vaultSt.meta = fresh
	vaultSt.Unlock()
	vaultSetKey(master)
	logf("vault", "info", "保险库密码已修改（只重包裹主密钥，数据未重加密）")
	writeJSON(w, map[string]any{"ok": true})
}

func vaultDisableHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	vaultSt.Lock()
	meta := vaultSt.meta
	phase := vaultSt.phase
	vaultSt.Unlock()
	if meta == nil {
		writeJSON(w, map[string]any{"error": "保险库未启用"})
		return
	}
	if phase != vaultPhaseUnlocked {
		writeJSON(w, map[string]any{"error": "只能在解锁状态停用保险库"})
		return
	}
	if len(meta.Shards) > 0 {
		writeJSON(w, map[string]any{"error": "数据还在分片里，先完成一次解锁恢复再停用"})
		return
	}
	if _, err := vaultVerifyPassword(meta, body.Password); err != nil {
		writeJSON(w, map[string]any{"error": "密码不对"})
		return
	}
	if err := os.RemoveAll(vaultDir()); err != nil {
		writeJSON(w, map[string]any{"error": "删除保险库元数据失败：" + err.Error()})
		return
	}
	vaultSt.Lock()
	vaultSt.meta = nil
	vaultSt.Unlock()
	vaultWipeKey()
	vaultSetPhase(vaultPhaseOff)
	logf("vault", "info", "保险库已停用（数据保持明文，退出不再上锁）")
	writeJSON(w, map[string]any{"ok": true})
}
