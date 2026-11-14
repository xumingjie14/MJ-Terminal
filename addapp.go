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
// through the node and stores it under Data/webicons, named
// by the FNV digest of the entry URL. Since v0.71 test4 the
// fetch chain is the browser-start-page one (the user's icon
// benchmark): the PWA manifest's icons first, then the page's
// own icon links, then the conventional locations -- every
// candidate scored by its DECLARED size, every download
// verified by its REAL pixel size, the largest verified image
// wins (v0.69 took the first download that succeeded, which
// let a 32px favicon beat a 512px manifest icon). The page's
// theme color is captured alongside and persisted on the
// entry, so the letter tile (the last resort now -- the old
// Google favicon-service path is retired) wears the site's
// own colour. Entries added before test4 are backfilled once
// after startup (backfillWebAppIcons). A failed fetch is not
// an error: the entry saves fine and the letter tile stands
// in.
//
// Duplicate URLs are detected and reported by both entries
// (the ledger's identity IS the URL -- re-adding one would
// silently rename it, which the dialog must surface instead).

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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
	reLinkTag   = regexp.MustCompile(`(?is)<link\b[^>]*>`)
	reMetaTag   = regexp.MustCompile(`(?is)<meta\b[^>]*>`)
	reSpaceRuns = regexp.MustCompile(`\s+`)
	reThemeHex  = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)
)

// tagAttr pulls one attribute out of a single HTML tag, in
// either quote style and either attribute order -- the loose
// parsing the icon hunt needs (pages in the wild write their
// link tags every which way).
func tagAttr(tag, name string) string {
	re := regexp.MustCompile(`(?is)(?:^|[\s/])` + name + `\s*=\s*["']([^"']*)["']`)
	m := re.FindStringSubmatch(tag)
	if m == nil {
		return ""
	}
	return m[1]
}

// iconLink is one icon <link> from a page head: its href, its
// class (apple-touch-icon vs the rest) and the largest size it
// DECLARES (0 = undeclared, iconSizeAny = a scalable vector).
type iconLink struct {
	Href     string
	Apple    bool
	Declared int
}

// pageMeta is everything the add/backfill flows read from a
// page head (v0.71 test4): the title, the theme color, the PWA
// manifest href and the icon links.
type pageMeta struct {
	Title      string
	ThemeColor string // validated + normalized #rrggbb; "" when absent or unusable
	Manifest   string // href of <link rel="manifest">; "" when absent
	Icons      []iconLink
}

// iconSizeAny stands in for a vector image's "size": an SVG is
// crisp at every size, so it outranks every raster candidate.
const iconSizeAny = 1 << 20

// declaredIconSize reads a link's sizes attribute ("48x48
// 192x192", or "any") into its largest declared dimension;
// def answers for an absent or unreadable attribute (an
// apple-touch-icon declares nothing but is 180px by platform
// convention, so its callers pass 180).
func declaredIconSize(sizes string, def int) int {
	sizes = strings.ToLower(strings.TrimSpace(sizes))
	if sizes == "" {
		return def
	}
	if sizes == "any" {
		return iconSizeAny
	}
	best := 0
	for _, part := range strings.Fields(sizes) {
		var w, h int
		if _, err := fmt.Sscanf(part, "%dx%d", &w, &h); err == nil {
			best = max(best, w, h)
		}
	}
	if best == 0 {
		return def
	}
	return best
}

// validThemeColor validates a theme-color value and normalizes
// it to lowercase #rrggbb. Only hex values are accepted -- the
// tile derivation on the view side consumes exactly that shape,
// and a named/rgb() colour is rarer than it is worth here.
func validThemeColor(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if !reThemeHex.MatchString(s) {
		return "", false
	}
	s = strings.ToLower(s)
	if len(s) == 4 { // #rgb -> #rrggbb
		s = "#" + string([]byte{s[1], s[1], s[2], s[2], s[3], s[3]})
	}
	return s, true
}

