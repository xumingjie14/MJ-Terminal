package main

// taskmgr.go -- v0.69 block 1: the terminal's own Task Manager
// （任务管理器）, benchmarked against macOS Activity Monitor and
// the Windows Task Manager details page, but scoped to the one
// process tree this product may honestly claim: MJ Terminal's
// own.
//
// Scope (three layers, matching the page):
//   1. in-shell windows/apps -- pure view state, the frontend
//      renders and closes those itself through the existing
//      window-close path; nothing here.
//   2. daemon-launched / daemon-managed external processes --
//      THIS file. Attribution reuses the patrol's ownership
//      engine (owncheck.go classifyOwnership with the same
//      context the patrol builds): only processes the daemon
//      spawned (spawn ledger), processes running our profiles,
//      executables under the terminal folder / Data, and
//      Sandboxie box members are ever listed. Host processes
//      are never listed, never touched.
//   3. the daemon itself -- one status row (version / uptime /
//      own CPU+memory), deliberately NO kill action: its only
//      exit is 设置 → 退出系统.
//
// Data: per-process CPU% (delta of kernel+user time between
// samples) and working set, aggregated per app group. The
// frontend polls GET /api/taskmgr once a second while the
// menu-bar panel or the 任务管理器 window is open and stops
// when it closes, so sampling cost exists only while someone
// is looking. Registered windows are probed with Win32's
// IsHungAppWindow and flagged 「无响应」.
//
// Actions: end a single process or a whole group (children
// first). The core group ends through the existing
// stopCoreLocked path and restarts through restartCoreLocked;
// the browser group is end-only (there is no existing browser
// restart path and none is invented here). Every action is
// logged under [taskmgr].

import (
	"encoding/json"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// tmDaemonStart approximates the daemon's birth (package init
// runs within milliseconds of process start); the daemon row's
// uptime is measured from it.
var tmDaemonStart = time.Now()

// ---------- CPU sampler ----------

type tmSampleKey struct {
	pid    uint32
	create int64 // process creation time (100ns units): a PID-reuse discriminator
}

type tmSample struct {
	cpu int64 // cumulative kernel+user time, 100ns units
	at  time.Time
}

type tmSamplerT struct {
	mu   sync.Mutex
	last map[tmSampleKey]tmSample
}

var tmSampler = &tmSamplerT{last: map[tmSampleKey]tmSample{}}

// observe records one usage sample and returns the CPU percent
// since the previous sample of the same process (0 on first
// sight). Percent is of TOTAL machine capacity, Task-Manager
// style: a process saturating one of 8 cores reads 12.5%.
func (s *tmSamplerT) observe(pid uint32, cpu, create int64, now time.Time) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := tmSampleKey{pid: pid, create: create}
	prev, ok := s.last[k]
	s.last[k] = tmSample{cpu: cpu, at: now}
	if !ok || !now.After(prev.at) || cpu < prev.cpu {
		return 0
	}
	wall100 := now.Sub(prev.at).Nanoseconds() / 100
	if wall100 <= 0 {
		return 0
	}
	nc := int64(runtime.NumCPU())
	if nc < 1 {
		nc = 1
	}
	pct := float64(cpu-prev.cpu) / float64(wall100) / float64(nc) * 100
	return math.Round(pct*10) / 10
}

// prune drops samples for processes that left the snapshot so
// the map cannot grow across a long daemon life.
func (s *tmSamplerT) prune(alive map[tmSampleKey]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := range s.last {
		if !alive[k] {
			delete(s.last, k)
		}
	}
}

// ---------- grouping ----------

// tmClassCtx carries everything root classification needs, in
// pure data, so tests can drive it without a live daemon.
type tmClassCtx struct {
	CorePID        uint32
	CoreExe        string // sing-box image path
	ChromeProfile  string
	FFAppProfile   string
	FFWinProfile   string
	BoxPIDs        map[uint32]bool
	AppsRoot       string // Data/apps
	CompRoot       string // Data/components
	OpenClawDir    string // Data/openclaw
	DirGroups      []tmDirGroup
	ShellExclusion map[uint32]bool // shell-view family: never listed
}

