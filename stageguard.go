package main

// ---------- v0.62: detail-fix trio (window management) ----------
//
// Three field reports from 2026-11-01, one file for their shared
// machinery (the handlers and per-fix edits live next to the
// code they fix -- winmgr.go / winset.go / winmgr_windows.go):
//
//  ⑤ Clicking the desktop view's own UI (Control Center,
//     Notification Center, menubar menus -- any overlay) while
//     an external app was open made the app "vanish". No daemon
//     code hid it: the shell view is a full-size host window and
//     the birth placement deliberately leaves shell strips
//     uncovered (menubar, Stage strip, Dock), so clicking one
//     ACTIVATED the shell and plain Windows z-order slid it over
//     every claimed window it overlaps. The only z-order care
//     was the one-shot birth backdrop. Fix: the view reports its
//     overlay sessions (POST /api/view/overlay, web/src/b1/
//     overlay.ts); while one is live AND the shell is the
//     foreground window, the shell watcher (winset.go) runs the
//     z-order guard below -- every visible claimed window found
//     BELOW the shell is lifted back directly above it,
//     NOACTIVATE, focus never touched. Explicit stage actions
//     (Stage strip click, Dock click, tray summon, boss key)
//     never open an overlay session, so the shell may still
//     legitimately come front for them. And every remaining
//     hide path now logs its reason (winset.go's grpHideSet
//     takes the reason from the state-machine event): a hide
//     with no explicit reason no longer exists.
//
//  ① Chrome with 2-3 windows: Stage cards "did nothing" and
//     windows vanished. Compound cause: platformFocusWindow's
//     AttachThreadInput foreground-lock workaround never pinned
//     its OS thread (fixed in winmgr_windows.go), raise failures
//     were silent (no [winmgr] line), and the action handlers
//     DELETED a registration when a title probe failed -- a
//     transient probe miss could strike a live window from the
//     ledger. The registry was already per-HWND; what was
//     missing is the discipline pinned here: decideRaise rules
//     every raise attempt, failures keep the window claimed and
//     are logged with HWND + app, and removal belongs ONLY to
//     the patrol prune (the same IsWindow-based verdict).
//
//  ③ Stage card right-click menu: it shipped in v0.56 with
//     focus/minimize/close; this version adds 隐藏其他窗口 --
//     stage-focus: raise the anchor, hide every other visible
//     claimed window as an EXPLICIT stage action. Solo-hidden
//     windows are booked separately from the group book
//     (grpHidden is tied to the set state machine): they come
//     back when raised by hand (Stage click clears the
//     booking), when the anchor dies (patrol reconcile), when
//     a group hide folds the book into grpHidden, and always
//     before daemon exit -- the v0.37 never-strand line.
//
// test3 (same trio, the raise half one level deeper): test2's
// field verification passed the boss key but the raise still
// failed the user's lived test -- his video shows two Chrome
// windows opening, one sliding out of sight, and Stage clicks
// doing nothing, while the daemon log claimed ~50 raises
// "succeeded". The log recorded SetForegroundWindow's return
// value, not the pixels: foreground is not on-stage. The shell
// is a full-size window; any click in it (the browser panel,
// the Stage strip itself) activates the shell FIRST and plain
// z-order covers the claimed window; the daemon's foreground
// call then "succeeds" while the target stays under the shell.
// The birth path learned this in v0.54 and verifies
// (platformWindowAbove + presentSweep); the raise path never
// did. focusClaimedWindow now verifies after the dance
// (decideRaiseVerify): a raise counts only when the window is
// above the shell and not iconic; otherwise it is lifted
// explicitly above the shell, refocused, and re-verified; and
// a short generation-bound sweep re-lifts it if the click's
// own activation tail shoves the shell back on top within the
// first second (decideRaiseSweep).
//
// All decisions are pure functions over plain facts, in the
// house style (winset_test.go / owncheck_test.go); the platform
// layer only collects facts and performs the outcomes.

import (
	"encoding/json"
	"net/http"
	"sort"
	"sync"
	"time"
)

// ---------- ① raise outcome ----------

type raiseFacts struct {
	Alive   bool // the handle still names a window (IsWindow-based probe)
	FocusOK bool // the shared raise (platformFocusWindow) reported success
}

