package main

// registry_test.go -- v0.60 architecture block 4: the feature
// registry gate. These tests are the structural form of the
// project's three-piece shipping standard: a feature that
// skips its console integration, its Settings keys, or its
// route registration fails the build here (and in CI, which
// runs the same suite in the verify job).
//
// The checks compare the registry against the codebase's own
// sources of truth, in both directions:
//
//   - console: every logf("tag", ...) literal anywhere in the
//     Go tree names a registered feature's module tag;
//   - settings: the POST whitelist (the case labels of
//     boolSettingPtr + settingsHandler in settings.go, the
//     only two switches that admit a settings key) equals the
//     union of registered per-feature keys, and every
//     mjSettings field is either POST-writable or one of the
//     two per-app notification maps behind the registered
//     dynamic prefixes;
//   - routes: the local mux table (the mux.HandleFunc literals
//     in runServer) and the remote table (remoteRoutes()) each
//     equal the registered route set exactly.
//
// The sources are read from disk; `go test` runs in the
// package directory, so the relative paths below are the
// repo's own files.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestFeatureRegistryShape(t *testing.T) {
	ids := map[string]bool{}
	localPaths := map[string]string{}
	remoteKeys := map[string]string{}
	settingKeys := map[string]string{}
	for _, f := range featureRegistry {
		if f.ID == "" || ids[f.ID] {
			t.Errorf("feature id empty or duplicated: %q", f.ID)
		}
		ids[f.ID] = true
		if f.Name == "" {
			t.Errorf("feature %s has no display name", f.ID)
		}
		if len(f.Modules) == 0 {
			t.Errorf("feature %s (%s) registers no console module tag -- the three-piece standard requires console integration", f.ID, f.Name)
		}
		for _, r := range f.Routes {
			if r.Local {
				if prev, dup := localPaths[r.Path]; dup {
					t.Errorf("local route %s registered by both %s and %s", r.Path, prev, f.ID)
				}
				localPaths[r.Path] = f.ID
			}
			for _, m := range r.Remote {
				k := m + " " + r.Path
				if prev, dup := remoteKeys[k]; dup {
					t.Errorf("remote route %s registered by both %s and %s", k, prev, f.ID)
				}
				remoteKeys[k] = f.ID
			}
		}
		for _, k := range f.Settings {
			if prev, dup := settingKeys[k]; dup {
				t.Errorf("settings key %s owned by both %s and %s", k, prev, f.ID)
			}
			settingKeys[k] = f.ID
		}
	}
}

var logfTagRe = regexp.MustCompile(`[Ll]ogf\("([a-z0-9_-]+)"`)

