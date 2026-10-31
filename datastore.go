package main

// datastore.go -- v0.57 architecture block 1: the ONE data layer
// for daemon-owned state under Data/.
//
// Why this exists: before v0.57 every subsystem read and wrote its
// own state file with its own habits -- some atomic (tmp+rename),
// some plain os.WriteFile (a crash mid-write tears the file), none
// with a last-good backup, and a damaged file silently reset the
// user's state to defaults (settings and nodes both had field
// reports in that family). Now every managed file passes through
// here:
//
//   * saves are atomic -- write a temp file in the same directory,
//     fsync, rename over the target; a crash leaves the old OR the
//     new contents, never a torn half;
//   * before a save replaces a file, the previous GOOD contents
//     (parse-checked when a validator is registered) rotate to a
//     sibling "<file>.bak" -- keep-last-good;
//   * a load whose contents fail validation restores the .bak,
//     logs LOUDLY under the [data] module, and hands the caller
//     the recovered bytes; with no usable backup the caller gets
//     an error and applies its own defaults -- the daemon never
//     crashes and never silently zeroes state anymore (the event
//     is always in the console hub);
//   * schema versions live in ONE sidecar manifest,
//     Data/schema.json, so NO managed payload format changed by a
//     single byte (anything else that parses these files -- older
//     builds during a downgrade, the vault seal/restore, a human
//     with a text editor -- sees exactly the v0.56 formats).
//     Migrations are a registry of per-file steps; v1 = the
//     current on-disk format, so the registry starts empty.
//
// The layer is deliberately a bounded module inside package main
// (this repo is a single main package; dataDir/logf live here).
// Architecture block 4 (backend layering) can promote it to an
// internal package unchanged -- its surface is only the ds*
// functions below.
//
// INVENTORY of everything the daemon persists under Data/
// (audited 2026-10-30 for v0.57; keep this list honest):
//
// IN SCOPE -- daemon-owned app state, managed below:
//   settings.json                 settings.go      user preferences
//   nodes.json                    nodes.go         node library + subs + rules
//   links.txt                     main/nodes       legacy node-link mirror (still written)
//   apps.txt                      main.go          web-app list (name<TAB>url<TAB>mode)
//   apps-deleted.txt              main.go          bundled web-app tombstones
//   layout.json                   main.go          desktop icon positions + folders (frontend-owned blob)
//   wingeom.json                  wingeom.go       remembered window rectangles
//   notifications.json            notify.go        notification center store
//   remote.json                   remote.go        remote-assist operator list
//   ssh/connections.json          ssh.go           SSH connection list (NOT the keys)
//   servermon/config.json         servermon.go     monitored-server list
//   servermon/usage.json          servermon.go     per-server usage rollup
//   servermon/agent-<id>.json     servermon_agent  per-server agent metadata (dynamic path)
//   apps/apps.json                apps.go          store-app install registry
//   sandbox/apps.json             sandbox.go       isolated-install registry
//   sandbox/pending-install.json  sandbox.go       in-flight install record (dynamic path in tests)
//   components/sandboxie.json     sandbox.go       Sandboxie component stamp
//   openclaw/mj-prefs.json        openclaw_plus    OpenClaw preferences (MJ-owned)
//   ffstate.json                  firefox.go       Firefox process birth-mode record
//   accel.txt                     accel.go         global-acceleration flag ("1"/"0")
//   update-token.txt              update.go        update channel token (backup rotation OFF: it is a credential)
//   firefox-version.txt           firefox.go       installed Firefox component stamp
//   openclaw-version.txt          openclaw.go      installed OpenClaw component stamp
//   components/<id>/mj-version.txt procomp.go      per-component version stamp (dynamic path)
//
// OUT OF SCOPE -- with the reason each stays on its own path:
//   mj-vault/**, vault meta       vault.go         sealed/crypto data; format frozen, untouched by design
//   ChromeProfile/, FirefoxProfile/, ShellProfile/  browser-owned profiles, thousands of files, browser-managed
//   core/** (config.generated.json, core binaries)  generated sing-box config + payloads; regenerated from nodes.json
//   apps/<id>/**, components/<id>/** payloads       installed program files, not state
//   chrome-icon.ico, ff-icon.ico (+ .stamp sidecars), files/.thumbs/**  regenerable caches
//   wallpaper.bin, wallpaper.ctype                  user binary asset (blob), not state
//   ssh/keys/**, ssh/known_hosts                    key material / SSH-format files owned by the SSH layer
//   openclaw/state/openclaw.json, openclaw/.mj-lock-baseline.json  the child program's OWN config files
//   Sandboxie.ini (in C:\Windows)                   Sandboxie's own config format
//   logs/**, servermon/hist-*.jsonl                 append-only log/time-series data, trimmed by their owners
//   tsnet/**                                        tsnet library state directory
//   update/** staging, *.tmp/*.part downloads       transient transfer staging
//   files/**                                        the user's imported-file store (content, not state)

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// ---------- registry ----------

