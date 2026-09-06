package sftpadmin

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	sessionCookieName = "sftp_admin_session"
	// csrfField is the hidden form field carrying the per-session token.
	csrfField = "csrf_token"
	// sessionTTL bounds every authenticated session to 30 minutes.
	sessionTTL = 30 * time.Minute
	// maxLoginAttempts caps failed logins per IP inside loginWindow;
	// further attempts get 429 until the window slides past.
	maxLoginAttempts = 5
	loginWindow      = time.Minute
)

// session is an authenticated browser session with its CSRF token.
type session struct {
	csrf    string
	expires time.Time
}

// authState holds sessions and login-attempt counters. All methods are
// safe for concurrent use.
type authState struct {
	mu       sync.Mutex
	pwHash   [32]byte
	sessions map[string]session
	failures map[string][]time.Time
	ttl      time.Duration
}

// newAuthState hashes the admin password once; later comparisons run in
// constant time over fixed-size digests, leaking neither value nor length.
func newAuthState(password string) *authState {
	return &authState{
		pwHash:   sha256.Sum256([]byte(password)),
		sessions: map[string]session{},
		failures: map[string][]time.Time{},
		ttl:      sessionTTL,
	}
}

// checkPassword compares the candidate against the configured password in
// constant time. An empty candidate never authenticates, even when the
// configured password is itself empty: fail closed, never empty == empty.
func (a *authState) checkPassword(got string) bool {
	if got == "" {
		return false
	}
	sum := sha256.Sum256([]byte(got))
	return subtle.ConstantTimeCompare(sum[:], a.pwHash[:]) == 1
}

// checkPasswordAgainst compares a candidate against an explicit password in
// constant time, for secrets resolved outside authState. Either side empty
// fails closed: an unset configured secret must never accept a login.
func checkPasswordAgainst(got, want string) bool {
	if got == "" || want == "" {
		return false
	}
	a, b := sha256.Sum256([]byte(got)), sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}

// loginAllowed reports whether ip may attempt another login.
func (a *authState) loginAllowed(ip string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.recentFailuresLocked(ip)) < maxLoginAttempts
}

// recordFailure notes a failed login from ip.
func (a *authState) recordFailure(ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.failures[ip] = append(a.recentFailuresLocked(ip), time.Now())
}

// recordSuccess clears the failure counter for ip after a good login.
func (a *authState) recordSuccess(ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.failures, ip)
}

// recentFailuresLocked prunes entries older than the window. Callers must
// hold a.mu.
func (a *authState) recentFailuresLocked(ip string) []time.Time {
	cutoff := time.Now().Add(-loginWindow)
	kept := a.failures[ip][:0]
	for _, t := range a.failures[ip] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(a.failures, ip)
		return nil
	}
	a.failures[ip] = kept
	return kept
}

// createSession mints a random opaque session id plus its CSRF token.
func (a *authState) createSession() (id, csrf string, err error) {
	id, err = newToken()
	if err != nil {
		return "", "", err
	}
	csrf, err = newToken()
	if err != nil {
		return "", "", err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sessions[id] = session{csrf: csrf, expires: time.Now().Add(a.ttl)}
	return id, csrf, nil
}

// lookup resolves a session cookie, expiring stale sessions eagerly.
func (a *authState) lookup(id string) (session, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.sessions[id]
	if !ok {
		return session{}, false
	}
	if time.Now().After(s.expires) {
		delete(a.sessions, id)
		return session{}, false
	}
	return s, true
}

// destroy ends a session immediately (logout).
func (a *authState) destroy(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.sessions, id)
}

// newToken returns 32 random bytes as unpadded base64url. crypto/rand never
// fails on Linux without already breaking the process; on the theoretical
// error path callers receive a shorter-lived fallback is avoided by panicking
// loudly instead of issuing a predictable session.
func newToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// clientIP extracts the remote IP for rate limiting.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// setSessionCookie issues the opaque session cookie: HttpOnly so page JS
// can never read it, SameSite-protected, scoped to 30 minutes.
func setSessionCookie(w http.ResponseWriter, id string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
}

// clearSessionCookie ends the browser side of a session.
func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

// checkCSRF compares the submitted token with the session token in
// constant time over fixed-size digests.
func checkCSRF(sess session, got string) bool {
	want := sha256.Sum256([]byte(sess.csrf))
	have := sha256.Sum256([]byte(got))
	return subtle.ConstantTimeCompare(want[:], have[:]) == 1
}
