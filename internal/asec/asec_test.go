package asec

// Unit pins for the consolidated crypto module: the vault wire
// shapes and the signature entry must behave exactly as the
// pre-consolidation vault.go / (new) updater code expects.

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

func TestSealOpenRoundtrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	enc, err := SealWithKey(key, []byte("MJ-VAULT-VERIFY\x01"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		t.Fatal(err)
	}
	// Wire shape: 12-byte GCM nonce || ciphertext+16-byte tag.
	if want := 12 + len("MJ-VAULT-VERIFY\x01") + 16; len(raw) != want {
		t.Fatalf("sealed wire length = %d, want %d (nonce||ct||tag)", len(raw), want)
	}
	pt, err := OpenWithKey(key, enc)
	if err != nil || string(pt) != "MJ-VAULT-VERIFY\x01" {
		t.Fatalf("open = %q, %v", pt, err)
	}
	if _, err := OpenWithKey(make([]byte, 32), enc); err == nil {
		t.Fatal("open under a different key must fail")
	}
	if _, err := OpenWithKey(key, "!!!not base64!!!"); err == nil {
		t.Fatal("garbage encoding must fail")
	}
	if _, err := OpenWithKey(key, base64.StdEncoding.EncodeToString([]byte("short"))); err == nil {
		t.Fatal("truncated ciphertext must fail")
	}
}

func TestDeriveVaultKEKDeterministic(t *testing.T) {
	salt := []byte("0123456789abcdef")
	a := DeriveVaultKEK("password", salt, 3, 64*1024, 4, 32)
	b := DeriveVaultKEK("password", salt, 3, 64*1024, 4, 32)
	if !bytes.Equal(a, b) || len(a) != 32 {
		t.Fatal("Argon2id derivation must be deterministic and 32 bytes")
	}
	c := DeriveVaultKEK("passwore", salt, 3, 64*1024, 4, 32)
	if bytes.Equal(a, c) {
		t.Fatal("different passwords must derive different keys")
	}
}

func TestVerifyEd25519GarbageNeverPanics(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte(`{"version":"0.61"}`)
	sig := ed25519.Sign(priv, msg)
	if !VerifyEd25519(pub, msg, sig) {
		t.Fatal("valid signature must verify")
	}
	for _, bad := range [][]byte{nil, {}, {1}, make([]byte, 63), make([]byte, 65), make([]byte, 64)} {
		if VerifyEd25519(pub, msg, bad) {
			t.Fatalf("signature %d bytes of garbage must not verify", len(bad))
		}
	}
	if VerifyEd25519(pub[:16], msg, sig) {
		t.Fatal("truncated public key must not verify")
	}
	if VerifyEd25519(nil, msg, sig) {
		t.Fatal("nil public key must not verify")
	}
	tampered := append([]byte{}, msg...)
	tampered[0] ^= 1
	if VerifyEd25519(pub, tampered, sig) {
		t.Fatal("tampered message must not verify")
	}
}

func TestEmbeddedUpdateKeyShape(t *testing.T) {
	if len(UpdateSigningPublicKey) != ed25519.PublicKeySize {
		t.Fatalf("embedded update key is %d bytes, want %d", len(UpdateSigningPublicKey), ed25519.PublicKeySize)
	}
	if got := Sum256Hex(UpdateSigningPublicKey); !strings.HasPrefix(got, "e6d7b9fc") {
		t.Fatalf("embedded update key fingerprint = %s, want the e6d7b9fc… release key", got)
	}
	if VerifyUpdateManifest([]byte("{}"), nil) {
		t.Fatal("no manifest may verify without a signature")
	}
}

func TestX25519KeyPair(t *testing.T) {
	priv, pub, err := NewX25519KeyPair()
	if err != nil {
		t.Fatal(err)
	}
	if len(priv) != 32 || len(pub) != 32 {
		t.Fatalf("key pair sizes = %d/%d, want 32/32", len(priv), len(pub))
	}
	priv2, pub2, err := NewX25519KeyPair()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(priv, priv2) || bytes.Equal(pub, pub2) {
		t.Fatal("two generated key pairs must differ")
	}
}

func TestHashFileHex(t *testing.T) {
	p := t.TempDir() + "/blob"
	content := []byte("mj-terminal")
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := HashFileHex(p); err != nil || got != Sum256Hex(content) {
		t.Fatalf("HashFileHex = %q, %v; want %q", got, err, Sum256Hex(content))
	}
	if _, err := HashFileHex(p + ".missing"); err == nil {
		t.Fatal("missing file must error")
	}
}