type dsSpec struct {
	rel      string             // path under Data/, forward slashes
	version  int                // current schema version (all 1: v1 == the pre-v0.57 format)
	perm     os.FileMode        // file mode used when the file is (re)created by a save
	validate func([]byte) error // parse check for corruption detection + backup rotation; nil = any bytes are good (text state)
	backup   bool               // rotate a keep-last-good .bak on save (off for the update token: a credential must not multiply)
}

// dsJSONValidatorOf builds a parse validator for the
// dynamic-path managed files (no registry entry): b is good when
// it unmarshals into the same shape as zero.
func dsJSONValidatorOf[T any](zero T) func([]byte) error {
	return func(b []byte) error {
		v := zero
		return json.Unmarshal(b, &v)
	}
}

var dsRegistry = map[string]dsSpec{
	"settings":   {rel: "settings.json", version: 1, perm: 0o600, backup: true, validate: func(b []byte) error { var s mjSettings; return json.Unmarshal(b, &s) }},
	"nodes":      {rel: "nodes.json", version: 1, perm: 0o600, backup: true, validate: func(b []byte) error { var s nodeStore; return json.Unmarshal(b, &s) }},
	"links":      {rel: "links.txt", version: 1, perm: 0o600, backup: true},
	"webapps":    {rel: "apps.txt", version: 1, perm: 0o644, backup: true},
	"tombstones": {rel: "apps-deleted.txt", version: 1, perm: 0o644, backup: true},
	"layout": {rel: "layout.json", version: 1, perm: 0o644, backup: true, validate: func(b []byte) error {
		if !json.Valid(b) {
			return fmt.Errorf("invalid JSON")
		}
		return nil
	}},
	"wingeom":       {rel: "wingeom.json", version: 1, perm: 0o644, backup: true, validate: func(b []byte) error { var m map[string]winGeom; return json.Unmarshal(b, &m) }},
	"notifications": {rel: "notifications.json", version: 1, perm: 0o600, backup: true, validate: func(b []byte) error { var f notifyFile; return json.Unmarshal(b, &f) }},
	"remote": {rel: "remote.json", version: 1, perm: 0o600, backup: true, validate: func(b []byte) error {
		var f struct {
			Operators []remoteOperator `json:"operators"`
		}
		return json.Unmarshal(b, &f)
	}},
	"ssh-connections":  {rel: "ssh/connections.json", version: 1, perm: 0o600, backup: true, validate: func(b []byte) error { var c []sshConn; return json.Unmarshal(b, &c) }},
	"servermon-config": {rel: "servermon/config.json", version: 1, perm: 0o600, backup: true, validate: func(b []byte) error { var m map[string]monCfg; return json.Unmarshal(b, &m) }},
	"servermon-usage":  {rel: "servermon/usage.json", version: 1, perm: 0o600, backup: true, validate: func(b []byte) error { var m map[string]*monUsage; return json.Unmarshal(b, &m) }},
	"apps-registry":    {rel: "apps/apps.json", version: 1, perm: 0o644, backup: true, validate: func(b []byte) error { var m map[string]installedApp; return json.Unmarshal(b, &m) }},
	"sandbox-apps":     {rel: "sandbox/apps.json", version: 1, perm: 0o644, backup: true, validate: func(b []byte) error { var m map[string]boxApp; return json.Unmarshal(b, &m) }},
	"sbie-meta":        {rel: "components/sandboxie.json", version: 1, perm: 0o644, backup: true, validate: func(b []byte) error { var m sbieMeta; return json.Unmarshal(b, &m) }},
	"oc-prefs":         {rel: "openclaw/mj-prefs.json", version: 1, perm: 0o644, backup: true, validate: func(b []byte) error { var m map[string]json.RawMessage; return json.Unmarshal(b, &m) }},
	"ffstate":          {rel: "ffstate.json", version: 1, perm: 0o644, backup: true, validate: func(b []byte) error { var r ffProcRecord; return json.Unmarshal(b, &r) }},
	"accel":            {rel: "accel.txt", version: 1, perm: 0o600, backup: true},
	"update-token":     {rel: "update-token.txt", version: 1, perm: 0o600, backup: false},
	// v0.59: the updater's memory of which release tag the
	// running build was installed from (drives the beta
	// channel's offer rule; written by the apply handover).
	"update-state": {rel: "update-state.json", version: 1, perm: 0o600, backup: true, validate: func(b []byte) error { var s updateStateFile; return json.Unmarshal(b, &s) }},
	"ff-version":   {rel: "firefox-version.txt", version: 1, perm: 0o644, backup: true},
	"oc-version":   {rel: "openclaw-version.txt", version: 1, perm: 0o644, backup: true},
}

