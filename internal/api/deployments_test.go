package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

	"proxmox-orchestrator/internal/store"
)

func deploymentServer(t *testing.T) (*httptest.Server, *store.Memory, string) {
	t.Helper()
	database := store.NewMemory()
	handler, err := NewManagementHandlerWithConfig(database, bytes.Repeat([]byte{1}, 32), false, ManagementConfig{RunnerToken: strings.Repeat("r", 32), EncryptionKey: bytes.Repeat([]byte{2}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	server.Client().Jar = jar
	client := server.Client()
	csrf := csrfFor(t, client, server.URL)
	response := postJSON(t, client, server.URL+"/api/v1/setup", csrf, setupInput())
	response.Body.Close()
	return server, database, csrf
}

func runnerPost(t *testing.T, client *http.Client, url string, body any, authorized bool) *http.Response {
	t.Helper()
	payload, _ := json.Marshal(body)
	request, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if authorized {
		request.Header.Set("Authorization", "Bearer "+strings.Repeat("r", 32))
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func TestOpenTofuDeploymentRunnerFlowAndAuthentication(t *testing.T) {
	server, _, csrf := deploymentServer(t)
	client := server.Client()
	artifact := []byte("test-artifact")
	response := postJSON(t, client, server.URL+"/api/v1/deployments/opentofu", csrf, map[string]any{"resourceId": "deployment/test", "idempotencyKey": "deploy-1", "artifact": base64.StdEncoding.EncodeToString(artifact)})
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create deployment status: %d", response.StatusCode)
	}
	var created struct {
		Job            store.Job `json:"job"`
		ArtifactSHA256 string    `json:"artifactSha256"`
	}
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()

	response = runnerPost(t, client, server.URL+"/api/v1/runner/claim", map[string]any{"workerId": "runner-test", "leaseSeconds": 60}, false)
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous runner status: %d", response.StatusCode)
	}
	response = runnerPost(t, client, server.URL+"/api/v1/runner/claim", map[string]any{"workerId": "runner-test", "leaseSeconds": 60}, true)
	var claim struct {
		Job            store.Job `json:"job"`
		Mode           string    `json:"mode"`
		Artifact       []byte    `json:"artifact"`
		ArtifactSHA256 string    `json:"artifactSha256"`
	}
	if err := json.NewDecoder(response.Body).Decode(&claim); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || claim.Mode != "plan" || !bytes.Equal(claim.Artifact, artifact) {
		t.Fatalf("plan claim: status=%d claim=%+v", response.StatusCode, claim)
	}

	plan := []byte("binary-plan")
	planHash := "cb198cb79f4edda85235e58a73f32b79525b08c6d3f09fd04a4f95bf2df5b38c"
	response = runnerPost(t, client, server.URL+"/api/v1/runner/jobs/"+created.Job.ID+"/plan", map[string]any{"workerId": "runner-test", "artifactSha256": created.ArtifactSHA256, "planSha256": planHash, "planText": "1 to change", "plan": base64.StdEncoding.EncodeToString(plan)}, true)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("save plan status: %d", response.StatusCode)
	}
	response = postJSON(t, client, server.URL+"/api/v1/jobs/"+created.Job.ID+"/approve", csrf, map[string]any{"artifactSha256": "wrong"})
	response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("changed artifact approval status: %d", response.StatusCode)
	}
	response = postJSON(t, client, server.URL+"/api/v1/jobs/"+created.Job.ID+"/approve", csrf, map[string]any{"artifactSha256": created.ArtifactSHA256})
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("approval status: %d", response.StatusCode)
	}

	response = runnerPost(t, client, server.URL+"/api/v1/runner/claim", map[string]any{"workerId": "runner-test", "leaseSeconds": 60}, true)
	var applyClaim struct {
		Mode       string `json:"mode"`
		Plan       []byte `json:"plan"`
		PlanSHA256 string `json:"planSha256"`
	}
	if err := json.NewDecoder(response.Body).Decode(&applyClaim); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if applyClaim.Mode != "apply" || !bytes.Equal(applyClaim.Plan, plan) || applyClaim.PlanSHA256 != planHash {
		t.Fatalf("apply claim: %+v", applyClaim)
	}
	response = runnerPost(t, client, server.URL+"/api/v1/runner/jobs/"+created.Job.ID+"/complete", map[string]any{"workerId": "runner-test", "status": store.JobSucceeded}, true)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("complete status: %d", response.StatusCode)
	}
}