type raiseOutcome int

const (
	raiseOK raiseOutcome = iota
	raiseGone
	raiseFailed
)

// decideRaise is the only verdict an action path may act on: a
// dead handle answers the click "gone" but the registration
// STAYS (the patrol prune owns removal); a live handle whose
// raise failed also stays claimed, and the failure is logged.
// Nothing in this version unclaims or hides a window because a
// raise failed.
func decideRaise(f raiseFacts) raiseOutcome {
	if !f.Alive {
		return raiseGone
	}
	if !f.FocusOK {
		return raiseFailed
	}
	return raiseOK
}

// ---------- ① raise retry (v0.62 test2) ----------
//
// Field evidence (2026-11-05 21:09:19, the test1 build's own
// live log): ONE user card-click raise in ~15 failed outright
// -- "窗口置前失败，窗口保留登记（hwnd=23595422，www.google.com）"
// -- while its neighbours all succeeded. test1 correctly kept
// the registration, but a click that does nothing is still a
// broken click from the user's seat, so a user-initiated raise
// now retries ONCE: a short settle delay, a restore-if-iconic
// (platformRestoreWindow), then the full foreground dance
// again. decideRaise semantics are untouched -- a failure still
// never deletes the registration; the retry decision is its own
// pure seam (decideRaiseRetry), and only a double failure earns
// the WARN.

const (
	raiseMaxAttempts = 2
	raiseRetryDelay  = 150 * time.Millisecond
)

type raiseRetryAction int

const (
	raiseActOK           raiseRetryAction = iota // an attempt succeeded, first try
	raiseActOKAfterRetry                         // the retry succeeded
	raiseActRetry                                // the first attempt failed: wait, restore, raise again
	raiseActGiveUp                               // both attempts failed: the caller's WARN path
)

// decideRaiseRetry is the pure retry contract for a user raise:
// given how many attempts have been made and whether the last
// one succeeded, what happens next.
func decideRaiseRetry(attempts int, ok bool) raiseRetryAction {
	if ok {
		if attempts > 1 {
			return raiseActOKAfterRetry
		}
		return raiseActOK
	}
	if attempts >= raiseMaxAttempts {
		return raiseActGiveUp
	}
	return raiseActRetry
}

// focusClaimedWindow runs ONE user-initiated raise of a live
// claimed window (a Stage card click, or a stage-focus anchor)
// and returns the decideRaise-level verdict the caller acts
// on: raiseOK only when the window is verified ON STAGE (see
// verifyRaisedOnStage), raiseFailed when even the escalation
// could not put it there (the caller then emits its WARN --
// the registration stays either way). A retry success is named
// here so the console shows the recovery happened instead of
// silence. A verified raise also arms the anti-re-cover sweep.
func focusClaimedWindow(hwnd uint64, label string) raiseOutcome {
	focusOK := false
	if decideRaiseRetry(1, platformFocusWindow(hwnd)) == raiseActOK {
		focusOK = true
	} else {
		time.Sleep(raiseRetryDelay)
		// SW_RESTORE first if the first dance left the window
		// iconic, then the full foreground dance once more.
		platformRestoreWindow(hwnd)
		if decideRaiseRetry(2, platformFocusWindow(hwnd)) == raiseActOKAfterRetry {
			logf("winmgr", "info", "台前调度：置前重试成功（hwnd=%d，%s）", hwnd, label)
			focusOK = true
		}
	}
	// v0.62 test3: the dance's own report is not the verdict --
	// the stage is. Verify (with one escalation lift in hand)
	// before answering, so a caller's 已置前 always means the
	// window is really up where the user can see it.
	if !verifyRaisedOnStage(hwnd, label, focusOK) {
		return raiseFailed
	}
	raiseSweepArm(hwnd, label)
	return raiseOK
}

