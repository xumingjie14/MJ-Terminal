package main

// Table-driven tests for the v0.36 ownership classifier and claim
// decider. The negative cases matter as much as the positive ones:
// a single false claim pulls a host window into the Stage Manager,
// which is the exact failure this feature exists to prevent.

import "testing"

const (
	tRoot      = `C:\MJ-Terminal`
	tChromeP   = `C:\MJ-Terminal\Data\ChromeProfile`
	tFFP       = `C:\MJ-Terminal\Data\FirefoxProfile`
	tChromeExe = `C:\Program Files\Google\Chrome\Application\chrome.exe`
	tEdgeExe   = `C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`
)

func tCtx() ownContext {
	return ownContext{RootDir: tRoot, ProfileDirs: []string{tChromeP, tFFP}, SelfPID: 999}
}

func TestClassifyOwnership(t *testing.T) {
	cases := []struct {
		name    string
		procs   []procFact
		seeds   map[uint32]bool
		wantPID uint32
		want    string // "" = must NOT be owned
	}{
		{
			name: "tree: descendant and grandchild of a daemon-started root",
			procs: []procFact{
				{PID: 100, PPID: 4, ImagePath: `C:\Tools\launcher.exe`},
				{PID: 101, PPID: 100, ImagePath: `C:\Tools\app.exe`},
				{PID: 102, PPID: 101, ImagePath: `C:\Tools\helper.exe`},
			},
			seeds: map[uint32]bool{100: true}, wantPID: 102, want: ownTree,
		},
		{
			name: "exe: store app image under Data/apps",
			procs: []procFact{
				{PID: 110, PPID: 4, ImagePath: `C:\MJ-Terminal\Data\apps\telegram\Telegram.exe`},
			},
			wantPID: 110, want: ownExe,
		},
		{
			name: "exe: Firefox component image under core/firefox",
			procs: []procFact{
				{PID: 111, PPID: 4, ImagePath: `C:\MJ-Terminal\core\firefox\firefox.exe`},
			},
			wantPID: 111, want: ownExe,
		},
		{
			name: "profile: isolated Chrome command line",
			procs: []procFact{
				{PID: 120, PPID: 4, ImagePath: tChromeExe,
					CmdLine: `"C:\Program Files\Google\Chrome\Application\chrome.exe" --user-data-dir=C:\MJ-Terminal\Data\ChromeProfile --app=https://x.com`},
			},
			wantPID: 120, want: ownProfile,
		},
		{
			name: "profile: quoted profile argument",
			procs: []procFact{
				{PID: 121, PPID: 4, ImagePath: tChromeExe,
					CmdLine: `chrome.exe --user-data-dir="C:\MJ-Terminal\Data\ChromeProfile"`},
			},
			wantPID: 121, want: ownProfile,
		},
		{
			name: "profile: Firefox -profile argument (space-separated)",
			procs: []procFact{
				{PID: 122, PPID: 4, ImagePath: `C:\MJ-Terminal\core\firefox\firefox.exe`,
					CmdLine: `firefox.exe -profile C:\MJ-Terminal\Data\FirefoxProfile -no-remote -new-window about:home`},
			},
			wantPID: 122, want: ownProfile, // profile evidence outranks exe
		},
		{
			name: "tree: browser hands a PDF to an external viewer (child of Chrome)",
			procs: []procFact{
				{PID: 130, PPID: 4, ImagePath: tChromeExe,
					CmdLine: `chrome.exe --user-data-dir=C:\MJ-Terminal\Data\ChromeProfile`},
				{PID: 131, PPID: 130, ImagePath: `C:\Users\a\AppData\Local\SumatraPDF\SumatraPDF.exe`},
			},
			wantPID: 131, want: ownTree,
		},
		// ---- negatives: none of these may ever be owned ----
		{
			name: "NEG host Chrome with its own user profile",
			procs: []procFact{
				{PID: 140, PPID: 4, ImagePath: tChromeExe,
					CmdLine: `chrome.exe --user-data-dir=C:\Users\a\AppData\Local\Google\Chrome\User Data`},
			},
			wantPID: 140, want: "",
		},
		{
			name: "NEG host Edge, no command line evidence",
			procs: []procFact{
				{PID: 141, PPID: 4, ImagePath: tEdgeExe, CmdLine: `msedge.exe --no-first-run`},
			},
			wantPID: 141, want: "",
		},
		{
			name: "NEG same-named exe outside the terminal folder",
			procs: []procFact{
				{PID: 142, PPID: 4, ImagePath: `C:\Other\Telegram.exe`},
			},
			wantPID: 142, want: "",
		},
		{
			name: "NEG sibling folder with shared prefix (MJ-Terminal2)",
			procs: []procFact{
				{PID: 143, PPID: 4, ImagePath: `C:\MJ-Terminal2\Data\apps\x\x.exe`},
			},
			wantPID: 143, want: "",
		},
		{
			name: "NEG profile prefix trap: ChromeProfile2",
			procs: []procFact{
				{PID: 144, PPID: 4, ImagePath: tChromeExe,
					CmdLine: `chrome.exe --user-data-dir=C:\MJ-Terminal\Data\ChromeProfile2`},
			},
			wantPID: 144, want: "",
		},
		{
			name: "NEG truncated profile path",
			procs: []procFact{
				{PID: 145, PPID: 4, ImagePath: tChromeExe,
					CmdLine: `chrome.exe --user-data-dir=C:\MJ-Terminal\Data\ChromeProfil`},
			},
			wantPID: 145, want: "",
		},
		{
			name: "NEG profile path glued inside a longer directory name",
			procs: []procFact{
				{PID: 146, PPID: 4, ImagePath: tChromeExe,
					CmdLine: `chrome.exe --user-data-dir=C:\MJ-Terminal\Data\NotChromeProfile`},
			},
			wantPID: 146, want: "",
		},
		{
			name: "NEG browser with empty command line and no other evidence",
			procs: []procFact{
				{PID: 147, PPID: 4, ImagePath: tChromeExe, CmdLine: ""},
			},
			wantPID: 147, want: "",
		},
		{
			name: "NEG the daemon itself (image under root, but SelfPID)",
			procs: []procFact{
				{PID: 999, PPID: 4, ImagePath: `C:\MJ-Terminal\MJ-System.exe`},
			},
			wantPID: 999, want: "",
		},
		{
			name: "NEG child of the host browser stays host (no poisoned closure)",
			procs: []procFact{
				{PID: 150, PPID: 4, ImagePath: tChromeExe,
					CmdLine: `chrome.exe --user-data-dir=C:\Users\a\AppData\Local\Google\Chrome\User Data`},
				{PID: 151, PPID: 150, ImagePath: `C:\Users\a\Downloads\random.exe`},
			},
			wantPID: 151, want: "",
		},
	}
	for _, c := range cases {
		owned := classifyOwnership(c.procs, c.seeds, tCtx())
		got := owned[c.wantPID]
		if got != c.want {
			t.Errorf("%s: pid %d classified %q, want %q", c.name, c.wantPID, got, c.want)
		}
	}
}

