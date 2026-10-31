package main

// apicontract.go -- v0.59 architecture block 3: the typed
// front<->back API contract for the LOCAL mux.
//
// Before v0.59 the daemon's JSON responses were mostly ad-hoc
// map[string]any literals: a renamed or dropped field compiled
// fine and failed only in the user's hands. Now the state-bearing
// endpoints the desktop view renders from marshal the NAMED
// structs below, a generator (genAPITypes, driven by the
// -apigen / -apigen-out flags in main) emits
// web/src/api-types.ts from them, and both the Go test suite
// (TestAPIContractFresh) and the CI verify job (regenerate +
// git-diff) fail when the committed TypeScript drifts from the
// Go source of truth. The frontend api client
// (web/src/b1/apiclient.ts) returns these types.
//
// Scope (the full endpoint inventory lives in the block-3
// report): state-bearing JSON endpoints the page parses into
// view state are IN. Out, with reasons: streaming/binary routes
// (thumbnails, icons, wallpaper, file downloads -- bytes, not
// JSON state), the SSE streams (their message types ARE in the
// contract: busEvent, viewCommand), /api/nodepro/status (a
// semi-structured aggregate whose per-node entries carry
// protocol-specific fields the view treats opaquely), the
// legacy workstation action endpoints (/api/list, /api/run,
// /api/open, /api/openurl -- command results, not view state),
// the SSH/SFTP + servermon + sandbox + openclaw feature areas
// (their pages are self-contained writers/readers of their own
// endpoints; they adopt the error envelope opportunistically,
// not the struct registry), and the ENTIRE remote-assist mux
// (byte-compatible by standing rule -- untouched).
//
// Errors: in-scope endpoints fail through writeAPIError, the
// uniform envelope {"error":{"code","message"}} with a stable
// code string, logged via logf under [api]. The view's api()
// helper normalizes the envelope back to the legacy string
// field (+ errorCode) so existing page code surfaces errors
// exactly as before.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"
)

// ---------- uniform error envelope + ok ----------

// apiError is the wire error body: a stable machine code plus
// the human message the view shows (the same Chinese strings the
// handlers have always produced).
type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// apiErrorEnvelope is the top-level error response shape.
type apiErrorEnvelope struct {
	Error apiError `json:"error"`
}

// okResponse is the success body of the simple mutation
// endpoints ({"ok":true}).
type okResponse struct {
	OK bool `json:"ok"`
}

// writeAPIError writes the uniform envelope with the given HTTP
// status and stable code, and logs the failure under [api] with
// its code so a console lookup finds every contract-level error
// by code.
func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	logf("api", "warn", "接口错误 %s：%s", code, message)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(apiErrorEnvelope{Error: apiError{Code: code, Message: message}})
}

// apiErrBadRequest is writeAPIError with 400 for the common
// malformed-input case.
func apiErrBadRequest(w http.ResponseWriter, code, message string) {
	writeAPIError(w, http.StatusBadRequest, code, message)
}

// Stable error codes (the view may branch on errorCode; the
// strings are contract -- do not rename casually):
const (
	errCodeBadRequest = "bad_request"
	errCodeBadJSON    = "bad_json"
	errCodeNotFound   = "not_found"
	errCodeInvalid    = "invalid_value"
	errCodeInternal   = "internal"
	errCodeMethod     = "method_not_allowed"
)

// ---------- contract response types ----------

// systemInfo is GET /api/system.
type systemInfo struct {
	App          string `json:"app"`
	Version      string `json:"version"`
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	NumCPU       int    `json:"numCPU"`
	Hostname     string `json:"hostname"`
	Home         string `json:"home"`
	ExeDir       string `json:"exeDir"`
	DataDir      string `json:"dataDir"`
	DataFallback bool   `json:"dataFallback"`
	Time         string `json:"time"`
}

// settingsResponse is GET /api/settings: the flattened settings
// struct plus the fresh-install flag. settingsMap additionally
// merges the dynamic per-app notification keys
// (notify_allow_app_<id> / notify_banner_app_<id>) on top --
// those are Record<string, boolean> by nature and are NOT part
// of this fixed shape.
type settingsResponse struct {
	mjSettings
	Fresh bool `json:"fresh"`
}