// ---------- ① on-stage verification (v0.62 test3) ----------
//
// The test2 field failure, precisely: platformFocusWindow
// trusts SetForegroundWindow, and SetForegroundWindow answers
// a question about FOCUS, not about the stage. On the user's
// single-monitor machine the desktop view is a full-size
// window: clicking anything in it -- the browser panel he
// opens Chrome from, the Stage strip itself -- activates the
// shell first, and plain Windows z-order slides the shell over
// the claimed window ("它就会自动隐藏"). The subsequent card
// click also lands in the shell first; the daemon's foreground
// call then returns success while the target stays UNDER the
// shell, and the user watches his clicks do nothing, fifty
// logged successes in a row. The birth path solved this exact
// class in v0.54: presentSweep verifies a newborn is ABOVE the
// shell (platformWindowAbove) and re-lifts it when it is not.
// This is the same pattern, moved to the raise path.

type raiseStageFacts struct {
	FocusOK    bool // the raise dance (test2 retry included) reported success
	AboveShell bool // the window sits above the shell in z-order (vacuously true when there is no shell)
	Iconic     bool // minimized: cannot count as on stage
}

type raiseVerifyAction int

const (
	raiseVerifyDone raiseVerifyAction = iota // verified on stage: the raise is complete
	raiseVerifyLift                          // not on stage, escalation unspent: lift above the shell, refocus, verify again
	raiseVerifyFail                          // still not on stage after the lift: the caller's WARN path
)

// raiseStageMaxChecks bounds the verification: the first check
// judges the dance as it landed, the second judges it after the
// one escalation lift. There is no third -- a window that still
// cannot be put on stage is the user's to summon by hand, and
// the WARN now says exactly that instead of claiming a success
// that never reached the screen.
const raiseStageMaxChecks = 2

// decideRaiseVerify is the pure on-stage contract for a user
// raise: focus reported AND above the shell AND not minimized
// is the only "done". Anything else spends the one escalation
// lift; a second bad verdict fails -- and per decideRaise's
// standing rule, failure still never deletes the registration.
func decideRaiseVerify(checks int, f raiseStageFacts) raiseVerifyAction {
	if f.FocusOK && f.AboveShell && !f.Iconic {
		return raiseVerifyDone
	}
	if checks >= raiseStageMaxChecks {
		return raiseVerifyFail
	}
	return raiseVerifyLift
}

// raiseStageFactsNow collects the verification facts for hwnd.
// The above-shell fact mirrors presentSweep's: with no shell
// (or the target BEING the shell) nothing can cover the
// window, so the fact reads true and foreground carries the
// verdict -- off Windows platformWindowAbove reads true too,
// so verification there falls back to the dance's own report,
// exactly as the birth path does.
func raiseStageFactsNow(hwnd uint64, focusOK bool) raiseStageFacts {
	shell := shellHWND()
	above := true
	if shell != 0 && shell != hwnd {
		above = platformWindowAbove(hwnd, shell)
	}
	return raiseStageFacts{
		FocusOK:    focusOK,
		AboveShell: above,
		Iconic:     platformWindowIconic(hwnd),
	}
}

// verifyRaisedOnStage judges a completed raise dance and, if
// the window is not really up, escalates ONCE: an explicit
// z-order lift directly above the shell (the z-order guard's
// own platformInsertAboveShell, NOACTIVATE), one more
// foreground ask from that footing, then a second verdict.
// The escalation is named in the console so a recovered raise
// is distinguishable from a clean one -- and from a failure.
func verifyRaisedOnStage(hwnd uint64, label string, focusOK bool) bool {
	for checks := 1; ; checks++ {
		switch decideRaiseVerify(checks, raiseStageFactsNow(hwnd, focusOK)) {
		case raiseVerifyDone:
			return true
		case raiseVerifyFail:
			return false
		}
		// raiseVerifyLift.
		if shell := shellHWND(); shell != 0 && shell != hwnd {
			platformInsertAboveShell(hwnd, shell)
		}
		focusOK = platformFocusWindow(hwnd)
		logf("winmgr", "info", "台前调度：置前未上台，已补提（hwnd=%d，%s）", hwnd, label)
	}
}

