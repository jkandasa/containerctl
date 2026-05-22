package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

const sessionCookie = "containerctl_session"

const (
	maxLoginFailures = 5
	loginBlockDur    = 30 * time.Second
)

type sessionEntry struct {
	createdAt time.Time
}

type loginAttempt struct {
	count     int
	blockedAt time.Time // zero when not blocked
}

type sessionStore struct {
	token    string
	ttl      time.Duration
	mu       sync.RWMutex
	sessions map[string]*sessionEntry
	failMu   sync.Mutex
	failures map[string]*loginAttempt // keyed by client IP
}

func newSessionStore(token string, ttl time.Duration) *sessionStore {
	return &sessionStore{
		token:    token,
		ttl:      ttl,
		sessions: make(map[string]*sessionEntry),
		failures: make(map[string]*loginAttempt),
	}
}

func (s *sessionStore) validateToken(tok string) bool {
	return subtle.ConstantTimeCompare([]byte(tok), []byte(s.token)) == 1
}

// validateLogin checks rate-limiting, validates the token, and updates the
// failure counter for ip. Returns ok=true on success. When blocked=true the
// caller should show a retry-after message; retryAfter is the remaining block
// duration. blocked can be true even on first call if the block window just
// started (5th consecutive failure).
func (s *sessionStore) validateLogin(ip, token string) (ok, blocked bool, retryAfter time.Duration) {
	s.failMu.Lock()
	defer s.failMu.Unlock()

	a := s.failures[ip]
	if a == nil {
		a = &loginAttempt{}
		s.failures[ip] = a
	}

	// Check active block window.
	if !a.blockedAt.IsZero() {
		elapsed := time.Since(a.blockedAt)
		if elapsed < loginBlockDur {
			return false, true, loginBlockDur - elapsed
		}
		// Block expired — reset counters and allow a fresh attempt.
		a.count = 0
		a.blockedAt = time.Time{}
	}

	// validateToken is pure CPU; safe to call under failMu.
	if s.validateToken(token) {
		delete(s.failures, ip)
		return true, false, 0
	}

	a.count++
	if a.count >= maxLoginFailures {
		a.blockedAt = time.Now()
		return false, true, loginBlockDur
	}
	return false, false, 0
}

func (s *sessionStore) create() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)
	s.mu.Lock()
	s.sessions[id] = &sessionEntry{createdAt: time.Now()}
	s.mu.Unlock()
	return id
}

func (s *sessionStore) valid(id string) bool {
	s.mu.RLock()
	e, ok := s.sessions[id]
	s.mu.RUnlock()
	if !ok {
		return false
	}
	if time.Since(e.createdAt) > s.ttl {
		s.mu.Lock()
		delete(s.sessions, id)
		s.mu.Unlock()
		return false
	}
	return true
}

func (s *sessionStore) delete(id string) {
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
}

func (s *sessionStore) validRequest(r *http.Request) bool {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	return s.valid(c.Value)
}

func (s *sessionStore) sessionID(r *http.Request) string {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return ""
	}
	return c.Value
}
