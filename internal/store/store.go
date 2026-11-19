package store

import (
	"context"
	"errors"
	"time"
)

var ErrAlreadyInitialized = errors.New("platform is already initialized")

type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	DisplayName  string    `json:"displayName"`
	Role         string    `json:"role"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"createdAt"`
}

type Session struct {
	User      User
	ExpiresAt time.Time
}

type Store interface {
	JobStore
	Ready(context.Context) error
	Initialized(context.Context) (bool, error)
	CreateInitialAdmin(context.Context, User) error
	UserByEmail(context.Context, string) (User, error)
	CreateSession(context.Context, [32]byte, string, time.Time) error
	Session(context.Context, [32]byte) (Session, error)
	DeleteSession(context.Context, [32]byte) error
	Audit(context.Context, string, *string, string, string) error
	Close()
}
