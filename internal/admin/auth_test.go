package admin

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

// request builds a request whose RemoteAddr and headers the caller controls,
// the way a peer or a reverse proxy would present them.
func request(t *testing.T, remoteAddr string, headers map[string]string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	r.RemoteAddr = remoteAddr
	for name := range r.Header {
		delete(r.Header, name)
	}
	for name, value := range headers {
		r.Header.Set(name, value)
	}
	return r
}

func TestClientIPFor(t *testing.T) {
	proxy := []string{"127.0.0.1"}
	cases := []struct {
		name       string
		remoteAddr string
		headers    map[string]string
		trusted    []string
		want       string
	}{
		{"direct peer", "203.0.113.9:5555", nil, proxy, "203.0.113.9"},
		{"trusted proxy forwards client", "127.0.0.1:60000", map[string]string{"X-Forwarded-For": "198.51.100.7"}, proxy, "198.51.100.7"},
		{"forged prefix ignored, rightmost wins", "127.0.0.1:60000", map[string]string{"X-Forwarded-For": "1.1.1.1, 203.0.113.9"}, proxy, "203.0.113.9"},
		{"invalid forwarded falls back to peer", "127.0.0.1:60000", map[string]string{"X-Forwarded-For": "not-an-ip"}, proxy, "127.0.0.1"},
		{"no header falls back to peer", "127.0.0.1:60000", nil, proxy, "127.0.0.1"},
		{"cidr trust matches peer", "127.0.0.2:60000", map[string]string{"X-Forwarded-For": "198.51.100.7"}, []string{"127.0.0.0/8"}, "198.51.100.7"},
		{"cidr trust rejects peer", "203.0.113.9:5555", map[string]string{"X-Forwarded-For": "198.51.100.7"}, []string{"10.0.0.0/8"}, "203.0.113.9"},
		{"forwarded with port", "127.0.0.1:60000", map[string]string{"X-Forwarded-For": "198.51.100.7:4444"}, proxy, "198.51.100.7"},
		{"empty proxy list ignores header", "127.0.0.1:60000", map[string]string{"X-Forwarded-For": "198.51.100.7"}, nil, "127.0.0.1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := request(t, tc.remoteAddr, tc.headers)
			if got := clientIPFor(r, tc.trusted); got != tc.want {
				t.Fatalf("clientIPFor = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRequestIsSecureFor(t *testing.T) {
	proxy := []string{"127.0.0.1"}
	if !requestIsSecureFor(request(t, "203.0.113.9:5555", map[string]string{"X-Forwarded-Proto": "https"}), nil) {
		t.Fatal("no trusted proxies configured: forwarded https is honored as before")
	}
	if !requestIsSecureFor(request(t, "127.0.0.1:60000", map[string]string{"X-Forwarded-Proto": "https"}), proxy) {
		t.Fatal("trusted proxy: forwarded https is honored")
	}
	if requestIsSecureFor(request(t, "203.0.113.9:5555", map[string]string{"X-Forwarded-Proto": "https"}), proxy) {
		t.Fatal("untrusted peer must not be able to claim https")
	}
	if requestIsSecureFor(request(t, "127.0.0.1:60000", nil), proxy) {
		t.Fatal("plain request without the header is not secure")
	}
	r := request(t, "203.0.113.9:5555", nil)
	r.TLS = &tls.ConnectionState{}
	if !requestIsSecureFor(r, proxy) {
		t.Fatal("direct TLS is secure")
	}
}
