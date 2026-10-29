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
// replaced by the canonical one for boxRoot, every other byte of
// user content preserved. Pure and idempotent: merging the result
// again with the same boxRoot reports changed=false. Section
// headers match case-insensitively, as Sandboxie itself treats
// them.
func mergeBoxSection(text, boxRoot string) (string, bool) {
	section := "[" + sbieBoxName + "]\r\n" +
		"Enabled=y\r\n" +
		"FileRootPath=" + boxRoot + "\r\n" +
		"AutoRecover=n\r\n"
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
// our section pointing at the CURRENT box root.
func iniHasCurrentBox(text, boxRoot string) bool {
	merged, changed := mergeBoxSection(text, boxRoot)
	_ = merged
	return !changed
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

// rankBoxCandidates orders the exes an install left behind so the
// most likely MAIN program comes first (pure). Uninstallers are
// excluded outright; setup/update/helper-looking names sink; a
// Program Files location and a name resembling the installer's
// float to the top.
func rankBoxCandidates(cands []boxExeCand, installerBase string) []boxExeCand {
	hint := installerHint(installerBase)
	score := func(c boxExeCand) int {
		vp := strings.ReplaceAll(c.Virtual, "/", `\`)
		segs := strings.Split(vp, `\`)
		stem := strings.ToLower(virtualStem(c.Virtual))
		s := 0
		low := strings.ToLower(vp)
		switch {
		case strings.Contains(low, `\program files\`) || strings.Contains(low, `\program files (x86)\`):
			s += 120
		case strings.Contains(low, `\appdata\local\programs\`):
			s += 70
		case strings.Contains(low, `\programdata\`):
			s += 40
		}
		if hint != "" && len(stem) >= 3 && (strings.Contains(stem, hint) || strings.Contains(hint, stem)) {
			s += 50
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

// parseBoxPids extracts PIDs from Start.exe /listpids output
// (pure).
func parseBoxPids(out string) []int {
	var pids []int
	seen := map[int]bool{}
	for _, tok := range strings.FieldsFunc(out, func(r rune) bool {
		return r < '0' || r > '9'
	}) {
		if tok == "" {
			continue
		}
		n, err := strconv.Atoi(tok)
		if err == nil && n > 0 && !seen[n] {
			seen[n] = true
			pids = append(pids, n)
		}
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
}

var sbieBoxOp = struct {
	sync.Mutex
	Phase      string // idle|preparing|downloading|installing|choosing|done|error
	Message    string
	Kind       string
	Installer  string
	Progress   int64
	Size       int64
	Candidates []boxExeCand
	Pending    *sbiePendingInstall
}{Phase: "idle"}

func sbieBoxSet(phase, msg string) {
	sbieBoxOp.Lock()
	sbieBoxOp.Phase, sbieBoxOp.Message = phase, msg
	sbieBoxOp.Unlock()
}

func sbieBoxBusy() bool {
	sbieBoxOp.Lock()
	defer sbieBoxOp.Unlock()
	return sbieBoxOp.Phase == "preparing" || sbieBoxOp.Phase == "downloading" || sbieBoxOp.Phase == "installing"
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
	if iniHasCurrentBox(text, sandboxBoxRoot()) {
		return true, false
	}
	merged, _ := mergeBoxSection(text, sandboxBoxRoot())
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
	return iniHasCurrentBox(text, sandboxBoxRoot())
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
	merged, _ := mergeBoxSection(text, boxRoot)
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

func sbieBoxPids() []int {
	out, err := sbieRun("/box:"+sbieBoxName, "/listpids")
	if err != nil {
		return nil
	}
	return parseBoxPids(out)
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
	args := []string{"/box:" + sbieBoxName, "/wait", pkgPath}
	if sa := silentArgsFor(kind); sa != nil {
		args = append(args, sa...)
		sbieBoxSet("installing", fmt.Sprintf("正在盒内静默安装 %s（%s）…", installerName, strings.ToUpper(kind)))
	} else {
		sbieBoxSet("installing", fmt.Sprintf("已在盒内打开 %s 的安装界面，在那个窗口里点完安装（全程在盒子里）…", installerName))
	}
	logf("sandbox", "info", "盒内安装开始：%s（类型 %s，静默参数 %v）", installerName, orDash(kind), silentArgsFor(kind))
	start := time.Now()
	cmd := exec.Command(sbieStartExePath(), args...)
	hideConsole(cmd)
	runErr := cmd.Run()
	// The starter can return while children finish up; wait for the
	// box to drain before judging the result.
	drainDeadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(drainDeadline) && !sbieBoxCancel.Load() {
		if len(sbieBoxPids()) == 0 {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if sbieBoxCancel.Load() {
		sbieTerminateBox()
		sbieBoxSet("idle", "已终止")
		logf("sandbox", "warn", "盒内安装被终止：%s", installerName)
		return
	}
	if runErr != nil {
		logf("sandbox", "warn", "盒内安装启动器返回：%v（继续按盒内结果核对）", runErr)
	}
	after := snapshotBoxExes(boxRoot)
	var fresh []boxExeCand
	for _, c := range after {
		if !beforeExes[strings.ToLower(c.Physical)] {
			fresh = append(fresh, c)
		}
	}
	ranked := rankBoxCandidates(fresh, installerName)
	logf("sandbox", "info", "盒内安装结束：%s 用时 %s，盒内新增 exe %d 个，候选 %d 个",
		installerName, time.Since(start).Round(time.Second), len(fresh), len(ranked))
	if len(ranked) == 0 {
		fail("安装跑完了，但盒子里没找到新装的程序——可能安装失败、装到了盒子外，或这个安装包不往 Program Files 放程序。安装包还留在 Data/sandbox/incoming，可在控制台看日志。")
		return
	}
	sbieBoxOp.Lock()
	sbieBoxOp.Candidates = ranked
	sbieBoxOp.Pending = &sbiePendingInstall{
		InstallerName: installerName,
		BeforeExes:    beforeExes,
		BeforeAppData: beforeAppData,
	}
	sbieBoxOp.Unlock()
	sbieBoxSet("choosing", "装完了。确认一下哪个是主程序、叫什么名字，登记后图标就上桌面。")
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
	sbieBoxSet("done", "已登记到桌面："+name)
	logf("sandbox", "info", "盒内应用已登记：%s（%s，数据目录 %d 个）", name, virtualExe, len(appDataV))
	return app, nil
}

// ---------- boxed app launch / remove / box clear ----------

func launchBoxApp(app boxApp) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("当前不是 Windows 环境（这是测试环境），到 Windows 上会正常打开")
	}
	se := sbieStartExePath()
	if se == "" {
		return fmt.Errorf("应用隔离组件还没装，先去 设置 → 应用隔离 装组件")
	}
	if !iniPlacedCurrent() {
		if placed, _ := ensureBoxPlaced(); !placed {
			return fmt.Errorf("盒子配置与当前 Data 位置对不上，去 设置 → 应用隔离 点一次「修复」")
		}
	}
	physExe := virtualToPhysical(sandboxBoxRoot(), app.Exe)
	if _, err := os.Stat(physExe); err != nil {
		return fmt.Errorf("盒子里找不到这个程序了（盒子可能被清空过），请移除后重新隔离安装")
	}
	before := platformSnapshotWindows(physExe)
	cmd := exec.Command(se, "/box:"+sbieBoxName, app.Exe)
	hideConsole(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动失败：%w", err)
	}
	noteSpawnedProcess(uint32(cmd.Process.Pid), app.Name)
	logf("sandbox", "info", "盒内启动：%s", app.Name)
	trackAppLaunch(app.Name, physExe, before)
	return nil
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

func sandboxStatusMap() map[string]any {
	svc, drv, ver := sbiePlatformStatus()
	installed := sbieStartExePath() != ""
	meta := readComponentMeta()
	cPhase, cMsg, cSource, cProg, cSize := sbieCompSnapshot()
	sbieBoxOp.Lock()
	bPhase, bMsg, bKind, bInst := sbieBoxOp.Phase, sbieBoxOp.Message, sbieBoxOp.Kind, sbieBoxOp.Installer
	bProg, bSize := sbieBoxOp.Progress, sbieBoxOp.Size
	cands := sbieBoxOp.Candidates
	sbieBoxOp.Unlock()
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
			"candidates": cands,
		},
		"apps": apps,
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
	}
	views := make([]appView, 0, len(apps))
	for _, a := range apps {
		phys := virtualToPhysical(sandboxBoxRoot(), a.Exe)
		_, err := os.Stat(phys)
		views = append(views, appView{boxApp: a, Present: err == nil})
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
	sbieBoxSet("idle", "")
	logf("sandbox", "info", "用户放弃登记这次隔离安装的结果（文件留盒内，未上桌面）")
	writeJSON(w, map[string]any{"ok": true})
}

func sandboxBoxStopHandler(w http.ResponseWriter, r *http.Request) {
	sbieBoxCancel.Store(true)
	sbieTerminateBox()
	logf("sandbox", "warn", "用户终止了盒内进程")
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
		} else {
			logf("sandbox", "warn", "应用隔离：盒子配置与当前 Data 位置不一致（多半是刚换了电脑或挪了文件夹），去 设置 → 应用隔离 点「修复」")
		}
	}()
}
