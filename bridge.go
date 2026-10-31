package main

// bridge.go -- v0.60 architecture block 4: the strangler bridge
// between package main and the extracted foundation packages
// (internal/diag, internal/datastore, internal/events).
//
// Two kinds of seams live here:
//
//   - Wiring: the foundation packages own no domain state; what
//     they need from the daemon (the Data directory, the core
//     log path, the live status fields, the save->bus hook) is
//     injected through their hook variables in init() below,
//     which runs before main() and therefore before any daemon
//     activity can reach them.
//
//   - Forwarders: ultra-shared entry points (logf, the ds* data
//     layer API, the bus* change-feed API) keep their historical
//     unqualified names in package main as one-line forwarders,
//     so the hundreds of existing call sites stay untouched in
//     this block. The implementations live in the packages; as
//     feature domains move out in later blocks, their call sites
//     qualify directly and this file shrinks.

import (
	"os"

	"mjlight/internal/datastore"
	"mjlight/internal/diag"
	"mjlight/internal/events"
)

func init() {
	diag.DataDirFunc = dataDir
	diag.CoreLogPathFunc = coreLogPath
	diag.StatusProvider = diagStatusProvider
	datastore.RootFunc = dataDir
	datastore.SaveHook = events.EmitForSave
}

// diagStatusProvider supplies the domain-owned fields of the
// diagnostics status map (see diag.StatusMap). Every value is
// read from live program state exactly as the pre-layering
// diagStatusMap read it.
func diagStatusProvider() map[string]any {
	coreMu.Lock()
	coreUp := coreRunningLocked()
	coreMu.Unlock()
	extMu.Lock()
	extN := 0
	for _, e := range extWins {
		if !e.Shell {
			extN++
		}
	}
	extMu.Unlock()
	sshLiveMu.Lock()
	live := len(sshLive)
	sshLiveMu.Unlock()
	nodeN := len(nodes) // plain global, read the same way the other handlers do
	return map[string]any{
		"version":        AppVersion,
		"coreRunning":    coreUp,
		"nodeConnected":  coreUp && nodeN > 0,
		"nodeCount":      nodeN,
		"accelEnabled":   accelEnabled(),
		"openclaw":       map[string]any{"installed": ocInstalled(), "running": ocRunning()},
		"extWindows":     extN,
		"sshConnections": len(sshLoadConns()),
		"sshLive":        live,
	}
}

// logf is THE logging entry point for the whole program:
// logf("core", "error", "sing-box 启动失败：%v", err). Levels are
// "info", "warn", "error". The hub itself is internal/diag; this
// forwarder keeps the historical call form for package main.
func logf(module, level, format string, args ...any) {
	diag.Logf(module, level, format, args...)
}

// ---------- datastore forwarders (implementation: internal/datastore) ----------

type dsStatus = datastore.Status

const (
	dsOK        = datastore.OK
	dsMissing   = datastore.Missing
	dsCorrupt   = datastore.Corrupt
	dsRecovered = datastore.Recovered
)

type dsSpec = datastore.Spec

type dsManifest = datastore.Manifest

func dsSave(name string, b []byte) error { return datastore.Save(name, b) }

func dsLoad(name string) ([]byte, dsStatus, error) { return datastore.Load(name) }

func dsSaveFile(path string, b []byte, perm os.FileMode, validate func([]byte) error, backup bool) error {
	return datastore.SaveFile(path, b, perm, validate, backup)
}

func dsLoadFile(path string, validate func([]byte) error) ([]byte, dsStatus, error) {
	return datastore.LoadFile(path, validate)
}

func dsRemoveFile(path string) { datastore.RemoveFile(path) }

func dsPath(name string) (string, error) { return datastore.Path(name) }

func dsSpecFor(name string) (dsSpec, error) { return datastore.SpecFor(name) }

func dsNoteLoaded(name string, spec dsSpec) { datastore.NoteLoaded(name, spec) }

func dsJSONValidatorOf[T any](zero T) func([]byte) error {
	return datastore.JSONValidatorOf(zero)
}

// ---------- events forwarders (implementation: internal/events) ----------

type busEvent = events.BusEvent

func busSubscribe(topics ...string) (chan busEvent, func()) {
	return events.Subscribe(topics...)
}

func busPublish(topic, id string) { events.Publish(topic, id) }

func busWindows(hwnd uint64) { events.Windows(hwnd) }

func busDropCount() int64 { return events.DropCount() }

func busSubscriberCount() int { return events.SubscriberCount() }

const busSubBuffer = events.SubBuffer