type tmDirGroup struct {
	Prefix string // directory the group's executables live under
	ID     string // group id suffix
	Name   string
	Kind   string
}

// tmRootClass classifies ONE process by its own facts alone
// (no ancestry): the group it is a root of, or "" when the
// process only belongs through a classified ancestor.
func tmRootClass(p procFact, cx tmClassCtx) string {
	if cx.ShellExclusion[p.PID] {
		return ""
	}
	if cx.CorePID != 0 && p.PID == cx.CorePID {
		return "core"
	}
	if cx.CoreExe != "" && p.ImagePath != "" && normalizeWinPath(p.ImagePath) == normalizeWinPath(cx.CoreExe) {
		return "core"
	}
	// Firefox app profile FIRST: its path has the window
	// profile's path as a prefix ("FirefoxProfileApp" vs
	// "FirefoxProfile"), so order decides correctness.
	if cx.FFAppProfile != "" && cmdlineHasProfile(p.CmdLine, cx.FFAppProfile) {
		return "firefox-app"
	}
	if cx.FFWinProfile != "" && cmdlineHasProfile(p.CmdLine, cx.FFWinProfile) {
		return "firefox-window"
	}
	if cx.ChromeProfile != "" && cmdlineHasProfile(p.CmdLine, cx.ChromeProfile) {
		return "browser"
	}
	if cx.BoxPIDs[p.PID] {
		return "sandbox"
	}
	if p.ImagePath != "" {
		if cx.OpenClawDir != "" && pathUnderDir(p.ImagePath, cx.OpenClawDir) {
			return "openclaw"
		}
		for _, dg := range cx.DirGroups {
			if pathUnderDir(p.ImagePath, dg.Prefix) {
				return dg.Kind + "-" + dg.ID
			}
		}
	}
	return ""
}

// tmGroupMeta is the static presentation + policy of one group.
type tmGroupMeta struct {
	Name       string
	Kind       string
	Critical   bool // UI confirms before ending (core, whole browser)
	CanRestart bool // an existing restart path exists
}

func tmMetaFor(gid string, cx tmClassCtx) tmGroupMeta {
	switch {
	case gid == "core":
		return tmGroupMeta{Name: "节点核心 sing-box", Kind: "core", Critical: true, CanRestart: true}
	case gid == "browser":
		return tmGroupMeta{Name: "隔离 Chrome", Kind: "browser", Critical: true}
	case gid == "firefox-window":
		return tmGroupMeta{Name: "Firefox（窗口版）", Kind: "firefox"}
	case gid == "firefox-app":
		return tmGroupMeta{Name: "Firefox（应用化）", Kind: "firefox"}
	case gid == "openclaw":
		return tmGroupMeta{Name: "小龙虾 OpenClaw", Kind: "openclaw"}
	case gid == "sandbox":
		return tmGroupMeta{Name: "隔离应用（沙盒）", Kind: "sandbox"}
	case strings.HasPrefix(gid, "component-"):
		id := strings.TrimPrefix(gid, "component-")
		for _, dg := range cx.DirGroups {
			if dg.Kind == "component" && dg.ID == id {
				return tmGroupMeta{Name: dg.Name, Kind: "component"}
			}
		}
		return tmGroupMeta{Name: id, Kind: "component"}
	case strings.HasPrefix(gid, "store-"):
		id := strings.TrimPrefix(gid, "store-")
		for _, dg := range cx.DirGroups {
			if dg.Kind == "store" && dg.ID == id {
				return tmGroupMeta{Name: dg.Name, Kind: "store"}
			}
		}
		return tmGroupMeta{Name: id, Kind: "store"}
	default:
		return tmGroupMeta{Name: gid, Kind: "other"}
	}
}

