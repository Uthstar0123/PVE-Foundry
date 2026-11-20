package store

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"time"

	"proxmox-orchestrator/internal/redact"
)

const (
	maxStoredJobEvents = 1000
	jobEventsPageSize  = 200
)

func cloneJob(job Job) Job {
	if job.LeaseExpiresAt != nil {
		value := *job.LeaseExpiresAt
		job.LeaseExpiresAt = &value
	}
	if job.CancelRequestedAt != nil {
		value := *job.CancelRequestedAt
		job.CancelRequestedAt = &value
	}
	if job.StartedAt != nil {
		value := *job.StartedAt
		job.StartedAt = &value
	}
	if job.FinishedAt != nil {
		value := *job.FinishedAt
		job.FinishedAt = &value
	}
	return job
}

func normalizeLimit(limit, fallback int) int {
	if limit <= 0 || limit > fallback {
		return fallback
	}
	return limit
}

func validJobFinalStatus(status string) bool {
	return status == JobSucceeded || status == JobFailed || status == JobCancelled
}

func (m *Memory) EnqueueJob(_ context.Context, input EnqueueJobInput) (Job, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.jobs {
		if existing.InitiatorUserID == input.InitiatorUserID && existing.IdempotencyKey == input.IdempotencyKey {
			if existing.RequestHash != input.RequestHash {
				return Job{}, false, ErrIdempotencyConflict
			}
			return cloneJob(existing), false, nil
		}
	}
	if _, locked := m.locks[input.ResourceID]; locked {
		return Job{}, false, ErrResourceLocked
	}
	if input.Status == "" {
		input.Status = JobQueued
	}
	if input.Status != JobQueued && input.Status != JobWaitingApproval {
		return Job{}, false, ErrInvalidJobTransition
	}
	now := time.Now().UTC()
	job := Job{ID: NewUserID(), Operation: input.Operation, ResourceID: input.ResourceID, Status: input.Status,
		InitiatorUserID: input.InitiatorUserID, IdempotencyKey: input.IdempotencyKey, RequestHash: input.RequestHash,
		CreatedAt: now, UpdatedAt: now}
	m.jobs[job.ID] = job
	m.locks[job.ResourceID] = job.ID
	if len(input.Artifact) > 0 {
		if !validSHA256(input.ArtifactSHA256) {
			delete(m.jobs, job.ID)
			delete(m.locks, job.ResourceID)
			return Job{}, false, ErrPlanChanged
		}
		m.artifacts[job.ID] = DeploymentArtifact{Artifact: cloneBytes(input.Artifact), ArtifactSHA256: input.ArtifactSHA256}
	}
	m.Audits = append(m.Audits, "job.queued")
	m.AuditEvents = append(m.AuditEvents, AuditEvent{ID: int64(len(m.AuditEvents) + 1), EventType: "job.queued", UserID: &job.InitiatorUserID, Detail: "job " + job.ID + " queued", CreatedAt: now})
	return cloneJob(job), true, nil
}

func (m *Memory) ListJobs(_ context.Context, limit int) ([]Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	limit = normalizeLimit(limit, 100)
	jobs := make([]Job, 0, len(m.jobs))
	for _, job := range m.jobs {
		jobs = append(jobs, cloneJob(job))
	}
	sort.Slice(jobs, func(i, j int) bool {
		if jobs[i].CreatedAt.Equal(jobs[j].CreatedAt) {
			return jobs[i].ID > jobs[j].ID
		}
		return jobs[i].CreatedAt.After(jobs[j].CreatedAt)
	})
	if len(jobs) > limit {
		jobs = jobs[:limit]
	}
	return jobs, nil
}

