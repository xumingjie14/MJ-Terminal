package main

// files_event_test.go -- v0.66 (detail-fix ⑦): the image
// viewer's daemon seams: the /api/files/event whitelist (the
// log-only sink the viewer reports opens/hops/failures to) and
// the imgZoom settings domain end to end (valid values, the
// sanitize fallback, the registered ownership is covered by
// the registry gate).

import "testing"

func TestFilesEventKinds(t *testing.T) {
	for _, k := range []string{"open", "nav"} {
		if filesEventKinds[k] != "info" {
			t.Errorf("event %q must log at info, got %q", k, filesEventKinds[k])
		}
	}
	if filesEventKinds["error"] != "error" {
		t.Errorf("a decode failure must log at error, got %q", filesEventKinds["error"])
	}
	for _, k := range []string{"", "loaded", "delete", "OPEN"} {
		if _, ok := filesEventKinds[k]; ok {
			t.Errorf("event %q must not be whitelisted", k)
		}
	}
}

func TestImgZoomDomain(t *testing.T) {
	if !validImgZoom("fit") || !validImgZoom("actual") {
		t.Error("fit and actual are the image viewer's zoom domain")
	}
	for _, v := range []string{"", "fill", "fit-width", "100", "FIT"} {
		if validImgZoom(v) {
			t.Errorf("%q must not be a valid imgZoom", v)
		}
	}
	if got := defaultSettings().ImgZoom; got != "fit" {
		t.Errorf("the default imgZoom must be fit, got %q", got)
	}
	s := sanitizeSettings(mjSettings{ImgZoom: "bogus"})
	if s.ImgZoom != "fit" {
		t.Errorf("sanitize must fall an unknown imgZoom back to fit, got %q", s.ImgZoom)
	}
	s = sanitizeSettings(mjSettings{ImgZoom: "actual"})
	if s.ImgZoom != "actual" {
		t.Errorf("sanitize must keep a valid imgZoom, got %q", s.ImgZoom)
	}
}