// tmAssignGroups maps every owned PID to its group: a process
// belongs to the group of its nearest classified ancestor
// (itself included). Owned PIDs with no classified ancestor at
// all group under their topmost owned ancestor's image name --
// they are still ours (the ownership engine said so), just not
// one of the named families.
func tmAssignGroups(procs []procFact, owned map[uint32]string, cx tmClassCtx) map[uint32]string {
	byPID := make(map[uint32]procFact, len(procs))
	for _, p := range procs {
		byPID[p.PID] = p
	}
	out := make(map[uint32]string)
	var resolve func(pid uint32, depth int) string
	resolve = func(pid uint32, depth int) string {
		if g, ok := out[pid]; ok {
			return g
		}
		p, ok := byPID[pid]
		if !ok || depth > 64 {
			return ""
		}
		if g := tmRootClass(p, cx); g != "" {
			out[pid] = g
			return g
		}
		if p.PPID != 0 && p.PPID != pid {
			if _, isOwned := owned[p.PPID]; isOwned {
				if g := resolve(p.PPID, depth+1); g != "" {
					out[pid] = g
					return g
				}
			}
		}
		// No classified ancestor: group by this process's own
		// image (the topmost owned ancestor gets here first via
		// the recursion from its children, so they share it).
		base := tmImageBase(p.ImagePath)
		if base == "" {
			base = "unknown"
		}
		g := "other-" + base
		out[pid] = g
		return g
	}
	for pid := range owned {
		resolve(pid, 0)
	}
	return out
}

// tmKillOrder returns the given PID set in kill order:
// children strictly before their parents (a browser that is
// killed parent-first can respawn or orphan children mid-
// teardown; leaf-first is the order Task Manager uses).
func tmKillOrder(procs []procFact, set map[uint32]bool) []uint32 {
	depth := map[uint32]int{}
	byPID := make(map[uint32]procFact, len(procs))
	for _, p := range procs {
		byPID[p.PID] = p
	}
	var depthOf func(pid uint32, guard int) int
	depthOf = func(pid uint32, guard int) int {
		if d, ok := depth[pid]; ok {
			return d
		}
		d := 0
		if p, ok := byPID[pid]; ok && guard < 64 {
			if _, inSet := set[p.PPID]; inSet && p.PPID != pid {
				d = depthOf(p.PPID, guard+1) + 1
			}
		}
		depth[pid] = d
		return d
	}
	out := make([]uint32, 0, len(set))
	for pid := range set {
		out = append(out, pid)
	}
	sort.Slice(out, func(i, j int) bool {
		di, dj := depthOf(out[i], 0), depthOf(out[j], 0)
		if di != dj {
			return di > dj
		}
		return out[i] < out[j]
	})
	return out
}

// tmDescendants returns pid plus every descendant of it within
// the given process list (the kill set for "end this process").
func tmDescendants(procs []procFact, root uint32) map[uint32]bool {
	set := map[uint32]bool{root: true}
	for changed := true; changed; {
		changed = false
		for _, p := range procs {
			if set[p.PPID] && !set[p.PID] {
				set[p.PID] = true
				changed = true
			}
		}
	}
	return set
}

// ---------- collection ----------

