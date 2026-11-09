package main

// ---------- built-in PDF viewer (v0.65, detail-fix ⑥) ----------
//
// macOS Preview grammar, rendered by pdf.js in the desktop view.
// The daemon's half is deliberately small:
//
//   GET  /assets/pdfjs/...   the vendored pdf.js runtime (the
//                            library, its worker, the CJK cMaps
//                            and the standard fonts), embedded
//                            in the EXE -- the viewer works fully
//                            offline, every fetch stays on the
//                            local daemon. Binary assets are
//                            stored base64 (.b64, see
//                            web/sync-pdfjs.mjs) and decoded at
//                            serve time.
//   POST /api/pdf/open       {path} -- validate BEFORE any window
//                            opens: the path must resolve inside
//                            Data/files, end in .pdf, exist, fit
//                            the import size cap and carry a real
//                            %PDF- header in its first bytes.
//                            Refusals are answered here and logged
//                            under [pdf]; nothing half-opens.
//   POST /api/pdf/event      {path, event, pages?, detail?} --
//                            the view reports the events only it
//                            can see (document loaded, password
//                            challenge / failure, corrupt body)
//                            so the console hub holds the whole
//                            story under [pdf]. Log-only: it
//                            changes no state.
//
// The document bytes themselves ride the existing file route
// (GET /api/files/raw -- filesResolve-jailed, http.ServeContent
// range support, which is what pdf.js streams with). No new
// byte-serving surface is added.

import (
	"bytes"
	"embed"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed assets/pdfjs
var pdfjsAssets embed.FS

const pdfjsPrefix = "/assets/pdfjs/"

// pdfSniffHeader reports whether the head of a file carries a
// PDF header. The spec allows the "%PDF-" signature to appear
// within the first 1024 bytes (some producers prepend junk), so
// the check is a contains over that window, not a prefix test.
func pdfSniffHeader(head []byte) bool {
	if len(head) > 1024 {
		head = head[:1024]
	}
	return bytes.Contains(head, []byte("%PDF-"))
}

// pdfAssetContentType maps a vendored asset's extension to the
// type pdf.js expects (module scripts and wasm-adjacent data are
// picky about JavaScript MIME in particular).
func pdfAssetContentType(rel string) string {
	switch strings.ToLower(path.Ext(rel)) {
	case ".mjs", ".js":
		return "text/javascript; charset=utf-8"
	case ".ttf":
		return "font/ttf"
	case ".txt":
		return "text/plain; charset=utf-8"
	}
	return "application/octet-stream"
}

// pdfB64Cache holds decoded binary assets (name -> bytes) so a
// .b64 file is decoded once per daemon run, not per fetch.
var pdfB64Cache sync.Map

// pdfAssetBytes returns the bytes of one vendored asset: the
// verbatim embedded file when one exists, else the decoded
// contents of its .b64 sibling.
func pdfAssetBytes(rel string) ([]byte, bool) {
	if b, err := pdfjsAssets.ReadFile("assets/pdfjs/" + rel); err == nil {
		return b, true
	}
	if v, ok := pdfB64Cache.Load(rel); ok {
		return v.([]byte), true
	}
	raw, err := pdfjsAssets.ReadFile("assets/pdfjs/" + rel + ".b64")
	if err != nil {
		return nil, false
	}
	dec, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(string(raw)), ""))
	if err != nil {
		return nil, false
	}
	pdfB64Cache.Store(rel, dec)
	return dec, true
}

// pdfAssetsHandler serves the vendored pdf.js runtime. GET/HEAD
// only; the path is confined to the embedded assets/pdfjs tree
// (embed.FS itself refuses ".." and absolute paths, and the
// explicit check keeps odd URLs honest before that).
func pdfAssetsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rel := strings.TrimPrefix(r.URL.Path, pdfjsPrefix)
	if rel == "" || strings.HasPrefix(rel, "/") || strings.Contains(rel, "\\") ||
		path.Clean("/"+rel) != "/"+rel {
		http.NotFound(w, r)
		return
	}
	b, ok := pdfAssetBytes(rel)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", pdfAssetContentType(rel))
	// The runtime is versioned with the EXE: let the view cache
	// it hard, updates arrive as a new EXE with new bytes.
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeContent(w, r, path.Base(rel), time.Time{}, bytes.NewReader(b))
}

