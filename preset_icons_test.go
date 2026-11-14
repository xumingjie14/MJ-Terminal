package main

import "testing"

func TestPresetIconID(t *testing.T) {
	cases := []struct {
		url string
		id  string
		ok  bool
	}{
		{"https://www.youtube.com", "youtube", true},
		{"https://music.youtube.com", "youtube", true},
		{"https://web.telegram.org/a/", "telegram", true},
		{"https://telegram.org", "telegram", true},
		{"https://web.whatsapp.com", "whatsapp", true},
		{"https://x.com/home", "x", true},
		{"https://www.tradingview.com/chart/", "tradingview", true},
		{"https://github.com/xumingjie14", "github", true},
		{"https://mail.google.com/mail/u/0", "gmail", true},
		{"https://translate.google.com", "gtranslate", true},
		{"https://gemini.google.com/app", "gemini", true},
		{"https://outlook.live.com/mail", "outlook", true},
		{"https://outlook.office.com", "outlook", true},
		{"https://www.tiktok.com", "tiktok", true},
		{"https://claude.ai", "Muse", true},
		{"https://chatgpt.com", "chatgpt", true},
		{"https://vercel.com/dashboard", "vercel", true},
		{"https://www.coinglass.com", "coinglass", true},
		{"https://www.bilibili.com", "bilibili", true},
		{"https://live.bilibili.com/123", "bilibili", true},
		{"https://www.dida365.com", "dida365", true},
		{"https://login.tailscale.com/admin", "tailscale", true},
		{"https://tailscale.com", "tailscale", true},
		// No preset: the user's own Vercel-hosted site (a
		// vercel.app tenant is not vercel.com), farside
		// (publishes no qualifying artwork), unknown sites.
		{"https://fed-liquidity-mingjieofficial-7844s-projects.vercel.app/#/macro", "", false},
		{"https://farside.co.uk/eth", "", false},
		{"https://example.com", "", false},
		{"https://www.google.com", "", false},
		{"not a url", "", false},
	}
	for _, c := range cases {
		id, ok := presetIconID(c.url)
		if id != c.id || ok != c.ok {
			t.Errorf("presetIconID(%q) = (%q, %v), want (%q, %v)", c.url, id, ok, c.id, c.ok)
		}
	}
}

func TestIconRegistrableDomain(t *testing.T) {
	cases := map[string]string{
		"www.youtube.com":   "youtube.com",
		"a.b.claude.ai":     "claude.ai",
		"www.farside.co.uk": "farside.co.uk",
		"farside.co.uk":     "farside.co.uk",
		"mail.google.com":   "google.com",
		"localhost":         "localhost",
		"EXAMPLE.com.":      "example.com",
	}
	for host, want := range cases {
		if got := iconRegistrableDomain(host); got != want {
			t.Errorf("iconRegistrableDomain(%q) = %q, want %q", host, got, want)
		}
	}
}

// Every preset in the tables must carry real artwork at the
// bar the fetch chain uses (>=192px verified), and every
// embedded preset must be reachable from a table -- no
// orphan bytes, no table entry pointing at nothing.
func TestPresetIconsComplete(t *testing.T) {
	referenced := map[string]bool{}
	for host, id := range presetIconHosts {
		referenced[id] = true
		b, ok := presetIconPNG[id]
		if !ok || len(b) == 0 {
			t.Errorf("host %s -> preset %q has no embedded artwork", host, id)
			continue
		}
		if sz := iconRealSize(b); sz < 192 {
			t.Errorf("preset %q real size %dpx, want >= 192", id, sz)
		}
	}
	for dom, id := range presetIconDomains {
		referenced[id] = true
		b, ok := presetIconPNG[id]
		if !ok || len(b) == 0 {
			t.Errorf("domain %s -> preset %q has no embedded artwork", dom, id)
			continue
		}
		if sz := iconRealSize(b); sz < 192 {
			t.Errorf("preset %q real size %dpx, want >= 192", id, sz)
		}
	}
	for id := range presetIconPNG {
		if !referenced[id] {
			t.Errorf("preset %q is embedded but no table references it", id)
		}
	}
}