// ---------- ① anti-re-cover sweep (v0.62 test3) ----------
//
// Verification judges the instant the dance ends; the user's
// click is still landing at that instant. The click that
// asked for the raise was aimed at the shell (the Stage strip
// lives in it), and its activation tail can shove the shell
// back on top a beat AFTER the daemon verified the raise --
// the window was up, briefly, and then the desktop covered it
// again with nobody logging a thing. So a verified raise arms
// a short sweep in presentSweep's spirit: two ticks (~300 ms,
// ~800 ms) that re-check the one fact that matters -- is the
// window still above the shell -- and re-lift it (z-order
// only, focus never touched) when it fell back under.
//
// The sweep belongs to a raise GENERATION in the hotkey
// registrar's style: it serves exactly one window, the most
// recently verified raise. A newer verified raise (any
// window) bumps the generation and the old sweep stands down
// silently -- a sweep must never yank a window back over a
// stage the user has since re-cast. A target that closed or
// was pruned, a window the user minimized themselves, and a
// set the set machine has tucked away all stand the sweep
// down too: decideRaiseSweep's facts say so, and nothing here
// ever fights the set machine or the user's own hand.

type raiseSweepFacts struct {
	Registered bool // still in the registry (not closed / pruned)
	AboveShell bool // still above the shell in z-order
	Iconic     bool // user-minimized since: its state is its own business
	SetHidden  bool // the set machine has the set tucked away: never fight it
}

type raiseSweepAction int

const (
	raiseSweepStop raiseSweepAction = iota // nothing owed: stand down silently
	raiseSweepLift                         // fell below the shell: lift it back, this tick only
)

// decideRaiseSweep is the pure per-tick contract: re-lift only
// a live, on-desktop window that has actually fallen below the
// shell while its set is supposed to be up.
func decideRaiseSweep(f raiseSweepFacts) raiseSweepAction {
	if !f.Registered || f.SetHidden || f.Iconic || f.AboveShell {
		return raiseSweepStop
	}
	return raiseSweepLift
}

// The sweep book: the one raise that currently owns the stage.
// A generation starts at every verified raise; ticks carry
// their generation and are inert once it moves on (the
// hkRetryState discipline, same reason: a stale timer must
// never act on a stage the user has already re-cast).
var raiseSweepState = struct {
	sync.Mutex
	gen   int
	hwnd  uint64
	label string
}{}

// raiseSweepSchedule is time.AfterFunc behind a var so tests
// can capture the ticks instead of waiting on the clock (the
// hkScheduleRetry idiom).
var raiseSweepSchedule = time.AfterFunc

const (
	raiseSweepTick1 = 300 * time.Millisecond
	raiseSweepTick2 = 800 * time.Millisecond
)

// raiseSweepArm books the anti-re-cover sweep for a verified
// raise: two ticks, both carrying this raise's generation.
func raiseSweepArm(hwnd uint64, label string) {
	raiseSweepState.Lock()
	raiseSweepState.gen++
	gen := raiseSweepState.gen
	raiseSweepState.hwnd = hwnd
	raiseSweepState.label = label
	raiseSweepState.Unlock()
	raiseSweepSchedule(raiseSweepTick1, func() { raiseSweepFire(gen, hwnd) })
	raiseSweepSchedule(raiseSweepTick2, func() { raiseSweepFire(gen, hwnd) })
}

// raiseSweepClaim returns the booked label when a tick still
// owns the stage: its generation is the current one and it
// names the booked window. The claim is atomic with the label
// read, so a superseded tick can neither act nor log under a
// newer raise's name.
func raiseSweepClaim(gen int, hwnd uint64) (string, bool) {
	raiseSweepState.Lock()
	defer raiseSweepState.Unlock()
	if gen != raiseSweepState.gen || raiseSweepState.hwnd != hwnd {
		return "", false
	}
	return raiseSweepState.label, true
}

// raiseSweepCancel stands the sweep down for one window
// (v0.71 test3, L2-B1): a daemon-intentional hide must not be
// followed by a sweep tick re-lifting the hidden window --
// nor by its "put back on stage" log line, which would be a
// lie about a window the user cannot see.
func raiseSweepCancel(hwnd uint64) {
	raiseSweepState.Lock()
	if raiseSweepState.hwnd == hwnd {
		raiseSweepState.gen++
		raiseSweepState.hwnd = 0
		raiseSweepState.label = ""
	}
	raiseSweepState.Unlock()
}

