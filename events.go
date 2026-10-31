package main

// events.go -- v0.59 architecture block 3: the daemon-side change
// bus, plus the generalization of /api/view/stream into the
// change feed that carries it to the desktop view.
//
// Why this exists: before v0.59 the daemon's state was the single
// truth but most pages held fetch-on-open snapshots of it -- the
// user's item #8 field report (2026-10-30): rename a file in the
// Files app and the desktop did not follow until a page switch.
// The bus turns mutations into events; open surfaces subscribe,
// re-pull their slice through the typed API and re-render in
// place. It absorbs the fix proposed that night (generalize the
// /api/view/stream SSE channel into a change bus) wholesale.
//
// Topics (payload discipline: an event carries topic + id ONLY --
// never subscription URLs, keys, file contents or any other
// secret; the id names WHAT changed so the view can re-pull the
// slice it shows, nothing more):
//
//	files    a Data/files entry was created/renamed/deleted/imported (id = file name)
//	layout   Data/layout.json was saved (desktop arrangement)
//	webapps  the web-app list or its tombstones changed
//	settings settings.json was saved
//	nodes    the node library changed, or the core connected /
//	         disconnected / switched (id = nodes.json for library edits)
//	notify   a notification record was added/cleared
//	storeapps  the store-app or isolated-install registry changed
//	windows  an external window was claimed / closed / focused (id = hwnd)
//	vault    the vault locked or unlocked
//	update   the update channel state changed (check/download/apply)
//
// Producers: the datastore emits for its mapped registry names on
// every successful save (see dsBusTopics), and feature code emits
// explicitly where the mutation does not pass the data layer
// (files content, window registry, vault and update lifecycle).
//
// Slow-consumer policy: each subscriber owns a bounded buffer
// (busSubBuffer). A full buffer drops the event FOR THAT
// SUBSCRIBER ONLY and counts the drop; a stuck page can never
// block the daemon or starve other subscribers. Drops are logged
// under [bus], throttled, with the running total.

import (
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// busEvent is the wire shape of one change-feed message on
// /api/view/stream (alongside the older viewCommand messages,
// which carry "cmd" instead of "topic"). It is part of the API
// contract (apicontract.go) and mirrored in web/src/api-types.ts.
type busEvent struct {
	Topic string `json:"topic"`
	ID    string `json:"id,omitempty"`
	Ts    int64  `json:"ts"` // unix ms
}

const busSubBuffer = 32

type busSubscriber struct {
	ch     chan busEvent
	topics map[string]bool // nil = all topics
}

type eventBus struct {
	mu      sync.Mutex
	subs    map[*busSubscriber]struct{}
	dropped atomic.Int64
	lastLog atomic.Int64 // unix ms of the last drop log line
}

var bus = &eventBus{subs: map[*busSubscriber]struct{}{}}

// busSubscribe registers a subscriber for the given topics (no
// topics = all) and returns its channel plus the unsubscribe
// function. Lifecycle is logged under [bus] with the live
// subscriber count.
func busSubscribe(topics ...string) (chan busEvent, func()) {
	s := &busSubscriber{ch: make(chan busEvent, busSubBuffer)}
	if len(topics) > 0 {
		s.topics = map[string]bool{}
		for _, t := range topics {
			s.topics[t] = true
		}
	}
	bus.mu.Lock()
	bus.subs[s] = struct{}{}
	n := len(bus.subs)
	bus.mu.Unlock()
	logf("bus", "info", "变更订阅者接入（当前 %d 个）", n)
	var once sync.Once
	return s.ch, func() {
		once.Do(func() {
			bus.mu.Lock()
			delete(bus.subs, s)
			n := len(bus.subs)
			bus.mu.Unlock()
			logf("bus", "info", "变更订阅者断开（当前 %d 个）", n)
		})
	}
}

// busPublish delivers one event to every interested subscriber.
// It never blocks: a subscriber whose buffer is full misses this
// event (counted + throttled-logged) -- the daemon's mutation
// paths must never wait on a view.
func busPublish(topic, id string) {
	ev := busEvent{Topic: topic, ID: id, Ts: time.Now().UnixMilli()}
	bus.mu.Lock()
	defer bus.mu.Unlock()
	for s := range bus.subs {
		if s.topics != nil && !s.topics[topic] {
			continue
		}
		select {
		case s.ch <- ev:
		default:
			d := bus.dropped.Add(1)
			now := time.Now().UnixMilli()
			if last := bus.lastLog.Load(); now-last > 5000 && bus.lastLog.CompareAndSwap(last, now) {
				logf("bus", "warn", "订阅者处理不过来，已丢弃 %d 条变更事件（最新一条 topic=%s）", d, topic)
			}
		}
	}
}

// busSubscriberCount is the live subscriber count (tests +
// diagnostics).
// busDropCount is the running total of events dropped for
// lagging subscribers (test hook + diagnostics).
func busDropCount() int64 { return bus.dropped.Load() }

func busSubscriberCount() int {
	bus.mu.Lock()
	defer bus.mu.Unlock()
	return len(bus.subs)
}

// dsBusTopics maps datastore registry names to bus topics. A
// successful dsSave of a mapped name publishes (topic, registry
// rel path). Registry names NOT listed here deliberately emit
// nothing: wingeom (high-frequency, view-local), remote/ssh/
// servermon/apps/sandbox/component/openclaw/ffstate state (their
// surfaces are fetch-on-open pages whose only writer is the page
// itself, or whose changes the owning feature already announces
// on a dedicated channel), accel + version stamps (tiny flags
// the owning handler announces itself when user-visible), and
// update-token (a credential -- never announced).
var dsBusTopics = map[string]string{
	"settings":      "settings",
	"nodes":         "nodes",
	"links":         "nodes",
	"webapps":       "webapps",
	"tombstones":    "webapps",
	"layout":        "layout",
	"notifications": "notify",
	"apps-registry": "storeapps",
	"sandbox-apps":  "storeapps",
}

// dsBusEmit is called by the datastore after a successful save.
func dsBusEmit(name string) {
	if topic, ok := dsBusTopics[name]; ok {
		if spec, err := dsSpecFor(name); err == nil {
			busPublish(topic, spec.rel)
		}
	}
}

// busWindows announces an external-window registry change
// (claim / close / focus). The id is the hwnd in decimal -- a
// window handle is not a secret.
func busWindows(hwnd uint64) {
	busPublish("windows", strconv.FormatUint(hwnd, 10))
}
