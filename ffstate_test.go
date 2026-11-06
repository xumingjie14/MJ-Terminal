package main

import (
	"encoding/json"
	"testing"
)

// The Firefox launch rule (v0.56 semantics, asked PER ENTRY
// since v0.64). A process's birth mode is destiny: a request
// matching the birth mode of the process holding THIS entry's
// profile joins it; everything else conflicts (including a
// running process whose birth was never recorded, e.g. one
// started by a pre-v0.56 build). Since v0.64 the conflicting
// process can only be a pre-upgrade one on the window entry —
// each entry's own launches always match by construction.
func TestDecideFFLaunch(t *testing.T) {
	cases := []struct {
		name                         string
		alive, known, birth, wantApp bool
		want                         ffLaunchDecision
	}{
		{"no process, normal", false, false, false, false, ffFresh},
		{"no process, app", false, false, false, true, ffFresh},
		{"alive normal + want normal", true, true, false, false, ffJoin},
		{"alive app + want app", true, true, true, true, ffJoin},
		{"alive normal + want app", true, true, false, true, ffConflict},
		{"alive app + want normal", true, true, true, false, ffConflict},
		{"alive unknown + want normal", true, false, false, false, ffConflict},
		{"alive unknown + want app", true, false, false, true, ffConflict},
	}
	for _, c := range cases {
		if got := decideFFLaunch(c.alive, c.known, c.birth, c.wantApp); got != c.want {
			t.Errorf("%s: decideFFLaunch = %v, want %v", c.name, got, c.want)
		}
	}
}

// v0.64: the one-time state migration. A pre-v0.64 ffstate blob
// is a single bare record; it must land on the WINDOW entry
// with its birth mode preserved verbatim (a legacy app-born
// process really holds that profile in app mode — the window
// entry's launch rule depends on seeing it). The migration is
// a fixed point: the migrated blob is in the new format, so
// parsing it again yields the same records and no legacy.
func TestFFStateMigration(t *testing.T) {
	legacyApp := []byte(`{"appmode":true,"pid":4242,"startedAt":1700000000}`)
	records, legacy := ffParseState(legacyApp)
	if legacy == nil {
		t.Fatal("legacy app-born blob not detected as legacy")
	}
	if records != nil {
		t.Fatalf("legacy blob produced records %v, want nil", records)
	}
	if !legacy.AppMode || legacy.PID != 4242 || legacy.StartedAt != 1700000000 {
		t.Fatalf("legacy record mangled in parse: %+v", *legacy)
	}
	migrated := ffMigrateLegacyRecords(*legacy)
	if len(migrated) != 1 {
		t.Fatalf("migrated records = %v, want exactly the window entry", migrated)
	}
	rec, ok := migrated[ffEntryWindow]
	if !ok || !rec.AppMode || rec.PID != 4242 || rec.StartedAt != 1700000000 {
		t.Fatalf("window entry record = %+v (ok=%v), want the legacy record verbatim", rec, ok)
	}
	if _, ok := migrated[ffEntryApp]; ok {
		t.Fatal("migration invented an app-entry record")
	}

	// Idempotency: what ffLoadRecords persists after migrating
	// parses back as the new format — same records, no legacy,
	// so a second pass changes nothing.
	blob, err := json.Marshal(struct {
		Records map[string]ffProcRecord `json:"records"`
	}{Records: migrated})
	if err != nil {
		t.Fatal(err)
	}
	records2, legacy2 := ffParseState(blob)
	if legacy2 != nil {
		t.Fatal("migrated blob detected as legacy again — migration is not a fixed point")
	}
	if len(records2) != 1 || records2[ffEntryWindow] != rec {
		t.Fatalf("re-parsed records = %v, want the migrated set unchanged", records2)
	}

	// A legacy NORMAL-born record migrates the same way (the
	// common upgrade case: the window entry then joins it).
	_, legacyN := ffParseState([]byte(`{"appmode":false,"pid":777,"startedAt":5}`))
	if legacyN == nil || legacyN.AppMode {
		t.Fatalf("legacy normal blob: legacy = %+v", legacyN)
	}
	if got := ffMigrateLegacyRecords(*legacyN)[ffEntryWindow]; got.PID != 777 || got.AppMode {
		t.Fatalf("normal legacy migrated to %+v", got)
	}

	// New format with both entries round-trips untouched.
	both := map[string]ffProcRecord{
		ffEntryWindow: {AppMode: false, PID: 10, StartedAt: 1},
		ffEntryApp:    {AppMode: true, PID: 20, StartedAt: 2},
	}
	blob2, _ := json.Marshal(struct {
		Records map[string]ffProcRecord `json:"records"`
	}{Records: both})
	records3, legacy3 := ffParseState(blob2)
	if legacy3 != nil || len(records3) != 2 || records3[ffEntryApp].PID != 20 || records3[ffEntryWindow].PID != 10 {
		t.Fatalf("dual records round-trip: records=%v legacy=%v", records3, legacy3)
	}

	// Garbage and absence are neither records nor legacy.
	for _, bad := range [][]byte{[]byte("not json"), []byte("{}"), []byte(`{"records":{}}`), nil} {
		recs, leg := ffParseState(bad)
		if leg != nil {
			t.Fatalf("blob %q detected as legacy", bad)
		}
		if bad != nil && string(bad) == `{"records":{}}` && len(recs) != 0 {
			t.Fatalf("empty records blob parsed to %v", recs)
		}
	}
}

