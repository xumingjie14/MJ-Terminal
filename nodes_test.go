package main

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---- fixtures ----

func fxSSNode(id, server string, port int) cfgNode {
	return cfgNode{ID: id, Outbound: map[string]any{
		"type": "shadowsocks", "server": server, "server_port": port,
		"method": "aes-128-gcm", "password": "pw-" + id,
	}}
}

func fxBaseInput() cfgInput {
	return cfgInput{
		Mode: "rule", Selected: "",
		Nodes: []cfgNode{
			fxSSNode("aaaa1111aa", "203.0.113.10", 8388),
			fxSSNode("bbbb2222bb", "2001:db8::10", 8389),
		},
		Rules:   []userRule{{ID: "r1", Type: "suffix", Value: "example.org", Action: "direct", Enabled: true}},
		TestURL: "http://www.gstatic.com/generate_204",
		ClashOn: true, ClashSecret: "test-secret",
		DNSMode: "anti",
	}
}

func fxWarpOutbound() map[string]any {
	return map[string]any{
		"type": "wireguard", "address": []string{"172.16.0.2/32", "fd01::2/128"},
		"private_key": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", "mtu": 1280,
		"peers": []map[string]any{{"address": "engage.cloudflareclient.com", "port": 2408,
			"public_key":  "bmXOC+F1FxEMF9dyiK2H5/1SUtzH0JuVo51h2wPfgyo=",
			"allowed_ips": []string{"0.0.0.0/0", "::/0"}, "reserved": []int{1, 2, 3}}},
	}
}

func findOutbound(t *testing.T, cfg map[string]any, tag string) map[string]any {
	t.Helper()
	for _, o := range cfg["outbounds"].([]map[string]any) {
		if o["tag"] == tag {
			return o
		}
	}
	return nil
}

// ---- golden structure tests ----

func TestBuildCoreConfigModes(t *testing.T) {
	for _, mode := range []string{"rule", "global", "direct"} {
		in := fxBaseInput()
		in.Mode = mode
		in.RuleSets = true
		in.RuleSetDir = "/tmp/rs"
		cfg := buildCoreConfig(in)
		route := cfg["route"].(map[string]any)
		var wantFinal string
		switch mode {
		case "direct":
			wantFinal = "direct"
		default:
			wantFinal = "proxy"
		}
		if route["final"] != wantFinal {
			t.Errorf("%s: route.final = %v, want %s", mode, route["final"], wantFinal)
		}
		// Selector default follows the (empty) persisted
		// selection -> first enabled node.
		sel := findOutbound(t, cfg, "proxy")
		if sel["default"] != "node-aaaa1111aa" {
			t.Errorf("%s: selector default = %v", mode, sel["default"])
		}
		// urltest group exists, members are the real nodes.
		auto := findOutbound(t, cfg, "auto")
		if auto == nil || auto["type"] != "urltest" {
			t.Fatalf("%s: urltest group missing", mode)
		}
		members := auto["outbounds"].([]string)
		if len(members) != 2 || members[0] != "node-aaaa1111aa" {
			t.Errorf("%s: urltest members = %v", mode, members)
		}
		// Experimental Clash API present, loopback only.
		exp := cfg["experimental"].(map[string]any)["clash_api"].(map[string]any)
		if !strings.HasPrefix(exp["external_controller"].(string), "127.0.0.1:") {
			t.Errorf("%s: clash api not loopback: %v", mode, exp["external_controller"])
		}
		if exp["secret"] != "test-secret" {
			t.Errorf("%s: clash secret not carried", mode)
		}
	}

	// Rule mode: user rules first, then private, then the CN
	// sets, all before the implicit final.
	in := fxBaseInput()
	in.RuleSets = true
	in.RuleSetDir = "/tmp/rs"
	cfg := buildCoreConfig(in)
	rules := cfg["route"].(map[string]any)["rules"].([]map[string]any)
	if len(rules) != 4 {
		t.Fatalf("rule mode rules = %d, want 4", len(rules))
	}
	if rules[0]["domain_suffix"] == nil || rules[0]["outbound"] != "direct" {
		t.Errorf("user rule not first: %v", rules[0])
	}
	if rules[1]["ip_is_private"] != true {
		t.Errorf("private rule misplaced: %v", rules[1])
	}
	rs, _ := rules[2]["rule_set"].([]string)
	if len(rs) != 1 || rs[0] != "geosite-cn" || rules[2]["outbound"] != "direct" {
		t.Errorf("geosite-cn rule wrong: %v", rules[2])
	}
	rs, _ = rules[3]["rule_set"].([]string)
	if len(rs) != 1 || rs[0] != "geoip-cn" || rules[3]["outbound"] != "direct" {
		t.Errorf("geoip-cn rule wrong: %v", rules[3])
	}
	// Local rule_set definitions carry absolute paths.
	defs := cfg["route"].(map[string]any)["rule_set"].([]map[string]any)
	if len(defs) != 2 || defs[0]["path"] != "/tmp/rs/geosite-cn.srs" {
		t.Errorf("rule_set defs wrong: %v", defs)
	}
	// Global/direct modes carry no route rules and no sets.
	for _, mode := range []string{"global", "direct"} {
		in.Mode = mode
		cfg = buildCoreConfig(in)
		route := cfg["route"].(map[string]any)
		if _, ok := route["rules"]; ok {
			t.Errorf("%s: unexpected route rules", mode)
		}
		if _, ok := route["rule_set"]; ok {
			t.Errorf("%s: unexpected rule_set defs", mode)
		}
	}
}

