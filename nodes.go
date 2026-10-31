package main

// ---------- node professionalization (v0.49) ----------
//
// The Clash-Verge-grade node subsystem. Before v0.49 the node
// page was a link box: every node sat in one flat selector that
// always egressed through the FIRST node, there were no modes,
// no latency, no live connections, and no way to switch. This
// file is the whole replacement:
//
//   * Modes: rule (default, 绕过大陆 via GeoSite/GeoIP CN rule
//     sets) / global / direct, persisted in Data/nodes.json,
//     applied by regenerating the core config and restarting
//     the core.
//   * Real switching: the selection (a node, the urltest group
//     "自动选择", the daemon-driven "故障转移", or the WARP
//     bridge) persists and is applied HOT through the sing-box
//     Clash API when it is available (selector PUT, verified),
//     falling back to regenerate + restart.
//   * sing-box has NO fallback outbound type, so 故障转移 is a
//     daemon watchdog: it health-checks members in order and
//     keeps the selector pointed at the first live one. That is
//     stated in the UI copy and the release notes, not hidden.
//   * Subscriptions: many, individually named/enabled, with
//     subscription-userinfo (usage + expiry), per-sub or global
//     auto-update, share-link / base64 / Clash YAML parsing.
//   * Live plane: the generated config carries the Clash API
//     (127.0.0.1 only, random local secret, never logged) for
//     the connections table, the traffic chart and delay tests.
//   * Chaining: every outbound can take a detour (another node
//     or the built-in WARP WireGuard bridge, registered locally
//     via Cloudflare's public WARP API). IPv6-literal nodes on
//     a machine without IPv6 egress get a 不可直达 badge and a
//     one-click detour fix.
//   * DNS schemes （防污染 / 直连 / 自定义 DoH) and user rules
//     (domain / suffix / CIDR -> proxy / direct / reject)
//     compile into the same generated config.
//
// State lives in Data/nodes.json (therefore inside the vault's
// scope like everything else in Data). links.txt keeps being
// written as a legacy mirror of the share links so a downgrade
// still finds its nodes. Subscription URLs and the WARP private
// key live only in nodes.json: they are never logged and never
// served by /api/node (the remote channel's scrubbed list).

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/curve25519"
	"gopkg.in/yaml.v3"
)

// ---------- persisted state ----------

type subUserInfo struct {
	Has      bool  `json:"has"`
	Upload   int64 `json:"upload"`
	Download int64 `json:"download"`
	Total    int64 `json:"total"`
	Expire   int64 `json:"expire"` // unix seconds; 0 = provider gave none
}

type subEntry struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	URL       string      `json:"url"` // secret: Data only, never logged/served raw
	Enabled   bool        `json:"enabled"`
	UpdatedAt int64       `json:"updatedAt"`
	AutoMins  int         `json:"autoMins"` // 0 = global default from Settings
	UserInfo  subUserInfo `json:"userInfo"`
}

type userRule struct {
	ID      string `json:"id"`
	Type    string `json:"type"`   // domain | suffix | ipcidr
	Value   string `json:"value"`  // hostname / suffix / CIDR
	Action  string `json:"action"` // proxy | direct | block
	Enabled bool   `json:"enabled"`
}

type warpState struct {
	PrivateKey    string `json:"privateKey"` // base64; Data only, never logged
	PeerPublicKey string `json:"peerPublicKey"`
	Endpoint      string `json:"endpoint"` // host:port as returned by the WARP API
	AddressV4     string `json:"addressV4"`
	AddressV6     string `json:"addressV6"`
	Reserved      []int  `json:"reserved"`
	RegisteredAt  int64  `json:"registeredAt"`
}

type persistNode struct {
	ID          string         `json:"id"`
	Group       string         `json:"group"` // "manual" or a subscription ID
	Name        string         `json:"name,omitempty"`
	Raw         string         `json:"raw,omitempty"`
	Outbound    map[string]any `json:"outbound,omitempty"` // nodes without a share link (Clash YAML)
	Detour      string         `json:"detour,omitempty"`   // "" | "warp" | node ID
	LatencyMs   int            `json:"latencyMs"`
	LatencyKind string         `json:"latencyKind,omitempty"` // http | tcp
	LatencyErr  string         `json:"latencyErr,omitempty"`
	LatencyAt   int64          `json:"latencyAt,omitempty"`
}

type nodeStore struct {
	Mode        string        `json:"mode"` // rule | global | direct
	Selected    string        `json:"selected"`
	DNSMode     string        `json:"dnsMode"` // anti | direct | custom
	DNSCustom   string        `json:"dnsCustom,omitempty"`
	ClashSecret string        `json:"clashSecret"`
	Subs        []subEntry    `json:"subs"`
	Nodes       []persistNode `json:"nodes"`
	Rules       []userRule    `json:"rules"`
	Warp        *warpState    `json:"warp,omitempty"`
}

var (
	npMu     sync.Mutex // guards npStore and every mutation of the runtime nodes slice
	npStore  nodeStore
	npLoaded bool
)

func nodeStorePath() string { return filepath.Join(dataDir(), "nodes.json") }

func nodeModes() map[string]bool {
	return map[string]bool{"rule": true, "global": true, "direct": true}
}

// nodeIDFor derives a node's stable identity from its outbound
// content: identical endpoints (same server, credentials, transport)
// keep the same ID across restarts and subscription refreshes, which
// is what lets a selection and a detour survive an update.
func nodeIDFor(outbound map[string]any) string {
	clean := map[string]any{}
	for k, v := range outbound {
		if k == "tag" || k == "detour" {
			continue
		}
		clean[k] = v
	}
	b, _ := json.Marshal(clean)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:10]
}

func nodeTag(id string) string { return "node-" + id }

func newRandID(prefix string) string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// npLoadStoreLocked loads Data/nodes.json (or imports the legacy
// links.txt as the manual group on first run) and rebuilds the
// runtime nodes slice. Caller holds npMu.
func npLoadStoreLocked() {
	if npLoaded {
		return
	}
	npLoaded = true
	npStore = nodeStore{Mode: "rule", DNSMode: "anti"}
	// v0.57: through the data layer -- a damaged nodes.json is
	// restored from its keep-last-good backup there ([data] logs
	// it); only a genuinely absent file takes the legacy import.
	b, bstatus, berr := dsLoad("nodes")
	if berr == nil && bstatus != dsMissing {
		var s nodeStore
		if jerr := json.Unmarshal(b, &s); jerr == nil {
			npStore = s
		} else {
			logf("node", "warn", "nodes.json 解析失败，已按空库启动：%v", jerr)
		}
	} else if bstatus == dsMissing {
		// Legacy import: everything in links.txt becomes the
		// manual group, exactly the nodes the old versions saved.
		lb, lstatus, lerr := dsLoad("links")
		if lerr == nil && lstatus != dsMissing {
			for _, line := range strings.Split(string(lb), "\n") {
				if n, perr := parseShareLink(line); perr == nil {
					n.Group = "manual"
					nodes = append(nodes, *n)
				}
			}
			if len(nodes) > 0 {
				logf("node", "info", "已把旧版 links.txt 的 %d 个节点导入节点库（手动组）", len(nodes))
			}
		}
	}
	if !nodeModes()[npStore.Mode] {
		npStore.Mode = "rule"
	}
	if npStore.DNSMode != "anti" && npStore.DNSMode != "direct" && npStore.DNSMode != "custom" {
		npStore.DNSMode = "anti"
	}
	if npStore.ClashSecret == "" {
		sb := make([]byte, 16)
		_, _ = rand.Read(sb)
		npStore.ClashSecret = hex.EncodeToString(sb)
	}
	npSecretCache.Store(npStore.ClashSecret)
	// Rebuild the runtime slice from the persisted nodes (the
	// store file is authoritative once it exists).
	if len(npStore.Nodes) > 0 || len(npStore.Subs) > 0 {
		nodes = nil
		for _, pn := range npStore.Nodes {
			if e := pn.toEntry(); e != nil {
				nodes = append(nodes, *e)
			}
		}
	}
	// Normalize every runtime node: stable ID, group, latency.
	for i := range nodes {
		if nodes[i].Outbound != nil {
			nodes[i].ID = nodeIDFor(nodes[i].Outbound)
		}
		if nodes[i].Group == "" {
			nodes[i].Group = "manual"
		}
	}
	// Identity is the endpoint: two entries with the same ID
	// would emit duplicate outbound tags and sing-box would
	// refuse the whole config. First copy wins.
	{
		seen := map[string]bool{}
		uniq := nodes[:0]
		for _, n := range nodes {
			if n.ID == "" || seen[n.ID] {
				continue
			}
			seen[n.ID] = true
			uniq = append(uniq, n)
		}
		if len(uniq) != len(nodes) {
			logf("node", "warn", "节点库里有 %d 个端点完全相同的重复节点，已只保留先出现的一份", len(nodes)-len(uniq))
		}
		nodes = uniq
	}
	// Settings can pin a start mode (默认启动模式）; "last" keeps
	// the persisted one.
	if sm := settingNodeStartMode(); sm != "last" && nodeModes()[sm] && sm != npStore.Mode {
		logf("node", "info", "按设置应用启动模式：%s（上次保存的是 %s）", npModeName(sm), npModeName(npStore.Mode))
		npStore.Mode = sm
	}
	npSaveStoreLocked()
}

func (pn persistNode) toEntry() *nodeEntry {
	var e *nodeEntry
	if pn.Raw != "" {
		if n, err := parseShareLink(pn.Raw); err == nil {
			e = n
		}
	}
	if e == nil && pn.Outbound != nil {
		// Outbound-only node (Clash YAML import): derive the
		// display fields from the outbound itself.
		e = &nodeEntry{Outbound: pn.Outbound, Name: pn.Name}
		e.Type, _ = pn.Outbound["type"].(string)
		e.Server, _ = pn.Outbound["server"].(string)
		if p, ok := pn.Outbound["server_port"].(float64); ok {
			e.Port = int(p)
		}
		if e.Name == "" {
			e.Name = e.Server
		}
	}
	if e == nil {
		return nil
	}
	e.Group = pn.Group
	e.Detour = pn.Detour
	e.LatencyMs = pn.LatencyMs
	if pn.LatencyAt == 0 && pn.LatencyErr == "" {
		e.LatencyMs = -1 // never tested
	}
	e.LatencyKind = pn.LatencyKind
	e.LatencyErr = pn.LatencyErr
	e.LatencyAt = pn.LatencyAt
	if e.Outbound != nil {
		e.ID = nodeIDFor(e.Outbound)
	}
	return e
}

