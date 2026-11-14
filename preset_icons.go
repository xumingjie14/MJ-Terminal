package main

// preset_icons.go -- v0.71 test5: preset REAL artwork for
// well-known web services.
//
// The user's icon benchmark (2026-11-14): a browser start
// page, where every shortcut wears the service's own
// high-resolution artwork in one rounded tile. The test4
// fetch chain (manifest -> touch icon -> conventional paths,
// verified by real pixel size) gets there for sites that
// publish good icons AND are reachable at add time -- but a
// first-come favicon stored months ago, or a site that
// blocks the fetch, still leaves a letter tile on the
// desktop. So the services the user actually runs also ship
// as PRESETS: the icon each service itself publishes (its
// PWA manifest / touch icon, or the artwork it publishes
// for its app), fetched once at build time, verified by
// real pixel size (>=192px, most >=256px), embedded in
// preset_icons_data.go and recorded in icon-provenance.md.
//
// At runtime a preset outranks the live hunt whenever it is
// larger than what an entry has stored (refreshWebAppAssets
// in addapp.go), and the startup backfill upgrades old
// entries to it (webAppIconStale). A service with no
// qualifying published artwork is simply absent from the
// tables -- farside.co.uk, for instance, publishes only an
// 80px default favicon, so it stays on the live chain. No
// preset ever stands in with a letter tile's clothing: the
// bytes here are the services' own artwork, byte for byte.

import (
	"net/url"
	"strings"
)

// presetIconPNG maps a preset id to its PNG bytes. Filled by
// preset_icons_data.go's init() from embedded base64.
var presetIconPNG = map[string][]byte{}

// presetIconHosts maps an exact host to a preset id. It
// exists for services whose icon differs per host on a
// shared domain (Google's mail/translate/gemini) and for
// canonical web-app hosts.
var presetIconHosts = map[string]string{
	"mail.google.com":      "gmail",
	"gmail.com":            "gmail",
	"translate.google.com": "gtranslate",
	"gemini.google.com":    "gemini",
	"web.telegram.org":     "telegram",
	"web.whatsapp.com":     "whatsapp",
	"outlook.live.com":     "outlook",
	"outlook.office.com":   "outlook",
	"login.tailscale.com":  "tailscale",
}

// presetIconDomains maps a registrable domain to a preset
// id: any subdomain of the service wears the same face.
var presetIconDomains = map[string]string{
	"telegram.org":    "telegram",
	"whatsapp.com":    "whatsapp",
	"x.com":           "x",
	"youtube.com":     "youtube",
	"tradingview.com": "tradingview",
	"github.com":      "github",
	"outlook.com":     "outlook",
	"tiktok.com":      "tiktok",
	"claude.ai":       "Muse",
	"chatgpt.com":     "chatgpt",
	"vercel.com":      "vercel",
	"coinglass.com":   "coinglass",
	"bilibili.com":    "bilibili",
	"dida365.com":     "dida365",
	"tailscale.com":   "tailscale",
	"firefox.com":     "firefox",
	"mozilla.org":     "firefox",
}

// iconRegistrableDomain reduces a host to its registrable
// domain (public suffix + one label), keeping common
// two-part suffixes (co.uk, com.cn, ...) whole. Pure --
// tests pin it.
func iconRegistrableDomain(host string) string {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	labels := strings.Split(host, ".")
	if len(labels) <= 2 {
		return host
	}
	last, second := labels[len(labels)-1], labels[len(labels)-2]
	if len(last) == 2 {
		switch second {
		case "co", "com", "org", "net", "ac", "gov", "edu":
			return strings.Join(labels[len(labels)-3:], ".")
		}
	}
	return strings.Join(labels[len(labels)-2:], ".")
}

// presetIconID resolves a web app URL to a preset id: exact
// host first (per-host services on shared domains), then
// the registrable domain.
func presetIconID(rawURL string) (string, bool) {
	pu, err := url.Parse(rawURL)
	if err != nil || pu.Host == "" {
		return "", false
	}
	host := strings.ToLower(pu.Hostname())
	if id, ok := presetIconHosts[host]; ok {
		return id, true
	}
	id, ok := presetIconDomains[iconRegistrableDomain(host)]
	return id, ok
}

// presetIconFor returns the preset artwork for a web app
// URL: its id, its PNG bytes and its verified real pixel
// size (the largest frame dimension, measured by the same
// iconRealSize the fetch chain trusts).
func presetIconFor(rawURL string) (string, []byte, int, bool) {
	id, ok := presetIconID(rawURL)
	if !ok {
		return "", nil, 0, false
	}
	b := presetIconPNG[id]
	if len(b) == 0 {
		return "", nil, 0, false
	}
	return id, b, iconRealSize(b), true
}
