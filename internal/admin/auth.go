package admin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"opencode2api/internal/config"
	"opencode2api/internal/httpx"
)

const adminCookieName = "opencode2api_session"

// adminSession is a logged-in WebUI session. The map key is the SHA-256 digest
// of the cookie token, so the raw token is never stored. AuthVersion is the
// fingerprint of the password hash the session was created with: changing the
// username or password invalidates every session.
type adminSession struct {
	Username    string
	AuthVersion string
	CSRF        string
	Expires     time.Time
}

// loginWindow is a fixed-window counter for one client: Started is when the
// window opened and Count the events in it. Password checks (login, secret
// reveal and account changes) use a five-minute window, Playground requests a
// one-minute one. Failures counts consecutive failed checks and drives the
// backoff; BlockedUntil is when the client may try again. Both reset with the
// window and on success.
type loginWindow struct {
	Started      time.Time
	Count        int
	Failures     int
	BlockedUntil time.Time
}

// loginAdmission is the decision beginLoginAttempt returns.
type loginAdmission int

const (
	// loginAllowed means the caller owns a password-check slot and must
	// report the outcome through finishLoginAttempt.
	loginAllowed loginAdmission = iota
	// loginBusy means the server is already running maxLoginChecks password
	// hashes; the request changed no per-client counter.
	loginBusy
	// loginBlocked means this client must wait out its backoff or window.
	loginBlocked
)

// maxLoginChecks bounds concurrent Argon2 verifications. It is a resource
// guard, not a lockout: an over-limit request is answered 503 and does not
// consume a client's failure budget, so a burst of slow requests cannot lock
// the operator out permanently.
const maxLoginChecks = 4

// handleLogin checks the credentials against the configured username and
// password hash, creates a session and its CSRF token, and sets the session
// cookie. A successful login clears that client's failure window.
func (a *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	client := a.clientIP(r)
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeAdminJSON(w, r, &input); err != nil {
		writeAdminError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	finish, admitted := a.admitPasswordCheck(w, client)
	if !admitted {
		return
	}
	success := false
	defer func() { finish(success) }()
	cfg := a.manager.Config()
	usernameMatch := len(input.Username) == len(cfg.WebUI.Username) && subtle.ConstantTimeCompare([]byte(input.Username), []byte(cfg.WebUI.Username)) == 1
	passwordMatch := config.VerifyPassword(cfg.WebUI.PasswordHash, input.Password)
	if !usernameMatch || !passwordMatch {
		a.logger.Warn("admin login failed", "component", "auth", "event", "login_failed", "client_ip", client)
		writeAdminError(w, http.StatusUnauthorized, "invalid_credentials", "invalid username or password")
		return
	}
	success = true
	token, err := randomToken(32)
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "internal_error", "could not create session")
		return
	}
	csrf, err := randomToken(24)
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "internal_error", "could not create session")
		return
	}
	expires := time.Now().Add(time.Duration(cfg.WebUI.SessionTTLMinutes) * time.Minute)
	a.mu.Lock()
	delete(a.attempts, client)
	a.cleanupSessionsLocked(time.Now())
	if len(a.sessions) >= 2048 {
		a.removeEarliestSessionLocked()
	}
	a.sessions[tokenDigest(token)] = adminSession{Username: cfg.WebUI.Username, AuthVersion: config.Fingerprint(cfg.WebUI.PasswordHash), CSRF: csrf, Expires: expires}
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: adminCookieName, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Expires: expires, MaxAge: int(time.Until(expires).Seconds()), Secure: a.requestIsSecure(r)})
	w.Header().Set("Cache-Control", "no-store")
	a.logger.Info("admin login succeeded", "component", "auth", "event", "login_succeeded", "client_ip", client)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"username": cfg.WebUI.Username, "csrf_token": csrf, "expires_at": expires.UTC()})
}