// npSaveStoreLocked mirrors the runtime state into Data/nodes.json
// and refreshes the legacy links.txt. Caller holds npMu.
func npSaveStoreLocked() {
	npStore.Nodes = npStore.Nodes[:0]
	for _, n := range nodes {
		pn := persistNode{
			ID: n.ID, Group: n.Group, Raw: n.Raw, Detour: n.Detour,
			LatencyMs: n.LatencyMs, LatencyKind: n.LatencyKind,
			LatencyErr: n.LatencyErr, LatencyAt: n.LatencyAt,
		}
		if n.Raw == "" && n.Outbound != nil {
			pn.Outbound = n.Outbound
			pn.Name = n.Name
		}
		npStore.Nodes = append(npStore.Nodes, pn)
	}
	b, err := json.MarshalIndent(npStore, "", "  ")
	if err != nil {
		return
	}
	_ = dsSave("nodes", b)
	persistLinks()
}

func npModeName(m string) string {
	switch m {
	case "global":
		return "全局"
	case "direct":
		return "直连"
	}
	return "规则"
}

func npFindNode(id string) *nodeEntry {
	for i := range nodes {
		if nodes[i].ID == id {
			return &nodes[i]
		}
	}
	return nil
}

func npFindSub(id string) *subEntry {
	for i := range npStore.Subs {
		if npStore.Subs[i].ID == id {
			return &npStore.Subs[i]
		}
	}
	return nil
}

// npEnabledNodes returns the nodes that take part in routing:
// every manual node plus the nodes of enabled subscriptions.
func npEnabledNodes() []nodeEntry {
	var out []nodeEntry
	for _, n := range nodes {
		if n.Group == "manual" {
			out = append(out, n)
			continue
		}
		if s := npFindSub(n.Group); s != nil && s.Enabled {
			out = append(out, n)
		}
	}
	return out
}

// ---------- config generation (pure builder) ----------

type cfgNode struct {
	ID       string
	Outbound map[string]any
	Detour   string // "" | "warp" | node ID
}

type cfgInput struct {
	Mode         string
	Selected     string // node ID | "auto" | "fallback" | "warp" | ""
	FallbackPick string // node ID the fallback watchdog currently points at
	Nodes        []cfgNode
	WarpOutbound map[string]any // nil when no WARP bridge is registered
	Rules        []userRule
	RuleSets     bool   // geosite-cn / geoip-cn .srs present locally
	RuleSetDir   string // absolute dir holding the .srs files
	DNSMode      string
	DNSCustom    string
	TestURL      string
	ClashOn      bool
	ClashSecret  string
}

const clashAPIPort = 19090

// resolveSelectedTag decides which outbound tag the selector
// starts on. Pure; the golden tests pin its behaviour.
func resolveSelectedTag(in cfgInput) string {
	has := map[string]bool{}
	for _, n := range in.Nodes {
		has[n.ID] = true
	}
	switch {
	case in.Selected == "auto" && len(in.Nodes) > 0:
		return "auto"
	case in.Selected == "fallback" && len(in.Nodes) > 0:
		if in.FallbackPick != "" && has[in.FallbackPick] {
			return nodeTag(in.FallbackPick)
		}
		return nodeTag(in.Nodes[0].ID)
	case in.Selected == "warp" && in.WarpOutbound != nil:
		return "node-warp"
	case in.Selected != "" && has[in.Selected]:
		return nodeTag(in.Selected)
	case len(in.Nodes) > 0:
		return nodeTag(in.Nodes[0].ID)
	case in.WarpOutbound != nil:
		return "node-warp"
	}
	return "direct"
}

func compileUserRule(r userRule) map[string]any {
	rule := map[string]any{}
	switch r.Type {
	case "domain":
		rule["domain"] = []string{r.Value}
	case "suffix":
		rule["domain_suffix"] = []string{r.Value}
	case "ipcidr":
		rule["ip_cidr"] = []string{r.Value}
	}
	if r.Action == "block" {
		rule["action"] = "reject"
	} else {
		rule["outbound"] = r.Action // proxy | direct (validated upstream)
	}
	return rule
}

func buildCoreConfig(in cfgInput) map[string]any {
	// Defensive: a node without an ID, or two nodes sharing
	// one, would produce an empty or duplicate outbound tag
	// and sing-box would refuse the whole config. First wins.
	ids := map[string]bool{}
	var nodeTags []string
	uniqNodes := in.Nodes[:0]
	for _, n := range in.Nodes {
		if n.ID == "" || ids[n.ID] {
			continue
		}
		ids[n.ID] = true
		nodeTags = append(nodeTags, nodeTag(n.ID))
		uniqNodes = append(uniqNodes, n)
	}
	in.Nodes = uniqNodes
	// Selector members: the urltest group, every node, the WARP
	// bridge. ("fallback" is not a sing-box outbound; the daemon
	// watchdog points the selector at its current pick instead.)
	members := []string{}
	if len(nodeTags) > 0 {
		members = append(members, "auto")
	}
	members = append(members, nodeTags...)
	if in.WarpOutbound != nil {
		members = append(members, "node-warp")
	}
	if len(members) == 0 {
		members = []string{"direct"}
	}
	selector := map[string]any{
		"type": "selector", "tag": "proxy",
		"outbounds": members, "default": resolveSelectedTag(in),
	}
	outs := []map[string]any{selector}
	if len(nodeTags) > 0 {
		outs = append(outs, map[string]any{
			"type": "urltest", "tag": "auto", "outbounds": nodeTags,
			"url": in.TestURL, "interval": "10m", "tolerance": 50,
		})
	}
	for _, n := range in.Nodes {
		o := map[string]any{}
		for k, v := range n.Outbound {
			o[k] = v
		}
		o["tag"] = nodeTag(n.ID)
		// Detour: only when the target exists and is not the
		// node itself (a self-chain can never dial).
		switch {
		case n.Detour == "warp" && in.WarpOutbound != nil:
			o["detour"] = "node-warp"
		case n.Detour != "" && n.Detour != n.ID && ids[n.Detour]:
			o["detour"] = nodeTag(n.Detour)
		}
		outs = append(outs, o)
	}
	outs = append(outs, map[string]any{"type": "direct", "tag": "direct"})

	cfg := map[string]any{
		"log": map[string]any{"level": "info", "timestamp": true},
		"inbounds": []map[string]any{
			{"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": proxyPort},
		},
		"outbounds": outs,
		"dns":       buildDNS(in),
	}
	if in.WarpOutbound != nil {
		wg := map[string]any{}
		for k, v := range in.WarpOutbound {
			wg[k] = v
		}
		wg["tag"] = "node-warp"
		cfg["endpoints"] = []map[string]any{wg}
	}
	route := map[string]any{"default_domain_resolver": map[string]any{"server": "local-dns"}}
	switch in.Mode {
	case "global":
		route["final"] = "proxy"
	case "direct":
		route["final"] = "direct"
	default: // rule
		var rules []map[string]any
		for _, r := range in.Rules {
			if r.Enabled {
				rules = append(rules, compileUserRule(r))
			}
		}
		rules = append(rules, map[string]any{"ip_is_private": true, "outbound": "direct"})
		if in.RuleSets {
			route["rule_set"] = []map[string]any{
				{"type": "local", "tag": "geosite-cn", "format": "binary", "path": filepath.Join(in.RuleSetDir, "geosite-cn.srs")},
				{"type": "local", "tag": "geoip-cn", "format": "binary", "path": filepath.Join(in.RuleSetDir, "geoip-cn.srs")},
			}
			rules = append(rules,
				map[string]any{"rule_set": []string{"geosite-cn"}, "outbound": "direct"},
				map[string]any{"rule_set": []string{"geoip-cn"}, "outbound": "direct"})
		}
		route["rules"] = rules
		route["final"] = "proxy"
	}
	cfg["route"] = route
	if in.ClashOn && in.ClashSecret != "" {
		cfg["experimental"] = map[string]any{
			"clash_api": map[string]any{
				"external_controller": fmt.Sprintf("127.0.0.1:%d", clashAPIPort),
				"secret":              in.ClashSecret,
				"default_mode":        in.Mode,
			},
		}
	}
	return cfg
}

func buildDNS(in cfgInput) map[string]any {
	local := map[string]any{"type": "local", "tag": "local-dns"}
	servers := []map[string]any{local}
	final := "local-dns"
	switch in.DNSMode {
	case "custom":
		if srv := parseDoHServer(in.DNSCustom, in.Mode); srv != nil {
			servers = append(servers, srv)
			final = "custom-doh"
		}
	case "direct":
		// local only
	default: // anti （防污染）: public DoH, carried by the proxy
		remote := map[string]any{
			"type": "https", "tag": "remote-doh", "server": "1.1.1.1",
			"server_port": 443, "path": "/dns-query", "domain_resolver": "local-dns",
		}
		if in.Mode != "direct" {
			remote["detour"] = "proxy"
		}
		servers = append(servers, remote)
		final = "remote-doh"
	}
	dns := map[string]any{"servers": servers, "final": final}
	if in.DNSMode == "anti" && in.Mode == "rule" && in.RuleSets {
		dns["rules"] = []map[string]any{
			{"rule_set": "geosite-cn", "server": "local-dns"},
		}
	}
	return dns
}

// parseDoHServer turns a user DoH URL into a sing-box DNS server
// entry; nil when the URL is not a usable https DoH endpoint.
func parseDoHServer(raw, mode string) map[string]any {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		return nil
	}
	port := 443
	if p, err := strconv.Atoi(u.Port()); err == nil && p > 0 {
		port = p
	}
	path := u.Path
	if path == "" {
		path = "/dns-query"
	}
	srv := map[string]any{
		"type": "https", "tag": "custom-doh", "server": u.Hostname(),
		"server_port": port, "path": path, "domain_resolver": "local-dns",
	}
	if mode != "direct" {
		srv["detour"] = "proxy"
	}
	return srv
}

// ---------- rule sets （绕过大陆的数据文件） ----------

var ruleSetFiles = []struct{ name, url string }{
	{"geosite-cn.srs", "https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-cn.srs"},
	{"geoip-cn.srs", "https://raw.githubusercontent.com/SagerNet/sing-geoip/rule-set/geoip-cn.srs"},
}

func ruleSetsPresent() bool {
	for _, f := range ruleSetFiles {
		st, err := os.Stat(filepath.Join(coreDir(), f.name))
		if err != nil || st.Size() < 1024 {
			return false
		}
	}
	return true
}

// ensureRuleSets makes a best effort to have fresh rule sets in
// coreDir; it reports whether they are usable afterwards. A
// failed download degrades rule mode honestly (the caller logs
// it and the config ships without the CN sets) -- it must never
// stop the core from starting.
func ensureRuleSets() bool {
	if ruleSetsPresent() {
		fresh := true
		for _, f := range ruleSetFiles {
			if st, err := os.Stat(filepath.Join(coreDir(), f.name)); err != nil || time.Since(st.ModTime()) > 7*24*time.Hour {
				fresh = false
			}
		}
		if fresh {
			return true
		}
	}
	ok := true
	for _, f := range ruleSetFiles {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		resp, err := dlGet(ctx, f.url, "MJTerminal/"+AppVersion)
		if err != nil {
			cancel()
			ok = false
			continue
		}
		b, rerr := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
		_ = resp.Body.Close()
		cancel()
		if rerr != nil || len(b) < 1024 {
			ok = false
			continue
		}
		_ = os.WriteFile(filepath.Join(coreDir(), f.name), b, 0o644)
	}
	if ok {
		logf("node", "info", "规则集已更新（GeoSite/GeoIP CN，绕过大陆用）")
	}
	return ruleSetsPresent()
}

