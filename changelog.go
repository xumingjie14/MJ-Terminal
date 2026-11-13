package main

// changelog.go -- v0.71 block 3: the in-app changelog
// （设置 → 软件更新 → 更新日志）.
//
// The data is a structured file embedded in the binary
// (assets/changelog.json); this handler is its only reader
// and the Settings page its only consumer. Adding a release
// note for a future version means editing THAT FILE ONLY --
// newest entry first, user-facing wording (see the file's
// own _comment for the wording rules). The parse happens
// once; a malformed file degrades to an empty list and a
// console error instead of breaking the update page.

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"sync"
)

//go:embed assets/changelog.json
var changelogRaw []byte

var changelogOnce = struct {
	sync.Once
	resp changelogResponse
}{}

// changelogData parses the embedded changelog once and
// returns it with the running version stamped in.
func changelogData() changelogResponse {
	changelogOnce.Do(func() {
		var file struct {
			Entries []changelogEntry `json:"entries"`
		}
		if err := json.Unmarshal(changelogRaw, &file); err != nil {
			logf("update", "error", "更新日志数据解析失败：%v", err)
			file.Entries = nil
		}
		changelogOnce.resp = changelogResponse{Current: AppVersion, Entries: file.Entries}
		if changelogOnce.resp.Entries == nil {
			changelogOnce.resp.Entries = []changelogEntry{}
		}
	})
	return changelogOnce.resp
}

// changelogHandler serves GET /api/changelog.
func changelogHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, errCodeMethod, "method not allowed")
		return
	}
	writeJSON(w, changelogData())
}
