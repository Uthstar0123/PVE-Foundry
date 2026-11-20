package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"proxmox-orchestrator/internal/store"
)

func createTestJob(t *testing.T, database *store.Memory) store.Job {
	t.Helper()
	user, err := database.UserByEmail(context.Background(), "admin@example.local")
	if err != nil {
		t.Fatal(err)
	}
	job, _, err := database.EnqueueJob(context.Background(), store.EnqueueJobInput{Operation: "vm.create", ResourceID: "qemu/api-test", InitiatorUserID: user.ID, IdempotencyKey: "api-request", RequestHash: strings.Repeat("d", 64)})
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func TestJobAPIRequiresSessionAndSupportsLogsAndCancellation(t *testing.T) {
	server, database := managementServer(t)
	client := server.Client()
	token := csrfFor(t, client, server.URL)
	response := postJSON(t, client, server.URL+"/api/v1/setup", token, setupInput())
	response.Body.Close()
	job := createTestJob(t, database)
	if err := database.AppendEvent(context.Background(), job.ID, "info", "password=not-for-log"); err != nil {
		t.Fatal(err)
	}

	response, err := client.Get(server.URL + "/api/v1/jobs")
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("list jobs: status=%d err=%v", response.StatusCode, err)
	}
	var list struct {
		Jobs []store.Job `json:"jobs"`
	}
	if err := json.NewDecoder(response.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if len(list.Jobs) != 1 || list.Jobs[0].ID != job.ID {
		t.Fatalf("job list: %+v", list)
	}

	response, err = client.Get(server.URL + "/api/v1/jobs/" + job.ID + "/logs")
	if err != nil {
		t.Fatal(err)
	}
	logBody, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || strings.Contains(string(logBody), "not-for-log") || !strings.Contains(string(logBody), "[REDACTED]") {
		t.Fatalf("log response: status=%d body=%q", response.StatusCode, logBody)
	}

	response = postJSON(t, client, server.URL+"/api/v1/jobs/"+job.ID+"/cancel", token, map[string]any{})
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("cancel: status=%d body=%s", response.StatusCode, body)
	}
	response.Body.Close()
	response, _ = client.Get(server.URL + "/api/v1/audit")
	var auditBody struct {
		Events []store.AuditEvent `json:"events"`
	}
	_ = json.NewDecoder(response.Body).Decode(&auditBody)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || len(auditBody.Events) == 0 {
		t.Fatalf("audit: status=%d events=%d", response.StatusCode, len(auditBody.Events))
	}
}

func TestJobEndpointsRejectAnonymousRequests(t *testing.T) {
	server, _ := managementServer(t)
	response, err := server.Client().Get(server.URL + "/api/v1/jobs")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous jobs status: %d", response.StatusCode)
	}
}

func TestJobWebSocketAuthenticatesAndStreamsChanges(t *testing.T) {
	server, database := managementServer(t)
	client := server.Client()
	token := csrfFor(t, client, server.URL)
	response := postJSON(t, client, server.URL+"/api/v1/setup", token, setupInput())
	response.Body.Close()
	job := createTestJob(t, database)
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/jobs/" + job.ID

	anonymous, _, err := websocket.Dial(context.Background(), wsURL, nil)
	if err == nil {
		anonymous.Close(websocket.StatusNormalClosure, "")
		t.Fatal("anonymous websocket was accepted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	connection, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPClient: client})
	if err != nil {
		t.Fatalf("authenticated websocket: %v", err)
	}
	defer connection.Close(websocket.StatusNormalClosure, "test complete")
	_, payload, err := connection.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var initial store.JobDetail
	if err := json.Unmarshal(payload, &initial); err != nil || initial.Job.ID != job.ID {
		t.Fatalf("initial snapshot: %+v %v", initial, err)
	}
	if err := database.AppendEvent(ctx, job.ID, "info", "stream-update"); err != nil {
		t.Fatal(err)
	}
	_, payload, err = connection.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var updated store.JobDetail
	if err := json.Unmarshal(payload, &updated); err != nil || len(updated.Events) != 1 || updated.Events[0].Message != "stream-update" {
		t.Fatalf("streamed snapshot: %+v %v", updated, err)
	}
}