// npSnapshotCfgLocked snapshots the runtime state for the pure
// builder. Caller holds npMu.
func npSnapshotCfgLocked(ruleSets bool) cfgInput {
	en := npEnabledNodes()
	in := cfgInput{
		Mode: npStore.Mode, Selected: npStore.Selected,
		FallbackPick: fallbackPick(),
		RuleSets:     ruleSets, RuleSetDir: coreDir(),
		DNSMode: npStore.DNSMode, DNSCustom: npStore.DNSCustom,
		TestURL: settingNodeTestURL(), ClashOn: settingNodeClashAPI(),
		ClashSecret: npStore.ClashSecret, Rules: npStore.Rules,
	}
	for _, n := range en {
		in.Nodes = append(in.Nodes, cfgNode{ID: n.ID, Outbound: n.Outbound, Detour: n.Detour})
	}
	if npStore.Warp != nil {
		in.WarpOutbound = warpOutboundMap(npStore.Warp)
	}
	return in
}

// npGenConfigLocked is the v0.49 replacement for the old flat
// genConfigLocked (same contract: caller holds coreMu; npMu must
// also be held -- every caller in this subsystem does both, in
// that order). The generated config is validated with the real
// sing-box binary before it replaces the live one, so a bad
// node can never brick a working configuration.
func npGenConfigLocked() error {
	if len(npEnabledNodes()) == 0 && npStore.Warp == nil {
		return fmt.Errorf("no nodes")
	}
	sets := false
	if npStore.Mode == "rule" {
		// Presence ONLY: downloading happens in npRegen before
		// locks are taken (downloads consult the core state and
		// would deadlock under coreMu).
		sets = ruleSetsPresent()
	}
	cfg := buildCoreConfig(npSnapshotCfgLocked(sets))
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	dir := coreDir()
	_ = os.MkdirAll(dir, 0o755)
	tmp := filepath.Join(dir, "config.generated.json.tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if _, err := os.Stat(corePath()); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		check := exec.CommandContext(ctx, corePath(), "check", "-c", tmp)
		check.Dir = dir
		hideConsole(check)
		out, cerr := check.CombinedOutput()
		cancel()
		if cerr != nil {
			_ = os.Remove(tmp)
			msg := strings.TrimSpace(string(out))
			logf("core", "error", "sing-box 配置校验没过（新配置未生效）：%s", msg)
			return fmt.Errorf("sing-box 配置校验没过：%s", msg)
		}
	}
	if err := os.Rename(tmp, filepath.Join(dir, "config.generated.json")); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// restartCoreLocked restarts the core on the current config.
// Caller holds coreMu.
func restartCoreLocked() error {
	if !coreRunningLocked() {
		return nil
	}
	stopCoreLocked()
	return startCoreLocked()
}

// ---------- Clash API client ----------

// npSecretCache mirrors the store's Clash secret so the API
// client can read it WITHOUT taking npMu (status assembly holds
// npMu while calling into the Clash API -- locking there again
// would deadlock the daemon).
var npSecretCache atomic.Value // string

func clashSecret() string {
	if v := npSecretCache.Load(); v != nil {
		return v.(string)
	}
	return ""
}

// clashDo performs one Clash API call. Errors are returned
// plainly; the secret travels in the Authorization header only.
func clashDo(method, path string, body any, timeout time.Duration) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	}
	req, err := http.NewRequest(method, fmt.Sprintf("http://127.0.0.1:%d%s", clashAPIPort, path), rd)
	if err != nil {
		return 0, nil, err
	}
	if secret := clashSecret(); secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	return resp.StatusCode, b, nil
}

func clashAvailable() bool {
	if !settingNodeClashAPI() || !coreRunningQuick() {
		return false
	}
	code, _, err := clashDo(http.MethodGet, "/version", nil, 900*time.Millisecond)
	return err == nil && code == 200
}

// clashSelectorNow returns the selector's current target tag.
func clashSelectorNow() (string, bool) {
	code, b, err := clashDo(http.MethodGet, "/proxies/proxy", nil, 2*time.Second)
	if err != nil || code != 200 {
		return "", false
	}
	var v struct {
		Now string `json:"now"`
	}
	if json.Unmarshal(b, &v) != nil || v.Now == "" {
		return "", false
	}
	return v.Now, true
}

// clashGroupNow returns the current pick of a group outbound
// (used for the urltest group's live choice).
func clashGroupNow(tag string) (string, bool) {
	code, b, err := clashDo(http.MethodGet, "/proxies/"+url.PathEscape(tag), nil, 2*time.Second)
	if err != nil || code != 200 {
		return "", false
	}
	var v struct {
		Now string `json:"now"`
	}
	if json.Unmarshal(b, &v) != nil {
		return "", false
	}
	return v.Now, true
}

// clashHotSelect points the running core's selector at tag and
// verifies the switch actually landed.
func clashHotSelect(tag string) bool {
	code, _, err := clashDo(http.MethodPut, "/proxies/proxy", map[string]any{"name": tag}, 3*time.Second)
	if err != nil || (code != 200 && code != 204) {
		return false
	}
	now, ok := clashSelectorNow()
	return ok && now == tag
}

// ---------- live traffic (Clash /traffic stream) ----------

var traf struct {
	sync.Mutex
	up, down         int64 // bytes/sec, latest stream sample
	sessUp, sessDown int64 // accumulated while the core runs
	connected        bool
	wasRunning       bool
}

func trafficSnapshot() map[string]any {
	traf.Lock()
	defer traf.Unlock()
	return map[string]any{
		"available": traf.connected, "up": traf.up, "down": traf.down,
		"sessUp": traf.sessUp, "sessDown": traf.sessDown,
	}
}

// clashTrafficSupervisor keeps the /traffic stream open while
// the core runs with the API enabled, accumulating the session
// totals. Started once from nodeproInit.
func clashTrafficSupervisor() {
	for {
		running := coreRunningQuick() && settingNodeClashAPI()
		traf.Lock()
		if !running && traf.wasRunning {
			traf.up, traf.down = 0, 0
			traf.connected = false
			traf.wasRunning = false
		}
		connected := traf.connected
		traf.Unlock()
		if running && !connected {
			clashTrafficStream()
		}
		time.Sleep(2 * time.Second)
	}
}

func clashTrafficStream() {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/traffic", clashAPIPort), nil)
	if err != nil {
		return
	}
	if secret := clashSecret(); secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		if resp != nil {
			_ = resp.Body.Close()
		}
		time.Sleep(time.Second)
		return
	}
	defer resp.Body.Close()
	traf.Lock()
	traf.connected = true
	traf.wasRunning = true
	traf.Unlock()
	defer func() {
		traf.Lock()
		traf.connected = false
		traf.up, traf.down = 0, 0
		traf.Unlock()
	}()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 64*1024)
	for sc.Scan() {
		var v struct {
			Up   int64 `json:"up"`
			Down int64 `json:"down"`
		}
		if json.Unmarshal(sc.Bytes(), &v) == nil {
			traf.Lock()
			traf.up, traf.down = v.Up, v.Down
			traf.sessUp += v.Up
			traf.sessDown += v.Down
			traf.Unlock()
		}
		if !coreRunningQuick() {
			return
		}
	}
}

// ---------- latency tests ----------
//
// sing-box 1.14's Clash delay endpoint ignores the caller's
// test URL (it always probes its built-in gstatic address --
// verified empirically against the pinned binary), so an
// honest per-node HTTP measurement cannot ride on it. The
// daemon instead measures the way that stays under its own
// control: a throwaway sing-box instance per node (own port,
// only that node plus its detour chain), with the daemon's
// HTTP client fetching the configured test URL through it.
// TCP handshake timing remains as the reference when no core
// binary is available at all.

// tcpDialMs times a bare TCP handshake to the node's server --
// the reference measurement used when no core binary exists.
func tcpDialMs(server string, port int, timeout time.Duration) (int, error) {
	start := time.Now()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(server, strconv.Itoa(port)), timeout)
	if err != nil {
		return -1, err
	}
	_ = conn.Close()
	return int(time.Since(start) / time.Millisecond), nil
}

var npTestPortCtr atomic.Int64

// httpViaNode fetches the settings test URL through a
// throwaway sing-box instance whose only egress is the given
// node (plus its detour chain), and returns the time to
// response headers. Named errors throughout.
func httpViaNode(target nodeEntry, detour *nodeEntry, warp *warpState) (int, error) {
	bin := corePath()
	if _, err := os.Stat(bin); err != nil {
		return -1, fmt.Errorf("核心文件不在，没法起测速实例")
	}
	in := cfgInput{Mode: "global", Selected: target.ID, DNSMode: "direct",
		TestURL: settingNodeTestURL()}
	tgt := cfgNode{ID: target.ID, Outbound: target.Outbound}
	if target.Detour == "warp" && warp != nil {
		tgt.Detour = "warp"
		in.WarpOutbound = warpOutboundMap(warp)
	} else if target.Detour != "" && detour != nil && detour.ID == target.Detour {
		tgt.Detour = detour.ID
		in.Nodes = append(in.Nodes, cfgNode{ID: detour.ID, Outbound: detour.Outbound})
	}
	in.Nodes = append([]cfgNode{tgt}, in.Nodes...)
	cfg := buildCoreConfig(in)
	cfg["log"] = map[string]any{"disabled": true}
	port := 0
	for tries := 0; tries < 20; tries++ {
		cand := 19200 + int(npTestPortCtr.Add(1)%80)
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cand))
		if err == nil {
			_ = ln.Close()
			port = cand
			break
		}
	}
	if port == 0 {
		return -1, fmt.Errorf("测速端口都被占用了")
	}
	if inb, ok := cfg["inbounds"].([]map[string]any); ok && len(inb) > 0 {
		inb[0]["listen_port"] = port
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		return -1, err
	}
	tmp := filepath.Join(coreDir(), fmt.Sprintf("test-%d.json", port))
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return -1, err
	}
	defer os.Remove(tmp)
	timeout := time.Duration(settingNodeTestTimeoutMs())*time.Millisecond + 4*time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "run", "-c", tmp)
	hideConsole(cmd)
	if err := cmd.Start(); err != nil {
		return -1, fmt.Errorf("测速实例启动失败：%v", err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()
	// Wait for the test instance's mixed port.
	deadline := time.Now().Add(4 * time.Second)
	up := false
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
		if err == nil {
			_ = c.Close()
			up = true
			break
		}
		time.Sleep(60 * time.Millisecond)
	}
	if !up {
		return -1, fmt.Errorf("测速实例没起来（核心可能拒绝了该节点的配置）")
	}
	proxyURL, _ := url.Parse(fmt.Sprintf("socks5://127.0.0.1:%d", port))
	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		Timeout:   time.Duration(settingNodeTestTimeoutMs()) * time.Millisecond,
	}
	start := time.Now()
	resp, err := client.Get(settingNodeTestURL())
	if err != nil {
		if ctx.Err() != nil || os.IsTimeout(err) || strings.Contains(err.Error(), "Timeout") || strings.Contains(err.Error(), "deadline") {
			return -1, fmt.Errorf("测速超时（%d ms 内没经该节点打开测试页）", settingNodeTestTimeoutMs())
		}
		return -1, fmt.Errorf("经该节点打开测试页失败：%v", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		return -1, fmt.Errorf("测试页返回 HTTP %d（经该节点）", resp.StatusCode)
	}
	return int(time.Since(start) / time.Millisecond), nil
}

