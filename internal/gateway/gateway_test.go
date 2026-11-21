package gateway

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opencode2api/internal/config"
)

// testGateway builds a Gateway with enough state for the handler tests. No
// upstream is configured; the tests stop before any network work.
func testGateway(t *testing.T, serverKeys ...string) *Gateway {
	t.Helper()
	cfg := config.Config{
		ServerKeys: serverKeys,
		Models:     config.ModelsConfig{RefreshSeconds: 300},
	}
	g, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return g
}

// TestAuthenticateCredentials checks the four header combinations and the
// ambiguity rule: two different keys in one request are refused instead of
// letting either header win.
func TestAuthenticateCredentials(t *testing.T) {
	g := &Gateway{cfg: config.Config{ServerKeys: []string{"sk-good"}}}
	handler := g.authenticate(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	cases := []struct {
		name       string
		headers    map[string]string
		wantStatus int
		wantCode   string
	}{
		{"no credentials", nil, http.StatusUnauthorized, "authentication_error"},
		{"wrong key", map[string]string{"x-api-key": "sk-bad"}, http.StatusUnauthorized, "authentication_error"},
		{"x-api-key only", map[string]string{"x-api-key": "sk-good"}, http.StatusNoContent, ""},
		{"bearer only", map[string]string{"Authorization": "Bearer sk-good"}, http.StatusNoContent, ""},
		{"both agree", map[string]string{"x-api-key": "sk-good", "Authorization": "Bearer sk-good"}, http.StatusNoContent, ""},
		{"both disagree", map[string]string{"x-api-key": "sk-good", "Authorization": "Bearer sk-other"}, http.StatusUnauthorized, "ambiguous_credentials"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
			for name, value := range tc.headers {
				r.Header.Set(name, value)
			}
			w := httptest.NewRecorder()
			handler(w, r)
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", w.Code, tc.wantStatus, w.Body.String())
			}
			if tc.wantCode != "" {
				var body struct {
					Error struct {
						Type string `json:"type"`
					} `json:"error"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					t.Fatalf("decode error body: %v", err)
				}
				if body.Error.Type != tc.wantCode {
					t.Fatalf("error type = %q, want %q", body.Error.Type, tc.wantCode)
				}
			}
		})
	}
}

// TestHealthHidesTopology checks that /healthz without a valid server key
// carries only status and readiness, while a keyed request still gets the full
// document.
func TestHealthHidesTopology(t *testing.T) {
	g := testGateway(t, "sk-good")
	handler := g.Handler()

	summary := httptest.NewRecorder()
	handler.ServeHTTP(summary, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	var summaryBody map[string]any
	if err := json.Unmarshal(summary.Body.Bytes(), &summaryBody); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if len(summaryBody) != 2 || summaryBody["status"] == nil || summaryBody["ready"] == nil {
		t.Fatalf("unauthenticated health leaked fields: %v", summaryBody)
	}
	if got := summary.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("health Cache-Control = %q, want no-store", got)
	}

	full := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("x-api-key", "sk-good")
	handler.ServeHTTP(full, req)
	var fullBody map[string]any
	if err := json.Unmarshal(full.Body.Bytes(), &fullBody); err != nil {
		t.Fatalf("decode full health: %v", err)
	}
	for _, field := range []string{"status", "ready", "version", "models", "keys", "proxies"} {
		if _, ok := fullBody[field]; !ok {
			t.Fatalf("authenticated health missing %q: %v", field, fullBody)
		}
	}
}

// TestInflightGateRejectsWhenFull checks that a full gate answers 429 with a
// Retry-After header and releases the slot afterwards.
func TestInflightGateRejectsWhenFull(t *testing.T) {
	g := &Gateway{inflight: make(chan struct{}, 1)}
	g.inflight <- struct{}{}
	handler := g.limitInflight(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })

	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("{}"))
	w := httptest.NewRecorder()
	handler(w, r)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("429 response is missing Retry-After")
	}
	<-g.inflight

	passed := httptest.NewRecorder()
	handler(passed, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("{}")))
	if passed.Code != http.StatusNoContent {
		t.Fatalf("status after release = %d, want 204", passed.Code)
	}
}

// TestMaxBodyBytesSelection checks the per-route body caps and the fallback
// when a Gateway was built without configured limits.
func TestMaxBodyBytesSelection(t *testing.T) {
	g := &Gateway{maxRequestBytes: 1024, maxAttachmentBytes: 4096}
	if got := g.maxBodyBytes("/v1/chat/completions"); got != 1024 {
		t.Fatalf("chat limit = %d, want 1024", got)
	}
	if got := g.maxBodyBytes("/v1/responses"); got != 4096 {
		t.Fatalf("responses limit = %d, want 4096", got)
	}
	empty := &Gateway{}
	if got := empty.maxBodyBytes("/v1/responses"); got != defaultMaxAttachmentBytes {
		t.Fatalf("default attachment limit = %d, want %d", got, defaultMaxAttachmentBytes)
	}
	if got := empty.maxBodyBytes("/v1/chat/completions"); got != defaultMaxRequestBody {
		t.Fatalf("default request limit = %d, want %d", got, defaultMaxRequestBody)
	}
}

// TestWriteBodyErrorStatus checks that an oversized body is reported as 413
// and an unreadable one as 400.
func TestWriteBodyErrorStatus(t *testing.T) {
	w := httptest.NewRecorder()
	writeBodyError(w, "chat", &http.MaxBytesError{Limit: 10})
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body status = %d, want 413", w.Code)
	}
	w = httptest.NewRecorder()
	writeBodyError(w, "chat", io.ErrUnexpectedEOF)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unreadable body status = %d, want 400", w.Code)
	}
}
