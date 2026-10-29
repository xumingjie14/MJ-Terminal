package main

// ---------- v0.47 通知中心：事件通知 (module: notify) ----------
//
// Two kinds of notifications, kept honest (macOS User Guide,
// "Use Notification Center on Mac"):
//
//   - STATUS items exist while a state exists (a sandbox install
//     waiting for registration, an update ready, a server alert
//     tripped). They are assembled on read from each module's own
//     state -- never recorded, never dismissible.
//   - EVENT items are recorded when something happens (an install
//     finished, a node dropped, ...). They persist in
//     Data/notifications.json (rolling cap), are dismissible one
//     by one or per source, and a fresh event raises a banner in
//     the desktop view while Notification Center is closed.
//
// Modules report events through ONE helper, postNotify, which is
// asynchronous, never blocks the caller, dedupes the same
// source+key inside 10 minutes, honours the per-source switches
// in 设置 → 通知 (allow / banner), and logs under module notify.
// Banners ride a small SSE stream (/api/notify/stream) so the
// view learns about an event the moment it is recorded.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Notification sources (also the suffixes of the settings keys
// notify_allow_<src> / notify_banner_<src>).
const (
	notifySrcSandbox   = "sandbox"
	notifySrcUpdate    = "update"
	notifySrcServermon = "servermon"
	notifySrcNode      = "node"
	notifySrcRemote    = "remote"
	notifySrcFiles     = "files"
)

// notifySources lists every source with its 设置 → 通知 display
// name, in settings-page order.
var notifySources = []struct{ ID, Name string }{
	{notifySrcSandbox, "应用隔离"},
	{notifySrcUpdate, "软件更新"},
	{notifySrcServermon, "服务器监控"},
	{notifySrcNode, "节点"},
	{notifySrcRemote, "远程协助"},
	{notifySrcFiles, "文件"},
}

func notifySourceName(id string) string {
	for _, s := range notifySources {
		if s.ID == id {
			return s.Name
		}
	}
	return id
}

const (
	notifyMaxEvents  = 50      // rolling cap in the store
	notifyDedupeSecs = 10 * 60 // same source+key inside this window is dropped
	notifyFileVer    = 1
)

type notifyEvent struct {
	ID     string `json:"id"`
	Time   int64  `json:"time"` // unix milliseconds
	Source string `json:"source"`
	Key    string `json:"key,omitempty"`
	Title  string `json:"title"`
	Body   string `json:"body,omitempty"`
	Route  string `json:"route,omitempty"` // view-side jump target: an app id, or "setpage:<page>"
}

type notifyFile struct {
	Version int              `json:"version"`
	Events  []notifyEvent    `json:"events"` // newest first
	Dedupe  map[string]int64 `json:"dedupe"` // source\x00key -> last recorded unix ms
	Once    []string         `json:"once"`   // once-only keys already fired
}

type notifyStore struct {
	mu     sync.Mutex
	path   string
	loaded bool
	events []notifyEvent
	dedupe map[string]int64
	once   map[string]bool
	seq    int64
}

var notifySt = &notifyStore{}

// notifyStoreForTests builds an isolated store on a temp path.
func notifyStoreForTests(path string) *notifyStore {
	return &notifyStore{path: path}
}

func (st *notifyStore) filePath() string {
	if st.path != "" {
		return st.path
	}
	return filepath.Join(dataDir(), "notifications.json")
}

func (st *notifyStore) loadLocked() {
	if st.loaded {
		return
	}
	st.loaded = true
	st.dedupe = map[string]int64{}
	st.once = map[string]bool{}
	b, err := os.ReadFile(st.filePath())
	if err != nil {
		return // first run: empty store
	}
	var f notifyFile
	if json.Unmarshal(b, &f) != nil {
		logf("notify", "warn", "notifications.json 解析失败，已按空通知重置")
		return
	}
	st.events = f.Events
	if f.Dedupe != nil {
		st.dedupe = f.Dedupe
	}
	for _, k := range f.Once {
		st.once[k] = true
	}
	// Continue the id sequence past anything already stored so
	// ids stay unique across restarts.
	for _, e := range st.events {
		var n int64
		if _, err := fmt.Sscanf(e.ID, "n%d", &n); err == nil && n > st.seq {
			st.seq = n
		}
	}
}

