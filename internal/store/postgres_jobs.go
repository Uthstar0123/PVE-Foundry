package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"proxmox-orchestrator/internal/redact"
)

const jobColumns = `SELECT id,operation,resource_id,status,initiator_user_id,idempotency_key,request_hash,COALESCE(worker_id,''),lease_expires_at,cancel_requested_at,COALESCE(error_code,''),logs_truncated,created_at,started_at,finished_at,updated_at FROM jobs`

const jobReturning = `RETURNING id,operation,resource_id,status,initiator_user_id,idempotency_key,request_hash,COALESCE(worker_id,''),lease_expires_at,cancel_requested_at,COALESCE(error_code,''),logs_truncated,created_at,started_at,finished_at,updated_at`

type rowScanner interface{ Scan(...any) error }

type contextExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func scanJob(row rowScanner) (Job, error) {
	var job Job
	var lease, cancel, started, finished sql.NullTime
	err := row.Scan(&job.ID, &job.Operation, &job.ResourceID, &job.Status, &job.InitiatorUserID,
		&job.IdempotencyKey, &job.RequestHash, &job.WorkerID, &lease, &cancel, &job.ErrorCode,
		&job.LogsTruncated, &job.CreatedAt, &started, &finished, &job.UpdatedAt)
	if lease.Valid {
		value := lease.Time
		job.LeaseExpiresAt = &value
	}
	if cancel.Valid {
		value := cancel.Time
		job.CancelRequestedAt = &value
	}
	if started.Valid {
		value := started.Time
		job.StartedAt = &value
	}
	if finished.Valid {
		value := finished.Time
		job.FinishedAt = &value
	}
	return job, err
}

func validateEnqueue(input EnqueueJobInput) error {
	if strings.TrimSpace(input.Operation) == "" || len(input.Operation) > 100 || strings.TrimSpace(input.ResourceID) == "" || len(input.ResourceID) > 255 ||
		strings.TrimSpace(input.IdempotencyKey) == "" || len(input.IdempotencyKey) > 255 || len(input.RequestHash) != 64 {
		return fmt.Errorf("invalid job input")
	}
	if _, err := uuid.Parse(input.InitiatorUserID); err != nil {
		return fmt.Errorf("invalid initiator: %w", err)
	}
	if input.Status != "" && input.Status != JobQueued && input.Status != JobWaitingApproval {
		return ErrInvalidJobTransition
	}
	return nil
}

