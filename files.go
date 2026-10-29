package main

// ---------- host file import onto the desktop (v0.32) ----------
//
// The host PC's files and the terminal's desktop meet here, on the
// terminal's terms: the user hands files IN (drag onto the desktop,
// the desktop menu's import picker, or paste), the daemon copies
// them into Data/files/ and the originals on the host are never
// touched. Imported files become a new desktop icon kind that mixes
// with the app icons in the same free layout, and every open route
// stays INSIDE the system (in-system viewers, the store's
// SumatraPDF, the isolated browser) -- nothing is silently handed
// to the host's default programs. The one way back out is the
// explicit "export to computer" menu item, which downloads through
// the desktop view into the host's Downloads folder and says so.
//
//	Endpoints:
//	  GET  /api/files            list Data/files (top level)
//	  POST /api/files/import     multipart upload, streamed, 500 MB/file
//	  POST /api/files/rename     {old, new} -- top-level entries
//	  POST /api/files/delete     {name}     -- top-level entries
//	  GET  /api/files/raw?path=&dl=1   inline serve / export download
//	  GET  /api/files/thumb?name=      ~128px JPEG thumbnail (jpg/png/gif)
//	  GET  /api/files/text?path=       plain text, or DOCX text preview
//	  GET  /api/files/csv?path=        CSV/TSV parsed to rows (capped)
//	  POST /api/files/openpdf    {path} -- SumatraPDF if installed, else isolated Chrome
//	  POST /api/files/run        {path} -- exe/bat/cmd/msi, frontend confirms first
//	  POST /api/files/unzip      {path} -- extract a .zip into a sibling folder
//
// Everything is logged under module "files" (registered in README's
// logging conventions with this version). File paths from the
// frontend are Data/files-relative and slash-separated; they are
// resolved through filesResolve, which refuses anything that would
// escape the directory.

import (
	"archive/zip"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"unicode/utf8"
)

const maxImportBytes = 500 << 20 // per-file import cap (v0.32 spec)
const maxTextBytes = 2 << 20     // text viewer reads at most this much
const maxUnzipBytes = 2 << 30    // total uncompressed cap for user zips

func filesDir() string { return filepath.Join(dataDir(), "files") }
func thumbsDir() string {
	return filepath.Join(filesDir(), ".thumbs")
}

// ---------- classification ----------

var (
	imageExts = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".bmp": true}
	thumbExts = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true} // stdlib-decodable
	textExts  = map[string]bool{
		".txt": true, ".md": true, ".markdown": true, ".log": true, ".json": true,
		".ini": true, ".cfg": true, ".conf": true, ".yaml": true, ".yml": true,
		".toml": true, ".xml": true, ".srt": true, ".css": true, ".html": true,
		".htm": true, ".js": true, ".go": true, ".py": true, ".sh": true,
	}
	exeExts = map[string]bool{".exe": true, ".bat": true, ".cmd": true, ".msi": true}
)

// fileKindOf classifies one Data/files entry for the desktop and the
// open router: image | pdf | text | csv | docx | xlsx | pptx | zip |
// exe | dir | other.
func fileKindOf(name string, isDir bool) string {
	if isDir {
		return "dir"
	}
	switch ext := strings.ToLower(filepath.Ext(name)); {
	case imageExts[ext]:
		return "image"
	case ext == ".pdf":
		return "pdf"
	case textExts[ext]:
		return "text"
	case ext == ".csv" || ext == ".tsv":
		return "csv"
	case ext == ".docx":
		return "docx"
	case ext == ".xlsx":
		return "xlsx"
	case ext == ".pptx":
		return "pptx"
	case ext == ".zip":
		return "zip"
	case exeExts[ext]:
		return "exe"
	}
	return "other"
}

// ---------- path safety ----------

// filesResolve turns a frontend-supplied relative path (slash
// separated, e.g. "notes.txt" or "photos.zip.d/img 1.png") into an
// absolute path guaranteed to stay inside Data/files.
func filesResolve(rel string) (string, error) {
	rel = strings.TrimSpace(strings.ReplaceAll(rel, "\\", "/"))
	if rel == "" || rel == "." || strings.HasPrefix(rel, "/") || strings.Contains(rel, ":") {
		return "", fmt.Errorf("文件名不对")
	}
	clean := path.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, ".thumbs") {
		return "", fmt.Errorf("文件名不对")
	}
	abs := filepath.Join(filesDir(), filepath.FromSlash(clean))
	root, _ := filepath.Abs(filesDir())
	absA, _ := filepath.Abs(abs)
	if absA != root && !strings.HasPrefix(absA, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("文件名不对")
	}
	return absA, nil
}

