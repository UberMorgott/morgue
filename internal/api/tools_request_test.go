package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Install requests accept an optional exact version and reject malformed
// requests before admission (nothing is installed or mutated).
func TestHandleInstallToolVersionValidation(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"version without name", `{"version":"1.2.3"}`, http.StatusBadRequest},
		{"unknown tool", `{"name":"no-such-tool","version":"1.2.3"}`, http.StatusBadRequest},
		{"unknown tool without version", `{"name":"no-such-tool"}`, http.StatusBadRequest},
		{"malformed body", `{"name":`, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/tools/install", bytes.NewBufferString(tc.body))
			rec := httptest.NewRecorder()
			s.handleInstallTool(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d (%s), want %d", rec.Code, rec.Body.String(), tc.want)
			}
		})
	}
}