// testOneNode measures one node and records the result on it.
func testOneNode(id string) (ms int, kind string, errText string) {
	npMu.Lock()
	npLoadStoreLocked()
	var pn *nodeEntry
	var detour *nodeEntry
	var warp *warpState
	if found := npFindNode(id); found != nil {
		cp := *found
		pn = &cp
	}
	if pn != nil && pn.Detour != "" && pn.Detour != "warp" {
		if d := npFindNode(pn.Detour); d != nil {
			cp := *d
			detour = &cp
		}
	}
	if npStore.Warp != nil {
		cp := *npStore.Warp
		warp = &cp
	}
	var srv string
	var port int
	if pn != nil {
		srv, port = pn.Server, pn.Port
	}
	npMu.Unlock()
	if pn == nil {
		return -1, "", "节点不存在"
	}
	if _, err := os.Stat(corePath()); err == nil {
		if d, err := httpViaNode(*pn, detour, warp); err == nil {
			ms, kind = d, "http"
		} else {
			errText = err.Error()
		}
	} else {
		if d, err := tcpDialMs(srv, port, time.Duration(settingNodeTestTimeoutMs())*time.Millisecond); err == nil {
			ms, kind = d, "tcp"
		} else {
			errText = "TCP 握手失败：" + err.Error()
		}
	}
	npMu.Lock()
	if nn := npFindNode(id); nn != nil {
		nn.LatencyAt = time.Now().Unix()
		if errText == "" {
			nn.LatencyMs, nn.LatencyKind, nn.LatencyErr = ms, kind, ""
		} else {
			nn.LatencyMs, nn.LatencyKind, nn.LatencyErr = -1, "", errText
		}
		npSaveStoreLocked()
	}
	npMu.Unlock()
	return ms, kind, errText
}

// testNodes runs latency tests over the given node IDs with
// bounded concurrency and logs the outcome per node + a summary.
func testNodes(ids []string, why string) []map[string]any {
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := []map[string]any{}
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			sem <- struct{}{}
			ms, kind, errText := testOneNode(id)
			<-sem
			npMu.Lock()
			name := id
			if n := npFindNode(id); n != nil {
				name = n.Name
			}
			npMu.Unlock()
			if errText != "" {
				logf("node", "warn", "测速失败：%s（%s）：%s", name, why, errText)
			} else {
				logf("node", "info", "测速：%s（%s）%d ms（%s）", name, why, ms, kind)
			}
			mu.Lock()
			results = append(results, map[string]any{"id": id, "name": name, "ms": ms, "kind": kind, "error": errText})
			mu.Unlock()
		}(id)
	}
	wg.Wait()
	sort.Slice(results, func(i, j int) bool { return results[i]["id"].(string) < results[j]["id"].(string) })
	return results
}

// ---------- fallback watchdog ----------
//
// sing-box has no fallback outbound, so 故障转移 lives here:
// while the persisted selection is "fallback" and the core is
// running, the watchdog health-checks the member nodes in
// order and keeps the selector on the first one that answers.

var fb struct {
	sync.Mutex
	pick string // node ID currently chosen
}

func fallbackPick() string {
	fb.Lock()
	defer fb.Unlock()
	return fb.pick
}

func fallbackSetPick(id string) {
	fb.Lock()
	fb.pick = id
	fb.Unlock()
}

// fallbackEvaluate returns the first live member in order,
// using the same through-node HTTP measurement as the page's
// speed test (which also refreshes the member's latency).
func fallbackEvaluate(ids []string) string {
	for _, id := range ids {
		if _, _, errText := testOneNode(id); errText == "" {
			return id
		}
	}
	return ""
}

func fallbackWatchdog() {
	for {
		time.Sleep(30 * time.Second)
		if !servicesActive() || !coreRunningQuick() {
			continue
		}
		npMu.Lock()
		npLoadStoreLocked()
		sel := npStore.Selected
		var ids []string
		for _, n := range npEnabledNodes() {
			ids = append(ids, n.ID)
		}
		npMu.Unlock()
		if sel != "fallback" || len(ids) == 0 {
			continue
		}
		cur := fallbackPick()
		// Only re-evaluate on a cadence: is the current pick
		// still alive? If yes, an earlier member may have
		// recovered (Clash fallback returns to the first
		// available), so a full ordered evaluation is the
		// honest check -- but it costs one delay test per
		// member; 30 s cadence keeps that polite.
		pick := fallbackEvaluate(ids)
		if pick == "" || pick == cur {
			continue
		}
		fallbackSetPick(pick)
		npMu.Lock()
		name := pick
		if n := npFindNode(pick); n != nil {
			name = n.Name
		}
		npMu.Unlock()
		if clashHotSelect(nodeTag(pick)) {
			logf("node", "info", "故障转移：出口已切到第一个存活节点 %s", name)
		} else {
			_ = npRegen(true)
			logf("node", "info", "故障转移：出口已切到第一个存活节点 %s（重启核心生效）", name)
		}
	}
}

// ---------- IPv6 egress detection ----------

func isIPv6Literal(s string) bool {
	s = strings.Trim(strings.TrimSpace(s), "[]")
	ip := net.ParseIP(s)
	return ip != nil && strings.Contains(s, ":")
}

// ipv6EgressProbe is the testable core: it reports whether any
// of the given IPv6 endpoints accepts a TCP connection.
func ipv6EgressProbe(addrs []string, dial func(network, addr string, timeout time.Duration) error) bool {
	for _, a := range addrs {
		if err := dial("tcp", a, 1500*time.Millisecond); err == nil {
			return true
		}
	}
	return false
}

var v6 struct {
	sync.Mutex
	known bool
	ok    bool
	at    time.Time
}

func refreshIPv6Async() {
	v6.Lock()
	if v6.known && time.Since(v6.at) < 5*time.Minute {
		v6.Unlock()
		return
	}
	v6.Unlock()
	go func() {
		ok := ipv6EgressProbe(
			[]string{"[2606:4700:4700::1111]:443", "[2001:4860:4860::8888]:443"},
			func(network, addr string, timeout time.Duration) error {
				c, err := net.DialTimeout(network, addr, timeout)
				if err == nil {
					_ = c.Close()
				}
				return err
			})
		v6.Lock()
		v6.known, v6.ok, v6.at = true, ok, time.Now()
		v6.Unlock()
	}()
}

func localIPv6() (known, ok bool) {
	v6.Lock()
	defer v6.Unlock()
	return v6.known, v6.ok
}

// ---------- subscriptions ----------

func parseSubUserInfo(h string) *subUserInfo {
	h = strings.TrimSpace(h)
	if h == "" {
		return nil
	}
	ui := &subUserInfo{Has: true}
	seen := false
	for _, part := range strings.Split(h, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		v, err := strconv.ParseInt(strings.TrimSpace(kv[1]), 10, 64)
		if err != nil {
			continue
		}
		switch strings.TrimSpace(kv[0]) {
		case "upload":
			ui.Upload, seen = v, true
		case "download":
			ui.Download, seen = v, true
		case "total":
			ui.Total, seen = v, true
		case "expire":
			ui.Expire = v
		}
	}
	if !seen {
		return nil
	}
	return ui
}

// npFetchSubRaw downloads a subscription body + its userinfo
// header, through the node when acceleration is live, with the
// usual direct retry (same route policy as dlGet).
func npFetchSubRaw(rawurl string) ([]byte, *subUserInfo, error) {
	try := func(c *http.Client) ([]byte, http.Header, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawurl, nil)
		if err != nil {
			return nil, nil, err
		}
		req.Header.Set("User-Agent", "MJTerminal/"+AppVersion+" (sing-box 1.14.4)")
		resp, err := c.Do(req)
		if err != nil {
			return nil, nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, nil, fmt.Errorf("订阅服务器返回 HTTP %d", resp.StatusCode)
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
		return b, resp.Header, err
	}
	proxied := proxyActive()
	body, hdr, err := try(dlClient())
	if err != nil && proxied {
		body, hdr, err = try(http.DefaultClient)
	}
	if err != nil {
		return nil, nil, err
	}
	return body, parseSubUserInfo(hdr.Get("Subscription-Userinfo")), nil
}

// parseSubContent parses a subscription body in any of the
// supported shapes: share-link lines, base64 of the same, or a
// Clash YAML document. Unsupported nodes are counted, not
// silently dropped.
func parseSubContent(body []byte) (entries []nodeEntry, skipped int, format string, err error) {
	s := strings.TrimSpace(string(body))
	if s == "" {
		return nil, 0, "", fmt.Errorf("订阅内容为空")
	}
	if isClashYAML(s) {
		ents, sk, yerr := parseClashYAML(s)
		return ents, sk, "Clash YAML", yerr
	}
	if !strings.Contains(s, "://") {
		if dec, derr := b64decode(s); derr == nil {
			s = dec
		}
	}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, "://") {
			continue
		}
		n, perr := parseShareLink(line)
		if perr != nil {
			skipped++
			continue
		}
		n.ID = nodeIDFor(n.Outbound)
		n.LatencyMs = -1
		entries = append(entries, *n)
	}
	return entries, skipped, "分享链接", nil
}

func isClashYAML(s string) bool {
	return strings.Contains(s, "\nproxies:") || strings.HasPrefix(s, "proxies:")
}

// ---------- Clash YAML -> sing-box outbounds ----------