// dsMigrations holds per-file schema steps: file name ->
// from-version -> transform. v1 is the current on-disk format for
// every file, so there is nothing to register yet; a future format
// change adds a step here (v1->v2 ...) and bumps the registry
// version, and dsLoad applies the chain and persists the result.
var dsMigrations = map[string]map[int]func([]byte) ([]byte, error){}

// ---------- status ----------

type dsStatus int

const (
	dsOK        dsStatus = iota // file present, contents valid (or validation not applicable)
	dsMissing                   // file does not exist: first run / never saved
	dsCorrupt                   // file present but invalid AND no usable backup; caller applies its defaults
	dsRecovered                 // file was invalid; contents restored from the keep-last-good backup
)

// ---------- core path primitives ----------

var dsSaveMu sync.Mutex // serializes rotation+replace so two subsystems can never interleave on one file

// dsWriteAtomic replaces path with b via a temp file in the same
// directory + fsync + rename. A crash at any point leaves either
// the old file or the new one, never a torn mixture.
func dsWriteAtomic(path string, b []byte, perm os.FileMode) error {
	tmp := path + ".dstmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// dsSaveFile is the save half of the layer, for any path (the
// named API below adds registry bookkeeping). When backup is set,
// the file's previous GOOD contents rotate to "<path>.bak" before
// the replacement lands; a previous file that itself fails
// validation never overwrites a good backup.
func dsSaveFile(path string, b []byte, perm os.FileMode, validate func([]byte) error, backup bool) error {
	dsSaveMu.Lock()
	defer dsSaveMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if backup {
		if cur, err := os.ReadFile(path); err == nil && len(cur) > 0 {
			if validate == nil || validate(cur) == nil {
				_ = dsWriteAtomic(path+".bak", cur, perm) // best effort: a failed rotation must not block the save
			}
		}
	}
	return dsWriteAtomic(path, b, perm)
}

// dsLoadFile is the load half of the layer. See the status
// constants for the contract; every recovery and every
// unrecoverable corruption is logged under [data] here, once, so
// no caller has to remember.
func dsLoadFile(path string, validate func([]byte) error) ([]byte, dsStatus, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, dsMissing, nil
		}
		logf("data", "warn", "数据文件 %s 读取失败：%v", filepath.Base(path), err)
		return nil, dsOK, err
	}
	if validate == nil {
		return b, dsOK, nil
	}
	if verr := validate(b); verr != nil {
		base := filepath.Base(path)
		bak, berr := os.ReadFile(path + ".bak")
		if berr == nil && validate(bak) == nil {
			// Restore the last-good bytes over the damaged file
			// (direct atomic write: routing this through the
			// save path would rotate the CORRUPT bytes into the
			// backup slot). Keep the damaged file's own mode.
			perm := os.FileMode(0o600)
			if st, serr := os.Stat(path); serr == nil {
				perm = st.Mode().Perm()
			}
			_ = dsWriteAtomic(path, bak, perm)
			logf("data", "error", "数据文件 %s 已损坏（%v），已从上一份完好备份恢复，内容未丢失", base, verr)
			return bak, dsRecovered, nil
		}
		logf("data", "error", "数据文件 %s 已损坏（%v）且没有可用备份，调用方将按默认值处理——损坏内容保留在原文件里以便排查", base, verr)
		return nil, dsCorrupt, verr
	}
	return b, dsOK, nil
}

