package main

// v0.59 block 3 contract tests: the codegen is deterministic,
// and the committed web/src/api-types.ts is exactly what the
// generator produces from the current Go structs (the same
// freshness rule the CI verify job enforces with git diff).

import (
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestAPIGenDeterministic(t *testing.T) {
	a, b := genAPITypes(), genAPITypes()
	if a != b {
		t.Fatal("genAPITypes is not deterministic")
	}
	for _, want := range []string{
		"export interface SettingsResponse",
		"export interface BusEvent",
		"export interface FilesListResponse",
		"export interface APIErrorEnvelope",
	} {
		if !strings.Contains(a, want) {
			t.Fatalf("generated contract is missing %s", want)
		}
	}
	// The flattened per-app settings keys must NOT surface as
	// object fields (mjapi:"-" exclusion).
	if strings.Contains(a, "notify_allow_app?:") {
		t.Fatal("per-app notify maps leaked into the API contract")
	}
}

func TestAPIContractFresh(t *testing.T) {
	committed, err := os.ReadFile("web/src/api-types.ts")
	if err != nil {
		t.Fatalf("read committed contract: %v", err)
	}
	if string(committed) != genAPITypes() {
		t.Fatal("web/src/api-types.ts is stale -- regenerate with `npm run api:gen`")
	}
}

// TestAPIErrorEnvelopeShape pins the wire shape of the uniform
// error envelope and the legacy field the page normalizes on.
func TestAPIErrorEnvelopeShape(t *testing.T) {
	rec := httptest.NewRecorder()
	writeAPIError(rec, 400, errCodeBadRequest, "测试错误")
	body := rec.Body.String()
	if !strings.Contains(body, `"error":{"code":"bad_request","message":"测试错误"}`) {
		t.Fatalf("envelope shape drifted: %s", body)
	}
	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