func TestBuildCoreConfigSelectionAndDetour(t *testing.T) {
	in := fxBaseInput()
	in.Selected = "bbbb2222bb"
	cfg := buildCoreConfig(in)
	if findOutbound(t, cfg, "proxy")["default"] != "node-bbbb2222bb" {
		t.Errorf("selector did not follow persisted selection")
	}
	// auto / fallback mapping.
	in.Selected = "auto"
	if got := resolveSelectedTag(in); got != "auto" {
		t.Errorf("auto selection -> %s", got)
	}
	in.Selected = "fallback"
	in.FallbackPick = "bbbb2222bb"
	if got := resolveSelectedTag(in); got != "node-bbbb2222bb" {
		t.Errorf("fallback selection -> %s", got)
	}
	in.FallbackPick = ""
	if got := resolveSelectedTag(in); got != "node-aaaa1111aa" {
		t.Errorf("fallback without pick -> %s", got)
	}
	// Detour: node -> node and node -> warp.
	in = fxBaseInput()
	in.Nodes[1].Detour = "aaaa1111aa"
	cfg = buildCoreConfig(in)
	if got := findOutbound(t, cfg, "node-bbbb2222bb")["detour"]; got != "node-aaaa1111aa" {
		t.Errorf("node->node detour = %v", got)
	}
	in.Nodes[0].Detour = "warp"
	in.WarpOutbound = fxWarpOutbound()
	cfg = buildCoreConfig(in)
	if got := findOutbound(t, cfg, "node-aaaa1111aa")["detour"]; got != "node-warp" {
		t.Errorf("node->warp detour = %v", got)
	}
	// WARP lives in endpoints, referenced by the selector.
	eps := cfg["endpoints"].([]map[string]any)
	if len(eps) != 1 || eps[0]["tag"] != "node-warp" || eps[0]["type"] != "wireguard" {
		t.Errorf("warp endpoint wrong: %v", eps)
	}
	sel := findOutbound(t, cfg, "proxy")
	found := false
	for _, m := range sel["outbounds"].([]string) {
		if m == "node-warp" {
			found = true
		}
	}
	if !found {
		t.Errorf("selector lacks node-warp: %v", sel["outbounds"])
	}
	// Detour to a missing target is dropped, not emitted.
	in = fxBaseInput()
	in.Nodes[0].Detour = "zzzz9999zz"
	cfg = buildCoreConfig(in)
	if _, ok := findOutbound(t, cfg, "node-aaaa1111aa")["detour"]; ok {
		t.Errorf("phantom detour emitted")
	}
	// WARP excluded from the urltest members.
	in.WarpOutbound = fxWarpOutbound()
	cfg = buildCoreConfig(in)
	for _, m := range findOutbound(t, cfg, "auto")["outbounds"].([]string) {
		if m == "node-warp" {
			t.Errorf("warp leaked into urltest members")
		}
	}
}

