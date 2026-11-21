package admin

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// loginServer builds a Server with the state the login admission helpers use.
func loginServer() *Server {
	return &Server{attempts: make(map[string]loginWindow)}
}

// TestLoginBusyDoesNotConsumeBudget checks that the resource guard rejects
// rather than locking a client out: a busy server must not count an attempt or
// start a backoff.
func TestLoginBusyDoesNotConsumeBudget(t *testing.T) {
	s := loginServer()
	s.loginChecks = maxLoginChecks
	admission, _ := s.beginLoginAttempt("203.0.113.9")
	if admission != loginBusy {
		t.Fatalf("admission = %v, want loginBusy", admission)
	}
	if len(s.attempts) != 0 || s.loginChecks != maxLoginChecks {
		t.Fatalf("busy admission changed state: attempts=%d checks=%d", len(s.attempts), s.loginChecks)
	}
}

// TestLoginBackoffGrows checks the exponential backoff and its one-minute cap.
func TestLoginBackoffGrows(t *testing.T) {
	s := loginServer()
	admission, _ := s.beginLoginAttempt("203.0.113.9")
	if admission != loginAllowed {
		t.Fatalf("admission = %v, want loginAllowed", admission)
	}
	s.finishLoginAttempt("203.0.113.9", false)
	window := s.attempts["203.0.113.9"]
	if window.Failures != 1 {
		t.Fatalf("failures = %d, want 1", window.Failures)
	}
	if delay := time.Until(window.BlockedUntil); delay < time.Second || delay > 3*time.Second {
		t.Fatalf("first backoff = %v, want about 2s", delay)
	}
	s.attempts["203.0.113.9"] = loginWindow{Started: time.Now(), Count: 1, Failures: 6}
	s.finishLoginAttempt("203.0.113.9", false) // was not admitted, but finish is defensive
	window = s.attempts["203.0.113.9"]
	if window.Failures != 7 {
		t.Fatalf("failures = %d, want 7", window.Failures)
	}
	if delay := time.Until(window.BlockedUntil); delay < 50*time.Second || delay > 61*time.Second {
		t.Fatalf("capped backoff = %v, want about 60s", delay)
	}
}

// TestLoginBlockedAndExpiredWindow checks that a client in backoff is refused
// and that an expired window starts over.
func TestLoginBlockedAndExpiredWindow(t *testing.T) {
	s := loginServer()
	s.attempts["203.0.113.9"] = loginWindow{Started: time.Now(), Count: 1, Failures: 1, BlockedUntil: time.Now().Add(10 * time.Second)}
	if admission, retry := s.beginLoginAttempt("203.0.113.9"); admission != loginBlocked || retry <= 0 {
		t.Fatalf("admission = %v retry = %v, want loginBlocked with a positive delay", admission, retry)
	}
	s.attempts["203.0.113.9"] = loginWindow{Started: time.Now().Add(-10 * time.Minute), Count: 5, Failures: 5, BlockedUntil: time.Now().Add(-time.Minute)}
	admission, _ := s.beginLoginAttempt("203.0.113.9")
	if admission != loginAllowed {
		t.Fatalf("admission = %v, want loginAllowed after the window expired", admission)
	}
	if window := s.attempts["203.0.113.9"]; window.Count != 1 || window.Failures != 0 {
		t.Fatalf("window after reset = %+v, want Count 1 and no failures", window)
	}
}

// TestLoginSuccessClearsWindow checks that a successful login releases the
// slot and forgets the client's failures.
func TestLoginSuccessClearsWindow(t *testing.T) {
	s := loginServer()
	if admission, _ := s.beginLoginAttempt("203.0.113.9"); admission != loginAllowed {
		t.Fatalf("admission = %v, want loginAllowed", admission)
	}
	s.finishLoginAttempt("203.0.113.9", true)
	if len(s.attempts) != 0 || s.loginChecks != 0 {
		t.Fatalf("after success: attempts=%d checks=%d, want 0 and 0", len(s.attempts), s.loginChecks)
	}
}

// TestAdmitPasswordCheckWritesAdmissionErrors checks that login, reveal and
// account changes share the same busy and backoff responses.
func TestAdmitPasswordCheckWritesAdmissionErrors(t *testing.T) {
	s := loginServer()
	s.loginChecks = maxLoginChecks
	recorder := httptest.NewRecorder()
	if _, ok := s.admitPasswordCheck(recorder, "203.0.113.9"); ok {
		t.Fatal("busy server admitted a password check")
	}
	if recorder.Code != http.StatusServiceUnavailable || recorder.Header().Get("Retry-After") != "1" {
		t.Fatalf("busy response = %d Retry-After %q, want 503 and 1", recorder.Code, recorder.Header().Get("Retry-After"))
	}
	s.loginChecks = 0
	s.attempts["203.0.113.9"] = loginWindow{Started: time.Now(), Count: 1, Failures: 1, BlockedUntil: time.Now().Add(10 * time.Second)}
	recorder = httptest.NewRecorder()
	if _, ok := s.admitPasswordCheck(recorder, "203.0.113.9"); ok {
		t.Fatal("blocked client was admitted")
	}
	if recorder.Code != http.StatusTooManyRequests || recorder.Header().Get("Retry-After") == "" {
		t.Fatalf("blocked response = %d Retry-After %q, want 429 with a delay", recorder.Code, recorder.Header().Get("Retry-After"))
	}
}

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

func TestRequestClaimsHTTPS(t *testing.T) {
	if !requestClaimsHTTPS(request(t, "203.0.113.9:5555", map[string]string{"X-Forwarded-Proto": "https"})) {
		t.Fatal("claimed https is enough for the HSTS header")
	}
	if requestClaimsHTTPS(request(t, "203.0.113.9:5555", nil)) {
		t.Fatal("plain request does not claim https")
	}
	if requestClaimsHTTPS(nil) {
		t.Fatal("nil request does not claim https")
	}
	r := request(t, "203.0.113.9:5555", nil)
	r.TLS = &tls.ConnectionState{}
	if !requestClaimsHTTPS(r) {
		t.Fatal("direct TLS claims https")
	}
}
