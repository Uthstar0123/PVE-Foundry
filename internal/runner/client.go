package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"proxmox-orchestrator/internal/store"
)

type Claim struct {
	Job            store.Job `json:"job"`
	Mode           string    `json:"mode"`
	Artifact       []byte    `json:"artifact"`
	ArtifactSHA256 string    `json:"artifactSha256"`
	Plan           []byte    `json:"plan"`
	PlanSHA256     string    `json:"planSha256"`
}

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewClient(baseURL, token string, client *http.Client) (*Client, error) {
	if strings.TrimSpace(baseURL) == "" || len(token) < 32 {
		return nil, fmt.Errorf("management URL and runner token are required")
	}
	if client == nil {
		client = &http.Client{Timeout: 35 * time.Second}
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, http: client}, nil
}

func (c *Client) call(ctx context.Context, path string, body, response any) (bool, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		return false, nil
	}
	limited := io.LimitReader(resp.Body, maxPlanSize*2+64*1024)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(limited, 4096))
		return false, fmt.Errorf("management returned %s: %s", resp.Status, strings.TrimSpace(string(message)))
	}
	if response == nil {
		_, _ = io.Copy(io.Discard, limited)
		return true, nil
	}
	if err := json.NewDecoder(limited).Decode(response); err != nil {
		return false, err
	}
	return true, nil
}

func (c *Client) Claim(ctx context.Context, workerID string, lease time.Duration) (*Claim, error) {
	var claim Claim
	ok, err := c.call(ctx, "/api/v1/runner/claim", map[string]any{"workerId": workerID, "leaseSeconds": int(lease.Seconds())}, &claim)
	if err != nil || !ok {
		return nil, err
	}
	return &claim, nil
}

func (c *Client) Renew(ctx context.Context, id, workerID string, lease time.Duration) (bool, error) {
	var response struct {
		CancelRequested bool `json:"cancelRequested"`
	}
	_, err := c.call(ctx, "/api/v1/runner/jobs/"+id+"/renew", map[string]any{"workerId": workerID, "leaseSeconds": int(lease.Seconds())}, &response)
	return response.CancelRequested, err
}

func (c *Client) Plan(ctx context.Context, id, workerID string, claim *Claim, planText string, plan []byte, planHash string) error {
	_, err := c.call(ctx, "/api/v1/runner/jobs/"+id+"/plan", map[string]any{"workerId": workerID, "artifactSha256": claim.ArtifactSHA256, "planSha256": planHash, "planText": planText, "plan": plan}, nil)
	return err
}

func (c *Client) Step(ctx context.Context, id, workerID, name, status, detail string) error {
	_, err := c.call(ctx, "/api/v1/runner/jobs/"+id+"/step", map[string]any{"workerId": workerID, "name": name, "status": status, "detail": detail}, nil)
	return err
}

func (c *Client) Event(ctx context.Context, id, level, message string) error {
	_, err := c.call(ctx, "/api/v1/runner/jobs/"+id+"/event", map[string]any{"level": level, "message": message}, nil)
	return err
}

func (c *Client) Complete(ctx context.Context, id, workerID, status, errorCode string) error {
	_, err := c.call(ctx, "/api/v1/runner/jobs/"+id+"/complete", map[string]any{"workerId": workerID, "status": status, "errorCode": errorCode}, nil)
	return err
}
