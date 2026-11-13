package main

// changelog_test.go -- v0.71 block 3: the embedded changelog
// must parse, cover the mandated range (current version
// down to at least v0.65), stay newest-first, and keep the
// user-facing wording clean -- the banned-jargon scan the
// release process applies to GitHub notes applies to the
// in-app page too.

import (
	"strings"
	"testing"
)

func TestChangelogData(t *testing.T) {
	resp := changelogData()
	if resp.Current != AppVersion {
		t.Fatalf("current = %q, want AppVersion %q", resp.Current, AppVersion)
	}
	if len(resp.Entries) == 0 {
		t.Fatal("no entries")
	}
	if resp.Entries[0].Version != AppVersion {
		t.Fatalf("newest entry = %q, want the running version %q first", resp.Entries[0].Version, AppVersion)
	}
	seen := map[string]bool{}
	for _, e := range resp.Entries {
		if e.Version == "" || e.Date == "" || len(e.Notes) == 0 {
			t.Fatalf("entry incomplete: %+v", e)
		}
		if seen[e.Version] {
			t.Fatalf("duplicate entry for %s", e.Version)
		}
		seen[e.Version] = true
		for _, n := range e.Notes {
			if strings.TrimSpace(n) == "" {
				t.Fatalf("empty note in %s", e.Version)
			}
		}
	}
	for _, v := range []string{"0.71", "0.70", "0.69", "0.68", "0.67", "0.66", "0.65"} {
		if !seen[v] {
			t.Fatalf("missing mandated entry v%s", v)
		}
	}
}

func TestChangelogWording(t *testing.T) {
	banned := []string{"main.go", "core.go", "拆分", "重构", "守护进程", "驻留", "回钉", "收口", "sing-box", "进程"}
	for _, e := range changelogData().Entries {
		for _, n := range e.Notes {
			for _, b := range banned {
				if strings.Contains(n, b) {
					t.Errorf("v%s note %q contains banned jargon %q", e.Version, n, b)
				}
			}
		}
	}
}