// webAppsResponse is GET /api/apps.
type webAppsResponse struct {
	Apps []webApp `json:"apps"`
}

// webAppSaveResponse is POST /api/apps/save when it returns the
// updated list (winmode updates); simple saves return
// okResponse.
type webAppSaveResponse struct {
	OK   bool     `json:"ok"`
	Apps []webApp `json:"apps"`
}

// layoutPoint / layoutFolder / layoutBlob describe the
// frontend-owned layout document served raw by GET /api/layout
// (the daemon passes the stored bytes through; this struct is
// the shape both sides agree on, and the daemon's own collision
// check parses Pos through it).
type layoutPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type layoutFolder struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Items []string `json:"items"`
}

type layoutBlob struct {
	Pos         map[string]layoutPoint `json:"pos"`
	Folders     []layoutFolder         `json:"folders"`
	Opens       map[string]int64       `json:"opens"`
	Added       map[string]int64       `json:"added"`
	HiddenFiles []string               `json:"hiddenFiles"`
}

// notifyServerWidget is the servermon summary inside the
// notification-center payload.
type notifyServerWidget struct {
	Name    string  `json:"name"`
	Online  bool    `json:"online"`
	HasSnap bool    `json:"hasSnap"`
	CPUPct  float64 `json:"cpuPct"`
	MemPct  float64 `json:"memPct"`
}

// notifyWidgets groups the notification-center widgets.
type notifyWidgets struct {
	Server notifyServerWidget `json:"server"`
}

// notifyResponse is GET /api/notify.
type notifyResponse struct {
	Status  []notifyStatusItem `json:"status"`
	Events  []notifyEvent      `json:"events"`
	Widgets notifyWidgets      `json:"widgets"`
}

// extWinListResponse is GET /api/extwin.
type extWinListResponse struct {
	Windows []*extWindow `json:"windows"`
}

// libFileItem is one entry of GET /api/files (the library root).
type libFileItem struct {
	Name  string `json:"name"`
	Path  string `json:"path"` // Data/files-relative, slash separated
	Size  int64  `json:"size"`
	Ext   string `json:"ext"`
	Kind  string `json:"kind"`
	Dir   bool   `json:"dir"`
	Mtime int64  `json:"mtime"` // unix ms (import time; see files.go)
}

// filesListResponse is GET /api/files.
type filesListResponse struct {
	Files []libFileItem `json:"files"`
	Dir   string        `json:"dir"`
}

// libFileSubItem is one entry of GET /api/files/sub.
type libFileSubItem struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	Kind  string `json:"kind"`
	Dir   bool   `json:"dir"`
	Mtime int64  `json:"mtime"` // unix ms
}

// filesSubListResponse is GET /api/files/sub.
type filesSubListResponse struct {
	Files []libFileSubItem `json:"files"`
}

// fileRenameResponse is POST /api/files/rename on success.
type fileRenameResponse struct {
	OK   bool   `json:"ok"`
	Name string `json:"name"`
}

// filesUnzipResponse is POST /api/files/unzip on success.
type filesUnzipResponse struct {
	OK    bool   `json:"ok"`
	Dir   string `json:"dir"`
	Count int    `json:"count"`
}

// fileImportEntry is one successfully imported file of
// POST /api/files/import.
type fileImportEntry struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// fileImportError is one rejected file of POST /api/files/import
// (a per-file failure inside a 200 response, not the envelope).
type fileImportError struct {
	Name  string `json:"name"`
	Error string `json:"error"`
}

// filesImportResponse is POST /api/files/import.
type filesImportResponse struct {
	Imported []fileImportEntry `json:"imported"`
	Errors   []fileImportError `json:"errors"`
}

// coreStatusResponse is POST /api/core (status/start/stop).
type coreStatusResponse struct {
	Running bool   `json:"running"`
	Message string `json:"message"`
}

