package main

// asec_gate_test.go -- v0.61 architecture block 5: the crypto
// consolidation gate, in the registry gate's style. All
// cryptographic operations live behind internal/asec (the one
// auditable module); this test scans every NON-TEST Go source
// in the module (root package + internal/*) and fails the
// build if a file imports a crypto/* or golang.org/x/crypto/*
// primitive directly, outside the allowlist below.
//
// Scope notes:
//   - Test files are excluded: they are not shipped, and their
//     crypto uses are fixture scaffolding (computing an
//     expected digest, generating a throwaway fixture key).
//     The shipped attack surface is what this gate audits.
//   - internal/asec itself is the module; it is skipped, not
//     allowlisted line by line.
//
// The allowlist is exact and anti-stale: an entry whose file
// no longer imports the named package fails the gate, so the
// list cannot quietly grow vestigial exceptions.

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// cryptoAllowlist maps a source file to the crypto imports it
// may carry, each with its justification. Everything else must
// go through internal/asec.
var cryptoAllowlist = map[string]map[string]string{
	// The SSH wire protocol is a transport library, not a
	// general-purpose crypto primitive: key exchange, channel
	// multiplexing and the agent protocol cannot be
	// re-implemented behind asec. These files import ONLY the
	// ssh package from x/crypto (the gate rejects any other
	// x/crypto import in them, e.g. argon2 or curve25519).
	"ssh.go":             {"golang.org/x/crypto/ssh": "SSH wire-protocol library (transport)"},
	"servermon.go":       {"golang.org/x/crypto/ssh": "SSH wire-protocol library (transport)"},
	"servermon_agent.go": {"golang.org/x/crypto/ssh": "SSH wire-protocol library (transport)"},
	// The release signing tool: //go:build ignore, never
	// compiled into the daemon or any ./... build. It is the
	// PRIVATE side of the update signature -- by design it
	// lives outside the shipped binary's attack surface.
	"signmanifest_tool.go": {
		"crypto/ed25519": "release signing tool (build-ignored, private-key side)",
		"crypto/x509":    "release signing tool (build-ignored, PKCS#8 key parsing)",
	},
}

func isCryptoImport(path string) bool {
	return strings.HasPrefix(path, "crypto/") || strings.HasPrefix(path, "golang.org/x/crypto/")
}

func TestCryptoConsolidationGate(t *testing.T) {
	var files []string
	root, _ := filepath.Glob("*.go")
	files = append(files, root...)
	internal, _ := filepath.Glob("internal/*/*.go")
	files = append(files, internal...)
	if len(files) < 40 {
		t.Fatalf("source scan found only %d Go files; the scan itself is suspect", len(files))
	}
	seenAllow := map[string]map[string]bool{}
	fset := token.NewFileSet()
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue // fixture scaffolding, not shipped code (see header)
		}
		if strings.HasPrefix(filepath.ToSlash(path), "internal/asec/") {
			continue // the crypto module itself
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		base := filepath.Base(path)
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if !isCryptoImport(p) {
				continue
			}
			if allowed, ok := cryptoAllowlist[base][p]; ok {
				_ = allowed
				if seenAllow[base] == nil {
					seenAllow[base] = map[string]bool{}
				}
				seenAllow[base][p] = true
				continue
			}
			t.Errorf("%s imports %q directly -- cryptographic operations belong in internal/asec (or need a justified allowlist entry in asec_gate_test.go)", path, p)
		}
	}
	// Anti-staleness: every allowlist entry must name a real,
	// still-present import.
	for file, imports := range cryptoAllowlist {
		for p := range imports {
			if !seenAllow[file][p] {
				t.Errorf("allowlist entry %s -> %q is stale: the file no longer imports it -- remove the entry", file, p)
			}
		}
	}
}