func (st *notifyStore) saveLocked() {
	f := notifyFile{Version: notifyFileVer, Events: st.events, Dedupe: st.dedupe}
	for k := range st.once {
		f.Once = append(f.Once, k)
	}
	b, err := json.MarshalIndent(f, "", " ")
	if err != nil {
		return
	}
	p := st.filePath()
	tmp := p + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, p)
	}
}

// record appends an event unless the same source+key was recorded
// inside the dedupe window. Returns the stored event and whether
// it was actually recorded. The rolling cap drops the oldest.
func (st *notifyStore) record(source, key, title, body, route string, now time.Time) (notifyEvent, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.loadLocked()
	dk := source + "\x00" + key
	if key != "" {
		if last, ok := st.dedupe[dk]; ok && now.UnixMilli()-last < notifyDedupeSecs*1000 {
			return notifyEvent{}, false
		}
	}
	st.seq++
	ev := notifyEvent{
		ID:     fmt.Sprintf("n%d", st.seq),
		Time:   now.UnixMilli(),
		Source: source, Key: key, Title: title, Body: body, Route: route,
	}
	st.events = append([]notifyEvent{ev}, st.events...)
	if len(st.events) > notifyMaxEvents {
		st.events = st.events[:notifyMaxEvents]
	}
	if key != "" {
		st.dedupe[dk] = ev.Time
	}
	st.saveLocked()
	return ev, true
}

func (st *notifyStore) list() []notifyEvent {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.loadLocked()
	out := make([]notifyEvent, len(st.events))
	copy(out, st.events)
	return out
}

func (st *notifyStore) dismiss(id string) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.loadLocked()
	for i, e := range st.events {
		if e.ID == id {
			st.events = append(st.events[:i], st.events[i+1:]...)
			st.saveLocked()
			return true
		}
	}
	return false
}

func (st *notifyStore) clearSource(source string) int {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.loadLocked()
	kept := st.events[:0]
	n := 0
	for _, e := range st.events {
		if e.Source == source {
			n++
		} else {
			kept = append(kept, e)
		}
	}
	if n > 0 {
		st.events = kept
		st.saveLocked()
	}
	return n
}

// tryOnce reports true exactly once per key (persisted), for
// events that must fire only at the moment something is first
// detected -- e.g. the update checker spotting a new version.
func (st *notifyStore) tryOnce(key string) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.loadLocked()
	if st.once[key] {
		return false
	}
	st.once[key] = true
	st.saveLocked()
	return true
}

// ---------- banner push (SSE) ----------

var notifySubs = struct {
	sync.Mutex
	m map[chan notifyEvent]struct{}
}{m: map[chan notifyEvent]struct{}{}}

func notifyPushBanner(ev notifyEvent) {
	notifySubs.Lock()
	defer notifySubs.Unlock()
	for ch := range notifySubs.m {
		select {
		case ch <- ev:
		default: // a stuck view must never block the daemon
		}
	}
}

// postNotify is THE event hook. Asynchronous and non-blocking;
// failures are logged, never propagated. Honours 设置 → 通知：
// allow off = not recorded at all; banner off = recorded, but no
// banner is pushed.
func postNotify(source, key, title, body, route string) {
	go func() {
		defer func() { _ = recover() }()
		s := settingsGet()
		if !notifyAllowed(s, source) {
			return
		}
		ev, ok := notifySt.record(source, key, title, body, route, time.Now())
		if !ok {
			return // deduped inside 10 minutes
		}
		logf("notify", "info", "通知 [%s] %s", notifySourceName(source), title)
		if notifyBanner(s, source) {
			notifyPushBanner(ev)
		}
	}()
}

// postNotifyOnce is postNotify gated by a persisted once-key: it
// fires only the first time its onceKey is seen, ever. Used by
// the update checker (the "available" status item already lives
// in Notification Center for as long as the update waits; the
// event exists to raise one banner at the detection moment).
func postNotifyOnce(onceKey, source, key, title, body, route string) {
	go func() {
		defer func() { _ = recover() }()
		if !notifySt.tryOnce(onceKey) {
			return
		}
		// Route through postNotify's own goroutine semantics
		// inline (settings gate + dedupe + banner) -- calling
		// postNotify itself would double-async and race the
		// once mark against nothing, which is fine, but inline
		// keeps ordering deterministic for tests of the flow.
		s := settingsGet()
		if !notifyAllowed(s, source) {
			return
		}
		ev, ok := notifySt.record(source, key, title, body, route, time.Now())
		if !ok {
			return
		}
		logf("notify", "info", "通知 [%s] %s", notifySourceName(source), title)
		if notifyBanner(s, source) {
			notifyPushBanner(ev)
		}
	}()
}