// coreLogResponse is GET /api/core/log.
type coreLogResponse struct {
	Log string `json:"log"`
}

// ffStatusResponse is GET /api/firefox. Available /
// AvailableSize / UpdateAvailable are present only when the
// release-manifest fetch succeeded.
type ffStatusResponse struct {
	Installed       bool   `json:"installed"`
	Version         string `json:"version"`
	Phase           string `json:"phase"`
	Progress        int64  `json:"progress"`
	Size            int64  `json:"size"`
	Message         string `json:"message"`
	ProcAlive       bool   `json:"procAlive"`
	ProcMode        string `json:"procMode"`
	Available       string `json:"available,omitempty"`
	AvailableSize   int64  `json:"availableSize,omitempty"`
	UpdateAvailable *bool  `json:"updateAvailable,omitempty"`
}

// compStatusRow is one entry of GET /api/components.
// DiskBytes / HasIcon are present for installed components;
// Available / AvailableSize / UpdateAvailable only when the
// release-manifest fetch succeeded.
type compStatusRow struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Blurb           string `json:"blurb"`
	Installed       bool   `json:"installed"`
	Version         string `json:"version"`
	Phase           string `json:"phase"`
	Progress        int64  `json:"progress"`
	Size            int64  `json:"size"`
	Message         string `json:"message"`
	DiskBytes       *int64 `json:"diskBytes,omitempty"`
	HasIcon         bool   `json:"hasIcon,omitempty"`
	Available       string `json:"available,omitempty"`
	AvailableSize   int64  `json:"availableSize,omitempty"`
	UpdateAvailable *bool  `json:"updateAvailable,omitempty"`
}

// componentsResponse is GET /api/components.
type componentsResponse struct {
	Components []compStatusRow `json:"components"`
}

// accelStatusResponse is GET/POST /api/accel.
type accelStatusResponse struct {
	Enabled     bool `json:"enabled"`
	CoreRunning bool `json:"coreRunning"`
	Active      bool `json:"active"`
}

// vaultStatusResponse is GET /api/vault/status. Shards /
// TotalBytes / Sealed are present when the vault is enabled;
// Progress / ProgressTotal during sealing/unlocking. (Status
// projection only -- no vault format or crypto lives here.)
type vaultStatusResponse struct {
	Enabled       bool   `json:"enabled"`
	Phase         string `json:"phase"`
	Locked        bool   `json:"locked"`
	Partial       bool   `json:"partial"`
	HasKey        bool   `json:"hasKey"`
	LastError     string `json:"lastError"`
	Shards        *int   `json:"shards,omitempty"`
	TotalBytes    *int64 `json:"totalBytes,omitempty"`
	Sealed        *bool  `json:"sealed,omitempty"`
	Progress      *int64 `json:"progress,omitempty"`
	ProgressTotal *int64 `json:"progressTotal,omitempty"`
}

// hotkeyStatusResponse is GET /api/hotkey/status.
type hotkeyStatusResponse struct {
	Key        string `json:"key"`
	Registered bool   `json:"registered"`
	Supported  bool   `json:"supported"`
	Note       string `json:"note"`
	Status     string `json:"status"`
}

// updateSnapshot is GET /api/update and the success body of the
// update mutation endpoints.
type updateSnapshot struct {
	Current  string `json:"current"`
	Token    bool   `json:"token"`
	Phase    string `json:"phase"`
	Latest   string `json:"latest"`
	Size     int64  `json:"size"`
	Progress int64  `json:"progress"`
	Message  string `json:"message"`
}

// updateTokenResponse is POST /api/update/token.
type updateTokenResponse struct {
	OK    bool `json:"ok"`
	Token bool `json:"token"`
}

// updateApplyResponse is POST /api/update/apply on success.
type updateApplyResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// ---------- codegen ----------

// contractType binds the emitted TypeScript name to a Go type.
type contractType struct {
	Name string
	Type reflect.Type
}