func TestClassifyOwnershipBox(t *testing.T) {
	weixin := `C:\Program Files\Tencent\Weixin\Weixin.exe` // virtual path, outside the terminal folder
	boxCtx := func(pids map[uint32]bool) ownContext {
		c := tCtx()
		c.BoxPIDs = pids
		return c
	}
	cases := []struct {
		name    string
		procs   []procFact
		ctx     ownContext
		wantPID uint32
		want    string
	}{
		{
			name: "box: boxed WeChat (service-created, virtual image path) is owned via the box list",
			procs: []procFact{
				{PID: 300, PPID: 88, ImagePath: weixin},
			},
			ctx: boxCtx(map[uint32]bool{300: true}), wantPID: 300, want: ownBox,
		},
		{
			name: "box: child of a boxed process joins via tree closure",
			procs: []procFact{
				{PID: 300, PPID: 88, ImagePath: weixin},
				{PID: 301, PPID: 300, ImagePath: `C:\Program Files\Tencent\Weixin\WeChatAppEx.exe`},
			},
			ctx: boxCtx(map[uint32]bool{300: true}), wantPID: 301, want: ownTree,
		},
		{
			name: "NEG host WeChat, same image, PID not on the box list",
			procs: []procFact{
				{PID: 301, PPID: 4, ImagePath: weixin},
			},
			ctx: boxCtx(map[uint32]bool{300: true}), wantPID: 301, want: "",
		},
		{
			name: "NEG empty box list: a boxed-looking process is not owned",
			procs: []procFact{
				{PID: 300, PPID: 88, ImagePath: weixin},
			},
			ctx: boxCtx(map[uint32]bool{}), wantPID: 300, want: "",
		},
		{
			name: "NEG nil box list (component absent): not owned",
			procs: []procFact{
				{PID: 300, PPID: 88, ImagePath: weixin},
			},
			ctx: boxCtx(nil), wantPID: 300, want: "",
		},
		{
			name: "NEG the daemon itself is never box-owned even if listed",
			procs: []procFact{
				{PID: 999, PPID: 4, ImagePath: `C:\MJ-Terminal\MJ-System.exe`},
			},
			ctx: boxCtx(map[uint32]bool{999: true}), wantPID: 999, want: "",
		},
	}
	for _, c := range cases {
		owned := classifyOwnership(c.procs, nil, c.ctx)
		if got := owned[c.wantPID]; got != c.want {
			t.Errorf("%s: pid %d classified %q, want %q", c.name, c.wantPID, got, c.want)
		}
	}
	if txt := ownReasonText(ownBox); txt != "盒内进程名单" {
		t.Errorf("ownReasonText(ownBox) = %q", txt)
	}
}