// admitPasswordCheck reserves one password-hash slot for client and writes the
// busy or backoff response itself when the request may not proceed. The caller
// must invoke finish with the verification outcome. Login, secret reveal and
// account changes share this budget, so re-verification cannot bypass the login
// backoff.
func (a *Server) admitPasswordCheck(w http.ResponseWriter, client string) (finish func(bool), ok bool) {
	admission, retryAfter := a.beginLoginAttempt(client)
	switch admission {
	case loginBusy:
		w.Header().Set("Retry-After", "1")
		writeAdminError(w, http.StatusServiceUnavailable, "login_busy", "login is busy; retry shortly")
		return nil, false
	case loginBlocked:
		seconds := int((retryAfter + time.Second - 1) / time.Second)
		if seconds < 1 {
			seconds = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		writeAdminError(w, http.StatusTooManyRequests, "rate_limited", "too many password attempts; try again shortly")
		return nil, false
	}
	return func(success bool) { a.finishLoginAttempt(client, success) }, true
}

// handleSession reports the current session's username, CSRF token and
// expiry; it runs behind authenticate, so the session already exists.
func (a *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	session, _ := sessionFromContext(r)
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"authenticated": true, "username": session.Username, "csrf_token": session.CSRF, "expires_at": session.Expires.UTC()})
}

// handleLogout deletes the session and expires the cookie; it answers 204 even
// when the request carried no cookie.
func (a *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, _ := r.Cookie(adminCookieName)
	if cookie != nil {
		a.mu.Lock()
		delete(a.sessions, tokenDigest(cookie.Value))
		a.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: adminCookieName, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1, Secure: a.requestIsSecure(r)})
	w.WriteHeader(http.StatusNoContent)
}

type sessionContextKey struct{}

// authenticate rejects a request without a live session cookie and otherwise
// stores the session in the request context. A session dies with its expiry,
// and also as soon as the configured username or password hash changes.
func (a *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(adminCookieName)
		if err != nil || cookie.Value == "" {
			writeAdminError(w, http.StatusUnauthorized, "authentication_required", "login required")
			return
		}
		now := time.Now()
		a.mu.Lock()
		session, ok := a.sessions[tokenDigest(cookie.Value)]
		if ok && now.After(session.Expires) {
			delete(a.sessions, tokenDigest(cookie.Value))
			ok = false
		}
		a.mu.Unlock()
		if !ok {
			writeAdminError(w, http.StatusUnauthorized, "authentication_required", "session expired or invalid")
			return
		}
		cfg := a.manager.Config()
		if session.Username != cfg.WebUI.Username || session.AuthVersion != config.Fingerprint(cfg.WebUI.PasswordHash) {
			writeAdminError(w, http.StatusUnauthorized, "authentication_required", "session is no longer valid")
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), sessionContextKey{}, session))
		next.ServeHTTP(w, r)
	})
}

// sessionFromContext returns the session authenticate stored, if any.
func sessionFromContext(r *http.Request) (adminSession, bool) {
	session, ok := r.Context().Value(sessionContextKey{}).(adminSession)
	return session, ok
}

// csrf rejects a state-changing request whose X-CSRF-Token header does not
// match the session's token. A request that carries an Origin header must also
// have been sent by this server: same host and the scheme the request arrived
// on.
func (a *Server) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok := sessionFromContext(r)
		if !ok || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(session.CSRF)) != 1 {
			writeAdminError(w, http.StatusForbidden, "csrf_failed", "invalid CSRF token")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			parsed, err := url.Parse(origin)
			expectedScheme := "http"
			if a.requestIsSecure(r) {
				expectedScheme = "https"
			}
			if err != nil || !strings.EqualFold(parsed.Host, r.Host) || !strings.EqualFold(parsed.Scheme, expectedScheme) {
				writeAdminError(w, http.StatusForbidden, "origin_failed", "request origin does not match this server")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// handleAccount changes the WebUI username and/or password after verifying the
// current password, then drops every session (including this one) and clears
// the cookie, so the operator has to log in again.
func (a *Server) handleAccount(w http.ResponseWriter, r *http.Request) {
	var input struct {
		CurrentPassword string `json:"current_password"`
		Username        string `json:"username"`
		NewPassword     string `json:"new_password"`
	}
	if err := decodeAdminJSON(w, r, &input); err != nil {
		writeAdminError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	client := a.clientIP(r)
	finish, admitted := a.admitPasswordCheck(w, client)
	if !admitted {
		return
	}
	success := false
	defer func() { finish(success) }()
	cfg := a.manager.Config()
	if !config.VerifyPassword(cfg.WebUI.PasswordHash, input.CurrentPassword) {
		a.logger.Warn("admin account verification failed", "component", "auth", "event", "account_verification_failed", "client_ip", client)
		writeAdminError(w, http.StatusForbidden, "verification_failed", "current password is incorrect")
		return
	}
	success = true
	_, err := a.manager.Update(func(latest *config.Config) error {
		if !a.sessionValid(sessionID(r), *latest) || !config.VerifyPassword(latest.WebUI.PasswordHash, input.CurrentPassword) {
			return errors.New("management account changed; sign in again")
		}
		if username := strings.TrimSpace(input.Username); username != "" {
			latest.WebUI.Username = username
		}
		if input.NewPassword != "" {
			latest.WebUI.Password = input.NewPassword
		}
		return nil
	})
	if err != nil {
		// The error can pass through Normalize and persistence; redact it like
		// the other configuration-update handlers do.
		writeAdminError(w, http.StatusBadRequest, "account_update_failed", a.manager.Redact(err.Error()))
		return
	}
	a.mu.Lock()
	a.sessions = make(map[string]adminSession)
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: adminCookieName, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1, Secure: a.requestIsSecure(r)})
	a.logger.Info("admin account updated", "component", "auth", "event", "account_updated", "client_ip", client)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"updated": true, "reauthenticate": true})
}

