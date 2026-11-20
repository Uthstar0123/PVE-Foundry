package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"
)

func TestPostgresOpenTofuPlanApprovalLifecycle(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	database, err := OpenPostgres(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	userID := NewUserID()
	if _, err := database.db.ExecContext(ctx, `INSERT INTO users(id,email,display_name,password_hash,role) VALUES($1,$2,'Deployment integration','test-only','administrator')`, userID, "deployment-"+userID+"@example.local"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = database.db.ExecContext(cleanupCtx, `DELETE FROM jobs WHERE initiator_user_id=$1`, userID)
		_, _ = database.db.ExecContext(cleanupCtx, `DELETE FROM users WHERE id=$1`, userID)
	}()
	artifactHash := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	job, _, err := database.EnqueueJob(ctx, EnqueueJobInput{Operation: "opentofu.plan_apply", ResourceID: "deployment/" + userID, InitiatorUserID: userID, IdempotencyKey: "one", RequestHash: artifactHash, Artifact: []byte("encrypted-artifact"), ArtifactSHA256: artifactHash})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ClaimJob(ctx, "integration-runner", time.Minute); err != nil {
		t.Fatal(err)
	}
	planHash := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	if err := database.SavePlan(ctx, job.ID, "integration-runner", PlanResult{PlanText: "1 to change", PlanFile: []byte("encrypted-plan"), PlanSHA256: planHash, ArtifactSHA256: artifactHash}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ApprovePlan(ctx, job.ID, userID, "wrong", 10*time.Minute); !errors.Is(err, ErrPlanChanged) {
		t.Fatalf("wrong artifact approved: %v", err)
	}
	detail, err := database.ApprovePlan(ctx, job.ID, userID, artifactHash, 10*time.Minute)
	if err != nil || detail.ApprovalExpiresAt == nil || detail.PlanText != "1 to change" {
		t.Fatalf("approval: detail=%+v err=%v", detail, err)
	}
	claimed, err := database.ClaimApprovedJob(ctx, "integration-runner", time.Minute)
	if err != nil || claimed.ID != job.ID {
		t.Fatalf("claim approved: %+v %v", claimed, err)
	}
	if _, err := database.ClaimApprovedJob(ctx, "other-runner", time.Minute); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("double apply claim: %v", err)
	}
	artifact, err := database.DeploymentArtifact(ctx, job.ID)
	if err != nil || string(artifact.Artifact) != "encrypted-artifact" || string(artifact.PlanFile) != "encrypted-plan" || artifact.PlanSHA256 != planHash {
		t.Fatalf("stored artifact: %+v %v", artifact, err)
	}
	if err := database.CompleteJob(ctx, job.ID, "integration-runner", JobSucceeded, ""); err != nil {
		t.Fatal(err)
	}
}