func (m *Memory) Job(_ context.Context, id string) (JobDetail, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[id]
	if !ok {
		return JobDetail{}, sql.ErrNoRows
	}
	steps := append([]JobStep(nil), m.steps[id]...)
	events := append([]JobEvent(nil), m.events[id]...)
	if len(events) > jobEventsPageSize {
		events = events[len(events)-jobEventsPageSize:]
	}
	if steps == nil {
		steps = []JobStep{}
	}
	if events == nil {
		events = []JobEvent{}
	}
	detail := JobDetail{Job: cloneJob(job), Steps: steps, Events: events, PlanText: m.plans[id]}
	if artifact, ok := m.artifacts[id]; ok {
		detail.ArtifactSHA256, detail.PlanSHA256 = artifact.ArtifactSHA256, artifact.PlanSHA256
	}
	if approval, ok := m.approvals[id]; ok {
		expires, approved := approval.expiresAt, approval.approvedAt
		detail.ApprovalExpiresAt, detail.ApprovedAt = &expires, &approved
	}
	return detail, nil
}

func (m *Memory) CancelJob(_ context.Context, id string) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[id]
	if !ok {
		return Job{}, sql.ErrNoRows
	}
	now := time.Now().UTC()
	switch job.Status {
	case JobQueued, JobWaitingApproval:
		job.Status = JobCancelled
		job.CancelRequestedAt = &now
		job.FinishedAt = &now
		delete(m.locks, job.ResourceID)
		m.Audits = append(m.Audits, "job.cancelled")
	case JobRunning:
		if job.CancelRequestedAt == nil {
			job.CancelRequestedAt = &now
			m.Audits = append(m.Audits, "job.cancel_requested")
		}
	default:
		return cloneJob(job), nil
	}
	job.UpdatedAt = now
	m.jobs[id] = job
	return cloneJob(job), nil
}

func (m *Memory) ClaimJob(_ context.Context, workerID string, lease time.Duration) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if strings.TrimSpace(workerID) == "" || lease <= 0 {
		return Job{}, ErrInvalidJobTransition
	}
	jobs, _ := m.listJobsLocked()
	for _, candidate := range jobs {
		if candidate.Status != JobQueued {
			continue
		}
		job := m.jobs[candidate.ID]
		now := time.Now().UTC()
		expires := now.Add(lease)
		job.Status, job.WorkerID, job.LeaseExpiresAt, job.StartedAt, job.UpdatedAt = JobRunning, workerID, &expires, &now, now
		m.jobs[job.ID] = job
		return cloneJob(job), nil
	}
	return Job{}, sql.ErrNoRows
}

func (m *Memory) listJobsLocked() ([]Job, error) {
	jobs := make([]Job, 0, len(m.jobs))
	for _, job := range m.jobs {
		jobs = append(jobs, job)
	}
	sort.Slice(jobs, func(i, j int) bool {
		if jobs[i].CreatedAt.Equal(jobs[j].CreatedAt) {
			return jobs[i].ID < jobs[j].ID
		}
		return jobs[i].CreatedAt.Before(jobs[j].CreatedAt)
	})
	return jobs, nil
}

func (m *Memory) RenewLease(_ context.Context, id, workerID string, lease time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[id]
	if !ok {
		return sql.ErrNoRows
	}
	now := time.Now().UTC()
	if job.Status != JobRunning || job.WorkerID != workerID || job.LeaseExpiresAt == nil || !job.LeaseExpiresAt.After(now) || lease <= 0 {
		return ErrLeaseExpired
	}
	expires := now.Add(lease)
	job.LeaseExpiresAt, job.UpdatedAt = &expires, now
	m.jobs[id] = job
	return nil
}

func (m *Memory) ownedRunningJob(id, workerID string) (Job, error) {
	job, ok := m.jobs[id]
	if !ok {
		return Job{}, sql.ErrNoRows
	}
	if job.Status != JobRunning || job.WorkerID != workerID {
		return Job{}, ErrInvalidJobTransition
	}
	return job, nil
}

func (m *Memory) StartStep(_ context.Context, id, workerID, name string) (JobStep, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.ownedRunningJob(id, workerID); err != nil {
		return JobStep{}, err
	}
	for _, step := range m.steps[id] {
		if step.Name == name {
			return step, nil
		}
	}
	now := time.Now().UTC()
	step := JobStep{ID: NewUserID(), JobID: id, Name: name, Status: JobRunning, StartedAt: &now, CreatedAt: now, UpdatedAt: now}
	m.steps[id] = append(m.steps[id], step)
	return step, nil
}

