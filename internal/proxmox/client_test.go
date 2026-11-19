package proxmox

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestInventoryUsesReadOnlyTokenAndDecodesResources(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "PVEAPIToken=reader@pve!inventory=secret" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		var data any
		switch r.URL.Path {
		case "/api2/json/version":
			data = Version{Version: "9.2.10", Release: "9.2"}
		case "/api2/json/nodes":
			data = []Node{{Node: "pve", Status: "online", MaxCPU: 48}}
		case "/api2/json/cluster/resources":
			data = []Resource{{ID: "qemu/100", VMID: 100, Name: "test", Type: "qemu"}}
		case "/api2/json/pools":
			data = []Pool{{PoolID: "test"}}
		case "/api2/json/nodes/pve/storage":
			data = []Storage{{Storage: "local", Total: 100}}
		case "/api2/json/nodes/pve/network":
			data = []Network{{Iface: "vmbr0", Type: "bridge"}}
		default:
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer server.Close()
	client, err := NewClient(Config{APIURL: server.URL, APIToken: "reader@pve!inventory=secret", InsecureSkipVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := client.Inventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if inventory.Version.Version != "9.2.10" || len(inventory.Nodes) != 1 || len(inventory.Resources) != 1 || len(inventory.Storage) != 1 || len(inventory.Networks) != 1 {
		t.Fatalf("unexpected inventory: %+v", inventory)
	}
}

func TestCustomCAIsTrustedAndSelfSignedIsRejectedByDefault(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": Version{Version: "9.2.10"}})
	}))
	defer server.Close()
	if _, err := NewClient(Config{APIURL: "http://example.test", APIToken: "reader@pve!inventory=secret"}); err == nil {
		t.Fatal("plaintext API URL accepted")
	}
	client, err := NewClient(Config{APIURL: server.URL, APIToken: "reader@pve!inventory=secret"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Inventory(context.Background()); err == nil {
		t.Fatal("untrusted self-signed certificate accepted")
	}
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	caFile := filepath.Join(t.TempDir(), "proxmox-ca.pem")
	if err := os.WriteFile(caFile, certificate, 0o600); err != nil {
		t.Fatal(err)
	}
	trusted, err := NewClient(Config{APIURL: server.URL, APIToken: "reader@pve!inventory=secret", CAFile: caFile})
	if err != nil {
		t.Fatal(err)
	}
	var version Version
	if err := trusted.get(context.Background(), "version", &version); err != nil || version.Version != "9.2.10" {
		t.Fatalf("custom CA failed: %+v %v", version, err)
	}
}

func TestAuthenticationErrorsAreActionable(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
	defer server.Close()
	client, _ := NewClient(Config{APIURL: server.URL, APIToken: "reader@pve!inventory=secret", InsecureSkipVerify: true})
	var version Version
	if err := client.get(context.Background(), "version", &version); err == nil {
		t.Fatal("HTTP 401 accepted")
	}
}
