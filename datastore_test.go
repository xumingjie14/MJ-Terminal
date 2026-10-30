package main

// v0.57 datastore tests: the data layer's promises, pinned --
// atomic saves never tear, corruption recovers from the
// keep-last-good backup, a missing file is a first run (not an
// error), an unrecoverable file surfaces an error instead of
// silently zeroing state, backups never rotate garbage over
// gold, and the v1 migration no-op leaves the REAL current
// settings/nodes/layout formats byte-identical.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func dsTestRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	oldRoot, oldCache, oldTried := dsRootOverride, dsManifestCache, dsManifestTried
	dsRootOverride = root
	dsManifestMu.Lock()
	dsManifestCache = nil
	dsManifestTried = false
	dsManifestMu.Unlock()
	t.Cleanup(func() {
		dsRootOverride = oldRoot
		dsManifestMu.Lock()
		dsManifestCache = oldCache
		dsManifestTried = oldTried
		dsManifestMu.Unlock()
	})
	return root
}

func dsTestJSONValidator(b []byte) error {
	var v map[string]any
	return json.Unmarshal(b, &v)
}

func TestDSMissingIsFirstRun(t *testing.T) {
	dsTestRoot(t)
	_, status, err := dsLoad("settings")
	if err != nil {
		t.Fatalf("missing file must not error: %v", err)
	}
	if status != dsMissing {
		t.Fatalf("status = %v, want dsMissing", status)
	}
}

func TestDSSaveLoadRoundTripAndBackupRotation(t *testing.T) {
	root := dsTestRoot(t)
	v1 := []byte(`{"a":1}`)
	v2 := []byte(`{"a":2}`)
	if err := dsSave("layout", v1); err != nil {
		t.Fatal(err)
	}
	got, status, err := dsLoad("layout")
	if err != nil || status != dsOK || string(got) != string(v1) {
		t.Fatalf("round trip 1: got %q status %v err %v", got, status, err)
	}
	if err := dsSave("layout", v2); err != nil {
		t.Fatal(err)
	}
	// The previous good contents rotated into the sibling backup.
	bak, err := os.ReadFile(filepath.Join(root, "layout.json.bak"))
	if err != nil || string(bak) != string(v1) {
		t.Fatalf("backup = %q err %v, want the v1 bytes", bak, err)
	}
	got, _, err = dsLoad("layout")
	if err != nil || string(got) != string(v2) {
		t.Fatalf("round trip 2: got %q err %v", got, err)
	}
}

