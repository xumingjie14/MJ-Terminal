package main

// vault_fuzz_test.go -- v0.61 architecture block 5: fuzz
// targets for the vault's metadata parsers. The header
// (mj-vault/meta.json) and the shard headers are files an
// attacker with folder access can rewrite at will; the unlock
// path parses them before any key material exists, so they
// must fail closed -- errors, never panics, never wild
// allocations (the shard header length is capped in the
// parser for exactly this reason).
//
// These targets deliberately exercise PARSING only. Password
// verification (Argon2id with header-carried parameters) is
// not fuzzed: its cost parameters come from this same header
// by design, and a fuzz corpus of KDF parameters proves
// nothing about parser safety.

import (
	"os"
	"path/filepath"
	"testing"
)

func FuzzVaultLoadMeta(f *testing.F) {
	f.Add([]byte(`{"version":1,"kdf":{"salt":"AAECAwQFBgcICQoLDA0ODw==","time":3,"memory":65536,"threads":4},"wrappedKey":"AAAA","verify":"BBBB","sealed":true,"shards":[],"totalBytes":0}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(``))
	f.Add([]byte(`{"kdf":{"salt":"!!!","time":0,"memory":0,"threads":0}}`))
	f.Add([]byte(`{"shards":[{"name":"shard-0001.mjv","size":-1,"sha256":"zz","files":-3,"bytes":-9}]}`))
	f.Add([]byte{0x00, 0xff, 0x7b, 0x22})
	f.Fuzz(func(t *testing.T, data []byte) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "meta.json"), data, 0o600); err != nil {
			t.Fatal(err)
		}
		_, _ = vaultLoadMeta(dir)
	})
}

func FuzzVaultShardHeader(f *testing.F) {
	// A genuine shard, produced by the real writer over one
	// tiny file, is the primary seed; the fuzzer mutates its
	// header length, header JSON and chunk framing.
	dataRoot := f.TempDir()
	vaultRoot := f.TempDir()
	if err := os.WriteFile(filepath.Join(dataRoot, "a.txt"), []byte("hello vault"), 0o644); err != nil {
		f.Fatal(err)
	}
	key := make([]byte, vaultKeyLen)
	for i := range key {
		key[i] = byte(i + 1)
	}
	if _, err := vaultWriteShard(dataRoot, vaultRoot, "shard-0001.mjv", []string{"a.txt"}, key, nil); err != nil {
		f.Fatal(err)
	}
	genuine, err := os.ReadFile(filepath.Join(vaultRoot, "shard-0001.mjv"))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(genuine)
	f.Add(genuine[:8])                    // prefix only: magic + huge/absent header
	f.Add([]byte("MJV1"))                 // magic alone
	f.Add([]byte("XXXX...."))             // wrong magic
	f.Add([]byte{})                       // empty
	f.Add([]byte("MJV1\xff\xff\xff\xff")) // header length at the cap boundary
	f.Fuzz(func(t *testing.T, data []byte) {
		p := filepath.Join(t.TempDir(), "shard.mjv")
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		_, _, _ = vaultReadShardHeader(p)
	})
}

// FuzzVaultOpenEncoded throws arbitrary strings at the wrapped
// key / verify-token opener: bad base64, truncated ciphertext
// and tag failures must all be plain errors.
func FuzzVaultOpenEncoded(f *testing.F) {
	key := make([]byte, vaultKeyLen)
	enc, err := vaultSealWithKey(key, []byte(vaultVerifyText))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(enc)
	f.Add("")
	f.Add("!!!")
	f.Add("AAAA")
	f.Add("AAECAwQFBgcICQoLDA0ODw==")
	f.Fuzz(func(t *testing.T, s string) {
		_, _ = vaultOpenWithKey(key, s)
	})
}
