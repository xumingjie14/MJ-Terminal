# Icon provenance

## Built-in app icons — SELF-DRAWN (v0.56 test2, current)

The built-in apps' icons are original artwork drawn for this
project. They are authored as SVG in `assets/icons-svg/`
(one 256×256 canvas, one superellipse "squircle" geometry,
one top-light gradient model, bold glyphs, no text except the
terminal's `>_` motif), rendered to PNG, and embedded as
base64 in `icons_data.go`, served by the daemon at
`/api/appicon/<id>`.

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
- Web apps — the site's own favicon.

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