// apiContractTypes is the registry the generator emits. Every
// struct reachable from these roots (nested named structs) is
// emitted too, under its Go type name with the first letter
// upper-cased when the TS name differs (webApp -> WebApp,
// extWindow -> ExtWindow, busEvent -> BusEvent,
// viewCommand -> ViewCommand, mjSettings is reached through
// settingsResponse's embedding and emitted as MjSettings).
var apiContractTypes = []contractType{
	{"APIError", reflect.TypeOf(apiError{})},
	{"APIErrorEnvelope", reflect.TypeOf(apiErrorEnvelope{})},
	{"OkResponse", reflect.TypeOf(okResponse{})},
	{"SystemInfo", reflect.TypeOf(systemInfo{})},
	{"SettingsResponse", reflect.TypeOf(settingsResponse{})},
	{"WebApp", reflect.TypeOf(webApp{})},
	{"WebAppsResponse", reflect.TypeOf(webAppsResponse{})},
	{"WebAppSaveResponse", reflect.TypeOf(webAppSaveResponse{})},
	{"LayoutBlob", reflect.TypeOf(layoutBlob{})},
	{"NotifyResponse", reflect.TypeOf(notifyResponse{})},
	{"ExtWinListResponse", reflect.TypeOf(extWinListResponse{})},
	{"FilesListResponse", reflect.TypeOf(filesListResponse{})},
	{"FilesSubListResponse", reflect.TypeOf(filesSubListResponse{})},
	{"FileRenameResponse", reflect.TypeOf(fileRenameResponse{})},
	{"FilesUnzipResponse", reflect.TypeOf(filesUnzipResponse{})},
	{"FilesImportResponse", reflect.TypeOf(filesImportResponse{})},
	{"CoreStatusResponse", reflect.TypeOf(coreStatusResponse{})},
	{"CoreLogResponse", reflect.TypeOf(coreLogResponse{})},
	{"FFStatusResponse", reflect.TypeOf(ffStatusResponse{})},
	{"ComponentsResponse", reflect.TypeOf(componentsResponse{})},
	{"AccelStatusResponse", reflect.TypeOf(accelStatusResponse{})},
	{"VaultStatusResponse", reflect.TypeOf(vaultStatusResponse{})},
	{"HotkeyStatusResponse", reflect.TypeOf(hotkeyStatusResponse{})},
	{"UpdateSnapshot", reflect.TypeOf(updateSnapshot{})},
	{"UpdateTokenResponse", reflect.TypeOf(updateTokenResponse{})},
	{"UpdateApplyResponse", reflect.TypeOf(updateApplyResponse{})},
	{"BusEvent", reflect.TypeOf(busEvent{})},
	{"ViewCommand", reflect.TypeOf(viewCommand{})},
}

var rawMessageType = reflect.TypeOf(json.RawMessage{})

func tsTypeName(t reflect.Type) string {
	if t.Name() == "" {
		return ""
	}
	n := t.Name()
	return strings.ToUpper(n[:1]) + n[1:]
}

// genAPITypes renders web/src/api-types.ts from the registry.
// The output is fully deterministic: roots sorted by emitted
// name, discovered types sorted by emitted name, fields in Go
// declaration order. The freshness test and the CI gate diff a
// regeneration against the committed file.
func genAPITypes() string {
	roots := map[string]reflect.Type{}
	names := map[reflect.Type]string{} // struct type -> emitted name
	for _, ct := range apiContractTypes {
		roots[ct.Name] = ct.Type
		if ct.Type.Kind() == reflect.Struct {
			names[ct.Type] = ct.Name
		}
	}
	// Discover nested named structs until fixpoint.
	queue := make([]reflect.Type, 0, len(apiContractTypes))
	for _, ct := range apiContractTypes {
		queue = append(queue, ct.Type)
	}
	seen := map[reflect.Type]bool{}
	for len(queue) > 0 {
		t := queue[0]
		queue = queue[1:]
		if seen[t] {
			continue
		}
		seen[t] = true
		for _, st := range structTypesIn(t) {
			if _, ok := names[st]; !ok {
				names[st] = tsTypeName(st)
			}
			queue = append(queue, st)
		}
	}
	// Name collisions between distinct Go types would corrupt
	// the output silently; refuse instead.
	byName := map[string]reflect.Type{}
	for st, n := range names {
		if prev, dup := byName[n]; dup && prev != st {
			return "// apigen ERROR: type name collision: " + n + "\n"
		}
		byName[n] = st
	}

	ordered := make([]string, 0, len(byName))
	for n := range byName {
		ordered = append(ordered, n)
	}
	sort.Strings(ordered)

	var b strings.Builder
	b.WriteString("/* GENERATED by the daemon's API contract codegen (apicontract.go).\n")
	b.WriteString("   Do not edit by hand: change the Go structs, then regenerate with\n")
	b.WriteString("   `npm run api:gen`. Freshness is enforced by TestAPIContractFresh\n")
	b.WriteString("   and by the CI verify job (regenerate + git diff). */\n\n")
	for _, n := range ordered {
		emitStructTS(&b, n, byName[n], names)
	}
	_ = roots
	return b.String()
}

