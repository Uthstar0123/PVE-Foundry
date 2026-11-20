package store

import (
	"context"
	"errors"
	"time"
)

const (
	JobQueued          = "queued"
	JobWaitingApproval = "waiting_approval"
	JobRunning         = "running"
	JobSucceeded       = "succeeded"
	JobFailed          = "failed"
	JobCancelled       = "cancelled"
)

var (
	ErrIdempotencyConflict  = errors.New("idempotency key already belongs to a different request")
	ErrResourceLocked       = errors.New("resource is locked by another active job")
	ErrInvalidJobTransition = errors.New("invalid job state transition")
	ErrLeaseExpired         = errors.New("job lease is expired or owned by another worker")
	ErrApprovalExpired      = errors.New("plan approval is expired")
	ErrPlanChanged          = errors.New("configuration changed after planning")
)

type Job struct {
	ID                string     `json:"id"`
	Operation         string     `json:"operation"`
	ResourceID        string     `json:"resourceId"`
	Status            string     `json:"status"`
	InitiatorUserID   string     `json:"initiatorUserId"`
	IdempotencyKey    string     `json:"-"`
	RequestHash       string     `json:"-"`
	WorkerID          string     `json:"workerId,omitempty"`
	LeaseExpiresAt    *time.Time `json:"leaseExpiresAt,omitempty"`
	CancelRequestedAt *time.Time `json:"cancelRequestedAt,omitempty"`
	ErrorCode         string     `json:"errorCode,omitempty"`
	LogsTruncated     bool       `json:"logsTruncated"`
	CreatedAt         time.Time  `json:"createdAt"`
	StartedAt         *time.Time `json:"startedAt,omitempty"`
	FinishedAt        *time.Time `json:"finishedAt,omitempty"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

type JobStep struct {
	ID         string     `json:"id"`
	JobID      string     `json:"jobId"`
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	Detail     string     `json:"detail"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
}

type JobEvent struct {
	ID        int64     `json:"id"`
	JobID     string    `json:"jobId"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"createdAt"`
}

type JobDetail struct {
	Job               Job        `json:"job"`
	Steps             []JobStep  `json:"steps"`
	Events            []JobEvent `json:"events"`
	ArtifactSHA256    string     `json:"artifactSha256,omitempty"`
	PlanText          string     `json:"planText,omitempty"`
	PlanSHA256        string     `json:"planSha256,omitempty"`
	ApprovalExpiresAt *time.Time `json:"approvalExpiresAt,omitempty"`
	ApprovedAt        *time.Time `json:"approvedAt,omitempty"`
}

type EnqueueJobInput struct {
	Operation       string
	ResourceID      string
	InitiatorUserID string
	IdempotencyKey  string
	RequestHash     string
	Status          string
	Artifact        []byte
	ArtifactSHA256  string
}

type DeploymentArtifact struct {
	Artifact       []byte
	ArtifactSHA256 string
	PlanFile       []byte
	PlanSHA256     string
}

type PlanResult struct {
	PlanText       string
	PlanFile       []byte
	PlanSHA256     string
	ArtifactSHA256 string
}

type AuditEvent struct {
	ID        int64     `json:"id"`
	EventType string    `json:"eventType"`
	UserID    *string   `json:"userId,omitempty"`
	SourceIP  string    `json:"sourceIp"`
	Detail    string    `json:"detail"`
	CreatedAt time.Time `json:"createdAt"`
}

type JobStore interface {
	EnqueueJob(context.Context, EnqueueJobInput) (Job, bool, error)
	ListJobs(context.Context, int) ([]Job, error)
	Job(context.Context, string) (JobDetail, error)
	CancelJob(context.Context, string) (Job, error)
	ClaimJob(context.Context, string, time.Duration) (Job, error)
	ClaimApprovedJob(context.Context, string, time.Duration) (Job, error)
	DeploymentArtifact(context.Context, string) (DeploymentArtifact, error)
	SavePlan(context.Context, string, string, PlanResult) error
	ApprovePlan(context.Context, string, string, string, time.Duration) (JobDetail, error)
	RenewLease(context.Context, string, string, time.Duration) error
	StartStep(context.Context, string, string, string) (JobStep, error)
	CompleteStep(context.Context, string, string, string, string, string) error
	CompleteJob(context.Context, string, string, string, string) error
	AppendEvent(context.Context, string, string, string, ...string) error
	JobLogs(context.Context, string) ([]JobEvent, error)
	FailExpiredLeases(context.Context) (int, error)
	ListAudit(context.Context, int) ([]AuditEvent, error)
}
