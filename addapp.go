package main

// addapp.go -- v0.69 block 2: MJ-native "add app" flows.
//
// The problem (user, 2026-11-11): installing a web app from
// inside the isolated Chrome registers it in Chrome's own app
// system (chrome://apps) and as host-OS shortcuts -- it never
// lands on the MJ desktop, because the desktop renders only
// the WEBAPPS ledger (Data/apps.txt). These are the native
// flows that feed that ledger:
//
//   Entry A -- pin an open window: the Stage strip's
//   right-click menu on an app-mode window posts the window
//   handle to /api/webapps/pin; the daemon already knows the
//   window's URL and title, so registration needs no browser
//   cooperation at all (no extension dependency).
//   Entry B -- paste a URL: the desktop menu / Launchpad
//   "添加应用…" dialog posts the URL to /api/webapps/probe,
//   which fetches the page THROUGH THE NODE (dlGet) and
//   prefills the site's real title; saving goes through the
//   existing /api/apps/save ledger path.
//
// Icons: at add time the daemon fetches the site's icon
// through the node (apple-touch-icon -> icon link -> /favicon
// .ico fallback chain) and stores it under Data/webicons,
// named by the SHA-1 of the entry URL. The desktop renders
// the local file when an entry has one and keeps the previous
// behaviour (favicon service / letter tile) otherwise, so
// bundled entries are untouched. A failed fetch is not an
// error: the entry saves fine and the letter tile stands in.
//
// Duplicate URLs are detected and reported by both entries
// (the ledger's identity IS the URL -- re-adding one would
// silently rename it, which the dialog must surface instead).

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"hash/fnv"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const webAppUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36"

// ---------- icon storage ----------

func webIconDir() string { return filepath.Join(dataDir(), "webicons") }

// webIconName is the stored icon's file name for an entry URL,
// before its extension is known (the sniffed extension is
// appended at store time). A plain FNV-1a digest: this is a
// cache key, not a security boundary.
func webIconName(rawURL string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(rawURL))
	return hex.EncodeToString(h.Sum(nil))
}

var webIconFileRe = regexp.MustCompile(`^[a-f0-9]{16}\.(png|ico|jpg|gif|webp|svg)$`)

// sniffIconExt identifies an icon payload by magic bytes (or,
// for SVG, by a leading tag). "" = not an icon we will store.
func sniffIconExt(b []byte) string {
	switch {
	case len(b) >= 8 && b[0] == 0x89 && b[1] == 'P' && b[2] == 'N' && b[3] == 'G':
		return ".png"
	case len(b) >= 4 && b[0] == 0 && b[1] == 0 && b[2] == 1 && b[3] == 0:
		return ".ico"
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return ".jpg"
	case len(b) >= 6 && string(b[:6]) == "GIF89a" || len(b) >= 6 && string(b[:6]) == "GIF87a":
		return ".gif"
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return ".webp"
	default:
		s := strings.ToLower(strings.TrimSpace(string(b[:min(len(b), 512)])))
		if strings.HasPrefix(s, "<svg") || strings.HasPrefix(s, "<?xml") && strings.Contains(s, "<svg") {
			return ".svg"
		}
		return ""
	}
}

// ---------- page metadata ----------

var (
	reTitleTag  = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	reOGTitle   = regexp.MustCompile(`(?is)<meta[^>]+(?:property|name)\s*=\s*["']og:title["'][^>]*content\s*=\s*["']([^"']*)["']|<meta[^>]+content\s*=\s*["']([^"']*)["'][^>]*(?:property|name)\s*=\s*["']og:title["']`)
	reIconLink  = regexp.MustCompile(`(?is)<link[^>]+rel\s*=\s*["']([^"']+)["'][^>]*?href\s*=\s*["']([^"']+)["']|<link[^>]+href\s*=\s*["']([^"']+)["'][^>]*?rel\s*=\s*["']([^"']+)["']`)
	reSpaceRuns = regexp.MustCompile(`\s+`)
)

