package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"proxmox-orchestrator/internal/proxmox"
	"proxmox-orchestrator/internal/store"
)

type fakeInventoryClient struct {
	inventory proxmox.Inventory
	err       error
	calls     int
}

func (f *fakeInventoryClient) Inventory(context.Context) (proxmox.Inventory, error) {
	f.calls++
	return f.inventory, f.err
}

func authenticatedInventoryRequest(t *testing.T, database *store.Memory) *http.Request {
	t.Helper()
	user := store.User{ID: "admin-id", Email: "admin@example.local", DisplayName: "Admin", Role: "administrator"}
	if err := database.CreateInitialAdmin(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	const sessionToken = "inventory-test-session"
	if err := database.CreateSession(context.Background(), sha256.Sum256([]byte(sessionToken)), user.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/proxmox/inventory", nil)
	request.AddCookie(&http.Cookie{Name: "orchestrator_session", Value: sessionToken})
	return request
}

func newInventoryTestHandler(t *testing.T, database store.Store, client InventoryClient) http.Handler {
	t.Helper()
	clients := []InventoryClient{}
	if client != nil {
		clients = append(clients, client)
	}
	handler, err := NewManagementHandler(database, make([]byte, 32), false, clients...)
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func TestInventoryRequiresSession(t *testing.T) {
	client := &fakeInventoryClient{}
	handler := newInventoryTestHandler(t, store.NewMemory(), client)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/proxmox/inventory", nil))
	if response.Code != http.StatusUnauthorized || client.calls != 0 {
		t.Fatalf("status=%d calls=%d", response.Code, client.calls)
	}
}

func TestInventoryReturnsReadOnlySnapshot(t *testing.T) {
	database := store.NewMemory()
	client := &fakeInventoryClient{inventory: proxmox.Inventory{
		CollectedAt: time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC),
		Version:     proxmox.Version{Version: "9.2.10"},
		Nodes:       []proxmox.Node{{Node: "pve-test", Status: "online"}},
		Resources:   []proxmox.Resource{{ID: "qemu/100", VMID: 100, Name: "vm-test", Type: "qemu"}},
	}}
	handler := newInventoryTestHandler(t, database, client)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedInventoryRequest(t, database))
	if response.Code != http.StatusOK || client.calls != 1 {
		t.Fatalf("status=%d calls=%d body=%s", response.Code, client.calls, response.Body.String())
	}
	var inventory proxmox.Inventory
	if err := json.NewDecoder(response.Body).Decode(&inventory); err != nil {
		t.Fatal(err)
	}
	if inventory.Version.Version != "9.2.10" || len(inventory.Nodes) != 1 || inventory.Resources[0].Name != "vm-test" {
		t.Fatalf("unexpected inventory: %+v", inventory)
	}
}

func TestInventoryNotConfigured(t *testing.T) {
	database := store.NewMemory()
	handler := newInventoryTestHandler(t, database, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedInventoryRequest(t, database))
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "proxmox_not_configured") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestInventoryFailureDoesNotExposeToken(t *testing.T) {
	database := store.NewMemory()
	client := &fakeInventoryClient{err: errors.New("upstream unavailable")}
	handler := newInventoryTestHandler(t, database, client)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedInventoryRequest(t, database))
	body := response.Body.String()
	if response.Code != http.StatusBadGateway || !strings.Contains(body, "proxmox_unavailable") || strings.Contains(body, "reader@pve!inventory=secret") {
		t.Fatalf("status=%d body=%s", response.Code, body)
	}
}
