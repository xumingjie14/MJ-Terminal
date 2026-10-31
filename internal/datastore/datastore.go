package datastore

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
// The layer is a bounded internal package since v0.60
// architecture block 4 (internal/datastore). Its seams back
// into the daemon are deliberately narrow: RootFunc resolves
// the Data directory (package main's dataDir), logging goes
// through internal/diag, the managed-file table is registered
// from package main (the validators name domain types), and a
// successful named save reports through SaveHook (wired to
// the change bus). Package main keeps ds* forwarders in
// bridge.go so existing call sites are untouched.
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

	"mjlight/internal/diag"
)

// ---------- registry ----------

type Spec struct {
	Rel      string             // path under Data/, forward slashes
	Version  int                // current schema version (all 1: v1 == the pre-v0.57 format)
	Perm     os.FileMode        // file mode used when the file is (re)created by a save
	Validate func([]byte) error // parse check for corruption detection + backup rotation; nil = any bytes are good (text state)
	Backup   bool               // rotate a keep-last-good .bak on save (off for the update token: a credential must not multiply)
}

// JSONValidatorOf builds a parse validator for the
// dynamic-path managed files (no registry entry): b is good when
// it unmarshals into the same shape as zero.
func JSONValidatorOf[T any](zero T) func([]byte) error {
	return func(b []byte) error {
		v := zero
		return json.Unmarshal(b, &v)
	}
}

// registry holds the managed-file table. It is populated by
// Register calls from package main (dsregistry.go) during
// init: the validators parse into domain types (mjSettings,
// nodeStore, ...), so the table cannot live in this package
// without inverting the dependency -- this package must never
// import the domains it serves.
var registry = map[string]Spec{}

// migrations holds per-file schema steps: file name ->
// from-version -> transform. v1 is the current on-disk format
// for every file, so nothing is registered yet; a future
// format change registers a step (v1->v2 ...) via
// RegisterMigration, bumps the registry version, and Load
// applies the chain and persists the result.
var migrations = map[string]map[int]func([]byte) ([]byte, error){}

// Register adds (or replaces) one managed file. Package main
// calls it from init with exactly the entries the pre-v0.60
// dsRegistry literal held -- paths, versions, permissions and
// validators unchanged, so no Data file changes by a byte.
func Register(name string, spec Spec) { registry[name] = spec }

// RegisterMigration adds one schema migration step for a
// managed file (see migrations above).
func RegisterMigration(name string, fromVersion int, step func([]byte) ([]byte, error)) {
	m := migrations[name]
	if m == nil {
		m = map[int]func([]byte) ([]byte, error){}
		migrations[name] = m
	}
	m[fromVersion] = step
}

// RegisteredSpecs returns a copy of the managed-file table
// (the datastore tests and the feature-registry gate read it).
func RegisteredSpecs() map[string]Spec {
	out := make(map[string]Spec, len(registry))
	for name, spec := range registry {
		out[name] = spec
	}
	return out
}

// ---------- status ----------

type Status int

const (
	OK        Status = iota // file present, contents valid (or validation not applicable)
	Missing                 // file does not exist: first run / never saved
	Corrupt                 // file present but invalid AND no usable backup; caller applies its defaults
	Recovered               // file was invalid; contents restored from the keep-last-good backup
)

// ---------- core path primitives ----------

var saveMu sync.Mutex // serializes rotation+replace so two subsystems can never interleave on one file