// parsePageMeta extracts a page's display title and its icon
// link hrefs (best first: apple-touch-icon, then icon links,
// in document order within each class). Pure -- tests pin it.
func parsePageMeta(page []byte) (title string, iconHrefs []string) {
	s := string(page)
	if m := reTitleTag.FindSubmatch(page); m != nil {
		title = cleanPageTitle(string(m[1]))
	}
	if title == "" {
		if m := reOGTitle.FindStringSubmatch(s); m != nil {
			for _, g := range m[1:] {
				if g != "" {
					title = cleanPageTitle(g)
					break
				}
			}
		}
	}
	var apple, other []string
	for _, m := range reIconLink.FindAllStringSubmatch(s, -1) {
		rel, href := "", ""
		if m[1] != "" {
			rel, href = m[1], m[2]
		} else {
			href, rel = m[3], m[4]
		}
		rel = strings.ToLower(rel)
		href = strings.TrimSpace(href)
		if href == "" || strings.HasPrefix(href, "data:") {
			continue
		}
		switch {
		case strings.Contains(rel, "apple-touch-icon"):
			apple = append(apple, href)
		case strings.Contains(rel, "icon"):
			other = append(other, href)
		}
	}
	return title, append(apple, other...)
}

func cleanPageTitle(t string) string {
	t = html.UnescapeString(t)
	return strings.TrimSpace(reSpaceRuns.ReplaceAllString(t, " "))
}

// normalizeWebURL applies the ledger's URL rule (bare hosts
// gain https://) and validates the result.
func normalizeWebURL(raw string) (string, bool) {
	u := strings.TrimSpace(raw)
	if u != "" && !strings.Contains(u, "://") {
		u = "https://" + u
	}
	pu, err := url.Parse(u)
	if err != nil || pu.Host == "" || (pu.Scheme != "http" && pu.Scheme != "https") {
		return "", false
	}
	return u, true
}

// webAppExists finds a ledger entry by URL, tolerating a
// trailing-slash difference, and returns its name.
func webAppExists(list []webApp, u string) (string, bool) {
	trim := func(s string) string { return strings.TrimSuffix(s, "/") }
	for _, a := range list {
		if a.URL == u || trim(a.URL) == trim(u) {
			return a.Name, true
		}
	}
	return "", false
}

// ---------- fetching (through the node) ----------

func fetchViaNode(ctx context.Context, rawURL string, capBytes int64) ([]byte, error) {
	resp, err := dlGet(ctx, rawURL, webAppUA)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, capBytes))
}

// ensureWebAppIcon fetches and stores the entry's icon if it
// does not have a live one yet, updating a.Icon in place.
// Best effort throughout: any failure leaves the entry
// icon-less (the letter tile stands in) and returns false.
func ensureWebAppIcon(ctx context.Context, a *webApp) bool {
	if a.Icon != "" && webIconFileRe.MatchString(a.Icon) {
		if _, err := os.Stat(filepath.Join(webIconDir(), a.Icon)); err == nil {
			return true
		}
	}
	pu, err := url.Parse(a.URL)
	if err != nil || pu.Host == "" {
		return false
	}
	origin := pu.Scheme + "://" + pu.Host
	var candidates []string
	if page, err := fetchViaNode(ctx, a.URL, 512*1024); err == nil {
		_, hrefs := parsePageMeta(page)
		for _, h := range hrefs {
			if ru, rerr := pu.Parse(h); rerr == nil {
				candidates = append(candidates, ru.String())
			}
		}
	}
	candidates = append(candidates,
		origin+"/apple-touch-icon.png",
		origin+"/favicon.ico",
	)
	seen := map[string]bool{}
	for _, c := range candidates {
		if seen[c] {
			continue
		}
		seen[c] = true
		b, err := fetchViaNode(ctx, c, 256*1024)
		if err != nil || len(b) == 0 {
			continue
		}
		ext := sniffIconExt(b)
		if ext == "" {
			continue
		}
		name := webIconName(a.URL) + ext
		if err := os.MkdirAll(webIconDir(), 0o755); err != nil {
			return false
		}
		if err := os.WriteFile(filepath.Join(webIconDir(), name), b, 0o644); err != nil {
			return false
		}
		a.Icon = name
		return true
	}
	return false
}

// ---------- handlers ----------

