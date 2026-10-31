//go:build ignore

// signmanifest_tool.go -- release tooling, NOT part of the
// daemon (the //go:build ignore tag keeps it out of every
// package build, test and vet run; it is invoked by file name).
//
// It signs an update manifest with the release Ed25519 key:
//
//	go run signmanifest_tool.go -key update-signing-key.pem -in update.json -out update.json.sig
//
// (Named-file `go run` is the point: the file is excluded from
// package builds by its build tag, but stays runnable as a
// standalone tool, with no module build state required -- it
// uses only the standard library.)
//
// The key is a PKCS#8 PEM ("PRIVATE KEY") Ed25519 key. The
// output is the raw 64-byte detached signature over the exact
// input bytes -- the update.json.sig release asset the daemon
// verifies (internal/asec.VerifyUpdateManifest) before it
// parses update.json.
//
// The CI workflow's signing step calls this with the key staged
// from the MJ_UPDATE_SIGNING_KEY secret; locally it exists so a
// manifest can be signed by hand with the offline key file.
package main

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"flag"
	"fmt"
	"os"
)

func run() error {
	keyPath := flag.String("key", "", "path to the Ed25519 private key (PKCS#8 PEM)")
	inPath := flag.String("in", "", "manifest file to sign (its exact bytes are signed)")
	outPath := flag.String("out", "", "where to write the raw 64-byte signature")
	flag.Parse()
	if *keyPath == "" || *inPath == "" || *outPath == "" {
		return fmt.Errorf("-key, -in and -out are all required")
	}
	pemBytes, err := os.ReadFile(*keyPath)
	if err != nil {
		return fmt.Errorf("read key: %w", err)
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return fmt.Errorf("key file carries no PEM block")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("parse key: %w", err)
	}
	priv, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return fmt.Errorf("key is %T, not an Ed25519 private key", parsed)
	}
	manifest, err := os.ReadFile(*inPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	sig := ed25519.Sign(priv, manifest)
	if len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("signature has unexpected size %d", len(sig))
	}
	if !ed25519.Verify(priv.Public().(ed25519.PublicKey), manifest, sig) {
		return fmt.Errorf("self-check failed: produced signature does not verify")
	}
	if err := os.WriteFile(*outPath, sig, 0o644); err != nil {
		return fmt.Errorf("write signature: %w", err)
	}
	fmt.Printf("signed %s (%d bytes) -> %s (%d bytes)\n", *inPath, len(manifest), *outPath, len(sig))
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "signmanifest:", err)
		os.Exit(1)
	}
}