// writeAtomic replaces path with b via a temp file in the same
// directory + fsync + rename. A crash at any point leaves either
// the old file or the new one, never a torn mixture.
func writeAtomic(path string, b []byte, perm os.FileMode) error {
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

// SaveFile is the save half of the layer, for any path (the
// named API below adds registry bookkeeping). When backup is set,
// the file's previous GOOD contents rotate to "<path>.bak" before
// the replacement lands; a previous file that itself fails
// validation never overwrites a good backup.
func SaveFile(path string, b []byte, perm os.FileMode, validate func([]byte) error, backup bool) error {
	saveMu.Lock()
	defer saveMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if backup {
		if cur, err := os.ReadFile(path); err == nil && len(cur) > 0 {
			if validate == nil || validate(cur) == nil {
				_ = writeAtomic(path+".bak", cur, perm) // best effort: a failed rotation must not block the save
			}
		}
	}
	return writeAtomic(path, b, perm)
}

// LoadFile is the load half of the layer. See the status
// constants for the contract; every recovery and every
// unrecoverable corruption is logged under [data] here, once, so
// no caller has to remember.
func LoadFile(path string, validate func([]byte) error) ([]byte, Status, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, Missing, nil
		}
		diag.Logf("data", "warn", "数据文件 %s 读取失败：%v", filepath.Base(path), err)
		return nil, OK, err
	}
	if validate == nil {
		return b, OK, nil
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
			_ = writeAtomic(path, bak, perm)
			diag.Logf("data", "error", "数据文件 %s 已损坏（%v），已从上一份完好备份恢复，内容未丢失", base, verr)
			return bak, Recovered, nil
		}
		diag.Logf("data", "error", "数据文件 %s 已损坏（%v）且没有可用备份，调用方将按默认值处理——损坏内容保留在原文件里以便排查", base, verr)
		return nil, Corrupt, verr
	}
	return b, OK, nil
}

// RemoveFile deletes a managed file AND its backup, for the
// call sites whose semantics are "this record is gone" (a cleared
// pending install or Firefox process record must not be able to
// resurrect from the .bak on a later damaged read).
func RemoveFile(path string) {
	_ = os.Remove(path)
	_ = os.Remove(path + ".bak")
}

// ---------- named (registry) API ----------

// RootOverride exists for tests: when set, named files resolve
// under it instead of the daemon's Data directory. Never set
// in production.
var RootOverride string

// RootFunc resolves the daemon's Data directory for named
// files. Assigned by package main's bridge (its dataDir
// function) during init.
var RootFunc func() string

// SaveHook, when set, is called with the registry name after
// every successful named Save. Package main wires it to the
// change bus (internal/events), so a save of a bus-mapped file
// announces the mutation to open views; the data layer itself
// stays bus-agnostic.
var SaveHook func(name string)

func root() string {
	if RootOverride != "" {
		return RootOverride
	}
	if RootFunc != nil {
		return RootFunc()
	}
	return "Data"
}

// ResetManifestCache drops the in-memory schema-manifest
// cache, so the next named load/save re-reads Data/schema.json
// under the current root. The test suite uses it (via
// dsTestRoot) to simulate a fresh daemon start per scenario.
func ResetManifestCache() {
	manifestMu.Lock()
	manifestCache = nil
	manifestTried = false
	manifestMu.Unlock()
}

func SpecFor(name string) (Spec, error) {
	spec, ok := registry[name]
	if !ok {
		return Spec{}, fmt.Errorf("datastore: unregistered file %q", name)
	}
	return spec, nil
}

