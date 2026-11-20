package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func plannedMemoryJob(t *testing.T) (*Memory, User, Job, DeploymentArtifact) {
	t.Helper()
	memory, user := memoryWithUser(t)
	input := jobInput(user)
	input.Artifact = []byte("encrypted-artifact")
	input.ArtifactSHA256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	job, _, err := memory.EnqueueJob(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := memory.ClaimJob(context.Background(), "runner-test", time.Minute); err != nil {
		t.Fatal(err)
	}
	artifact, err := memory.DeploymentArtifact(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	return memory, user, job, artifact
}

func saveMemoryPlan(memory *Memory, job Job, artifact DeploymentArtifact) error {
	return memory.SavePlan(context.Background(), job.ID, "runner-test", PlanResult{PlanText: "1 to change", PlanFile: []byte("encrypted-plan"), PlanSHA256: "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789", ArtifactSHA256: artifact.ArtifactSHA256})
}

func TestPlanApprovalPinsArtifactAndIsClaimedOnce(t *testing.T) {
	memory, user, job, artifact := plannedMemoryJob(t)
	if err := saveMemoryPlan(memory, job, artifact); err != nil {
		t.Fatal(err)
	}
	if _, err := memory.ApprovePlan(context.Background(), job.ID, user.ID, "wrong", 10*time.Minute); !errors.Is(err, ErrPlanChanged) {
		t.Fatalf("approval with changed artifact: %v", err)
	}
	detail, err := memory.ApprovePlan(context.Background(), job.ID, user.ID, artifact.ArtifactSHA256, 10*time.Minute)
	if err != nil || detail.ApprovalExpiresAt == nil || detail.PlanText != "1 to change" {
		t.Fatalf("approval: detail=%+v err=%v", detail, err)
	}
	claimed, err := memory.ClaimApprovedJob(context.Background(), "runner-test", time.Minute)
	if err != nil || claimed.ID != job.ID || claimed.Status != JobRunning {
		t.Fatalf("claim approved: %+v %v", claimed, err)
	}
	if _, err := memory.ClaimApprovedJob(context.Background(), "runner-other", time.Minute); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("approved plan claimed twice: %v", err)
	}
}

func TestPlanRejectsConfigurationSubstitution(t *testing.T) {
	memory, _, job, artifact := plannedMemoryJob(t)
	artifact.ArtifactSHA256 = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	if err := saveMemoryPlan(memory, job, artifact); !errors.Is(err, ErrPlanChanged) {
		t.Fatalf("substituted configuration accepted: %v", err)
	}
}

func TestExpiredPlanAndApprovalCannotBeApplied(t *testing.T) {
	memory, user, job, artifact := plannedMemoryJob(t)
	if err := saveMemoryPlan(memory, job, artifact); err != nil {
		t.Fatal(err)
	}
	memory.planCreated[job.ID] = time.Now().Add(-MaxPlanAge - time.Minute)
	if _, err := memory.ApprovePlan(context.Background(), job.ID, user.ID, artifact.ArtifactSHA256, 10*time.Minute); !errors.Is(err, ErrApprovalExpired) {
		t.Fatalf("expired plan approved: %v", err)
	}
	memory.planCreated[job.ID] = time.Now()
	if _, err := memory.ApprovePlan(context.Background(), job.ID, user.ID, artifact.ArtifactSHA256, 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	approval := memory.approvals[job.ID]
	approval.expiresAt = time.Now().Add(-time.Second)
	memory.approvals[job.ID] = approval
	if _, err := memory.ClaimApprovedJob(context.Background(), "runner-test", time.Minute); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expired approval claimed: %v", err)
	}
}
