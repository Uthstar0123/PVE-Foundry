package app

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"proxmox-orchestrator/internal/api"
	"proxmox-orchestrator/internal/config"
	"proxmox-orchestrator/internal/proxmox"
	"proxmox-orchestrator/internal/store"
)

func Run(service, prefix, defaultAddress string) {
	cfg, err := config.Load(prefix, defaultAddress)
	if err != nil {
		log.Fatalf("load configuration: %v", err)
	}
	var handler http.Handler
	var database store.Store
	if service == "management" {
		if cfg.DatabaseURL == "" {
			log.Fatal("MANAGEMENT_DATABASE_URL is required")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		database, err = store.OpenPostgres(ctx, cfg.DatabaseURL)
		cancel()
		if err != nil {
			log.Fatalf("initialize database: %v", err)
		}
		defer database.Close()
		recoveryCtx, stopRecovery := context.WithCancel(context.Background())
		defer stopRecovery()
		recoverLeases := func() {
			ctx, cancel := context.WithTimeout(recoveryCtx, 5*time.Second)
			defer cancel()
			failed, err := database.FailExpiredLeases(ctx)
			if err != nil {
				log.Printf("recover expired job leases: %v", err)
			} else if failed > 0 {
				log.Printf("marked %d expired jobs failed; outcomes require manual reconciliation", failed)
			}
		}
		recoverLeases()
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-recoveryCtx.Done():
					return
				case <-ticker.C:
					recoverLeases()
				}
			}
		}()
		var inventoryClients []api.InventoryClient
		if cfg.ProxmoxAPIURL != "" {
			client, clientErr := proxmox.NewClient(proxmox.Config{
				APIURL:             cfg.ProxmoxAPIURL,
				APIToken:           cfg.ProxmoxAPIToken,
				CAFile:             cfg.ProxmoxCAFile,
				InsecureSkipVerify: cfg.ProxmoxInsecure,
			})
			if clientErr != nil {
				log.Fatalf("initialize Proxmox client: %v", clientErr)
			}
			cached := proxmox.NewCachedClient(client, 30*time.Second)
			inventoryClients = append(inventoryClients, cached)
			syncCtx, stopSync := context.WithCancel(context.Background())
			defer stopSync()
			go cached.Run(syncCtx, time.Minute)
		}
		handler, err = api.NewManagementHandler(database, cfg.AuthKey, cfg.Environment == "production", inventoryClients...)
		if err != nil {
			log.Fatalf("initialize management API: %v", err)
		}
	} else {
		handler = api.NewHandler(service)
	}
	server := &http.Server{
		Addr:              cfg.Address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       20 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("graceful shutdown: %v", err)
		}
	}()
	log.Printf("%s listening on %s", service, cfg.Address)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("serve: %v", err)
	}
}
