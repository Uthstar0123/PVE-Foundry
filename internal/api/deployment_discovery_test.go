package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

	"proxmox-orchestrator/internal/proxmox"
	"proxmox-orchestrator/internal/store"
)

type deploymentInventoryClient struct{ inventory proxmox.Inventory }

func (client deploymentInventoryClient) Inventory(context.Context) (proxmox.Inventory, error) {
	return client.inventory, nil
}

func TestDeploymentDiscoveryAndPlainFileEndpoints(t *testing.T) {
	inventory := proxmox.Inventory{NextVMID: 500, Nodes: []proxmox.Node{{Node: "node-a", Status: "online"}}, Pools: []proxmox.Pool{{PoolID: "lab"}}, Storage: []proxmox.Storage{{Node: "node-a", Storage: "data", Content: "rootdir", Active: 1, Enabled: 1}}, Networks: []proxmox.Network{{Node: "node-a", Iface: "vmbr0", Type: "bridge", Active: 1}}}
	handler, err := NewManagementHandlerWithConfig(store.NewMemory(), bytes.Repeat([]byte{1}, 32), false, ManagementConfig{RunnerToken: strings.Repeat("r", 32), EncryptionKey: bytes.Repeat([]byte{2}, 32)}, deploymentInventoryClient{inventory})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	server.Client().Jar = jar
	client := server.Client()
	csrf := csrfFor(t, client, server.URL)
	response := postJSON(t, client, server.URL+"/api/v1/setup", csrf, setupInput())
	response.Body.Close()

	response, err = client.Get(server.URL + "/api/v1/deployments/defaults")
	if err != nil {
		t.Fatal(err)
	}
	var defaults struct {
		NodeName string `json:"nodeName"`
		NextVMID int `json:"nextVmid"`
	}
	if err := json.NewDecoder(response.Body).Decode(&defaults); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || defaults.NodeName != "node-a" || defaults.NextVMID != 500 {
		t.Fatalf("defaults: status=%d body=%+v", response.StatusCode, defaults)
	}

	configuration := []byte("variable \"node_name\" { type = string }\nvariable \"custom_role\" { type = string }\n")
	files := []map[string]string{{"name": "main.tf", "content": base64.StdEncoding.EncodeToString(configuration)}}
	response = postJSON(t, client, server.URL+"/api/v1/deployments/opentofu/preflight", csrf, map[string]any{"files": files})
	var preflight struct {
		Variables []struct {
			Name string `json:"name"`
			Required bool `json:"required"`
			Source string `json:"source"`
		} `json:"variables"`
	}
	if err := json.NewDecoder(response.Body).Decode(&preflight); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || len(preflight.Variables) != 2 || preflight.Variables[1].Required || !preflight.Variables[0].Required {
		t.Fatalf("preflight: status=%d body=%+v", response.StatusCode, preflight)
	}

	response = postJSON(t, client, server.URL+"/api/v1/deployments/opentofu", csrf, map[string]any{"resourceId": "deployment/files", "idempotencyKey": "files-1", "files": files, "variables": map[string]any{"node_name": "node-a", "custom_role": "database"}})
	response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("plain file deployment status: %d", response.StatusCode)
	}
}

func TestDeploymentDefaultsRequireProxmox(t *testing.T) {
	server, _, _ := deploymentServer(t)
	response, err := server.Client().Get(server.URL + "/api/v1/deployments/defaults")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("defaults without Proxmox: %d", response.StatusCode)
	}
}