type clashProxy struct {
	Name              string `yaml:"name"`
	Type              string `yaml:"type"`
	Server            string `yaml:"server"`
	Port              int    `yaml:"port"`
	Cipher            string `yaml:"cipher"`
	Password          string `yaml:"password"`
	UUID              string `yaml:"uuid"`
	AlterID           int    `yaml:"alterId"`
	Network           string `yaml:"network"`
	TLS               bool   `yaml:"tls"`
	ServerName        string `yaml:"servername"`
	SNI               string `yaml:"sni"`
	SkipCertVerify    bool   `yaml:"skip-cert-verify"`
	ClientFingerprint string `yaml:"client-fingerprint"`
	Flow              string `yaml:"flow"`
	Plugin            string `yaml:"plugin"`
	Obfs              string `yaml:"obfs"`
	ObfsPassword      string `yaml:"obfs-password"`
	WSOpts            struct {
		Path    string            `yaml:"path"`
		Headers map[string]string `yaml:"headers"`
	} `yaml:"ws-opts"`
	GrpcOpts struct {
		ServiceName string `yaml:"grpc-service-name"`
	} `yaml:"grpc-opts"`
	RealityOpts struct {
		PublicKey string `yaml:"public-key"`
		ShortID   string `yaml:"short-id"`
	} `yaml:"reality-opts"`
}

type clashDoc struct {
	Proxies []clashProxy `yaml:"proxies"`
}

func parseClashYAML(s string) ([]nodeEntry, int, error) {
	var doc clashDoc
	if err := yaml.Unmarshal([]byte(s), &doc); err != nil {
		return nil, 0, fmt.Errorf("Clash YAML 解析失败：%v", err)
	}
	var out []nodeEntry
	skipped := 0
	for _, cp := range doc.Proxies {
		ob, ok := clashProxyToOutbound(cp)
		if !ok {
			skipped++
			continue
		}
		name := cp.Name
		if name == "" {
			name = cp.Server
		}
		typ, _ := ob["type"].(string)
		e := nodeEntry{Name: name, Type: typ, Server: cp.Server, Port: cp.Port, Outbound: ob, LatencyMs: -1}
		e.ID = nodeIDFor(ob)
		out = append(out, e)
	}
	return out, skipped, nil
}

func clashProxyToOutbound(cp clashProxy) (map[string]any, bool) {
	sni := cp.ServerName
	if sni == "" {
		sni = cp.SNI
	}
	var out map[string]any
	switch strings.ToLower(cp.Type) {
	case "ss":
		if cp.Plugin != "" {
			return nil, false // ss plugins (obfs / v2ray-plugin) unsupported
		}
		out = map[string]any{"type": "shadowsocks", "server": cp.Server, "server_port": cp.Port,
			"method": cp.Cipher, "password": cp.Password}
	case "vmess":
		sec := cp.Cipher
		if sec == "" {
			sec = "auto"
		}
		out = map[string]any{"type": "vmess", "server": cp.Server, "server_port": cp.Port,
			"uuid": cp.UUID, "alter_id": cp.AlterID, "security": sec}
		if cp.TLS {
			addTLS(out, sni, cp.SkipCertVerify, nil, cp.ClientFingerprint)
		}
	case "vless":
		out = map[string]any{"type": "vless", "server": cp.Server, "server_port": cp.Port, "uuid": cp.UUID}
		if cp.Flow != "" {
			out["flow"] = cp.Flow
		}
		if cp.RealityOpts.PublicKey != "" {
			reality := map[string]any{"enabled": true, "public_key": cp.RealityOpts.PublicKey, "short_id": cp.RealityOpts.ShortID}
			addTLS(out, sni, cp.SkipCertVerify, reality, cp.ClientFingerprint)
		} else if cp.TLS {
			addTLS(out, sni, cp.SkipCertVerify, nil, cp.ClientFingerprint)
		}
	case "trojan":
		out = map[string]any{"type": "trojan", "server": cp.Server, "server_port": cp.Port, "password": cp.Password}
		addTLS(out, sni, cp.SkipCertVerify, nil, cp.ClientFingerprint)
	case "hysteria2", "hy2":
		out = map[string]any{"type": "hysteria2", "server": cp.Server, "server_port": cp.Port, "password": cp.Password}
		addTLS(out, sni, cp.SkipCertVerify, nil, "")
		if cp.Obfs == "salamander" && cp.ObfsPassword != "" {
			out["obfs"] = map[string]any{"type": "salamander", "password": cp.ObfsPassword}
		}
	default:
		return nil, false
	}
	switch cp.Network {
	case "ws":
		addTransport(out, "ws", cp.WSOpts.Path, cp.WSOpts.Headers["Host"], "")
	case "grpc":
		addTransport(out, "grpc", "", "", cp.GrpcOpts.ServiceName)
	}
	return out, true
}

// npUpdateSubscription fetches and re-parses one subscription,
// preserving the current selection (by node identity, then by
// name) and per-node detour/latency where the node survives.
func npUpdateSubscription(subID, why string) (int, error) {
	npMu.Lock()
	npLoadStoreLocked()
	sub := npFindSub(subID)
	if sub == nil {
		npMu.Unlock()
		return 0, fmt.Errorf("订阅不存在")
	}
	subCopy := *sub
	npMu.Unlock()

	body, ui, err := npFetchSubRaw(subCopy.URL)
	if err != nil {
		logf("node", "error", "订阅更新失败（%s）：%s：%v", why, subCopy.Name, err)
		return 0, err
	}
	entries, skipped, format, err := parseSubContent(body)
	if err != nil {
		logf("node", "error", "订阅更新失败（%s）：%s：%v", why, subCopy.Name, err)
		return 0, err
	}
	if len(entries) == 0 {
		err := fmt.Errorf("订阅里没解析出可用节点（跳过 %d 个不支持的）", skipped)
		logf("node", "error", "订阅更新失败（%s）：%s：%v", why, subCopy.Name, err)
		return 0, err
	}

	npMu.Lock()
	// Preserve what can be preserved from the old group.
	oldByID := map[string]nodeEntry{}
	var selName string
	for _, n := range nodes {
		if n.Group == subID {
			oldByID[n.ID] = n
			if npStore.Selected == n.ID {
				selName = n.Name
			}
		}
	}
	kept := nodes[:0]
	for _, n := range nodes {
		if n.Group != subID {
			kept = append(kept, n)
		}
	}
	nodes = kept
	selKept := false
	// Node identity is the endpoint: an identical endpoint
	// already living in another group (or earlier in this
	// batch) is the same node -- keeping both would emit a
	// duplicate outbound tag and sing-box would refuse the
	// whole config. First copy wins; the rest are counted as
	// skipped so the update log says what happened.
	seenIDs := map[string]bool{}
	for _, n := range nodes {
		seenIDs[n.ID] = true
	}
	dupSkipped := 0
	addedCount := 0
	for _, e := range entries {
		e.Group = subID
		if e.ID == "" || seenIDs[e.ID] {
			dupSkipped++
			continue
		}
		seenIDs[e.ID] = true
		addedCount++
		if old, ok := oldByID[e.ID]; ok {
			e.Detour, e.LatencyMs, e.LatencyKind, e.LatencyErr, e.LatencyAt =
				old.Detour, old.LatencyMs, old.LatencyKind, old.LatencyErr, old.LatencyAt
		}
		nodes = append(nodes, e)
		if npStore.Selected == e.ID {
			selKept = true
		}
	}
	if selName != "" && !selKept {
		// Selection pointed into the old group: keep it by
		// name when a same-named node exists, else fall back
		// (resolveSelectedTag lands on the first node) and
		// say so in the log.
		byName := ""
		for _, e := range entries {
			if e.Name == selName {
				byName = e.ID
				break
			}
		}
		if byName != "" {
			npStore.Selected = byName
			logf("node", "info", "订阅更新后已按名称保留当前出口：%s", selName)
		} else {
			npStore.Selected = ""
			logf("node", "warn", "订阅更新后原出口 %s 已不在订阅里，出口回退到第一个可用节点", selName)
		}
	}
	sub = npFindSub(subID)
	if sub != nil {
		sub.UpdatedAt = time.Now().Unix()
		if ui != nil {
			sub.UserInfo = *ui
		}
	}
	npSaveStoreLocked()
	npMu.Unlock()

	if err := npRegen(true); err != nil {
		logf("node", "error", "订阅 %s 更新后重建配置失败：%v", subCopy.Name, err)
		return addedCount, err
	}
	msg := fmt.Sprintf("订阅更新（%s）：%s 共 %d 个节点（%s）", why, subCopy.Name, addedCount, format)
	if skipped-dupSkipped > 0 {
		msg += fmt.Sprintf("，跳过 %d 个不支持的节点", skipped-dupSkipped)
	}
	if dupSkipped > 0 {
		msg += fmt.Sprintf("，另有 %d 个与已有节点端点完全相同，已跳过", dupSkipped)
	}
	logf("node", "info", "%s", msg)
	return addedCount, nil
}

func npSubAutoLoop() {
	for {
		time.Sleep(time.Minute)
		if !servicesActive() || !settingNodeSubAuto() {
			continue
		}
		npMu.Lock()
		npLoadStoreLocked()
		var due []string
		now := time.Now().Unix()
		for _, s := range npStore.Subs {
			if !s.Enabled {
				continue
			}
			mins := s.AutoMins
			if mins <= 0 {
				mins = settingNodeSubIntervalMin()
			}
			if s.UpdatedAt == 0 || now-s.UpdatedAt >= int64(mins)*60 {
				due = append(due, s.ID)
			}
		}
		npMu.Unlock()
		for _, id := range due {
			if _, err := npUpdateSubscription(id, "自动"); err != nil {
				// already logged inside; the loop stays quiet
				_ = err
			}
		}
	}
}

// ---------- WARP bridge ----------

func warpOutboundMap(ws *warpState) map[string]any {
	host, port := ws.Endpoint, 2408
	if h, p, err := net.SplitHostPort(ws.Endpoint); err == nil {
		host = h
		if pi, err := strconv.Atoi(p); err == nil {
			port = pi
		}
	}
	addrs := []string{}
	if ws.AddressV4 != "" {
		addrs = append(addrs, strings.Split(ws.AddressV4, "/")[0]+"/32")
	}
	if ws.AddressV6 != "" {
		addrs = append(addrs, strings.Split(ws.AddressV6, "/")[0]+"/128")
	}
	return map[string]any{
		"type": "wireguard", "address": addrs, "private_key": ws.PrivateKey, "mtu": 1280,
		"peers": []map[string]any{{
			"address": host, "port": port, "public_key": ws.PeerPublicKey,
			"allowed_ips": []string{"0.0.0.0/0", "::/0"}, "reserved": ws.Reserved,
		}},
	}
}

type warpRegResponse struct {
	Config struct {
		Peers []struct {
			PublicKey string `json:"public_key"`
			Endpoint  struct {
				Host string `json:"host"`
			} `json:"endpoint"`
		} `json:"peers"`
		Interface struct {
			Addresses struct {
				V4 string `json:"v4"`
				V6 string `json:"v6"`
			} `json:"addresses"`
		} `json:"interface"`
		ClientID string `json:"client_id"`
	} `json:"config"`
}