// parsePageMetaFull extracts the full pageMeta from a page.
// Pure -- tests pin it.
func parsePageMetaFull(page []byte) pageMeta {
	s := string(page)
	var m pageMeta
	if mm := reTitleTag.FindSubmatch(page); mm != nil {
		m.Title = cleanPageTitle(string(mm[1]))
	}
	if m.Title == "" {
		if mm := reOGTitle.FindStringSubmatch(s); mm != nil {
			for _, g := range mm[1:] {
				if g != "" {
					m.Title = cleanPageTitle(g)
					break
				}
			}
		}
	}
	var apple, other []iconLink
	for _, tag := range reLinkTag.FindAllString(s, -1) {
		rel := strings.ToLower(tagAttr(tag, "rel"))
		href := strings.TrimSpace(tagAttr(tag, "href"))
		if href == "" || strings.HasPrefix(href, "data:") {
			continue
		}
		switch {
		case strings.Contains(rel, "manifest"):
			if m.Manifest == "" {
				m.Manifest = href
			}
		case strings.Contains(rel, "apple-touch-icon"):
			apple = append(apple, iconLink{Href: href, Apple: true, Declared: declaredIconSize(tagAttr(tag, "sizes"), 180)})
		case strings.Contains(rel, "icon"):
			other = append(other, iconLink{Href: href, Declared: declaredIconSize(tagAttr(tag, "sizes"), 0)})
		}
	}
	m.Icons = append(apple, other...)
	for _, tag := range reMetaTag.FindAllString(s, -1) {
		if strings.EqualFold(tagAttr(tag, "name"), "theme-color") {
			if c, ok := validThemeColor(tagAttr(tag, "content")); ok {
				m.ThemeColor = c
				break
			}
		}
	}
	return m
}

