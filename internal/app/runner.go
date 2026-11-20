package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"proxmox-orchestrator/internal/config"
	"proxmox-orchestrator/internal/runner"
)

func RunRunner() {
	cfg, err := config.Load("RUNNER", ":8081")
	if err != nil {
		log.Fatalf("load runner configuration: %v", err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.ManagementCAFile != "" {
		pem, err := os.ReadFile(cfg.ManagementCAFile)
		if err != nil {
			log.Fatalf("read Management CA: %v", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil {
			log.Fatalf("load system CA pool: %v", err)
		}
		if !pool.AppendCertsFromPEM(pem) {
			log.Fatal("Management CA file contains no certificates")
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}
	client, err := runner.NewClient(cfg.ManagementURL, cfg.RunnerToken, &http.Client{Transport: transport, Timeout: 35 * time.Second})
	if err != nil {
		log.Fatalf("initialize Management client: %v", err)
	}
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "runner-" + fmt.Sprint(os.Getpid())
	}
	service, err := runner.NewService(client, runner.Config{WorkerID: hostname, OpenTofuBinary: cfg.OpenTofuBinary, WorkRoot: cfg.WorkRoot, StateRoot: cfg.StateRoot, Environment: cfg.Environment})
	if err != nil {
		log.Fatalf("initialize runner: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("runner %s polling Management; no administrative listener is exposed", hostname)
	if err := service.Run(ctx); err != nil {
		log.Fatalf("run runner: %v", err)
	}
}