// webAppProbeHandler serves POST /api/webapps/probe {url}:
// the 添加应用 dialog's prefill. Duplicate -> exists with the
// current name and NO fetch. Otherwise the page is fetched
// through the node for its title; a fetch failure still
// returns a usable prefill (the host as the name) so the
// dialog never dead-ends on a slow site.
func webAppProbeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, errCodeMethod, "只接受 POST")
		return
	}
	var body struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apiErrBadRequest(w, errCodeBadJSON, "请求格式不对")
		return
	}
	u, ok := normalizeWebURL(body.URL)
	if !ok {
		apiErrBadRequest(w, errCodeBadRequest, "网址不对，请输入完整网址，如 https://example.com")
		return
	}
	if name, exists := webAppExists(loadWebApps(), u); exists {
		writeJSON(w, webAppProbeResponse{OK: true, Exists: true, Name: name, URL: u})
		return
	}
	name := ""
	if pu, err := url.Parse(u); err == nil {
		name = pu.Host
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	if page, err := fetchViaNode(ctx, u, 512*1024); err == nil {
		if title, _ := parsePageMeta(page); title != "" {
			name = title
		}
	} else {
		logf("browser", "warn", "添加应用：抓取页面标题失败（%s）：%v", u, err)
	}
	if rr := []rune(name); len(rr) > 24 {
		name = string(rr[:24])
	}
	writeJSON(w, webAppProbeResponse{OK: true, Name: name, URL: u})
}

// pinNameFromTitle turns a window title into an entry name:
// browser window titles for app windows are the page title,
// sometimes with a browser suffix. Falls back to the site's
// host. Pure -- tests pin it.
func pinNameFromTitle(title, rawURL string) string {
	name := strings.TrimSpace(title)
	for _, suf := range []string{" - Google Chrome", " - Microsoft Edge", " - Chromium", " — Google Chrome", " — Microsoft Edge"} {
		name = strings.TrimSuffix(name, suf)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		if pu, err := url.Parse(rawURL); err == nil && pu.Host != "" {
			name = pu.Host
		} else {
			name = "网站应用"
		}
	}
	if rr := []rune(name); len(rr) > 24 {
		name = string(rr[:24])
	}
	return name
}

// webAppPinHandler serves POST /api/webapps/pin {hwnd}:
// register an open app-mode window as a desktop app. The
// daemon already knows the window's URL and title from the
// window registry, so nothing is asked of the browser.
func webAppPinHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, errCodeMethod, "只接受 POST")
		return
	}
	var body struct {
		HWND uint64 `json:"hwnd"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.HWND == 0 {
		apiErrBadRequest(w, errCodeBadJSON, "请求格式不对")
		return
	}
	var win *extWindow
	extMu.Lock()
	if e, ok := extWins[body.HWND]; ok {
		cp := *e
		win = &cp
	}
	extMu.Unlock()
	if win == nil {
		writeAPIError(w, http.StatusNotFound, errCodeNotFound, "这个窗口已经关掉了")
		return
	}
	u, ok := normalizeWebURL(win.URL)
	if !ok {
		apiErrBadRequest(w, errCodeInvalid, "这个窗口没有可登记的网址")
		return
	}
	list := loadWebApps()
	if name, exists := webAppExists(list, u); exists {
		writeJSON(w, map[string]any{"error": "这个网页已经在桌面上了（" + name + "）"})
		return
	}
	entry := webApp{Name: pinNameFromTitle(win.Title, u), URL: u}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	iconOK := ensureWebAppIcon(ctx, &entry)
	cancel()
	if isBundledURL(u) {
		// Re-adding a deleted bundled entry clears its tombstone.
		tomb := loadTombstones()
		delete(tomb, u)
		saveTombstones(tomb)
	}
	list = append(list, entry)
	saveWebApps(list)
	logf("browser", "info", "已固定为桌面应用：%s（%s，图标%s）", entry.Name, u, map[bool]string{true: "已抓取", false: "未抓到，用字母图标"}[iconOK])
	writeJSON(w, webAppSaveResponse{OK: true, Apps: list})
}

// webAppIconHandler serves GET /api/webapps/icon?f=<name>:
// the locally stored site icons. The file name is validated
// against the exact stored pattern -- nothing outside
// Data/webicons can be reached through it.
func webAppIconHandler(w http.ResponseWriter, r *http.Request) {
	f := r.URL.Query().Get("f")
	if !webIconFileRe.MatchString(f) {
		http.NotFound(w, r)
		return
	}
	b, err := os.ReadFile(filepath.Join(webIconDir(), f))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ct := "image/png"
	switch filepath.Ext(f) {
	case ".ico":
		ct = "image/x-icon"
	case ".jpg":
		ct = "image/jpeg"
	case ".gif":
		ct = "image/gif"
	case ".webp":
		ct = "image/webp"
	case ".svg":
		ct = "image/svg+xml"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "max-age=86400")
	_, _ = w.Write(b)
}
