package deployment

import (
	"encoding/base64"
	"strings"
	"testing"

	"proxmox-orchestrator/internal/proxmox"
	"proxmox-orchestrator/internal/runner"
)

func TestPreflightUsesInventoryAndKeepsUnknownRequired(t *testing.T) {
	files := []File{{Name: "main.tf", Content: []byte(`
variable "node_name" { type = string }
variable "vm_id" { type = number }
variable "custom_role" { type = string }
variable "api_token" {
  type = string
  sensitive = true
}
variable "optional" {
  type = string
  default = "ready"
}
`)}}
	variables, err := Preflight(files, Defaults{NodeName: "node-a", NextVMID: 500})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Variable{}
	for _, variable := range variables {
		got[variable.Name] = variable
	}
	if got["node_name"].Source != "auto" || got["node_name"].Value != "node-a" || got["vm_id"].Value != 500 {
		t.Fatalf("automatic variables: %+v", variables)
	}
	if !got["custom_role"].Required || !got["api_token"].Required || got["optional"].Required {
		t.Fatalf("required variables: %+v", variables)
	}
}

func TestSingleFileArchivePassesRunnerValidation(t *testing.T) {
	content := []byte(`terraform {
  required_providers {
    proxmox = {
      source = "bpg/proxmox"
      version = "0.123.0"
    }
  }
}
variable "node_name" { type = string }
`)
	files, err := DecodeFiles([]InputFile{{Name: "main.tf", Content: base64.StdEncoding.EncodeToString(content)}})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Archive(files, map[string]any{"node_name": "node-a"})
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err := runner.SecureExtractTarGz(artifact, directory); err != nil {
		t.Fatal(err)
	}
	if err := runner.ValidateConfiguration(directory); err != nil {
		t.Fatal(err)
	}
}

func TestDesignerGeneratesConnectedLXC(t *testing.T) {
	design := DesignerValue{Name: "deployment/test", Nodes: []DesignerNode{
		{ID: "vm", Kind: "lxc", Label: "foundry-500", Config: map[string]any{"vmid": 500, "nodeName": "node-a", "poolId": "lab", "cores": 2, "memory": 1024, "disk": 8}},
		{ID: "net", Kind: "network", Label: "network", Config: map[string]any{"bridge": "vmbr0"}},
		{ID: "disk", Kind: "storage", Label: "storage", Config: map[string]any{"datastoreId": "data"}},
		{ID: "tpl", Kind: "template", Label: "template", Config: map[string]any{"templateId": "local:vztmpl/debian.tar.zst"}},
	}, Edges: []DesignerEdge{{Source: "net", Target: "vm"}, {Source: "disk", Target: "vm"}, {Source: "tpl", Target: "vm"}}}
	files, err := GenerateDesigner(design)
	if err != nil {
		t.Fatal(err)
	}
	text := string(files[0].Content)
	for _, expected := range []string{"proxmox_virtual_environment_container", "started      = false", "template_file_id = \"local:vztmpl/debian.tar.zst\"", "bridge = \"vmbr0\""} {
		if !strings.Contains(text, expected) {
			t.Fatalf("generated configuration lacks %q:\n%s", expected, text)
		}
	}
	artifact, err := Archive(files, nil)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err := runner.SecureExtractTarGz(artifact, directory); err != nil {
		t.Fatal(err)
	}
	if err := runner.ValidateConfiguration(directory); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultsSelectUsableResources(t *testing.T) {
	defaults := DefaultsFromInventory(proxmox.Inventory{NextVMID: 501,
		Nodes:    []proxmox.Node{{Node: "offline", Status: "offline"}, {Node: "online", Status: "online"}},
		Pools:    []proxmox.Pool{{PoolID: "lab"}},
		Storage:  []proxmox.Storage{{Node: "online", Storage: "data", Content: "images,rootdir", Active: 1, Enabled: 1}},
		Networks: []proxmox.Network{{Node: "online", Iface: "vmbr0", Type: "bridge", Active: 1}},
	})
	if defaults.NodeName != "online" || defaults.DatastoreID != "data" || defaults.Bridge != "vmbr0" || defaults.PoolID != "lab" {
		t.Fatalf("unexpected defaults: %+v", defaults)
	}
}

func TestDesignerRejectsMismatchedTemplateTypes(t *testing.T) {
	baseNodes := func(templateID string) []DesignerNode {
		return []DesignerNode{
			{ID: "vm", Kind: "lxc", Label: "foundry-500", Config: map[string]any{"vmid": 500, "nodeName": "node-a", "cores": 2, "memory": 1024, "disk": 8}},
			{ID: "net", Kind: "network", Label: "network", Config: map[string]any{"bridge": "vmbr0"}},
			{ID: "disk", Kind: "storage", Label: "storage", Config: map[string]any{"datastoreId": "data"}},
			{ID: "tpl", Kind: "template", Label: "template", Config: map[string]any{"templateId": templateID}},
		}
	}
	edges := []DesignerEdge{{Source: "net", Target: "vm"}, {Source: "disk", Target: "vm"}, {Source: "tpl", Target: "vm"}}

	if _, err := GenerateDesigner(DesignerValue{Name: "deployment/test", Nodes: baseNodes("9000"), Edges: edges}); err == nil || !strings.Contains(err.Error(), "LXC-шаблон") {
		t.Fatalf("ожидалась ошибка про LXC-шаблон для QEMU VMID, получено: %v", err)
	}

	qemuNodes := baseNodes("local:vztmpl/debian.tar.zst")
	qemuNodes[0].Kind = "qemu"
	if _, err := GenerateDesigner(DesignerValue{Name: "deployment/test", Nodes: qemuNodes, Edges: edges}); err == nil || !strings.Contains(err.Error(), "QEMU-шаблон") {
		t.Fatalf("ожидалась ошибка про QEMU-шаблон для vztmpl, получено: %v", err)
	}
}