// raiseSweepFire is one sweep tick coming due. Every stand-
// down is silent by design: a superseded tick, a vanished
// shell, a pruned window are all ordinary endings, and the
// console line is reserved for the one event worth naming --
// a window that had to be put back on stage.
func raiseSweepFire(gen int, hwnd uint64) {
	label, live := raiseSweepClaim(gen, hwnd)
	if !live {
		return
	}
	shell := shellHWND()
	if shell == 0 || shell == hwnd {
		return
	}
	extMu.Lock()
	_, registered := extWins[hwnd]
	extMu.Unlock()
	facts := raiseSweepFacts{
		Registered: registered,
		AboveShell: platformWindowAbove(hwnd, shell),
		Iconic:     platformWindowIconic(hwnd),
		SetHidden:  grpIsHidden(),
	}
	if decideRaiseSweep(facts) != raiseSweepLift {
		return
	}
	if platformInsertAboveShell(hwnd, shell) {
		logf("winmgr", "info", "台前调度：置前巡查：窗口被桌面盖回，已再次提上台（hwnd=%d，%s）", hwnd, label)
	}
}

// ---------- ③ stage-focus (hide others) ----------

type soloCandidate struct {
	HWND    uint64
	Visible bool
	Iconic  bool // user-minimized: its state is its own business
}