// tmClassCtxLive builds the classification context from the
// daemon's live registries, mirroring the patrol's inputs.
func tmClassCtxLive(procs []procFact, facts []windowFact) tmClassCtx {
	// Shell family exclusion, computed exactly like the patrol:
	// registered shell windows' PIDs plus their descendants.
	shellPIDs := map[uint32]bool{}
	extMu.Lock()
	for _, e := range extWins {
		if e.Shell && e.pid != 0 {
			shellPIDs[e.pid] = true
		}
	}
	extMu.Unlock()
	if len(shellPIDs) > 0 {
		for changed := true; changed; {
			changed = false
			for _, p := range procs {
				if shellPIDs[p.PPID] && !shellPIDs[p.PID] {
					shellPIDs[p.PID] = true
					changed = true
				}
			}
		}
	}
	// Directory groups: one per installed store app / component,
	// keyed by the directory its executables live under.
	var dgs []tmDirGroup
	reg := loadAppsRegistry()
	embeddedCatalogOnce.once.Do(func() {
		embeddedCatalogOnce.apps, _ = parseCatalog(embeddedAppsJSON)
	})
	for _, a := range embeddedCatalogOnce.apps {
		if appInstalled(a, reg) {
			dgs = append(dgs, tmDirGroup{Prefix: appDir(a.ID), ID: a.ID, Name: a.Name, Kind: "store"})
		}
	}
	for _, c := range prodComponents {
		if compInstalled(c) {
			dgs = append(dgs, tmDirGroup{Prefix: compDir(c.ID), ID: c.ID, Name: c.Name, Kind: "component"})
		}
	}
	var corePID uint32
	coreMu.Lock()
	if coreCmd != nil && coreCmd.Process != nil {
		corePID = uint32(coreCmd.Process.Pid)
	}
	coreMu.Unlock()
	ffDirs := ffProfileDirs()
	cx := tmClassCtx{
		CorePID:        corePID,
		CoreExe:        filepath.Join(exeDir(), "core", "sing-box.exe"),
		ChromeProfile:  filepath.Join(dataDir(), "ChromeProfile"),
		BoxPIDs:        map[uint32]bool{},
		AppsRoot:       appsRoot(),
		CompRoot:       compRoot(),
		OpenClawDir:    filepath.Join(dataDir(), "openclaw"),
		DirGroups:      dgs,
		ShellExclusion: shellPIDs,
	}
	if len(ffDirs) == 2 {
		cx.FFWinProfile, cx.FFAppProfile = ffDirs[0], ffDirs[1]
	}
	for pid := range refreshBoxPIDsForPatrol() {
		cx.BoxPIDs[pid] = true
	}
	return cx
}

// tmOwnedSet runs the patrol's ownership engine with the
// patrol's inputs and returns the owned PID set (reason map),
// minus the shell family.
func tmOwnedSet(procs []procFact, facts []windowFact, cx tmClassCtx) map[uint32]string {
	alive := make(map[uint32]bool, len(procs))
	for _, p := range procs {
		alive[p.PID] = true
	}
	seeds := spawnSeedPIDs(alive)
	extMu.Lock()
	for _, e := range extWins {
		if !e.Shell && e.pid != 0 {
			seeds[e.pid] = true
		}
	}
	extMu.Unlock()
	if cx.CorePID != 0 {
		seeds[cx.CorePID] = true
	}
	delete(seeds, uint32(os.Getpid()))
	profileDirs := []string{cx.ChromeProfile}
	if cx.FFWinProfile != "" {
		profileDirs = append(profileDirs, cx.FFWinProfile, cx.FFAppProfile)
	}
	owned := classifyOwnership(procs, seeds, ownContext{
		RootDir:     exeDir(),
		ProfileDirs: profileDirs,
		SelfPID:     uint32(os.Getpid()),
		BoxPIDs:     cx.BoxPIDs,
	})
	for pid := range owned {
		if cx.ShellExclusion[pid] {
			delete(owned, pid)
		}
	}
	return owned
}

// tmCollectGroups snapshots the process table and returns the
// group assignment -- shared by the read endpoint and by the
// kill endpoint (which re-validates its target against a FRESH
// assignment, so a stale page can never kill a PID the daemon
// does not currently own).
func tmCollectGroups() (procs []procFact, groups map[uint32]string, cx tmClassCtx) {
	procs = platformProcessSnapshot()
	facts := platformAllTopWindows()
	cx = tmClassCtxLive(procs, facts)
	owned := tmOwnedSet(procs, facts, cx)
	groups = tmAssignGroups(procs, owned, cx)
	return procs, groups, cx
}