// ---------- status-type assembly ----------

type notifyStatusFacts struct {
	SandboxPending bool
	SandboxDetail  string
	UpdatePhase    string // "available" | "downloading" | "ready"
	UpdateDetail   string
	ServerAlert    bool
	ServerDetail   string
}

type notifyStatusItem struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Title  string `json:"title"`
	Body   string `json:"body,omitempty"`
	Action string `json:"action"` // primary button label
	Route  string `json:"route"`
}

// notifyStatusItems is the pure judgement: facts + per-source
// allow switches -> the status items Notification Center shows,
// in a fixed order (sandbox, update, server). Fact gathering is
// gatherNotifyStatusFacts; this function is unit-tested.
func notifyStatusItems(s mjSettings, f notifyStatusFacts) []notifyStatusItem {
	var out []notifyStatusItem
	if f.SandboxPending && notifyAllowed(s, notifySrcSandbox) {
		out = append(out, notifyStatusItem{
			ID: "sandbox-pending", Source: notifySrcSandbox,
			Title: "隔离安装待登记", Body: f.SandboxDetail,
			Action: "去登记", Route: "appstore",
		})
	}
	if (f.UpdatePhase == "available" || f.UpdatePhase == "downloading" || f.UpdatePhase == "ready") && notifyAllowed(s, notifySrcUpdate) {
		body := f.UpdateDetail
		if f.UpdatePhase == "ready" && body == "" {
			body = "更新已下载好，重启即可完成升级"
		}
		out = append(out, notifyStatusItem{
			ID: "update-ready", Source: notifySrcUpdate,
			Title: "软件更新可用", Body: body,
			Action: "去更新", Route: "setpage:update",
		})
	}
	if f.ServerAlert && notifyAllowed(s, notifySrcServermon) {
		out = append(out, notifyStatusItem{
			ID: "server-alert", Source: notifySrcServermon,
			Title: "服务器告警中", Body: f.ServerDetail,
			Action: "看服务器", Route: "servermon",
		})
	}
	return out
}

// gatherNotifyStatusFacts reads each module's existing state.
// No new polling: everything here is a cheap in-memory read done
// when the view asks (Notification Center open / refresh tick).
func gatherNotifyStatusFacts() notifyStatusFacts {
	var f notifyStatusFacts
	// Sandbox: an install that finished and waits at the
	// registration picker (sandboxStatusMap's boxInstall phase).
	if bi, ok := sandboxStatusMap()["boxInstall"].(map[string]any); ok {
		if ph, _ := bi["phase"].(string); ph == "choosing" {
			f.SandboxPending = true
			inst, _ := bi["installer"].(string)
			msg, _ := bi["message"].(string)
			f.SandboxDetail = strings.TrimSpace(strings.Join([]string{inst, msg}, " "))
		}
	}
	// Update: the checker's own status snapshot.
	snap := upd.snapshot()
	f.UpdatePhase, _ = snap["phase"].(string)
	f.UpdateDetail, _ = snap["message"].(string)
	// Server alerts: same rules as the monitor's own evaluation.
	f.ServerAlert, f.ServerDetail = monAlertSummary()
	return f
}

// ---------- HTTP ----------

// notifyHandler serves GET /api/notify: status items (assembled
// live) + recorded events + the server/node widget data, merged
// in one payload so the view needs a single fetch.
func notifyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, map[string]any{"error": "method not allowed"})
		return
	}
	s := settingsGet()
	items := notifyStatusItems(s, gatherNotifyStatusFacts())
	if items == nil {
		items = []notifyStatusItem{}
	}
	evs := notifySt.list()
	if evs == nil {
		evs = []notifyEvent{}
	}
	name, online, hasSnap, cpu, mem := monWidgetSummary()
	writeJSON(w, map[string]any{
		"status": items,
		"events": evs,
		"widgets": map[string]any{
			"server": map[string]any{"name": name, "online": online, "hasSnap": hasSnap, "cpuPct": cpu, "memPct": mem},
		},
	})
}

