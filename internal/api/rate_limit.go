package api

import (
	"sync"
	"time"
)

type loginAttempt struct {
	failures     int
	blockedUntil time.Time
}

type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]loginAttempt
	now      func() time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{attempts: make(map[string]loginAttempt), now: time.Now}
}

func (l *loginLimiter) retryAfter(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	attempt := l.attempts[key]
	now := l.now()
	if now.Before(attempt.blockedUntil) {
		return attempt.blockedUntil.Sub(now)
	}
	if !attempt.blockedUntil.IsZero() {
		delete(l.attempts, key)
	}
	return 0
}

func (l *loginLimiter) failed(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	attempt := l.attempts[key]
	attempt.failures++
	if attempt.failures >= 5 {
		attempt.blockedUntil = l.now().Add(time.Minute)
		attempt.failures = 0
	}
	l.attempts[key] = attempt
}

func (l *loginLimiter) succeeded(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}