func TestDSCorruptionRecoversFromLastGood(t *testing.T) {
	root := dsTestRoot(t)
	good := []byte(`{"a":1}`)
	newer := []byte(`{"a":2}`)
	if err := dsSave("layout", good); err != nil {
		t.Fatal(err)
	}
	if err := dsSave("layout", newer); err != nil { // rotates `good` into .bak
		t.Fatal(err)
	}
	// Corrupt the live file the way a torn hand-edit / disk glitch would.
	if err := os.WriteFile(filepath.Join(root, "layout.json"), []byte(`{"a":`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, status, err := dsLoad("layout")
	if err != nil {
		t.Fatalf("recovery must not error: %v", err)
	}
	if status != dsRecovered {
		t.Fatalf("status = %v, want dsRecovered", status)
	}
	if string(got) != string(good) {
		t.Fatalf("recovered %q, want the last-good %q", got, good)
	}
	// And the file on disk is whole again.
	raw, err := os.ReadFile(filepath.Join(root, "layout.json"))
	if err != nil || string(raw) != string(good) {
		t.Fatalf("restored file = %q err %v", raw, err)
	}
}

func TestDSCorruptionWithoutBackupSurfaces(t *testing.T) {
	root := dsTestRoot(t)
	if err := os.WriteFile(filepath.Join(root, "settings.json"), []byte(`{oops`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, status, err := dsLoad("settings")
	if err == nil {
		t.Fatal("unrecoverable corruption must surface an error, not silent defaults")
	}
	if status != dsCorrupt {
		t.Fatalf("status = %v, want dsCorrupt", status)
	}
	// The damaged bytes stay on disk for forensics.
	raw, rerr := os.ReadFile(filepath.Join(root, "settings.json"))
	if rerr != nil || string(raw) != `{oops` {
		t.Fatalf("damaged file was modified: %q %v", raw, rerr)
	}
}

func TestDSFailedSaveLeavesOldFileIntact(t *testing.T) {
	root := dsTestRoot(t)
	good := []byte(`{"a":1}`)
	if err := dsSave("layout", good); err != nil {
		t.Fatal(err)
	}
	// Simulate a mid-write failure: the temp path is occupied by a
	// DIRECTORY, so the atomic write cannot even start.
	if err := os.Mkdir(filepath.Join(root, "layout.json.dstmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := dsSave("layout", []byte(`{"a":2}`)); err == nil {
		t.Fatal("save with a blocked temp path must fail")
	}
	raw, err := os.ReadFile(filepath.Join(root, "layout.json"))
	if err != nil || string(raw) != string(good) {
		t.Fatalf("old file torn by failed save: %q %v", raw, err)
	}
	// A stale partial temp file (killed mid-write) must not affect
	// the load either: loads read the real file, never the temp.
	if err := os.WriteFile(filepath.Join(root, "layout.json.dstmp2"), []byte(`{"a":`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, status, err := dsLoad("layout")
	if err != nil || status != dsOK || string(got) != string(good) {
		t.Fatalf("load after failed save: %q %v %v", got, status, err)
	}
}

func TestDSBackupNeverRotatesGarbageOverGold(t *testing.T) {
	root := dsTestRoot(t)
	gold := []byte(`{"a":1}`)
	if err := dsSave("layout", gold); err != nil {
		t.Fatal(err)
	}
	if err := dsSave("layout", []byte(`{"a":2}`)); err != nil { // .bak = gold
		t.Fatal(err)
	}
	// The live file gets corrupted, then a save happens before any
	// load notices: the corrupt bytes must NOT become the backup.
	if err := os.WriteFile(filepath.Join(root, "layout.json"), []byte(`garbage`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := dsSave("layout", []byte(`{"a":3}`)); err != nil {
		t.Fatal(err)
	}
	bak, err := os.ReadFile(filepath.Join(root, "layout.json.bak"))
	if err != nil || string(bak) != string(gold) {
		t.Fatalf("backup = %q err %v, want the gold bytes preserved", bak, err)
	}
}

func TestDSNoBackupForUpdateToken(t *testing.T) {
	root := dsTestRoot(t)
	if err := dsSave("update-token", []byte("tok1")); err != nil {
		t.Fatal(err)
	}
	if err := dsSave("update-token", []byte("tok2")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "update-token.txt.bak")); !os.IsNotExist(err) {
		t.Fatal("the update token (a credential) must never get a .bak sibling")
	}
}

func TestDSManifestRecordsAllFilesAtV1(t *testing.T) {
	root := dsTestRoot(t)
	if err := dsSave("accel", []byte("1")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "schema.json"))
	if err != nil {
		t.Fatalf("schema manifest was not created: %v", err)
	}
	var m dsManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("manifest does not parse: %v", err)
	}
	for name, spec := range dsRegistry {
		if v, ok := m.Files[spec.rel]; !ok || v != 1 {
			t.Errorf("manifest[%q] (file %s) = %d, %v; want 1, present", spec.rel, name, v, ok)
		}
	}
}

// The REAL current formats, as a v0.56 install would have them on
// disk: loading through the layer must return them byte-identical
// (the v1 migration step is a no-op) and they must still parse
// into the live structs with their values intact.
const dsSettingsFixture = `{"wallpaper":"sonoma","stageManager":true,"idleExitMinutes":30,"phoneMode":true,"phoneTriggerWidth":1200,"phoneWidth":400,"chromeCacheMB":200,"stageAutoClaimBrowser":true,"filesOnDesktop":true,"remoteEnabled":false}`
const dsNodesFixture = `{"mode":"rule","selected":"abc123","dnsMode":"anti","clashSecret":"0123456789abcdef","subs":[{"id":"sub1","name":"机场A","url":"https://example.invalid/sub","updatedAt":1761000000}],"nodes":[{"id":"abc123","group":"manual","raw":"vless://00000000-0000-0000-0000-000000000000@example.invalid:443?security=tls#n1","latencyMs":42,"latencyKind":"http"}],"rules":[]}`
const dsLayoutFixture = `{"pos":{"chrome":{"x":12,"y":24},"files":{"x":124,"y":24}},"folders":{"f1":{"name":"工具","members":["chrome","files"]}},"order":["chrome","files"]}`

func TestDSV1FixturesRoundTripUnchanged(t *testing.T) {
	root := dsTestRoot(t)
	fixtures := map[string]string{
		"settings.json": dsSettingsFixture,
		"nodes.json":    dsNodesFixture,
		"layout.json":   dsLayoutFixture,
	}
	for rel, body := range fixtures {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	names := map[string]string{"settings.json": "settings", "nodes.json": "nodes", "layout.json": "layout"}
	for rel, body := range fixtures {
		got, status, err := dsLoad(names[rel])
		if err != nil || status != dsOK {
			t.Fatalf("%s: status %v err %v", rel, status, err)
		}
		if string(got) != body {
			t.Fatalf("%s: load altered the bytes:\n got %s\nwant %s", rel, got, body)
		}
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil || string(raw) != body {
			t.Fatalf("%s: the load rewrote the file (migration no-op violated): %q %v", rel, raw, err)
		}
		// Save-load round trip through the layer is byte-stable too.
		if err := dsSave(names[rel], got); err != nil {
			t.Fatal(err)
		}
		again, _, err := dsLoad(names[rel])
		if err != nil || string(again) != body {
			t.Fatalf("%s: round trip changed bytes: %q %v", rel, again, err)
		}
	}
	// Semantics: the fixtures still mean what v0.56 meant.
	var s mjSettings
	if err := json.Unmarshal([]byte(dsSettingsFixture), &s); err != nil {
		t.Fatal(err)
	}
	if s.Wallpaper != "sonoma" || !s.StageManager || s.IdleExitMinutes != 30 || s.ChromeCacheMB != 200 {
		t.Fatalf("settings fixture semantics drifted: %+v", s)
	}
	var ns nodeStore
	if err := json.Unmarshal([]byte(dsNodesFixture), &ns); err != nil {
		t.Fatal(err)
	}
	if ns.Selected != "abc123" || len(ns.Subs) != 1 || len(ns.Nodes) != 1 || ns.Nodes[0].LatencyMs != 42 {
		t.Fatalf("nodes fixture semantics drifted: %+v", ns)
	}
	if !json.Valid([]byte(dsLayoutFixture)) || !strings.Contains(dsLayoutFixture, `"folders"`) {
		t.Fatal("layout fixture invalid")
	}
}

func TestDSPathAPIRecovery(t *testing.T) {
	root := dsTestRoot(t)
	// The dynamic-path files (per-server agent meta, per-component
	// stamps, the notification test store) ride the same
	// primitives; pin that here with a JSON shape validator.
	p := filepath.Join(root, "servermon", "agent-s1.json")
	v := dsJSONValidatorOf(struct {
		Installed bool `json:"installed"`
	}{})
	if err := dsSaveFile(p, []byte(`{"installed":true}`), 0o600, v, true); err != nil {
		t.Fatal(err)
	}
	if err := dsSaveFile(p, []byte(`{"installed":false}`), 0o600, v, true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, status, err := dsLoadFile(p, v)
	if err != nil || status != dsRecovered || string(got) != `{"installed":true}` {
		t.Fatalf("path API recovery: %q %v %v", got, status, err)
	}
	// Removal takes the backup with it.
	dsRemoveFile(p)
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("dsRemoveFile left the file behind")
	}
	if _, err := os.Stat(p + ".bak"); !os.IsNotExist(err) {
		t.Fatal("dsRemoveFile left the backup behind")
	}
}