func TestBuildCoreConfigDNS(t *testing.T) {
	in := fxBaseInput()
	in.RuleSets = true
	cfg := buildCoreConfig(in)
	dns := cfg["dns"].(map[string]any)
	servers := dns["servers"].([]map[string]any)
	if len(servers) != 2 || servers[1]["type"] != "https" || servers[1]["detour"] != "proxy" {
		t.Errorf("anti-dns servers wrong: %v", servers)
	}
	drules := dns["rules"].([]map[string]any)
	if len(drules) != 1 || drules[0]["server"] != "local-dns" {
		t.Errorf("anti-dns split rule wrong: %v", drules)
	}
	if dns["final"] != "remote-doh" {
		t.Errorf("anti-dns final = %v", dns["final"])
	}
	// Direct mode: system DNS only, no detour anywhere.
	in.Mode = "direct"
	in.DNSMode = "anti"
	cfg = buildCoreConfig(in)
	dns = cfg["dns"].(map[string]any)
	servers = dns["servers"].([]map[string]any)
	if len(servers) != 2 {
		t.Fatalf("direct-mode anti dns servers = %v", servers)
	}
	if _, ok := servers[1]["detour"]; ok {
		t.Errorf("direct mode must not detour DNS through the proxy")
	}
	in.DNSMode = "direct"
	cfg = buildCoreConfig(in)
	dns = cfg["dns"].(map[string]any)
	if len(dns["servers"].([]map[string]any)) != 1 || dns["final"] != "local-dns" {
		t.Errorf("direct dns wrong: %v", dns)
	}
	// Custom DoH.
	in.DNSMode = "custom"
	in.DNSCustom = "https://dns.google/dns-query"
	in.Mode = "global"
	cfg = buildCoreConfig(in)
	dns = cfg["dns"].(map[string]any)
	servers = dns["servers"].([]map[string]any)
	if len(servers) != 2 || servers[1]["server"] != "dns.google" || servers[1]["server_port"] != 443 {
		t.Errorf("custom dns wrong: %v", servers)
	}
	if parseDoHServer("http://dns.google/dns-query", "rule") != nil {
		t.Errorf("non-https DoH must be rejected")
	}
	if parseDoHServer("https://", "rule") != nil {
		t.Errorf("hostless DoH must be rejected")
	}
}

func TestBuildCoreConfigDeterministic(t *testing.T) {
	in := fxBaseInput()
	in.RuleSets = true
	in.RuleSetDir = "/tmp/rs"
	a, _ := json.Marshal(buildCoreConfig(in))
	b, _ := json.Marshal(buildCoreConfig(in))
	if string(a) != string(b) {
		t.Errorf("config generation is not deterministic")
	}
}

// ---- sing-box check over the reachable matrix ----

