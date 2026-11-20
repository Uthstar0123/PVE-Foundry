package api

import (
	"context"
	"log"
	"net/http"
	"time"

	"proxmox-orchestrator/internal/deployment"
)

func (m *Management) loadDeploymentDefaults(w http.ResponseWriter, r *http.Request) (deployment.Defaults, bool) {
	if m.proxmox == nil {
		writeError(w, http.StatusServiceUnavailable, "proxmox_not_configured", "Подключение Proxmox не настроено")
		return deployment.Defaults{}, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	inventory, err := m.proxmox.Inventory(ctx)
	if err != nil {
		log.Printf("discover deployment defaults: %v", err)
		writeError(w, http.StatusBadGateway, "proxmox_unavailable", "Не удалось определить параметры Proxmox")
		return deployment.Defaults{}, false
	}
	return deployment.DefaultsFromInventory(inventory), true
}

func (m *Management) deploymentDefaults(w http.ResponseWriter, r *http.Request) {
	defaults, ok := m.loadDeploymentDefaults(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, defaults)
}

func (m *Management) preflightOpenTofu(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Files []deployment.InputFile `json:"files"`
	}
	if !decodeSizedJSON(w, r, &input, maxArtifactSize*6) {
		return
	}
	files, err := deployment.DecodeFiles(input.Files)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_files", err.Error())
		return
	}
	defaults, ok := m.loadDeploymentDefaults(w, r)
	if !ok {
		return
	}
	variables, err := deployment.Preflight(files, defaults)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_configuration", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"defaults": defaults, "variables": variables})
}

func (m *Management) createDesignerDeployment(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ResourceID     string                   `json:"resourceId"`
		IdempotencyKey string                   `json:"idempotencyKey"`
		Design         deployment.DesignerValue `json:"design"`
	}
	if !decodeSizedJSON(w, r, &input, maxArtifactSize*2) {
		return
	}
	files, err := deployment.GenerateDesigner(input.Design)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_design", err.Error())
		return
	}
	artifact, err := deployment.Archive(files, nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_design", err.Error())
		return
	}
	m.enqueueOpenTofuArtifact(w, r, input.ResourceID, input.IdempotencyKey, artifact)
}
