package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPostgresQueueLifecycleAndLogRotation(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	database, err := OpenPostgres(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	userID := NewUserID()
	if _, err := database.db.ExecContext(ctx, `INSERT INTO users(id,email,display_name,password_hash,role) VALUES($1,$2,'Queue integration','test-only','administrator')`, userID, "queue-"+userID+"@example.local"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = database.db.ExecContext(cleanupCtx, `DELETE FROM jobs WHERE initiator_user_id=$1`, userID)
		_, _ = database.db.ExecContext(cleanupCtx, `DELETE FROM users WHERE id=$1`, userID)
	}()
	input := EnqueueJobInput{Operation: "integration.test", ResourceID: "test/" + userID, InitiatorUserID: userID, IdempotencyKey: "one", RequestHash: strings.Repeat("e", 64)}
	job, created, err := database.EnqueueJob(ctx, input)
	if err != nil || !created {
		t.Fatalf("enqueue: created=%v err=%v", created, err)
	}
	replayed, created, err := database.EnqueueJob(ctx, input)
	if err != nil || created || replayed.ID != job.ID {
		t.Fatalf("replay: created=%v err=%v", created, err)
	}
	conflict := input
	conflict.IdempotencyKey = "two"
	if _, _, err := database.EnqueueJob(ctx, conflict); !errors.Is(err, ErrResourceLocked) {
		t.Fatalf("resource conflict: %v", err)
	}
	claimed, err := database.ClaimJob(ctx, "integration-runner", time.Minute)
	if err != nil || claimed.ID != job.ID {
		t.Fatalf("claim: %+v %v", claimed, err)
	}
	if _, err := database.ClaimJob(ctx, "second-runner", time.Minute); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("double claim: %v", err)
	}
	if err := database.RenewLease(ctx, job.ID, "wrong-runner", time.Minute); !errors.Is(err, ErrLeaseExpired) {
		t.Fatalf("wrong worker renewal: %v", err)
	}
	if _, err := database.StartStep(ctx, job.ID, "integration-runner", "validate"); err != nil {
		t.Fatal(err)
	}
	if err := database.CompleteStep(ctx, job.ID, "integration-runner", "validate", JobSucceeded, "validated"); err != nil {
		t.Fatal(err)
	}
	if err := database.AppendEvent(ctx, job.ID, "info", "token=postgres-secret"); err != nil {
		t.Fatal(err)
	}
	cancelled, err := database.CancelJob(ctx, job.ID)
	if err != nil || cancelled.Status != JobRunning || cancelled.CancelRequestedAt == nil {
		t.Fatalf("cancel running: %+v %v", cancelled, err)
	}
	if err := database.CompleteJob(ctx, job.ID, "integration-runner", JobCancelled, "cancelled_by_user"); err != nil {
		t.Fatal(err)
	}
	detail, err := database.Job(ctx, job.ID)
	if err != nil || detail.Job.Status != JobCancelled || len(detail.Steps) != 1 || strings.Contains(detail.Events[0].Message, "postgres-secret") {
		t.Fatalf("completed detail: %+v %v", detail, err)
	}

	input.IdempotencyKey = "expired"
	expired, _, err := database.EnqueueJob(ctx, input)
	if err != nil {
		t.Fatalf("resource was not released: %v", err)
	}
	if _, err := database.ClaimJob(ctx, "lost-runner", time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	failed, err := database.FailExpiredLeases(ctx)
	if err != nil || failed != 1 {
		t.Fatalf("expire: failed=%d err=%v", failed, err)
	}
	expiredDetail, err := database.Job(ctx, expired.ID)
	if err != nil || expiredDetail.Job.Status != JobFailed || expiredDetail.Job.ErrorCode != "lease_expired" {
		t.Fatalf("expired detail: %+v %v", expiredDetail, err)
	}

	input.IdempotencyKey = "rotation"
	input.ResourceID += "/logs"
	rotated, _, err := database.EnqueueJob(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < maxStoredJobEvents+1; index++ {
		if err := database.AppendEvent(ctx, rotated.ID, "info", "event"); err != nil {
			t.Fatal(err)
		}
	}
	logs, err := database.JobLogs(ctx, rotated.ID)
	if err != nil || len(logs) != maxStoredJobEvents {
		t.Fatalf("rotated logs=%d err=%v", len(logs), err)
	}
	rotatedDetail, err := database.Job(ctx, rotated.ID)
	if err != nil || !rotatedDetail.Job.LogsTruncated || len(rotatedDetail.Events) != jobEventsPageSize {
		t.Fatalf("rotation detail: job=%+v events=%d err=%v", rotatedDetail.Job, len(rotatedDetail.Events), err)
	}
}