// npWarpRegister performs Cloudflare's public WARP registration
// (the wgcf flow) on this machine and stores the resulting
// WireGuard identity in the node store. Failures are named --
// no placeholder WARP entry is ever created.
func npWarpRegister() error {
	priv := make([]byte, 32)
	if _, err := rand.Read(priv); err != nil {
		return err
	}
	pub, err := curve25519.X25519(priv, curve25519.Basepoint)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"key":        base64.StdEncoding.EncodeToString(pub),
		"install_id": "",
		"tos":        time.Now().UTC().Format("2006-01-02T15:04:05.999Z"),
		"model":      "PC",
		"type":       "Android",
		"locale":     "en_US",
	}
	post := func(c *http.Client) (*warpRegResponse, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		b, _ := json.Marshal(payload)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			"https://api.cloudflareclient.com/v0a2158/reg", strings.NewReader(string(b)))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "okhttp/3.12.1")
		resp, err := c.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		rb, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
		if resp.StatusCode != 200 {
			snip := strings.TrimSpace(string(rb))
			if len(snip) > 200 {
				snip = snip[:200]
			}
			return nil, fmt.Errorf("WARP 注册接口返回 HTTP %d：%s", resp.StatusCode, snip)
		}
		var wr warpRegResponse
		if err := json.Unmarshal(rb, &wr); err != nil {
			return nil, fmt.Errorf("WARP 注册响应解析失败：%v", err)
		}
		return &wr, nil
	}
	wr, err := post(dlClient())
	if err != nil && proxyActive() {
		wr, err = post(http.DefaultClient)
	}
	if err != nil {
		logf("node", "error", "WARP 注册失败：%v", err)
		return err
	}
	if len(wr.Config.Peers) == 0 || wr.Config.Peers[0].PublicKey == "" {
		err := fmt.Errorf("WARP 注册响应里没有对端信息")
		logf("node", "error", "%v", err)
		return err
	}
	var reserved []int
	if raw, derr := base64.StdEncoding.DecodeString(wr.Config.ClientID); derr == nil {
		for _, by := range raw {
			reserved = append(reserved, int(by))
		}
	}
	ws := &warpState{
		PrivateKey:    base64.StdEncoding.EncodeToString(priv),
		PeerPublicKey: wr.Config.Peers[0].PublicKey,
		Endpoint:      wr.Config.Peers[0].Endpoint.Host,
		AddressV4:     wr.Config.Interface.Addresses.V4,
		AddressV6:     wr.Config.Interface.Addresses.V6,
		Reserved:      reserved,
		RegisteredAt:  time.Now().Unix(),
	}
	npMu.Lock()
	npLoadStoreLocked()
	npStore.Warp = ws
	npSaveStoreLocked()
	npMu.Unlock()
	if err := npRegen(true); err != nil {
		logf("node", "error", "WARP 已注册，但重建配置失败：%v", err)
		return err
	}
	logf("node", "info", "WARP 中继已注册（端点 %s），可作为前置或出口使用", ws.Endpoint)
	return nil
}

// ---------- applying state changes ----------

// npRegen rebuilds the generated config from the store,
// restarting the core when asked and it is running. Rule-set
// fetching happens BEFORE any lock is taken (downloads consult
// the core state, which needs coreMu -- holding it here would
// deadlock).
func npRegen(restart bool) error {
	npMu.Lock()
	npLoadStoreLocked()
	mode := npStore.Mode
	npMu.Unlock()
	if mode == "rule" {
		if !ensureRuleSets() {
			logf("node", "warn", "规则集（GeoSite/GeoIP CN）暂不可用：规则模式下绕过大陆未生效（自定义规则与私有地址直连照常）")
		}
	}
	npMu.Lock()
	coreMu.Lock()
	err := npGenConfigLocked()
	if err == nil && restart {
		err = restartCoreLocked()
	}
	coreMu.Unlock()
	npMu.Unlock()
	return err
}

// npSetMode switches the routing mode (persist + regenerate +
// restart). Mode changes always go through a core restart:
// the generated config is the single source of truth for the
// mode, so a runtime-only override could silently revert on
// the next core restart.
func npSetMode(mode string) error {
	if !nodeModes()[mode] {
		return fmt.Errorf("未知模式：%s", mode)
	}
	npMu.Lock()
	npLoadStoreLocked()
	old := npStore.Mode
	npStore.Mode = mode
	npSaveStoreLocked()
	npMu.Unlock()
	if err := npRegen(true); err != nil {
		logf("core", "error", "模式切换失败（%s → %s）：%v", npModeName(old), npModeName(mode), err)
		return err
	}
	if coreRunningQuick() {
		logf("core", "info", "模式已切换为%s（核心已重启生效）", npModeName(mode))
	} else {
		logf("core", "info", "模式已切换为%s（核心启动时生效）", npModeName(mode))
	}
	return nil
}

// npSelectExit switches the egress. Hot via the Clash API when
// possible (verified after the PUT); otherwise regenerate +
// restart. The selection is persisted either way, and the
// generated config's selector default carries it across core
// restarts.
func npSelectExit(target string) (string, error) {
	npMu.Lock()
	npLoadStoreLocked()
	// Resolve + validate the target, and the tag it maps to.
	var tag, name string
	switch target {
	case "auto":
		if len(npEnabledNodes()) == 0 {
			npMu.Unlock()
			return "", fmt.Errorf("还没有可用节点")
		}
		tag, name = "auto", "自动选择"
	case "fallback":
		en := npEnabledNodes()
		if len(en) == 0 {
			npMu.Unlock()
			return "", fmt.Errorf("还没有可用节点")
		}
		name = "故障转移"
	case "warp":
		if npStore.Warp == nil {
			npMu.Unlock()
			return "", fmt.Errorf("WARP 中继还没注册")
		}
		tag, name = "node-warp", "WARP 中继"
	default:
		n := npFindNode(target)
		if n == nil {
			npMu.Unlock()
			return "", fmt.Errorf("节点不存在")
		}
		// A node in a disabled subscription cannot be picked.
		enabled := false
		for _, e := range npEnabledNodes() {
			if e.ID == target {
				enabled = true
			}
		}
		if !enabled {
			npMu.Unlock()
			return "", fmt.Errorf("这个节点所在的订阅已停用")
		}
		tag, name = nodeTag(target), n.Name
	}
	npStore.Selected = target
	npSaveStoreLocked()
	npMu.Unlock()

	if target == "fallback" {
		// Point the watchdog's pick at the first live member
		// right away instead of waiting for its tick.
		npMu.Lock()
		var ids []string
		for _, e := range npEnabledNodes() {
			ids = append(ids, e.ID)
		}
		npMu.Unlock()
		pick := fallbackEvaluate(ids)
		if pick == "" && len(ids) > 0 {
			pick = ids[0]
		}
		fallbackSetPick(pick)
		tag = nodeTag(pick)
	}

	if coreRunningQuick() && settingNodeClashAPI() && clashHotSelect(tag) {
		logf("core", "info", "出口已切换为 %s（热切换，核心未重启）", name)
		return "hot", nil
	}
	if err := npRegen(true); err != nil {
		logf("core", "error", "出口切换失败（%s）：%v", name, err)
		return "", err
	}
	if coreRunningQuick() {
		logf("core", "info", "出口已切换为 %s（重启核心生效）", name)
	} else {
		logf("core", "info", "出口已选定 %s（核心启动时生效）", name)
	}
	return "restart", nil
}

// ---------- status payload ----------

func npExitJSON() map[string]any {
	sel := npStore.Selected
	en := npEnabledNodes()
	byID := map[string]nodeEntry{}
	for _, n := range en {
		byID[n.ID] = n
	}
	nodeBrief := func(n nodeEntry) map[string]any {
		return map[string]any{"kind": "node", "id": n.ID, "name": n.Name, "type": n.Type,
			"latencyMs": n.LatencyMs, "latencyKind": n.LatencyKind}
	}
	switch sel {
	case "auto":
		out := map[string]any{"kind": "auto", "id": "auto", "name": "自动选择"}
		if now, ok := clashGroupNow("auto"); ok {
			if id, found := strings.CutPrefix(now, "node-"); found {
				if n, yes := byID[id]; yes {
					out["currentId"], out["currentName"] = n.ID, n.Name
				}
			}
		}
		return out
	case "fallback":
		out := map[string]any{"kind": "fallback", "id": "fallback", "name": "故障转移"}
		if n, yes := byID[fallbackPick()]; yes {
			out["currentId"], out["currentName"] = n.ID, n.Name
		}
		return out
	case "warp":
		if npStore.Warp != nil {
			return map[string]any{"kind": "warp", "id": "warp", "name": "WARP 中继", "type": "wireguard"}
		}
	}
	if n, yes := byID[sel]; yes {
		return nodeBrief(n)
	}
	if len(en) > 0 {
		return nodeBrief(en[0])
	}
	if npStore.Warp != nil {
		return map[string]any{"kind": "warp", "id": "warp", "name": "WARP 中继", "type": "wireguard"}
	}
	return map[string]any{"kind": "none", "name": "还没有节点"}
}

func npNodeJSON(n nodeEntry, selectedID string, v6ok bool) map[string]any {
	m := map[string]any{
		"id": n.ID, "name": n.Name, "type": n.Type, "server": n.Server, "port": n.Port,
		"group": n.Group, "detour": n.Detour, "latencyMs": n.LatencyMs,
		"latencyKind": n.LatencyKind, "latencyErr": n.LatencyErr, "latencyAt": n.LatencyAt,
		"selected": n.ID == selectedID,
	}
	if isIPv6Literal(n.Server) {
		m["ipv6"] = true
		if !v6ok {
			m["unreachable6"] = true
		}
	}
	return m
}

