package proxmox

import (
	"context"
	"sync"
	"time"
)

type InventoryClient interface {
	Inventory(context.Context) (Inventory, error)
}

// CachedClient coalesces concurrent reads and keeps a short-lived in-memory
// snapshot. It intentionally does not persist Proxmox data in PostgreSQL.
type CachedClient struct {
	client InventoryClient
	ttl    time.Duration

	mu        sync.Mutex
	inventory Inventory
	fetchedAt time.Time
	lastErr   error
	inFlight  chan struct{}
}

func NewCachedClient(client InventoryClient, ttl time.Duration) *CachedClient {
	return &CachedClient{client: client, ttl: ttl}
}

func (c *CachedClient) Inventory(ctx context.Context) (Inventory, error) {
	c.mu.Lock()
	if c.fetchedAt.IsZero() == false && time.Since(c.fetchedAt) < c.ttl {
		inventory := c.inventory
		c.mu.Unlock()
		return inventory, nil
	}
	if c.inFlight != nil {
		done := c.inFlight
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return Inventory{}, ctx.Err()
		case <-done:
		}
		c.mu.Lock()
		inventory, err := c.inventory, c.lastErr
		c.mu.Unlock()
		return inventory, err
	}
	done := make(chan struct{})
	c.inFlight = done
	c.mu.Unlock()

	inventory, err := c.client.Inventory(ctx)

	c.mu.Lock()
	if err == nil {
		c.inventory = inventory
		c.fetchedAt = time.Now()
	}
	c.lastErr = err
	c.inFlight = nil
	close(done)
	c.mu.Unlock()
	return inventory, err
}

// Run refreshes the snapshot immediately and then at the requested interval.
// Errors are exposed to the next API caller; Run never logs credentials.
func (c *CachedClient) Run(ctx context.Context, interval time.Duration) {
	_, _ = c.Inventory(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = c.Inventory(ctx)
		}
	}
}