// pdfOpenHandler validates a would-be PDF open (see header).
func pdfOpenHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeJSON(w, map[string]any{"error": "请求内容不对"})
		return
	}
	abs, name, fail := pdfValidatePath(body.Path)
	if fail != "" {
		logf("pdf", "warn", "拒绝打开 PDF %s：%s", pdfLogName(body.Path), fail)
		writeJSON(w, map[string]any{"error": fail})
		return
	}
	fi, err := os.Stat(abs)
	if err != nil || fi.IsDir() {
		logf("pdf", "warn", "拒绝打开 PDF %s：文件不存在", name)
		writeJSON(w, map[string]any{"error": "文件不存在"})
		return
	}
	if fi.Size() > maxImportBytes {
		msg := fmt.Sprintf("文件太大（%d MB，上限 %d MB）", fi.Size()>>20, maxImportBytes>>20)
		logf("pdf", "warn", "拒绝打开 PDF %s：%s", name, msg)
		writeJSON(w, map[string]any{"error": msg})
		return
	}
	f, err := os.Open(abs)
	if err != nil {
		logf("pdf", "warn", "拒绝打开 PDF %s：读不到文件", name)
		writeJSON(w, map[string]any{"error": "文件读不到"})
		return
	}
	head := make([]byte, 1024)
	n, _ := io.ReadFull(f, head)
	_ = f.Close()
	if !pdfSniffHeader(head[:n]) {
		logf("pdf", "warn", "拒绝打开 PDF %s：内容不是 PDF（没有 %%PDF- 文件头）", name)
		writeJSON(w, map[string]any{"error": "这个文件的内容不是 PDF（改名成 .pdf 的其他文件打不开）"})
		return
	}
	logf("pdf", "info", "打开 PDF：%s（%d 字节）", name, fi.Size())
	writeJSON(w, map[string]any{"ok": true, "name": name, "size": fi.Size()})
}

// pdfValidatePath resolves a frontend path inside Data/files
// and checks the .pdf extension. It returns the absolute path,
// the display name, and -- on failure -- the user-facing reason.
func pdfValidatePath(rel string) (string, string, string) {
	abs, err := filesResolve(rel)
	if err != nil {
		return "", "", "文件名不对"
	}
	if strings.ToLower(filepath.Ext(abs)) != ".pdf" {
		return "", "", "这不是 PDF 文件"
	}
	return abs, filepath.Base(abs), ""
}

// pdfLogName renders a frontend-supplied path for a refusal log
// line even when it does not resolve: base name only, length-
// capped, so a junk path can neither inject log lines nor leak
// a host path into the console.
func pdfLogName(rel string) string {
	name := filepath.Base(strings.ReplaceAll(rel, "\\", "/"))
	if name == "" || name == "." || name == "/" {
		return "（无名）"
	}
	if len(name) > 80 {
		name = name[:80] + "…"
	}
	return name
}

// pdfEventKinds is the whitelist of view-reported events. The
// level each kind logs at is part of the table: a password
// challenge is routine (info), a failed password is a warning,
// a corrupt body is an error.
var pdfEventKinds = map[string]string{
	"loaded":          "info",
	"password":        "info",
	"password-failed": "warn",
	"corrupt":         "error",
	"error":           "error",
}

// pdfEventHandler logs view-side viewer events (see header).
func pdfEventHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path   string `json:"path"`
		Event  string `json:"event"`
		Pages  int    `json:"pages"`
		Detail string `json:"detail"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeJSON(w, map[string]any{"error": "请求内容不对"})
		return
	}
	level, ok := pdfEventKinds[body.Event]
	if !ok {
		writeJSON(w, map[string]any{"error": "不认识的事件"})
		return
	}
	abs, err := filesResolve(body.Path)
	if err != nil {
		writeJSON(w, map[string]any{"error": "文件名不对"})
		return
	}
	name := filepath.Base(abs)
	detail := strings.NewReplacer("\r", " ", "\n", " ").Replace(body.Detail)
	if len(detail) > 200 {
		detail = detail[:200] + "…"
	}
	switch body.Event {
	case "loaded":
		logf("pdf", level, "PDF 已载入：%s（%d 页）", name, body.Pages)
	case "password":
		logf("pdf", level, "PDF 需要密码：%s", name)
	case "password-failed":
		logf("pdf", level, "PDF 密码不对：%s", name)
	case "corrupt":
		logf("pdf", level, "PDF 内容损坏、解析失败：%s（%s）", name, detail)
	default:
		logf("pdf", level, "PDF 查看出错：%s（%s）", name, detail)
	}
	writeJSON(w, map[string]any{"ok": true})
}
