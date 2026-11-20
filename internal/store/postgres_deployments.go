package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

func (p *Postgres) DeploymentArtifact(ctx context.Context, id string) (DeploymentArtifact, error) {
	var artifact DeploymentArtifact
	err := p.db.QueryRowContext(ctx, `SELECT artifact,artifact_sha256,COALESCE(plan_file,''),COALESCE(plan_sha256,'') FROM deployment_artifacts WHERE job_id=$1`, id).Scan(
		&artifact.Artifact, &artifact.ArtifactSHA256, &artifact.PlanFile, &artifact.PlanSHA256)
	return artifact, err
}

func (p *Postgres) SavePlan(ctx context.Context, id, workerID string, result PlanResult) error {
	if !validSHA256(result.PlanSHA256) || result.PlanText == "" || len(result.PlanFile) == 0 {
		return ErrPlanChanged
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	job, err := ownedRunningJob(ctx, tx, id, workerID)
	if err != nil {
		return err
	}
	if job.CancelRequestedAt != nil {
		return ErrInvalidJobTransition
	}
	var artifactHash string
	if err := tx.QueryRowContext(ctx, `SELECT artifact_sha256 FROM deployment_artifacts WHERE job_id=$1 FOR UPDATE`, id).Scan(&artifactHash); err != nil {
		return err
	}
	if artifactHash != result.ArtifactSHA256 {
		return ErrPlanChanged
	}
	if _, err = tx.ExecContext(ctx, `UPDATE deployment_artifacts SET plan_text=$2,plan_file=$3,plan_sha256=$4,plan_created_at=now(),approval_expires_at=NULL,approved_by=NULL,approved_at=NULL WHERE job_id=$1`, id, result.PlanText, result.PlanFile, result.PlanSHA256); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE jobs SET status='waiting_approval',worker_id=NULL,lease_expires_at=NULL,updated_at=now() WHERE id=$1`, id); err != nil {
		return err
	}
	if err = recordAudit(ctx, tx, "job.plan_ready", &job.InitiatorUserID, "job "+id+" plan saved; approval required"); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *Postgres) ApprovePlan(ctx context.Context, id, userID, artifactHash string, ttl time.Duration) (JobDetail, error) {
	if ttl <= 0 || ttl > MaxPlanAge {
		return JobDetail{}, ErrApprovalExpired
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return JobDetail{}, err
	}
	defer tx.Rollback()
	var status, storedHash string
	var planCreated time.Time
	err = tx.QueryRowContext(ctx, `SELECT j.status,a.artifact_sha256,a.plan_created_at FROM jobs j JOIN deployment_artifacts a ON a.job_id=j.id WHERE j.id=$1 FOR UPDATE OF j,a`, id).Scan(&status, &storedHash, &planCreated)
	if err != nil {
		return JobDetail{}, err
	}
	if status != JobWaitingApproval || storedHash != artifactHash {
		return JobDetail{}, ErrPlanChanged
	}
	var expires time.Time
	err = tx.QueryRowContext(ctx, `SELECT LEAST(now()+$1::interval,$2::timestamptz+$3::interval)`, fmt.Sprintf("%f seconds", ttl.Seconds()), planCreated, fmt.Sprintf("%f seconds", MaxPlanAge.Seconds())).Scan(&expires)
	if err != nil {
		return JobDetail{}, err
	}
	if !expires.After(time.Now().UTC()) {
		return JobDetail{}, ErrApprovalExpired
	}
	if _, err = tx.ExecContext(ctx, `UPDATE deployment_artifacts SET approved_by=$2,approved_at=now(),approval_expires_at=$3 WHERE job_id=$1`, id, userID, expires); err != nil {
		return JobDetail{}, err
	}
	if err = recordAudit(ctx, tx, "job.plan_approved", &userID, "job "+id+" apply approved for artifact "+artifactHash); err != nil {
		return JobDetail{}, err
	}
	if err = tx.Commit(); err != nil {
		return JobDetail{}, err
	}
	return p.Job(ctx, id)
}

func (p *Postgres) ClaimApprovedJob(ctx context.Context, workerID string, lease time.Duration) (Job, error) {
	if strings.TrimSpace(workerID) == "" || lease <= 0 {
		return Job{}, ErrInvalidJobTransition
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(ctx, `SELECT j.id FROM jobs j JOIN deployment_artifacts a ON a.job_id=j.id WHERE j.status='waiting_approval' AND a.approved_at IS NOT NULL AND a.approval_expires_at>now() ORDER BY a.approved_at,j.id LIMIT 1 FOR UPDATE OF j SKIP LOCKED`).Scan(&id)
	if err != nil {
		return Job{}, err
	}
	job, err := scanJob(tx.QueryRowContext(ctx, `UPDATE jobs SET status='running',worker_id=$2,lease_expires_at=now()+$3::interval,started_at=now(),updated_at=now() WHERE id=$1 `+jobReturning, id, workerID, fmt.Sprintf("%f seconds", lease.Seconds())))
	if err != nil {
		return Job{}, err
	}
	var userID string
	if err = tx.QueryRowContext(ctx, `SELECT approved_by FROM deployment_artifacts WHERE job_id=$1`, id).Scan(&userID); err != nil {
		return Job{}, err
	}
	if err = recordAudit(ctx, tx, "job.apply_started", &userID, "job "+id+" claimed for approved apply by "+workerID); err != nil {
		return Job{}, err
	}
	return job, tx.Commit()
}

func (p *Postgres) deploymentDetail(ctx context.Context, detail *JobDetail, id string) error {
	var approvalExpires, approvedAt sql.NullTime
	err := p.db.QueryRowContext(ctx, `SELECT artifact_sha256,COALESCE(plan_text,''),COALESCE(plan_sha256,''),approval_expires_at,approved_at FROM deployment_artifacts WHERE job_id=$1`, id).Scan(
		&detail.ArtifactSHA256, &detail.PlanText, &detail.PlanSHA256, &approvalExpires, &approvedAt)
	if err == sql.ErrNoRows {
		return nil
	}
	if approvalExpires.Valid {
		detail.ApprovalExpiresAt = &approvalExpires.Time
	}
	if approvedAt.Valid {
		detail.ApprovedAt = &approvedAt.Time
	}
	return err
}
