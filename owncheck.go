package main

// ---------- v0.36: ownership classification (platform-neutral) ----------
//
// The v0.31 patrol watched only the PIDs of already-registered
// browser windows. Field testing showed the holes in that model: a
// browser that came up through an unregistered path, a store app
// relaunched by a stub, a PDF handed from the browser to an external
// viewer, a browser that crashed and respawned under a new PID --
// every hand-off broke the watch and the window never reached the
// Stage Manager, while sitting visibly on the host taskbar.
//
// v0.36 inverts the model. The patrol now inventories ALL visible
// top-level windows plus a full process snapshot, and classifies
// every process as terminal-owned or not from durable facts:
//
//	profile -- the process command line carries one of the isolated
//	           browser profile directories as a whole argument value
//	           (the isolated Chrome's Data/ChromeProfile, the
//	           Firefox component's profile dir). This is the hard
//	           fingerprint: the host's own browser processes cannot
//	           have it, whatever window they show.
//	exe     -- the process image lives under the terminal folder
//	           (store apps in Data/apps, the Firefox component in
//	           core/firefox, imported executables in Data/files).
//	tree    -- the process descends from a seed: a process the
//	           daemon started itself, or a process that already
//	           owns a registered window. Descendance is computed
//	           over the snapshot's parent links each tick.
//	box     -- (v0.40) the process PID is on Sandboxie's own list
//	           of processes running inside MJ's box (Start.exe
//	           /listpids, refreshed by the patrol). Boxed processes
//	           are created by the Sandboxie service, NOT by the
//	           daemon, and their image path reports the VIRTUAL
//	           path (C:\Program Files\...) -- so tree and exe both
//	           miss them, which is exactly why boxed apps' windows
//	           went unclaimed in v0.39. Membership in the box list
//	           is first-party evidence from Sandboxie itself, as
//	           strong as the spawn ledger: a host process (the
//	           user's real WeChat, say) is never on it, whatever
//	           its image is named.
//
// This file holds ONLY the pure decision logic: the platform layer
// collects procFact / windowFact values, everything testable lives
// here (see owncheck_test.go). Path handling is Windows-flavoured
// (case-insensitive, separator-tolerant) because that is the only
// platform where the patrol is live; the functions are total and
// side-effect free either way.

import "strings"

// procFact is one process as the platform layer observed it.
type procFact struct {
	PID       uint32
	PPID      uint32
	ImagePath string // full path, "" when it could not be queried
	CmdLine   string // "" when not collected (only browser images get one)
}

// windowFact is one visible top-level window as the platform layer
// observed it, in the same neutral shape the v0.31 patrol used.
type windowFact struct {
	HWND    uint64
	PID     uint32
	Class   string
	Title   string
	Visible bool
	Owned   bool // has an owner window (dialogs, palettes) -- not a main window
	W, H    int32
}

// Ownership reasons, strongest first. They are recorded on the
// registration and into the console log so the user can audit WHY a
// window was claimed.
const (
	ownProfile = "profile" // command line carries an isolated profile dir
	ownExe     = "exe"     // image lives under the terminal folder
	ownTree    = "tree"    // descends from a daemon seed process
	ownBox     = "box"     // PID is on Sandboxie's in-box process list (v0.40)
)

// ownContext carries the fixed facts classification runs against.
type ownContext struct {
	RootDir     string   // terminal folder (the EXE's directory)
	ProfileDirs []string // isolated browser profile directories
	SelfPID     uint32   // the daemon itself: never owned, never a seed target
	// BoxPIDs is the boxed-process PID set the patrol refreshed
	// from Sandboxie for this tick (v0.40). Nil/empty when the
	// component is absent or the box is not placed: the box rule
	// then simply never fires -- an empty list must never be read
	// as "everything is boxed" OR as positive evidence of any kind.
	BoxPIDs map[uint32]bool
}