func recordAudit(ctx context.Context, execer contextExecer, eventType string, userID *string, detail string) error {
	_, err := execer.ExecContext(ctx, `INSERT INTO audit_events(event_type,user_id,source_ip,detail) VALUES($1,$2,'internal',$3)`, eventType, userID, detail)
	return err
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (p *Postgres) EnqueueJob(ctx context.Context, input EnqueueJobInput) (Job, bool, error) {
	if err := validateEnqueue(input); err != nil {
		return Job{}, false, err
	}
	if input.Status == "" {
		input.Status = JobQueued
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, false, err
	}
	defer tx.Rollback()

	existing, err := scanJob(tx.QueryRowContext(ctx, jobColumns+` WHERE initiator_user_id=$1 AND idempotency_key=$2`, input.InitiatorUserID, input.IdempotencyKey))
	if err == nil {
		if existing.RequestHash != input.RequestHash {
			return Job{}, false, ErrIdempotencyConflict
		}
		return existing, false, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Job{}, false, err
	}

	job, err := scanJob(tx.QueryRowContext(ctx, `INSERT INTO jobs(id,operation,resource_id,status,initiator_user_id,idempotency_key,request_hash)
		VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (initiator_user_id,idempotency_key) DO NOTHING `+jobReturning,
		NewUserID(), input.Operation, input.ResourceID, input.Status, input.InitiatorUserID, input.IdempotencyKey, input.RequestHash))
	if errors.Is(err, sql.ErrNoRows) {
		existing, err = scanJob(tx.QueryRowContext(ctx, jobColumns+` WHERE initiator_user_id=$1 AND idempotency_key=$2`, input.InitiatorUserID, input.IdempotencyKey))
		if err != nil {
			return Job{}, false, err
		}
		if existing.RequestHash != input.RequestHash {
			return Job{}, false, ErrIdempotencyConflict
		}
		return existing, false, tx.Commit()
	}
	if err != nil {
		return Job{}, false, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO resource_locks(resource_id,job_id) VALUES($1,$2)`, input.ResourceID, job.ID); err != nil {
		if isUniqueViolation(err) {
			return Job{}, false, ErrResourceLocked
		}
		return Job{}, false, err
	}
	if err := recordAudit(ctx, tx, "job.queued", &job.InitiatorUserID, "job "+job.ID+" queued"); err != nil {
		return Job{}, false, err
	}
	return job, true, tx.Commit()
}

func (p *Postgres) ListJobs(ctx context.Context, limit int) ([]Job, error) {
	limit = normalizeLimit(limit, 100)
	rows, err := p.db.QueryContext(ctx, jobColumns+` ORDER BY created_at DESC,id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := []Job{}
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

const stepColumns = `SELECT id,job_id,name,status,detail,started_at,finished_at,created_at,updated_at FROM job_steps`

func scanStep(row rowScanner) (JobStep, error) {
	var step JobStep
	var started, finished sql.NullTime
	err := row.Scan(&step.ID, &step.JobID, &step.Name, &step.Status, &step.Detail, &started, &finished, &step.CreatedAt, &step.UpdatedAt)
	if started.Valid {
		value := started.Time
		step.StartedAt = &value
	}
	if finished.Valid {
		value := finished.Time
		step.FinishedAt = &value
	}
	return step, err
}

func scanEvent(row rowScanner) (JobEvent, error) {
	var event JobEvent
	err := row.Scan(&event.ID, &event.JobID, &event.Level, &event.Message, &event.CreatedAt)
	return event, err
}

func (p *Postgres) Job(ctx context.Context, id string) (JobDetail, error) {
	job, err := scanJob(p.db.QueryRowContext(ctx, jobColumns+` WHERE id=$1`, id))
	if err != nil {
		return JobDetail{}, err
	}
	detail := JobDetail{Job: job, Steps: []JobStep{}, Events: []JobEvent{}}
	rows, err := p.db.QueryContext(ctx, stepColumns+` WHERE job_id=$1 ORDER BY created_at,id`, id)
	if err != nil {
		return JobDetail{}, err
	}
	for rows.Next() {
		step, err := scanStep(rows)
		if err != nil {
			rows.Close()
			return JobDetail{}, err
		}
		detail.Steps = append(detail.Steps, step)
	}
	if err := rows.Close(); err != nil {
		return JobDetail{}, err
	}
	rows, err = p.db.QueryContext(ctx, `SELECT id,job_id,level,message,created_at FROM (
		SELECT id,job_id,level,message,created_at FROM job_events WHERE job_id=$1 ORDER BY id DESC LIMIT $2
	) recent ORDER BY id`, id, jobEventsPageSize)
	if err != nil {
		return JobDetail{}, err
	}
	defer rows.Close()
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return JobDetail{}, err
		}
		detail.Events = append(detail.Events, event)
	}
	return detail, rows.Err()
}

func (p *Postgres) CancelJob(ctx context.Context, id string) (Job, error) {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()
	job, err := scanJob(tx.QueryRowContext(ctx, jobColumns+` WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return Job{}, err
	}
	now := time.Now().UTC()
	switch job.Status {
	case JobQueued, JobWaitingApproval:
		job.Status, job.CancelRequestedAt, job.FinishedAt, job.UpdatedAt = JobCancelled, &now, &now, now
		if _, err = tx.ExecContext(ctx, `UPDATE jobs SET status='cancelled',cancel_requested_at=$2,finished_at=$2,updated_at=$2 WHERE id=$1`, id, now); err != nil {
			return Job{}, err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM resource_locks WHERE job_id=$1`, id); err != nil {
			return Job{}, err
		}
		if err = recordAudit(ctx, tx, "job.cancelled", &job.InitiatorUserID, "job "+id+" cancelled before execution"); err != nil {
			return Job{}, err
		}
	case JobRunning:
		if job.CancelRequestedAt == nil {
			job.CancelRequestedAt, job.UpdatedAt = &now, now
			if _, err = tx.ExecContext(ctx, `UPDATE jobs SET cancel_requested_at=$2,updated_at=$2 WHERE id=$1`, id, now); err != nil {
				return Job{}, err
			}
			if err = recordAudit(ctx, tx, "job.cancel_requested", &job.InitiatorUserID, "job "+id+" cancellation requested; no rollback promised"); err != nil {
				return Job{}, err
			}
		}
	default:
		return job, tx.Commit()
	}
	return job, tx.Commit()
}

func (p *Postgres) ClaimJob(ctx context.Context, workerID string, lease time.Duration) (Job, error) {
	if strings.TrimSpace(workerID) == "" || len(workerID) > 255 || lease <= 0 {
		return Job{}, ErrInvalidJobTransition
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(ctx, `SELECT id FROM jobs WHERE status='queued' ORDER BY created_at,id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&id)
	if err != nil {
		return Job{}, err
	}
	job, err := scanJob(tx.QueryRowContext(ctx, `UPDATE jobs SET status='running',worker_id=$2,lease_expires_at=now()+$3::interval,started_at=COALESCE(started_at,now()),updated_at=now() WHERE id=$1 `+jobReturning,
		id, workerID, fmt.Sprintf("%f seconds", lease.Seconds())))
	if err != nil {
		return Job{}, err
	}
	if err := recordAudit(ctx, tx, "job.started", &job.InitiatorUserID, "job "+id+" claimed by "+workerID); err != nil {
		return Job{}, err
	}
	return job, tx.Commit()
}

func (p *Postgres) RenewLease(ctx context.Context, id, workerID string, lease time.Duration) error {
	if lease <= 0 {
		return ErrLeaseExpired
	}
	result, err := p.db.ExecContext(ctx, `UPDATE jobs SET lease_expires_at=now()+$3::interval,updated_at=now() WHERE id=$1 AND worker_id=$2 AND status='running' AND lease_expires_at>now()`, id, workerID, fmt.Sprintf("%f seconds", lease.Seconds()))
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return ErrLeaseExpired
	}
	return err
}

func ownedRunningJob(ctx context.Context, tx *sql.Tx, id, workerID string) (Job, error) {
	job, err := scanJob(tx.QueryRowContext(ctx, jobColumns+` WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return Job{}, err
	}
	if job.Status != JobRunning || job.WorkerID != workerID {
		return Job{}, ErrInvalidJobTransition
	}
	return job, nil
}

func (p *Postgres) StartStep(ctx context.Context, id, workerID, name string) (JobStep, error) {
	if strings.TrimSpace(name) == "" || len(name) > 100 {
		return JobStep{}, ErrInvalidJobTransition
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return JobStep{}, err
	}
	defer tx.Rollback()
	if _, err := ownedRunningJob(ctx, tx, id, workerID); err != nil {
		return JobStep{}, err
	}
	step, err := scanStep(tx.QueryRowContext(ctx, `INSERT INTO job_steps(id,job_id,name,status,started_at) VALUES($1,$2,$3,'running',now())
		ON CONFLICT (job_id,name) DO NOTHING RETURNING id,job_id,name,status,detail,started_at,finished_at,created_at,updated_at`, NewUserID(), id, name))
	if errors.Is(err, sql.ErrNoRows) {
		step, err = scanStep(tx.QueryRowContext(ctx, stepColumns+` WHERE job_id=$1 AND name=$2`, id, name))
	}
	if err != nil {
		return JobStep{}, err
	}
	return step, tx.Commit()
}

func (p *Postgres) CompleteStep(ctx context.Context, id, workerID, name, status, detail string) error {
	if status != JobSucceeded && status != JobFailed && status != "skipped" && status != JobCancelled {
		return ErrInvalidJobTransition
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	job, err := ownedRunningJob(ctx, tx, id, workerID)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE job_steps SET status=$3,detail=$4,finished_at=now(),updated_at=now() WHERE job_id=$1 AND name=$2`, id, name, status, redact.Text(detail))
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	if _, err = tx.ExecContext(ctx, `UPDATE jobs SET updated_at=now() WHERE id=$1`, job.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *Postgres) CompleteJob(ctx context.Context, id, workerID, status, errorCode string) error {
	if !validJobFinalStatus(status) {
		return ErrInvalidJobTransition
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	job, err := ownedRunningJob(ctx, tx, id, workerID)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE jobs SET status=$2,error_code=NULLIF($3,''),finished_at=now(),updated_at=now(),lease_expires_at=NULL WHERE id=$1`, id, status, errorCode); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM resource_locks WHERE job_id=$1`, id); err != nil {
		return err
	}
	if err = recordAudit(ctx, tx, "job."+status, &job.InitiatorUserID, "job "+id+" finished with status "+status); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *Postgres) AppendEvent(ctx context.Context, id, level, message string, secrets ...string) error {
	switch level {
	case "debug", "info", "warn", "error":
	default:
		level = "info"
	}
	runes := []rune(message)
	if len(runes) > 16384 {
		message = string(runes[:16384])
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM jobs WHERE id=$1)`, id).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return sql.ErrNoRows
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO job_events(job_id,level,message) VALUES($1,$2,$3)`, id, level, redact.Text(message, secrets...)); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `WITH deleted AS (
		DELETE FROM job_events WHERE job_id=$1 AND id NOT IN (SELECT id FROM job_events WHERE job_id=$1 ORDER BY id DESC LIMIT 1000) RETURNING 1
	) UPDATE jobs SET logs_truncated=logs_truncated OR EXISTS(SELECT 1 FROM deleted),updated_at=now() WHERE id=$1`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *Postgres) JobLogs(ctx context.Context, id string) ([]JobEvent, error) {
	var exists bool
	if err := p.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM jobs WHERE id=$1)`, id).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, sql.ErrNoRows
	}
	rows, err := p.db.QueryContext(ctx, `SELECT id,job_id,level,message,created_at FROM job_events WHERE job_id=$1 ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []JobEvent{}
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (p *Postgres) FailExpiredLeases(ctx context.Context) (int, error) {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id,resource_id,initiator_user_id FROM jobs WHERE status='running' AND lease_expires_at<=now() ORDER BY id FOR UPDATE SKIP LOCKED`)
	if err != nil {
		return 0, err
	}
	type expiredJob struct{ id, resourceID, userID string }
	expired := []expiredJob{}
	for rows.Next() {
		var job expiredJob
		if err := rows.Scan(&job.id, &job.resourceID, &job.userID); err != nil {
			rows.Close()
			return 0, err
		}
		expired = append(expired, job)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	for _, job := range expired {
		if _, err = tx.ExecContext(ctx, `UPDATE jobs SET status='failed',error_code='lease_expired',finished_at=now(),updated_at=now(),lease_expires_at=NULL WHERE id=$1 AND status='running'`, job.id); err != nil {
			return 0, err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM resource_locks WHERE job_id=$1`, job.id); err != nil {
			return 0, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO job_events(job_id,level,message) VALUES($1,'error','Lease expired; operation outcome is unknown and will not be retried automatically.')`, job.id); err != nil {
			return 0, err
		}
		if err = recordAudit(ctx, tx, "job.lease_expired", &job.userID, "job "+job.id+" failed with unknown outcome; automatic retry disabled"); err != nil {
			return 0, err
		}
	}
	return len(expired), tx.Commit()
}

func (p *Postgres) ListAudit(ctx context.Context, limit int) ([]AuditEvent, error) {
	limit = normalizeLimit(limit, 100)
	rows, err := p.db.QueryContext(ctx, `SELECT id,event_type,user_id,source_ip,detail,created_at FROM audit_events ORDER BY id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []AuditEvent{}
	for rows.Next() {
		var event AuditEvent
		if err := rows.Scan(&event.ID, &event.EventType, &event.UserID, &event.SourceIP, &event.Detail, &event.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}
