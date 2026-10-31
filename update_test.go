package main

// v0.48 updater-hardening tests: the rename retry helper (with an
// injected fail-then-succeed rename) and the .part reuse
// predicate (size + SHA-256) -- the two pure pieces behind the
// field failure "58 MB downloads fine, rename dies on a lock".

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func stubRenameSleep(t *testing.T) {
	t.Helper()
	old := renameRetrySleep
	renameRetrySleep = func(time.Duration) {}
	t.Cleanup(func() { renameRetrySleep = old })
}

func TestRenameWithRetrySucceedsAfterFailures(t *testing.T) {
	stubRenameSleep(t)
	attempts := 0
	logged := 0
	err := renameWithRetry(func() error {
		attempts++
		if attempts < 3 {
			return errors.New("文件正被另一进程使用")
		}
		return nil
	}, func(attempt int, err error) { logged++ })
	if err != nil {
		t.Fatalf("renameWithRetry = %v, want nil", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	if logged != 2 {
		t.Fatalf("logAttempt calls = %d, want 2 (one per failure)", logged)
	}
}

func TestRenameWithRetryExhausts(t *testing.T) {
	stubRenameSleep(t)
	attempts := 0
	err := renameWithRetry(func() error {
		attempts++
		return errors.New("始终被占用")
	}, nil)
	if err == nil {
		t.Fatal("renameWithRetry = nil, want the last error")
	}
	if attempts != 12 {
		t.Fatalf("attempts = %d, want 12 (the spec floor is >=10 over ~30-60s)", attempts)
	}
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func writeTemp(t *testing.T, dir, name string, b []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPartFileStatus(t *testing.T) {
	dir := t.TempDir()
	content := []byte("the quick brown fox jumps over the lazy dog")
	sum := sha256Hex(content)

	// Complete: size and hash both match.
	p := writeTemp(t, dir, "pkg.zip.part", content)
	if complete, from := partFileStatus(p, int64(len(content)), sum); !complete || from != int64(len(content)) {
		t.Errorf("complete case = (%v, %d), want (true, %d)", complete, from, len(content))
	}
	// Complete by size alone when the manifest has no hash.
	if complete, _ := partFileStatus(p, int64(len(content)), ""); !complete {
		t.Error("size-only case: complete = false, want true")
	}
	// Partial: usable as a Range prefix.
	part := writeTemp(t, dir, "partial.part", content[:10])
	if complete, from := partFileStatus(part, int64(len(content)), sum); complete || from != 10 {
		t.Errorf("partial case = (%v, %d), want (false, 10)", complete, from)
	}
	// Oversized: garbage, restart.
	over := writeTemp(t, dir, "over.part", append(append([]byte{}, content...), 'x'))
	if complete, from := partFileStatus(over, int64(len(content)), sum); complete || from != 0 {
		t.Errorf("oversized case = (%v, %d), want (false, 0)", complete, from)
	}
	// Full size, wrong bytes: restart, never resume.
	wrong := writeTemp(t, dir, "wrong.part", []byte("the quick brown fox jumps over the lazy cog"))
	if complete, from := partFileStatus(wrong, int64(len(content)), sum); complete || from != 0 {
		t.Errorf("wrong-content case = (%v, %d), want (false, 0)", complete, from)
	}
	// Missing file.
	if complete, from := partFileStatus(filepath.Join(dir, "nope.part"), 100, sum); complete || from != 0 {
		t.Errorf("missing case = (%v, %d), want (false, 0)", complete, from)
	}
}

func TestFinalizePartFileHappyPath(t *testing.T) {
	stubRenameSleep(t)
	dir := t.TempDir()
	content := []byte("package-bytes")
	part := writeTemp(t, dir, "pkg.zip.part", content)
	dest := filepath.Join(dir, "pkg.zip")
	if err := finalizePartFile(part, dest); err != nil {
		t.Fatalf("finalizePartFile = %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != string(content) {
		t.Fatalf("dest = %q, %v", got, err)
	}
	if _, err := os.Stat(part); !os.IsNotExist(err) {
		t.Errorf(".part should be gone after finalize, stat err = %v", err)
	}
}

// ---------- v0.59 beta channel ----------

// betaFixture stands up a fake GitHub: the releases list, the
// latest-release endpoint, and per-tag manifests served from
// the release-download path -- the exact three reads the two
// check channels perform.
type betaFixture struct {
	srv  *httptest.Server
	list []ghRelease
	lat  *ghRelease
	mans map[string]updateManifest // by tag
}

func newBetaFixture(t *testing.T) *betaFixture {
	t.Helper()
	f := &betaFixture{mans: map[string]updateManifest{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/"+updateRepo+"/releases", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(f.list)
	})
	mux.HandleFunc("/repos/"+updateRepo+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		if f.lat == nil {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(f.lat)
	})
	mux.HandleFunc("/"+updateRepo+"/releases/download/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"+updateRepo+"/releases/download/"), "/")
		if len(parts) == 2 && parts[1] == updateManifestName {
			if man, ok := f.mans[parts[0]]; ok {
				_ = json.NewEncoder(w).Encode(man)
				return
			}
		}
		http.NotFound(w, r)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	oldAPI, oldDL := ghAPIBase, ghDLBase
	ghAPIBase, ghDLBase = f.srv.URL, f.srv.URL
	t.Cleanup(func() { ghAPIBase, ghDLBase = oldAPI, oldDL })
	return f
}

func fixtureRelease(tag string, prerelease bool) ghRelease {
	var rel ghRelease
	rel.TagName = tag
	rel.Name = "release " + tag
	rel.Prerelease = prerelease
	rel.Assets = []struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}{{Name: updateZipName, URL: "https://example.invalid/" + tag + ".zip"}}
	return rel
}

func resetUpd(t *testing.T) {
	t.Helper()
	reset := func() {
		upd.mu.Lock()
		defer upd.mu.Unlock()
		upd.Phase, upd.Latest, upd.Message = "idle", "", ""
		upd.Size, upd.Progress = 0, 0
		upd.zipAssetURL, upd.sha256 = "", ""
		upd.sourceTag, upd.prerelease, upd.relName, upd.relBody = "", false, "", ""
	}
	reset()
	t.Cleanup(reset)
}

func TestBetaOfferRule(t *testing.T) {
	cases := []struct {
		name                string
		tag, man, inst, app string
		official            bool // an official release exists for man
		want                bool
	}{
		{"newer test line, no baseline", "v0.60-test1", "0.60", "", "0.59", false, true},
		{"newer test line, official baseline", "v0.59-test1", "0.59", "v0.58", "0.58", false, true},
		{"same-version next iteration, official exists", "v0.59-test2", "0.59", "v0.59-test1", "0.59", true, true},
		{"same-version next iteration, no official yet", "v0.59-test2", "0.59", "v0.59-test1", "0.59", false, true},
		{"same tag as installed", "v0.59-test1", "0.59", "v0.59-test1", "0.59", false, false},
		{"older line never offers", "v0.58-test3", "0.58", "v0.58", "0.59", false, false},
		{"same version, no baseline (manual swap-in), official exists", "v0.59-test1", "0.59", "", "0.59", true, false},
		{"same version, no baseline, no official yet", "v0.59-test1", "0.59", "", "0.59", false, true},
		{"empty tag", "", "0.59", "", "0.58", false, false},
		// The v0.60 tightening: an OFFICIAL install is not
		// offered the same version's test build (identical
		// code) while the official release exists...
		{"official baseline, same-version test, official exists", "v0.59-test1", "0.59", "v0.59", "0.59", true, false},
		{"official baseline, same-version test2, official exists", "v0.59-test2", "0.59", "v0.59", "0.59", true, false},
		// ...but in the pre-official window (no official
		// release for that version) the same-version test
		// build is the stream entry and does offer.
		{"official baseline, same-version test, no official yet", "v0.59-test2", "0.59", "v0.59", "0.59", false, true},
		{"official baseline, higher-version test", "v0.60-test1", "0.60", "v0.59", "0.59", true, true},
	}
	for _, c := range cases {
		if got := betaOffer(c.tag, c.man, c.inst, c.app, c.official); got != c.want {
			t.Errorf("%s: betaOffer(%q,%q,%q,%q,official=%v) = %v, want %v", c.name, c.tag, c.man, c.inst, c.app, c.official, got, c.want)
		}
	}
}

func TestPickNewestPrerelease(t *testing.T) {
	rels := []ghRelease{
		fixtureRelease("v0.59", false),
		fixtureRelease("v0.60-test1", true),
		fixtureRelease("v0.59-test2", true),
	}
	if got := pickNewestPrerelease(rels); got == nil || got.TagName != "v0.60-test1" {
		t.Fatalf("pick = %v, want v0.60-test1 (first prerelease)", got)
	}
	draft := fixtureRelease("v0.61-test1", true)
	draft.Draft = true
	if got := pickNewestPrerelease([]ghRelease{draft, fixtureRelease("v0.60-test1", true)}); got == nil || got.TagName != "v0.60-test1" {
		t.Fatalf("drafts must be skipped, pick = %v", got)
	}
	if got := pickNewestPrerelease([]ghRelease{fixtureRelease("v0.59", false)}); got != nil {
		t.Fatalf("no prerelease must yield nil, got %v", got.TagName)
	}
}

// nextVersion returns the version one minor above the running
// AppVersion, so the "newer test line" fixtures stay newer than
// the build under test across version bumps.
func nextVersion() string {
	parts := strings.Split(AppVersion, ".")
	if len(parts) < 2 {
		return AppVersion + ".1"
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return AppVersion + ".1"
	}
	return parts[0] + "." + strconv.Itoa(minor+1)
}

func TestBetaCheckNewerTestOffers(t *testing.T) {
	dsTestRoot(t)
	resetUpd(t)
	nv := nextVersion()
	f := newBetaFixture(t)
	f.list = []ghRelease{fixtureRelease("v"+AppVersion, false), fixtureRelease("v"+nv+"-test1", true)}
	f.mans["v"+nv+"-test1"] = updateManifest{Version: nv, File: updateZipName, Size: 62_000_000, SHA256: "ab"}
	checkUpdateBeta()
	snap := upd.snapshotLocal()
	if snap.Phase != "available" || !snap.Prerelease || snap.SourceTag != "v"+nv+"-test1" || snap.Latest != nv {
		t.Fatalf("newer test must be offered: %+v", snap)
	}
}

func TestBetaCheckSameVersionNextIteration(t *testing.T) {
	dsTestRoot(t)
	resetUpd(t)
	cur := AppVersion
	if err := dsSave("update-state", []byte(`{"installedTag":"v`+cur+`-test1","installedVersion":"`+cur+`"}`)); err != nil {
		t.Fatal(err)
	}
	f := newBetaFixture(t)
	f.list = []ghRelease{fixtureRelease("v"+cur+"-test2", true)}
	f.mans["v"+cur+"-test2"] = updateManifest{Version: cur, File: updateZipName, Size: 62_000_000, SHA256: "ab"}
	checkUpdateBeta()
	snap := upd.snapshotLocal()
	if snap.Phase != "available" || snap.SourceTag != "v"+cur+"-test2" {
		t.Fatalf("same-version next test iteration must be offered: %+v", snap)
	}
}

// The v0.60 tightening, end to end: an install recorded from
// the OFFICIAL tag of the running version must NOT be offered
// that version's test build (identical code) while the
// official release exists.
func TestBetaCheckOfficialInstallNotOfferedSameVersionTest(t *testing.T) {
	dsTestRoot(t)
	resetUpd(t)
	cur := AppVersion
	if err := dsSave("update-state", []byte(`{"installedTag":"v`+cur+`","installedVersion":"`+cur+`"}`)); err != nil {
		t.Fatal(err)
	}
	f := newBetaFixture(t)
	f.list = []ghRelease{fixtureRelease("v"+cur, false), fixtureRelease("v"+cur+"-test1", true)}
	f.mans["v"+cur+"-test1"] = updateManifest{Version: cur, File: updateZipName, Size: 62_000_000, SHA256: "ab"}
	checkUpdateBeta()
	snap := upd.snapshotLocal()
	if snap.Phase != "uptodate" || !strings.Contains(snap.Message, "已是最新测试版") {
		t.Fatalf("an official install must not be offered the same version's test build: %+v", snap)
	}
}

// The other half of the tightening: from an official install,
// a HIGHER-version test build still offers as before.
func TestBetaCheckOfficialInstallOfferedHigherTest(t *testing.T) {
	dsTestRoot(t)
	resetUpd(t)
	cur, nv := AppVersion, nextVersion()
	if err := dsSave("update-state", []byte(`{"installedTag":"v`+cur+`","installedVersion":"`+cur+`"}`)); err != nil {
		t.Fatal(err)
	}
	f := newBetaFixture(t)
	f.list = []ghRelease{fixtureRelease("v"+cur, false), fixtureRelease("v"+nv+"-test1", true)}
	f.mans["v"+nv+"-test1"] = updateManifest{Version: nv, File: updateZipName, Size: 62_000_000, SHA256: "ab"}
	checkUpdateBeta()
	snap := upd.snapshotLocal()
	if snap.Phase != "available" || snap.SourceTag != "v"+nv+"-test1" {
		t.Fatalf("a higher-version test must offer from an official install: %+v", snap)
	}
}

func TestBetaCheckAlreadyOnNewestTest(t *testing.T) {
	dsTestRoot(t)
	resetUpd(t)
	cur := AppVersion
	if err := dsSave("update-state", []byte(`{"installedTag":"v`+cur+`-test1","installedVersion":"`+cur+`"}`)); err != nil {
		t.Fatal(err)
	}
	f := newBetaFixture(t)
	f.list = []ghRelease{fixtureRelease("v"+cur+"-test1", true)}
	f.mans["v"+cur+"-test1"] = updateManifest{Version: cur, File: updateZipName, Size: 62_000_000, SHA256: "ab"}
	checkUpdateBeta()
	snap := upd.snapshotLocal()
	if snap.Phase != "uptodate" || !strings.Contains(snap.Message, "已是最新测试版") {
		t.Fatalf("running the newest test must read as up-to-date: %+v", snap)
	}
}

func TestBetaCheckOlderLineDoesNotOffer(t *testing.T) {
	dsTestRoot(t)
	resetUpd(t)
	f := newBetaFixture(t)
	f.list = []ghRelease{fixtureRelease("v0.58-test3", true)}
	f.mans["v0.58-test3"] = updateManifest{Version: "0.58", File: updateZipName, Size: 62_000_000, SHA256: "ab"}
	checkUpdateBeta()
	if snap := upd.snapshotLocal(); snap.Phase != "uptodate" {
		t.Fatalf("an older-line prerelease must not be offered: %+v", snap)
	}
}

func TestBetaCheckNoPrerelease(t *testing.T) {
	dsTestRoot(t)
	resetUpd(t)
	f := newBetaFixture(t)
	f.list = []ghRelease{fixtureRelease("v0.59", false)}
	checkUpdateBeta()
	snap := upd.snapshotLocal()
	if snap.Phase != "uptodate" || !strings.Contains(snap.Message, "暂无测试版") {
		t.Fatalf("no prerelease must report 暂无测试版: %+v", snap)
	}
}

func TestBetaCheckNetworkFailureSurfaces(t *testing.T) {
	dsTestRoot(t)
	resetUpd(t)
	oldAPI, oldDL := ghAPIBase, ghDLBase
	ghAPIBase, ghDLBase = "http://127.0.0.1:1", "http://127.0.0.1:1"
	defer func() { ghAPIBase, ghDLBase = oldAPI, oldDL }()
	checkUpdateBeta()
	snap := upd.snapshot()
	if snap.Phase != "error" || !strings.Contains(snap.Message, "检查失败") {
		t.Fatalf("network failure must surface like the official check: %+v", snap)
	}
}

// The downgrade-protection pin: a v0.59 (test-line) build
// checking the OFFICIAL channel whose latest is v0.58 must
// read as up-to-date, never as an offer.
func TestOfficialCheckNoDowngradeOffer(t *testing.T) {
	dsTestRoot(t)
	resetUpd(t)
	f := newBetaFixture(t)
	lat := fixtureRelease("v0.58", false)
	// the official flow reads the manifest through the ASSET
	// url; point it at the fixture's download path.
	lat.Assets = append(lat.Assets, struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}{Name: updateManifestName, URL: f.srv.URL + "/" + updateRepo + "/releases/download/v0.58/" + updateManifestName})
	f.lat = &lat
	f.mans["v0.58"] = updateManifest{Version: "0.58", File: updateZipName, Size: 62_000_000, SHA256: "ab"}
	checkUpdateOfficial()
	snap := upd.snapshot()
	if snap.Phase != "uptodate" {
		t.Fatalf("official 0.58 must not be offered to a newer build: %+v", snap)
	}
}

func TestRecordInstalledTagRoundtrip(t *testing.T) {
	dsTestRoot(t)
	if got := loadUpdateState().InstalledTag; got != "" {
		t.Fatalf("fresh state tag = %q, want empty", got)
	}
	recordInstalledTag("v0.59-test2")
	st := loadUpdateState()
	if st.InstalledTag != "v0.59-test2" || st.InstalledVersion != AppVersion {
		t.Fatalf("recorded state = %+v", st)
	}
}
