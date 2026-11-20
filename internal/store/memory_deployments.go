package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"strings"
	"time"
)

const MaxPlanAge = 15 * time.Minute

func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == strings.ToLower(value)
}

func cloneBytes(value []byte) []byte { return append([]byte(nil), value...) }

func (m *Memory) DeploymentArtifact(_ context.Context, id string) (DeploymentArtifact, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	artifact, ok := m.artifacts[id]
	if !ok {
		return DeploymentArtifact{}, sql.ErrNoRows
	}
	artifact.Artifact = cloneBytes(artifact.Artifact)
	artifact.PlanFile = cloneBytes(artifact.PlanFile)
	return artifact, nil
}

func (m *Memory) SavePlan(_ context.Context, id, workerID string, result PlanResult) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, err := m.ownedRunningJob(id, workerID)
	if err != nil {
		return err
	}
	artifact, ok := m.artifacts[id]
	if !ok || artifact.ArtifactSHA256 != result.ArtifactSHA256 || !validSHA256(result.PlanSHA256) || result.PlanText == "" || len(result.PlanFile) == 0 {
		return ErrPlanChanged
	}
	if job.CancelRequestedAt != nil {
		return ErrInvalidJobTransition
	}
	now := time.Now().UTC()
	job.Status = JobWaitingApproval
	job.WorkerID = ""
	job.LeaseExpiresAt = nil
	job.UpdatedAt = now
	m.jobs[id] = job
	m.artifacts[id] = DeploymentArtifact{Artifact: cloneBytes(artifact.Artifact), ArtifactSHA256: artifact.ArtifactSHA256, PlanFile: cloneBytes(result.PlanFile), PlanSHA256: result.PlanSHA256}
	m.plans[id] = result.PlanText
	m.planCreated[id] = now
	return nil
}

func (m *Memory) ApprovePlan(_ context.Context, id, userID, artifactHash string, ttl time.Duration) (JobDetail, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[id]
	if !ok {
		return JobDetail{}, sql.ErrNoRows
	}
	artifact := m.artifacts[id]
	created := m.planCreated[id]
	if job.Status != JobWaitingApproval || artifact.ArtifactSHA256 != artifactHash {
		return JobDetail{}, ErrPlanChanged
	}
	if ttl <= 0 || ttl > MaxPlanAge || created.IsZero() || time.Since(created) > MaxPlanAge {
		return JobDetail{}, ErrApprovalExpired
	}
	now := time.Now().UTC()
	expires := created.Add(MaxPlanAge)
	if expires.After(now.Add(ttl)) {
		expires = now.Add(ttl)
	}
	m.approvals[id] = approval{userID: userID, approvedAt: now, expiresAt: expires}
	steps := append([]JobStep(nil), m.steps[id]...)
	events := append([]JobEvent(nil), m.events[id]...)
	return JobDetail{Job: cloneJob(job), Steps: steps, Events: events, ArtifactSHA256: artifact.ArtifactSHA256, PlanText: m.plans[id], PlanSHA256: artifact.PlanSHA256, ApprovalExpiresAt: &expires, ApprovedAt: &now}, nil
}

func (m *Memory) ClaimApprovedJob(_ context.Context, workerID string, lease time.Duration) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if strings.TrimSpace(workerID) == "" || lease <= 0 {
		return Job{}, ErrInvalidJobTransition
	}
	jobs, _ := m.listJobsLocked()
	now := time.Now().UTC()
	for _, candidate := range jobs {
		approval, ok := m.approvals[candidate.ID]
		if candidate.Status != JobWaitingApproval || !ok || !approval.expiresAt.After(now) {
			continue
		}
		job := m.jobs[candidate.ID]
		expires := now.Add(lease)
		started := now
		job.Status, job.WorkerID, job.LeaseExpiresAt, job.StartedAt, job.UpdatedAt = JobRunning, workerID, &expires, &started, now
		m.jobs[job.ID] = job
		return cloneJob(job), nil
	}
	return Job{}, sql.ErrNoRows
}