// decideHideOthers picks the windows a stage-focus action
// hides: every OTHER claimed window that is currently up on the
// desktop. Iconic or already-invisible windows are skipped --
// solo restore must never resurrect what it did not hide (the
// grpHideSet rule, applied to the solo book).
func decideHideOthers(anchor uint64, cands []soloCandidate) []uint64 {
	out := []uint64{}
	for _, c := range cands {
		if c.HWND == 0 || c.HWND == anchor {
			continue
		}
		if c.Visible && !c.Iconic {
			out = append(out, c.HWND)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// The solo book: windows hidden by ONE stage-focus action and
// the window that anchored it. Deliberately separate from
// grpHidden (winset.go): the group book restores on set-state
// transitions, the solo book on the gestures listed in the
// header. While a group hide is in progress the solo book folds
// into grpHidden (grpApplyEvent), so the two never own the same
// window at once.
var (
	soloMu     sync.Mutex
	soloAnchor uint64
	soloHidden = map[uint64]bool{}
)

// soloBook records a stage-focus result. A second focus action
// with a different anchor MERGES: windows hidden by the earlier
// action are invisible, so decideHideOthers cannot pick them up
// again -- dropping their booking would strand them.
func soloBook(anchor uint64, hidden []uint64) {
	soloMu.Lock()
	defer soloMu.Unlock()
	soloAnchor = anchor
	for _, h := range hidden {
		soloHidden[h] = true
	}
	if len(soloHidden) == 0 {
		soloAnchor = 0
	}
}

// soloForget drops one window from the solo book: it was just
// raised by hand, so it is nobody's hidden window any more.
// Returns whether it had been booked.
func soloForget(hwnd uint64) bool {
	soloMu.Lock()
	defer soloMu.Unlock()
	if !soloHidden[hwnd] {
		return false
	}
	delete(soloHidden, hwnd)
	if len(soloHidden) == 0 {
		soloAnchor = 0
	}
	return true
}

// soloOwns reports whether the solo book currently owns this
// window's hidden state (v0.71 test3: the unlock invariant
// consults it, next to grpOwnsHidden).
func soloOwns(hwnd uint64) bool {
	soloMu.Lock()
	defer soloMu.Unlock()
	return soloHidden[hwnd]
}

// soloTakeAll drains the book (group-hide fold, daemon exit).
func soloTakeAll() (uint64, []uint64) {
	soloMu.Lock()
	defer soloMu.Unlock()
	out := make([]uint64, 0, len(soloHidden))
	for h := range soloHidden {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	anchor := soloAnchor
	soloAnchor = 0
	soloHidden = map[uint64]bool{}
	return anchor, out
}

// soloReconcile keeps the solo book honest against the registry
// after a patrol prune: booked windows that died leave the
// book, and if the ANCHOR died the solo premise is gone -- the
// remaining hidden windows come straight back to the desktop.
func soloReconcile(registered map[uint64]bool) {
	soloMu.Lock()
	if soloAnchor == 0 && len(soloHidden) == 0 {
		soloMu.Unlock()
		return
	}
	for h := range soloHidden {
		if !registered[h] {
			delete(soloHidden, h)
		}
	}
	var restore []uint64
	if soloAnchor != 0 && !registered[soloAnchor] {
		for h := range soloHidden {
			restore = append(restore, h)
		}
		soloHidden = map[uint64]bool{}
		soloAnchor = 0
	}
	if len(soloHidden) == 0 {
		soloAnchor = 0
	}
	soloMu.Unlock()
	if len(restore) > 0 {
		n := 0
		for _, h := range restore {
			if platformWindowExists(h) && platformShowBackWindow(h) {
				n++
			}
		}
		logf("winmgr", "info", "台前调度：聚焦锚点窗口已关闭，已把单独隐藏的窗口放回桌面（%d/%d 个，原因：锚点消失）", n, len(restore))
	}
}

// extWinFocusOnlyHandler serves POST /api/extwin/focusonly
// {"hwnd": N} (v0.62, detail-fix ③'s 隐藏其他窗口）: raise the
// anchor first -- if the anchor cannot come up, nothing hides,
// so the user is never left staring at an empty desktop -- then
// hide every other visible claimed window as an explicit stage
// action and book them solo.
func extWinFocusOnlyHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		HWND uint64 `json:"hwnd"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.HWND == 0 {
		apiErrBadRequest(w, errCodeBadRequest, "参数不对")
		return
	}
	extMu.Lock()
	label := ""
	registered := false
	if e, ok := extWins[body.HWND]; ok {
		label, registered = e.App, true
	}
	extMu.Unlock()
	if !registered {
		writeAPIError(w, http.StatusNotFound, errCodeNotFound, "这个窗口不在台前调度里")
		return
	}
	if _, alive := platformWindowTitle(body.HWND); !alive {
		// Same rule as the focus handler (①): the probe decides
		// only this answer; the registration stays for the prune.
		logf("winmgr", "warn", "台前调度聚焦：目标窗口已不存在，保留登记待巡逻修剪（hwnd=%d，%s）", body.HWND, label)
		writeAPIError(w, http.StatusNotFound, errCodeNotFound, "这个窗口已经关了")
		return
	}
	// If the whole set is tucked away, the Stage click rule
	// restores it first (grpRestoreForFocus); the solo hide
	// below then applies to the restored set.
	grpRestoreForFocus()
	if focusClaimedWindow(body.HWND, label) != raiseOK {
		logf("winmgr", "warn", "台前调度聚焦：锚点窗口置前失败，其他窗口不隐藏（hwnd=%d，%s）", body.HWND, label)
		writeAPIError(w, http.StatusInternalServerError, errCodeInternal, "窗口置前失败")
		return
	}
	soloForget(body.HWND)
	extMu.Lock()
	if e, ok := extWins[body.HWND]; ok {
		e.presentPending = false
	}
	others := make([]uint64, 0, len(extWins))
	labels := map[uint64]string{}
	for h, e := range extWins {
		if h != body.HWND && !e.Shell {
			others = append(others, h)
			labels[h] = e.App
		}
	}
	extMu.Unlock()
	cands := make([]soloCandidate, 0, len(others))
	for _, h := range others {
		cands = append(cands, soloCandidate{
			HWND:    h,
			Visible: platformWindowVisible(h),
			Iconic:  platformWindowIconic(h),
		})
	}
	hidden := []uint64{}
	for _, h := range decideHideOthers(body.HWND, cands) {
		if platformHideWindow(h) {
			hidden = append(hidden, h)
		} else {
			logf("winmgr", "warn", "台前调度聚焦：隐藏窗口失败，窗口保持可见（hwnd=%d，%s）", h, labels[h])
		}
	}
	soloBook(body.HWND, hidden)
	logf("winmgr", "info", "台前调度聚焦（隐藏其他窗口）：已提出 %s（hwnd=%d），其余 %d 个窗口按显式舞台动作隐藏（reason=stage-focus）", label, body.HWND, len(hidden))
	busWindows(body.HWND) // v0.59 block 3: live sync
	writeJSON(w, okResponse{OK: true})
}

// ---------- ⑤ overlay sessions + z-order guard ----------

// The view's overlay sessions, as reported by POST
// /api/view/overlay. A plain set, not a refcount: the view
// reports transitions only (web/src/b1/overlay.ts collapses
// repeats), and the view-stream disconnect clears the table
// (viewcmd.go), so a lost close can never leave the guard
// armed.
var (
	overlayMu    sync.Mutex
	overlayKinds = map[string]bool{}
)

func overlaySession(kind string, on bool) {
	if kind == "" {
		return
	}
	overlayMu.Lock()
	if kind == "*" && !on {
		had := len(overlayKinds)
		overlayKinds = map[string]bool{}
		overlayMu.Unlock()
		if had > 0 {
			logf("winmgr", "info", "桌面浮层会话表已清空（视图重置）")
		}
		return
	}
	changed := overlayKinds[kind] != on
	if on {
		overlayKinds[kind] = true
	} else {
		delete(overlayKinds, kind)
	}
	overlayMu.Unlock()
	if changed {
		if on {
			logf("winmgr", "info", "桌面浮层打开：%s（浮层不改变已认领窗口的可见性，z 序守护生效）", kind)
		} else {
			logf("winmgr", "info", "桌面浮层关闭：%s", kind)
		}
	}
}

func overlayLive() bool {
	overlayMu.Lock()
	defer overlayMu.Unlock()
	return len(overlayKinds) > 0
}

// overlayClearAll drops every overlay session (the desktop
// view's stream went away: its overlays died with it).
func overlayClearAll() {
	overlayMu.Lock()
	had := len(overlayKinds)
	overlayKinds = map[string]bool{}
	overlayMu.Unlock()
	if had > 0 {
		logf("winmgr", "info", "桌面浮层会话表已清空（视图断开）")
	}
}

// viewOverlayHandler serves POST /api/view/overlay
// {"kind": "cc|nc|menu|spotlight|launchpad|ctx", "on": bool}:
// the desktop view reporting one of its own overlays opening or
// closing. Local mux only -- the remote scrubbed set is
// unchanged (block-3 rule).
func viewOverlayHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind string `json:"kind"`
		On   bool   `json:"on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Kind == "" {
		apiErrBadRequest(w, errCodeBadRequest, "参数不对")
		return
	}
	overlaySession(body.Kind, body.On)
	writeJSON(w, okResponse{OK: true})
}

type zGuardFacts struct {
	OverlayLive       bool // a desktop-view overlay session is live
	ForegroundIsShell bool // the shell view holds the foreground
	ShellUsable       bool // shell exists, visible, not minimized
	SetHidden         bool // the set machine has the set tucked away
}

// decideZGuard: the guard acts only while an overlay is up and
// the shell is foreground -- the exact shape of the ⑤ report.
// While the set is group-hidden the claimed windows are SUPPOSED
// to be down; the guard must never fight the set machine.
func decideZGuard(f zGuardFacts) bool {
	return f.OverlayLive && f.ForegroundIsShell && f.ShellUsable && !f.SetHidden
}

// liftClaimedAboveShell is the guard's arm (called from the
// shell watcher, winset.go): every registered window that is
// visible, not minimized and currently BELOW the shell is
// lifted to directly above it. platformAllTopWindows walks in
// z-order (top first) and platformInsertAboveShell targets the
// slot directly above the shell, so processing in walk order
// preserves the claimed windows' relative order. NOACTIVATE
// throughout: focus is never touched, and windows already above
// the shell are left exactly where they are.
func liftClaimedAboveShell(shell uint64) {
	extMu.Lock()
	reg := make(map[uint64]string, len(extWins))
	for h, e := range extWins {
		if !e.Shell {
			reg[h] = e.App
		}
	}
	extMu.Unlock()
	if len(reg) == 0 {
		return
	}
	for _, wf := range platformAllTopWindows() {
		app, ok := reg[wf.HWND]
		if !ok {
			continue
		}
		if !platformWindowVisible(wf.HWND) || platformWindowIconic(wf.HWND) {
			continue
		}
		if platformWindowAbove(wf.HWND, shell) {
			continue
		}
		if platformInsertAboveShell(wf.HWND, shell) {
			logf("winmgr", "info", "台前调度：桌面浮层打开期间把被盖住的窗口提回桌面之上：%s（hwnd=%d，原因：浮层不改变窗口可见性）", app, wf.HWND)
		}
	}
}
