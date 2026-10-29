package main

// ---------- app isolation: Sandboxie component + managed box (v0.39) ----------
//
// Portable apps (the store) cover software that ships a green build.
// Everything else -- WeChat and the long tail of "real" installers --
// only runs on Windows by writing itself into Program Files, the
// registry and AppData. MJ's answer is not to fake isolation but to
// host the real thing: Sandboxie Plus (pinned below), whose kernel
// driver intercepts those writes into a sandbox ("box") whose file
// root MJ pins inside Data/sandbox. A boxed install therefore
// physically lives in Data, travels with the folder, and deleting
// the box is a true uninstall. Validated end-to-end on Windows CI
// before productization (sandboxie-validate run #1: WeChat 4.2.0
// install / containment / launch / path-change / cleanup all PASS).
//
// The honest boundaries, also printed verbatim in 设置 → 应用隔离:
//   - the component is a kernel driver + service: installing it
//     needs ONE administrator confirmation (UAC) per machine, and
//     it stays on the host until uninstalled there. This feature is
//     for the user's own PCs, not for shared/public machines.
//   - boxed apps use the host network directly. An app that takes
//     a proxy can be pointed at 127.0.0.1:2080 and then uses the
//     node; forcing ALL of a boxed app's traffic through the node
//     is a paid Sandboxie Plus feature and MJ does not offer it.
//   - apps that install their own service/driver, DRM or anti-cheat
//     may not run boxed. That is a property of those apps, not a
//     silent failure: the install flow reports what it found.
//
// Packaging: the pinned installer is embedded in the EXE (the same
// go:embed trick as the sing-box core), so a fresh PC with no
// network can install the component offline. The binary itself is
// NOT committed to git (see .gitignore): the build workflow
// downloads it from the official release and verifies size+SHA-256
// before compiling, and a local build needs the same fetch:
//
//   curl -L -o assets/sandboxie-plus-x64-v1.18.8.exe \
//     https://github.com/sandboxie-plus/Sandboxie/releases/download/v1.18.8/Sandboxie-Plus-x64-v1.18.8.exe
//   (24,930,680 bytes, SHA-256 pinned in sbieSHA256 below)
//
// The daemon extracts the embedded package to Data/components on
// demand, verifies it, and installs from there; an online download
// (same pin, same verification) is only the fallback when the
// embedded copy somehow fails to extract or verify.
//
// Local mux only: nothing in this module is exposed on the remote
// (Tailscale) mux -- installing a driver is a local, physical act.

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf16"
)

// ---------- pinned component ----------

const (
	sbieVersion  = "1.18.8"
	sbieFileName = "sandboxie-plus-x64-v1.18.8.exe"
	sbieSize     = int64(24930680)
	sbieSHA256   = "f27078ff74a07906fd51476079e1a7a8f5e20c8bb75a9d2ae86169d47179ae76"
	sbieURL      = "https://github.com/sandboxie-plus/Sandboxie/releases/download/v1.18.8/Sandboxie-Plus-x64-v1.18.8.exe"
	sbieBoxName  = "MJBox"
)

//go:embed assets/sandboxie-plus-x64-v1.18.8.exe
var sbieEmbedded []byte

// Inno silent-install switches (the Sandboxie Plus installer is
// Inno Setup; verified against the official installer in CI).
var sbieInstallArgs = []string{"/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART", "/SP-"}

// Inno silent-uninstall switches (unins000.exe).
var sbieUninstallArgs = []string{"/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART"}

// ---------- paths ----------

func sandboxDir() string     { return filepath.Join(dataDir(), "sandbox") }
func sandboxBoxRoot() string { return filepath.Join(sandboxDir(), sbieBoxName) }
func sandboxIncomingDir() string {
	d := filepath.Join(sandboxDir(), "incoming")
	_ = os.MkdirAll(d, 0o755)
	return d
}
func sandboxAppsPath() string { return filepath.Join(sandboxDir(), "apps.json") }

func componentsDir() string {
	d := filepath.Join(dataDir(), "components")
	_ = os.MkdirAll(d, 0o755)
	return d
}
func componentPkgPath() string { return filepath.Join(componentsDir(), sbieFileName) }
func componentMetaPath() string {
	return filepath.Join(componentsDir(), "sandboxie.json")
}

func sbieIniPath() string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return filepath.Join(root, "Sandboxie.ini")
}

// sbieStartExePath returns the installed Start.exe path, or "".
// Sandboxie Plus installs under Program Files\Sandboxie-Plus.
func sbieStartExePath() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	pf := os.Getenv("ProgramFiles")
	if pf == "" {
		pf = `C:\Program Files`
	}
	p := filepath.Join(pf, "Sandboxie-Plus", "Start.exe")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	if pfx := os.Getenv("ProgramFiles(x86)"); pfx != "" {
		p2 := filepath.Join(pfx, "Sandboxie-Plus", "Start.exe")
		if _, err := os.Stat(p2); err == nil {
			return p2
		}
	}
	return ""
}

func sbieUninstallerPath() string {
	se := sbieStartExePath()
	if se == "" {
		return ""
	}
	p := filepath.Join(filepath.Dir(se), "unins000.exe")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

// ---------- Sandboxie.ini: decode / encode / merge ----------
//
// Sandboxie.ini lives in C:\Windows and is normally UTF-16LE with
// BOM. The daemon must preserve whatever encoding and whatever
// sections other tools (or the user, in SandMan) put there; MJ owns
// exactly one section, [MJBox], and rewrites only that one.

// decodeSbieIni turns raw ini bytes into text, reporting whether
// the file was UTF-16 (so the write-back can keep the encoding).
func decodeSbieIni(b []byte) (string, bool) {
	if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE {
		u := make([]uint16, 0, len(b)/2)
		for i := 2; i+1 < len(b); i += 2 {
			u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
		}
		return string(utf16.Decode(u)), true
	}
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return string(b[3:]), false
	}
	return string(b), false
}

// encodeSbieIni renders text back to ini bytes in the chosen
// encoding (UTF-16LE+BOM when utf16 is set).
func encodeSbieIni(text string, utf16enc bool) []byte {
	if !utf16enc {
		return []byte(text)
	}
	u := utf16.Encode([]rune(text))
	out := make([]byte, 0, 2+len(u)*2)
	out = append(out, 0xFF, 0xFE)
	for _, c := range u {
		out = append(out, byte(c), byte(c>>8))
	}
	return out
}

// mergeBoxSection returns the ini text with the [MJBox] section
// replaced by the canonical one for boxRoot (plus the current
// data-seal rule lines), every other byte of user content
// preserved. Pure and idempotent: merging the result again with
// the same inputs reports changed=false. Section headers match
// case-insensitively, as Sandboxie itself treats them.
func mergeBoxSection(text, boxRoot string, sealLines []string) (string, bool) {
	section := "[" + sbieBoxName + "]\r\n" +
		"Enabled=y\r\n" +
		"FileRootPath=" + boxRoot + "\r\n" +
		"AutoRecover=n\r\n" +
		// BorderColor is the official per-sandbox key for the
		// frame Sandboxie draws around a boxed app's foreground
		// window (v0.42): MJ turns it off for the managed box —
		// Stage Manager already badges these windows, and the
		// yellow frame fights the desktop's macOS look. The
		// value follows the documented Plus format
		// (#hex,mode,width,alpha,label): mode off = never draw,
		// label no = no sandbox-name label either. Whether the
		// field machine's frame (seen in the v0.41 acceptance
		// photo, where SandMan is not part of MJ's runtime)
		// answers to this key is a real-PC check, not assumed.
		"BorderColor=#000000,off,6,192,no\r\n"
	for _, ln := range sealLines {
		section += ln + "\r\n"
	}
	// Normalize to CRLF for the surgery, whatever the file used.
	norm := strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(norm, "\n")
	var kept []string
	inBox := false
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			name := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			inBox = strings.EqualFold(name, sbieBoxName)
			if inBox {
				continue // drop the old section header
			}
		}
		if inBox {
			continue // drop the old section body
		}
		kept = append(kept, ln)
	}
	body := strings.TrimRight(strings.Join(kept, "\r\n"), "\r\n")
	var out string
	if body == "" {
		out = section
	} else {
		out = body + "\r\n\r\n" + section
	}
	orig := strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	newNorm := strings.ReplaceAll(out, "\r\n", "\n")
	return out, strings.TrimRight(orig, "\n") != strings.TrimRight(newNorm, "\n")
}

// iniHasCurrentBox reports whether the ini text already carries
// our section pointing at the CURRENT box root, with the CURRENT
// data-seal rule lines.
func iniHasCurrentBox(text, boxRoot string, sealLines []string) bool {
	_, changed := mergeBoxSection(text, boxRoot, sealLines)
	return !changed
}

// ---------- data seal: per-program read sealing (v0.42) ----------
//
// Sandboxie's default file rule is WRITE-isolated but READ-
// transparent: a boxed program that finds no copy of a file in
// its box layer reads the host's real file. For most paths that
// transparency is a feature; for an app's own data root it
// breaks MJ's core promise — the v0.41 field test showed the
// boxed WeChat opening on the HOST account's one-click login
// ("Jason XU · 进入 WeChat") because it read the host WeChat's
// account store straight through the box wall. A covered app
// therefore gets per-program WriteFilePath rules (Sandboxie's
// "Box Only" access) for its data roots: host-side contents are
// hidden, the folder exists only inside the box, and the app's
// data can only ever grow in Data. WriteFilePath, not
// ClosedFilePath: the official docs draw exactly this line —
// WriteFilePath "closes the host side while permitting the
// sandbox-copy side", ClosedFilePath closes BOTH, so a
// ClosedFilePath-sealed WeChat could never even create its data
// folder and the fresh start the seal exists for would be
// impossible.
//
// The variable spellings are Sandboxie shell-folder variables,
// verified against the official documentation (Expandable
// Variables / Shell Folders): %AppData% is the Roaming folder,
// %Personal% is Documents, and the Local folder's shell-folder
// name is "Local AppData" (with the space). A pattern carrying
// no wildcard gets one appended by Sandboxie, so a trailing
// backslash covers the folder and everything below it.
//
// The registry half of the seal is the same read-through in
// another dress: WeChat decides where its data lives partly
// from values under HKCU\Software\Tencent\WeChat and
// ...\Weixin (FileSavePath and friends), and while the box
// has no shadow of those keys the reads go straight to the
// host's — a custom storage folder set in the host WeChat
// would steer the boxed one outside every sealed file root
// above. Covered apps therefore also get per-program
// WriteKeyPath rules for those key trees: Sandboxie's
// registry twin of WriteFilePath (official UI wording: the
// keys "will appear empty to programs running in this
// sandbox, but the programs may write new data within these
// registry keys in the sandbox"), so the box-side key tree
// reads and writes exactly as before while the host's values
// are hidden. The spelling follows the official Templates.ini
// idiom (OpenKeyPath=iexplore.exe,HKEY_CURRENT_USER\
// Software\...): HKEY_CURRENT_USER is the user-mode prefix
// the driver's config expansion translates to
// \REGISTRY\USER\%SID% (conf_expand.c), so no SID and no %
// variable belongs in the rule itself; and a key pattern is
// written bare — no trailing backslash, no star — because a
// star-less pattern names the whole tree: the key itself,
// its values and everything below it, the same way the
// official templates name HKEY_CURRENT_USER\Software\
// Microsoft\Office to cover a whole key tree in one line.

type sealRootKind int

const (
	sealRootRoaming sealRootKind = iota // %AppData%
	sealRootLocal                       // %Local AppData%
	sealRootDocs                        // %Personal%
)

// sealPath is one covered data root: the ini pattern as
// Sandboxie reads it, plus how the same folder is found inside
// the box's shadow tree when purging leaked copies.
type sealPath struct {
	pattern string // ini form, e.g. `%AppData%\Tencent\Weixin\`
	kind    sealRootKind
	sub     string // below the known folder, e.g. `Tencent\Weixin`
}

