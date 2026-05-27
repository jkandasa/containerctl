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

	// Brute force protection
	failMu         sync.Mutex
	failures       map[string]*loginAttempt // keyed by client IP
	globalFailures []time.Time              // recent failed attempts (any IP)
}

func newSessionStore(token string, ttl time.Duration) *sessionStore {
	return &sessionStore{
		token:          token,
		ttl:            ttl,
		sessions:       make(map[string]*sessionEntry),
		failures:       make(map[string]*loginAttempt),
		globalFailures: make([]time.Time, 0, 64),
	}
}

func (s *sessionStore) validateToken(tok string) bool {
	return subtle.ConstantTimeCompare([]byte(tok), []byte(s.token)) == 1
}

// validateLogin checks rate-limiting, validates the token, and updates the
// failure counter for ip. Returns ok=true on success. When blocked=true the
// caller should show a retry-after message; retryAfter is the remaining block
// duration.
func (s *sessionStore) validateLogin(ip, token string) (ok, blocked bool, retryAfter time.Duration) {
	s.failMu.Lock()
	defer s.failMu.Unlock()

	now := time.Now()

	// Global rate limiting: prune old failures (last 10 minutes)
	cutoff := now.Add(-10 * time.Minute)
	filtered := s.globalFailures[:0]
	for _, t := range s.globalFailures {
		if t.After(cutoff) {
			filtered = append(filtered, t)
		}
	}
	s.globalFailures = filtered

	// If we have had many failures globally recently, slow everyone down
	const globalThreshold = 25
	const globalMinDelay = 3 * time.Second

	if len(s.globalFailures) >= globalThreshold {
		// Enforce a minimum delay between any login attempts
		last := s.globalFailures[len(s.globalFailures)-1]
		if now.Sub(last) < globalMinDelay {
			return false, true, globalMinDelay - now.Sub(last)
		}
	}

	a := s.failures[ip]
	if a == nil {
		a = &loginAttempt{}
		s.failures[ip] = a
	}

	// Check active block window for this IP.
	if !a.blockedAt.IsZero() {
		elapsed := time.Since(a.blockedAt)
		if elapsed < loginBlockDur {
			return false, true, loginBlockDur - elapsed
		}
		a.count = 0
		a.blockedAt = time.Time{}
	}

	if s.validateToken(token) {
		delete(s.failures, ip)
		return true, false, 0
	}

	// Record this failure globally
	s.globalFailures = append(s.globalFailures, now)

	a.count++
	if a.count >= maxLoginFailures {
		a.blockedAt = now
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

// TTL returns the configured session lifetime.
func (s *sessionStore) TTL() time.Duration {
	return s.ttl
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