// notifyDismissHandler: POST /api/notify/dismiss {id} -- one
// event; POST /api/notify/clear-source {source} -- a whole stack.
func notifyDismissHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, map[string]any{"error": "method not allowed"})
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
		writeJSON(w, map[string]any{"error": "参数不对"})
		return
	}
	if notifySt.dismiss(body.ID) {
		logf("notify", "info", "通知已清除（单条 %s）", body.ID)
	}
	writeJSON(w, map[string]any{"ok": true})
}

func notifyClearSourceHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, map[string]any{"error": "method not allowed"})
		return
	}
	var body struct {
		Source string `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Source == "" {
		writeJSON(w, map[string]any{"error": "参数不对"})
		return
	}
	if n := notifySt.clearSource(body.Source); n > 0 {
		logf("notify", "info", "通知已清除（%s 整组 %d 条）", notifySourceName(body.Source), n)
	}
	writeJSON(w, map[string]any{"ok": true})
}

// notifyStreamHandler is the banner channel: SSE, one event per
// recorded notification whose source allows banners. A 25 s
// comment heartbeat keeps proxies from idling the stream out.
func notifyStreamHandler(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, map[string]any{"error": "stream unsupported"})
		return
	}
	ch := make(chan notifyEvent, 8)
	notifySubs.Lock()
	notifySubs.m[ch] = struct{}{}
	notifySubs.Unlock()
	defer func() {
		notifySubs.Lock()
		delete(notifySubs.m, ch)
		notifySubs.Unlock()
	}()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	fl.Flush()
	hb := time.NewTicker(25 * time.Second)
	defer hb.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-hb.C:
			fmt.Fprint(w, ": hb\n\n")
			fl.Flush()
		case ev := <-ch:
			b, _ := json.Marshal(ev)
			fmt.Fprintf(w, "data: %s\n\n", b)
			fl.Flush()
		}
	}
}

// ---------- remote-assist session tracking (for the privacy dot
// and the session start/end events) ----------
//
// The remote mux calls notifyRemoteActivity on every allowed
// operator request (its one hook). A "session" is activity with
// no gap longer than notifyRemoteIdleSecs; the start fires one
// event, and a lazy once-per-second check (started on the first
// activity) fires the end event when the gap elapses. Nothing in
// remote.go's logic changes -- this only observes.

const notifyRemoteIdleSecs = 180

var notifyRemote = struct {
	sync.Mutex
	lastActivity int64 // unix seconds
	active       bool
	watcher      bool
}{}

// remoteSessionActive reports whether an operator session is live
// right now (activity within the idle window). Read-only; backs
// the menu bar privacy indicator (remoteStateMap field).
func remoteSessionActive() bool {
	notifyRemote.Lock()
	defer notifyRemote.Unlock()
	return notifyRemote.active && time.Now().Unix()-notifyRemote.lastActivity <= notifyRemoteIdleSecs
}

func notifyRemoteActivity(operatorName string) {
	notifyRemote.Lock()
	now := time.Now().Unix()
	was := notifyRemote.active && now-notifyRemote.lastActivity <= notifyRemoteIdleSecs
	notifyRemote.lastActivity = now
	notifyRemote.active = true
	startWatcher := !notifyRemote.watcher
	if startWatcher {
		notifyRemote.watcher = true
	}
	notifyRemote.Unlock()
	if !was {
		postNotify(notifySrcRemote, "session-start", "远程协助会话开始",
			"操作者 "+operatorName+" 正在远程协助", "setpage:security")
	}
	if startWatcher {
		go notifyRemoteWatch()
	}
}

func notifyRemoteWatch() {
	for {
		time.Sleep(time.Second)
		notifyRemote.Lock()
		if !notifyRemote.active {
			notifyRemote.watcher = false
			notifyRemote.Unlock()
			return
		}
		if time.Now().Unix()-notifyRemote.lastActivity > notifyRemoteIdleSecs {
			notifyRemote.active = false
			notifyRemote.watcher = false
			notifyRemote.Unlock()
			postNotify(notifySrcRemote, "session-end", "远程协助会话结束",
				"操作者已断开", "setpage:security")
			return
		}
		notifyRemote.Unlock()
	}
}