// beginLoginAttempt reserves one password check for client and reports whether
// it may run now. The attempt is counted before verification so concurrent
// requests cannot all pass the same cap; the caller reports the outcome
// through finishLoginAttempt only when the admission was loginAllowed.
func (a *Server) beginLoginAttempt(client string) (loginAdmission, time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	if len(a.attempts) > 1024 {
		for address, candidate := range a.attempts {
			if now.Sub(candidate.Started) > 5*time.Minute {
				delete(a.attempts, address)
			}
		}
	}
	window := a.attempts[client]
	if !window.Started.IsZero() && now.Sub(window.Started) > 5*time.Minute {
		window = loginWindow{}
	}
	if !window.BlockedUntil.IsZero() && now.Before(window.BlockedUntil) {
		return loginBlocked, window.BlockedUntil.Sub(now)
	}
	if window.Count >= 5 {
		// The fixed window is exhausted. Waiting it out keeps the cap
		// bounded instead of extending a penalty indefinitely.
		return loginBlocked, window.Started.Add(5 * time.Minute).Sub(now)
	}
	if a.loginChecks >= maxLoginChecks {
		return loginBusy, 0
	}
	if _, exists := a.attempts[client]; !exists && len(a.attempts) >= 4096 {
		a.removeOldestLoginWindowLocked()
	}
	if window.Started.IsZero() {
		window.Started = now
	}
	window.Count++
	a.attempts[client] = window
	a.loginChecks++
	return loginAllowed, 0
}

// finishLoginAttempt releases the reservation and records the outcome. A
// failure sets an exponential backoff (2, 4, 8, ... seconds) capped at one
// minute; success clears the client's window.
func (a *Server) finishLoginAttempt(client string, success bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.loginChecks > 0 {
		a.loginChecks--
	}
	window, ok := a.attempts[client]
	if !ok {
		return
	}
	if success {
		delete(a.attempts, client)
		return
	}
	window.Failures++
	backoff := time.Duration(1<<min(window.Failures, 6)) * time.Second
	if backoff > time.Minute {
		backoff = time.Minute
	}
	window.BlockedUntil = time.Now().Add(backoff)
	a.attempts[client] = window
}

// removeOldestLoginWindowLocked evicts one window so inserting a new client
// cannot exceed the cap. a.mu must be held.
func (a *Server) removeOldestLoginWindowLocked() {
	var oldestAddress string
	var oldest time.Time
	for address, candidate := range a.attempts {
		if oldest.IsZero() || candidate.Started.Before(oldest) {
			oldestAddress, oldest = address, candidate.Started
		}
	}
	if !oldest.IsZero() {
		delete(a.attempts, oldestAddress)
	}
}

// cleanupSessionsLocked deletes the expired sessions; a.mu must be held.
func (a *Server) cleanupSessionsLocked(now time.Time) {
	for token, session := range a.sessions {
		if now.After(session.Expires) {
			delete(a.sessions, token)
		}
	}
}

// removeEarliestSessionLocked deletes the session that expires first, making
// room when the session map is full; a.mu must be held.
func (a *Server) removeEarliestSessionLocked() {
	var earliestToken string
	var earliest time.Time
	for token, session := range a.sessions {
		if earliestToken == "" || session.Expires.Before(earliest) {
			earliestToken, earliest = token, session.Expires
		}
	}
	if earliestToken != "" {
		delete(a.sessions, earliestToken)
	}
}