// validEntryName checks a single top-level entry name (no separators,
// no leading dot -- dotfiles are the daemon's own bookkeeping, like
// .thumbs, and never show on the desktop).
func validEntryName(name string) error {
	if name == "" || name != filepath.Base(name) ||
		strings.ContainsAny(name, "/\\") || strings.HasPrefix(name, ".") ||
		name == "." || name == ".." || len(name) > 160 {
		return fmt.Errorf("名字不合法（不能带路径、不能以 . 开头）")
	}
	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return fmt.Errorf("名字不能以点或空格结尾")
	}
	return nil
}

// dedupeEntryName returns name, or "name (2).ext", "name (3).ext"...
// when something already sits there -- imports never overwrite.
func dedupeEntryName(dir, name string) string {
	if _, err := os.Stat(filepath.Join(dir, name)); os.IsNotExist(err) {
		return name
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 2; ; i++ {
		cand := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if _, err := os.Stat(filepath.Join(dir, cand)); os.IsNotExist(err) {
			return cand
		}
	}
}

// ---------- list ----------

func filesListHandler(w http.ResponseWriter, r *http.Request) {
	_ = os.MkdirAll(filesDir(), 0o755)
	entries, _ := os.ReadDir(filesDir())
	type fitem struct {
		Name string `json:"name"`
		Path string `json:"path"` // Data/files-relative, slash separated
		Size int64  `json:"size"`
		Ext  string `json:"ext"`
		Kind string `json:"kind"`
		Dir  bool   `json:"dir"`
		// Mtime is the entry's modification time (unix ms). v0.50:
		// the Files app's Finder face shows it as 导入时间 -- the
		// library stores no separate import timestamp, and an
		// imported file's mtime IS its arrival time (the copy
		// preserves nothing older; unzip results carry their own).
		Mtime int64 `json:"mtime"`
	}
	out := []fitem{}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue // .thumbs and import temp files are bookkeeping
		}
		it := fitem{Name: name, Path: name, Dir: e.IsDir()}
		if fi, err := e.Info(); err == nil {
			it.Mtime = fi.ModTime().UnixMilli()
		}
		if e.IsDir() {
			it.Kind = "dir"
			it.Size = dirSize(filepath.Join(filesDir(), name))
		} else {
			it.Kind = fileKindOf(name, false)
			it.Ext = strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
			if fi, err := e.Info(); err == nil {
				it.Size = fi.Size()
			}
		}
		out = append(out, it)
	}
	// Stable display order: folders first, then name order.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	writeJSON(w, map[string]any{"files": out, "dir": filesDir()})
}

// filesSubListHandler lists one extracted folder (first level only)
// for the folder-contents window: GET /api/files/sub?path=<dir>.
func filesSubListHandler(w http.ResponseWriter, r *http.Request) {
	abs, err := filesResolve(r.URL.Query().Get("path"))
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		writeJSON(w, map[string]any{"error": "打不开这个文件夹"})
		return
	}
	base := strings.TrimPrefix(r.URL.Query().Get("path"), "./")
	type sitem struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		Size  int64  `json:"size"`
		Kind  string `json:"kind"`
		Dir   bool   `json:"dir"`
		Mtime int64  `json:"mtime"` // unix ms, see filesListHandler
	}
	out := []sitem{}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		it := sitem{Name: e.Name(), Path: base + "/" + e.Name(), Dir: e.IsDir(), Kind: fileKindOf(e.Name(), e.IsDir())}
		if fi, ierr := e.Info(); ierr == nil {
			it.Mtime = fi.ModTime().UnixMilli()
			if !e.IsDir() {
				it.Size = fi.Size()
			}
		}
		out = append(out, it)
	}
	writeJSON(w, map[string]any{"files": out})
}

// ---------- import ----------

func filesImportHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, map[string]any{"error": "method not allowed"})
		return
	}
	_ = os.MkdirAll(filesDir(), 0o755)
	mr, err := r.MultipartReader()
	if err != nil {
		writeJSON(w, map[string]any{"error": "上传格式不对"})
		return
	}
	via := "picker"
	type imp struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
	}
	imported := []imp{}
	errs := []map[string]string{}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		if part.FormName() == "via" && part.FileName() == "" {
			b, _ := io.ReadAll(io.LimitReader(part, 64))
			if v := strings.TrimSpace(string(b)); v == "drag" || v == "paste" || v == "picker" {
				via = v
			}
			continue
		}
		rawName := part.FileName()
		if rawName == "" {
			continue
		}
		name := filepath.Base(strings.ReplaceAll(rawName, "\\", "/"))
		if verr := validEntryName(name); verr != nil {
			_, _ = io.Copy(io.Discard, part)
			errs = append(errs, map[string]string{"name": rawName, "error": verr.Error()})
			logf("files", "warn", "导入被拒绝：%s（%s）", rawName, verr.Error())
			continue
		}
		final := dedupeEntryName(filesDir(), name)
		tmp, terr := os.CreateTemp(filesDir(), ".import-*")
		if terr != nil {
			errs = append(errs, map[string]string{"name": rawName, "error": "写入失败"})
			continue
		}
		tmpName := tmp.Name()
		n, cerr := io.Copy(tmp, io.LimitReader(part, maxImportBytes+1))
		_ = tmp.Close()
		if cerr != nil || n > maxImportBytes {
			_ = os.Remove(tmpName)
			reason := "写入失败"
			if n >= maxImportBytes {
				// Already holding a full cap's worth when the copy
				// ended: the limit is the useful explanation (a
				// write error at exactly this point is the disk
				// refusing the over-limit file, e.g. a tiny tmpfs).
				reason = "超过 500 MB 上限"
			}
			errs = append(errs, map[string]string{"name": rawName, "error": reason})
			logf("files", "warn", "导入失败：%s（%s）", rawName, reason)
			continue
		}
		dest := filepath.Join(filesDir(), final)
		if rerr := os.Rename(tmpName, dest); rerr != nil {
			_ = os.Remove(tmpName)
			errs = append(errs, map[string]string{"name": rawName, "error": "落盘失败"})
			logf("files", "error", "导入落盘失败：%s：%v", rawName, rerr)
			continue
		}
		logf("files", "info", "导入文件：%s（%d 字节，通道 %s）", final, n, via)
		imported = append(imported, imp{Name: final, Size: n})
	}
	if len(imported) > 0 {
		// v0.47 hook (notify): one event per import request. The
		// dedupe key carries the first file's name, so a multi-file
		// drag reports once as a batch while separate single-file
		// drags each report themselves.
		title := "文件导入完成"
		body := imported[0].Name
		if len(imported) > 1 {
			body = fmt.Sprintf("%s 等 %d 个文件", imported[0].Name, len(imported))
		}
		postNotify(notifySrcFiles, "import:"+imported[0].Name, title, body, "")
	}
	writeJSON(w, map[string]any{"imported": imported, "errors": errs})
}

// ---------- rename / delete ----------

func filesRenameHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Old string `json:"old"`
		New string `json:"new"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeJSON(w, map[string]any{"error": "请求内容不对"})
		return
	}
	if err := validEntryName(body.Old); err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	body.New = strings.TrimSpace(body.New)
	if err := validEntryName(body.New); err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	src := filepath.Join(filesDir(), body.Old)
	dst := filepath.Join(filesDir(), body.New)
	if _, err := os.Stat(src); err != nil {
		writeJSON(w, map[string]any{"error": "原文件不存在"})
		return
	}
	if _, err := os.Stat(dst); err == nil {
		writeJSON(w, map[string]any{"error": "已经有同名文件了，换个名字"})
		return
	}
	if err := os.Rename(src, dst); err != nil {
		logf("files", "error", "重命名失败：%s → %s：%v", body.Old, body.New, err)
		writeJSON(w, map[string]any{"error": "重命名失败：" + err.Error()})
		return
	}
	dropThumbCache(body.Old)
	logf("files", "info", "文件改名：%s → %s", body.Old, body.New)
	writeJSON(w, map[string]any{"ok": true, "name": body.New})
}

func filesDeleteHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeJSON(w, map[string]any{"error": "请求内容不对"})
		return
	}
	if err := validEntryName(body.Name); err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	p := filepath.Join(filesDir(), body.Name)
	fi, err := os.Stat(p)
	if err != nil {
		writeJSON(w, map[string]any{"error": "文件不存在"})
		return
	}
	var rerr error
	if fi.IsDir() {
		rerr = os.RemoveAll(p)
	} else {
		rerr = os.Remove(p)
	}
	if rerr != nil {
		logf("files", "error", "删除失败：%s：%v", body.Name, rerr)
		writeJSON(w, map[string]any{"error": "删除失败：" + rerr.Error()})
		return
	}
	dropThumbCache(body.Name)
	logf("files", "info", "删除导入的文件副本：%s（电脑上的原文件不受影响）", body.Name)
	writeJSON(w, map[string]any{"ok": true})
}

func dropThumbCache(name string) {
	entries, _ := os.ReadDir(thumbsDir())
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), name+"-") {
			_ = os.Remove(filepath.Join(thumbsDir(), e.Name()))
		}
	}
}

// ---------- raw serve / export ----------

func filesRawHandler(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	abs, err := filesResolve(rel)
	if err != nil {
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}
	fi, err := os.Stat(abs)
	if err != nil || fi.IsDir() {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(abs)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	ct := mime.TypeByExtension(strings.ToLower(filepath.Ext(abs)))
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	if r.URL.Query().Get("dl") == "1" {
		w.Header().Set("Content-Disposition",
			"attachment; filename*=UTF-8''"+url.PathEscape(filepath.Base(abs)))
		logf("files", "info", "导出文件到电脑：%s（%d 字节）", filepath.Base(abs), fi.Size())
	}
	http.ServeContent(w, r, filepath.Base(abs), fi.ModTime(), f)
}

// ---------- thumbnails ----------

// GET /api/files/thumb?name=<top-level image name>: a ~128px JPEG
// rendered by the stdlib decoders (jpg/png/gif). webp/bmp cannot be
// decoded by the stdlib and answer 404 -- the desktop falls back to
// the type icon. Results are cached in Data/files/.thumbs keyed by
// name+size+mtime, so a replaced file re-renders.
func filesThumbHandler(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if validEntryName(name) != nil || !thumbExts[strings.ToLower(filepath.Ext(name))] {
		http.NotFound(w, r)
		return
	}
	src := filepath.Join(filesDir(), name)
	fi, err := os.Stat(src)
	if err != nil || fi.IsDir() || fi.Size() > 64<<20 {
		http.NotFound(w, r)
		return
	}
	_ = os.MkdirAll(thumbsDir(), 0o755)
	cache := filepath.Join(thumbsDir(), fmt.Sprintf("%s-%d-%d.jpg", name, fi.Size(), fi.ModTime().Unix()))
	if b, err := os.ReadFile(cache); err == nil {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "max-age=3600")
		_, _ = w.Write(b)
		return
	}
	f, err := os.Open(src)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	img, _, derr := image.Decode(f)
	_ = f.Close()
	if derr != nil {
		http.NotFound(w, r)
		return
	}
	thumb := scaleToFit(img, 128, 128)
	tmp := cache + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	encErr := jpeg.Encode(out, thumb, &jpeg.Options{Quality: 82})
	_ = out.Close()
	if encErr != nil {
		_ = os.Remove(tmp)
		http.NotFound(w, r)
		return
	}
	_ = os.Rename(tmp, cache)
	b, err := os.ReadFile(cache)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "max-age=3600")
	_, _ = w.Write(b)
}

// scaleToFit shrinks src to fit maxW x maxH (never upscales) with
// bilinear sampling -- the stdlib has no scaler, and thumbnails do
// not need a fancy filter.
func scaleToFit(src image.Image, maxW, maxH int) image.Image {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw <= maxW && sh <= maxH {
		return src
	}
	dw, dh := maxW, sh*maxW/sw
	if dh > maxH {
		dh, dw = maxH, sw*maxH/sh
	}
	if dw < 1 {
		dw = 1
	}
	if dh < 1 {
		dh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		fy := (float64(y)+0.5)*float64(sh)/float64(dh) - 0.5
		y0 := int(fy)
		if y0 < 0 {
			y0, fy = 0, 0
		}
		y1 := y0 + 1
		if y1 >= sh {
			y1 = sh - 1
		}
		wy := fy - float64(int(fy))
		if y0 == y1 {
			wy = 0
		}
		for x := 0; x < dw; x++ {
			fx := (float64(x)+0.5)*float64(sw)/float64(dw) - 0.5
			x0 := int(fx)
			if x0 < 0 {
				x0, fx = 0, 0
			}
			x1 := x0 + 1
			if x1 >= sw {
				x1 = sw - 1
			}
			wx := fx - float64(int(fx))
			if x0 == x1 {
				wx = 0
			}
			c00 := src.At(b.Min.X+x0, b.Min.Y+y0)
			c10 := src.At(b.Min.X+x1, b.Min.Y+y0)
			c01 := src.At(b.Min.X+x0, b.Min.Y+y1)
			c11 := src.At(b.Min.X+x1, b.Min.Y+y1)
			dst.Set(x, y, bilerpColor(c00, c10, c01, c11, wx, wy))
		}
	}
	return dst
}

func bilerpColor(c00, c10, c01, c11 color.Color, wx, wy float64) color.Color {
	lerp := func(a, b uint32, t float64) float64 { return float64(a) + (float64(b)-float64(a))*t }
	r00, g00, b00, a00 := c00.RGBA()
	r10, g10, b10, a10 := c10.RGBA()
	r01, g01, b01, a01 := c01.RGBA()
	r11, g11, b11, a11 := c11.RGBA()
	mix := func(v00, v10, v01, v11 uint32) uint8 {
		top := lerp(v00, v10, wx)
		bot := lerp(v01, v11, wx)
		return uint8((top + (bot-top)*wy) / 257)
	}
	return color.RGBA{mix(r00, r10, r01, r11), mix(g00, g10, g01, g11), mix(b00, b10, b01, b11), mix(a00, a10, a01, a11)}
}

// ---------- text / docx preview ----------

func filesTextHandler(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	abs, err := filesResolve(rel)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	name := filepath.Base(abs)
	if strings.ToLower(filepath.Ext(name)) == ".docx" {
		text, terr := docxText(abs)
		if terr != nil {
			logf("files", "warn", "DOCX 预览失败：%s：%v", name, terr)
			writeJSON(w, map[string]any{"error": "这个文档读不出文字（可能已损坏或不是标准 docx）"})
			return
		}
		trunc := false
		if len(text) > maxTextBytes {
			text, trunc = cutUTF8(text, maxTextBytes), true
		}
		writeJSON(w, map[string]any{"name": name, "kind": "docx", "text": text, "truncated": trunc})
		return
	}
	f, err := os.Open(abs)
	if err != nil {
		writeJSON(w, map[string]any{"error": "文件打不开"})
		return
	}
	defer f.Close()
	b, _ := io.ReadAll(io.LimitReader(f, maxTextBytes+1))
	trunc := len(b) > maxTextBytes
	if trunc {
		b = b[:maxTextBytes]
	}
	text := string(b)
	if !utf8.ValidString(text) {
		// Best effort: invalid sequences render as U+FFFD. A GBK
		// fallback would need golang.org/x/text, which this module
		// deliberately does not pull in (see the v0.32 notes).
		text = strings.ToValidUTF8(text, "�")
	}
	writeJSON(w, map[string]any{"name": name, "kind": "text", "text": text, "truncated": trunc})
}

func cutUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

var docxTagRe = regexp.MustCompile(`<[^>]*>`)

// docxText pulls the plain text out of a .docx using only the
// stdlib: unzip word/document.xml, turn paragraph ends into
// newlines, strip every remaining tag, unescape entities.
func docxText(abs string) (string, error) {
	zr, err := zip.OpenReader(abs)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	for _, zf := range zr.File {
		if zf.Name != "word/document.xml" {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return "", err
		}
		b, rerr := io.ReadAll(io.LimitReader(rc, 16<<20))
		_ = rc.Close()
		if rerr != nil {
			return "", rerr
		}
		s := string(b)
		s = strings.ReplaceAll(s, "</w:p>", "\n")
		s = strings.ReplaceAll(s, "<w:tab/>", "\t")
		s = strings.ReplaceAll(s, "<w:br/>", "\n")
		s = docxTagRe.ReplaceAllString(s, "")
		s = html.UnescapeString(s)
		for strings.Contains(s, "\n\n\n") {
			s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
		}
		return strings.TrimSpace(s), nil
	}
	return "", fmt.Errorf("word/document.xml 不在包里")
}

// ---------- csv ----------

func filesCSVHandler(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	abs, err := filesResolve(rel)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	f, err := os.Open(abs)
	if err != nil {
		writeJSON(w, map[string]any{"error": "文件打不开"})
		return
	}
	defer f.Close()
	rd := csv.NewReader(f)
	rd.FieldsPerRecord = -1
	rd.LazyQuotes = true
	rd.TrimLeadingSpace = true
	if strings.ToLower(filepath.Ext(abs)) == ".tsv" {
		rd.Comma = '\t'
	}
	const maxRows, maxCols = 200, 50
	rows := [][]string{}
	truncR, truncC := false, false
	for len(rows) <= maxRows {
		rec, rerr := rd.Read()
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			if len(rows) == 0 {
				writeJSON(w, map[string]any{"error": "这个 CSV 解析不了：" + rerr.Error()})
				return
			}
			break
		}
		if len(rec) > maxCols {
			rec, truncC = rec[:maxCols], true
		}
		rows = append(rows, rec)
	}
	if len(rows) > maxRows {
		rows, truncR = rows[:maxRows], true
	}
	writeJSON(w, map[string]any{
		"name": filepath.Base(abs), "rows": rows,
		"truncatedRows": truncR, "truncatedCols": truncC,
	})
}

// ---------- open: pdf ----------

// filesOpenPDFHandler routes a PDF entirely inside the system: the
// store's SumatraPDF when it is installed (launched with the file
// and claimed by the window manager, like any store app), otherwise
// the isolated browser on the file:// URL.
func filesOpenPDFHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeJSON(w, map[string]any{"error": "请求内容不对"})
		return
	}
	abs, err := filesResolve(body.Path)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	if strings.ToLower(filepath.Ext(abs)) != ".pdf" {
		writeJSON(w, map[string]any{"error": "这不是 PDF 文件"})
		return
	}
	if _, err := os.Stat(abs); err != nil {
		writeJSON(w, map[string]any{"error": "文件不存在"})
		return
	}
	if runtime.GOOS != "windows" {
		writeJSON(w, map[string]any{"error": "当前不是 Windows 环境（这是测试环境），到 Windows 上会正常打开"})
		return
	}
	name := filepath.Base(abs)
	if entry, ok := findApp("sumatra"); ok && appInstalled(entry, loadAppsRegistry()) {
		exePath := filepath.Join(appDir(entry.ID), filepath.FromSlash(entry.Exe))
		before := platformSnapshotWindows(exePath)
		cmd := exec.Command(exePath, abs)
		cmd.Dir = appDir(entry.ID)
		hideConsole(cmd)
		if err := cmd.Start(); err != nil {
			logf("files", "error", "用 SumatraPDF 打开 %s 失败：%v", name, err)
			writeJSON(w, map[string]any{"error": "SumatraPDF 启动失败：" + err.Error()})
			return
		}
		noteSpawnedProcess(uint32(cmd.Process.Pid), "SumatraPDF")
		logf("files", "info", "用 SumatraPDF 打开 PDF：%s", name)
		trackAppLaunch("SumatraPDF", exePath, before)
		writeJSON(w, map[string]any{"ok": true, "via": "sumatra"})
		return
	}
	u := url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(abs)}
	if err := launchIsolatedBrowser(false, u.String(), false); err != nil {
		logf("files", "error", "用隔离浏览器打开 PDF %s 失败：%v", name, err)
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	logf("files", "info", "用隔离浏览器打开 PDF：%s", name)
	writeJSON(w, map[string]any{"ok": true, "via": "browser"})
}

// ---------- open: executables (frontend confirms first) ----------

func filesRunHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeJSON(w, map[string]any{"error": "请求内容不对"})
		return
	}
	abs, err := filesResolve(body.Path)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	ext := strings.ToLower(filepath.Ext(abs))
	if !exeExts[ext] {
		writeJSON(w, map[string]any{"error": "这不是可执行文件"})
		return
	}
	if _, err := os.Stat(abs); err != nil {
		writeJSON(w, map[string]any{"error": "文件不存在"})
		return
	}
	if runtime.GOOS != "windows" {
		writeJSON(w, map[string]any{"error": "当前不是 Windows 环境（这是测试环境），到 Windows 上会运行"})
		return
	}
	name := filepath.Base(abs)
	dir := filepath.Dir(abs)
	switch ext {
	case ".exe":
		before := platformSnapshotWindows(abs)
		cmd := exec.Command(abs)
		cmd.Dir = dir
		hideConsole(cmd)
		if err := cmd.Start(); err != nil {
			logf("files", "error", "运行 %s 失败：%v", name, err)
			writeJSON(w, map[string]any{"error": "启动失败：" + err.Error()})
			return
		}
		noteSpawnedProcess(uint32(cmd.Process.Pid), name)
		logf("files", "info", "用户确认后运行可执行文件：%s", name)
		trackAppLaunch(name, abs, before)
	case ".bat", ".cmd":
		cmd := exec.Command("cmd", "/c", abs)
		cmd.Dir = dir
		if err := cmd.Start(); err != nil {
			logf("files", "error", "运行 %s 失败：%v", name, err)
			writeJSON(w, map[string]any{"error": "启动失败：" + err.Error()})
			return
		}
		logf("files", "info", "用户确认后运行脚本：%s（经 cmd 承载，窗口不登记台前调度）", name)
	case ".msi":
		cmd := exec.Command("msiexec", "/i", abs)
		if err := cmd.Start(); err != nil {
			logf("files", "error", "运行 %s 失败：%v", name, err)
			writeJSON(w, map[string]any{"error": "启动失败：" + err.Error()})
			return
		}
		logf("files", "info", "用户确认后运行安装包：%s（经 msiexec 承载，窗口不登记台前调度）", name)
	}
	writeJSON(w, map[string]any{"ok": true})
}

// ---------- unzip ----------

func filesUnzipHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeJSON(w, map[string]any{"error": "请求内容不对"})
		return
	}
	abs, err := filesResolve(body.Path)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	if strings.ToLower(filepath.Ext(abs)) != ".zip" {
		writeJSON(w, map[string]any{"error": "这不是 zip 文件"})
		return
	}
	name := filepath.Base(abs)
	targetName := dedupeEntryName(filesDir(), strings.TrimSuffix(name, filepath.Ext(name)))
	target := filepath.Join(filesDir(), targetName)
	count, err := filesExtractZip(abs, target)
	if err != nil {
		_ = os.RemoveAll(target)
		logf("files", "warn", "解压 %s 失败：%v", name, err)
		writeJSON(w, map[string]any{"error": "解压失败：" + err.Error()})
		return
	}
	logf("files", "info", "解压 %s → 文件夹 %s（%d 个文件）", name, targetName, count)
	writeJSON(w, map[string]any{"ok": true, "dir": targetName, "count": count})
}

// filesExtractZip is the store extractor's slip-guarded shape, made
// stricter for user-supplied archives: a single escaping entry (or
// an over-large total) refuses the WHOLE archive rather than
// silently skipping entries.
func filesExtractZip(zipPath, dir string) (int, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return 0, err
	}
	defer zr.Close()
	var total uint64
	for _, zf := range zr.File {
		clean := filepath.Clean(zf.Name)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) || strings.Contains(zf.Name, ":") {
			return 0, fmt.Errorf("包里有不安全的路径：%s，已拒绝整个包", zf.Name)
		}
		total += zf.UncompressedSize64
		if total > maxUnzipBytes {
			return 0, fmt.Errorf("解压后总大小超过 2 GB 上限，已拒绝")
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	count := 0
	for _, zf := range zr.File {
		dst := filepath.Join(dir, filepath.Clean(zf.Name))
		if zf.FileInfo().IsDir() {
			_ = os.MkdirAll(dst, 0o755)
			continue
		}
		_ = os.MkdirAll(filepath.Dir(dst), 0o755)
		rc, err := zf.Open()
		if err != nil {
			return count, err
		}
		out, err := os.Create(dst)
		if err != nil {
			_ = rc.Close()
			return count, err
		}
		_, err = io.Copy(out, rc)
		_ = out.Close()
		_ = rc.Close()
		if err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

// decodeJSONBody is a tiny shared helper for the JSON POST bodies in
// this file (the rest of the daemon decodes inline; new code here
// keeps it in one place).
func decodeJSONBody(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v)
}