// normalizeWinPath makes a path comparable: backslashes everywhere,
// lowercased, no trailing separator (except a bare root).
func normalizeWinPath(p string) string {
	p = strings.ReplaceAll(strings.TrimSpace(p), "/", `\`)
	p = strings.ToLower(p)
	for len(p) > 1 && strings.HasSuffix(p, `\`) && !strings.HasSuffix(p, `:\`) {
		p = p[:len(p)-1]
	}
	return p
}

// pathUnderDir reports whether path is dir itself or lives beneath
// it, with a real separator boundary (so C:\MJ2 is not under C:\MJ).
func pathUnderDir(path, dir string) bool {
	np, nd := normalizeWinPath(path), normalizeWinPath(dir)
	if np == "" || nd == "" {
		return false
	}
	if np == nd {
		return true
	}
	return strings.HasPrefix(np, nd+`\`)
}

// cmdlineHasProfile reports whether the command line carries the
// profile directory as a whole argument value: the character before
// the path must be start-of-string, a space, a quote or '=', and
// the character after it must be end-of-string, a space or a quote.
// That rejects the two classic traps: a LONGER sibling directory
// (…\ChromeProfile2) and a truncated prefix (…\ChromeProfil).
func cmdlineHasProfile(cmdline, profileDir string) bool {
	if cmdline == "" || profileDir == "" {
		return false
	}
	hay := strings.ReplaceAll(cmdline, "/", `\`)
	hay = strings.ToLower(hay)
	needle := normalizeWinPath(profileDir)
	for from := 0; ; {
		i := strings.Index(hay[from:], needle)
		if i < 0 {
			return false
		}
		i += from
		beforeOK := i == 0 || hay[i-1] == ' ' || hay[i-1] == '\t' || hay[i-1] == '"' || hay[i-1] == '='
		end := i + len(needle)
		afterOK := end == len(hay) || hay[end] == ' ' || hay[end] == '\t' || hay[end] == '"'
		if beforeOK && afterOK {
			return true
		}
		from = i + 1
	}
}

// classifyOwnership decides which processes in the snapshot belong
// to the terminal, and why. seeds are PIDs the daemon vouches for
// directly: processes it started (the spawn ledger) and processes
// that already own a registered window. The daemon's own PID is
// never classified, even though its image lives under RootDir.
func classifyOwnership(procs []procFact, seeds map[uint32]bool, ctx ownContext) map[uint32]string {
	owned := map[uint32]string{}
	// Direct evidence first; strongest reason wins per process.
	for _, p := range procs {
		if p.PID == 0 || p.PID == ctx.SelfPID {
			continue
		}
		for _, dir := range ctx.ProfileDirs {
			if cmdlineHasProfile(p.CmdLine, dir) {
				owned[p.PID] = ownProfile
				break
			}
		}
		if owned[p.PID] != "" {
			continue
		}
		if ctx.BoxPIDs[p.PID] {
			// v0.40: Sandboxie's own in-box list. Checked before
			// exe/tree: it is first-party evidence, and a boxed
			// process's virtual image path would fail the exe
			// rule anyway, so this ordering is what gives boxed
			// processes their (correct) audit reason.
			owned[p.PID] = ownBox
			continue
		}
		if p.ImagePath != "" && pathUnderDir(p.ImagePath, ctx.RootDir) {
			owned[p.PID] = ownExe
			continue
		}
		if seeds[p.PID] {
			owned[p.PID] = ownTree
		}
	}
	// Descendance closure over the snapshot's parent links: a
	// process whose parent (or any ancestor present in the
	// snapshot) is owned belongs to the terminal too. This is what
	// catches the browser handing a PDF to an external viewer, or a
	// launcher stub passing control to the real app process.
	for changed := true; changed; {
		changed = false
		for _, p := range procs {
			if p.PID == 0 || p.PID == ctx.SelfPID || owned[p.PID] != "" {
				continue
			}
			if p.PPID != 0 && p.PPID != p.PID && owned[p.PPID] != "" {
				owned[p.PID] = ownTree
				changed = true
			}
		}
	}
	return owned
}

// claimOpts carries the per-tick switches decideClaim needs.
type claimOpts struct {
	AutoClaimBrowser bool // 设置 -> stageAutoClaimBrowser (browser auto-claim gate)
	EmptySeenBefore  bool // this HWND was already seen title-less on an earlier tick
	// ShellPIDs own the desktop shell window: never claim, whatever
	// the ownership classifier said (the shell is registered
	// separately and excluded from the external list by design).
	ShellPIDs map[uint32]bool
	// BrowserPIDs own already-registered browser windows: their
	// further windows are the same auto-claim class as the profile
	// rule, including for the on/off gate.
	BrowserPIDs map[uint32]bool
}

// claimVerdict is decideClaim's answer for one window.
type claimVerdict struct {
	Claim   bool
	Defer   bool // owned and shape-valid, but the title is not ready: retry next tick, no alarm yet
	NoTitle bool // claiming despite a still-empty title (after the grace tick)
	Browser bool // register as a browser window (title gets the browser-suffix trim)
	Gated   bool // withheld only because the auto-claim switch is off: not a failure, never alarms
	Reason  string
	// HiddenClaim marks a register-only claim of a window that
	// is not currently visible (v0.41, ownBox only): the caller
	// must register it WITHOUT the presenting birth settlement
	// -- the patrol never reveals or raises a hidden window on
	// its own; revealing is the user's launch click's job.
	HiddenClaim bool
}

// stripSandboxClassPrefix removes Sandboxie's per-box window
// class prefix ("Sandbox:<box name>:<real class>") so class
// tests see the class the application actually registered. A
// class without the prefix is returned unchanged.
func stripSandboxClassPrefix(class string) string {
	if strings.HasPrefix(class, "Sandbox:") {
		rest := class[len("Sandbox:"):]
		if i := strings.Index(rest, ":"); i >= 0 {
			return rest[i+1:]
		}
	}
	return class
}

// isSystemWindowClass reports whether a window class belongs to
// the always-invisible plumbing every process carries -- IME
// hosts, text-services UI, Chromium/DLL message windows,
// crash-report watchers, Sandboxie's own DDE proxy -- and never
// to an application's main window. The test runs against the
// Sandboxie-stripped class, so a boxed "…:crashpad_…" is caught
// exactly like a host one; the Sandbox: prefix itself is normal
// for boxed windows and is NOT a reason to exclude anything.
func isSystemWindowClass(class string) bool {
	c := stripSandboxClassPrefix(class)
	switch c {
	case "IME", "MSCTFIME UI", "RunDLL":
		return true
	}
	if strings.HasSuffix(c, "SystemMessageWindow") { // Chrome_SystemMessageWindow, DisplayICC_SystemMessageWindow, ...
		return true
	}
	if strings.HasPrefix(c, "DisplayICC") {
		return true
	}
	if strings.HasPrefix(c, "crashpad") { // crashpad_SessionEndWatcher
		return true
	}
	if strings.HasPrefix(c, "Sandboxie_DDE") { // Sandboxie_DDE_ProxyClass1
		return true
	}
	return false
}

// isAppWindowShape is the "this is an application's main window"
// test (pure): titled, unowned (not a dialog/palette), and not a
// system plumbing class. Visibility is deliberately NOT part of
// the test -- v0.41 exists because a boxed app's main window can
// satisfy every clause here while WS_VISIBLE is off.
func isAppWindowShape(w windowFact) bool {
	return w.Title != "" && !w.Owned && !isSystemWindowClass(w.Class)
}

// Window shape filters, carried over from the v0.31 patrol: specks
// below this are toasts, bubbles and other transients.
const (
	claimMinW = 200
	claimMinH = 100
)

// decideClaim rules on one enumerated window. registered holds the
// HWNDs already in the Stage Manager registry. The function is
// deliberately conservative: anything doubtful is a no-claim, and
// the only path that can ever claim a window runs through the
// ownership map -- an unowned process's window is never claimed.
func decideClaim(w windowFact, owned map[uint32]string, registered map[uint64]bool, opts claimOpts) claimVerdict {
	if registered[w.HWND] || w.Owned {
		return claimVerdict{}
	}
	if w.W < claimMinW || w.H < claimMinH {
		return claimVerdict{}
	}
	if opts.ShellPIDs[w.PID] {
		return claimVerdict{}
	}
	reason, ok := owned[w.PID]
	if !ok {
		return claimVerdict{}
	}
	if !w.Visible {
		// v0.41: the ONE invisible-window claim. Field evidence
		// (v0.40): WeChat's Qt main window existed under a boxed
		// PID with WS_VISIBLE off, so the visible-only rule
		// missed it and nothing ever showed it. The relaxation
		// is scoped as narrowly as that evidence allows -- box
		// ownership ONLY, and the window must pass the
		// app-window shape test -- and it is register-only: the
		// verdict is marked HiddenClaim and the patrol caller
		// must not run the presenting birth settlement for it.
		if reason != ownBox || !isAppWindowShape(w) {
			return claimVerdict{}
		}
		return claimVerdict{Claim: true, Reason: reason, HiddenClaim: true}
	}
	v := claimVerdict{Reason: reason}
	// ownBox (v0.40) deliberately has no case below: a boxed app is
	// claimed like an exe/tree window -- any main-window class, and
	// NOT subject to the browser auto-claim gate (it is not a
	// browser window class by virtue of being boxed).
	switch {
	case reason == ownProfile:
		// A browser process grows helper windows of other classes
		// (notifications, IME, ...); only its MAIN window class is
		// a page window. The class check is what keeps them out.
		if !isBrowserWindowClass(w.Class) {
			return claimVerdict{}
		}
		v.Browser = true
		if !opts.AutoClaimBrowser {
			v.Gated = true
			return v
		}
	case isBrowserWindowClass(w.Class) && opts.BrowserPIDs[w.PID]:
		// A further window of an already-registered browser
		// process whose profile evidence was not readable this
		// tick: same auto-claim class, same gate.
		v.Browser = true
		if !opts.AutoClaimBrowser {
			v.Gated = true
			return v
		}
	}
	if w.Title == "" {
		if !opts.EmptySeenBefore {
			v.Defer = true
			return v
		}
		// Grace spent: register it anyway under a fallback label
		// rather than leaving a terminal window unfindable.
		v.NoTitle = true
	}
	v.Claim = true
	return v
}

// ownReasonText renders an ownership reason for the console log.
func ownReasonText(reason string) string {
	switch reason {
	case ownProfile:
		return "隔离档案指纹"
	case ownExe:
		return "终端文件夹内程序"
	case ownTree:
		return "终端进程树"
	case ownBox:
		return "盒内进程名单"
	}
	return reason
}