func TestFeatureRegistryModuleCoverage(t *testing.T) {
	registered := registeredModuleTags()
	var files []string
	root, _ := filepath.Glob("*.go")
	files = append(files, root...)
	internal, _ := filepath.Glob("internal/*/*.go")
	files = append(files, internal...)
	seen := map[string]bool{}
	for _, path := range files {
		if path == "registry_test.go" || path == "registry.go" {
			continue // the registry and its gate discuss logf("tag", ...) in comments; they contain no log calls of their own
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, m := range logfTagRe.FindAllSubmatch(b, -1) {
			tag := string(m[1])
			seen[tag] = true
			if _, ok := registered[tag]; !ok {
				t.Errorf("%s logs under module %q, which no registered feature owns -- register the feature (or its tag) in registry.go", path, tag)
			}
		}
	}
	if len(seen) < 20 {
		t.Fatalf("module scan found only %d distinct tags; the scan itself is suspect", len(seen))
	}
}

// goFuncBody returns the source of one top-level function (up
// to the next top-level func), for whitelist extraction.
func goFuncBody(t *testing.T, src, name string) string {
	t.Helper()
	i := strings.Index(src, "func "+name+"(")
	if i < 0 {
		t.Fatalf("function %s not found", name)
	}
	rest := src[i:]
	if j := strings.Index(rest[5:], "\nfunc "); j >= 0 {
		return rest[:j+5]
	}
	return rest
}

var caseKeysRe = regexp.MustCompile(`case\s+((?:"[^"]+"\s*,?\s*)+):`)
var quotedRe = regexp.MustCompile(`"([^"]+)"`)

func settingsWhitelistKeys(t *testing.T, src string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, fn := range []string{"boolSettingPtr", "settingsHandler"} {
		body := goFuncBody(t, src, fn)
		for _, cm := range caseKeysRe.FindAllStringSubmatch(body, -1) {
			for _, q := range quotedRe.FindAllStringSubmatch(cm[1], -1) {
				out[q[1]] = true
			}
		}
	}
	return out
}

func TestFeatureRegistrySettingsCoverage(t *testing.T) {
	b, err := os.ReadFile("settings.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	whitelist := settingsWhitelistKeys(t, src)
	if len(whitelist) < 40 {
		t.Fatalf("whitelist extraction found only %d keys; the extraction itself is suspect", len(whitelist))
	}
	registered := map[string]string{}
	for _, f := range featureRegistry {
		for _, k := range f.Settings {
			registered[k] = f.ID
		}
	}
	for k := range whitelist {
		if _, ok := registered[k]; !ok {
			t.Errorf("settings key %q is in the POST whitelist but no registered feature owns it (registry.go)", k)
		}
	}
	for k, owner := range registered {
		if !whitelist[k] {
			t.Errorf("feature %s registers settings key %q, which is not in the POST whitelist -- phantom key", owner, k)
		}
	}
	// Every mjSettings field is either POST-writable or one of
	// the two per-app notification maps (their keys arrive via
	// the registered dynamic prefixes, not literal cases).
	i := strings.Index(src, "type mjSettings struct {")
	if i < 0 {
		t.Fatal("mjSettings struct not found")
	}
	structBody := src[i:]
	structBody = structBody[:strings.Index(structBody, "\n}")]
	jsonTagRe := regexp.MustCompile("`json:\"([a-zA-Z0-9_]+)")
	for _, m := range jsonTagRe.FindAllStringSubmatch(structBody, -1) {
		tag := m[1]
		if whitelist[tag] {
			continue
		}
		if tag == "notify_allow_app" || tag == "notify_banner_app" {
			continue // per-app maps behind the dynamic prefixes
		}
		t.Errorf("mjSettings field %q is neither POST-writable nor a registered dynamic map", tag)
	}
	// Each registered dynamic prefix must literally exist in
	// settingsHandler's dynamic-key branch.
	handler := goFuncBody(t, src, "settingsHandler")
	for _, f := range featureRegistry {
		for _, p := range f.SettingPrefixes {
			if !strings.Contains(handler, `strings.HasPrefix(key, "`+p+`")`) {
				t.Errorf("feature %s registers dynamic settings prefix %q, but settingsHandler has no HasPrefix branch for it", f.ID, p)
			}
		}
	}
}

var handleFuncRe = regexp.MustCompile(`mux\.HandleFunc\("([^"]+)"`)

func TestFeatureRegistryRouteCoverage(t *testing.T) {
	b, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	actualLocal := map[string]bool{}
	for _, m := range handleFuncRe.FindAllSubmatch(b, -1) {
		actualLocal[string(m[1])] = true
	}
	if len(actualLocal) < 100 {
		t.Fatalf("local route scan found only %d routes; the scan itself is suspect", len(actualLocal))
	}
	registeredLocal := map[string]string{}
	registeredRemote := map[string]string{}
	for _, f := range featureRegistry {
		for _, r := range f.Routes {
			if r.Local {
				registeredLocal[r.Path] = f.ID
			}
			for _, m := range r.Remote {
				registeredRemote[m+" "+r.Path] = f.ID
			}
		}
	}
	for p := range actualLocal {
		if _, ok := registeredLocal[p]; !ok {
			t.Errorf("local route %s is registered on the mux but has no feature in registry.go", p)
		}
	}
	for p, owner := range registeredLocal {
		if !actualLocal[p] {
			t.Errorf("feature %s registers local route %s, which the mux does not serve -- phantom route", owner, p)
		}
	}
	actualRemote := map[string]bool{}
	for _, r := range remoteRoutes() {
		actualRemote[r.Method+" "+r.Path] = true
	}
	for k := range actualRemote {
		if _, ok := registeredRemote[k]; !ok {
			t.Errorf("remote route %s is in remoteRoutes() but no feature registers it in registry.go", k)
		}
	}
	for k, owner := range registeredRemote {
		if !actualRemote[k] {
			t.Errorf("feature %s registers remote route %s, which remoteRoutes() does not serve -- phantom route", owner, k)
		}
	}
	// Keep failure output readable: sort the registered ids once.
	ids := make([]string, 0, len(featureRegistry))
	for _, f := range featureRegistry {
		ids = append(ids, f.ID)
	}
	sort.Strings(ids)
	t.Logf("registry: %d features, %d local routes, %d remote routes", len(ids), len(registeredLocal), len(registeredRemote))
}
