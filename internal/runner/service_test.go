package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"proxmox-orchestrator/internal/store"
)

type fakeManagement struct {
	planText  string
	plan      []byte
	completed []string
}

func (f *fakeManagement) Claim(context.Context, string, time.Duration) (*Claim, error) {
	return nil, nil
}
func (f *fakeManagement) Renew(context.Context, string, string, time.Duration) (bool, error) {
	return false, nil
}
func (f *fakeManagement) Plan(_ context.Context, _, _ string, _ *Claim, text string, plan []byte, _ string) error {
	f.planText, f.plan = text, append([]byte(nil), plan...)
	return nil
}
func (f *fakeManagement) Step(context.Context, string, string, string, string, string) error {
	return nil
}
func (f *fakeManagement) Event(context.Context, string, string, string) error { return nil }
func (f *fakeManagement) Complete(_ context.Context, _, _, status, code string) error {
	f.completed = append(f.completed, status+":"+code)
	return nil
}

func fakeOpenTofu(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	source := filepath.Join(directory, "main.go")
	program := `package main
import("fmt";"os";"strings")
func main(){
 a:=os.Args[1]; if len(os.Args)>1 {a=os.Args[1]}
 switch a {
 case "plan":
  for _,v:=range os.Args {if strings.HasPrefix(v,"-out="){if err:=os.WriteFile(strings.TrimPrefix(v,"-out="),[]byte("plan-binary"),0600);err!=nil{panic(err)}}}
 case "show": fmt.Print("1 to change")
 case "apply":
  if len(os.Args)<2 {os.Exit(2)}; if _,err:=os.Stat(os.Args[len(os.Args)-1]);err!=nil{os.Exit(3)}
 }
}`
	if err := os.WriteFile(source, []byte(program), 0600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(directory, "tofu")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	command := exec.Command("go", "build", "-o", binary, source)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build fake OpenTofu: %v: %s", err, output)
	}
	return binary
}

func TestServicePlanThenApprovedApply(t *testing.T) {
	management := &fakeManagement{}
	service, err := NewService(management, Config{WorkerID: "runner-test", OpenTofuBinary: fakeOpenTofu(t), WorkRoot: t.TempDir(), CommandTimeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	artifact := buildTarGz(t, []tfFile{{name: "main.tf", body: `terraform {
  required_providers {
    proxmox = {
      source = "bpg/proxmox"
      version = "0.123.0"
    }
  }
}`}})
	claim := &Claim{Job: store.Job{ID: "11111111-1111-4111-8111-111111111111"}, Mode: "plan", Artifact: artifact, ArtifactSHA256: hashBytes(artifact)}
	service.execute(context.Background(), claim)
	if len(management.plan) == 0 || !strings.Contains(management.planText, "1 to change") || len(management.completed) != 0 {
		t.Fatalf("plan result: text=%q completed=%v", management.planText, management.completed)
	}
	apply := &Claim{Job: claim.Job, Mode: "apply", Artifact: artifact, ArtifactSHA256: claim.ArtifactSHA256, Plan: management.plan, PlanSHA256: hashBytes(management.plan)}
	service.execute(context.Background(), apply)
	if len(management.completed) != 1 || management.completed[0] != store.JobSucceeded+":" {
		t.Fatalf("apply completion: %v", management.completed)
	}
	if _, err := os.Stat(filepath.Join(service.cfg.WorkRoot, claim.Job.ID)); !os.IsNotExist(err) {
		t.Fatalf("job workdir was not cleaned: %v", err)
	}
}

func TestServiceRejectsArtifactSubstitution(t *testing.T) {
	management := &fakeManagement{}
	service, _ := NewService(management, Config{WorkerID: "runner-test", OpenTofuBinary: "unused", WorkRoot: t.TempDir()})
	claim := &Claim{Job: store.Job{ID: "11111111-1111-4111-8111-111111111111"}, Mode: "plan", Artifact: []byte("changed"), ArtifactSHA256: hashBytes([]byte("original"))}
	service.execute(context.Background(), claim)
	if len(management.completed) != 1 || management.completed[0] != store.JobFailed+":artifact_hash_mismatch" {
		t.Fatalf("substitution result: %v", management.completed)
	}
}
