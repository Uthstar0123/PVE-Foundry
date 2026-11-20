package store

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"time"
)

// Memory is an isolated test double. Production always uses PostgreSQL.
type approval struct {
	userID     string
	approvedAt time.Time
	expiresAt  time.Time
}

type Memory struct {
	mu          sync.Mutex
	users       map[string]User
	sessions    map[[32]byte]Session
	jobs        map[string]Job
	locks       map[string]string
	steps       map[string][]JobStep
	events      map[string][]JobEvent
	artifacts   map[string]DeploymentArtifact
	plans       map[string]string
	planCreated map[string]time.Time
	approvals   map[string]approval
	nextEventID int64
	Audits      []string
	AuditEvents []AuditEvent
}

func NewMemory() *Memory {
	return &Memory{
		users: make(map[string]User), sessions: make(map[[32]byte]Session),
		jobs: make(map[string]Job), locks: make(map[string]string),
		steps: make(map[string][]JobStep), events: make(map[string][]JobEvent),
		artifacts: make(map[string]DeploymentArtifact), plans: make(map[string]string),
		planCreated: make(map[string]time.Time), approvals: make(map[string]approval),
	}
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
func (m *Memory) Audit(_ context.Context, event string, userID *string, sourceIP, detail string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Audits = append(m.Audits, event)
	m.AuditEvents = append(m.AuditEvents, AuditEvent{ID: int64(len(m.AuditEvents) + 1), EventType: event, UserID: userID, SourceIP: sourceIP, Detail: detail, CreatedAt: time.Now().UTC()})
	return nil
}
