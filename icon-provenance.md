# Icon provenance

## Built-in app icons — SELF-DRAWN (v0.56 test2; redrawn v0.71 test5, current)

The built-in apps' icons are original artwork drawn for this
project. They are authored as SVG in `assets/icons-svg/`
(one 256×256 canvas, one superellipse "squircle" geometry,
one top-light gradient model, bold glyphs, no text except the
terminal's `>_` motif), rendered to PNG, and embedded as
base64 in `icons_data.go`, served by the daemon at
`/api/appicon/<id>`.

The test5 redraw (2026-11-15) kept the family's geometry and
light model but replaced the flat single-colour glyphs with
layered scenes: the workstation's monitor now carries a tiny
landscape, the files folder a paper sheet with a folded
corner, the console card coloured log lines, the settings
gear a metallic gradient with a recessed hub. Same ten ids,
same pipeline — richer faces, after the user field-rejected
the recoloured-tile pass ("就像我发给你那样的图标不行吗").

| id        | app      | motif                                        |
|-----------|----------|----------------------------------------------|
| home      | 工作站   | monitor on indigo                            |
| term      | 终端     | `>_` prompt on near-black                    |
| files     | 文件     | original two-tone folder (no Finder face)    |
| ssh       | SSH      | gold key on deep violet                      |
| servermon | 服务器   | pulse waveform on deep navy                  |
| console   | 控制台   | paper card, traffic dots + chunky log lines  |
| appstore  | 应用商店 | original storefront (awning + door)          |
| sys       | 设置     | original 8-tooth gear on graphite            |
| core      | 节点     | connected-nodes routing glyph on violet      |
| taskmgr   | 任务管理器 | usage bars on steel blue (v0.71 test4)       |

## Third-party icons (kept as-is, real artwork)

These are the apps' own brand/mascot artwork, not drawn here;
the UI only defines their *presentation* (one squircle mask,
edge-to-edge fill, one soft cell shadow — see index.html):

- `openclaw` — 小龙虾： the OpenClaw app's own mascot artwork
  (embedded in `icons_data.go` alongside the drawn set).
- Chrome — served live by the daemon from the installed
  Chrome's own resources (`/api/chrome/icon`).
- Firefox — served live from the component's own resources
  (`/api/firefox/icon`).
- Store apps / productivity components — each app's own icon,
  extracted from its executable by the daemon.
- Web apps — the site's own icon (see the preset library
  below and the test4 fetch chain in `addapp.go`).

## Preset web-service icons — the services' own artwork (v0.71 test5)

Nineteen well-known services ship as presets in
`preset_icons_data.go`: the icon each service itself
publishes for identification — its PWA manifest or touch
icon where the site offers one, otherwise the artwork the
service publishes for its own app — fetched once at build
time, verified by real pixel size (all ≥192px), and used
byte-for-byte. They exist for the same reason browsers
pre-ship start-page artwork: so a known service's face
never depends on a runtime fetch succeeding. At runtime a
preset outranks the live fetch chain whenever it is larger
than the icon an entry has stored; the startup backfill
upgrades old entries to it. Source per preset (fetched
2026-11-15):

| id          | service    | size | published source |
|-------------|------------|------|------------------|
| telegram    | Telegram   | 256  | telegram.org (official logo asset) |
| whatsapp    | WhatsApp   | 512  | WhatsApp's published app artwork |
| x           | X          | 512  | x.com apple-touch-icon (1024, downscaled) |
| youtube     | YouTube    | 512  | youtube.com PWA manifest icon (maskable) |
| tradingview | TradingView| 512  | tradingview.com PWA manifest icon |
| github      | GitHub     | 512  | github.com app icon (githubassets) |
| gmail       | Gmail      | 256  | ssl.gstatic.com Gmail icon (ICO largest frame) |
| outlook     | Outlook    | 512  | Microsoft's published Outlook app artwork |
| tiktok      | TikTok     | 512  | tiktok.com PWA manifest icon |
| Muse      | Muse     | 338  | claude.ai app icon |
| chatgpt     | ChatGPT    | 512  | chatgpt.com PWA icon |
| vercel      | Vercel     | 360  | vercel.com apple-touch-icon |
| coinglass   | Coinglass  | 417  | coinglasscdn.com published icon |
| bilibili    | 哔哩哔哩    | 512  | static.hdslb.com mobile web icon (carries bilibili's own "WEB" ribbon) |
| dida365     | 滴答清单    | 512  | Appest's published 滴答清单 app artwork |
| gtranslate  | 谷歌翻译    | 512  | Google's published Translate app artwork |
| gemini      | Gemini     | 512  | Google's published Gemini app artwork |
| tailscale   | Tailscale  | 512  | tailscale.com favicon.png (768, downscaled) |
| firefox     | Firefox    | 196  | firefox.com favicon-196 (the browser's own mark) |

Not preset, deliberately: farside.co.uk (ETF 资金流） — the
site publishes only an 80px default WordPress favicon,
which is neither ≥192px nor the service's own artwork; it
stays on the live fetch chain. Bilibili's App Store icon
was a limited-time game-promotion variant at fetch time
and was rejected for the standard mobile-web icon above.

## History

- v0.56 test1 shipped artwork downloaded from macosicons.com
  (decision "plan A" at the time). The user field-rejected the
  set the same evening: assets from many different uploaders
  never cohered as one family, several entries were off-style,
  and one (SSH) was a full-bleed third-party badge in a frame.
- test2 therefore replaced ALL built-in artwork with the
  self-drawn set above. The macosicons assets were removed
  from the embed entirely; this file is the record. The
  2026-10-29 "self-drawn only" line for MJ's own icons stands
  restored.
- v0.71 test4 aligned colours — the gradient fallbacks were
  retinted to match the art and web letter tiles took the
  site's theme colour. The user accepted the mechanism but
  field-rejected the LOOK (2026-11-15): retinted tiles are
  still tiles. test5 therefore changed the artwork itself:
  the preset library above for web services, and the
  layered redraw of the built-in family.