// randomToken returns length random bytes as unpadded base64url, safe for a
// cookie value and a header.
func randomToken(length int) (string, error) {
	data := make([]byte, length)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

// tokenDigest returns the hex SHA-256 of a session token, which is what the
// session map is keyed by.
func tokenDigest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func sessionID(r *http.Request) string {
	cookie, err := r.Cookie(adminCookieName)
	if err != nil || cookie.Value == "" {
		return ""
	}
	return tokenDigest(cookie.Value)
}

// sessionValid checks the current account as well as the expiry, so requests
// waiting on upstream work cannot persist changes after a password change.
func (a *Server) sessionValid(id string, cfg config.Config) bool {
	a.mu.Lock()
	session, ok := a.sessions[id]
	a.mu.Unlock()
	return ok && time.Now().Before(session.Expires) && session.Username == cfg.WebUI.Username && session.AuthVersion == config.Fingerprint(cfg.WebUI.PasswordHash)
}

// clientIP returns the address the login and Playground rate limits and the
// logs attribute a request to. It is the direct peer's address, without the
// port, unless the peer is a configured trusted proxy (webui.trusted_proxies):
// then the rightmost X-Forwarded-For entry, the address the trusted proxy
// appended for the client it saw, identifies the client. Entries the client
// sent itself are never used, so a forged header cannot rotate a rate-limit
// window through the proxy.
func (a *Server) clientIP(r *http.Request) string {
	return clientIPFor(r, a.manager.Config().WebUI.TrustedProxies)
}

// requestIsSecure reports whether the browser reached the server over TLS,
// directly or through a trusted proxy that set X-Forwarded-Proto to https.
func (a *Server) requestIsSecure(r *http.Request) bool {
	return requestIsSecureFor(r, a.manager.Config().WebUI.TrustedProxies)
}

// clientIPFor is clientIP for an explicit trusted-proxy set, so the identity
// resolution is testable without a runtime.
func clientIPFor(r *http.Request, trustedProxies []string) string {
	peer := peerHost(r.RemoteAddr)
	if !peerTrusted(peer, trustedProxies) {
		return peer
	}
	forwarded := ""
	for _, header := range r.Header.Values("X-Forwarded-For") {
		for _, entry := range strings.Split(header, ",") {
			if trimmed := strings.TrimSpace(entry); trimmed != "" {
				forwarded = trimmed
			}
		}
	}
	if forwarded != "" {
		if host := peerHost(forwarded); net.ParseIP(host) != nil {
			return host
		}
	}
	return peer
}

// peerTrusted reports whether peer appears in trustedProxies as an IP address
// or a CIDR range.
func peerTrusted(peer string, trustedProxies []string) bool {
	ip := net.ParseIP(peer)
	if ip == nil {
		return false
	}
	for _, entry := range trustedProxies {
		if _, network, err := net.ParseCIDR(entry); err == nil {
			if network.Contains(ip) {
				return true
			}
			continue
		}
		if candidate := net.ParseIP(strings.Trim(entry, "[]")); candidate != nil && candidate.Equal(ip) {
			return true
		}
	}
	return false
}

// requestClaimsHTTPS reports whether the request arrived over TLS or carried
// X-Forwarded-Proto: https. Unlike requestIsSecure it does not consult the
// trusted proxies: browsers ignore HSTS on plain HTTP responses, so the
// header does not need the stricter check that cookie flags and the CSRF
// origin scheme comparison use.
func requestClaimsHTTPS(r *http.Request) bool {
	if r == nil {
		return false
	}
	return r.TLS != nil || strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

// peerHost strips the port from a RemoteAddr or X-Forwarded-For entry.
func peerHost(address string) string {
	if host, _, err := net.SplitHostPort(address); err == nil {
		return host
	}
	return address
}

// requestIsSecureFor reports whether the browser reached the server over TLS,
// directly or, for the cookie's Secure flag and the CSRF origin check,
// through a proxy that set X-Forwarded-Proto to https. With trusted proxies
// configured, only a trusted peer's header is honored; with none configured
// the header is honored as before, for a listener that already sits behind a
// local proxy or accepts only loopback traffic.
func requestIsSecureFor(r *http.Request, trustedProxies []string) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	if !strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https") {
		return false
	}
	return len(trustedProxies) == 0 || peerTrusted(peerHost(r.RemoteAddr), trustedProxies)
}