func npStatus() map[string]any {
	refreshIPv6Async()
	npMu.Lock()
	npLoadStoreLocked()
	_, v6ok := localIPv6()
	sel := npStore.Selected
	groups := []map[string]any{}
	// Virtual group first: 自动选择 + 故障转移.
	en := npEnabledNodes()
	if len(en) > 0 {
		vnodes := []map[string]any{
			{"id": "auto", "name": "自动选择", "type": "urltest", "virtual": true,
				"selected": sel == "auto", "members": len(en)},
			{"id": "fallback", "name": "故障转移", "type": "fallback", "virtual": true,
				"selected": sel == "fallback", "members": len(en)},
		}
		if now, ok := clashGroupNow("auto"); ok {
			vnodes[0]["currentTag"] = now
		}
		if p := fallbackPick(); p != "" {
			vnodes[1]["currentId"] = p
		}
		groups = append(groups, map[string]any{"id": "__virtual", "name": "虚拟组", "kind": "virtual", "nodes": vnodes})
	}
	// Manual group (plus the WARP bridge as a member when set).
	var manual []map[string]any
	for _, n := range nodes {
		if n.Group == "manual" {
			manual = append(manual, npNodeJSON(n, sel, v6ok))
		}
	}
	if npStore.Warp != nil {
		manual = append(manual, map[string]any{
			"id": "warp", "name": "WARP 中继", "type": "wireguard", "server": npStore.Warp.Endpoint,
			"group": "manual", "isWarp": true, "selected": sel == "warp", "latencyMs": -1,
		})
	}
	groups = append(groups, map[string]any{"id": "manual", "name": "手动", "kind": "manual", "nodes": manual})
	// Subscription groups.
	for _, s := range npStore.Subs {
		var sn []map[string]any
		for _, n := range nodes {
			if n.Group == s.ID {
				sn = append(sn, npNodeJSON(n, sel, v6ok))
			}
		}
		sub := map[string]any{
			"id": s.ID, "name": s.Name, "enabled": s.Enabled, "updatedAt": s.UpdatedAt,
			"autoMins": s.AutoMins, "urlHost": subURLHost(s.URL), "nodeCount": len(sn),
		}
		if s.UserInfo.Has {
			sub["userInfo"] = map[string]any{
				"upload": s.UserInfo.Upload, "download": s.UserInfo.Download,
				"total": s.UserInfo.Total, "expire": s.UserInfo.Expire,
			}
		}
		groups = append(groups, map[string]any{
			"id": s.ID, "name": s.Name, "kind": "sub", "sub": sub, "nodes": sn,
		})
	}
	rules := npStore.Rules
	if rules == nil {
		rules = []userRule{}
	}
	st := map[string]any{
		"mode": npStore.Mode, "selected": sel, "exit": npExitJSON(),
		"coreRunning": coreRunningQuick(), "clashOn": settingNodeClashAPI(),
		"clashOK": false, "traffic": trafficSnapshot(),
		"dnsMode": npStore.DNSMode, "dnsCustom": npStore.DNSCustom,
		"groups": groups, "rules": rules,
		"ipv6Known": false, "ipv6Egress": v6ok,
		"warp":    map[string]any{"registered": npStore.Warp != nil},
		"testURL": settingNodeTestURL(), "testTimeoutMs": settingNodeTestTimeoutMs(),
		"defaultRules": []map[string]any{
			{"label": "局域网 / 私有地址", "action": "直连"},
			{"label": "中国大陆站点（GeoSite CN）", "action": "直连"},
			{"label": "中国大陆 IP（GeoIP CN）", "action": "直连"},
			{"label": "其余流量", "action": "代理"},
		},
	}
	if known, _ := localIPv6(); known {
		st["ipv6Known"] = true
	}
	if npStore.Warp != nil {
		st["warp"] = map[string]any{"registered": true, "endpoint": npStore.Warp.Endpoint, "registeredAt": npStore.Warp.RegisteredAt}
	}
	npMu.Unlock()
	st["clashOK"] = clashAvailable()
	return st
}

func subURLHost(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	return ""
}

// ---------- HTTP handlers (local mux only) ----------

func npStatusHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, npStatus())
}

func npModeHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apiErrBadRequest(w, errCodeBadJSON, "请求内容不对")
		return
	}
	if err := npSetMode(body.Mode); err != nil {
		apiErrBadRequest(w, errCodeBadRequest, err.Error())
		return
	}
	busPublish("nodes", "") // v0.59 block 3: live sync
	writeJSON(w, npStatus())
}

func npSelectHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Target string `json:"target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apiErrBadRequest(w, errCodeBadJSON, "请求内容不对")
		return
	}
	how, err := npSelectExit(body.Target)
	if err != nil {
		apiErrBadRequest(w, errCodeBadRequest, err.Error())
		return
	}
	st := npStatus()
	st["applied"] = how
	writeJSON(w, st)
}

func npTestHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Target string `json:"target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apiErrBadRequest(w, errCodeBadJSON, "请求内容不对")
		return
	}
	target := body.Target
	npMu.Lock()
	npLoadStoreLocked()
	var ids []string
	switch {
	case target == "all":
		for _, n := range npEnabledNodes() {
			ids = append(ids, n.ID)
		}
	case strings.HasPrefix(target, "group:"):
		gid := strings.TrimPrefix(target, "group:")
		for _, n := range nodes {
			if n.Group == gid {
				if gid == "manual" || (npFindSub(gid) != nil && npFindSub(gid).Enabled) {
					ids = append(ids, n.ID)
				}
			}
		}
	case target == "auto" || target == "fallback":
		// A virtual group has no single outbound to measure:
		// test every member and report the member results
		// plus, for 自动选择， the fastest one (what urltest
		// would pick).
		for _, n := range npEnabledNodes() {
			ids = append(ids, n.ID)
		}
		npMu.Unlock()
		if len(ids) == 0 {
			apiErrBadRequest(w, errCodeInvalid, "没有可测速的节点")
			return
		}
		results := testNodes(ids, "虚拟组")
		if target == "auto" {
			best := -1
			for _, r0 := range results {
				if r0["error"] == "" {
					if ms, ok := r0["ms"].(int); ok && (best < 0 || ms < best) {
						best = ms
					}
				}
			}
			results = append(results, map[string]any{"id": "auto", "name": "自动选择（最快成员）", "ms": best, "kind": "http", "error": ""})
		}
		writeJSON(w, map[string]any{"results": results})
		return
	default:
		if npFindNode(target) != nil {
			ids = []string{target}
		}
	}
	npMu.Unlock()
	if len(ids) == 0 {
		apiErrBadRequest(w, errCodeInvalid, "没有可测速的节点")
		return
	}
	results := testNodes(ids, "手动")
	writeJSON(w, map[string]any{"results": results})
}

func npDetourHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Node   string `json:"node"`
		Detour string `json:"detour"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apiErrBadRequest(w, errCodeBadJSON, "请求内容不对")
		return
	}
	npMu.Lock()
	npLoadStoreLocked()
	n := npFindNode(body.Node)
	if n == nil {
		npMu.Unlock()
		writeAPIError(w, http.StatusNotFound, errCodeNotFound, "节点不存在")
		return
	}
	switch {
	case body.Detour == "":
		n.Detour = ""
	case body.Detour == "warp":
		if npStore.Warp == nil {
			npMu.Unlock()
			apiErrBadRequest(w, errCodeInvalid, "WARP 中继还没注册")
			return
		}
		n.Detour = "warp"
	case body.Detour == body.Node:
		npMu.Unlock()
		apiErrBadRequest(w, errCodeInvalid, "前置不能是节点自己")
		return
	default:
		if npFindNode(body.Detour) == nil {
			npMu.Unlock()
			apiErrBadRequest(w, errCodeInvalid, "前置节点不存在")
			return
		}
		n.Detour = body.Detour
	}
	name, det := n.Name, n.Detour
	npSaveStoreLocked()
	npMu.Unlock()
	if err := npRegen(true); err != nil {
		apiErrBadRequest(w, errCodeBadRequest, err.Error())
		return
	}
	if det == "" {
		logf("core", "info", "节点 %s 的前置已清除（直连节点服务器）", name)
	} else {
		logf("core", "info", "节点 %s 的前置已设为 %s", name, det)
	}
	busPublish("nodes", "") // v0.59 block 3: live sync
	writeJSON(w, npStatus())
}

func npSubsHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action   string `json:"action"`
		ID       string `json:"id"`
		Name     string `json:"name"`
		URL      string `json:"url"`
		Enabled  *bool  `json:"enabled"`
		AutoMins *int   `json:"autoMins"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apiErrBadRequest(w, errCodeBadJSON, "请求内容不对")
		return
	}
	switch body.Action {
	case "add":
		raw := strings.TrimSpace(body.URL)
		if raw == "" {
			apiErrBadRequest(w, errCodeInvalid, "订阅地址不能为空")
			return
		}
		name := strings.TrimSpace(body.Name)
		if name == "" {
			name = subURLHost(raw)
		}
		if name == "" {
			name = "订阅"
		}
		npMu.Lock()
		npLoadStoreLocked()
		sub := subEntry{ID: newRandID("sub-"), Name: name, URL: raw, Enabled: true}
		npStore.Subs = append(npStore.Subs, sub)
		npSaveStoreLocked()
		npMu.Unlock()
		cnt, err := npUpdateSubscription(sub.ID, "手动")
		if err != nil {
			// Keep the subscription (the URL may just be
			// temporarily unreachable); the error says why.
			st := npStatus()
			st["error"] = fmt.Sprintf("订阅已添加，但首次拉取失败：%v", err)
			st["added"] = cnt
			writeJSON(w, st)
			return
		}
		logf("node", "info", "已添加订阅：%s（%s）", name, subURLHost(raw))
		st := npStatus()
		st["added"] = cnt
		writeJSON(w, st)
	case "remove":
		npMu.Lock()
		npLoadStoreLocked()
		sub := npFindSub(body.ID)
		if sub == nil {
			npMu.Unlock()
			writeAPIError(w, http.StatusNotFound, errCodeNotFound, "订阅不存在")
			return
		}
		name := sub.Name
		npStore.Subs = append(npStore.Subs[:0:0], filterSubs(npStore.Subs, body.ID)...)
		kept := nodes[:0]
		for _, n := range nodes {
			if n.Group != body.ID {
				kept = append(kept, n)
			}
		}
		nodes = kept
		npSaveStoreLocked()
		npMu.Unlock()
		if err := npRegen(true); err != nil {
			apiErrBadRequest(w, errCodeBadRequest, err.Error())
			return
		}
		logf("node", "info", "已删除订阅：%s（其节点一并移除）", name)
		busPublish("nodes", "") // v0.59 block 3: live sync
		writeJSON(w, npStatus())
	case "rename":
		name := strings.TrimSpace(body.Name)
		if name == "" {
			apiErrBadRequest(w, errCodeInvalid, "名字不能为空")
			return
		}
		npMu.Lock()
		npLoadStoreLocked()
		sub := npFindSub(body.ID)
		if sub == nil {
			npMu.Unlock()
			writeAPIError(w, http.StatusNotFound, errCodeNotFound, "订阅不存在")
			return
		}
		sub.Name = name
		npSaveStoreLocked()
		npMu.Unlock()
		logf("node", "info", "订阅已改名为：%s", name)
		busPublish("nodes", "") // v0.59 block 3: live sync
		writeJSON(w, npStatus())
	case "enable":
		if body.Enabled == nil {
			apiErrBadRequest(w, errCodeInvalid, "缺少 enabled")
			return
		}
		npMu.Lock()
		npLoadStoreLocked()
		sub := npFindSub(body.ID)
		if sub == nil {
			npMu.Unlock()
			writeAPIError(w, http.StatusNotFound, errCodeNotFound, "订阅不存在")
			return
		}
		sub.Enabled = *body.Enabled
		name, en := sub.Name, sub.Enabled
		npSaveStoreLocked()
		npMu.Unlock()
		if err := npRegen(true); err != nil {
			apiErrBadRequest(w, errCodeBadRequest, err.Error())
			return
		}
		logf("node", "info", "订阅 %s 已%s", name, map[bool]string{true: "启用", false: "停用"}[en])
		busPublish("nodes", "") // v0.59 block 3: live sync
		writeJSON(w, npStatus())
	case "automins":
		if body.AutoMins == nil || *body.AutoMins < 0 {
			apiErrBadRequest(w, errCodeInvalid, "间隔不对")
			return
		}
		npMu.Lock()
		npLoadStoreLocked()
		sub := npFindSub(body.ID)
		if sub == nil {
			npMu.Unlock()
			writeAPIError(w, http.StatusNotFound, errCodeNotFound, "订阅不存在")
			return
		}
		sub.AutoMins = *body.AutoMins
		npSaveStoreLocked()
		npMu.Unlock()
		busPublish("nodes", "") // v0.59 block 3: live sync
		writeJSON(w, npStatus())
	case "update":
		cnt, err := npUpdateSubscription(body.ID, "手动")
		if err != nil {
			apiErrBadRequest(w, errCodeBadRequest, err.Error())
			return
		}
		st := npStatus()
		st["updated"] = cnt
		writeJSON(w, st)
	case "updateAll":
		npMu.Lock()
		npLoadStoreLocked()
		var ids []string
		for _, s := range npStore.Subs {
			if s.Enabled {
				ids = append(ids, s.ID)
			}
		}
		npMu.Unlock()
		var firstErr error
		total := 0
		for _, id := range ids {
			cnt, err := npUpdateSubscription(id, "手动")
			total += cnt
			if err != nil && firstErr == nil {
				firstErr = err
			}
		}
		st := npStatus()
		st["updated"] = total
		if firstErr != nil {
			st["error"] = firstErr.Error()
		}
		writeJSON(w, st)
	default:
		apiErrBadRequest(w, errCodeInvalid, "未知操作")
	}
}