func TestSingBoxCheckMatrix(t *testing.T) {
	bin := os.Getenv("SINGBOX_BIN")
	if bin == "" {
		home, _ := os.UserHomeDir()
		bin = filepath.Join(home, "workspace", "tools", "singbox-1.14.4", "sing-box")
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("sing-box binary not available at %s", bin)
	}
	home, _ := os.UserHomeDir()
	rsSrc := filepath.Join(home, "workspace", "tools", "singbox-1.14.4", "rulesets")
	rsDir := t.TempDir()
	haveRS := true
	for _, f := range []string{"geosite-cn.srs", "geoip-cn.srs"} {
		b, err := os.ReadFile(filepath.Join(rsSrc, f))
		if err != nil {
			haveRS = false
			break
		}
		if err := os.WriteFile(filepath.Join(rsDir, f), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	type tc struct {
		name   string
		mutate func(*cfgInput)
	}
	cases := []tc{}
	for _, mode := range []string{"rule", "global", "direct"} {
		for _, dnsMode := range []string{"anti", "direct", "custom"} {
			mode, dnsMode := mode, dnsMode
			cases = append(cases, tc{mode + "/" + dnsMode, func(in *cfgInput) {
				in.Mode, in.DNSMode = mode, dnsMode
				in.DNSCustom = "https://dns.google/dns-query"
			}})
		}
	}
	cases = append(cases,
		tc{"detour-node-node", func(in *cfgInput) { in.Nodes[1].Detour = "aaaa1111aa" }},
		tc{"detour-node-warp", func(in *cfgInput) {
			in.WarpOutbound = fxWarpOutbound()
			in.Nodes[0].Detour = "warp"
		}},
		tc{"warp-as-exit", func(in *cfgInput) {
			in.WarpOutbound = fxWarpOutbound()
			in.Selected = "warp"
		}},
		tc{"warp-only-no-nodes", func(in *cfgInput) {
			in.WarpOutbound = fxWarpOutbound()
			in.Nodes = nil
			in.Selected = "warp"
		}},
		tc{"selected-auto", func(in *cfgInput) { in.Selected = "auto" }},
		tc{"selected-fallback", func(in *cfgInput) {
			in.Selected = "fallback"
			in.FallbackPick = "bbbb2222bb"
		}},
		tc{"selected-ipv6-node", func(in *cfgInput) { in.Selected = "bbbb2222bb" }},
		tc{"clash-off", func(in *cfgInput) { in.ClashOn = false }},
		tc{"block-rule", func(in *cfgInput) {
			in.Rules = append(in.Rules, userRule{ID: "r2", Type: "domain", Value: "ads.example.com", Action: "block", Enabled: true})
		}},
		tc{"ipcidr-rule", func(in *cfgInput) {
			in.Rules = append(in.Rules, userRule{ID: "r3", Type: "ipcidr", Value: "198.51.100.0/24", Action: "proxy", Enabled: true})
		}},
		tc{"no-rulesets", func(in *cfgInput) { in.RuleSets = false }},
	)
	for _, c := range cases {
		in := fxBaseInput()
		in.RuleSetDir = rsDir
		in.RuleSets = haveRS
		c.mutate(&in)
		cfg := buildCoreConfig(in)
		b, err := json.Marshal(cfg)
		if err != nil {
			t.Fatalf("%s: marshal: %v", c.name, err)
		}
		p := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command(bin, "check", "-c", p).CombinedOutput()
		if err != nil {
			t.Errorf("%s: sing-box check failed: %v\n%s\nconfig:\n%s", c.name, err, out, b)
		}
	}
	if !haveRS {
		t.Log("rule set fixtures missing; rule-mode cases ran without the CN sets")
	}
}

// ---- parsers ----

func TestParseSubUserInfo(t *testing.T) {
	ui := parseSubUserInfo("upload=1024; download=2048; total=10737418240; expire=1893456000")
	if ui == nil || !ui.Has || ui.Upload != 1024 || ui.Download != 2048 || ui.Total != 10737418240 || ui.Expire != 1893456000 {
		t.Fatalf("userinfo parsed wrong: %+v", ui)
	}
	if parseSubUserInfo("") != nil {
		t.Errorf("empty header must yield nil")
	}
	if parseSubUserInfo("garbage") != nil {
		t.Errorf("garbage header must yield nil")
	}
	ui = parseSubUserInfo("total=100")
	if ui == nil || ui.Total != 100 {
		t.Errorf("partial header should still parse: %+v", ui)
	}
}

const fxSSLink = "ss://YWVzLTEyOC1nY206cHctYQ==@203.0.113.10:8388#Alpha"
const fxTrojanLink = "trojan://secretpw@203.0.113.20:443?sni=example.com#Beta"

func TestParseSubContentLinks(t *testing.T) {
	body := fxSSLink + "\n" + fxTrojanLink + "\nnot-a-link\n"
	ents, skipped, format, err := parseSubContent([]byte(body))
	if err != nil || len(ents) != 2 || skipped != 0 {
		t.Fatalf("links parse: ents=%d skipped=%d format=%s err=%v", len(ents), skipped, format, err)
	}
	if ents[0].Name != "Alpha" || ents[0].Type != "shadowsocks" {
		t.Errorf("first entry wrong: %+v", ents[0])
	}
	// base64-wrapped body decodes to the same set.
	enc := base64.StdEncoding.EncodeToString([]byte(body))
	ents2, _, _, err := parseSubContent([]byte(enc))
	if err != nil || len(ents2) != 2 {
		t.Fatalf("base64 parse: ents=%d err=%v", len(ents2), err)
	}
	if ents2[0].ID != ents[0].ID {
		t.Errorf("same endpoint must keep identity across encodings: %s vs %s", ents2[0].ID, ents[0].ID)
	}
	// Empty content is a named error.
	if _, _, _, err := parseSubContent([]byte("  ")); err == nil {
		t.Errorf("empty body must error")
	}
}

func TestParseClashYAMLSubscription(t *testing.T) {
	doc := `
proxies:
  - name: "YamlSS"
    type: ss
    server: 203.0.113.30
    port: 8388
    cipher: aes-128-gcm
    password: pw-yaml
  - name: "YamlVless"
    type: vless
    server: 203.0.113.31
    port: 443
    uuid: 11111111-2222-3333-4444-555555555555
    tls: true
    servername: cdn.example.com
    network: ws
    ws-opts:
      path: /ws
      headers:
        Host: cdn.example.com
  - name: "YamlSnell"
    type: snell
    server: 203.0.113.32
    port: 6160
    psk: abc
`
	ents, skipped, format, err := parseSubContent([]byte(doc))
	if err != nil {
		t.Fatalf("yaml parse: %v", err)
	}
	if format != "Clash YAML" || len(ents) != 2 || skipped != 1 {
		t.Fatalf("yaml parse: ents=%d skipped=%d format=%s", len(ents), skipped, format)
	}
	if ents[0].Type != "shadowsocks" || ents[0].Outbound["method"] != "aes-128-gcm" {
		t.Errorf("yaml ss entry wrong: %+v", ents[0])
	}
	tr, _ := ents[1].Outbound["transport"].(map[string]any)
	if tr == nil || tr["type"] != "ws" || tr["path"] != "/ws" {
		t.Errorf("yaml vless transport wrong: %v", ents[1].Outbound)
	}
	tls, _ := ents[1].Outbound["tls"].(map[string]any)
	if tls == nil || tls["server_name"] != "cdn.example.com" {
		t.Errorf("yaml vless tls wrong: %v", ents[1].Outbound)
	}
}

func TestBuildCoreConfigDuplicateEndpoints(t *testing.T) {
	// The same endpoint arriving from two sources must not
	// produce duplicate outbound tags (sing-box refuses the
	// whole config when it does -- found in the v0.49 smoke).
	in := fxBaseInput()
	in.Nodes = append(in.Nodes, in.Nodes[0])
	in.Nodes = append(in.Nodes, cfgNode{ID: "", Outbound: map[string]any{"type": "direct"}})
	cfg := buildCoreConfig(in)
	seen := map[string]bool{}
	for _, o := range cfg["outbounds"].([]map[string]any) {
		tag := o["tag"].(string)
		if seen[tag] {
			t.Errorf("duplicate outbound tag %q", tag)
		}
		seen[tag] = true
		if tag == "node-" {
			t.Errorf("empty-id node leaked into the config")
		}
	}
	// A subscription refresh re-parsing the same link keeps
	// the node's identity (selection preservation depends on
	// it).
	ents, _, _, err := parseSubContent([]byte(fxSSLink))
	if err != nil || len(ents) != 1 || ents[0].ID == "" {
		t.Fatalf("link entry must carry an ID: %+v err=%v", ents, err)
	}
}

func TestNodeIDStableAcrossNameAndDetour(t *testing.T) {
	a := map[string]any{"type": "shadowsocks", "server": "203.0.113.10", "server_port": 8388, "method": "aes-128-gcm", "password": "pw", "tag": "node-x"}
	b := map[string]any{"type": "shadowsocks", "server": "203.0.113.10", "server_port": 8388, "method": "aes-128-gcm", "password": "pw", "tag": "node-y", "detour": "node-z"}
	if nodeIDFor(a) != nodeIDFor(b) {
		t.Errorf("identity must ignore tag/detour")
	}
	c := map[string]any{"type": "shadowsocks", "server": "203.0.113.11", "server_port": 8388, "method": "aes-128-gcm", "password": "pw"}
	if nodeIDFor(a) == nodeIDFor(c) {
		t.Errorf("different endpoints must not share identity")
	}
}

func TestIPv6Decisions(t *testing.T) {
	if !isIPv6Literal("2001:db8::10") || !isIPv6Literal("[2001:db8::10]") {
		t.Errorf("ipv6 literals not detected")
	}
	if isIPv6Literal("203.0.113.10") || isIPv6Literal("example.com") || isIPv6Literal("") {
		t.Errorf("non-ipv6 misdetected")
	}
	dialOK := func(network, addr string, timeout time.Duration) error { return nil }
	dialFail := func(network, addr string, timeout time.Duration) error {
		return &net.DNSError{IsTimeout: true}
	}
	if !ipv6EgressProbe([]string{"[2001:db8::1]:443"}, dialOK) {
		t.Errorf("probe should report egress when a dial succeeds")
	}
	if ipv6EgressProbe([]string{"[2001:db8::1]:443"}, dialFail) {
		t.Errorf("probe must not report egress when all dials fail")
	}
}

func TestValidateUserRule(t *testing.T) {
	ok := []userRule{
		{Type: "domain", Value: "example.com", Action: "proxy"},
		{Type: "suffix", Value: "corp", Action: "direct"},
		{Type: "ipcidr", Value: "203.0.113.0/24", Action: "block"},
		{Type: "ipcidr", Value: "203.0.113.9", Action: "proxy"},
	}
	for _, r := range ok {
		if err := validateUserRule(r); err != nil {
			t.Errorf("rule %+v should pass: %v", r, err)
		}
	}
	bad := []userRule{
		{Type: "domain", Value: "", Action: "proxy"},
		{Type: "domain", Value: "nodot", Action: "proxy"},
		{Type: "domain", Value: "exa mple.com", Action: "proxy"},
		{Type: "ipcidr", Value: "999.1.1.0/24", Action: "proxy"},
		{Type: "ipcidr", Value: "example.com", Action: "proxy"},
		{Type: "weird", Value: "x.com", Action: "proxy"},
		{Type: "domain", Value: "x.com", Action: "teleport"},
	}
	for _, r := range bad {
		if err := validateUserRule(r); err == nil {
			t.Errorf("rule %+v should fail validation", r)
		}
	}
	if got := normalizeRuleValue(userRule{Type: "ipcidr", Value: "203.0.113.9"}); got != "203.0.113.9/32" {
		t.Errorf("normalize ipcidr = %s", got)
	}
	if got := normalizeRuleValue(userRule{Type: "domain", Value: "EXAMPLE.com"}); got != "example.com" {
		t.Errorf("normalize domain = %s", got)
	}
}
