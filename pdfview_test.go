package main

// pdfview_test.go -- v0.65 (detail-fix ⑥): the built-in PDF
// viewer's daemon seams: header sniffing, path validation, the
// event whitelist, the settings domain, and the vendored asset
// store (verbatim text + base64-decoded binaries).

import (
	"strings"
	"testing"
)

func TestPDFSniffHeader(t *testing.T) {
	if !pdfSniffHeader([]byte("%PDF-1.7\n...")) {
		t.Error("a plain header must pass")
	}
	// The spec tolerates leading junk before the signature.
	junk := append([]byte(strings.Repeat("x", 900)), []byte("%PDF-2.0")...)
	if !pdfSniffHeader(junk) {
		t.Error("a header within the first 1024 bytes must pass")
	}
	// ...but not beyond them.
	far := append([]byte(strings.Repeat("x", 2000)), []byte("%PDF-2.0")...)
	if pdfSniffHeader(far) {
		t.Error("a signature past the first 1024 bytes must not pass")
	}
	if pdfSniffHeader([]byte("PK\x03\x04 a zip renamed to .pdf")) {
		t.Error("zip bytes must not pass")
	}
	if pdfSniffHeader(nil) || pdfSniffHeader([]byte("%PD")) {
		t.Error("empty/truncated input must not pass")
	}
}

func TestPDFValidatePath(t *testing.T) {
	if _, _, fail := pdfValidatePath("notes.txt"); fail == "" {
		t.Error("a non-.pdf name must be refused")
	}
	if _, _, fail := pdfValidatePath("../escape.pdf"); fail == "" {
		t.Error("a path escaping Data/files must be refused")
	}
	if _, _, fail := pdfValidatePath(""); fail == "" {
		t.Error("an empty path must be refused")
	}
	// A well-formed name passes validation (existence is the
	// open handler's own check, not this seam's).
	abs, name, fail := pdfValidatePath("sub/报告.PDF")
	if fail != "" || name != "报告.PDF" || !strings.HasSuffix(abs, "报告.PDF") {
		t.Errorf("a legit nested .PDF path must pass: abs=%q name=%q fail=%q", abs, name, fail)
	}
}

func TestPDFEventKinds(t *testing.T) {
	for _, k := range []string{"loaded", "password", "password-failed", "corrupt", "error"} {
		if _, ok := pdfEventKinds[k]; !ok {
			t.Errorf("event %q must be whitelisted", k)
		}
	}
	if _, ok := pdfEventKinds["rm -rf"]; ok {
		t.Error("arbitrary event strings must not be whitelisted")
	}
	if pdfEventKinds["corrupt"] != "error" || pdfEventKinds["loaded"] != "info" {
		t.Error("event levels: corrupt=error, loaded=info")
	}
}

func TestPDFZoomDomain(t *testing.T) {
	for _, v := range []string{"fit-width", "fit-page", "actual"} {
		if !validPDFZoom(v) {
			t.Errorf("%q must be a valid pdfZoom", v)
		}
	}
	for _, v := range []string{"", "fill", "100", "FIT-WIDTH"} {
		if validPDFZoom(v) {
			t.Errorf("%q must not be a valid pdfZoom", v)
		}
	}
	if got := defaultSettings().PDFZoom; got != "fit-width" {
		t.Errorf("default pdfZoom = %q, want fit-width", got)
	}
	s := sanitizeSettings(mjSettings{PDFZoom: "bogus"})
	if s.PDFZoom != "fit-width" {
		t.Errorf("sanitize must restore the default, got %q", s.PDFZoom)
	}
}

func TestPDFAssetBytes(t *testing.T) {
	// Verbatim text assets.
	lib, ok := pdfAssetBytes("pdf.mjs")
	if !ok || len(lib) < 100_000 {
		t.Fatalf("pdf.mjs must be vendored (ok=%v, %d bytes)", ok, len(lib))
	}
	worker, ok := pdfAssetBytes("pdf.worker.mjs")
	if !ok || len(worker) < 500_000 {
		t.Fatalf("pdf.worker.mjs must be vendored (ok=%v, %d bytes)", ok, len(worker))
	}
	// A base64 binary asset: the Adobe-GB1 character map must
	// decode to real bcmap bytes (a binary cmap starts with its
	// %!PS-AdobeFont header comment in most Adobe maps, or raw
	// cmap records -- assert only that it decoded non-trivially
	// and is NOT base64 text anymore).
	cm, ok := pdfAssetBytes("cmaps/Adobe-GB1-UCS2.bcmap")
	if !ok || len(cm) < 1000 {
		t.Fatalf("a cmap must decode (ok=%v, %d bytes)", ok, len(cm))
	}
	if strings.Contains(string(cm[:64]), "QUJD") {
		t.Error("cmap still looks like base64 -- decode did not happen")
	}
	// The decode cache serves the same bytes twice.
	cm2, ok := pdfAssetBytes("cmaps/Adobe-GB1-UCS2.bcmap")
	if !ok || len(cm2) != len(cm) {
		t.Error("cached cmap must serve identical length")
	}
	// Fonts too.
	font, ok := pdfAssetBytes("standard_fonts/LiberationSans-Regular.ttf")
	if !ok || len(font) < 10_000 {
		t.Fatalf("a standard font must decode (ok=%v, %d bytes)", ok, len(font))
	}
	// Missing assets stay missing.
	if _, ok := pdfAssetBytes("cmaps/No-Such-Map.bcmap"); ok {
		t.Error("an unknown cmap must not resolve")
	}
	if _, ok := pdfAssetBytes("../settings.go"); ok {
		t.Error("a traversal name must not resolve")
	}
}

func TestPDFAssetContentType(t *testing.T) {
	if got := pdfAssetContentType("pdf.worker.mjs"); !strings.HasPrefix(got, "text/javascript") {
		t.Errorf("worker MIME = %q, want javascript", got)
	}
	if got := pdfAssetContentType("standard_fonts/LiberationSans-Regular.ttf"); got != "font/ttf" {
		t.Errorf("ttf MIME = %q", got)
	}
	if got := pdfAssetContentType("cmaps/GB-EUC-H.bcmap"); got != "application/octet-stream" {
		t.Errorf("bcmap MIME = %q", got)
	}
}

func TestPDFLogName(t *testing.T) {
	if got := pdfLogName("a/b/c.pdf"); got != "c.pdf" {
		t.Errorf("base name: got %q", got)
	}
	if got := pdfLogName(""); got == "" {
		t.Error("an empty path still logs a placeholder")
	}
	long := pdfLogName(strings.Repeat("长", 100) + ".pdf")
	if len(long) > 90 {
		t.Errorf("log name must be capped, got %d bytes", len(long))
	}
}
