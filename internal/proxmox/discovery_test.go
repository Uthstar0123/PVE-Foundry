package proxmox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInventoryDiscoversTemplatesAndNextVMID(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data any
		switch r.URL.Path {
		case "/api2/json/version":
			data = Version{Version: "9.2"}
		case "/api2/json/nodes":
			data = []Node{{Node: "node-a", Status: "online"}}
		case "/api2/json/cluster/resources":
			data = []Resource{{VMID: 900, Name: "cloud", Type: "qemu", Node: "node-a", Template: 1}}
		case "/api2/json/pools":
			data = []Pool{{PoolID: "lab"}}
		case "/api2/json/nodes/node-a/storage":
			data = []Storage{{Storage: "local", Content: "vztmpl", Active: 1, Enabled: 1}}
		case "/api2/json/nodes/node-a/network":
			data = []Network{{Iface: "vmbr0", Type: "bridge", Active: 1}}
		case "/api2/json/cluster/nextid":
			data = "501"
		case "/api2/json/nodes/node-a/storage/local/content":
			data = []map[string]any{{"volid": "local:vztmpl/debian.tar.zst", "name": "debian.tar.zst"}}
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
	if inventory.NextVMID != 501 || len(inventory.Templates) != 2 {
		t.Fatalf("unexpected discovery: %+v", inventory)
	}
	if inventory.Templates[0].Type != "qemu" || inventory.Templates[1].ID != "local:vztmpl/debian.tar.zst" {
		t.Fatalf("unexpected templates: %+v", inventory.Templates)
	}
}
