package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"proxmox-orchestrator/internal/redact"
	"proxmox-orchestrator/internal/store"
)

const maxPlanSize = 8 << 20

type Management interface {
	Claim(context.Context, string, time.Duration) (*Claim, error)
	Renew(context.Context, string, string, time.Duration) (bool, error)
	Plan(context.Context, string, string, *Claim, string, []byte, string) error
	Step(context.Context, string, string, string, string, string) error
	Event(context.Context, string, string, string) error
	Complete(context.Context, string, string, string, string) error
}

type Config struct {
	WorkerID       string
	OpenTofuBinary string
	WorkRoot       string
	StateRoot      string
	Environment    string
	PollInterval   time.Duration
	Lease          time.Duration
	CommandTimeout time.Duration
}

type Service struct {
	management Management
	cfg        Config
}

func NewService(management Management, cfg Config) (*Service, error) {
	if management == nil || strings.TrimSpace(cfg.WorkerID) == "" || cfg.OpenTofuBinary == "" || cfg.WorkRoot == "" {
		return nil, fmt.Errorf("runner management, worker ID, OpenTofu binary and work root are required")
	}
	if cfg.StateRoot == "" {
		cfg.StateRoot = cfg.WorkRoot + "-state"
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 2 * time.Second
	}
	if cfg.Lease < 30*time.Second || cfg.Lease > 300*time.Second {
		cfg.Lease = 60 * time.Second
	}
	if cfg.CommandTimeout <= 0 {
		cfg.CommandTimeout = 10 * time.Minute
	}
	return &Service{management: management, cfg: cfg}, nil
}

func (s *Service) Run(ctx context.Context) error {
	if err := os.MkdirAll(s.cfg.WorkRoot, 0700); err != nil {
		return fmt.Errorf("create runner work root: %w", err)
	}
	for {
		claim, err := s.management.Claim(ctx, s.cfg.WorkerID, s.cfg.Lease)
		if err != nil && ctx.Err() == nil {
			log.Printf("claim job: %v", err)
		}
		if claim != nil {
			s.execute(ctx, claim)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(s.cfg.PollInterval):
		}
	}
}

func hashBytes(value []byte) string {
	hash := sha256.Sum256(value)
	return hex.EncodeToString(hash[:])
}