func Path(name string) (string, error) {
	spec, err := SpecFor(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(root(), filepath.FromSlash(spec.Rel)), nil
}

var loadLogged sync.Map // name -> bool: the "loaded (schema vN)" info line fires once per file per process

// NoteLoaded emits the one-time [data] load line for a named
// file. Path-API users whose file is also in the registry (the
// notification store, which supports test paths) call it too, so
// every managed file's load shows up exactly once.
func NoteLoaded(name string, spec Spec) {
	if _, seen := loadLogged.LoadOrStore(name, true); !seen {
		diag.Logf("data", "info", "已加载 %s（schema v%d）", spec.Rel, spec.Version)
	}
}

// Load reads a registered state file through the layer: schema
// check against the manifest, corruption recovery, migration
// chain (empty at v1), and a one-time [data] load line naming the
// file and its schema version.
func Load(name string) ([]byte, Status, error) {
	spec, err := SpecFor(name)
	if err != nil {
		return nil, OK, err
	}
	path := filepath.Join(root(), filepath.FromSlash(spec.Rel))
	ensureManifest()
	b, status, lerr := LoadFile(path, spec.Validate)
	if lerr == nil && (status == OK || status == Recovered) {
		// Migration chain: the manifest may record an older schema
		// than the registry's current version (a future upgrade).
		if from := manifestVersion(spec.Rel); from < spec.Version {
			cur := b
			for v := from; v < spec.Version; v++ {
				step := migrations[name][v]
				if step == nil {
					diag.Logf("data", "error", "数据文件 %s 缺少 schema v%d→v%d 的迁移步骤，已停止迁移并保留原内容", spec.Rel, v, v+1)
					break
				}
				nb, merr := step(cur)
				if merr != nil {
					diag.Logf("data", "error", "数据文件 %s 的 schema v%d→v%d 迁移失败（%v），保留原内容", spec.Rel, v, v+1, merr)
					break
				}
				cur = nb
				diag.Logf("data", "info", "数据文件 %s 已完成 schema 迁移 v%d → v%d", spec.Rel, v, v+1)
				if v+1 == spec.Version {
					b = cur
					_ = Save(name, b) // persist the migrated form through the same safe path
				}
			}
		}
		NoteLoaded(name, spec)
	}
	return b, status, lerr
}

// Save writes a registered state file through the layer
// (atomic + keep-last-good per the registry entry).
func Save(name string, b []byte) error {
	spec, err := SpecFor(name)
	if err != nil {
		return err
	}
	path := filepath.Join(root(), filepath.FromSlash(spec.Rel))
	ensureManifest()
	if err := SaveFile(path, b, spec.Perm, spec.Validate, spec.Backup); err != nil {
		return err
	}
	// v0.59 block 3: a successful save of a bus-mapped file is a
	// state mutation -- announce it so open views re-pull (item
	// #8). The mapping lives with the bus (wired in through
	// SaveHook by package main); unmapped files stay silent by
	// design.
	if SaveHook != nil {
		SaveHook(name)
	}
	return nil
}

// ---------- schema manifest (Data/schema.json) ----------

type Manifest struct {
	Files map[string]int `json:"files"` // rel path (slash form) -> schema version
}

var (
	manifestMu    sync.Mutex
	manifestCache *Manifest
	manifestTried bool
)

func manifestPath() string { return filepath.Join(root(), "schema.json") }

// ensureManifest loads (or creates) Data/schema.json and brings
// it in line with the registry: every managed file recorded at
// its current version. Best effort throughout -- the manifest is
// bookkeeping, never a reason to fail a load or a save. A manifest
// recording a NEWER version than this build's registry (a
// downgrade) is left alone and logged, never rewritten downwards.
func ensureManifest() {
	manifestMu.Lock()
	defer manifestMu.Unlock()
	if manifestCache != nil {
		return
	}
	m := &Manifest{Files: map[string]int{}}
	if b, err := os.ReadFile(manifestPath()); err == nil {
		_ = json.Unmarshal(b, m)
		if m.Files == nil {
			m.Files = map[string]int{}
		}
	}
	changed := false
	for _, spec := range registry {
		cur, ok := m.Files[spec.Rel]
		if !ok {
			m.Files[spec.Rel] = spec.Version
			changed = true
		} else if cur > spec.Version {
			diag.Logf("data", "warn", "schema.json 记录的 %s 版本 v%d 高于本程序支持的 v%d（可能刚从更新的版本回退），保留记录不改动", spec.Rel, cur, spec.Version)
		}
	}
	manifestCache = m
	if changed || !manifestTried {
		manifestTried = true
		out, err := json.MarshalIndent(m, "", "  ")
		if err == nil {
			_ = SaveFile(manifestPath(), out, 0o600, nil, false)
			names := make([]string, 0, len(m.Files))
			for rel := range m.Files {
				names = append(names, rel)
			}
			sort.Strings(names)
			diag.Logf("data", "info", "数据清单 schema.json 已更新：%d 个受管文件（schema v1 起步）", len(names))
		}
	}
}

// manifestVersion reports the schema version the manifest
// records for a file, defaulting to the registry version when the
// manifest has nothing to say (fresh install: everything is
// current by definition).
func manifestVersion(rel string) int {
	manifestMu.Lock()
	defer manifestMu.Unlock()
	if manifestCache != nil {
		if v, ok := manifestCache.Files[rel]; ok {
			return v
		}
	}
	for _, spec := range registry {
		if spec.Rel == rel {
			return spec.Version
		}
	}
	return 1
}
