package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"proxmox-orchestrator/internal/store"
)

type stateManagement struct {
	fakeManagement
	events []string
}

func (m *stateManagement) Event(_ context.Context, _, level, message string) error {
	m.events = append(m.events, level+": "+message)
	return nil
}

func stateFakeOpenTofu(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	source := filepath.Join(directory, "main.go")
	program := `package main
import("fmt";"os";"strings")
func main(){
 switch os.Args[1] {
 case "init":
  for _,v:=range os.Args {if strings.HasPrefix(v,"-backend-config=path="){if err:=os.WriteFile(strings.TrimPrefix(v,"-backend-config=path="),[]byte("persistent-state"),0600);err!=nil{panic(err)}}}
 case "plan":
  for _,v:=range os.Args {if strings.HasPrefix(v,"-out="){if err:=os.WriteFile(strings.TrimPrefix(v,"-out="),[]byte("plan-binary"),0600);err!=nil{panic(err)}}}
 case "show": fmt.Print("1 to change")
 case "apply":
  if _,err:=os.Stat(os.Args[len(os.Args)-1]);err!=nil{os.Exit(3)}
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
		t.Fatalf("build state fake OpenTofu: %v: %s", err, output)
	}
	return binary
}

func TestServiceKeepsResourceStateOutsideWorkdir(t *testing.T) {
	management := &stateManagement{}
	workRoot, stateRoot := t.TempDir(), t.TempDir()
	service, err := NewService(management, Config{WorkerID: "runner-test", OpenTofuBinary: stateFakeOpenTofu(t), WorkRoot: workRoot, StateRoot: stateRoot, CommandTimeout: 10 * time.Second})
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
	claim := &Claim{Job: store.Job{ID: "11111111-1111-4111-8111-111111111111", ResourceID: "deployment-alpha"}, Mode: "plan", Artifact: artifact, ArtifactSHA256: hashBytes(artifact)}
	service.execute(context.Background(), claim)
	if len(management.plan) == 0 {
		t.Fatalf("plan was not saved: text=%q completed=%v events=%v", management.planText, management.completed, management.events)
	}
	statePath := filepath.Join(stateRoot, hashBytes([]byte(claim.Job.ResourceID)), "terraform.tfstate")
	if state, err := os.ReadFile(statePath); err != nil || string(state) != "persistent-state" {
		t.Fatalf("resource state after plan: state=%q err=%v", state, err)
	}

	apply := &Claim{Job: claim.Job, Mode: "apply", Artifact: artifact, ArtifactSHA256: claim.ArtifactSHA256, Plan: management.plan, PlanSHA256: hashBytes(management.plan)}
	service.execute(context.Background(), apply)
	if len(management.completed) != 1 || management.completed[0] != store.JobSucceeded+":" {
		t.Fatalf("apply completion: %v", management.completed)
	}
	if state, err := os.ReadFile(statePath); err != nil || string(state) != "persistent-state" {
		t.Fatalf("resource state after apply: state=%q err=%v", state, err)
	}
	if _, err := os.Stat(filepath.Join(workRoot, claim.Job.ID)); !os.IsNotExist(err) {
		t.Fatalf("job workdir was not cleaned: %v", err)
	}
}