// parsePageMeta is the v0.69 shape kept for the probe flow and
// its tests: a page's display title and its icon link hrefs
// (best first: apple-touch-icon, then icon links, in document
// order within each class).
func parsePageMeta(page []byte) (title string, iconHrefs []string) {
	m := parsePageMetaFull(page)
	hrefs := make([]string, 0, len(m.Icons))
	for _, ic := range m.Icons {
		hrefs = append(hrefs, ic.Href)
	}
	return m.Title, hrefs
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

// ---------- icon candidates: declared size in, real size verified ----------
//
// v0.71 test4: the fetch chain, scored. Candidates are gathered
// from the manifest, the page's icon links and the two
// conventional locations, then fetched biggest-declared first;
// a download counts only at its REAL pixel size (declared
// sizes lie often enough that the v0.69 first-success chain
// shipped 32px favicons over 512px manifest art). Fetching
// stops early at the first verified iconStopPx image -- a
// 192px+ icon is the browser-start-page quality bar, and more
// downloads would only cost node traffic.

const (
	iconStopPx   = 192     // verified size that ends the hunt
	iconGoodPx   = 128     // a stored icon below this is "stale" for the backfill
	iconFetchMax = 10      // candidate downloads per entry, worst case
	iconFetchCap = 1 << 20 // per-download byte cap (512px PNGs outgrow the old 256K)
)

type iconCandidate struct {
	URL      string
	Declared int
}

type fetchedIcon struct {
	Cand  iconCandidate
	Bytes []byte
	Ext   string
	Real  int // real largest dimension; iconSizeAny for SVG; 0 = unmeasurable
}

// iconEff is the size a fetched icon competes at: its real
// size when measurable, else what it declared.
func iconEff(f fetchedIcon) int {
	if f.Real > 0 {
		return f.Real
	}
	return f.Cand.Declared
}

// decideIconWinner picks the fetched icon with the largest
// effective size; ties go to the earliest fetch (the candidate
// order is a preference order). Pure -- tests pin it.
func decideIconWinner(fs []fetchedIcon) int {
	best := 0
	for i := range fs {
		if iconEff(fs[i]) > iconEff(fs[best]) {
			best = i
		}
	}
	return best
}

// iconRealSize measures an icon payload's largest real
// dimension from its own headers -- PNG IHDR, the ICO frame
// directory, the GIF logical screen, a JPEG SOF segment, the
// WebP canvas headers. 0 = could not measure (the caller then
// trusts the declared size). Pure -- tests pin it.
func iconRealSize(b []byte) int {
	switch sniffIconExt(b) {
	case ".svg":
		return iconSizeAny
	case ".png":
		if len(b) >= 24 {
			return max(int(binary.BigEndian.Uint32(b[16:20])), int(binary.BigEndian.Uint32(b[20:24])))
		}
	case ".ico":
		if len(b) >= 6 {
			best := 0
			count := int(binary.LittleEndian.Uint16(b[4:6]))
			for i := 0; i < count; i++ {
				off := 6 + i*16
				if off+2 > len(b) {
					break
				}
				w, h := int(b[off]), int(b[off+1])
				if w == 0 {
					w = 256
				}
				if h == 0 {
					h = 256
				}
				best = max(best, w, h)
			}
			return best
		}
	case ".gif":
		if len(b) >= 10 {
			return max(int(binary.LittleEndian.Uint16(b[6:8])), int(binary.LittleEndian.Uint16(b[8:10])))
		}
	case ".jpg":
		return jpegRealSize(b)
	case ".webp":
		return webpRealSize(b)
	}
	return 0
}

// jpegRealSize scans JPEG segments for a start-of-frame
// segment and reads its dimensions.
func jpegRealSize(b []byte) int {
	i := 2 // skip the SOI marker
	for i+4 <= len(b) {
		if b[i] != 0xFF {
			return 0
		}
		marker := b[i+1]
		if marker == 0xD9 || marker == 0xDA {
			return 0 // EOI / SOS: no SOF before the scan data
		}
		segLen := int(binary.BigEndian.Uint16(b[i+2 : i+4]))
		if segLen < 2 {
			return 0
		}
		if marker >= 0xC0 && marker <= 0xCF && marker != 0xC4 && marker != 0xC8 && marker != 0xCC {
			if i+9 > len(b) {
				return 0
			}
			h := int(binary.BigEndian.Uint16(b[i+5 : i+7]))
			w := int(binary.BigEndian.Uint16(b[i+7 : i+9]))
			return max(w, h)
		}
		i += 2 + segLen
	}
	return 0
}

// webpRealSize reads the canvas size from the VP8X / VP8L /
// lossy-VP8 headers.
func webpRealSize(b []byte) int {
	if len(b) < 30 {
		return 0
	}
	switch string(b[12:16]) {
	case "VP8X":
		w := 1 + int(b[24]) | int(b[25])<<8 | int(b[26])<<16
		h := 1 + int(b[27]) | int(b[28])<<8 | int(b[29])<<16
		return max(w, h)
	case "VP8L":
		if b[20] != 0x2f {
			return 0
		}
		w := 1 + int(b[21]) | (int(b[22])&0x3f)<<8
		h := 1 + (int(b[22]) >> 6) | int(b[23])<<2 | (int(b[24])&0x0f)<<10
		return max(w, h)
	case "VP8 ":
		w := int(binary.LittleEndian.Uint16(b[26:28])) & 0x3fff
		h := int(binary.LittleEndian.Uint16(b[28:30])) & 0x3fff
		return max(w, h)
	}
	return 0
}

// parseManifestIcons reads a PWA manifest's icons array into
// candidates. Icons whose purpose is ONLY "monochrome" are
// skipped -- they are single-colour badge masks, not artwork.
// Pure -- tests pin it.
func parseManifestIcons(b []byte) []iconCandidate {
	var doc struct {
		Icons []struct {
			Src     string `json:"src"`
			Sizes   string `json:"sizes"`
			Purpose string `json:"purpose"`
		} `json:"icons"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil
	}
	var out []iconCandidate
	for _, ic := range doc.Icons {
		src := strings.TrimSpace(ic.Src)
		if src == "" {
			continue
		}
		if p := strings.ToLower(ic.Purpose); p != "" {
			fields := strings.Fields(p)
			if len(fields) == 1 && fields[0] == "monochrome" {
				continue
			}
		}
		out = append(out, iconCandidate{URL: src, Declared: declaredIconSize(ic.Sizes, 0)})
	}
	return out
}

// fetchBestIcon runs the scored chain for one entry URL and
// returns the winning download. meta is the already-fetched
// page head (zero value when the page itself would not load --
// the conventional locations are still tried).
func fetchBestIcon(ctx context.Context, pu *url.URL, meta pageMeta) (fetchedIcon, bool) {
	origin := pu.Scheme + "://" + pu.Host
	var cands []iconCandidate
	// 1. The manifest's icons, biggest declared first: the
	//    192/512px artwork usually lives ONLY here.
	if meta.Manifest != "" {
		if mu, err := pu.Parse(meta.Manifest); err == nil {
			if mb, err := fetchViaNode(ctx, mu.String(), 256*1024); err == nil {
				var mc []iconCandidate
				for _, c := range parseManifestIcons(mb) {
					if ru, rerr := mu.Parse(c.URL); rerr == nil {
						mc = append(mc, iconCandidate{URL: ru.String(), Declared: c.Declared})
					}
				}
				sort.SliceStable(mc, func(i, j int) bool { return mc[i].Declared > mc[j].Declared })
				cands = append(cands, mc...)
			}
		}
	}
	// 2. The page's own icon links, biggest declared first
	//    (an undeclared apple-touch-icon counts 180 by
	//    convention, an undeclared favicon counts nothing).
	links := make([]iconCandidate, 0, len(meta.Icons))
	for _, ic := range meta.Icons {
		if ru, err := pu.Parse(ic.Href); err == nil {
			links = append(links, iconCandidate{URL: ru.String(), Declared: ic.Declared})
		}
	}
	sort.SliceStable(links, func(i, j int) bool { return links[i].Declared > links[j].Declared })
	cands = append(cands, links...)
	// 3. The conventional locations.
	cands = append(cands,
		iconCandidate{URL: origin + "/apple-touch-icon.png", Declared: 180},
		iconCandidate{URL: origin + "/favicon.ico", Declared: 0},
	)
	seen := map[string]bool{}
	var got []fetchedIcon
	for _, c := range cands {
		if len(got) >= iconFetchMax || len(seen) >= iconFetchMax+4 {
			break
		}
		if seen[c.URL] {
			continue
		}
		seen[c.URL] = true
		b, err := fetchViaNode(ctx, c.URL, iconFetchCap)
		if err != nil || len(b) == 0 {
			continue
		}
		ext := sniffIconExt(b)
		if ext == "" {
			continue
		}
		fi := fetchedIcon{Cand: c, Bytes: b, Ext: ext, Real: iconRealSize(b)}
		got = append(got, fi)
		if iconEff(fi) >= iconStopPx {
			break
		}
	}
	if len(got) == 0 {
		return fetchedIcon{}, false
	}
	return got[decideIconWinner(got)], true
}

// storedIconRealSize measures the entry's currently stored
// icon (0 when it has none or the file is gone/unmeasurable).
func storedIconRealSize(a *webApp) int {
	if a.Icon == "" || !webIconFileRe.MatchString(a.Icon) {
		return 0
	}
	b, err := os.ReadFile(filepath.Join(webIconDir(), a.Icon))
	if err != nil {
		return 0
	}
	return iconRealSize(b)
}

// refreshWebAppAssets fills an entry's visual assets from its
// site: the theme color whenever it is missing, and -- with
// wantIcon -- a stored icon that only ever gets REPLACED by a
// strictly larger verified download (a backfill must never
// trade a good stored icon for a worse one). Returns true when
// the entry changed. Best effort throughout: any fetch failure
// leaves the entry exactly as it was.
func refreshWebAppAssets(ctx context.Context, a *webApp, wantIcon bool) bool {
	pu, err := url.Parse(a.URL)
	if err != nil || pu.Host == "" {
		return false
	}
	changed := false
	var meta pageMeta
	if page, err := fetchViaNode(ctx, a.URL, 512*1024); err == nil {
		meta = parsePageMetaFull(page)
		if a.ThemeColor == "" && meta.ThemeColor != "" {
			a.ThemeColor = meta.ThemeColor
			changed = true
		}
	}
	if !wantIcon {
		return changed
	}
	best, ok := fetchBestIcon(ctx, pu, meta)
	if !ok {
		return changed
	}
	if a.Icon != "" && iconEff(best) <= storedIconRealSize(a) {
		return changed // no strict win: the stored icon stays
	}
	name := webIconName(a.URL) + best.Ext
	if err := os.MkdirAll(webIconDir(), 0o755); err != nil {
		return changed
	}
	if err := os.WriteFile(filepath.Join(webIconDir(), name), best.Bytes, 0o644); err != nil {
		return changed
	}
	a.Icon = name
	return true
}

// ensureWebAppIcon fetches and stores the entry's icon if it
// does not have a live one yet, updating a.Icon (and the theme
// color) in place. Best effort throughout: any failure leaves
// the entry icon-less (the letter tile stands in) and returns
// false.
func ensureWebAppIcon(ctx context.Context, a *webApp) bool {
	_ = refreshWebAppAssets(ctx, a, true)
	if a.Icon != "" && webIconFileRe.MatchString(a.Icon) {
		if _, err := os.Stat(filepath.Join(webIconDir(), a.Icon)); err == nil {
			return true
		}
	}
	return false
}

// ---------- old-entry backfill (v0.71 test4) ----------
//
// Entries added before test4 may have no icon at all (the old
// chain failed or never ran), a small favicon where the site
// offers manifest artwork, and no theme color. Once per daemon
// start, shortly after services come up, the ledger is walked
// and every stale entry gets ONE throttled refresh through the
// new chain (refreshWebAppAssets), saving after each success
// so an interrupted pass keeps its progress. This is also what
// retires the view's Google favicon-service path: once every
// reachable entry carries a local icon or a theme color, the
// letter tile it falls back to is the site's own.

func webAppIconStale(a *webApp) bool {
	if a.ThemeColor == "" {
		return true
	}
	if a.Icon == "" {
		return true
	}
	return storedIconRealSize(a) < iconGoodPx
}

func backfillWebAppIcons() {
	// Let startup settle first: the backfill is a background
	// nicety, never a competitor for the first-run work.
	time.Sleep(8 * time.Second)
	list := loadWebApps()
	filled := 0
	for i := range list {
		if !webAppIconStale(&list[i]) {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		changed := refreshWebAppAssets(ctx, &list[i], true)
		cancel()
		if changed {
			saveWebApps(list)
			filled++
		}
		time.Sleep(1200 * time.Millisecond) // throttle: one entry at a time, gently
	}
	if filled > 0 {
		logf("browser", "info", "网站图标回填：已为 %d 个老条目补齐高清图标或主题色（抓取链含站点清单大图）", filled)
	}
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
