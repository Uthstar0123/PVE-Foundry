package proxmox

import (
	"context"
	"testing"
	"time"
)

type inventoryFunc func(context.Context) (Inventory, error)

func (f inventoryFunc) Inventory(ctx context.Context) (Inventory, error) {
	return f(ctx)
}

func TestCachedClientReusesSnapshot(t *testing.T) {
	calls := 0
	client := NewCachedClient(inventoryFunc(func(context.Context) (Inventory, error) {
		calls++
		return Inventory{Version: Version{Version: "9.2.10"}}, nil
	}), time.Hour)
	for range 2 {
		inventory, err := client.Inventory(context.Background())
		if err != nil || inventory.Version.Version != "9.2.10" {
			t.Fatalf("inventory=%+v err=%v", inventory, err)
		}
	}
	if calls != 1 {
		t.Fatalf("expected one upstream request, got %d", calls)
	}
}

func TestCachedClientRefreshesPeriodically(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	client := NewCachedClient(inventoryFunc(func(context.Context) (Inventory, error) {
		calls++
		if calls == 2 {
			cancel()
		}
		return Inventory{}, nil
	}), 0)
	client.Run(ctx, 5*time.Millisecond)
	if calls < 2 {
		t.Fatalf("periodic refresh did not run: %d calls", calls)
	}
}
