package store

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"time"
)

// Memory is an isolated test double. Production always uses PostgreSQL.
type Memory struct {
	mu       sync.Mutex
	users    map[string]User
	sessions map[[32]byte]Session
	Audits   []string
}

func NewMemory() *Memory {
	return &Memory{users: make(map[string]User), sessions: make(map[[32]byte]Session)}
}
func (m *Memory) Ready(context.Context) error { return nil }
func (m *Memory) Close()                      {}
func (m *Memory) Initialized(context.Context) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.users) > 0, nil
}
func (m *Memory) CreateInitialAdmin(_ context.Context, user User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.users) > 0 {
		return ErrAlreadyInitialized
	}
	m.users[user.ID] = user
	return nil
}
func (m *Memory) UserByEmail(_ context.Context, email string) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, user := range m.users {
		if strings.EqualFold(strings.TrimSpace(user.Email), strings.TrimSpace(email)) {
			return user, nil
		}
	}
	return User{}, sql.ErrNoRows
}
func (m *Memory) CreateSession(_ context.Context, hash [32]byte, userID string, expires time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	user, ok := m.users[userID]
	if !ok {
		return sql.ErrNoRows
	}
	m.sessions[hash] = Session{User: user, ExpiresAt: expires}
	return nil
}
func (m *Memory) Session(_ context.Context, hash [32]byte) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[hash]
	if !ok || time.Now().After(session.ExpiresAt) {
		return Session{}, sql.ErrNoRows
	}
	return session, nil
}
func (m *Memory) DeleteSession(_ context.Context, hash [32]byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, hash)
	return nil
}
func (m *Memory) Audit(_ context.Context, event string, _ *string, _ string, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Audits = append(m.Audits, event)
	return nil
}
