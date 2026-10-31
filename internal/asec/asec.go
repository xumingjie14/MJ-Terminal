// Package asec is MJ Terminal's single cryptographic module
// (v0.61 architecture block 5: crypto consolidation). Every
// cryptographic operation the daemon performs lives behind
// this package, so the whole crypto surface can be audited by
// reading this one directory. A gate test (asec_gate_test.go in
// package main) fails the build if any other package imports
// crypto/* or golang.org/x/crypto primitives directly; the only
// exceptions are the entries of that test's allowlist (the SSH
// wire-protocol library), each justified there.
//
// Primitive + parameter inventory (the audit surface):
//
//   - Vault KDF: Argon2id (golang.org/x/crypto/argon2 IDKey),
//     deriving a 32-byte KEK from the user password. Parameters
//     are NOT chosen here: time/memory/threads/salt travel in
//     the vault header (mj-vault/meta.json) and are passed in by
//     the caller. The production values the vault seals with
//     are time=3, memory=64 MiB (65536 KiB), threads=4, salt=16
//     random bytes (see vault.go's vaultKDF* constants). This
//     split is what lets a header written by any version be
//     opened by any other. (v0.61: unchanged.)
//   - Vault AEAD: AES-256-GCM (crypto/aes + crypto/cipher
//     NewGCM, standard 12-byte nonce, 16-byte tag). Two wire
//     shapes, both frozen by the on-disk vault format:
//     SealWithKey/OpenWithKey move nonce || ciphertext as one
//     base64 string (wrapped key + verify token in the header);
//     shard payloads are chunked by the vault itself with
//     nonces derived from a per-shard random base + counter,
//     using the AEAD handed out by NewAESGCM.
//   - Randomness: crypto/rand only (FillRandom / RandomBytes).
//     Used for master keys, KDF salts, GCM nonces, shard nonce
//     bases, node/store secrets, OpenClaw tokens, WARP keys.
//   - Hashing: SHA-256 (crypto/sha256) for integrity digests --
//     update packages, component downloads, shard files, node
//     identity, pairing codes. Integrity only, never a KDF.
//   - Update manifest signatures: Ed25519 (crypto/ed25519).
//     The daemon VERIFIES ONLY: UpdateSigningPublicKey below is
//     the release signing key's public half, embedded at build
//     time; the private half exists only in the release
//     pipeline's secret store and as an offline backup file,
//     never in this repository and never in the binary. The
//     detached signature covers the exact update.json bytes and
//     is verified before the manifest is parsed (see update.go).
//   - X25519 (golang.org/x/crypto/curve25519): WireGuard key
//     agreement for WARP registration -- a fresh 32-byte random
//     scalar against the standard basepoint; the scalar is
//     handed to the WireGuard config unclamped, exactly as the
//     pre-consolidation code did.
//
// Changing any parameter or wire shape above is a format
// change, not a refactor: a vault sealed by v0.60 must open
// under v0.61, and the vault tests pin that.
package asec

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"os"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/curve25519"
)

// ---------- randomness (crypto/rand) ----------

// FillRandom fills b from the OS CSPRNG.
func FillRandom(b []byte) error {
	_, err := rand.Read(b)
	return err
}

// RandomBytes returns n fresh CSPRNG bytes.
func RandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if err := FillRandom(b); err != nil {
		return nil, err
	}
	return b, nil
}

// ---------- SHA-256 (integrity digests) ----------

// NewSHA256 returns a fresh SHA-256 hasher for streaming
// digests (download verification loops).
func NewSHA256() hash.Hash { return sha256.New() }

// Sum256 is the one-shot SHA-256 of b.
func Sum256(b []byte) [32]byte { return sha256.Sum256(b) }

// Sum256Hex is the hex SHA-256 of b.
func Sum256Hex(b []byte) string {
	sum := Sum256(b)
	return hex.EncodeToString(sum[:])
}

