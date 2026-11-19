package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"proxmox-orchestrator/migrations"
)

type Postgres struct {
	db *sql.DB
}

func OpenPostgres(ctx context.Context, databaseURL string) (*Postgres, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	store := &Postgres{db: db}
	if err := store.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (p *Postgres) migrate(ctx context.Context) error {
	if _, err := p.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version integer PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("create migration metadata: %w", err)
	}
	entries, err := fs.ReadDir(migrations.Files, ".")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		version, err := strconv.Atoi(strings.SplitN(entry.Name(), "_", 2)[0])
		if err != nil {
			return fmt.Errorf("parse migration %s: %w", entry.Name(), err)
		}
		var applied bool
		if err := p.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, version).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		body, err := migrations.Files.ReadFile(entry.Name())
		if err != nil {
			return err
		}
		tx, err := p.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, string(body)); err == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES($1)`, version)
		}
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %d: %w", version, err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (p *Postgres) Ready(ctx context.Context) error { return p.db.PingContext(ctx) }
func (p *Postgres) Close()                          { _ = p.db.Close() }

func (p *Postgres) Initialized(ctx context.Context) (bool, error) {
	var initialized bool
	err := p.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users)`).Scan(&initialized)
	return initialized, err
}

func (p *Postgres) CreateInitialAdmin(ctx context.Context, user User) error {
	tx, err := p.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return ErrAlreadyInitialized
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO users(id,email,display_name,password_hash,role) VALUES($1,$2,$3,$4,'administrator')`, user.ID, user.Email, user.DisplayName, user.PasswordHash)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (p *Postgres) UserByEmail(ctx context.Context, email string) (User, error) {
	return scanUser(p.db.QueryRowContext(ctx, `SELECT id,email,display_name,role,password_hash,created_at FROM users WHERE email_normalized=lower(trim($1))`, email))
}

func scanUser(row interface{ Scan(...any) error }) (User, error) {
	var user User
	err := row.Scan(&user.ID, &user.Email, &user.DisplayName, &user.Role, &user.PasswordHash, &user.CreatedAt)
	return user, err
}

func (p *Postgres) CreateSession(ctx context.Context, tokenHash [32]byte, userID string, expiresAt time.Time) error {
	_, err := p.db.ExecContext(ctx, `INSERT INTO sessions(token_hash,user_id,expires_at) VALUES($1,$2,$3)`, tokenHash[:], userID, expiresAt)
	return err
}

func (p *Postgres) Session(ctx context.Context, tokenHash [32]byte) (Session, error) {
	var session Session
	err := p.db.QueryRowContext(ctx, `SELECT u.id,u.email,u.display_name,u.role,u.password_hash,u.created_at,s.expires_at FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=$1 AND s.expires_at>now()`, tokenHash[:]).Scan(
		&session.User.ID, &session.User.Email, &session.User.DisplayName, &session.User.Role, &session.User.PasswordHash, &session.User.CreatedAt, &session.ExpiresAt)
	return session, err
}

func (p *Postgres) DeleteSession(ctx context.Context, tokenHash [32]byte) error {
	_, err := p.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=$1`, tokenHash[:])
	return err
}

func (p *Postgres) Audit(ctx context.Context, eventType string, userID *string, sourceIP, detail string) error {
	_, err := p.db.ExecContext(ctx, `INSERT INTO audit_events(event_type,user_id,source_ip,detail) VALUES($1,$2,$3,$4)`, eventType, userID, sourceIP, detail)
	return err
}

func NewUserID() string { return uuid.NewString() }

func IsNotFound(err error) bool { return errors.Is(err, sql.ErrNoRows) }
