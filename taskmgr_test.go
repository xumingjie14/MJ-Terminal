package main

// taskmgr_test.go -- v0.69 block 1 gates: attribution
// classification, group assignment through ancestry, kill
// ordering, and the CPU sampler's delta math. All pure: the
// live snapshot is replaced by synthetic process tables.

import (
	"strings"
	"testing"
	"time"
)

// The classification context is exercised in THREE path forms
// on every run, because the path SHAPE is itself the variable
// that once hid a real failure from Linux CI (v0.69-test1,
// fixed in test2):
//
//   - windows rooted: what the daemon's live context actually
//     carries on the only platform the task manager runs on
//     (dataDir()/exeDir() are absolute, rooted paths).
//   - posix separators: what filepath.Join produces on Linux.
//   - windows drive-relative: what filepath.Join produces on
//     WINDOWS from a bare drive element -- Join("D:", "MJ") ==
//     "D:MJ", no separator after the colon. The test1 fixtures
//     were built exactly that way, so on the Windows runner
//     every profile needle was drive-relative while process
//     command lines are rooted, tmRootClass silently returned
//     "" for Chrome/Firefox, and the tree test watched them
//     land in "other-chrome.exe" / "other-unknown". The forms
//     stay in the suite so a separator can never again be a
//     CI-only variable.
//
// Command-line fixtures stay literal rooted Windows text in
// all three forms: that is what real Windows processes carry,
// whatever shape our own context paths take.
var tmCtxForms = []struct {
	name string
	root string
	sep  string
}{
	{"windows rooted", `D:\MJ`, `\`},
	{"posix separators", "D:/MJ", "/"},
	{"windows drive-relative", `D:MJ`, `\`},
}

func tmTestCtxForm(root, sep string) tmClassCtx {
	j := func(parts ...string) string { return strings.Join(parts, sep) }
	data := j(root, "Data")
	return tmClassCtx{
		CorePID:       500,
		CoreExe:       j(root, "core", "sing-box.exe"),
		ChromeProfile: j(data, "ChromeProfile"),
		FFAppProfile:  j(data, "FirefoxProfileApp"),
		FFWinProfile:  j(data, "FirefoxProfile"),
		BoxPIDs:       map[uint32]bool{900: true},
		OpenClawDir:   j(data, "openclaw"),
		DirGroups: []tmDirGroup{
			{Prefix: j(data, "apps", "telegram"), ID: "telegram", Name: "Telegram", Kind: "store"},
			{Prefix: j(data, "components", "krita"), ID: "krita", Name: "Krita", Kind: "component"},
		},
		ShellExclusion: map[uint32]bool{},
	}
}

func TestTMRootClass(t *testing.T) {
	for _, form := range tmCtxForms {
		cx := tmTestCtxForm(form.root, form.sep)
		j := func(parts ...string) string { return strings.Join(parts, form.sep) }
		cases := []struct {
			name string
			p    procFact
			want string
		}{
			{"core by pid", procFact{PID: 500, ImagePath: `C:\elsewhere\sing-box.exe`}, "core"},
			{"core by image", procFact{PID: 501, ImagePath: cx.CoreExe}, "core"},
			{"chrome main", procFact{PID: 100, CmdLine: `chrome.exe --user-data-dir="D:\MJ\Data\ChromeProfile"`}, "browser"},
			{"firefox app profile wins over window prefix", procFact{PID: 200, CmdLine: `firefox.exe -profile "D:\MJ\Data\FirefoxProfileApp"`}, "firefox-app"},
			{"firefox window profile", procFact{PID: 201, CmdLine: `firefox.exe -profile "D:\MJ\Data\FirefoxProfile"`}, "firefox-window"},
			{"box member", procFact{PID: 900, ImagePath: `C:\Program Files\WeChat\WeChat.exe`}, "sandbox"},
			{"openclaw node", procFact{PID: 300, ImagePath: j(cx.OpenClawDir, "node.exe")}, "openclaw"},
			{"store app", procFact{PID: 400, ImagePath: j(cx.DirGroups[0].Prefix, "Telegram.exe")}, "store-telegram"},
			{"component", procFact{PID: 401, ImagePath: j(cx.DirGroups[1].Prefix, "bin", "krita.exe")}, "component-krita"},
			{"unrelated host process", procFact{PID: 999, ImagePath: `C:\Windows\explorer.exe`}, ""},
			{"chrome renderer without profile arg", procFact{PID: 101, ImagePath: `C:\Program Files\Google\Chrome\Application\chrome.exe`, CmdLine: `chrome.exe --type=renderer`}, ""},
		}
		for _, tc := range cases {
			if got := tmRootClass(tc.p, cx); got != tc.want {
				t.Errorf("[%s] %s: tmRootClass = %q, want %q", form.name, tc.name, got, tc.want)
			}
		}
	}
}

func TestTMAssignGroupsAggregatesChromeTree(t *testing.T) {
	procs := []procFact{
		{PID: 100, PPID: 1, ImagePath: `C:\Program Files\Google\Chrome\Application\chrome.exe`, CmdLine: `chrome.exe --user-data-dir="D:\MJ\Data\ChromeProfile"`},
		{PID: 101, PPID: 100, ImagePath: `C:\Program Files\Google\Chrome\Application\chrome.exe`, CmdLine: `chrome.exe --type=renderer`},
		{PID: 102, PPID: 101, ImagePath: `C:\Program Files\Google\Chrome\Application\chrome.exe`, CmdLine: `chrome.exe --type=gpu-process`},
		{PID: 200, PPID: 1, CmdLine: `firefox.exe -profile "D:\MJ\Data\FirefoxProfile"`},
		{PID: 201, PPID: 200, CmdLine: `firefox.exe -contentproc`},
		{PID: 700, PPID: 1, ImagePath: `C:\Tools\hostapp.exe`}, // daemon-spawned but unclassified
		{PID: 701, PPID: 700, ImagePath: `C:\Tools\hostapp.exe`},
	}
	owned := map[uint32]string{100: "ownTree", 101: "ownTree", 102: "ownTree", 200: "ownProfile", 201: "ownTree", 700: "ownTree", 701: "ownTree"}
	want := map[uint32]string{
		100: "browser", 101: "browser", 102: "browser",
		200: "firefox-window", 201: "firefox-window",
		700: "other-hostapp.exe", 701: "other-hostapp.exe",
	}
	for _, form := range tmCtxForms {
		cx := tmTestCtxForm(form.root, form.sep)
		got := tmAssignGroups(procs, owned, cx)
		for pid, w := range want {
			if got[pid] != w {
				t.Errorf("[%s] pid %d: group = %q, want %q", form.name, pid, got[pid], w)
			}
		}
	}
}

func TestTMKillOrderChildrenFirst(t *testing.T) {
	procs := []procFact{
		{PID: 100, PPID: 1},
		{PID: 101, PPID: 100},
		{PID: 102, PPID: 101},
		{PID: 103, PPID: 100},
	}
	set := map[uint32]bool{100: true, 101: true, 102: true, 103: true}
	order := tmKillOrder(procs, set)
	pos := map[uint32]int{}
	for i, pid := range order {
		pos[pid] = i
	}
	if len(order) != 4 {
		t.Fatalf("order = %v, want 4 pids", order)
	}
	if !(pos[102] < pos[101] && pos[101] < pos[100] && pos[103] < pos[100]) {
		t.Errorf("children must precede parents, got order %v", order)
	}
}

func TestTMDescendants(t *testing.T) {
	procs := []procFact{
		{PID: 100, PPID: 1}, {PID: 101, PPID: 100}, {PID: 102, PPID: 101}, {PID: 200, PPID: 1},
	}
	set := tmDescendants(procs, 100)
	if len(set) != 3 || !set[100] || !set[101] || !set[102] || set[200] {
		t.Errorf("descendants(100) = %v, want {100,101,102}", set)
	}
}

func TestTMSamplerDelta(t *testing.T) {
	s := &tmSamplerT{last: map[tmSampleKey]tmSample{}}
	t0 := time.Unix(1700000000, 0)
	if got := s.observe(42, 1_000_000, 7, t0); got != 0 {
		t.Fatalf("first sight must read 0, got %v", got)
	}
	// Half a core-second of CPU over 1s of wall time: on any
	// machine with >=1 core the percentage is 50/NumCPU --
	// bounded (0, 50], never negative, never over 100.
	got := s.observe(42, 1_000_000+5_000_000, 7, t0.Add(time.Second))
	if got <= 0 || got > 50 {
		t.Errorf("delta CPU%% = %v, want in (0, 50]", got)
	}
	// Same PID, new creation time (PID reuse): a fresh process,
	// first sight again.
	if got := s.observe(42, 100, 8, t0.Add(2*time.Second)); got != 0 {
		t.Errorf("reused PID must read 0 on first sight, got %v", got)
	}
}