// dsRemoveFile deletes a managed file AND its backup, for the
// call sites whose semantics are "this record is gone" (a cleared
// pending install or Firefox process record must not be able to
// resurrect from the .bak on a later damaged read).
func dsRemoveFile(path string) {
	_ = os.Remove(path)
	_ = os.Remove(path + ".bak")
}

// ---------- named (registry) API ----------

// dsRootOverride exists for tests: when set, named files resolve
// under it instead of dataDir(). Never set in production.
var dsRootOverride string

func dsRoot() string {
	if dsRootOverride != "" {
		return dsRootOverride
	}
	return dataDir()
}

func dsSpecFor(name string) (dsSpec, error) {
	spec, ok := dsRegistry[name]
	if !ok {
		return dsSpec{}, fmt.Errorf("datastore: unregistered file %q", name)
	}
	return spec, nil
}

func dsPath(name string) (string, error) {
	spec, err := dsSpecFor(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(dsRoot(), filepath.FromSlash(spec.rel)), nil
}

var dsLoadLogged sync.Map // name -> bool: the "loaded (schema vN)" info line fires once per file per process

// dsNoteLoaded emits the one-time [data] load line for a named
// file. Path-API users whose file is also in the registry (the
// notification store, which supports test paths) call it too, so
// every managed file's load shows up exactly once.
func dsNoteLoaded(name string, spec dsSpec) {
	if _, seen := dsLoadLogged.LoadOrStore(name, true); !seen {
		logf("data", "info", "已加载 %s（schema v%d）", spec.rel, spec.version)
	}
}

// dsLoad reads a registered state file through the layer: schema
// check against the manifest, corruption recovery, migration
// chain (empty at v1), and a one-time [data] load line naming the
// file and its schema version.
func dsLoad(name string) ([]byte, dsStatus, error) {
	spec, err := dsSpecFor(name)
	if err != nil {
		return nil, dsOK, err
	}
	path := filepath.Join(dsRoot(), filepath.FromSlash(spec.rel))
	dsEnsureManifest()
	b, status, lerr := dsLoadFile(path, spec.validate)
	if lerr == nil && (status == dsOK || status == dsRecovered) {
		// Migration chain: the manifest may record an older schema
		// than the registry's current version (a future upgrade).
		if from := dsManifestVersion(spec.rel); from < spec.version {
			cur := b
			for v := from; v < spec.version; v++ {
				step := dsMigrations[name][v]
				if step == nil {
					logf("data", "error", "数据文件 %s 缺少 schema v%d→v%d 的迁移步骤，已停止迁移并保留原内容", spec.rel, v, v+1)
					break
				}
				nb, merr := step(cur)
				if merr != nil {
					logf("data", "error", "数据文件 %s 的 schema v%d→v%d 迁移失败（%v），保留原内容", spec.rel, v, v+1, merr)
					break
				}
				cur = nb
				logf("data", "info", "数据文件 %s 已完成 schema 迁移 v%d → v%d", spec.rel, v, v+1)
				if v+1 == spec.version {
					b = cur
					_ = dsSave(name, b) // persist the migrated form through the same safe path
				}
			}
		}
		dsNoteLoaded(name, spec)
	}
	return b, status, lerr
}

// dsSave writes a registered state file through the layer
// (atomic + keep-last-good per the registry entry).
func dsSave(name string, b []byte) error {
	spec, err := dsSpecFor(name)
	if err != nil {
		return err
	}
	path := filepath.Join(dsRoot(), filepath.FromSlash(spec.rel))
	dsEnsureManifest()
	if err := dsSaveFile(path, b, spec.perm, spec.validate, spec.backup); err != nil {
		return err
	}
	// v0.59 block 3: a successful save of a bus-mapped file is a
	// state mutation -- announce it so open views re-pull (item
	// #8). dsBusEmit (events.go) knows the mapping; unmapped
	// files stay silent by design.
	dsBusEmit(name)
	return nil
}

// ---------- schema manifest (Data/schema.json) ----------

type dsManifest struct {
	Files map[string]int `json:"files"` // rel path (slash form) -> schema version
}

var (
	dsManifestMu    sync.Mutex
	dsManifestCache *dsManifest
	dsManifestTried bool
)

func dsManifestPath() string { return filepath.Join(dsRoot(), "schema.json") }

// dsEnsureManifest loads (or creates) Data/schema.json and brings
// it in line with the registry: every managed file recorded at
// its current version. Best effort throughout -- the manifest is
// bookkeeping, never a reason to fail a load or a save. A manifest
// recording a NEWER version than this build's registry (a
// downgrade) is left alone and logged, never rewritten downwards.
func dsEnsureManifest() {
	dsManifestMu.Lock()
	defer dsManifestMu.Unlock()
	if dsManifestCache != nil {
		return
	}
	m := &dsManifest{Files: map[string]int{}}
	if b, err := os.ReadFile(dsManifestPath()); err == nil {
		_ = json.Unmarshal(b, m)
		if m.Files == nil {
			m.Files = map[string]int{}
		}
	}
	changed := false
	for _, spec := range dsRegistry {
		cur, ok := m.Files[spec.rel]
		if !ok {
			m.Files[spec.rel] = spec.version
			changed = true
		} else if cur > spec.version {
			logf("data", "warn", "schema.json 记录的 %s 版本 v%d 高于本程序支持的 v%d（可能刚从更新的版本回退），保留记录不改动", spec.rel, cur, spec.version)
		}
	}
	dsManifestCache = m
	if changed || !dsManifestTried {
		dsManifestTried = true
		out, err := json.MarshalIndent(m, "", "  ")
		if err == nil {
			_ = dsSaveFile(dsManifestPath(), out, 0o600, nil, false)
			names := make([]string, 0, len(m.Files))
			for rel := range m.Files {
				names = append(names, rel)
			}
			sort.Strings(names)
			logf("data", "info", "数据清单 schema.json 已更新：%d 个受管文件（schema v1 起步）", len(names))
		}
	}
}

// dsManifestVersion reports the schema version the manifest
// records for a file, defaulting to the registry version when the
// manifest has nothing to say (fresh install: everything is
// current by definition).
func dsManifestVersion(rel string) int {
	dsManifestMu.Lock()
	defer dsManifestMu.Unlock()
	if dsManifestCache != nil {
		if v, ok := dsManifestCache.Files[rel]; ok {
			return v
		}
	}
	for _, spec := range dsRegistry {
		if spec.rel == rel {
			return spec.version
		}
	}
	return 1
}
