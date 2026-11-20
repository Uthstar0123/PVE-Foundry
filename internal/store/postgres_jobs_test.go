package store

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPostgresJobsSurviveManagementRestart(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	first, err := OpenPostgres(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	user := User{ID: NewUserID(), Email: "restart-" + NewUserID() + "@example.local", DisplayName: "Restart test", Role: "administrator", PasswordHash: "test-only"}
	if _, err := first.db.ExecContext(ctx, `INSERT INTO users(id,email,display_name,password_hash,role) VALUES($1,$2,$3,$4,'administrator')`, user.ID, user.Email, user.DisplayName, user.PasswordHash); err != nil {
		first.Close()
		t.Fatal(err)
	}
	input := EnqueueJobInput{Operation: "restart.test", ResourceID: "test/" + user.ID, InitiatorUserID: user.ID, IdempotencyKey: "restart", RequestHash: strings.Repeat("c", 64)}
	job, created, err := first.EnqueueJob(ctx, input)
	if err != nil || !created {
		first.Close()
		t.Fatalf("enqueue: %v", err)
	}
	first.Close()

	second, err := OpenPostgres(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	defer func() {
		_, _ = second.db.ExecContext(context.Background(), `DELETE FROM jobs WHERE initiator_user_id=$1`, user.ID)
		_, _ = second.db.ExecContext(context.Background(), `DELETE FROM users WHERE id=$1`, user.ID)
	}()
	replayed, created, err := second.EnqueueJob(ctx, input)
	if err != nil || created || replayed.ID != job.ID {
		t.Fatalf("replay after restart: created=%v err=%v", created, err)
	}
	detail, err := second.Job(ctx, job.ID)
	if err != nil || detail.Job.Status != JobQueued {
		t.Fatalf("job did not survive restart: %+v %v", detail, err)
	}
}
