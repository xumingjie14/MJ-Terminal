package main

// parsers_fuzz_test.go -- v0.61 architecture block 5: fuzz
// targets for the untrusted-input parsers that face the
// network: share links and subscription bodies (whatever a
// subscription server or a pasted link throws at the daemon),
// the base64 helper underneath them, and the update manifest
// parse + signature-verify entry.
//
// Under a plain `go test` the seed corpora run once as unit
// tests; the CI verify job additionally runs each target for a
// bounded -fuzztime smoke. A crash here is a daemon crash on
// hostile input, so any failing input a fuzz run finds gets
// fixed and pinned as a regression test.

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func FuzzParseShareLink(f *testing.F) {
	f.Add(fxSSLink)
	f.Add(fxTrojanLink)
	f.Add("vless://11111111-2222-3333-4444-555555555555@203.0.113.40:443?security=reality&pbk=abc&sid=01&fp=chrome&type=ws&path=%2Fws#V")
	f.Add("hysteria2://pw@203.0.113.50:443?sni=a.b&insecure=1#H")
	f.Add("hy2://pw@203.0.113.50:443#H2")
	f.Add("vmess://" + base64.StdEncoding.EncodeToString([]byte(`{"v":"2","ps":"M","add":"203.0.113.60","port":"443","id":"11111111-2222-3333-4444-555555555555","aid":"0","net":"ws","host":"cdn.example.com","path":"/p","tls":"tls"}`)))
	// The whole-body ss form: base64 of method:password@host:port.
	f.Add("ss://" + base64.StdEncoding.EncodeToString([]byte("aes-128-gcm:pw-a@203.0.113.10:8388")))
	// Regression seed for the parseSS slice panic (fixed in
	// v0.61): base64 body whose decoded userinfo has no ':'.
	f.Add("ss://" + base64.StdEncoding.EncodeToString([]byte("methodonly@203.0.113.10:8388")))
	f.Add("ss://")
	f.Add("ss://@")
	f.Add("ss://#")
	f.Add("vless://")
	f.Add("vmess://!!!")
	f.Add("trojan://@:")
	f.Fuzz(func(t *testing.T, link string) {
		// Any result is acceptable (entry or error); a panic is not.
		_, _ = parseShareLink(link)
	})
}

func FuzzParseSubContent(f *testing.F) {
	body := fxSSLink + "\n" + fxTrojanLink + "\nnot-a-link\n"
	f.Add([]byte(body))
	f.Add([]byte(base64.StdEncoding.EncodeToString([]byte(body))))
	f.Add([]byte("\nproxies:\n  - name: \"YamlSS\"\n    type: ss\n    server: 203.0.113.30\n    port: 8388\n    cipher: aes-128-gcm\n    password: pw\n"))
	f.Add([]byte("proxies: []"))
	f.Add([]byte("proxies:\n  - type: vless\n    server: x\n    port: notaport\n"))
	f.Add([]byte(""))
	f.Add([]byte("://"))
	f.Add([]byte{0x00, 0xff, 0xfe, 0x0a, 0x3a, 0x2f, 0x2f})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _, _ = parseSubContent(data)
	})
}

func FuzzB64Decode(f *testing.F) {
	f.Add("YWVzLTEyOC1nY206cHctYQ==")
	f.Add("YWVzLTEyOC1nY206cHctYQ")
	f.Add("")
	f.Add("====")
	f.Add("!!!")
	f.Add("a")
	f.Add("ab")
	f.Add("abc")
	f.Add("abcd====")
	f.Add("YW\nVz\r\n")
	f.Fuzz(func(t *testing.T, s string) {
		_, _ = b64decode(s)
	})
}

// FuzzUpdateManifestParse hammers the update check's trust
// boundary: the signature verify (over a 64-byte prefix carved
// off the input) and the manifest JSON parse. Garbage must
// produce false/errors, never a panic.
func FuzzUpdateManifestParse(f *testing.F) {
	valid := []byte(`{"version":"0.61","file":"MJ-System-64bit.zip","size":62346142,"sha256":"d15c211356aa9423d99b95519b9493abf5f61c6120ad0cd84a566e862b2dd378"}`)
	seed := append(make([]byte, 64), valid...)
	f.Add(seed)
	f.Add(valid)
	f.Add([]byte{})
	f.Add([]byte("{"))
	f.Add([]byte(`{"version":123}`))
	f.Add([]byte(`{"version":"0.61","size":"huge"}`))
	f.Add([]byte{0xef, 0xbb, 0xbf, '{', '}'})
	f.Fuzz(func(t *testing.T, data []byte) {
		var sig, mb []byte
		if len(data) >= 64 {
			sig, mb = data[:64], data[64:]
		} else {
			sig, mb = data, nil
		}
		_ = verifyManifestSignature(mb, sig)
		var man updateManifest
		_ = json.Unmarshal(mb, &man)
	})
}

// TestParseSSWholeBodyNoColon is the pinned regression for the
// crash FuzzParseShareLink's seed corpus represents: in the
// whole-body base64 ss form, a decoded userinfo without a ':'
// drove a slice expression with index -1 (daemon panic on a
// hostile subscription line).
func TestParseSSWholeBodyNoColon(t *testing.T) {
	link := "ss://" + base64.StdEncoding.EncodeToString([]byte("methodonly@203.0.113.10:8388"))
	if _, err := parseShareLink(link); err == nil {
		t.Fatal("ss body without ':' in userinfo must be a parse error, not a node (and never a panic)")
	}
}