// HashFileHex streams a file's SHA-256 (hex). Used for vault
// shard integrity (verified BEFORE any decryption happens) and
// for downloaded-package checks.
func HashFileHex(path string) (string, error) {
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

// ---------- vault KDF + AEAD ----------

// DeriveVaultKEK runs Argon2id with the parameters carried by
// the vault header and returns a keyLen-byte key-encryption
// key. It never chooses parameters: the header does.
func DeriveVaultKEK(password string, salt []byte, time, memory uint32, threads uint8, keyLen uint32) []byte {
	return argon2.IDKey([]byte(password), salt, time, memory, threads, keyLen)
}

// AEAD is the AES-256-GCM construction the vault uses, aliased
// so vault code names this package's type, not crypto/cipher's.
type AEAD = cipher.AEAD

// NewAESGCM builds an AES-GCM AEAD from a raw key (16/24/32
// bytes; the vault always passes 32).
func NewAESGCM(key []byte) (AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// SealWithKey GCM-seals plaintext under key with a fresh random
// nonce; the wire form is base64(nonce || ciphertext) -- the
// vault header's wrapped-key and verify-token encoding.
func SealWithKey(key, plaintext []byte) (string, error) {
	g, err := NewAESGCM(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, g.NonceSize())
	if err := FillRandom(nonce); err != nil {
		return "", err
	}
	out := g.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(out), nil
}

// OpenWithKey reverses SealWithKey.
func OpenWithKey(key []byte, encoded string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	g, err := NewAESGCM(key)
	if err != nil {
		return nil, err
	}
	ns := g.NonceSize()
	if len(raw) < ns {
		return nil, errors.New("密文太短")
	}
	return g.Open(nil, raw[:ns], raw[ns:], nil)
}

// ---------- update manifest signatures (Ed25519) ----------

// UpdateSigningPublicKey is the public half of the release
// signing key, embedded in the daemon. Fingerprint (SHA-256 of
// the raw public key, hex):
// e6d7b9fc15b9ca4303d3a02fadd045e121977cd508c461287e79342bde6e4dc9.
// It is a var, not a const, so the updater's tests can swap in
// a fixture key; production code never writes it.
var UpdateSigningPublicKey = ed25519.PublicKey{
	0xa8, 0x29, 0xca, 0x58, 0x56, 0x90, 0xdd, 0xa6,
	0x65, 0x9f, 0x31, 0xa0, 0x5c, 0xd6, 0x36, 0x6a,
	0x57, 0xa8, 0xbe, 0xb3, 0x53, 0xc1, 0x72, 0x45,
	0x23, 0x18, 0x1a, 0xba, 0xa2, 0xde, 0xfa, 0x5b,
}

// VerifyEd25519 reports whether sig is a valid Ed25519
// signature of msg under pub. Malformed keys or signatures
// return false; this function never panics on garbage input.
func VerifyEd25519(pub ed25519.PublicKey, msg, sig []byte) bool {
	if len(pub) != ed25519.PublicKeySize || len(sig) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(pub, msg, sig)
}

// VerifyUpdateManifest verifies a detached release signature
// (the update.json.sig asset) over the exact update.json bytes
// under the embedded release key. The updater calls this BEFORE
// parsing the manifest; false means hard refuse.
func VerifyUpdateManifest(manifest, sig []byte) bool {
	return VerifyEd25519(UpdateSigningPublicKey, manifest, sig)
}

// ---------- X25519 (WARP / WireGuard key agreement) ----------

// NewX25519KeyPair generates a WireGuard key pair: a 32-byte
// random scalar (returned unclamped, as WireGuard tooling
// expects to clamp at use) and its basepoint public key.
func NewX25519KeyPair() (priv, pub []byte, err error) {
	priv, err = RandomBytes(32)
	if err != nil {
		return nil, nil, err
	}
	pub, err = curve25519.X25519(priv, curve25519.Basepoint)
	if err != nil {
		return nil, nil, err
	}
	return priv, pub, nil
}