func filterSubs(subs []subEntry, drop string) []subEntry {
	var out []subEntry
	for _, s := range subs {
		if s.ID != drop {
			out = append(out, s)
		}
	}
	return out
}

func npRulesHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Rules []userRule `json:"rules"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apiErrBadRequest(w, errCodeBadJSON, "请求内容不对")
		return
	}
	for i, rl := range body.Rules {
		if err := validateUserRule(rl); err != nil {
			apiErrBadRequest(w, errCodeInvalid, fmt.Sprintf("第 %d 条规则不对：%v", i+1, err))
			return
		}
		if body.Rules[i].ID == "" {
			body.Rules[i].ID = newRandID("rule-")
		}
		body.Rules[i].Value = normalizeRuleValue(rl)
	}
	npMu.Lock()
	npLoadStoreLocked()
	npStore.Rules = body.Rules
	npSaveStoreLocked()
	mode := npStore.Mode
	npMu.Unlock()
	if mode == "rule" {
		if err := npRegen(true); err != nil {
			apiErrBadRequest(w, errCodeBadRequest, err.Error())
			return
		}
		logf("core", "info", "自定义规则已保存（%d 条，规则模式已重新生成配置）", len(body.Rules))
	} else {
		logf("core", "info", "自定义规则已保存（%d 条，切到规则模式时生效）", len(body.Rules))
	}
	busPublish("nodes", "") // v0.59 block 3: live sync
	writeJSON(w, npStatus())
}

func validateUserRule(r userRule) error {
	v := strings.TrimSpace(r.Value)
	if v == "" {
		return fmt.Errorf("内容不能为空")
	}
	switch r.Type {
	case "domain", "suffix":
		if strings.ContainsAny(v, " /\t:*") {
			return fmt.Errorf("域名 %q 含非法字符", v)
		}
		if r.Type == "domain" && !strings.Contains(v, ".") {
			// A bare label is legal for sing-box but almost
			// always a typo in this UI; suffix rules are the
			// place for partial names.
			return fmt.Errorf("域名 %q 不完整，应形如 example.com", v)
		}
	case "ipcidr":
		if _, _, err := net.ParseCIDR(v); err != nil {
			if net.ParseIP(v) == nil {
				return fmt.Errorf("IP 段 %q 不是合法的 CIDR（如 203.0.113.0/24）或 IP", v)
			}
		}
	default:
		return fmt.Errorf("未知规则类型 %q", r.Type)
	}
	switch r.Action {
	case "proxy", "direct", "block":
	default:
		return fmt.Errorf("未知动作 %q", r.Action)
	}
	return nil
}

func normalizeRuleValue(r userRule) string {
	v := strings.TrimSpace(r.Value)
	if r.Type == "ipcidr" && !strings.Contains(v, "/") {
		if ip := net.ParseIP(v); ip != nil {
			if ip.To4() != nil {
				return v + "/32"
			}
			return v + "/128"
		}
	}
	return strings.ToLower(v)
}

func npDNSHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode   string `json:"mode"`
		Custom string `json:"custom"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apiErrBadRequest(w, errCodeBadJSON, "请求内容不对")
		return
	}
	switch body.Mode {
	case "anti", "direct":
	case "custom":
		if parseDoHServer(body.Custom, "rule") == nil {
			apiErrBadRequest(w, errCodeInvalid, "自定义 DoH 地址不对：需要 https:// 开头的 DoH 地址，如 https://dns.google/dns-query")
			return
		}
	default:
		apiErrBadRequest(w, errCodeInvalid, "未知 DNS 方案")
		return
	}
	npMu.Lock()
	npLoadStoreLocked()
	npStore.DNSMode = body.Mode
	if body.Mode == "custom" {
		npStore.DNSCustom = strings.TrimSpace(body.Custom)
	}
	npSaveStoreLocked()
	npMu.Unlock()
	if err := npRegen(true); err != nil {
		apiErrBadRequest(w, errCodeBadRequest, err.Error())
		return
	}
	logf("core", "info", "DNS 方案已切换为 %s", map[string]string{"anti": "防污染", "direct": "直连（系统 DNS）", "custom": "自定义 DoH"}[body.Mode])
	busPublish("nodes", "") // v0.59 block 3: live sync
	writeJSON(w, npStatus())
}

func npWarpHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apiErrBadRequest(w, errCodeBadJSON, "请求内容不对")
		return
	}
	switch body.Action {
	case "register":
		if err := npWarpRegister(); err != nil {
			apiErrBadRequest(w, errCodeBadRequest, err.Error())
			return
		}
		busPublish("nodes", "") // v0.59 block 3: live sync
		writeJSON(w, npStatus())
	case "remove":
		npMu.Lock()
		npLoadStoreLocked()
		npStore.Warp = nil
		if npStore.Selected == "warp" {
			npStore.Selected = ""
		}
		for i := range nodes {
			if nodes[i].Detour == "warp" {
				nodes[i].Detour = ""
			}
		}
		npSaveStoreLocked()
		npMu.Unlock()
		if err := npRegen(true); err != nil {
			apiErrBadRequest(w, errCodeBadRequest, err.Error())
			return
		}
		logf("node", "info", "WARP 中继已移除（注册信息已删除）")
		busPublish("nodes", "") // v0.59 block 3: live sync
		writeJSON(w, npStatus())
	default:
		apiErrBadRequest(w, errCodeInvalid, "未知操作")
	}
}

func npConnectionsHandler(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"available": false, "connections": []map[string]any{}, "traffic": trafficSnapshot()}
	if !settingNodeClashAPI() {
		out["reason"] = "Clash API 已在设置中关闭"
		writeJSON(w, out)
		return
	}
	if !coreRunningQuick() {
		out["reason"] = "核心未运行"
		writeJSON(w, out)
		return
	}
	code, b, err := clashDo(http.MethodGet, "/connections", nil, 3*time.Second)
	if err != nil || code != 200 {
		out["reason"] = "Clash API 暂不可用"
		writeJSON(w, out)
		return
	}
	var v struct {
		DownloadTotal int64 `json:"downloadTotal"`
		UploadTotal   int64 `json:"uploadTotal"`
		Connections   []struct {
			ID       string `json:"id"`
			Metadata struct {
				Network         string `json:"network"`
				Type            string `json:"type"`
				Host            string `json:"host"`
				DestinationIP   string `json:"destinationIP"`
				DestinationPort string `json:"destinationPort"`
			} `json:"metadata"`
			Upload   int64    `json:"upload"`
			Download int64    `json:"download"`
			Start    string   `json:"start"`
			Chains   []string `json:"chains"`
			Rule     string   `json:"rule"`
		} `json:"connections"`
	}
	if json.Unmarshal(b, &v) != nil {
		out["reason"] = "Clash API 返回解析失败"
		writeJSON(w, out)
		return
	}
	list := []map[string]any{}
	for _, c := range v.Connections {
		host := c.Metadata.Host
		if host == "" {
			host = c.Metadata.DestinationIP
		}
		start, _ := time.Parse(time.RFC3339, c.Start)
		list = append(list, map[string]any{
			"id": c.ID, "host": host, "port": c.Metadata.DestinationPort,
			"network": c.Metadata.Network, "type": c.Metadata.Type,
			"upload": c.Upload, "download": c.Download,
			"start": start.Unix(), "chains": c.Chains, "rule": c.Rule,
		})
	}
	out["available"] = true
	out["connections"] = list
	out["apiUpTotal"] = v.UploadTotal
	out["apiDownTotal"] = v.DownloadTotal
	writeJSON(w, out)
}

func npConnCloseHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID  string `json:"id"`
		All bool   `json:"all"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apiErrBadRequest(w, errCodeBadJSON, "请求内容不对")
		return
	}
	if body.All {
		code, _, err := clashDo(http.MethodDelete, "/connections", nil, 3*time.Second)
		if err != nil || (code != 200 && code != 204) {
			writeAPIError(w, http.StatusInternalServerError, errCodeInternal, "关闭全部连接失败（Clash API 不可用）")
			return
		}
		logf("core", "info", "已关闭全部实时连接")
		writeJSON(w, map[string]any{"ok": true})
		return
	}
	if body.ID == "" {
		apiErrBadRequest(w, errCodeInvalid, "缺少连接 id")
		return
	}
	code, _, err := clashDo(http.MethodDelete, "/connections/"+url.PathEscape(body.ID), nil, 3*time.Second)
	if err != nil || (code != 200 && code != 204) {
		writeAPIError(w, http.StatusInternalServerError, errCodeInternal, "关闭连接失败（Clash API 不可用）")
		return
	}
	logf("core", "info", "已关闭一条实时连接")
	writeJSON(w, map[string]any{"ok": true})
}

// nodeproInit starts the subsystem's background work. Called
// from startServicesOnce, i.e. only in an unlocked session.
func nodeproInit() {
	npMu.Lock()
	npLoadStoreLocked()
	npMu.Unlock()
	refreshIPv6Async()
	go clashTrafficSupervisor()
	go fallbackWatchdog()
	go npSubAutoLoop()
}