func (m *Memory) CompleteStep(_ context.Context, id, workerID, name, status, detail string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, err := m.ownedRunningJob(id, workerID)
	if err != nil {
		return err
	}
	if status != JobSucceeded && status != JobFailed && status != "skipped" && status != JobCancelled {
		return ErrInvalidJobTransition
	}
	for index := range m.steps[id] {
		if m.steps[id][index].Name != name {
			continue
		}
		now := time.Now().UTC()
		m.steps[id][index].Status = status
		m.steps[id][index].Detail = redact.Text(detail)
		m.steps[id][index].FinishedAt = &now
		m.steps[id][index].UpdatedAt = now
		job.UpdatedAt = now
		m.jobs[id] = job
		return nil
	}
	return sql.ErrNoRows
}

func (m *Memory) CompleteJob(_ context.Context, id, workerID, status, errorCode string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, err := m.ownedRunningJob(id, workerID)
	if err != nil {
		return err
	}
	if !validJobFinalStatus(status) {
		return ErrInvalidJobTransition
	}
	now := time.Now().UTC()
	job.Status, job.ErrorCode, job.FinishedAt, job.UpdatedAt = status, errorCode, &now, now
	m.jobs[id] = job
	delete(m.locks, job.ResourceID)
	m.Audits = append(m.Audits, "job."+status)
	return nil
}

func (m *Memory) AppendEvent(_ context.Context, id, level, message string, secrets ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[id]
	if !ok {
		return sql.ErrNoRows
	}
	switch level {
	case "debug", "info", "warn", "error":
	default:
		level = "info"
	}
	if len(message) > 16384 {
		message = message[:16384]
	}
	m.nextEventID++
	event := JobEvent{ID: m.nextEventID, JobID: id, Level: level, Message: redact.Text(message, secrets...), CreatedAt: time.Now().UTC()}
	m.events[id] = append(m.events[id], event)
	if len(m.events[id]) > maxStoredJobEvents {
		m.events[id] = append([]JobEvent(nil), m.events[id][len(m.events[id])-maxStoredJobEvents:]...)
		job.LogsTruncated = true
	}
	job.UpdatedAt = event.CreatedAt
	m.jobs[id] = job
	return nil
}

func (m *Memory) JobLogs(_ context.Context, id string) ([]JobEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.jobs[id]; !ok {
		return nil, sql.ErrNoRows
	}
	return append([]JobEvent(nil), m.events[id]...), nil
}

func (m *Memory) FailExpiredLeases(context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	failed := 0
	for id, job := range m.jobs {
		if job.Status != JobRunning || job.LeaseExpiresAt == nil || job.LeaseExpiresAt.After(now) {
			continue
		}
		job.Status, job.ErrorCode, job.FinishedAt, job.UpdatedAt = JobFailed, "lease_expired", &now, now
		m.jobs[id] = job
		delete(m.locks, job.ResourceID)
		m.nextEventID++
		m.events[id] = append(m.events[id], JobEvent{ID: m.nextEventID, JobID: id, Level: "error", Message: "Lease expired; operation outcome is unknown and will not be retried automatically.", CreatedAt: now})
		m.Audits = append(m.Audits, "job.lease_expired")
		failed++
	}
	return failed, nil
}

func (m *Memory) ListAudit(_ context.Context, limit int) ([]AuditEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	limit = normalizeLimit(limit, 100)
	start := len(m.AuditEvents) - limit
	if start < 0 {
		start = 0
	}
	events := append([]AuditEvent(nil), m.AuditEvents[start:]...)
	for left, right := 0, len(events)-1; left < right; left, right = left+1, right-1 {
		events[left], events[right] = events[right], events[left]
	}
	return events, nil
}
