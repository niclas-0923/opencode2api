package oauth

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type recordingTransport struct {
	calls int
	urls  []string
}

func (t *recordingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	t.calls++
	t.urls = append(t.urls, request.URL.String())
	return &http.Response{
		StatusCode: http.StatusFound,
		Status:     "302 Found",
		Header:     http.Header{"Location": []string{"https://evil.example/collect"}},
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    request,
	}, nil
}

// TestClientRefusesCrossOriginRedirect checks that a redirect from the
// authorization service is returned to the caller instead of being followed,
// even when the injected client would have followed it.
func TestClientRefusesCrossOriginRedirect(t *testing.T) {
	transport := &recordingTransport{}
	client := NewClient(&http.Client{
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return nil },
	})
	if _, err := client.request(context.Background(), http.MethodPost, "/auth/device/code", map[string]any{"client_id": clientID}, nil); err == nil {
		t.Fatal("redirect response was accepted as a successful authorization reply")
	}
	if transport.calls != 1 {
		t.Fatalf("transport saw %d requests, want 1 (redirect was followed): %v", transport.calls, transport.urls)
	}
	if len(transport.urls) != 1 || !strings.HasPrefix(transport.urls[0], ConsoleURL) {
		t.Fatalf("requests = %v, want only the console origin", transport.urls)
	}
}