// sealWeChatRoots is the WeChat family's data-root union, both
// generations (sources: WeChat 4.x keeps its account/message
// store under Documents\xwechat_files and its client config
// under Roaming\Tencent\xwechat and Roaming\Tencent\Weixin;
// 3.x keeps its store under Documents\WeChat Files and its
// config under Roaming\Tencent\WeChat):
var sealWeChatRoots = []sealPath{
	{pattern: `%Personal%\xwechat_files\`, kind: sealRootDocs, sub: `xwechat_files`},
	{pattern: `%Personal%\WeChat Files\`, kind: sealRootDocs, sub: `WeChat Files`},
	{pattern: `%AppData%\Tencent\xwechat\`, kind: sealRootRoaming, sub: `Tencent\xwechat`},
	{pattern: `%AppData%\Tencent\Weixin\`, kind: sealRootRoaming, sub: `Tencent\Weixin`},
	{pattern: `%AppData%\Tencent\WeChat\`, kind: sealRootRoaming, sub: `Tencent\WeChat`},
}

// sealTemplates maps a main exe's base name to its data roots.
// v1 covers the WeChat family only; both exes get the union of
// both generations' roots (4.x reads the legacy 3.x store while
// migrating, 3.x never touches the new roots, and it is the
// account store that must not leak either way). Extend by
// adding an exe base name with its roots — nothing else in the
// seal pipeline is per-app.
var sealTemplates = map[string][]sealPath{
	"weixin.exe": sealWeChatRoots,
	"wechat.exe": sealWeChatRoots,
}

// sealWeChatKeys is the WeChat family's registry half of the
// seal: the two vendor keys under which both generations keep
// their client settings — including the values that decide
// where the data store lives (see the section comment). Both
// keys, whole-tree, for both exes: the generations read each
// other's leftovers while migrating, and the steering values
// are exactly what must not cross.
var sealWeChatKeys = []string{
	`HKEY_CURRENT_USER\Software\Tencent\WeChat`,
	`HKEY_CURRENT_USER\Software\Tencent\Weixin`,
}

// sealKeyTemplates maps a main exe's base name to its sealed
// registry key roots, paralleling sealTemplates.
var sealKeyTemplates = map[string][]string{
	"weixin.exe": sealWeChatKeys,
	"wechat.exe": sealWeChatKeys,
}

// sealKeyPathsForExe returns the template key roots for an exe
// base name (case-insensitive), or nil when the app has none.
func sealKeyPathsForExe(exeBase string) []string {
	return sealKeyTemplates[strings.ToLower(exeBase)]
}

// sealPathsForExe returns the template roots for an exe base
// name (case-insensitive), or nil when the app is not covered.
func sealPathsForExe(exeBase string) []sealPath {
	return sealTemplates[strings.ToLower(exeBase)]
}

// appSealedByExe reports whether an exe base name is covered
// by a data-seal template (file roots, key roots, or both).
func appSealedByExe(exeBase string) bool {
	return len(sealPathsForExe(exeBase)) > 0 || len(sealKeyPathsForExe(exeBase)) > 0
}

// sealRuleLinesForExe renders the [MJBox] rule lines for ONE
// exe base name (pure): per-program WriteFilePath rules for
// the data roots, then per-program WriteKeyPath rules for the
// registry key roots, each in template order. The program
// prefix keeps the exe's own spelling; Sandboxie matches image
// names case-insensitively, and the rule applies only when the
// sandboxed process image has exactly that base name (the
// documented Program Name Prefix semantics).
func sealRuleLinesForExe(exeBase string) []string {
	paths := sealPathsForExe(exeBase)
	keys := sealKeyPathsForExe(exeBase)
	if len(paths) == 0 && len(keys) == 0 {
		return nil
	}
	out := make([]string, 0, len(paths)+len(keys))
	for _, p := range paths {
		out = append(out, "WriteFilePath="+exeBase+","+p.pattern)
	}
	for _, k := range keys {
		out = append(out, "WriteKeyPath="+exeBase+","+k)
	}
	return out
}

// sealRuleLines renders the MJ-managed rule lines for a whole
// registry (pure): every covered app contributes its lines,
// duplicates collapse (two registrations of one exe share its
// template), and the result is sorted, so the generated
// section is a pure function of the registry — merging twice
// is a no-op, and removing an app removes exactly its lines
// when no other registration still needs them.
func sealRuleLines(reg map[string]boxApp) []string {
	seen := map[string]bool{}
	var out []string
	for _, app := range reg {
		for _, ln := range sealRuleLinesForExe(virtualBase(app.Exe)) {
			if !seen[ln] {
				seen[ln] = true
				out = append(out, ln)
			}
		}
	}
	sort.Strings(out)
	return out
}

// currentSealLines is sealRuleLines over the live registry.
func currentSealLines() []string {
	boxAppsMu.Lock()
	defer boxAppsMu.Unlock()
	return sealRuleLines(loadBoxApps())
}

// sealNeeded reports whether an app still owes its one-time
// purge (pure): covered by a template and not yet sealed.
func sealNeeded(app boxApp) bool {
	return !app.DataSealed && appSealedByExe(virtualBase(app.Exe))
}

// sealVirtualForUser expands one template root to the virtual
// path it has for the given box user (pure).
func sealVirtualForUser(p sealPath, user string) string {
	var base string
	switch p.kind {
	case sealRootRoaming:
		base = `C:\Users\` + user + `\AppData\Roaming`
	case sealRootLocal:
		base = `C:\Users\` + user + `\AppData\Local`
	case sealRootDocs:
		base = `C:\Users\` + user + `\Documents`
	}
	if p.sub == "" {
		return base
	}
	return base + `\` + p.sub
}

// sealShadowPaths returns the physical shadow folders one
// covered exe's template maps to for the given box users
// (pure): one virtualToPhysical per (root, user).
func sealShadowPaths(boxRoot, exeBase string, users []string) []string {
	paths := sealPathsForExe(exeBase)
	if len(paths) == 0 {
		return nil
	}
	var out []string
	for _, u := range users {
		if u == "" {
			continue
		}
		for _, p := range paths {
			if phys := virtualToPhysical(boxRoot, sealVirtualForUser(p, u)); phys != "" {
				out = append(out, phys)
			}
		}
	}
	sort.Strings(out)
	return out
}

// insideBoxStrict reports whether p resolves strictly INSIDE
// boxRoot (pure): the box root itself, anything outside it,
// and prefix-siblings (MJBox2 parked next to MJBox) are all
// refused. The leaked-copy purge deletes only paths that pass
// this check.
func insideBoxStrict(boxRoot, p string) bool {
	rel, err := filepath.Rel(boxRoot, p)
	if err != nil || rel == "." || rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// ---------- virtual <-> physical path conversion ----------
//
// A box rooted at R stores drive X:'s shadow tree at
// R\drive\X\... and the user profile shadow (C:\Users\<name>)
// at R\user\<name>\... -- the two mappings Sandboxie itself uses.

// virtualToPhysical converts a path as the boxed app sees it
// (C:\Program Files\...) into the physical file inside the box.
func virtualToPhysical(boxRoot, vp string) string {
	vp = strings.ReplaceAll(vp, "/", `\`)
	segs := strings.Split(vp, `\`)
	if len(segs) == 0 || len(segs[0]) != 2 || segs[0][1] != ':' {
		return ""
	}
	drive := strings.ToUpper(segs[0][:1])
	// C:\Users\<name>\... lives in the box's user\<name> shadow.
	if drive == "C" && len(segs) >= 3 && strings.EqualFold(segs[1], "Users") && segs[2] != "" {
		p := filepath.Join(boxRoot, "user", segs[2])
		if len(segs) > 3 {
			p = filepath.Join(p, filepath.Join(segs[3:]...))
		}
		return p
	}
	p := filepath.Join(boxRoot, "drive", drive)
	rest := make([]string, 0, len(segs))
	for _, s := range segs[1:] {
		if s != "" {
			rest = append(rest, s)
		}
	}
	if len(rest) > 0 {
		p = filepath.Join(p, filepath.Join(rest...))
	}
	return p
}

// virtualBase returns the last segment of a virtual (Windows)
// path -- filepath.Base cannot be used on virtual paths because
// it honours the HOST separator.
func virtualBase(vp string) string {
	vp = strings.ReplaceAll(vp, "/", `\`)
	if i := strings.LastIndex(vp, `\`); i >= 0 {
		return vp[i+1:]
	}
	return vp
}

// virtualStem is virtualBase without its extension.
func virtualStem(vp string) string {
	b := virtualBase(vp)
	if i := strings.LastIndex(b, "."); i > 0 {
		return b[:i]
	}
	return b
}

// physicalToVirtual is the inverse mapping; "" when the physical
// path is not inside the box's drive/user shadows.
func physicalToVirtual(boxRoot, pp string) string {
	rel, err := filepath.Rel(boxRoot, pp)
	if err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	segs := strings.FieldsFunc(rel, func(r rune) bool { return r == '\\' || r == '/' })
	if len(segs) >= 2 && strings.EqualFold(segs[0], "drive") && len(segs[1]) == 1 {
		vp := strings.ToUpper(segs[1]) + ":"
		if len(segs) > 2 {
			vp += `\` + strings.Join(segs[2:], `\`)
		} else {
			vp += `\`
		}
		return vp
	}
	if len(segs) >= 2 && strings.EqualFold(segs[0], "user") {
		vp := `C:\Users\` + segs[1]
		if len(segs) > 2 {
			vp += `\` + strings.Join(segs[2:], `\`)
		}
		return vp
	}
	return ""
}

// sharedVirtualRoots are the virtual directories that belong to
// every app at once; an exe sitting directly inside one has no
// private install folder to remove.
func isSharedVirtualDir(vp string) bool {
	vp = strings.TrimRight(strings.ReplaceAll(vp, "/", `\`), `\`)
	segs := strings.Split(vp, `\`)
	low := make([]string, len(segs))
	for i, s := range segs {
		low[i] = strings.ToLower(s)
	}
	if len(segs) == 1 { // a drive root, e.g. "C:"
		return true
	}
	if len(segs) == 2 && (low[1] == "program files" || low[1] == "program files (x86)" || low[1] == "programdata" || low[1] == "users") {
		return true
	}
	// C:\Users\<name> and C:\Users\<name>\AppData(\Local) are shared.
	if len(segs) >= 3 && low[1] == "users" {
		if len(segs) == 3 {
			return true
		}
		if len(segs) == 4 && low[3] == "appdata" {
			return true
		}
		if len(segs) == 5 && low[3] == "appdata" && (low[4] == "local" || low[4] == "roaming" || low[4] == "locallow") {
			return true
		}
		if len(segs) == 6 && low[3] == "appdata" && low[4] == "local" && low[5] == "programs" {
			return true
		}
	}
	return false
}

var versionSegRe = regexp.MustCompile(`^\d+(\.\d+)+([.-].*)?$`)

// appInstallDirVirtual computes the virtual folder that belongs to
// the app alone: the exe's own folder, climbed out of leaf folders
// that are clearly plumbing (bin, version numbers), but never up
// into a shared root. "" means the exe sits in a shared root and
// only the file itself is the app's.
func appInstallDirVirtual(vp string) string {
	vp = strings.ReplaceAll(vp, "/", `\`)
	dir := vp
	if i := strings.LastIndex(dir, `\`); i >= 0 {
		dir = dir[:i]
	}
	for {
		if isSharedVirtualDir(dir) {
			return ""
		}
		leaf := dir
		if i := strings.LastIndex(dir, `\`); i >= 0 {
			leaf = dir[i+1:]
		}
		l := strings.ToLower(leaf)
		if l == "bin" || l == "x64" || l == "win64" || versionSegRe.MatchString(leaf) {
			parent := dir[:strings.LastIndex(dir, `\`)]
			if isSharedVirtualDir(parent) {
				return dir // cannot climb without eating a shared root
			}
			dir = parent
			continue
		}
		return dir
	}
}

// ---------- boxed-app registry (Data/sandbox/apps.json) ----------

// boxApp is one registered boxed application. Exe is the VIRTUAL
// path (what the app itself sees); InstallDir/AppDataDirs are
// VIRTUAL paths too, converted to physical at use time -- the
// registry therefore survives the whole folder moving to another
// PC (the box root is re-derived from the current Data location).
type boxApp struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Exe         string   `json:"exe"`                  // virtual main exe path
	InstallDir  string   `json:"installDir,omitempty"` // virtual, "" = exe file only
	AppDataDirs []string `json:"appDataDirs,omitempty"`
	Installer   string   `json:"installer,omitempty"` // installer file the app came from
	At          string   `json:"at"`
	// DataSealed records that the v0.42 one-time purge ran for
	// this app: the shadow copies its data-seal template covers
	// were deleted once, so its next start was a true first run.
	// Meaningful only for template-covered apps (see sealNeeded);
	// absent/false for everything else.
	DataSealed bool `json:"dataSealed,omitempty"`
}

var boxAppsMu sync.Mutex

func loadBoxApps() map[string]boxApp {
	out := map[string]boxApp{}
	b, err := os.ReadFile(sandboxAppsPath())
	if err != nil {
		return out
	}
	_ = json.Unmarshal(b, &out)
	return out
}

func saveBoxApps(reg map[string]boxApp) {
	_ = os.MkdirAll(sandboxDir(), 0o755)
	b, err := json.MarshalIndent(reg, "", " ")
	if err != nil {
		return
	}
	tmp := sandboxAppsPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err == nil {
		_ = os.Rename(tmp, sandboxAppsPath())
	}
}

// boxAppsUpsert inserts or replaces one entry (pure).
func boxAppsUpsert(reg map[string]boxApp, app boxApp) map[string]boxApp {
	if reg == nil {
		reg = map[string]boxApp{}
	}
	reg[app.ID] = app
	return reg
}

// boxAppsRemove deletes one entry (pure).
func boxAppsRemove(reg map[string]boxApp, id string) map[string]boxApp {
	delete(reg, id)
	return reg
}

// newBoxAppID derives a stable, collision-free id from a base name
// (pure).
func newBoxAppID(reg map[string]boxApp, base string) string {
	slug := strings.ToLower(base)
	var b strings.Builder
	for _, r := range slug {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteRune('-')
		}
	}
	slug = strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "app"
	}
	id := slug
	for i := 2; ; i++ {
		if _, taken := reg[id]; !taken {
			return id
		}
		id = slug + "-" + strconv.Itoa(i)
	}
}

// sortedBoxApps returns the registry as a list ordered by install
// time then name, for stable UI rendering.
func sortedBoxApps(reg map[string]boxApp) []boxApp {
	out := make([]boxApp, 0, len(reg))
	for _, a := range reg {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].At != out[j].At {
			return out[i].At < out[j].At
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// ---------- box content scanning / candidate ranking ----------

type boxExeCand struct {
	Virtual  string `json:"virtual"`
	Physical string `json:"-"`
	Size     int64  `json:"size"`
	// Sealed marks a candidate whose exe is covered by a
	// data-seal template (v0.42): the registration dialog uses
	// it to promise the fresh-data behavior up front.
	Sealed bool `json:"sealed,omitempty"`
	// Exists is the liveness verdict (v0.43): recomputed every
	// time the candidate list is served, because a candidate's
	// file can vanish while the user is still deciding (it may
	// have been another program's temporary drop all along).
	// The stored value is never trusted; sandboxStatusMap
	// re-stats the physical file on every read.
	Exists bool `json:"exists"`
	// Foreign marks a candidate that plausibly does NOT belong
	// to this install (v0.43): its app folder differs from the
	// top candidate's and its name does not resemble the
	// installer's. The dialog flags it instead of hiding it --
	// the user makes the final call.
	Foreign bool `json:"foreign,omitempty"`
	// Existing marks a candidate that did NOT appear during
	// this install but was already lying in the box (v0.44):
	// a same-version reinstall writes nothing at all (Inno
	// skips files whose version already matches), so the diff
	// comes back empty even though the program is right there.
	// Such candidates come from the name-affinity fallback
	// (existingBoxCandidates) and are labelled 「盒内已有」 in
	// the registration dialog.
	Existing bool `json:"existing,omitempty"`
}

// snapshotBoxExes walks the box and lists every .exe with its
// virtual path and size.
func snapshotBoxExes(boxRoot string) []boxExeCand {
	var out []boxExeCand
	_ = filepath.Walk(boxRoot, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(p), ".exe") {
			return nil
		}
		vp := physicalToVirtual(boxRoot, p)
		if vp == "" {
			return nil
		}
		out = append(out, boxExeCand{Virtual: vp, Physical: p, Size: info.Size()})
		return nil
	})
	return out
}

func exeSet(cands []boxExeCand) map[string]bool {
	s := map[string]bool{}
	for _, c := range cands {
		s[strings.ToLower(c.Physical)] = true
	}
	return s
}

// snapshotAppDataDirs lists depth-1 and depth-2 directories under
// every boxed user's AppData roots (physical paths).
func snapshotAppDataDirs(boxRoot string) []string {
	var out []string
	usersRoot := filepath.Join(boxRoot, "user")
	users, _ := os.ReadDir(usersRoot)
	for _, u := range users {
		if !u.IsDir() {
			continue
		}
		for _, root := range []string{"Roaming", "Local", "LocalLow"} {
			base := filepath.Join(usersRoot, u.Name(), "AppData", root)
			l1, _ := os.ReadDir(base)
			for _, d1 := range l1 {
				if !d1.IsDir() {
					continue
				}
				p1 := filepath.Join(base, d1.Name())
				out = append(out, p1)
				l2, _ := os.ReadDir(p1)
				for _, d2 := range l2 {
					if d2.IsDir() {
						out = append(out, filepath.Join(p1, d2.Name()))
					}
				}
			}
		}
	}
	return out
}

// matchNewAppDataDirs picks, among the directories that appeared
// during an install, the ones that plausibly belong to the app:
// any path segment containing one of the hints (install folder
// name, app name, main exe stem). Ancestors of a matched directory
// are dropped so a removal never double-counts (pure).
func matchNewAppDataDirs(after []string, before map[string]bool, hints []string) []string {
	var hs []string
	for _, h := range hints {
		if len(h) >= 2 {
			hs = append(hs, strings.ToLower(h))
		}
	}
	var matched []string
	for _, p := range after {
		if before[strings.ToLower(p)] {
			continue
		}
		segs := strings.FieldsFunc(strings.ToLower(p), func(r rune) bool { return r == '\\' || r == '/' })
		hit := false
		for _, s := range segs {
			for _, h := range hs {
				if strings.Contains(s, h) {
					hit = true
					break
				}
			}
			if hit {
				break
			}
		}
		if hit {
			matched = append(matched, p)
		}
	}
	// Drop entries that are descendants of another matched entry.
	var out []string
	for _, p := range matched {
		desc := false
		for _, q := range matched {
			if p != q && strings.HasPrefix(strings.ToLower(p), strings.ToLower(q)+`\`) {
				desc = true
				break
			}
		}
		if !desc {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// ---------- v0.43: candidate trust (junk shapes, foreign marks) ----------
//
// The install diff answers "which exes are NEW in the box", not
// "which exes belong to the program being installed". The box
// is shared: while Telegram installed (field 2026-10-29
// 13:35-13:40), the boxed WeChat was alive too, and its patcher
// briefly extracted a 7z.exe under
// …\Tencent\xwechat\xplugin\plugins\CourgettePatch\20\extracted\.
// The diff swept that file into the Telegram candidates; the
// user registered it as "Telegram"; the patcher then deleted
// its temp file, and the desktop icon died with Sandboxie's raw
// "cannot invoke program … file not found (2)". Three defences,
// all by SHAPE (never a hardcoded vendor path):
//
//	junk shapes -- generic small tools (7z family, unrar,
//	unzip) are parts of other programs, never an installed
//	app's main program; and no main program lives under a
//	plugin/temp path segment (plugins, extracted, temp, …).
//	Both are excluded from candidacy outright.
//
//	foreign marks -- a surviving candidate whose app folder
//	has nothing to do with the top candidate's and whose name
//	does not resemble the installer's is flagged, not hidden.
//
//	liveness -- every candidate's physical file is re-statted
//	when the list is generated, on every status read while
//	the dialog is open, and again at registration time. A
//	registration pointing at a nonexistent file is the one
//	accident that must never happen again.

var junkToolStems = map[string]bool{
	"7z": true, "7za": true, "7zr": true, "7zfm": true,
	"unrar": true, "unzip": true,
}

var junkPathSegments = map[string]bool{
	"extracted": true, "xplugin": true, "plugins": true,
	"plugin": true, "temp": true, "tmp": true,
}

// isJunkCandidateShape reports whether a virtual path has the
// shape of a tool/plugin/temp drop rather than an installed
// application's main program (pure). Segment comparison is by
// whole segment, so a legit "Templates" folder is safe.
func isJunkCandidateShape(virtual string) bool {
	if junkToolStems[strings.ToLower(virtualStem(virtual))] {
		return true
	}
	for _, seg := range strings.FieldsFunc(virtual, func(r rune) bool { return r == '\\' || r == '/' }) {
		if junkPathSegments[strings.ToLower(seg)] {
			return true
		}
	}
	return false
}

// candidateAppFolder names the folder a candidate belongs to:
// the segment right below its install root (Program Files,
// ProgramData, AppData\Roaming, AppData\Local), lowercased
// (pure).
func candidateAppFolder(virtual string) string {
	segs := strings.FieldsFunc(virtual, func(r rune) bool { return r == '\\' || r == '/' })
	for i, s := range segs {
		switch strings.ToLower(s) {
		case "program files", "program files (x86)", "programdata", "roaming", "local":
			if i+1 < len(segs) {
				return strings.ToLower(segs[i+1])
			}
		}
	}
	if len(segs) >= 3 {
		return strings.ToLower(segs[2])
	}
	return ""
}

// markForeignCandidates flags ranked candidates (all but the
// top one) whose app folder differs from the top candidate's
// AND whose name does not resemble the installer's (pure).
func markForeignCandidates(ranked []boxExeCand, installerBase string) {
	if len(ranked) < 2 {
		return
	}
	top := candidateAppFolder(ranked[0].Virtual)
	if top == "" {
		return
	}
	hint := installerHint(installerBase)
	for i := 1; i < len(ranked); i++ {
		stem := strings.ToLower(virtualStem(ranked[i].Virtual))
		resembles := hint != "" && len(stem) >= 3 && (strings.Contains(stem, hint) || strings.Contains(hint, stem))
		ranked[i].Foreign = candidateAppFolder(ranked[i].Virtual) != top && !resembles
	}
}

// candidateExists stats a candidate's physical shadow file.
func candidateExists(c boxExeCand) bool {
	if c.Physical == "" {
		return false
	}
	_, err := os.Stat(c.Physical)
	return err == nil
}

// rankBoxCandidates orders the exes an install left behind so the
// most likely MAIN program comes first (pure). Uninstallers and
// junk shapes (tools, plugin/temp drops) are excluded outright;
// setup/update/helper-looking names sink; a Program Files or
// per-user AppData location, a parent folder resembling the exe,
// and a name resembling the installer's float to the top.
func rankBoxCandidates(cands []boxExeCand, installerBase string) []boxExeCand {
	hint := installerHint(installerBase)
	instStem := strings.ToLower(virtualStem(installerBase))
	score := func(c boxExeCand) int {
		vp := strings.ReplaceAll(c.Virtual, "/", `\`)
		segs := strings.Split(vp, `\`)
		stem := strings.ToLower(virtualStem(c.Virtual))
		s := 0
		low := strings.ToLower(vp)
		switch {
		case strings.Contains(low, `\program files\`) || strings.Contains(low, `\program files (x86)\`):
			s += 120
		case strings.Contains(low, `\appdata\roaming\`):
			// Per-user installs (Telegram Desktop's home):
			// a first-class install root since v0.43.
			s += 90
		case strings.Contains(low, `\appdata\local\programs\`):
			s += 70
		case strings.Contains(low, `\appdata\local\`):
			s += 60
		case strings.Contains(low, `\programdata\`):
			s += 40
		}
		if hint != "" && len(stem) >= 3 && (strings.Contains(stem, hint) || strings.Contains(hint, stem)) {
			s += 50
		}
		if len(segs) >= 2 {
			// The main program usually sits in a folder named
			// after itself (Telegram.exe in "Telegram Desktop",
			// Weixin.exe in "Weixin").
			parent := strings.ToLower(segs[len(segs)-2])
			if stem != "" && (strings.Contains(parent, stem) || strings.Contains(stem, parent)) {
				s += 40
			}
		}
		if instStem != "" && stem == instStem {
			// The installer's own file copied into the box is
			// residue, never the product.
			s -= 200
		}
		for _, bad := range []string{"setup", "install", "update", "helper", "crash", "report", "service", "daemon", "repair"} {
			if strings.Contains(stem, bad) {
				s -= 60
				break
			}
		}
		if len(segs) > 5 {
			s -= (len(segs) - 5) * 5
		}
		if mb := int(c.Size / (10 * 1024 * 1024)); mb > 0 {
			if mb > 10 {
				mb = 10
			}
			s += mb
		}
		return s
	}
	out := make([]boxExeCand, 0, len(cands))
	for _, c := range cands {
		stem := strings.ToLower(virtualBase(c.Virtual))
		if strings.HasPrefix(stem, "unins") || strings.Contains(stem, "uninstall") {
			continue
		}
		if isJunkCandidateShape(c.Virtual) {
			continue
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		si, sj := score(out[i]), score(out[j])
		if si != sj {
			return si > sj
		}
		if len(out[i].Virtual) != len(out[j].Virtual) {
			return len(out[i].Virtual) < len(out[j].Virtual)
		}
		return out[i].Virtual < out[j].Virtual
	})
	return out
}

// installerHint reduces an installer file name ("WeChatWin_4.2.0",
// "npp.8.9.8.1.Installer.x64") to the token most likely shared with
// the main exe's name.
func installerHint(fileName string) string {
	s := strings.ToLower(strings.TrimSuffix(fileName, filepath.Ext(fileName)))
	for _, junk := range []string{"setup", "installer", "install", "win64", "win32", "x64", "x86", "universal", "win"} {
		s = strings.ReplaceAll(s, junk, "")
	}
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ---------- v0.44: empty-diff fallback (already-in-box) ----------
//
// Field, 2026-10-29 14:09: the user re-ran the very same
// Telegram installer through the isolated-install flow. The
// v0.43 machinery worked -- session exemption, heartbeats, the
// composite completion rule closed in 37 s -- and still ended
// in the named failure "no new program found": Telegram had
// been lying in the box since the v0.42 install (it was in
// that install's candidate list; the user had merely
// registered the wrong entry). A same-version reinstall makes
// Inno skip every identical file, so the installer ran
// cleanly and wrote NOTHING. An empty diff is not a failed
// install -- it is "already installed". The diff was the only
// candidate source, so the user was stuck: installed, with no
// way to register.
//
// The fallback below runs when the diff yields no registrable
// candidate: scan the whole box, run the standard ranking
// pipeline (uninstallers and junk shapes are stripped there),
// then keep ONLY the exes with a real name affinity to THIS
// installer. Below-threshold strangers are never offered --
// failing honestly beats registering somebody else's program
// under this install's name.

// installerNoiseTokens are the words in an installer file name
// that say nothing about the product.
var installerNoiseTokens = map[string]bool{
	"setup": true, "install": true, "installer": true,
	"win": true, "win32": true, "win64": true, "windows": true,
	"x64": true, "x86": true, "amd64": true, "arm64": true,
	"universal": true, "portable": true, "full": true,
	"free": true, "latest": true, "release": true, "final": true,
}

// splitAlphaNumParts cuts a segment at letter<->digit
// boundaries ("win64" -> "win","64"; "mp3tag" -> "mp","3","tag").
func splitAlphaNumParts(seg string) []string {
	var out []string
	start := 0
	isDig := func(c byte) bool { return c >= '0' && c <= '9' }
	for i := 1; i < len(seg); i++ {
		if isDig(seg[i]) != isDig(seg[i-1]) {
			out = append(out, seg[start:i])
			start = i
		}
	}
	return append(out, seg[start:])
}

// installerAffinityTokens extracts the product tokens from an
// installer file name (pure): split on non-alphanumerics and
// letter/digit boundaries, drop noise words (exact or embedded,
// the way installerHint strips "win" out of "wechatwin"), pure
// digit runs (version numbers) and one-letter scraps. What is
// left names the product: "td-setup-win-x64-7.3.3" -> ["td"].
func installerAffinityTokens(fileName string) []string {
	base := strings.ToLower(virtualStem(fileName))
	segs := strings.FieldsFunc(base, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
	seen := map[string]bool{}
	var out []string
	for _, seg := range segs {
		for _, part := range splitAlphaNumParts(seg) {
			digits := true
			for i := 0; i < len(part); i++ {
				if part[i] < '0' || part[i] > '9' {
					digits = false
					break
				}
			}
			if digits || installerNoiseTokens[part] {
				continue
			}
			cleaned := part
			for noise := range installerNoiseTokens {
				cleaned = strings.ReplaceAll(cleaned, noise, "")
			}
			if len(cleaned) < 2 || seen[cleaned] {
				continue
			}
			seen[cleaned] = true
			out = append(out, cleaned)
		}
	}
	return out
}

// virtualParentName is the immediate parent folder of a
// virtual path ("Telegram Desktop" for
// C:\Users\u\...\Telegram Desktop\Telegram.exe), lowercased.
func virtualParentName(virtual string) string {
	segs := strings.FieldsFunc(strings.ReplaceAll(virtual, "/", `\`), func(r rune) bool { return r == '\\' })
	if len(segs) < 2 {
		return ""
	}
	return strings.ToLower(segs[len(segs)-2])
}

func folderInitials(name string) string {
	var b strings.Builder
	for _, w := range strings.FieldsFunc(name, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		b.WriteString(w[:1])
	}
	return b.String()
}

// candAffinityScore scores how strongly one boxed exe is
// likely the program an installer names (pure). Strong signals
// only; anything weaker scores 0 and stays out:
//
//   - a product token (>=3 chars) appears in the exe stem, the
//     parent folder or the app folder -- or contains one of
//     them ("wechatwin" cleaned to "wechat" vs a "wechat" exe);
//   - a 2+ char token IS the exe stem;
//   - a token equals the initials of the app folder's words --
//     installers love acronyms: "td" is Telegram Desktop.
func candAffinityScore(tokens []string, c boxExeCand) int {
	stem := strings.ToLower(virtualStem(c.Virtual))
	idents := []string{stem, virtualParentName(c.Virtual), candidateAppFolder(c.Virtual)}
	best := 0
	for _, tok := range tokens {
		s := 0
		switch {
		case len(tok) >= 3:
			for _, id := range idents {
				if id != "" && len(id) >= 3 && (strings.Contains(id, tok) || strings.Contains(tok, id)) {
					s = 60
					break
				}
			}
		case len(tok) == 2 && stem == tok:
			s = 55
		}
		if s == 0 {
			for _, name := range []string{virtualParentName(c.Virtual), candidateAppFolder(c.Virtual)} {
				if name != "" && folderInitials(name) == tok {
					s = 50
					break
				}
			}
		}
		if s > best {
			best = s
		}
	}
	return best
}

// existingBoxCandidates is the empty-diff fallback (v0.44):
// the whole box through the standard ranking pipeline, then
// the affinity gate. Survivors are marked Existing and ordered
// by affinity first (pipeline order breaks ties).
func existingBoxCandidates(boxRoot, installerName string) []boxExeCand {
	tokens := installerAffinityTokens(installerName)
	if len(tokens) == 0 {
		return nil
	}
	type scored struct {
		c boxExeCand
		s int
	}
	var hits []scored
	for _, c := range rankBoxCandidates(snapshotBoxExes(boxRoot), installerName) {
		if s := candAffinityScore(tokens, c); s > 0 {
			c.Existing = true
			hits = append(hits, scored{c, s})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].s > hits[j].s })
	out := make([]boxExeCand, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.c)
	}
	return out
}

// ---------- installer kind sniffing / silent args ----------

// installerKind is "nsis", "inno", "msi" or "" (unknown). The kind
// decides whether the boxed install can run silently.
func sniffInstallerKind(path string) string {
	if strings.EqualFold(filepath.Ext(path), ".msi") {
		return "msi"
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	return sniffInstallerKindFrom(f)
}

var (
	sbieInnoMarkers = [][]byte{
		[]byte("Inno Setup"),
		utf16Bytes("Inno Setup"),
	}
	sbieNSISMarkers = [][]byte{
		[]byte("NullsoftInst"),
		[]byte("Nullsoft Install System"),
		utf16Bytes("Nullsoft"),
	}
)

func utf16Bytes(s string) []byte {
	out := make([]byte, 0, len(s)*2)
	for _, r := range s {
		out = append(out, byte(r), byte(r>>8))
	}
	return out
}

// sniffInstallerKindFrom streams a reader looking for the Inno
// Setup / NSIS marker strings (pure, testable).
func sniffInstallerKindFrom(r io.Reader) string {
	const chunk = 1 << 20
	const overlap = 64 // every marker is far shorter than this
	var tail []byte
	buf := make([]byte, chunk)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			data := make([]byte, 0, len(tail)+n)
			data = append(data, tail...)
			data = append(data, buf[:n]...)
			for _, m := range sbieInnoMarkers {
				if bytes.Contains(data, m) {
					return "inno"
				}
			}
			for _, m := range sbieNSISMarkers {
				if bytes.Contains(data, m) {
					return "nsis"
				}
			}
			if len(data) > overlap {
				tail = append([]byte{}, data[len(data)-overlap:]...)
			} else {
				tail = data
			}
		}
		if err != nil {
			return ""
		}
	}
}

// silentArgsFor returns the silent-install argument set for a
// sniffed kind; "" / msi / unknown kinds return nil, meaning the
// installer is launched interactively inside the box and the user
// clicks through it there (still fully boxed).
func silentArgsFor(kind string) []string {
	switch kind {
	case "nsis":
		return []string{"/S"}
	case "inno":
		return []string{"/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART", "/SP-"}
	}
	return nil
}

// parseBoxPids parses Start.exe /listpids output (pure).
//
// The documented format (Sandboxie "Start Command Line"
// reference): the FIRST line is the NUMBER of programs, then
// one PID per line:
//
//	3
//	3036
//	2136
//	384
//
// v0.42 and earlier split the output on any non-digit and so
// ingested the count itself as a PID -- the v0.40 field list
// literally began with "17" (seventeen programs), and a count
// that happens to equal a host PID makes the patrol claim a
// host window "by box list" (field 2026-10-29: an elevated
// host powershell claimed during a Telegram install). The
// parser is line-strict now: only lines consisting entirely of
// digits are numbers at all; the first number is the count
// header and is dropped whenever it does not exceed the number
// of remaining lines (a truncated list loses entries, it never
// gains fake ones); implausible values (under 8 -- no boxed
// user program can own one) are dropped as well.
func parseBoxPids(out string) []int {
	var nums []int
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		digits := true
		for _, r := range line {
			if r < '0' || r > '9' {
				digits = false
				break
			}
		}
		if !digits {
			continue
		}
		if n, err := strconv.Atoi(line); err == nil && n > 0 {
			nums = append(nums, n)
		}
	}
	if len(nums) >= 2 && nums[0] <= len(nums)-1 {
		nums = nums[1:] // the program-count header line
	}
	seen := map[int]bool{}
	var pids []int
	for _, n := range nums {
		if n < 8 || seen[n] {
			continue
		}
		seen[n] = true
		pids = append(pids, n)
	}
	sort.Ints(pids)
	return pids
}

// ---------- component package acquisition ----------

// componentPlan decides where the component installer comes from
// (pure): a previously extracted copy that still verifies is
// reused; else the embedded package is extracted; the online
// download is only the fallback.
func componentPlan(extractedVerifies, embeddedVerifies bool) string {
	if extractedVerifies {
		return "reuse"
	}
	if embeddedVerifies {
		return "extract"
	}
	return "download"
}

// verifyPinnedFile checks a file's size and SHA-256 against the pin.
func verifyPinnedFile(path string, wantSize int64, wantSHA string) bool {
	st, err := os.Stat(path)
	if err != nil || st.Size() != wantSize {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false
	}
	return strings.EqualFold(hex.EncodeToString(h.Sum(nil)), wantSHA)
}

var sbieEmbeddedOK struct {
	sync.Once
	ok bool
}

func embeddedVerifies() bool {
	sbieEmbeddedOK.Once.Do(func() {
		if int64(len(sbieEmbedded)) != sbieSize {
			return
		}
		sum := sha256.Sum256(sbieEmbedded)
		sbieEmbeddedOK.ok = strings.EqualFold(hex.EncodeToString(sum[:]), sbieSHA256)
	})
	return sbieEmbeddedOK.ok
}

// ---------- operation state (polled by the settings/store pages) ----------

var sbieComp = struct {
	sync.Mutex
	Phase    string // idle|preparing|installing|done|error
	Message  string
	Source   string // embedded|download (last successful acquisition)
	Progress int64
	Size     int64
}{Phase: "idle"}

func sbieCompSet(phase, msg string) {
	sbieComp.Lock()
	sbieComp.Phase, sbieComp.Message = phase, msg
	sbieComp.Unlock()
	logf("sandbox", "info", "组件：%s（%s）", msg, phase)
}

func sbieCompSnapshot() (phase, msg, source string, progress, size int64) {
	sbieComp.Lock()
	defer sbieComp.Unlock()
	return sbieComp.Phase, sbieComp.Message, sbieComp.Source, sbieComp.Progress, sbieComp.Size
}

func sbieCompBusy() bool {
	p, _, _, _, _ := sbieCompSnapshot()
	return p == "preparing" || p == "installing"
}

type sbiePendingInstall struct {
	InstallerName string
	BeforeExes    map[string]bool
	BeforeAppData map[string]bool
	// FromExisting records that the candidates came from the
	// empty-diff fallback (v0.44): the program was already in
	// the box, this install added nothing.
	FromExisting bool
}

var sbieBoxOp = struct {
	sync.Mutex
	Phase      string // idle|preparing|downloading|installing|scanning|choosing|done|error
	Message    string
	Kind       string
	Installer  string
	Progress   int64
	Size       int64
	StartedAt  int64 // unix ms when the current busy run began (v0.43: the store UI shows elapsed time)
	Candidates []boxExeCand
	Pending    *sbiePendingInstall
}{Phase: "idle"}

func sbieBoxSet(phase, msg string) {
	sbieBoxOp.Lock()
	sbieBoxOp.Phase, sbieBoxOp.Message = phase, msg
	switch phase {
	case "preparing", "downloading", "installing", "scanning":
		if sbieBoxOp.StartedAt == 0 {
			sbieBoxOp.StartedAt = time.Now().UnixMilli()
		}
	case "idle":
		sbieBoxOp.StartedAt = 0
	}
	sbieBoxOp.Unlock()
}

func sbieBoxBusy() bool {
	sbieBoxOp.Lock()
	defer sbieBoxOp.Unlock()
	return sbieBoxOp.Phase == "preparing" || sbieBoxOp.Phase == "downloading" || sbieBoxOp.Phase == "installing" || sbieBoxOp.Phase == "scanning"
}

var sbieBoxCancel atomic.Bool

// ---------- component metadata (install source, for the status card) ----------

type sbieMeta struct {
	Version string `json:"version"`
	Source  string `json:"source"` // embedded|download
	At      string `json:"at"`
}

func readComponentMeta() sbieMeta {
	var m sbieMeta
	b, err := os.ReadFile(componentMetaPath())
	if err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

func writeComponentMeta(source string) {
	b, _ := json.MarshalIndent(sbieMeta{
		Version: sbieVersion, Source: source,
		At: time.Now().Format("2006-01-02 15:04:05"),
	}, "", " ")
	tmp := componentMetaPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err == nil {
		_ = os.Rename(tmp, componentMetaPath())
	}
}

// ---------- box placement (Sandboxie.ini [MJBox]) ----------

// ensureBoxPlaced writes the [MJBox] section for the CURRENT Data
// location, in-process. Returns (true, false) when the ini is
// current afterwards; (false, true) when the write needs elevation
// (C:\Windows is admin-only for a non-elevated daemon).
func ensureBoxPlaced() (bool, bool) {
	iniPath := sbieIniPath()
	raw, rerr := os.ReadFile(iniPath)
	text, utf16enc := "", true
	if rerr == nil {
		text, utf16enc = decodeSbieIni(raw)
	} else if !os.IsNotExist(rerr) {
		return false, true
	}
	lines := currentSealLines()
	if iniHasCurrentBox(text, sandboxBoxRoot(), lines) {
		return true, false
	}
	merged, _ := mergeBoxSection(text, sandboxBoxRoot(), lines)
	if err := os.WriteFile(iniPath, encodeSbieIni(merged, utf16enc), 0o644); err != nil {
		return false, true
	}
	sbieReload()
	return true, false
}

// iniPlacedCurrent reports, read-only, whether the ini already
// points MJBox at the current box root.
func iniPlacedCurrent() bool {
	raw, err := os.ReadFile(sbieIniPath())
	if err != nil {
		return false
	}
	text, _ := decodeSbieIni(raw)
	return iniHasCurrentBox(text, sandboxBoxRoot(), currentSealLines())
}

// placeBox is the full placement used by install/repair/register
// flows: direct write first, the elevated self-helper (one UAC)
// when the direct write is refused.
func placeBox() bool {
	placed, needElev := ensureBoxPlaced()
	if placed {
		return true
	}
	if !needElev {
		return false
	}
	self, err := os.Executable()
	if err != nil {
		return false
	}
	code, err := sbieRunElevated(self, "-sbie-ini", sbieIniPath(), sandboxBoxRoot())
	if err != nil {
		logf("sandbox", "warn", "盒子落位提权失败：%v", err)
		return false
	}
	if code != 0 {
		logf("sandbox", "warn", "盒子落位提权返回 %d", code)
		return false
	}
	sbieReload()
	ok := iniPlacedCurrent()
	if ok {
		logf("sandbox", "info", "盒子已落位到当前 Data：%s", sandboxBoxRoot())
	}
	return ok
}

// ---------- elevated self-helper (invoked by main before flags) ----------
//
// The daemon normally runs unprivileged. Three operations need an
// administrator token exactly once: writing C:\Windows\Sandboxie.ini,
// running the component installer/uninstaller (they demand it in
// their manifests), and starting the Sandboxie service. Instead of
// asking the user to run MJ as admin, the daemon re-launches ITSELF
// elevated (ShellExecute "runas" -> one UAC prompt) in one of these
// narrow modes; the helper does its one job and exits. The modes
// take explicit paths only and never touch anything else.

// sbieHelperExitCode runs a helper mode; handled=false means the
// args were not a helper invocation at all.
func sbieHelperExitCode(args []string) (int, bool) {
	if len(args) == 0 {
		return 0, false
	}
	switch args[0] {
	case "-sbie-ini": // <iniPath> <boxRoot>
		if len(args) != 3 {
			return 2, true
		}
		return helperWriteIni(args[1], args[2]), true
	case "-sbie-setup": // <pkgPath> <iniPath> <boxRoot>
		if len(args) != 4 {
			return 2, true
		}
		if code := helperRunWait(args[1], sbieInstallArgs...); code != 0 {
			return code, true
		}
		if helperWriteIni(args[2], args[3]) != 0 {
			return 90, true
		}
		return 0, true
	case "-sbie-uninstall": // <uninsPath>
		if len(args) != 2 {
			return 2, true
		}
		return helperRunWait(args[1], sbieUninstallArgs...), true
	case "-sbie-startsvc":
		sc := filepath.Join(os.Getenv("SystemRoot"), "System32", "sc.exe")
		helperRunWait(sc, "start", "SbieDrv")
		return helperRunWait(sc, "start", "SbieSvc"), true
	}
	return 0, false
}

func helperWriteIni(iniPath, boxRoot string) int {
	raw, _ := os.ReadFile(iniPath)
	text, utf16enc := decodeSbieIni(raw)
	if len(raw) == 0 {
		utf16enc = true // a fresh ini gets Sandboxie's native encoding
	}
	merged, _ := mergeBoxSection(text, boxRoot, currentSealLines())
	_ = os.MkdirAll(filepath.Dir(iniPath), 0o755)
	if err := os.WriteFile(iniPath, encodeSbieIni(merged, utf16enc), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "sbie-ini:", err)
		return 1
	}
	return 0
}

// helperRunWait runs exe with args and waits, returning its exit
// code (or 1 when it cannot even start).
func helperRunWait(exe string, args ...string) int {
	cmd := exec.Command(exe, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "sbie-helper:", err)
		return 1
	}
	return 0
}

// ---------- Start.exe wrappers ----------

func sbieRun(args ...string) (string, error) {
	se := sbieStartExePath()
	if se == "" {
		return "", fmt.Errorf("应用隔离组件未安装")
	}
	cmd := exec.Command(se, args...)
	hideConsole(cmd)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func sbieReload() {
	if _, err := sbieRun("/reload"); err != nil {
		logf("sandbox", "warn", "Sandboxie 配置重载失败（服务会自动感知 ini 变化）：%v", err)
	}
}

// sbieBoxPidsE is sbieBoxPids with the error kept: callers that
// make identity decisions from the list (v0.40 window claiming)
// need to tell "box is empty" apart from "the list could not be
// read".
func sbieBoxPidsE() ([]int, error) {
	out, err := sbieRun("/box:"+sbieBoxName, "/listpids")
	if err != nil {
		return nil, err
	}
	return parseBoxPids(out), nil
}

func sbieBoxPids() []int {
	pids, _ := sbieBoxPidsE()
	return pids
}

func sbieTerminateBox() {
	_, _ = sbieRun("/box:"+sbieBoxName, "/terminate")
}

func waitBoxEmpty(d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if len(sbieBoxPids()) == 0 {
			return true
		}
		time.Sleep(2 * time.Second)
	}
	return len(sbieBoxPids()) == 0
}

// waitBoxPidsGone polls until none of the given PIDs remains on
// the box list (v0.41: after the residual sweep kills wedged
// same-name processes, the fresh Start.exe hand-off should not
// race their teardown). Best-effort: a failed list read just
// costs one poll interval, and the deadline caps the wait.
func waitBoxPidsGone(set map[uint32]bool, d time.Duration) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if pids, err := sbieBoxPidsE(); err == nil && !pidSetsOverlap(pidSetFromInts(pids), set) {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// ---------- component package acquisition ----------

// acquireComponentPackage returns (path, source, error): the
// verified installer on disk plus where it came from ("embedded"
// for the in-EXE package -- including a previously extracted copy
// of it -- or "download").
func acquireComponentPackage() (string, string, error) {
	dst := componentPkgPath()
	plan := componentPlan(verifyPinnedFile(dst, sbieSize, sbieSHA256), embeddedVerifies())
	switch plan {
	case "reuse":
		src := readComponentMeta().Source
		if src == "" {
			src = "embedded"
		}
		logf("sandbox", "info", "组件安装包：复用已释放的包（校验通过）")
		return dst, src, nil
	case "extract":
		tmp := dst + ".tmp"
		if err := os.WriteFile(tmp, sbieEmbedded, 0o644); err != nil {
			logf("sandbox", "warn", "内置组件包释放失败，转在线下载：%v", err)
			break // fall through to download
		}
		if !verifyPinnedFile(tmp, sbieSize, sbieSHA256) {
			_ = os.Remove(tmp)
			logf("sandbox", "warn", "内置组件包释放后校验不过，转在线下载")
			break
		}
		if err := os.Rename(tmp, dst); err != nil {
			_ = os.Remove(tmp)
			return "", "", fmt.Errorf("组件包落位失败：%w", err)
		}
		logf("sandbox", "info", "组件安装包：已从程序内置包释放到 %s（校验通过）", dst)
		return dst, "embedded", nil
	}
	// download fallback
	ensureCoreForDownload()
	sbieComp.Lock()
	sbieComp.Size = sbieSize
	sbieComp.Progress = 0
	sbieComp.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	resp, err := dlGet(ctx, sbieURL, "MJ-Terminal-Sandbox/"+AppVersion)
	if err != nil {
		return "", "", fmt.Errorf("下载组件安装包失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("下载组件安装包失败：服务器返回 %d", resp.StatusCode)
	}
	part := dst + ".part"
	f, err := os.Create(part)
	if err != nil {
		return "", "", err
	}
	h := sha256.New()
	buf := make([]byte, 256*1024)
	var written int64
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				_ = f.Close()
				_ = os.Remove(part)
				return "", "", werr
			}
			_, _ = h.Write(buf[:n])
			written += int64(n)
			sbieComp.Lock()
			sbieComp.Progress = written
			sbieComp.Unlock()
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			_ = f.Close()
			_ = os.Remove(part)
			return "", "", rerr
		}
	}
	_ = f.Close()
	if written != sbieSize || !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), sbieSHA256) {
		_ = os.Remove(part)
		return "", "", fmt.Errorf("下载的组件包校验不过（大小 %d，期望 %d），已删掉", written, sbieSize)
	}
	if err := os.Rename(part, dst); err != nil {
		return "", "", err
	}
	logf("sandbox", "info", "组件安装包：在线下载完成（校验通过）")
	return dst, "download", nil
}

// ---------- component install / uninstall / repair ----------

func sbieServiceRunning() bool {
	svc, _, _ := sbiePlatformStatus()
	return svc == "RUNNING"
}

// pollComponentReady waits until Start.exe exists and the service
// reports RUNNING.
func pollComponentReady(d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if sbieStartExePath() != "" && sbieServiceRunning() {
			return true
		}
		time.Sleep(3 * time.Second)
	}
	return sbieStartExePath() != "" && sbieServiceRunning()
}

func runComponentInstall() {
	defer func() {
		if r := recover(); r != nil {
			sbieCompSet("error", fmt.Sprintf("安装异常：%v", r))
		}
	}()
	sbieCompSet("preparing", "正在准备组件安装包…")
	pkg, source, err := acquireComponentPackage()
	if err != nil {
		sbieCompSet("error", err.Error())
		logf("sandbox", "error", "组件安装失败：%v", err)
		return
	}
	sbieComp.Lock()
	sbieComp.Source = source
	sbieComp.Unlock()
	sbieCompSet("installing", "正在安装驱动组件（需要你在弹出的系统窗口里点一次「是」）…")
	self, err := os.Executable()
	if err != nil {
		sbieCompSet("error", "找不到守护进程自身路径："+err.Error())
		return
	}
	code, err := sbieRunElevated(self, "-sbie-setup", pkg, sbieIniPath(), sandboxBoxRoot())
	if err != nil {
		if errors.Is(err, errSbieUACCanceled) {
			sbieCompSet("error", "管理员确认被取消了，安装没有开始")
		} else {
			sbieCompSet("error", "没能拉起管理员安装："+err.Error())
		}
		logf("sandbox", "error", "组件安装拉起失败：%v", err)
		return
	}
	if code != 0 {
		logf("sandbox", "warn", "组件安装过程返回 %d，以最终状态为准继续核对", code)
	}
	if !pollComponentReady(3 * time.Minute) {
		svc, drv, _ := sbiePlatformStatus()
		sbieCompSet("error", fmt.Sprintf("安装后服务没起来（服务=%s 驱动=%s），去设置页点「修复」再试，或看控制台日志", orDash(svc), orDash(drv)))
		logf("sandbox", "error", "组件安装后状态异常：服务=%s 驱动=%s", svc, drv)
		return
	}
	writeComponentMeta(source)
	sbieReload()
	sbieCompSet("done", "组件已就绪")
	logf("sandbox", "info", "应用隔离组件安装完成：Sandboxie Plus v%s（来源：%s）", sbieVersion, sourceLabel(source))
}

func runComponentUninstall() {
	defer func() {
		if r := recover(); r != nil {
			sbieCompSet("error", fmt.Sprintf("卸载异常：%v", r))
		}
	}()
	sbieCompSet("installing", "正在终止盒内应用…")
	sbieTerminateBox()
	for _, pid := range sbieBoxPids() {
		_ = sbieKillPid(pid)
	}
	waitBoxEmpty(10 * time.Second)
	unins := sbieUninstallerPath()
	if unins == "" {
		sbieCompSet("error", "找不到组件卸载器（组件可能已损坏），请在 Windows 设置里卸载 Sandboxie Plus")
		return
	}
	sbieCompSet("installing", "正在卸载驱动组件（需要你在弹出的系统窗口里点一次「是」）…")
	self, err := os.Executable()
	if err != nil {
		sbieCompSet("error", "找不到守护进程自身路径："+err.Error())
		return
	}
	code, err := sbieRunElevated(self, "-sbie-uninstall", unins)
	if err != nil {
		if errors.Is(err, errSbieUACCanceled) {
			sbieCompSet("error", "管理员确认被取消了，卸载没有开始")
		} else {
			sbieCompSet("error", "没能拉起管理员卸载："+err.Error())
		}
		return
	}
	if code != 0 {
		logf("sandbox", "warn", "组件卸载过程返回 %d，以最终状态为准继续核对", code)
	}
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) && sbieStartExePath() != "" {
		time.Sleep(3 * time.Second)
	}
	if sbieStartExePath() != "" {
		sbieCompSet("error", "卸载后组件文件还在，可能需要重启电脑后才完全移除")
		return
	}
	_ = os.Remove(componentMetaPath())
	sbieCompSet("done", "组件已卸载（盒子里已装的应用数据保留在 Data/sandbox，重装组件后可继续用）")
	logf("sandbox", "info", "应用隔离组件已卸载；盒内数据保留在 %s", sandboxDir())
}

// repairComponent re-places the box and, when the service is down,
// asks (one UAC) to start driver+service again.
func repairComponent() (string, error) {
	if sbieStartExePath() == "" {
		return "", fmt.Errorf("组件未安装，没有可修复的东西")
	}
	var notes []string
	if !sbieServiceRunning() {
		self, err := os.Executable()
		if err != nil {
			return "", err
		}
		if _, err := sbieRunElevated(self, "-sbie-startsvc"); err != nil {
			return "", fmt.Errorf("启动服务失败：%w", err)
		}
		if pollComponentReady(30 * time.Second) {
			notes = append(notes, "服务已重新启动")
		} else {
			return "", fmt.Errorf("服务还是没起来，可能需要重启电脑")
		}
	}
	if !placeBox() {
		return "", fmt.Errorf("盒子配置写不进去（管理员确认被取消或被拒绝）")
	}
	notes = append(notes, "盒子已按当前 Data 位置落位")
	logf("sandbox", "info", "组件修复完成：%s", strings.Join(notes, "；"))
	return strings.Join(notes, "；"), nil
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func sourceLabel(source string) string {
	switch source {
	case "embedded":
		return "内置组件包"
	case "download":
		return "在线下载"
	}
	return source
}

// ---------- boxed install flow ----------

// acquireBoxInstaller stages the installer into Data/sandbox/
// incoming and returns its physical path. URL downloads go through
// the daemon's download route and are hash-checked when the user
// supplied a SHA-256.
func acquireBoxInstaller(url, sha256hex, file string) (string, error) {
	inc := sandboxIncomingDir()
	if file != "" {
		name := filepath.Base(strings.ReplaceAll(file, "\\", "/"))
		if name != file || (!strings.EqualFold(filepath.Ext(name), ".exe") && !strings.EqualFold(filepath.Ext(name), ".msi")) {
			return "", fmt.Errorf("本地安装包名字不对")
		}
		p := filepath.Join(inc, name)
		if _, err := os.Stat(p); err != nil {
			return "", fmt.Errorf("导入的安装包不见了，请重新导入")
		}
		return p, nil
	}
	if url == "" {
		return "", fmt.Errorf("安装包地址和本地文件至少给一个")
	}
	name := urlFileName(url)
	if name == "" || (!strings.EqualFold(filepath.Ext(name), ".exe") && !strings.EqualFold(filepath.Ext(name), ".msi")) {
		return "", fmt.Errorf("从地址看不出是 .exe / .msi 安装包")
	}
	dst := filepath.Join(inc, name)
	ensureCoreForDownload()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	resp, err := dlGet(ctx, url, "MJ-Terminal-Sandbox/"+AppVersion)
	if err != nil {
		return "", fmt.Errorf("下载安装包失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载安装包失败：服务器返回 %d", resp.StatusCode)
	}
	sbieBoxOp.Lock()
	sbieBoxOp.Size = resp.ContentLength
	sbieBoxOp.Progress = 0
	sbieBoxOp.Unlock()
	part := dst + ".part"
	f, err := os.Create(part)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	buf := make([]byte, 256*1024)
	var written int64
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				_ = f.Close()
				_ = os.Remove(part)
				return "", werr
			}
			_, _ = h.Write(buf[:n])
			written += int64(n)
			sbieBoxOp.Lock()
			sbieBoxOp.Progress = written
			sbieBoxOp.Unlock()
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			_ = f.Close()
			_ = os.Remove(part)
			return "", rerr
		}
	}
	_ = f.Close()
	if sha256hex != "" && !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), sha256hex) {
		_ = os.Remove(part)
		return "", fmt.Errorf("安装包 SHA-256 对不上，已删掉（请核对地址或校验值）")
	}
	if err := os.Rename(part, dst); err != nil {
		return "", err
	}
	return dst, nil
}

// ---------- v0.43: install session + completion watch ----------
//
// Field failure this replaces (Telegram, 2026-10-29 13:35):
// runBoxInstall blocked in Start.exe /wait, then drained for
// the FULL five minutes because the installer auto-launched
// the Telegram app, which (correctly) never exits. For 5m19s
// the console said nothing at all, the store UI spun without
// an elapsed clock, and the user concluded "不能安装、没有报错".
//
// The v0.43 shape:
//   - an install SESSION registers every box PID the install
//     brings in; while a session is active the patrol exempts
//     box-owned windows wholesale (owncheck's BoxExempt): the
//     installer's windows belong to the install flow alone and
//     are never claimed, landed or presented by the Stage
//     Manager. (In the field run the patrol claimed the silent
//     installer's hidden Setup window at 13:35:43 and the prune
//     pass landed it inside the desktop at 13:35:45.)
//   - completion is a WATCH, not a wait: the installer process
//     has exited AND the box file tree has been quiet for a
//     stretch. Remaining session processes (the auto-launched
//     app) never block completion; they become ordinary boxed
//     processes for the launch path to claim.
//   - a heartbeat line every 30 s keeps the wait honest, and a
//     hard total timeout ends the install as a NAMED failure
//     carrying the state summary.
//   - 「终止本次安装」 kills ONLY the session's PID set.

var sbieInstallSess = struct {
	sync.Mutex
	active bool
	name   string
	pids   map[int]bool
}{pids: map[int]bool{}}

func installSessionBegin(name string) {
	sbieInstallSess.Lock()
	sbieInstallSess.active = true
	sbieInstallSess.name = name
	sbieInstallSess.pids = map[int]bool{}
	sbieInstallSess.Unlock()
	logf("sandbox", "info", "隔离安装会话开始：%s（会话期间盒内窗口归安装流程独占，台前调度不认领、不落位、不呈现）", name)
}

// installSessionTrack adds PIDs to the session and returns the
// ones that are new to it.
func installSessionTrack(pids []int) []int {
	sbieInstallSess.Lock()
	defer sbieInstallSess.Unlock()
	if !sbieInstallSess.active {
		return nil
	}
	var added []int
	for _, p := range pids {
		if !sbieInstallSess.pids[p] {
			sbieInstallSess.pids[p] = true
			added = append(added, p)
		}
	}
	return added
}

func installSessionEnd() {
	sbieInstallSess.Lock()
	if !sbieInstallSess.active {
		sbieInstallSess.Unlock()
		return
	}
	name, n := sbieInstallSess.name, len(sbieInstallSess.pids)
	sbieInstallSess.active = false
	sbieInstallSess.pids = map[int]bool{}
	sbieInstallSess.Unlock()
	logf("sandbox", "info", "隔离安装会话结束：%s（会话共见盒内进程 %d 个，巡逻豁免解除）", name, n)
}

func installSessionActive() bool {
	sbieInstallSess.Lock()
	defer sbieInstallSess.Unlock()
	return sbieInstallSess.active
}

func installSessionPIDs() []int {
	sbieInstallSess.Lock()
	defer sbieInstallSess.Unlock()
	out := make([]int, 0, len(sbieInstallSess.pids))
	for p := range sbieInstallSess.pids {
		out = append(out, p)
	}
	sort.Ints(out)
	return out
}

// boxTreeSig is a cheap activity fingerprint of the whole box:
// if files are still being written anywhere in it, some part
// of the signature moves.
type boxTreeSig struct {
	Files  int
	Bytes  int64
	MaxMod int64
}

func boxTreeSignature(boxRoot string) boxTreeSig {
	var sig boxTreeSig
	_ = filepath.Walk(boxRoot, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		sig.Files++
		sig.Bytes += info.Size()
		if m := info.ModTime().Unix(); m > sig.MaxMod {
			sig.MaxMod = m
		}
		return nil
	})
	return sig
}

type installWatchCfg struct {
	StableNeed        time.Duration // quiet stretch that proves "done" once the installer is gone
	UnknownStableNeed time.Duration // longer quiet stretch when the installer process was never identified
	UnknownMinElapsed time.Duration // ...and the watch must have run at least this long to use it
	Timeout           time.Duration // hard total deadline -> named failure
}

func defaultInstallWatchCfg(interactive bool) installWatchCfg {
	to := 15 * time.Minute
	if interactive {
		to = 30 * time.Minute
	}
	return installWatchCfg{
		StableNeed:        8 * time.Second,
		UnknownStableNeed: 30 * time.Second,
		UnknownMinElapsed: 60 * time.Second,
		Timeout:           to,
	}
}

type installWatchObs struct {
	Elapsed        time.Duration
	InstallerSeen  bool // a session PID's image name matched the installer file's
	InstallerGone  bool // seen, and none of them is alive any more
	AnySessionSeen bool // the session produced at least one process
	SessionAlive   int  // session PIDs still in the box right now
	StableFor      time.Duration
	FreshFiles     int // box file count now minus at install start
}

type installVerdict int

const (
	watchWait installVerdict = iota
	watchComplete
	watchTimeout
)

// decideInstallWatch is the completion rule (pure). Remaining
// session processes NEVER block: the auto-launched app is the
// normal case, not a stall. Three roads to "complete": the
// installer was seen and is gone + tree quiet; everything the
// session brought in has left again + tree quiet (the
// installer was never name-matched but the drain is real); or,
// when the installer process could not be identified at all, a
// long quiet stretch after a minimum watch -- installers do
// not pause half a minute mid-work without writing anything.
func decideInstallWatch(o installWatchObs, c installWatchCfg) installVerdict {
	if o.Elapsed >= c.Timeout {
		return watchTimeout
	}
	switch {
	case o.InstallerGone && o.StableFor >= c.StableNeed:
		return watchComplete
	case !o.InstallerSeen && o.AnySessionSeen && o.SessionAlive == 0 &&
		o.StableFor >= c.StableNeed && o.Elapsed >= 20*time.Second:
		return watchComplete
	case !o.InstallerSeen && o.AnySessionSeen && o.Elapsed >= c.UnknownMinElapsed &&
		o.StableFor >= c.UnknownStableNeed:
		return watchComplete
	}
	return watchWait
}

// installTimeoutSummary is the named failure's wording (pure):
// what was waited for, what state things were in when the
// deadline hit.
func installTimeoutSummary(name string, o installWatchObs, sessionPIDs []int) string {
	inst := "一直没能识别出安装器进程"
	if o.InstallerGone {
		inst = "安装器已退出"
	} else if o.InstallerSeen {
		inst = "安装器仍未退出"
	}
	return fmt.Sprintf("隔离安装超时未完成：%s 已等 %s（%s；盒内会话进程还剩 %d 个 %v；安装期间盒内新增文件 %d 个）。盒子里的东西原样保留，可以稍后重试，或在设置里清空盒子。",
		name, o.Elapsed.Round(time.Second), inst, o.SessionAlive, sessionPIDs, o.FreshFiles)
}

// installEmptyScanSummary is the named ending when an install
// completed but left nothing registrable (pure).
func installEmptyScanSummary(name string, freshFiles, freshExes, dropped int) string {
	return fmt.Sprintf("安装跑完了，但盒子里没找到能登记的新程序：%s（安装期间盒内新增文件 %d 个、新增可执行文件 %d 个，其中 %d 个在复核时已消失或属于工具/临时形态被剔除）。可能是安装失败、装到了盒子外，或安装器把程序放进了非常规位置。安装包还留在 Data/sandbox/incoming，细节看控制台日志。",
		name, freshFiles, freshExes, dropped)
}

// ---------- v0.43: pending registration persistence ----------
//
// Reaching "choosing" used to be a state only the store page's
// memory knew about: the field user finished the Telegram
// install while looking at the desktop/console and never knew
// a step was waiting for him. The pending registration is now
// written to disk the moment it exists and restored at daemon
// start, so the step survives page closes and restarts until
// the user registers or discards it.

type sbiePendingDisk struct {
	Installer     string   `json:"installer"`
	BeforeExes    []string `json:"beforeExes"`
	BeforeAppData []string `json:"beforeAppData"`
	At            string   `json:"at"`
	// FromExisting: the persisted source mark (v0.44) -- this
	// pending's candidates were the box's pre-existing programs
	// matched by name affinity, not files the install added.
	FromExisting bool `json:"fromExisting,omitempty"`
}

func pendingInstallPath() string { return filepath.Join(sandboxDir(), "pending-install.json") }

func savePendingInstallFile(path string, p *sbiePendingInstall) {
	if p == nil {
		return
	}
	d := sbiePendingDisk{Installer: p.InstallerName, At: time.Now().Format("2006-01-02 15:04:05"), FromExisting: p.FromExisting}
	for k := range p.BeforeExes {
		d.BeforeExes = append(d.BeforeExes, k)
	}
	for k := range p.BeforeAppData {
		d.BeforeAppData = append(d.BeforeAppData, k)
	}
	sort.Strings(d.BeforeExes)
	sort.Strings(d.BeforeAppData)
	b, err := json.MarshalIndent(d, "", " ")
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err == nil {
		_ = os.Rename(tmp, path)
	}
}

func clearPendingInstallFile() { _ = os.Remove(pendingInstallPath()) }

// finalizeCandidates runs the post-scan trust pipeline shared
// by the install flow and the pending restore: seal flags,
// foreign marks, and the generation-time liveness re-check.
// dropped counts candidates removed by the re-check.
func finalizeCandidates(ranked []boxExeCand, installerName string) (kept []boxExeCand, dropped int) {
	for _, c := range ranked {
		if !candidateExists(c) {
			dropped++
			logf("sandbox", "warn", "候选复核剔除：%s（文件已不在盒子里——多半是别的程序的临时文件，被它自己清掉了）", c.Virtual)
			continue
		}
		c.Exists = true
		c.Sealed = appSealedByExe(virtualBase(c.Virtual))
		kept = append(kept, c)
	}
	markForeignCandidates(kept, installerName)
	return kept, dropped
}

// restorePendingFrom rebuilds a persisted pending registration
// against the CURRENT box contents (the candidate list is
// re-derived: what the install left behind, minus what has
// since vanished or been registered away). False means there
// was nothing to restore.
func restorePendingFrom(path, boxRoot string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var d sbiePendingDisk
	if json.Unmarshal(b, &d) != nil || d.Installer == "" {
		_ = os.Remove(path)
		return false
	}
	before := map[string]bool{}
	for _, k := range d.BeforeExes {
		before[k] = true
	}
	beforeAD := map[string]bool{}
	for _, k := range d.BeforeAppData {
		beforeAD[k] = true
	}
	var fresh []boxExeCand
	for _, c := range snapshotBoxExes(boxRoot) {
		if !before[strings.ToLower(c.Physical)] {
			fresh = append(fresh, c)
		}
	}
	ranked, dropped := finalizeCandidates(rankBoxCandidates(fresh, d.Installer), d.Installer)
	fromExisting := false
	if len(ranked) == 0 {
		// v0.44: the restore shares the install flow's
		// empty-diff fallback -- a pending saved by an
		// already-in-box install has an empty diff BY
		// CONSTRUCTION (its before snapshot already contains
		// the program), so without this the restore would
		// retire exactly the pendings the fallback created.
		fb, fbDropped := finalizeCandidates(existingBoxCandidates(boxRoot, d.Installer), d.Installer)
		dropped += fbDropped
		if len(fb) > 0 {
			ranked, fromExisting = fb, true
		}
	}
	if len(ranked) == 0 {
		_ = os.Remove(path)
		logf("sandbox", "info", "待登记的隔离安装（%s）已无可登记的程序（候选复核剔除 %d 个），这条待办自动销掉", d.Installer, dropped)
		return false
	}
	sbieBoxOp.Lock()
	sbieBoxOp.Candidates = ranked
	sbieBoxOp.Pending = &sbiePendingInstall{InstallerName: d.Installer, BeforeExes: before, BeforeAppData: beforeAD, FromExisting: fromExisting}
	sbieBoxOp.Unlock()
	if fromExisting {
		sbieBoxSet("choosing", fmt.Sprintf("有个隔离安装（%s）还等着你确认主程序——它应该是已经装在盒子里了，确认即可登记，图标就上桌面。", d.Installer))
		logf("sandbox", "info", "待登记的隔离安装已恢复（已在盒内）：%s（%s 存盘，%d 个候选，来源：盒内已有）——到 应用商店 → 隔离安装 选主程序完成登记，或在那里放弃它", d.Installer, d.At, len(ranked))
	} else {
		sbieBoxSet("choosing", fmt.Sprintf("有个隔离安装（%s）还等着你确认主程序——选好登记，图标就上桌面。", d.Installer))
		logf("sandbox", "info", "待登记的隔离安装已恢复：%s（%s 存盘，%d 个候选）——到 应用商店 → 隔离安装 选主程序完成登记，或在那里放弃它", d.Installer, d.At, len(ranked))
	}
	return true
}

func runBoxInstall(url, sha256hex, file string) {
	defer func() {
		if r := recover(); r != nil {
			sbieBoxSet("error", fmt.Sprintf("隔离安装异常：%v", r))
		}
	}()
	fail := func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		sbieBoxSet("error", msg)
		logf("sandbox", "error", "隔离安装失败：%s", msg)
	}
	sbieBoxCancel.Store(false)
	sbieBoxSet("preparing", "正在准备…")
	if runtime.GOOS != "windows" {
		fail("当前不是 Windows 环境（这是测试环境），隔离安装要在 Windows 上用")
		return
	}
	if sbieStartExePath() == "" {
		fail("应用隔离组件还没装，先去 设置 → 应用隔离 装组件")
		return
	}
	if !sbieServiceRunning() {
		fail("隔离服务没在运行，去 设置 → 应用隔离 点「修复」")
		return
	}
	sbieBoxSet("downloading", "正在取得安装包…")
	pkgPath, err := acquireBoxInstaller(url, sha256hex, file)
	if err != nil {
		fail("%v", err)
		return
	}
	installerName := filepath.Base(pkgPath)
	kind := sniffInstallerKind(pkgPath)
	sbieBoxOp.Lock()
	sbieBoxOp.Kind = kind
	sbieBoxOp.Installer = installerName
	sbieBoxOp.Unlock()
	if kind == "msi" {
		fail("这是 MSI 安装包：它的安装服务跑在盒子外面，隔离不住，本流程不接。请找这个软件的 .exe 安装版再来。")
		return
	}
	if !placeBox() {
		fail("盒子配置写不进去（需要一次管理员确认，可能被取消了），请重试或去设置页点「修复」")
		return
	}
	boxRoot := sandboxBoxRoot()
	beforeExes := exeSet(snapshotBoxExes(boxRoot))
	beforeAppData := map[string]bool{}
	for _, d := range snapshotAppDataDirs(boxRoot) {
		beforeAppData[strings.ToLower(d)] = true
	}
	interactive := silentArgsFor(kind) == nil
	args := []string{"/box:" + sbieBoxName, pkgPath} // no /wait: the watch below owns completion (v0.43)
	if sa := silentArgsFor(kind); sa != nil {
		args = append(args, sa...)
		sbieBoxSet("installing", fmt.Sprintf("正在盒内静默安装 %s（%s）…", installerName, strings.ToUpper(kind)))
	} else {
		sbieBoxSet("installing", fmt.Sprintf("已在盒内打开 %s 的安装界面，在那个窗口里点完安装（全程在盒子里）…", installerName))
	}
	logf("sandbox", "info", "盒内安装开始：%s（类型 %s，静默参数 %v）", installerName, orDash(kind), silentArgsFor(kind))
	if sbieBoxCancel.Load() {
		sbieBoxSet("idle", "已终止本次隔离安装")
		logf("sandbox", "warn", "盒内安装在启动前被终止：%s", installerName)
		return
	}
	// Session baseline: the box PIDs and file tree as they were
	// BEFORE the installer starts. The session then owns every
	// PID that appears after this point.
	baseList, _ := sbieBoxPidsE()
	baseSet := pidSetFromInts(baseList)
	sigBefore := boxTreeSignature(boxRoot)
	installSessionBegin(installerName)
	defer installSessionEnd()
	start := time.Now()
	cmd := exec.Command(sbieStartExePath(), args...)
	hideConsole(cmd)
	if err := cmd.Start(); err != nil {
		fail("盒内安装启动失败：%v", err)
		return
	}
	go func() { _ = cmd.Wait() }() // reap Start.exe; it exits once the boxed hand-off is done

	cfg := defaultInstallWatchCfg(interactive)
	installerBase := strings.ToLower(installerName)
	installerPIDs := map[int]bool{}
	imageKnown := map[int]string{}
	lastSig := sigBefore
	lastChange := time.Now()
	lastBeat := time.Time{}
	var obs installWatchObs
	outcome := watchWait
	for outcome == watchWait {
		time.Sleep(2 * time.Second)
		if sbieBoxCancel.Load() {
			pids := installSessionPIDs()
			for _, pid := range pids {
				_ = sbieKillPid(pid)
			}
			sbieBoxSet("idle", "已终止本次隔离安装（只结束了这次安装的进程，盒子里其他应用没动）")
			logf("sandbox", "warn", "盒内安装被终止：%s（终止本会话进程 %v，其他盒内应用不受影响）", installerName, pids)
			return
		}
		var curSet map[uint32]bool
		if cur, perr := sbieBoxPidsE(); perr == nil {
			curSet = pidSetFromInts(cur)
			var freshPids []int
			for _, p := range cur {
				if !baseSet[uint32(p)] {
					freshPids = append(freshPids, p)
				}
			}
			for _, pid := range installSessionTrack(freshPids) {
				base := strings.ToLower(filepath.Base(sbieProcImagePath(uint32(pid))))
				imageKnown[pid] = base
				if base == installerBase {
					installerPIDs[pid] = true
				}
			}
		}
		sig := boxTreeSignature(boxRoot)
		if sig != lastSig {
			lastSig = sig
			lastChange = time.Now()
		}
		installerAlive := false
		for pid := range installerPIDs {
			if curSet[uint32(pid)] {
				installerAlive = true
			}
		}
		alive := 0
		for _, pid := range installSessionPIDs() {
			if curSet[uint32(pid)] {
				alive++
			}
		}
		obs = installWatchObs{
			Elapsed:        time.Since(start),
			InstallerSeen:  len(installerPIDs) > 0,
			InstallerGone:  len(installerPIDs) > 0 && !installerAlive,
			AnySessionSeen: len(imageKnown) > 0,
			SessionAlive:   alive,
			StableFor:      time.Since(lastChange),
			FreshFiles:     sig.Files - sigBefore.Files,
		}
		outcome = decideInstallWatch(obs, cfg)
		if outcome == watchWait && time.Since(lastBeat) >= 30*time.Second {
			lastBeat = time.Now()
			inst := "未识别到安装器进程"
			if obs.InstallerGone {
				inst = "安装器已退出"
			} else if obs.InstallerSeen {
				inst = "安装器仍在运行"
			}
			logf("sandbox", "info", "隔离安装进行中：%s 已用时 %s；%s；盒内会话进程 %d 个；盒内新增文件 %d 个；文件树已稳定 %s",
				installerName, obs.Elapsed.Round(time.Second), inst, obs.SessionAlive, obs.FreshFiles, obs.StableFor.Round(time.Second))
			sbieBoxSet("installing", fmt.Sprintf("盒内安装中：%s（已用时 %s，%s，盒内新增文件 %d 个）",
				installerName, obs.Elapsed.Round(time.Second), inst, obs.FreshFiles))
		}
	}
	if outcome == watchTimeout {
		fail("%s", installTimeoutSummary(installerName, obs, installSessionPIDs()))
		return
	}
	sbieBoxSet("scanning", "安装完成了，正在盒子里找新装的程序并逐个复核…")
	after := snapshotBoxExes(boxRoot)
	var fresh []boxExeCand
	for _, c := range after {
		if !beforeExes[strings.ToLower(c.Physical)] {
			fresh = append(fresh, c)
		}
	}
	ranked, dropped := finalizeCandidates(rankBoxCandidates(fresh, installerName), installerName)
	logf("sandbox", "info", "盒内安装结束：%s 用时 %s，盒内新增 exe %d 个，候选 %d 个（复核剔除 %d 个）",
		installerName, time.Since(start).Round(time.Second), len(fresh), len(ranked), dropped)
	fromExisting := false
	if len(ranked) == 0 {
		// v0.44: an empty diff is not automatically a failed
		// install -- a same-version reinstall writes nothing
		// (the installer skips identical files), and the
		// program is already lying in the box. Fall back to
		// the box's existing programs that match this
		// installer's name before declaring failure.
		fb, fbDropped := finalizeCandidates(existingBoxCandidates(boxRoot, installerName), installerName)
		dropped += fbDropped
		if len(fb) == 0 {
			fail("%s", installEmptyScanSummary(installerName, obs.FreshFiles, len(fresh), dropped))
			return
		}
		ranked, fromExisting = fb, true
		logf("sandbox", "info", "隔离安装空差分回退：%s 本次安装没有新增能登记的程序（盒内新增文件 %d 个）——这个程序应该是已经装在盒子里了（同版本重装时安装器会跳过已存在的文件）。已按安装包名从盒内已有程序中找到 %d 个对得上的（标记「盒内已有」），进待登记：到 应用商店 → 隔离安装 确认主程序即可登记。",
			installerName, obs.FreshFiles, len(ranked))
	}
	pend := &sbiePendingInstall{
		InstallerName: installerName,
		BeforeExes:    beforeExes,
		BeforeAppData: beforeAppData,
		FromExisting:  fromExisting,
	}
	sbieBoxOp.Lock()
	sbieBoxOp.Candidates = ranked
	sbieBoxOp.Pending = pend
	sbieBoxOp.Unlock()
	savePendingInstallFile(pendingInstallPath(), pend)
	if fromExisting {
		sbieBoxSet("choosing", "这个程序看起来已经装在盒子里了（本次安装没有新增文件，多半是同版本重装）。下面是从盒内找到的已有程序，确认主程序即可登记，图标就上桌面。")
		logf("sandbox", "info", "隔离安装待登记（已在盒内）：%s（%d 个候选，来源：盒内已有）。请到 应用商店 → 隔离安装 选主程序完成登记——这一步已经存盘，关掉页面或重启终端都不会丢。",
			installerName, len(ranked))
	} else {
		sbieBoxSet("choosing", "装完了。确认一下哪个是主程序、叫什么名字，登记后图标就上桌面。")
		logf("sandbox", "info", "隔离安装待登记：%s 已装入盒子（%d 个候选）。请到 应用商店 → 隔离安装 选主程序完成登记——这一步已经存盘，关掉页面或重启终端都不会丢。",
			installerName, len(ranked))
	}
	// v0.47 hook (notify): the install finished and waits at the
	// registration picker (both fresh and already-in-box paths).
	postNotify(notifySrcSandbox, "install-done:"+installerName, "隔离安装完成",
		installerName+" 已装入盒子，等你确认主程序完成登记", "appstore")
}

// registerBoxApp finalizes a boxed install into the registry.
func registerBoxApp(name, virtualExe string) (boxApp, error) {
	sbieBoxOp.Lock()
	pend := sbieBoxOp.Pending
	cands := sbieBoxOp.Candidates
	sbieBoxOp.Unlock()
	if pend == nil {
		return boxApp{}, fmt.Errorf("没有等待登记的安装")
	}
	var chosen *boxExeCand
	for i := range cands {
		if strings.EqualFold(cands[i].Virtual, virtualExe) {
			chosen = &cands[i]
			break
		}
	}
	if chosen == nil {
		return boxApp{}, fmt.Errorf("选的主程序不在这次安装的候选里")
	}
	// v0.43: registration-time re-verification, the last gate.
	// A registration pointing at a file that is not in the box
	// (any more) is the one accident that must never ship: the
	// field "Telegram" registration named a temporary 7z.exe
	// another program had already cleaned up.
	if isJunkCandidateShape(virtualExe) {
		return boxApp{}, fmt.Errorf("这个路径是工具/临时形态（通用小工具，或插件、临时目录里的文件），不是应用的主程序，不能登记——请选真正的那个主程序")
	}
	if !candidateExists(*chosen) {
		return boxApp{}, fmt.Errorf("选定的主程序文件刚刚已经不在盒子里了（多半是别的程序的临时文件，被它自己清掉了），请换一个候选，或放弃这次登记")
	}
	if name == "" {
		name = virtualStem(virtualExe)
	}
	boxRoot := sandboxBoxRoot()
	installDirV := appInstallDirVirtual(virtualExe)
	hints := []string{name, virtualStem(virtualExe)}
	if installDirV != "" {
		hints = append(hints, virtualBase(installDirV))
	}
	matchedPhys := matchNewAppDataDirs(snapshotAppDataDirs(boxRoot), pend.BeforeAppData, hints)
	var appDataV []string
	for _, p := range matchedPhys {
		if v := physicalToVirtual(boxRoot, p); v != "" {
			appDataV = append(appDataV, v)
		}
	}
	boxAppsMu.Lock()
	reg := loadBoxApps()
	app := boxApp{
		ID:          newBoxAppID(reg, name),
		Name:        name,
		Exe:         virtualExe,
		InstallDir:  installDirV,
		AppDataDirs: appDataV,
		Installer:   pend.InstallerName,
		At:          time.Now().Format("2006-01-02 15:04:05"),
	}
	reg = boxAppsUpsert(reg, app)
	saveBoxApps(reg)
	boxAppsMu.Unlock()
	sbieBoxOp.Lock()
	sbieBoxOp.Pending = nil
	sbieBoxOp.Candidates = nil
	sbieBoxOp.Unlock()
	clearPendingInstallFile()
	sbieBoxSet("done", "已登记到桌面："+name)
	logf("sandbox", "info", "盒内应用已登记：%s（%s，数据目录 %d 个）", name, virtualExe, len(appDataV))
	// v0.42: the seal rules are a function of the registry, so
	// a new registration refreshes the ini placement — the new
	// app's WriteFilePath lines land with it — and a covered
	// app's one-time purge runs right away: anything the
	// installer copied into the shadow while reading the host
	// is wiped before the app's first launch.
	if appSealedByExe(virtualBase(app.Exe)) {
		if !placeBox() {
			logf("sandbox", "warn", "盒内应用已登记：%s 的数据封堵规则这次没写进盒子配置，将在下次启动或修复时补上", name)
		}
		applyDataSeal(app)
	}
	return app, nil
}

// ---------- boxed app launch / remove / box clear ----------

// ---------- v0.41: same-name residual sweep ----------
//
// The v0.39/v0.40 field failure's second half: the first launch
// died before ever showing a window, leaving a hidden instance
// behind in the box. A single-instance app then delegates every
// later launch to that wedged instance -- which stays hidden --
// so no amount of claiming fixes the second, third, fourth
// click. The launch path therefore rules on same-name processes
// BEFORE handing off: leftovers showing no app window are
// killed (PID by PID, same name only -- never the whole box),
// an instance that IS showing its window is a live app and is
// presented through the second-launch route instead.

// procNameFact is one boxed process as the launch path sees it:
// its PID and its image's base name. The base name is the one
// part of a boxed process's image path that reads the same in
// the virtual and the physical world (…\Weixin\Weixin.exe
// either way), which is what makes it a safe same-name key.
type procNameFact struct {
	PID       uint32
	ImageBase string
}

// sameNameBoxPIDs selects, in ascending order, the PIDs whose
// image base name equals exeBase, case-insensitively (pure). A
// process whose image could not be read (empty base) never
// matches: an unreadable process is not evidence and is never
// killed on a guess.
func sameNameBoxPIDs(procs []procNameFact, exeBase string) []uint32 {
	if exeBase == "" {
		return nil
	}
	var out []uint32
	for _, p := range procs {
		if p.ImageBase != "" && strings.EqualFold(p.ImageBase, exeBase) {
			out = append(out, p.PID)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

type boxResidualDecision int

const (
	boxResidualNone    boxResidualDecision = iota // no same-name process in the box
	boxResidualKill                               // same-name processes, none showing a visible app window: wedged leftovers
	boxResidualPresent                            // a same-name process already shows a visible app window: a live app
)

// decideBoxResidual rules on the same-name processes a launch
// found already inside the box (pure). Only windows owned by the
// residual PIDs themselves count, and only a VISIBLE app-shaped
// one makes the instance "live": the wedged shape is precisely
// processes whose every window is hidden or plumbing.
func decideBoxResidual(residuals []uint32, wins []windowFact) boxResidualDecision {
	if len(residuals) == 0 {
		return boxResidualNone
	}
	set := make(map[uint32]bool, len(residuals))
	for _, p := range residuals {
		set[p] = true
	}
	for _, w := range windowsOwnedByPIDs(wins, set) {
		if w.Visible && isAppWindowShape(w) {
			return boxResidualPresent
		}
	}
	return boxResidualKill
}

// boxGhostError is a launch failure caused by the REGISTRATION
// being dead (v0.43): the file it names is gone, or never was
// an application at all. The UI keys off this type to offer
// 「移除这条登记」 instead of a dead-end alert.
type boxGhostError struct {
	App    boxApp
	Reason string
}

func (e *boxGhostError) Error() string {
	return fmt.Sprintf("%s 打不开了：%s（登记路径：%s）。这条登记已经失效，可以移除它。", e.App.Name, e.Reason, e.App.Exe)
}

func launchBoxApp(app boxApp) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("当前不是 Windows 环境（这是测试环境），到 Windows 上会正常打开")
	}
	se := sbieStartExePath()
	if se == "" {
		return fmt.Errorf("应用隔离组件还没装，先去 设置 → 应用隔离 装组件")
	}
	if !iniPlacedCurrent() {
		// placeBox (direct write, else the one-UAC helper), not
		// the bare in-process attempt: after a version update
		// the section usually only lacks the newest managed
		// lines (v0.42: an app's data-seal rules), and sending
		// the user to 设置 → 修复 for that would be pure
		// friction on the exact click they are making.
		if !placeBox() {
			return fmt.Errorf("盒子配置与当前 Data 位置对不上，去 设置 → 应用隔离 点一次「修复」")
		}
	}
	physExe := virtualToPhysical(sandboxBoxRoot(), app.Exe)
	// v0.43: the registration itself is re-verified before
	// Start.exe is ever invoked. A registration can name a file
	// that was another program's temporary drop (field: the
	// "Telegram" registration pointed at a WeChat patcher's
	// extracted 7z.exe) — launching it either fails inside
	// Sandboxie with a raw system popup, or worse, runs a tool
	// the user never installed. Ghost registrations are
	// reported in MJ's words and flagged so the UI can offer
	// one-click removal; Sandboxie's own error never reaches
	// the user from this path.
	if isJunkCandidateShape(app.Exe) {
		return &boxGhostError{App: app, Reason: "登记指向的路径是工具/临时形态（通用小工具，或插件、临时目录里的文件），不是这个应用的主程序——这条登记多半是当时选错了候选留下的"}
	}
	if _, err := os.Stat(physExe); err != nil {
		return &boxGhostError{App: app, Reason: "登记的程序文件已不在盒子里，可能被它所属的程序在更新时清理掉了"}
	}
	// v0.42: a covered app's first launch under the seal runs
	// its one-time purge of leaked shadow copies first (it
	// stops the app if it is somehow already running). No-op
	// once sealed, and for uncovered apps.
	applyDataSeal(app)
	// v0.40: the claiming identity for a boxed launch is the box
	// PID list itself, snapshotted BEFORE Start.exe hands off.
	// The old flow (trackAppLaunch, keyed on the started process
	// and the exe's folder) structurally cannot see a boxed
	// process: SbieSvc creates it, Start.exe exits, and its image
	// path is the virtual one. The raw before-set goes into the
	// log verbatim -- it is the baseline every later diagnosis
	// diff is read against.
	beforePIDs := sbieBoxPids()

	// v0.41: rule on same-name processes already in the box
	// BEFORE handing off (see the residual-sweep section above).
	// A wedged leftover is killed by PID -- same name only, the
	// rest of the box is never touched -- and the baseline
	// snapshot is retaken afterwards, so the poll's difference
	// describes only what the fresh hand-off brings in. A live
	// instance (visible app window) skips the hand-off entirely:
	// Start.exe would only delegate to it anyway, and the poll's
	// second-launch route presents its window.
	skipStart := false
	exeBase := virtualBase(app.Exe)
	facts := make([]procNameFact, 0, len(beforePIDs))
	for _, pid := range beforePIDs {
		facts = append(facts, procNameFact{PID: uint32(pid), ImageBase: filepath.Base(sbieProcImagePath(uint32(pid)))})
	}
	residuals := sameNameBoxPIDs(facts, exeBase)
	switch decideBoxResidual(residuals, platformAllTopWindows()) {
	case boxResidualKill:
		killed := make([]int, 0, len(residuals))
		for _, pid := range residuals {
			if err := sbieKillPid(int(pid)); err == nil {
				killed = append(killed, int(pid))
			} else {
				logf("sandbox", "warn", "盒内启动 %s：终止同名残留进程 %d 失败：%v（照常继续启动）", app.Name, pid, err)
			}
		}
		if len(killed) > 0 {
			logf("sandbox", "info", "盒内启动 %s：启动前清掉同名残留进程 %d 个（PID %v；它们名下没有可见的应用窗口），再全新拉起",
				app.Name, len(killed), killed)
			waitBoxPidsGone(pidSetFromInts(killed), 3*time.Second)
			beforePIDs = sbieBoxPids()
		}
	case boxResidualPresent:
		skipStart = true
		logf("sandbox", "info", "盒内启动 %s：同名进程已在盒内运行且有可见窗口（PID %v），不重复拉起，走二次启动呈现",
			app.Name, residuals)
	}

	beforeWins := currentTopWindowHWNDs()
	if !skipStart {
		cmd := exec.Command(se, "/box:"+sbieBoxName, app.Exe)
		hideConsole(cmd)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("启动失败：%w", err)
		}
		noteSpawnedProcess(uint32(cmd.Process.Pid), app.Name)
	}
	logf("sandbox", "info", "盒内启动：%s（启动前盒内 PID 集：%v）", app.Name, beforePIDs)
	go pollBoxLaunch(app, pidSetFromInts(beforePIDs), beforeWins)
	return nil
}

// ---------- v0.40: boxed-launch window tracking ----------
//
// pollBoxLaunch follows one boxed launch for up to 15 s, polling
// the box PID list every 500 ms. Three success routes, in the
// order they are tried each round:
//
//	newpid   -- the PID difference (processes this launch brought
//	            into the box) owns a new window: register it by
//	            explicit PID set (claimWindowByPIDs).
//	existing -- the difference is empty because the app was
//	            ALREADY running in the box (second click on its
//	            icon; single-instance apps do exactly this): its
//	            already-registered window is presented, or an
//	            unregistered window of the existing PID set is
//	            claimed. An empty difference is a normal route,
//	            never a failure.
//	cross    -- at the deadline, one last cross-check of the FULL
//	            current box PID set against every top-level
//	            window, in case the window is held by a boxed
//	            process the difference did not name.
//
// Every PID set the poll discovers is logged verbatim (sandbox
// module), and a timeout produces ONE typed failure line (winmgr
// module) whose wording separates the field cases: the process
// never appeared / appeared but exited / is alive with zero
// windows / a window exists but belongs to another boxed PID /
// (v0.41) an app window exists but is hidden and could not be
// revealed. The next real-machine log is expected to settle
// which of the shapes happened from that line alone.

// boxLaunchFailKind names the timeout shapes (five since
// v0.41: the born-hidden shape is its own kind, no longer folded
// into "windows under other boxed PIDs").
type boxLaunchFailKind string

const (
	boxFailNoProcess    boxLaunchFailKind = "进程未出现"
	boxFailExited       boxLaunchFailKind = "进程已退出"
	boxFailNoWindow     boxLaunchFailKind = "进程在但无窗口"
	boxFailWindowPID    boxLaunchFailKind = "窗口在其他盒内进程名下"
	boxFailHiddenWindow boxLaunchFailKind = "有隐藏应用窗口但呈现失败"
)

// classifyBoxLaunchTimeout picks the failure kind (pure).
// crossHiddenApp means the deadline cross-check found an
// app-shaped window that is HIDDEN under a boxed PID -- the
// v0.40 field shape, and since v0.41 the claim path tries to
// reveal exactly that window, so reaching classification with
// one means the reveal itself failed; it outranks every other
// reading. crossCount > 0 means the cross-check found windows
// owned by boxed PIDs at all (none of them a hidden app
// window); stillPresent means a PID this launch added is still
// in the box at the deadline; sawNew means such a PID was seen
// at all.
func classifyBoxLaunchTimeout(sawNew, stillPresent bool, crossCount int, crossHiddenApp bool) boxLaunchFailKind {
	switch {
	case crossHiddenApp:
		return boxFailHiddenWindow
	case crossCount > 0:
		return boxFailWindowPID
	case sawNew && !stillPresent:
		return boxFailExited
	case sawNew || stillPresent:
		return boxFailNoWindow
	default:
		return boxFailNoProcess
	}
}

// hasHiddenAppWindow reports whether a cross-check list holds a
// born-hidden app window (pure): app-shaped but not visible --
// the exact window the v0.41 claim path exists to reveal.
func hasHiddenAppWindow(cross []windowFact) bool {
	for _, w := range cross {
		if !w.Visible && isAppWindowShape(w) {
			return true
		}
	}
	return false
}

// formatBoxCrossCheck renders the deadline cross-check result
// for the log (pure): every window a boxed PID holds, with the
// PID / title / class that identify it.
func formatBoxCrossCheck(cross []windowFact) string {
	if len(cross) == 0 {
		return "无（盒内 PID 名下没有任何顶级窗口）"
	}
	parts := make([]string, 0, len(cross))
	for _, w := range cross {
		parts = append(parts, fmt.Sprintf("hwnd=%d pid=%d 标题=%q 类=%s 可见=%v",
			w.HWND, w.PID, w.Title, w.Class, w.Visible))
	}
	return fmt.Sprintf("%d 个：%s", len(cross), strings.Join(parts, "；"))
}

// boxLaunchFailMessage builds the typed timeout line (pure).
func boxLaunchFailMessage(app string, kind boxLaunchFailKind, before, last []int, cross []windowFact) string {
	head := fmt.Sprintf("盒内应用窗口认领失败（%s）：%s", kind, app)
	crossTxt := formatBoxCrossCheck(cross)
	switch kind {
	case boxFailExited:
		return fmt.Sprintf("%s——盒内进程出现过又消失了（启动前 PID 集 %v，最后 PID 集 %v；进程中途退出，可能崩溃或单实例自退，不下结论）；超时交叉核对：%s",
			head, before, last, crossTxt)
	case boxFailNoWindow:
		return fmt.Sprintf("%s——盒内进程还活着（最后 PID 集 %v），但一直没有窗口（可能卡在初始化）；超时交叉核对：%s",
			head, last, crossTxt)
	case boxFailHiddenWindow:
		return fmt.Sprintf("%s——盒内应用的主窗口已经生成，但一直是隐藏状态，本次没能把它显示出来（最后 PID 集 %v）；超时交叉核对：%s",
			head, last, crossTxt)
	case boxFailWindowPID:
		return fmt.Sprintf("%s——超时交叉核对在盒内 PID 名下找到了窗口，但没能登记（最后 PID 集 %v）；交叉核对：%s",
			head, last, crossTxt)
	default: // boxFailNoProcess
		return fmt.Sprintf("%s——盒内进程名单里一直没出现该应用（启动前 PID 集 %v，最后 PID 集 %v），进程可能压根没起来；超时交叉核对：%s",
			head, before, last, crossTxt)
	}
}

func equalIntSlices(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func pollBoxLaunch(app boxApp, before map[uint32]bool, beforeWins map[uint64]bool) {
	imageBase := strings.ToLower(virtualBase(app.Exe))
	beforeList := sortedPIDList(before)
	lastList := beforeList
	sawNew := false
	newSeen := map[uint32]bool{} // every new PID observed along the way
	errNoted := false
	deadline := time.Now().Add(15 * time.Second)
	for {
		time.Sleep(500 * time.Millisecond)
		curList, err := sbieBoxPidsE()
		if err != nil {
			// A failed read is not an empty box; note it once
			// per launch (the patrol owns the throttled repeat
			// warnings) and keep polling.
			if !errNoted {
				errNoted = true
				logf("sandbox", "warn", "盒内启动 %s：读取盒内 PID 名单失败一次：%v（继续轮询）", app.Name, err)
			}
			if time.Now().After(deadline) {
				break
			}
			continue
		}
		cur := pidSetFromInts(curList)
		if !equalIntSlices(curList, lastList) {
			diffNow := diffPIDs(before, cur)
			logf("sandbox", "info", "盒内启动 %s：发现盒内 PID 集 %v（相对启动前新增 %v）",
				app.Name, curList, sortedPIDList(diffNow))
			lastList = curList
		}
		diff := diffPIDs(before, cur)
		if len(diff) > 0 {
			sawNew = true
			for p := range diff {
				newSeen[p] = true
			}
			if hwnd, ok := claimWindowByPIDs(app.Name, diff, diff, beforeWins, imageBase, ownBox); ok {
				logf("sandbox", "info", "盒内启动：%s 已认领窗口（路径：新 PID 直供登记，hwnd=%d，新增 PID 集 %v）",
					app.Name, hwnd, sortedPIDList(diff))
				return
			}
			// The patrol may have beaten us to this window.
			if hwnd, ok := presentWindowForPIDs(app.Name, diff); ok {
				logf("sandbox", "info", "盒内启动：%s 的窗口已在台前调度中（路径：新 PID 已被认领，hwnd=%d）", app.Name, hwnd)
				return
			}
		} else if len(before) > 0 {
			// Second-launch route: no new PID is the expected
			// shape when the app already runs in the box.
			if hwnd, ok := presentWindowForPIDs(app.Name, before); ok {
				logf("sandbox", "info", "盒内启动：%s 走二次启动路径（应用已在盒内运行，本次无新 PID），已呈现其已有窗口（hwnd=%d）", app.Name, hwnd)
				return
			}
			if hwnd, ok := claimWindowByPIDs(app.Name, before, nil, nil, imageBase, ownBox); ok {
				logf("sandbox", "info", "盒内启动：%s 走二次启动路径（应用已在盒内运行，本次无新 PID），已用已有 PID 集认领其窗口（hwnd=%d）", app.Name, hwnd)
				return
			}
		}
		if time.Now().After(deadline) {
			break
		}
	}
	// Deadline: the typed diagnosis. One final cross-check of
	// the FULL current box PID set against every top-level
	// window separates "no window at all" from "a window held by
	// a boxed process the difference never named" -- and doubles
	// as one last claim attempt for the latter.
	lastList = sbieBoxPids()
	last := pidSetFromInts(lastList)
	cross := windowsOwnedByPIDs(platformAllTopWindows(), last)
	if len(cross) > 0 {
		// v0.41: the cross-check claim ranks this launch's own
		// new PIDs first (prefer), and the picker now admits the
		// born-hidden app window and reveals it on claim.
		if hwnd, ok := claimWindowByPIDs(app.Name, last, newSeen, beforeWins, "", ownBox); ok {
			logf("sandbox", "info", "盒内启动：%s 已认领窗口（路径：超时交叉核对——窗口在盒内其他进程名下，hwnd=%d，盒内 PID 集 %v）",
				app.Name, hwnd, lastList)
			return
		}
		// The patrol may have register-only-claimed the window
		// (v0.41 HiddenClaim) while we polled: presenting it is
		// still a successful launch.
		if hwnd, ok := presentWindowForPIDs(app.Name, last); ok {
			logf("sandbox", "info", "盒内启动：%s 的窗口已在台前调度中（路径：超时交叉核对——巡逻已登记，hwnd=%d）", app.Name, hwnd)
			return
		}
	}
	// stillPresent: a PID this launch was seen to add is still
	// boxed at the deadline, or (second-launch shape, nothing new
	// was ever seen) a pre-existing boxed PID survives -- either
	// way "the process is in there" is the honest reading. A new
	// PID that was seen but is gone now is the exited shape, and
	// other apps' pre-existing PIDs must not mask it.
	stillPresent := pidSetsOverlap(newSeen, last) || (!sawNew && pidSetsOverlap(before, last))
	kind := classifyBoxLaunchTimeout(sawNew, stillPresent, len(cross), hasHiddenAppWindow(cross))
	logf("winmgr", "warn", "%s", boxLaunchFailMessage(app.Name, kind, beforeList, lastList, cross))
}

// ---------- data seal: one-time leaked-copy purge (v0.42) ----------

// boxUserNames lists the user shadows present in the box.
func boxUserNames(boxRoot string) []string {
	entries, _ := os.ReadDir(filepath.Join(boxRoot, "user"))
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// applyDataSeal runs one covered app's one-time purge of the
// shadow copies that leaked under its template roots before
// the seal existed. The app's own processes are stopped first
// — same-name PIDs only, the v0.41 residual discipline —
// because deleting a live app's data folder under it helps no
// one; if they will not die, the purge is skipped and the
// marker left unset, so the next launch retries it. Only paths
// strictly inside the box root are ever removed (each one is
// prefix-checked and logged); host data is never touched —
// this function cannot even name a path outside the box.
//
// The purge is files-only, deliberately — the registry half
// of the seal needs no purge, and could not get a safe one.
// Host-key READ-through never materializes into the box's
// registry shadow: the RegHive file at the box root records
// only what boxed programs themselves wrote (copy-on-write),
// so there is no leaked host copy sitting in it to remove,
// and the WriteKeyPath rules close the read prospectively
// the moment the ini regenerates — no DataSealed re-run is
// owed for them. Per-key surgery on that hive is off the
// table anyway: it is ONE binary file shared by every boxed
// app, and no offline editor can rewrite a live box's hive
// without risking the whole box's registry. The one residue
// that can exist is a value the boxed app itself persisted
// into its shadow key before the seal (say it once stored
// the host storage path it had read through): that is the
// app's own box-side writing, not host data, the host value
// behind it is no longer reachable, and erasing it would
// mean resetting the entire box registry — the user's call
// via clear-box in settings, never a silent purge step.
func applyDataSeal(app boxApp) {
	if !sealNeeded(app) {
		return
	}
	exeBase := virtualBase(app.Exe)
	pids, err := sbieBoxPidsE()
	if err != nil {
		logf("sandbox", "warn", "数据封堵 %s：读不到盒内进程名单（%v），这次先不清，下次启动前再试", app.Name, err)
		return
	}
	if len(pids) > 0 {
		facts := make([]procNameFact, 0, len(pids))
		for _, pid := range pids {
			facts = append(facts, procNameFact{PID: uint32(pid), ImageBase: filepath.Base(sbieProcImagePath(uint32(pid)))})
		}
		same := sameNameBoxPIDs(facts, exeBase)
		if len(same) > 0 {
			for _, pid := range same {
				if err := sbieKillPid(int(pid)); err != nil {
					logf("sandbox", "warn", "数据封堵 %s：终止其盒内进程 %d 失败：%v", app.Name, pid, err)
				}
			}
			set := make(map[uint32]bool, len(same))
			for _, p := range same {
				set[p] = true
			}
			waitBoxPidsGone(set, 3*time.Second)
			if left, err2 := sbieBoxPidsE(); err2 == nil {
				facts2 := make([]procNameFact, 0, len(left))
				for _, pid := range left {
					facts2 = append(facts2, procNameFact{PID: uint32(pid), ImageBase: filepath.Base(sbieProcImagePath(uint32(pid)))})
				}
				if len(sameNameBoxPIDs(facts2, exeBase)) > 0 {
					logf("sandbox", "warn", "数据封堵 %s：它还有进程在盒内跑着，这次先不清，下次启动前再试", app.Name)
					return
				}
			}
		}
	}
	boxRoot := sandboxBoxRoot()
	removed := 0
	for _, target := range sealShadowPaths(boxRoot, exeBase, boxUserNames(boxRoot)) {
		if !insideBoxStrict(boxRoot, target) {
			logf("sandbox", "warn", "数据封堵 %s：拒绝清除盒子之外的路径 %s", app.Name, target)
			continue
		}
		if _, err := os.Stat(target); err != nil {
			continue // no shadow copy there: nothing leaked in
		}
		if err := os.RemoveAll(target); err != nil {
			logf("sandbox", "warn", "数据封堵 %s：清除 %s 失败：%v", app.Name, target, err)
			continue
		}
		removed++
		logf("sandbox", "info", "数据封堵 %s：已清除盒内漏入的旧数据副本 %s", app.Name, target)
	}
	boxAppsMu.Lock()
	reg := loadBoxApps()
	if cur, ok := reg[app.ID]; ok {
		cur.DataSealed = true
		reg[app.ID] = cur
		saveBoxApps(reg)
	}
	boxAppsMu.Unlock()
	logf("sandbox", "info", "数据封堵 %s：已套上——它的数据目录从此只认盒内这一份，宿主机那份读不到；清除旧副本 %d 处，下次启动是全新首启", app.Name, removed)
}

func removeBoxApp(app boxApp) error {
	boxRoot := sandboxBoxRoot()
	physExe := virtualToPhysical(boxRoot, app.Exe)
	physDir := ""
	if app.InstallDir != "" {
		physDir = virtualToPhysical(boxRoot, app.InstallDir)
	}
	// Stop this app's processes (only those whose image lives in
	// its install folder / is its main exe).
	for _, pid := range sbieBoxPids() {
		ip := sbieProcImagePath(uint32(pid))
		if ip == "" {
			continue
		}
		if strings.EqualFold(ip, physExe) || (physDir != "" && strings.HasPrefix(strings.ToLower(ip), strings.ToLower(physDir)+`\`)) {
			_ = sbieKillPid(pid)
		}
	}
	time.Sleep(600 * time.Millisecond)
	if physDir != "" {
		if err := os.RemoveAll(physDir); err != nil {
			return fmt.Errorf("删程序目录失败（程序可能还在运行）：%w", err)
		}
	} else if physExe != "" {
		_ = os.Remove(physExe)
	}
	for _, vd := range app.AppDataDirs {
		if pd := virtualToPhysical(boxRoot, vd); pd != "" {
			if err := os.RemoveAll(pd); err != nil {
				logf("sandbox", "warn", "移除 %s 时数据目录没删干净：%s：%v", app.Name, vd, err)
			}
		}
	}
	boxAppsMu.Lock()
	reg := loadBoxApps()
	reg = boxAppsRemove(reg, app.ID)
	saveBoxApps(reg)
	boxAppsMu.Unlock()
	// v0.42: a removed app's seal rules leave the ini with it.
	// Lines shared with another registration of the same exe
	// survive — sealRuleLines is registry-driven, so the
	// refresh simply regenerates from what remains.
	if appSealedByExe(virtualBase(app.Exe)) && sbieStartExePath() != "" && !placeBox() {
		logf("sandbox", "warn", "移除 %s 后刷新盒子配置失败，它的数据封堵规则可能还留在配置里（下次落位会清掉）", app.Name)
	}
	logf("sandbox", "info", "盒内应用已移除：%s（程序与数据一并删除）", app.Name)
	return nil
}

func clearBox() error {
	sbieTerminateBox()
	for _, pid := range sbieBoxPids() {
		_ = sbieKillPid(pid)
	}
	waitBoxEmpty(10 * time.Second)
	boxRoot := sandboxBoxRoot()
	if err := os.RemoveAll(boxRoot); err != nil {
		return fmt.Errorf("清空盒子失败：%w", err)
	}
	_ = os.MkdirAll(boxRoot, 0o755)
	sbieBoxSizeCache.Lock()
	sbieBoxSizeCache.v = 0
	sbieBoxSizeCache.at = time.Now()
	sbieBoxSizeCache.Unlock()
	boxAppsMu.Lock()
	saveBoxApps(map[string]boxApp{})
	boxAppsMu.Unlock()
	sbieBoxOp.Lock()
	sbieBoxOp.Pending = nil
	sbieBoxOp.Candidates = nil
	sbieBoxOp.Unlock()
	sbieBoxSet("idle", "")
	logf("sandbox", "warn", "盒子已清空：盒内全部应用与数据删除（Data/sandbox/%s 已重建）", sbieBoxName)
	return nil
}

var sbieBoxSizeCache struct {
	sync.Mutex
	at time.Time
	v  int64
}

// sandboxBoxBytes is dirSizeBytes over the box root behind a 10 s
// cache: the status endpoint is polled every second or two by the
// settings/store pages, and a populated box holds tens of
// thousands of files.
func sandboxBoxBytes() int64 {
	sbieBoxSizeCache.Lock()
	defer sbieBoxSizeCache.Unlock()
	if time.Since(sbieBoxSizeCache.at) < 10*time.Second {
		return sbieBoxSizeCache.v
	}
	sbieBoxSizeCache.v = dirSizeBytes(sandboxBoxRoot())
	sbieBoxSizeCache.at = time.Now()
	return sbieBoxSizeCache.v
}

// dirSizeBytes walks a directory tree summing file sizes.
func dirSizeBytes(root string) int64 {
	var total int64
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

// ---------- status ----------

// boxAppView is a registry entry plus what the UI needs to
// badge it: Sealed = the app is covered by a data-seal
// template (v0.42), i.e. its data roots exist only inside the
// box and the host's copy is unreadable to it.
type boxAppView struct {
	boxApp
	Sealed bool `json:"sealed"`
	// Ghost marks a dead registration (v0.43): the file it
	// names is gone, or the path is a tool/temp shape that
	// was never the app. The settings list badges it so the
	// user can remove it without having to click into the
	// failure first.
	Ghost bool `json:"ghost,omitempty"`
}

func boxAppViews(apps []boxApp) []boxAppView {
	out := make([]boxAppView, 0, len(apps))
	boxRoot := sandboxBoxRoot()
	for _, a := range apps {
		ghost := isJunkCandidateShape(a.Exe)
		if !ghost {
			_, err := os.Stat(virtualToPhysical(boxRoot, a.Exe))
			ghost = err != nil
		}
		out = append(out, boxAppView{boxApp: a, Sealed: appSealedByExe(virtualBase(a.Exe)), Ghost: ghost})
	}
	return out
}

func sandboxStatusMap() map[string]any {
	svc, drv, ver := sbiePlatformStatus()
	installed := sbieStartExePath() != ""
	meta := readComponentMeta()
	cPhase, cMsg, cSource, cProg, cSize := sbieCompSnapshot()
	sbieBoxOp.Lock()
	bPhase, bMsg, bKind, bInst := sbieBoxOp.Phase, sbieBoxOp.Message, sbieBoxOp.Kind, sbieBoxOp.Installer
	bProg, bSize := sbieBoxOp.Progress, sbieBoxOp.Size
	bStarted := sbieBoxOp.StartedAt
	// v0.43: candidate liveness is recomputed on EVERY status
	// read — a file that vanishes while the registration dialog
	// is open must be struck through in that dialog, not
	// discovered at registration (or worse, at launch).
	cands := make([]boxExeCand, len(sbieBoxOp.Candidates))
	copy(cands, sbieBoxOp.Candidates)
	sbieBoxOp.Unlock()
	for i := range cands {
		cands[i].Exists = candidateExists(cands[i])
	}
	if ver == "" {
		ver = meta.Version
	}
	boxAppsMu.Lock()
	apps := sortedBoxApps(loadBoxApps())
	boxAppsMu.Unlock()
	src := meta.Source
	if src == "" {
		src = cSource
	}
	return map[string]any{
		"supported":     runtime.GOOS == "windows",
		"installed":     installed,
		"version":       ver,
		"pinnedVersion": sbieVersion,
		"serviceState":  svc,
		"driverState":   drv,
		"boxRoot":       sandboxBoxRoot(),
		"boxBytes":      sandboxBoxBytes(),
		"iniPlaced":     iniPlacedCurrent(),
		"metaSource":    src,
		"metaAt":        meta.At,
		"component": map[string]any{
			"phase": cPhase, "message": cMsg, "source": cSource,
			"progress": cProg, "size": cSize, "busy": sbieCompBusy(),
		},
		"boxInstall": map[string]any{
			"phase": bPhase, "message": bMsg, "kind": bKind, "installer": bInst,
			"progress": bProg, "size": bSize, "busy": sbieBoxBusy(),
			"startedAt":  bStarted,
			"candidates": cands,
		},
		"apps": boxAppViews(apps),
	}
}

// ---------- HTTP handlers (local mux only) ----------

func sandboxStatusHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, sandboxStatusMap())
}

func sandboxAppsHandler(w http.ResponseWriter, r *http.Request) {
	boxAppsMu.Lock()
	apps := sortedBoxApps(loadBoxApps())
	boxAppsMu.Unlock()
	type appView struct {
		boxApp
		Present bool `json:"present"`
		Sealed  bool `json:"sealed"`
	}
	views := make([]appView, 0, len(apps))
	for _, a := range apps {
		phys := virtualToPhysical(sandboxBoxRoot(), a.Exe)
		_, err := os.Stat(phys)
		views = append(views, appView{boxApp: a, Present: err == nil, Sealed: appSealedByExe(virtualBase(a.Exe))})
	}
	writeJSON(w, map[string]any{"apps": views})
}

func sandboxInstallComponentHandler(w http.ResponseWriter, r *http.Request) {
	if runtime.GOOS != "windows" {
		writeJSON(w, map[string]any{"error": "当前不是 Windows 环境（这是测试环境），组件安装要在 Windows 上用"})
		return
	}
	if sbieCompBusy() {
		writeJSON(w, map[string]any{"ok": true, "message": "组件操作进行中"})
		return
	}
	if sbieStartExePath() != "" {
		writeJSON(w, map[string]any{"error": "组件已经装好了"})
		return
	}
	logf("sandbox", "info", "开始安装应用隔离组件（Sandboxie Plus v%s）", sbieVersion)
	go runComponentInstall()
	writeJSON(w, map[string]any{"ok": true, "message": "开始安装"})
}

func sandboxUninstallComponentHandler(w http.ResponseWriter, r *http.Request) {
	if sbieCompBusy() {
		writeJSON(w, map[string]any{"error": "组件操作进行中，等它结束"})
		return
	}
	if sbieStartExePath() == "" {
		writeJSON(w, map[string]any{"error": "组件本来就没装"})
		return
	}
	logf("sandbox", "info", "开始卸载应用隔离组件")
	go runComponentUninstall()
	writeJSON(w, map[string]any{"ok": true, "message": "开始卸载"})
}

func sandboxRepairHandler(w http.ResponseWriter, r *http.Request) {
	msg, err := repairComponent()
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "message": msg})
}

// sandboxImportHandler receives one local installer file (multipart
// form field "file") into Data/sandbox/incoming -- the same
// user-carries-it-in shape as the v0.32 file import.
func sandboxImportHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, map[string]any{"error": "method not allowed"})
		return
	}
	mr, err := r.MultipartReader()
	if err != nil {
		writeJSON(w, map[string]any{"error": "上传格式不对"})
		return
	}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		rawName := part.FileName()
		if rawName == "" {
			continue
		}
		name := filepath.Base(strings.ReplaceAll(rawName, "\\", "/"))
		if !strings.EqualFold(filepath.Ext(name), ".exe") && !strings.EqualFold(filepath.Ext(name), ".msi") {
			_, _ = io.Copy(io.Discard, part)
			writeJSON(w, map[string]any{"error": "只收 .exe / .msi 安装包"})
			return
		}
		dst := uniqueIncomingPath(name)
		f, err := os.Create(dst)
		if err != nil {
			writeJSON(w, map[string]any{"error": "存不下这个文件：" + err.Error()})
			return
		}
		n, cerr := io.Copy(f, io.LimitReader(part, (1536+1)*1024*1024))
		_ = f.Close()
		if cerr != nil || n > 1536*1024*1024 {
			_ = os.Remove(dst)
			writeJSON(w, map[string]any{"error": "文件太大或上传中断（上限 1.5 GB）"})
			return
		}
		logf("sandbox", "info", "本地安装包已导入：%s（%.1f MB）", filepath.Base(dst), float64(n)/1048576)
		writeJSON(w, map[string]any{"ok": true, "name": filepath.Base(dst), "size": n})
		return
	}
	writeJSON(w, map[string]any{"error": "没收到文件"})
}

// uniqueIncomingPath applies the house (2)-style collision rename.
func uniqueIncomingPath(name string) string {
	dir := sandboxIncomingDir()
	dst := filepath.Join(dir, name)
	if _, err := os.Stat(dst); err != nil {
		return dst
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 2; ; i++ {
		p := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
		if _, err := os.Stat(p); err != nil {
			return p
		}
	}
}

func sandboxBoxInstallHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL    string `json:"url"`
		SHA256 string `json:"sha256"`
		File   string `json:"file"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if sbieBoxBusy() {
		writeJSON(w, map[string]any{"error": "已经有一单隔离安装在跑了"})
		return
	}
	// v0.43: a pending registration also blocks a new install —
	// starting one would clobber the choice the user still owes
	// (the pending state is persisted now, so it would survive
	// everything except exactly this).
	sbieBoxOp.Lock()
	choosing := sbieBoxOp.Phase == "choosing"
	sbieBoxOp.Unlock()
	if choosing {
		writeJSON(w, map[string]any{"error": "上一个隔离安装还等着你确认主程序（在隔离安装区选好登记，或点「先不登记」放弃），处理完再开始新的"})
		return
	}
	if sbieStartExePath() == "" {
		writeJSON(w, map[string]any{"error": "应用隔离组件还没装，先去 设置 → 应用隔离 装组件"})
		return
	}
	if body.File != "" {
		logf("sandbox", "info", "新的隔离安装：本地导入的安装包 %s", body.File)
	} else {
		logf("sandbox", "info", "新的隔离安装：从 %s 下载", urlHost(body.URL))
	}
	go runBoxInstall(strings.TrimSpace(body.URL), strings.TrimSpace(body.SHA256), strings.TrimSpace(body.File))
	writeJSON(w, map[string]any{"ok": true, "message": "开始隔离安装"})
}

func sandboxBoxRegisterHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
		Exe  string `json:"exe"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	app, err := registerBoxApp(strings.TrimSpace(body.Name), strings.TrimSpace(body.Exe))
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "app": app})
}

// sandboxBoxDiscardHandler drops a pending registration (the user
// decided not to register what an install left in the box; the
// files stay in the box, unregistered, until the box is cleared).
func sandboxBoxDiscardHandler(w http.ResponseWriter, r *http.Request) {
	sbieBoxOp.Lock()
	sbieBoxOp.Pending = nil
	sbieBoxOp.Candidates = nil
	sbieBoxOp.Unlock()
	clearPendingInstallFile()
	sbieBoxSet("idle", "")
	logf("sandbox", "info", "用户放弃登记这次隔离安装的结果（文件留盒内，未上桌面）")
	writeJSON(w, map[string]any{"ok": true})
}

// sandboxBoxStopHandler cancels the CURRENT install only
// (v0.43): it raises the flag and the install goroutine kills
// just its session's PID set. v0.42 terminated the WHOLE box,
// taking every other boxed app down with the install.
func sandboxBoxStopHandler(w http.ResponseWriter, r *http.Request) {
	sbieBoxCancel.Store(true)
	logf("sandbox", "warn", "用户终止本次隔离安装（只终止本会话进程，其他盒内应用不受影响）")
	writeJSON(w, map[string]any{"ok": true})
}

func sandboxLaunchHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	boxAppsMu.Lock()
	app, ok := loadBoxApps()[body.ID]
	boxAppsMu.Unlock()
	if !ok {
		writeJSON(w, map[string]any{"error": "没有这个隔离应用"})
		return
	}
	if err := launchBoxApp(app); err != nil {
		logf("sandbox", "error", "启动 %s 失败：%v", app.Name, err)
		var ghost *boxGhostError
		if errors.As(err, &ghost) {
			writeJSON(w, map[string]any{"error": err.Error(), "ghost": true, "id": app.ID})
			return
		}
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func sandboxRemoveHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	boxAppsMu.Lock()
	app, ok := loadBoxApps()[body.ID]
	boxAppsMu.Unlock()
	if !ok {
		writeJSON(w, map[string]any{"error": "没有这个隔离应用"})
		return
	}
	if err := removeBoxApp(app); err != nil {
		logf("sandbox", "error", "移除 %s 失败：%v", app.Name, err)
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "message": "已移除：" + app.Name})
}

func sandboxClearHandler(w http.ResponseWriter, r *http.Request) {
	if sbieBoxBusy() {
		writeJSON(w, map[string]any{"error": "有隔离安装在跑，先终止它"})
		return
	}
	if err := clearBox(); err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "message": "盒子已清空"})
}

// sandboxInit runs at daemon start: when the component is present,
// make sure the box points at THIS machine's Data folder (the whole
// folder may have just arrived from another PC). Placement here is
// the no-UAC attempt only -- a stale box surfaces as a warning and
// a one-click 修复 in settings, never as a surprise prompt at boot.
func sandboxInit() {
	go func() {
		if runtime.GOOS != "windows" || sbieStartExePath() == "" {
			return
		}
		if placed, _ := ensureBoxPlaced(); placed {
			logf("sandbox", "info", "应用隔离组件就绪：盒子指向当前 Data（%s）", sandboxBoxRoot())
			logf("sandbox", "info", "盒内窗口的 Sandboxie 黄色边框已在盒子配置里关闭（BorderColor 按官方键设为 off；窗口归属看台前调度）")
		} else {
			logf("sandbox", "warn", "应用隔离：盒子配置与当前 Data 位置不一致（多半是刚换了电脑或挪了文件夹），去 设置 → 应用隔离 点「修复」")
		}
		// v0.43: a pending registration survives the restart —
		// rebuild it against the current box and put the
		// "choosing" step back in front of the user.
		restorePendingFrom(pendingInstallPath(), sandboxBoxRoot())
	}()
}