// v0.64: the aggregate procMode wire field is derived from the
// per-entry truths in entry order (window, then app).
func TestFFProcModeSummary(t *testing.T) {
	cases := []struct {
		parts []string
		want  string
	}{
		{nil, ""},
		{[]string{"普通"}, "普通"},
		{[]string{"应用化"}, "应用化"},
		{[]string{"未知"}, "未知"},
		{[]string{"普通", "应用化"}, "普通+应用化"},
		{[]string{"未知", "应用化"}, "未知+应用化"},
	}
	for _, c := range cases {
		if got := ffProcModeSummary(c.parts); got != c.want {
			t.Errorf("ffProcModeSummary(%v) = %q, want %q", c.parts, got, c.want)
		}
	}
}

// v0.64: claim attribution. A Firefox process belongs to the
// entry whose profile dir its -profile argument names. The
// sibling trap is the whole point: the window profile's name
// is a PREFIX of the app profile's, so a naive substring check
// would attribute every app process to the window entry too;
// cmdlineHasProfile's boundary rule is what keeps them apart.
func TestFFEntryForProfile(t *testing.T) {
	winDir := `C:\MJ-Terminal\Data\FirefoxProfile`
	appDir := `C:\MJ-Terminal\Data\FirefoxProfileApp`
	exe := `"C:\MJ-Terminal\core\firefox\firefox.exe"`
	cases := []struct {
		name    string
		cmdline string
		want    string
	}{
		{"window process", exe + ` -profile ` + winDir + ` -no-remote -new-window about:home`, ffEntryWindow},
		{"app process", exe + ` -profile ` + appDir + ` -no-remote -new-window about:home`, ffEntryApp},
		{"app process, quoted", exe + ` -profile "` + appDir + `" -new-window x`, ffEntryApp},
		{"window process, -profile= form", exe + ` -profile=` + winDir + ` -new-window x`, ffEntryWindow},
		{"host firefox (default profile)", `"C:\Program Files\Mozilla Firefox\firefox.exe" -profile C:\Users\x\AppData\Roaming\Mozilla\Firefox\Profiles\a.default -new-window x`, ""},
		{"lookalike profile dir", exe + ` -profile ` + winDir + `2 -new-window x`, ""},
		{"no -profile at all", exe + ` -new-window x`, ""},
		{"empty cmdline", "", ""},
	}
	for _, c := range cases {
		if got := ffEntryForProfile(c.cmdline, winDir, appDir); got != c.want {
			t.Errorf("%s: ffEntryForProfile = %q, want %q", c.name, got, c.want)
		}
	}
	// The label decoration rides on the same attribution.
	if got := ffEntryLabel("某个页面", "应用化"); got != "某个页面 · 应用化" {
		t.Errorf("ffEntryLabel decorated = %q", got)
	}
	if got := ffEntryLabel("某个页面", ""); got != "某个页面" {
		t.Errorf("ffEntryLabel with no entry = %q, want undecorated", got)
	}
}
