package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

func memoryWithUser(t *testing.T) (*Memory, User) {
	t.Helper()
	memory := NewMemory()
	user := User{ID: NewUserID(), Email: "jobs@example.local", DisplayName: "Jobs", Role: "administrator"}
	if err := memory.CreateInitialAdmin(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	return memory, user
}

func jobInput(user User) EnqueueJobInput {
	return EnqueueJobInput{Operation: "vm.create", ResourceID: "qemu/test-resource", InitiatorUserID: user.ID, IdempotencyKey: "request-1", RequestHash: strings.Repeat("a", 64)}
}

func TestJobIdempotencyAndResourceLocks(t *testing.T) {
	memory, user := memoryWithUser(t)
	input := jobInput(user)
	first, created, err := memory.EnqueueJob(context.Background(), input)
	if err != nil || !created {
		t.Fatalf("first enqueue: created=%v err=%v", created, err)
	}
	replay, created, err := memory.EnqueueJob(context.Background(), input)
	if err != nil || created || replay.ID != first.ID {
		t.Fatalf("idempotent replay: %+v created=%v err=%v", replay, created, err)
	}
	conflicting := input
	conflicting.RequestHash = strings.Repeat("b", 64)
	if _, _, err := memory.EnqueueJob(context.Background(), conflicting); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("expected idempotency conflict, got %v", err)
	}
	other := input
	other.IdempotencyKey = "request-2"
	if _, _, err := memory.EnqueueJob(context.Background(), other); !errors.Is(err, ErrResourceLocked) {
		t.Fatalf("expected resource lock, got %v", err)
	}
}

func TestClaimStepsCancelAndCompletion(t *testing.T) {
	memory, user := memoryWithUser(t)
	job, _, err := memory.EnqueueJob(context.Background(), jobInput(user))
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := memory.ClaimJob(context.Background(), "runner-test", time.Minute)
	if err != nil || claimed.ID != job.ID || claimed.Status != JobRunning {
		t.Fatalf("claim: %+v %v", claimed, err)
	}
	if _, err := memory.ClaimJob(context.Background(), "runner-other", time.Minute); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("queue was claimed twice: %v", err)
	}
	if err := memory.RenewLease(context.Background(), job.ID, "runner-other", time.Minute); !errors.Is(err, ErrLeaseExpired) {
		t.Fatalf("foreign lease renewal: %v", err)
	}
	if err := memory.RenewLease(context.Background(), job.ID, "runner-test", time.Minute); err != nil {
		t.Fatal(err)
	}
	step, err := memory.StartStep(context.Background(), job.ID, "runner-test", "validate")
	if err != nil || step.Status != JobRunning {
		t.Fatalf("start step: %+v %v", step, err)
	}
	if err := memory.CompleteStep(context.Background(), job.ID, "runner-test", "validate", JobSucceeded, "ok"); err != nil {
		t.Fatal(err)
	}
	cancelled, err := memory.CancelJob(context.Background(), job.ID)
	if err != nil || cancelled.Status != JobRunning || cancelled.CancelRequestedAt == nil {
		t.Fatalf("running cancellation: %+v %v", cancelled, err)
	}
	if err := memory.CompleteJob(context.Background(), job.ID, "runner-test", JobCancelled, "cancelled_by_user"); err != nil {
		t.Fatal(err)
	}
	detail, err := memory.Job(context.Background(), job.ID)
	if err != nil || detail.Job.Status != JobCancelled || len(detail.Steps) != 1 || detail.Steps[0].Status != JobSucceeded {
		t.Fatalf("completed detail: %+v %v", detail, err)
	}
}

func TestQueuedCancellationReleasesResource(t *testing.T) {
	memory, user := memoryWithUser(t)
	job, _, _ := memory.EnqueueJob(context.Background(), jobInput(user))
	cancelled, err := memory.CancelJob(context.Background(), job.ID)
	if err != nil || cancelled.Status != JobCancelled {
		t.Fatalf("cancel queued: %+v %v", cancelled, err)
	}
	input := jobInput(user)
	input.IdempotencyKey = "after-cancel"
	if _, _, err := memory.EnqueueJob(context.Background(), input); err != nil {
		t.Fatalf("resource remained locked: %v", err)
	}
}

func TestExpiredLeaseFailsWithoutAutomaticRetry(t *testing.T) {
	memory, user := memoryWithUser(t)
	job, _, _ := memory.EnqueueJob(context.Background(), jobInput(user))
	if _, err := memory.ClaimJob(context.Background(), "runner-lost", time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	failed, err := memory.FailExpiredLeases(context.Background())
	if err != nil || failed != 1 {
		t.Fatalf("expired recovery: failed=%d err=%v", failed, err)
	}
	detail, _ := memory.Job(context.Background(), job.ID)
	if detail.Job.Status != JobFailed || detail.Job.ErrorCode != "lease_expired" || len(detail.Events) != 1 {
		t.Fatalf("unexpected recovery detail: %+v", detail)
	}
	if _, err := memory.ClaimJob(context.Background(), "runner-retry", time.Minute); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expired operation was retried: %v", err)
	}
	input := jobInput(user)
	input.IdempotencyKey = "manual-reconciliation"
	if _, _, err := memory.EnqueueJob(context.Background(), input); err != nil {
		t.Fatalf("resource not released after recovery: %v", err)
	}
}

func TestLogsAreRedactedRotatedAndDownloadable(t *testing.T) {
	memory, user := memoryWithUser(t)
	job, _, _ := memory.EnqueueJob(context.Background(), jobInput(user))
	if err := memory.AppendEvent(context.Background(), job.ID, "info", "token=visible-secret explicit-secret", "explicit-secret"); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < maxStoredJobEvents+5; index++ {
		_ = memory.AppendEvent(context.Background(), job.ID, "info", "entry")
	}
	detail, err := memory.Job(context.Background(), job.ID)
	if err != nil || !detail.Job.LogsTruncated || len(detail.Events) != jobEventsPageSize {
		t.Fatalf("log rotation: %+v %v", detail.Job, err)
	}
	logs, err := memory.JobLogs(context.Background(), job.ID)
	if err != nil || len(logs) != maxStoredJobEvents {
		t.Fatalf("stored log count=%d err=%v", len(logs), err)
	}
	for _, event := range logs {
		if strings.Contains(event.Message, "visible-secret") || strings.Contains(event.Message, "explicit-secret") {
			t.Fatalf("unredacted log: %+v", event)
		}
	}
}