func (s *Service) execute(parent context.Context, claim *Claim) {
	if _, err := uuid.Parse(claim.Job.ID); err != nil || (claim.Mode != "plan" && claim.Mode != "apply") {
		log.Printf("reject malformed claimed job")
		return
	}
	if hashBytes(claim.Artifact) != claim.ArtifactSHA256 {
		s.finish(claim, store.JobFailed, "artifact_hash_mismatch")
		return
	}
	ctx, cancel := context.WithTimeout(parent, s.cfg.CommandTimeout)
	defer cancel()
	cancelRequested := atomic.Bool{}
	leaseLost := atomic.Bool{}
	stopRenew := make(chan struct{})
	renewDone := make(chan struct{})
	go func() {
		defer close(renewDone)
		ticker := time.NewTicker(s.cfg.Lease / 3)
		defer ticker.Stop()
		for {
			select {
			case <-stopRenew:
				return
			case <-ticker.C:
				cancelled, err := s.management.Renew(ctx, claim.Job.ID, s.cfg.WorkerID, s.cfg.Lease)
				if err != nil {
					leaseLost.Store(true)
					cancel()
					return
				}
				if cancelled {
					cancelRequested.Store(true)
					cancel()
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	defer func() { close(stopRenew); <-renewDone }()

	root := filepath.Join(s.cfg.WorkRoot, claim.Job.ID)
	defer os.RemoveAll(root)
	if err := os.RemoveAll(root); err != nil {
		s.finish(claim, store.JobFailed, "workdir_failed")
		return
	}
	work := filepath.Join(root, "config")
	if err := SecureExtractTarGz(claim.Artifact, work); err != nil {
		s.event(claim, "error", "Artifact rejected: "+err.Error())
		s.finish(claim, store.JobFailed, "artifact_rejected")
		return
	}
	if err := ValidateConfiguration(work); err != nil {
		s.event(claim, "error", "Configuration rejected: "+err.Error())
		s.finish(claim, store.JobFailed, "configuration_rejected")
		return
	}

	var err error
	if claim.Mode == "plan" {
		err = s.plan(ctx, claim, work)
	} else {
		err = s.apply(ctx, claim, work)
	}
	if err == nil {
		if claim.Mode == "apply" {
			s.finish(claim, store.JobSucceeded, "")
		}
		return
	}
	if leaseLost.Load() || parent.Err() != nil {
		return
	}
	if cancelRequested.Load() {
		s.finish(claim, store.JobCancelled, "cancelled_by_user")
		return
	}
	code := "opentofu_failed"
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		code = "opentofu_timeout"
	}
	s.event(claim, "error", err.Error())
	s.finish(claim, store.JobFailed, code)
}

func (s *Service) plan(ctx context.Context, claim *Claim, work string) error {
	if err := s.step(ctx, claim, "init", func() error { return s.tofuInit(ctx, claim, work) }); err != nil {
		return err
	}
	if err := s.step(ctx, claim, "validate", func() error { return s.tofu(ctx, claim, work, "validate", "-no-color") }); err != nil {
		return err
	}
	planPath := filepath.Join(work, "saved.tfplan")
	if err := s.step(ctx, claim, "plan", func() error { return s.tofu(ctx, claim, work, "plan", "-input=false", "-no-color", "-out="+planPath) }); err != nil {
		return err
	}
	plan, err := readLimited(planPath, maxPlanSize)
	if err != nil {
		return err
	}
	var output strings.Builder
	output.WriteString("tofu show -no-color saved.tfplan\n")
	if err := s.tofuOutput(ctx, claim, work, &output, "show", "-no-color", planPath); err != nil {
		return errors.New(output.String())
	}
	if err := s.management.Plan(ctx, claim.Job.ID, s.cfg.WorkerID, claim, output.String(), plan, hashBytes(plan)); err != nil {
		return fmt.Errorf("save plan: %w", err)
	}
	s.event(claim, "info", "План сохранён. Для Apply требуется подтверждение пользователя в течение ограниченного срока.")
	return nil
}

func (s *Service) apply(ctx context.Context, claim *Claim, work string) error {
	if hashBytes(claim.Plan) != claim.PlanSHA256 || len(claim.Plan) == 0 {
		return fmt.Errorf("saved plan hash mismatch")
	}
	planPath := filepath.Join(work, "saved.tfplan")
	if err := os.WriteFile(planPath, claim.Plan, 0600); err != nil {
		return err
	}
	if err := s.step(ctx, claim, "apply_init", func() error { return s.tofuInit(ctx, claim, work) }); err != nil {
		return err
	}
	if err := s.step(ctx, claim, "apply_validate", func() error { return s.tofu(ctx, claim, work, "validate", "-no-color") }); err != nil {
		return err
	}
	return s.step(ctx, claim, "apply", func() error { return s.tofu(ctx, claim, work, "apply", "-input=false", "-no-color", planPath) })
}

func (s *Service) tofuInit(ctx context.Context, claim *Claim, work string) error {
	stateDirectory := filepath.Join(s.cfg.StateRoot, hashBytes([]byte(claim.Job.ResourceID)))
	if err := os.MkdirAll(stateDirectory, 0700); err != nil {
		return fmt.Errorf("create deployment state directory: %w", err)
	}
	statePath := filepath.Join(stateDirectory, "terraform.tfstate")
	return s.tofu(ctx, claim, work, "init", "-input=false", "-no-color", "-backend-config=path="+statePath)
}

func (s *Service) step(ctx context.Context, claim *Claim, name string, operation func() error) error {
	if err := s.management.Step(ctx, claim.Job.ID, s.cfg.WorkerID, name, store.JobRunning, ""); err != nil {
		return err
	}
	err := operation()
	status, detail := store.JobSucceeded, ""
	if err != nil {
		status, detail = store.JobFailed, err.Error()
	}
	if reportErr := s.management.Step(context.Background(), claim.Job.ID, s.cfg.WorkerID, name, status, detail); reportErr != nil {
		return reportErr
	}
	return err
}

func (s *Service) tofu(ctx context.Context, claim *Claim, work string, args ...string) error {
	var output strings.Builder
	output.WriteString("tofu " + strings.Join(args, " ") + "\n")
	err := s.tofuOutput(ctx, claim, work, &output, args...)
	s.event(claim, map[bool]string{true: "error", false: "info"}[err != nil], output.String())
	return err
}

func (s *Service) tofuOutput(ctx context.Context, _ *Claim, work string, output *strings.Builder, args ...string) error {
	command := s.command(ctx, work, args...)
	writer := &limitedWriter{writer: output, remaining: 1 << 20}
	command.Stdout, command.Stderr = writer, writer
	if err := command.Run(); err != nil {
		return fmt.Errorf("OpenTofu %s failed: %w", args[0], err)
	}
	return nil
}

func (s *Service) event(claim *Claim, level, message string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.management.Event(ctx, claim.Job.ID, level, redact.Text(message))
}

func (s *Service) finish(claim *Claim, status, code string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.management.Complete(ctx, claim.Job.ID, s.cfg.WorkerID, status, code); err != nil {
		log.Printf("complete job %s: %v", claim.Job.ID, err)
	}
}

func readLimited(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	value, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(value)) > limit {
		return nil, fmt.Errorf("file exceeds %d bytes", limit)
	}
	return value, nil
}

type limitedWriter struct {
	writer    io.Writer
	remaining int
}

func (w *limitedWriter) Write(value []byte) (int, error) {
	original := len(value)
	if w.remaining > 0 {
		if len(value) > w.remaining {
			value = value[:w.remaining]
		}
		_, _ = w.writer.Write(value)
		w.remaining -= len(value)
	}
	return original, nil
}
