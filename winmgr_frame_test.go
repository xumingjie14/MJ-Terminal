package main

// v0.54: the isolated-Chrome frame eligibility verdict
// (winmgr.go). The DWM calls themselves are platform code; what
// the tests pin is WHICH windows may be framed -- above all that
// the desktop view and anything in its process family (the shell
// is usually Edge, the same image as a fallback isolated
// browser) are never touched.

import "testing"

func TestChromeFrameEligible(t *testing.T) {
	base := chromeFrameFacts{Browser: true, ImageBase: "chrome.exe", IsolatedBase: "chrome.exe"}
	cases := []struct {
		name string
		f    chromeFrameFacts
		want bool
	}{
		{"隔离 Chrome 的窗口→套框", base, true},
		{"隔离浏览器是 Edge 时其窗口→套框", chromeFrameFacts{Browser: true, ImageBase: "msedge.exe", IsolatedBase: "msedge.exe"}, true},
		{"桌面视图本身→永不", chromeFrameFacts{Browser: true, Shell: true, ImageBase: "msedge.exe", IsolatedBase: "msedge.exe"}, false},
		{"视图进程家族的窗口（同为 Edge 镜像）→永不", chromeFrameFacts{Browser: true, ShellPID: true, ImageBase: "msedge.exe", IsolatedBase: "msedge.exe"}, false},
		{"不是守护进程启动的浏览器窗口→不套", chromeFrameFacts{Browser: false, ImageBase: "chrome.exe", IsolatedBase: "chrome.exe"}, false},
		{"镜像与隔离浏览器对不上（宿主自己的另一个浏览器）→不套", chromeFrameFacts{Browser: true, ImageBase: "msedge.exe", IsolatedBase: "chrome.exe"}, false},
		{"隔离浏览器未知（没装 Chrome/Edge）→不套", chromeFrameFacts{Browser: true, ImageBase: "chrome.exe", IsolatedBase: ""}, false},
		{"普通应用窗口（商店/组件）→不套", chromeFrameFacts{Browser: false, ImageBase: "telegram.exe", IsolatedBase: "chrome.exe"}, false},
	}
	for _, c := range cases {
		if got := chromeFrameEligible(c.f); got != c.want {
			t.Errorf("%s: chromeFrameEligible(%+v) = %v, want %v", c.name, c.f, got, c.want)
		}
	}
}