func taskMgrSnapshot() taskMgrResponse {
	procs, assign, cx := tmCollectGroups()
	byPID := make(map[uint32]procFact, len(procs))
	for _, p := range procs {
		byPID[p.PID] = p
	}
	now := time.Now()
	alive := map[tmSampleKey]bool{}
	type gacc struct {
		meta  tmGroupMeta
		procs []taskMgrProc
		cpu   float64
		mem   uint64
	}
	accs := map[string]*gacc{}
	for pid, gid := range assign {
		p := byPID[pid]
		cpu100, create, ws, ok := platformProcUsage(pid)
		var cpu float64
		if ok {
			cpu = tmSampler.observe(pid, cpu100, create, now)
			alive[tmSampleKey{pid: pid, create: create}] = true
		}
		a := accs[gid]
		if a == nil {
			m := tmMetaFor(gid, cx)
			if m.Kind == "other" {
				m.Name = tmImageBase(p.ImagePath) + " · 终端进程"
				if p.ImagePath == "" {
					m.Name = "终端进程"
				}
			}
			a = &gacc{meta: m}
			accs[gid] = a
		}
		name := tmImageBase(p.ImagePath)
		if name == "" {
			name = "进程"
		}
		a.procs = append(a.procs, taskMgrProc{PID: pid, Name: name, CPU: cpu, Mem: ws})
		a.cpu += cpu
		a.mem += ws
	}
	tmSampler.prune(alive)
	// Registered windows attach to their process's group, with
	// the hung probe applied live.
	winByPID := map[uint32][]taskMgrWindow{}
	extMu.Lock()
	wins := make([]*extWindow, 0, len(extWins))
	for _, e := range extWins {
		if !e.Shell {
			cp := *e
			wins = append(wins, &cp)
		}
	}
	extMu.Unlock()
	for _, e := range wins {
		if e.pid == 0 {
			continue
		}
		if _, ok := assign[e.pid]; !ok {
			continue
		}
		winByPID[e.pid] = append(winByPID[e.pid], taskMgrWindow{
			HWND: e.HWND, App: e.App, Title: e.Title,
			Hung: platformWindowHung(e.HWND),
		})
	}
	out := taskMgrResponse{At: now.Unix()}
	for gid, a := range accs {
		g := taskMgrGroup{
			ID: gid, Name: a.meta.Name, Kind: a.meta.Kind,
			Critical: a.meta.Critical, CanRestart: a.meta.CanRestart,
			CPU: math.Round(a.cpu*10) / 10, Mem: a.mem,
		}
		sort.Slice(a.procs, func(i, j int) bool {
			if a.procs[i].Mem != a.procs[j].Mem {
				return a.procs[i].Mem > a.procs[j].Mem
			}
			return a.procs[i].PID < a.procs[j].PID
		})
		g.Procs = a.procs
		seen := map[uint64]bool{}
		for _, pr := range a.procs {
			for _, w := range winByPID[pr.PID] {
				if !seen[w.HWND] {
					seen[w.HWND] = true
					g.Windows = append(g.Windows, w)
				}
			}
		}
		if g.Windows == nil {
			g.Windows = []taskMgrWindow{}
		}
		out.Groups = append(out.Groups, g)
	}
	sort.Slice(out.Groups, func(i, j int) bool {
		if out.Groups[i].Mem != out.Groups[j].Mem {
			return out.Groups[i].Mem > out.Groups[j].Mem
		}
		return out.Groups[i].Name < out.Groups[j].Name
	})
	if out.Groups == nil {
		out.Groups = []taskMgrGroup{}
	}
	// Daemon row: own usage through the same sampler.
	self := uint32(os.Getpid())
	d := taskMgrDaemon{Version: AppVersion, PID: self, UptimeSec: int64(time.Since(tmDaemonStart).Seconds())}
	if cpu100, create, ws, ok := platformProcUsage(self); ok {
		d.CPU = tmSampler.observe(self, cpu100, create, now)
		d.Mem = ws
	}
	out.Daemon = d
	return out
}

// ---------- handlers ----------

func taskMgrHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, errCodeMethod, "只接受 GET")
		return
	}
	writeJSON(w, taskMgrSnapshot())
}