// structTypesIn returns the named struct types directly
// referenced by t's fields (through pointers, slices, maps and
// embeddings).
func structTypesIn(t reflect.Type) []reflect.Type {
	var out []reflect.Type
	var walk func(t reflect.Type)
	walk = func(t reflect.Type) {
		switch t.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array:
			walk(t.Elem())
		case reflect.Map:
			walk(t.Key())
			walk(t.Elem())
		case reflect.Struct:
			if t != rawMessageType {
				out = append(out, t)
			}
		}
	}
	if t.Kind() == reflect.Struct {
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.PkgPath != "" && !f.Anonymous {
				continue // unexported, non-embedded: never marshaled
			}
			walk(f.Type)
		}
	} else {
		walk(t)
	}
	return out
}

func emitStructTS(b *strings.Builder, name string, t reflect.Type, names map[reflect.Type]string) {
	fmt.Fprintf(b, "export interface %s {\n", name)
	emitFieldsTS(b, t, names, map[reflect.Type]bool{})
	b.WriteString("}\n\n")
}

func emitFieldsTS(b *strings.Builder, t reflect.Type, names map[reflect.Type]string, guard map[reflect.Type]bool) {
	if guard[t] {
		return
	}
	guard[t] = true
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			// Embedded struct: encoding/json flattens it.
			emitFieldsTS(b, f.Type, names, guard)
			continue
		}
		if f.PkgPath != "" {
			continue // unexported: not marshaled
		}
		if f.Tag.Get("mjapi") == "-" {
			continue // excluded from the API contract by the owner
		}
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		parts := strings.Split(tag, ",")
		jname := parts[0]
		if jname == "" {
			jname = f.Name
		}
		optional := false
		for _, p := range parts[1:] {
			if p == "omitempty" {
				optional = true
			}
		}
		ts := tsTypeOf(f.Type, names)
		if optional && f.Type.Kind() == reflect.Pointer {
			ts = strings.TrimSuffix(ts, " | null")
		}
		if optional {
			fmt.Fprintf(b, "  %s?: %s;\n", jname, ts)
		} else {
			fmt.Fprintf(b, "  %s: %s;\n", jname, ts)
		}
	}
}

// tsTypeOf maps a Go field type to its TypeScript type.
func tsTypeOf(t reflect.Type, names map[reflect.Type]string) string {
	if t == rawMessageType {
		return "unknown"
	}
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Pointer:
		if t.Elem().Kind() == reflect.Struct {
			if n, ok := names[t.Elem()]; ok {
				return n
			}
		}
		return tsTypeOf(t.Elem(), names) + " | null"
	case reflect.Slice, reflect.Array:
		el := tsTypeOf(t.Elem(), names)
		if strings.Contains(el, " ") || strings.Contains(el, "|") {
			return "(" + el + ")[]"
		}
		return el + "[]"
	case reflect.Map:
		return "Record<string, " + tsTypeOf(t.Elem(), names) + ">"
	case reflect.Struct:
		if n, ok := names[t]; ok {
			return n
		}
		return "unknown"
	case reflect.Interface:
		return "unknown"
	}
	return "unknown"
}