func TestDecideClaim(t *testing.T) {
	owned := map[uint32]string{200: ownProfile, 210: ownExe, 220: ownTree, 230: ownProfile, 240: ownBox}
	reg := map[uint64]bool{5000: true}
	base := claimOpts{AutoClaimBrowser: true, BrowserPIDs: map[uint32]bool{}, ShellPIDs: map[uint32]bool{}}
	browserWin := windowFact{HWND: 6001, PID: 200, Class: "Chrome_WidgetWin_1", Title: "GitHub - Google Chrome", Visible: true, W: 900, H: 700}

	cases := []struct {
		name string
		w    windowFact
		opts claimOpts
		want claimVerdict
	}{
		{"profile popup claims as browser", browserWin, base,
			claimVerdict{Claim: true, Browser: true, Reason: ownProfile}},
		{"already registered", windowFact{HWND: 5000, PID: 200, Class: "Chrome_WidgetWin_1", Title: "x", Visible: true, W: 900, H: 700}, base,
			claimVerdict{}},
		{"invisible", windowFact{HWND: 6002, PID: 210, Class: "TMainForm", Title: "x", Visible: false, W: 900, H: 700}, base,
			claimVerdict{}},
		{"owned dialog (has owner window)", windowFact{HWND: 6003, PID: 210, Class: "#32770", Title: "x", Visible: true, Owned: true, W: 900, H: 700}, base,
			claimVerdict{}},
		{"too small (toast speck)", windowFact{HWND: 6004, PID: 210, Class: "TMainForm", Title: "x", Visible: true, W: 120, H: 60}, base,
			claimVerdict{}},
		{"unowned process", windowFact{HWND: 6005, PID: 777, Class: "Chrome_WidgetWin_1", Title: "host", Visible: true, W: 900, H: 700}, base,
			claimVerdict{}},
		{"profile but helper class (notification window)", windowFact{HWND: 6006, PID: 200, Class: "Chrome_WidgetWin_0", Title: "x", Visible: true, W: 900, H: 700}, base,
			claimVerdict{}},
		{"profile gated by the switch", browserWin,
			claimOpts{AutoClaimBrowser: false, BrowserPIDs: map[uint32]bool{}, ShellPIDs: map[uint32]bool{}},
			claimVerdict{Gated: true, Browser: true, Reason: ownProfile}},
		{"exe window claims regardless of the switch", windowFact{HWND: 6007, PID: 210, Class: "TMainForm", Title: "Telegram", Visible: true, W: 900, H: 700},
			claimOpts{AutoClaimBrowser: false, BrowserPIDs: map[uint32]bool{}, ShellPIDs: map[uint32]bool{}},
			claimVerdict{Claim: true, Reason: ownExe}},
		{"empty title defers on first sight", windowFact{HWND: 6008, PID: 210, Class: "TMainForm", Title: "", Visible: true, W: 900, H: 700}, base,
			claimVerdict{Defer: true, Reason: ownExe}},
		{"empty title claims after grace with NoTitle", windowFact{HWND: 6008, PID: 210, Class: "TMainForm", Title: "", Visible: true, W: 900, H: 700},
			claimOpts{AutoClaimBrowser: true, EmptySeenBefore: true, BrowserPIDs: map[uint32]bool{}, ShellPIDs: map[uint32]bool{}},
			claimVerdict{Claim: true, NoTitle: true, Reason: ownExe}},
		{"shell process never claims", windowFact{HWND: 6009, PID: 230, Class: "Chrome_WidgetWin_1", Title: "shell", Visible: true, W: 900, H: 700},
			claimOpts{AutoClaimBrowser: true, BrowserPIDs: map[uint32]bool{}, ShellPIDs: map[uint32]bool{230: true}},
			claimVerdict{}},
		{"registered-browser pid: further window is browser class and gated", windowFact{HWND: 6010, PID: 220, Class: "Chrome_WidgetWin_1", Title: "tab", Visible: true, W: 900, H: 700},
			claimOpts{AutoClaimBrowser: false, BrowserPIDs: map[uint32]bool{220: true}, ShellPIDs: map[uint32]bool{}},
			claimVerdict{Gated: true, Browser: true, Reason: ownTree}},
		{"tree window (PDF viewer child) claims", windowFact{HWND: 6011, PID: 220, Class: "SUMATRA_PDF_FRAME", Title: "doc.pdf", Visible: true, W: 900, H: 700}, base,
			claimVerdict{Claim: true, Reason: ownTree}},
		{"box window claims (boxed app main window, non-browser class)", windowFact{HWND: 6012, PID: 240, Class: "Qt6QWindow", Title: "微信", Visible: true, W: 900, H: 700}, base,
			claimVerdict{Claim: true, Reason: ownBox}},
		{"box window claims regardless of the browser auto-claim switch", windowFact{HWND: 6012, PID: 240, Class: "Qt6QWindow", Title: "微信", Visible: true, W: 900, H: 700},
			claimOpts{AutoClaimBrowser: false, BrowserPIDs: map[uint32]bool{}, ShellPIDs: map[uint32]bool{}},
			claimVerdict{Claim: true, Reason: ownBox}},
		{"v0.41 hidden box app window claims register-only", windowFact{HWND: 6013, PID: 240, Class: "Sandbox:MJBox:Qt51514QWindowIcon", Title: "WeChat", Visible: false, W: 900, H: 700}, base,
			claimVerdict{Claim: true, Reason: ownBox, HiddenClaim: true}},
		{"v0.41 hidden box plumbing class never claims", windowFact{HWND: 6014, PID: 240, Class: "Sandbox:MJBox:Chrome_SystemMessageWindow", Title: "", Visible: false, W: 900, H: 700}, base,
			claimVerdict{}},
		{"v0.41 hidden box IME window never claims (titled but system class)", windowFact{HWND: 6015, PID: 240, Class: "IME", Title: "Default IME", Visible: false, W: 900, H: 700}, base,
			claimVerdict{}},
		{"v0.41 hidden box untitled app-class window never claims (no NoTitle grace for hidden)", windowFact{HWND: 6016, PID: 240, Class: "Qt6QWindow", Title: "", Visible: false, W: 900, H: 700}, base,
			claimVerdict{}},
		{"v0.41 hidden exe window still never claims (relaxation is box-only)", windowFact{HWND: 6017, PID: 210, Class: "TMainForm", Title: "Telegram", Visible: false, W: 900, H: 700}, base,
			claimVerdict{}},
		{"v0.41 hidden box app window too small never claims", windowFact{HWND: 6018, PID: 240, Class: "Sandbox:MJBox:Qt51514QWindowIcon", Title: "WeChat", Visible: false, W: 120, H: 60}, base,
			claimVerdict{}},
	}
	for _, c := range cases {
		got := decideClaim(c.w, owned, reg, c.opts)
		if got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestSystemWindowClass(t *testing.T) {
	system := []string{
		"IME", "MSCTFIME UI", "RunDLL",
		"Chrome_SystemMessageWindow", "DisplayICC_SystemMessageWindow",
		"DisplayICC_Foo", "crashpad_SessionEndWatcher", "Sandboxie_DDE_ProxyClass1",
		// The boxed spellings from the v0.40 field log: the
		// Sandbox:<box>: prefix must not shield the junk classes.
		"Sandbox:MJBox:Chrome_SystemMessageWindow",
		"Sandbox:MJBox:DisplayICC_SystemMessageWindow",
		"Sandbox:MJBox:crashpad_SessionEndWatcher",
		"Sandbox:MJBox:Sandboxie_DDE_ProxyClass1",
		"Sandbox:MJBox:RunDLL",
		"Sandbox:MJBox:IME",
	}
	for _, c := range system {
		if !isSystemWindowClass(c) {
			t.Errorf("class %q must be system", c)
		}
	}
	app := []string{
		"Qt51514QWindowIcon", "Sandbox:MJBox:Qt51514QWindowIcon",
		"Qt6QWindow", "TMainForm", "Chrome_WidgetWin_1", "SUMATRA_PDF_FRAME",
		"#32770", "",
	}
	for _, c := range app {
		if isSystemWindowClass(c) {
			t.Errorf("class %q must NOT be system", c)
		}
	}
	if got := stripSandboxClassPrefix("Sandbox:MJBox:Qt51514QWindowIcon"); got != "Qt51514QWindowIcon" {
		t.Errorf("strip = %q", got)
	}
	if got := stripSandboxClassPrefix("Qt6QWindow"); got != "Qt6QWindow" {
		t.Errorf("strip (no prefix) = %q", got)
	}
}

func TestAppWindowShape(t *testing.T) {
	if !isAppWindowShape(windowFact{Title: "WeChat", Class: "Sandbox:MJBox:Qt51514QWindowIcon", Visible: false}) {
		t.Error("hidden titled Qt window must be app-shaped (visibility is not part of the test)")
	}
	if isAppWindowShape(windowFact{Title: "", Class: "Qt6QWindow"}) {
		t.Error("untitled window must not be app-shaped")
	}
	if isAppWindowShape(windowFact{Title: "dlg", Class: "Qt6QWindow", Owned: true}) {
		t.Error("owned (dialog) window must not be app-shaped")
	}
	if isAppWindowShape(windowFact{Title: "Default IME", Class: "IME"}) {
		t.Error("titled system-class window must not be app-shaped")
	}
}