// tmKillPIDs kills the given PIDs children-first and reports
// (killed, firstError).
func tmKillPIDs(procs []procFact, set map[uint32]bool) (int, error) {
	killed := 0
	var firstErr error
	for _, pid := range tmKillOrder(procs, set) {
		proc, err := os.FindProcess(int(pid))
		if err == nil {
			err = proc.Kill()
		}
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		killed++
	}
	return killed, firstErr
}

func taskMgrKillHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, errCodeMethod, "只接受 POST")
		return
	}
	var body struct {
		Group string `json:"group"`
		PID   uint32 `json:"pid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apiErrBadRequest(w, errCodeBadJSON, "请求格式不对")
		return
	}
	procs, assign, cx := tmCollectGroups()
	// The core ends through its own lifecycle path, never a raw
	// kill: sing-box gets its clean shutdown and the node state
	// stays truthful.
	if body.Group == "core" || (body.PID != 0 && cx.CorePID != 0 && body.PID == cx.CorePID) {
		coreMu.Lock()
		running := coreRunningLocked()
		if running {
			stopCoreLocked()
		}
		coreMu.Unlock()
		if running {
			logf("taskmgr", "info", "任务管理器：已结束节点核心（走核心停止路径）")
			writeJSON(w, okResponse{OK: true})
		} else {
			writeJSON(w, map[string]any{"error": "核心当前没有在运行"})
		}
		return
	}
	var set map[uint32]bool
	var what string
	if body.Group != "" {
		set = map[uint32]bool{}
		for pid, gid := range assign {
			if gid == body.Group {
				set[pid] = true
			}
		}
		if len(set) == 0 {
			writeJSON(w, map[string]any{"error": "这个进程组已经不在运行了"})
			return
		}
		what = tmMetaFor(body.Group, cx).Name
	} else if body.PID != 0 {
		if _, ok := assign[body.PID]; !ok {
			// Never kill a PID the fresh ownership pass does not
			// attribute to the terminal -- a stale page must not
			// become a host task manager.
			writeJSON(w, map[string]any{"error": "这个进程不属于终端机（或已经退出），没有结束它"})
			return
		}
		set = tmDescendants(procs, body.PID)
		// Descendants outside the owned set are not ours to kill.
		for pid := range set {
			if _, ok := assign[pid]; !ok {
				delete(set, pid)
			}
		}
		what = "进程"
		for _, p := range procs {
			if p.PID == body.PID {
				what = "进程 " + tmImageBase(p.ImagePath)
			}
		}
	} else {
		apiErrBadRequest(w, errCodeBadRequest, "需要 group 或 pid")
		return
	}
	killed, kerr := tmKillPIDs(procs, set)
	if kerr != nil {
		logf("taskmgr", "warn", "结束%s：%d/%d 个进程已结束，其余失败：%v", what, killed, len(set), kerr)
		writeJSON(w, map[string]any{"error": "部分进程没能结束（可能已经退出或权限不足）"})
		return
	}
	logf("taskmgr", "info", "任务管理器：已结束%s（%d 个进程）", what, killed)
	writeJSON(w, okResponse{OK: true})
}

func taskMgrRestartHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, errCodeMethod, "只接受 POST")
		return
	}
	var body struct {
		Group string `json:"group"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apiErrBadRequest(w, errCodeBadJSON, "请求格式不对")
		return
	}
	if body.Group != "core" {
		// Only the core has an existing restart path. The
		// browser and everything else are end-only by design.
		apiErrBadRequest(w, errCodeInvalid, "这个进程组没有现成的重启路径，只能结束")
		return
	}
	coreMu.Lock()
	err := restartCoreLocked()
	coreMu.Unlock()
	if err != nil {
		logf("taskmgr", "error", "任务管理器：重启核心失败：%v", err)
		writeJSON(w, map[string]any{"error": "重启核心失败：" + err.Error()})
		return
	}
	logf("taskmgr", "info", "任务管理器：已重启节点核心")
	writeJSON(w, okResponse{OK: true})
}

// tmImageBase is the base name of a process image path (""
// stays ""). Separators are normalized first: process facts
// carry Windows paths, and this also runs in Linux CI.
func tmImageBase(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}
