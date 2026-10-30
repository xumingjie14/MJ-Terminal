# MJ Terminal icon refresh — provenance

Source: [macOSicons](https://macosicons.com) — community icon gallery (plan A: use this site's assets wholesale, user-approved).
Shipped in: MJ Terminal v0.56 (2026-10-30), per the user's plan-A decision of 2026-10-30 ("use the site's assets wholesale for now, revisit later"). Fetched: 2026-10-30. All files are 1024×1024 PNG, saved in `assets/icons-src/<mjid>.png`.

How the files were obtained: each icon's detail page / search record exposes three assets —
`lowResPngUrl` (256×256 preview, **not** used), `iOSUrl` (1024×1024 full-bleed iOS-style PNG), and
`icnsUrl` (the original `.icns`). The saved PNGs are the **largest PNG extracted from the `.icns`**
(1024×1024, the macOS squircle variant with transparent margins — same pixel resolution as
`iOSUrl`, but the correct macOS form; the iOS variant is full-bleed and would look oversized in a Dock).
Exception note: `ssh` has an empty `iOSUrl` on the site, so its `.icns` was the only high-res source anyway.

| MJ app | Site icon title | Uploader | Detail page | Downloaded resolution | File |
|---|---|---|---|---|---|
| home 工作站 | iMac Monterey | efcjimbo | https://macosicons.com/icon/imac-monterey-EXSf475Vu1 | 1024×1024 | `assets/icons-src/home.png` |
| term 终端 | Terminal | robertgs | https://macosicons.com/icon/terminal-eJCDM3QFYI | 1024×1024 | `assets/icons-src/term.png` |
| files 文件 | Finder | octaviotti | https://macosicons.com/icon/finder-B7DXS2NKpM | 1024×1024 | `assets/icons-src/files.png` |
| browser 浏览器 | Google_Chrome | octaviotti | https://macosicons.com/icon/google-chrome-ihihayFE4C | 1024×1024 | `assets/icons-src/browser.png` |
| ssh SSH | SSH Config Editor | christippett | https://macosicons.com/icon/ssh-config-editor-xTylM7mPPt | 1024×1024 (extracted from .icns; no iOS PNG on site) | `assets/icons-src/ssh.png` |
| servermon 服务器 | Activity_Monitor_Alt | octaviotti | https://macosicons.com/icon/activity-monitor-alt-sRGOKLpHUG | 1024×1024 | `assets/icons-src/servermon.png` |
| console 控制台 | Console | maicelo | https://macosicons.com/icon/console-Gyxn4flSbg | 1024×1024 | `assets/icons-src/console.png` |
| appstore 应用商店 | App Store | incorrect | https://macosicons.com/icon/app-store-f36wSCWrM0 | 1024×1024 | `assets/icons-src/appstore.png` |
| openclaw 小龙虾 | OpenClaw | erismachina | https://macosicons.com/icon/openclaw-SuT317nlsB | 1024×1024 | `assets/icons-src/openclaw.png` |
| core 节点 | VPN | epicdude2121 | https://macosicons.com/icon/vpn-uc7pTAcvCn | 1024×1024 | `assets/icons-src/core.png` |
| sys 设置 | System Preferences | incorrect | https://macosicons.com/icon/system-preferences-2DLff7nlvI | 1024×1024 | `assets/icons-src/sys.png` |

## Selection notes

- **home**: "iMac Monterey" — a desktop Mac with a colourful Monterey wallpaper; reads instantly as home base / workstation. (Plain "iMac" variants were black-screen and dull; MacBook variants are laptops.)
- **term**: most-downloaded "Terminal" on the site (6,171 downloads at fetch time) — dark squircle, white `>_`.
- **files**: most-downloaded "Finder" (7,125 downloads) — classic blue/white Finder face.
- **browser**: "Google_Chrome" squircle — white squircle with the Chrome circle, canonical.
- **ssh**: no generic "SSH terminal" icon exists on the site (search "SSH" returns only folder icons and SSH-client apps). "SSH Config Editor" is an orange/black folder-style squircle literally labelled `.ssh` with a `>_` corner — unmistakably SSH and distinct from Terminal. (Runner-up family: Termius cloud icons.)
- **servermon**: "Server" search results are mostly drives/globes/folders, not racks; "Activity_Monitor_Alt" (black squircle, green monitoring waveform) is the canonical monitoring/stats look and was an explicitly suggested alternative.
- **console**: every "Console" icon on the site uses the same magnified-log-screen design (that is the real Apple Console.app icon concept). The maicelo variant ("macOS 09:41" in amber monospace on dark brown) is the cleanest/fully legible one and is clearly distinct from Terminal. Runner-up: "Console" by Random Ics (dqC6Q2dv3e, yellow "WARNING … 7:36", 491 downloads).
- **openclaw**: searching "lobster" returns **0** icons on the site, and "claw"/"crab" return only game icons — but a real **OpenClaw** icon exists: a red lobster-like mascot character (the OpenClaw mascot) on black, by erismachina. Used that.
- **core**: "VPN" by epicdude2121 — blue network-globe with a white padlock; clean VPN/proxy read. (Alternatives considered: WireGuard red dragon, Clash/ClashX white-cat icons.)
- **sys**: the site title is "System Preferences" — that is the same macOS settings gear icon (silver gear squircle); it is the most-downloaded settings icon family on the site (3,590 downloads for this variant). Modern-name "System Settings" variants are the same gear in darker finishes.

## How search / download works on macosicons.com (reproduction)

- The public/paid API is separate: `POST https://api.macosicons.com/api/v1/search` with an `x-api-key` header (key from a macosicons account, see https://macosicons.com/developers).
- The website itself searches through its own keyless Nuxt server proxy, found in the site's JS bundle (`mainStore.searchIcons`):
  `POST https://macosicons.com/api/search` with JSON body `{"query": "<text>", "searchOptions": {"hitsPerPage": 30, "page": 1, "filters": [], "sort": ["downloads:desc"]}}`.
  `sort` is optional; `"downloads:desc"` sorts most-downloaded first. A single icon can be fetched with `{"query": "", "searchOptions": {"filters": ["objectID = <id>"]}}`.
  Each hit contains `objectID`, `appName`, `usersName`, `downloads`, `lowResPngUrl`, `iOSUrl`, `icnsUrl`.
- Detail pages are `https://macosicons.com/icon/<slug>-<objectID>` — the server resolves by the objectID suffix and canonicalises the slug from the icon name (any slug works to reach the page; the `<link rel="canonical">` gives the official URL used in the table above).
- Behind the scenes the data also lives in a Parse backend (`https://parse.macosicons.com/parse`, app id in the page payload) and files on `https://s3-new.macosicons.com/macosicons/parse/…`, which is where the three asset URLs point. Direct Parse class queries are permission-denied; the `/api/search` proxy is the working route.
