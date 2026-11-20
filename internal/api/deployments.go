package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"proxmox-orchestrator/internal/deployment"
	"proxmox-orchestrator/internal/redact"
	"proxmox-orchestrator/internal/seal"
	"proxmox-orchestrator/internal/store"
)

const (
	maxArtifactSize = 1 << 20
	maxPlanSize     = 8 << 20
	maxPlanTextSize = 2 << 20
)

func decodeSizedJSON(w http.ResponseWriter, r *http.Request, target any, size int64) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "json_required", "Ожидается JSON")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, size)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Некорректное тело запроса или превышен размер")
		return false
	}
	return true
}

func (m *Management) createOpenTofuDeployment(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ResourceID     string                 `json:"resourceId"`
		IdempotencyKey string                 `json:"idempotencyKey"`
		Artifact       string                 `json:"artifact"`
		Files          []deployment.InputFile `json:"files"`
		Variables      map[string]any         `json:"variables"`
	}
	if !decodeSizedJSON(w, r, &input, maxArtifactSize*6) {
		return
	}
	input.ResourceID, input.IdempotencyKey = strings.TrimSpace(input.ResourceID), strings.TrimSpace(input.IdempotencyKey)
	var artifact []byte
	var err error
	if len(input.Files) > 0 {
		var files []deployment.File
		files, err = deployment.DecodeFiles(input.Files)
		if err == nil {
			artifact, err = deployment.Archive(files, input.Variables)
		}
	} else {
		artifact, err = base64.StdEncoding.DecodeString(input.Artifact)
	}
	if err != nil || len(artifact) == 0 || len(artifact) > maxArtifactSize || input.ResourceID == "" || len(input.ResourceID) > 255 || input.IdempotencyKey == "" || len(input.IdempotencyKey) > 255 {
		message := "Проверьте идентификатор deployment и конфигурацию"
		if err != nil {
			message += ": " + err.Error()
		}
		writeError(w, http.StatusBadRequest, "invalid_deployment", message)
		return
	}
	m.enqueueOpenTofuArtifact(w, r, input.ResourceID, input.IdempotencyKey, artifact)
}

func (m *Management) enqueueOpenTofuArtifact(w http.ResponseWriter, r *http.Request, resourceID, idempotencyKey string, artifact []byte) {
	resourceID, idempotencyKey = strings.TrimSpace(resourceID), strings.TrimSpace(idempotencyKey)
	if resourceID == "" || len(resourceID) > 255 || idempotencyKey == "" || len(idempotencyKey) > 255 || len(artifact) == 0 || len(artifact) > maxArtifactSize {
		writeError(w, http.StatusBadRequest, "invalid_deployment", "Проверьте идентификатор deployment и размер конфигурации")
		return
	}
	hash := sha256.Sum256(artifact)
	hashText := hex.EncodeToString(hash[:])
	encrypted, err := seal.Encrypt(m.encryptionKey, artifact)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "encryption_failed", "Не удалось защитить конфигурацию")
		return
	}
	user := m.currentUser(r)
	job, created, err := m.store.EnqueueJob(r.Context(), store.EnqueueJobInput{Operation: "opentofu.plan_apply", ResourceID: resourceID, InitiatorUserID: user.ID, IdempotencyKey: idempotencyKey, RequestHash: hashText, Artifact: encrypted, ArtifactSHA256: hashText})
	if err != nil {
		m.writeJobError(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]any{"job": job, "artifactSha256": hashText})
}

func (m *Management) approvePlan(w http.ResponseWriter, r *http.Request) {
	id, ok := validJobID(w, r)
	if !ok {
		return
	}
	var input struct {
		ArtifactSHA256 string `json:"artifactSha256"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	user := m.currentUser(r)
	detail, err := m.store.ApprovePlan(r.Context(), id, user.ID, input.ArtifactSHA256, 10*time.Minute)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrPlanChanged):
			writeError(w, http.StatusConflict, "plan_changed", "Конфигурация изменилась. Создайте новый план.")
		case errors.Is(err, store.ErrApprovalExpired):
			writeError(w, http.StatusConflict, "plan_expired", "План устарел. Создайте новый план.")
		case store.IsNotFound(err):
			writeError(w, http.StatusNotFound, "job_not_found", "Задача не найдена")
		default:
			writeError(w, http.StatusServiceUnavailable, "approval_failed", "Не удалось подтвердить план")
		}
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (m *Management) requireRunner(w http.ResponseWriter, r *http.Request) bool {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(m.runnerToken)) != 1 {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeError(w, http.StatusUnauthorized, "runner_authentication_required", "Требуется аутентификация Runner")
		return false
	}
	return true
}

func validWorkerID(value string) bool {
	return len(value) > 0 && len(value) <= 255 && strings.TrimSpace(value) == value
}

func (m *Management) runnerClaim(w http.ResponseWriter, r *http.Request) {
	if !m.requireRunner(w, r) {
		return
	}
	var input struct {
		WorkerID     string `json:"workerId"`
		LeaseSeconds int    `json:"leaseSeconds"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if !validWorkerID(input.WorkerID) || input.LeaseSeconds < 30 || input.LeaseSeconds > 300 {
		writeError(w, http.StatusBadRequest, "invalid_claim", "Проверьте workerId и leaseSeconds (30–300)")
		return
	}
	lease := time.Duration(input.LeaseSeconds) * time.Second
	job, err := m.store.ClaimJob(r.Context(), input.WorkerID, lease)
	mode := "plan"
	if errors.Is(err, sql.ErrNoRows) {
		job, err = m.store.ClaimApprovedJob(r.Context(), input.WorkerID, lease)
		mode = "apply"
	}
	if errors.Is(err, sql.ErrNoRows) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "claim_failed", "Не удалось получить задачу")
		return
	}
	artifact, err := m.store.DeploymentArtifact(r.Context(), job.ID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "artifact_unavailable", "Не удалось получить конфигурацию")
		return
	}
	plainArtifact, err := seal.Decrypt(m.encryptionKey, artifact.Artifact)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "artifact_unavailable", "Не удалось расшифровать конфигурацию")
		return
	}
	response := map[string]any{"job": job, "mode": mode, "artifact": base64.StdEncoding.EncodeToString(plainArtifact), "artifactSha256": artifact.ArtifactSHA256}
	if mode == "apply" {
		plainPlan, decryptErr := seal.Decrypt(m.encryptionKey, artifact.PlanFile)
		if decryptErr != nil {
			writeError(w, http.StatusInternalServerError, "plan_unavailable", "Не удалось расшифровать план")
			return
		}
		response["plan"] = base64.StdEncoding.EncodeToString(plainPlan)
		response["planSha256"] = artifact.PlanSHA256
	}
	writeJSON(w, http.StatusOK, response)
}

func (m *Management) runnerJobID(w http.ResponseWriter, r *http.Request) (string, bool) {
	if !m.requireRunner(w, r) {
		return "", false
	}
	return validJobID(w, r)
}

func (m *Management) runnerRenew(w http.ResponseWriter, r *http.Request) {
	id, ok := m.runnerJobID(w, r)
	if !ok {
		return
	}
	var input struct {
		WorkerID     string `json:"workerId"`
		LeaseSeconds int    `json:"leaseSeconds"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if !validWorkerID(input.WorkerID) || input.LeaseSeconds < 30 || input.LeaseSeconds > 300 {
		writeError(w, http.StatusBadRequest, "invalid_lease", "Некорректный lease")
		return
	}
	if err := m.store.RenewLease(r.Context(), id, input.WorkerID, time.Duration(input.LeaseSeconds)*time.Second); err != nil {
		writeError(w, http.StatusConflict, "lease_lost", "Lease задачи потерян")
		return
	}
	detail, err := m.store.Job(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "job_unavailable", "Задача недоступна")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cancelRequested": detail.Job.CancelRequestedAt != nil})
}

func (m *Management) runnerPlan(w http.ResponseWriter, r *http.Request) {
	id, ok := m.runnerJobID(w, r)
	if !ok {
		return
	}
	var input struct {
		WorkerID       string `json:"workerId"`
		ArtifactSHA256 string `json:"artifactSha256"`
		PlanSHA256     string `json:"planSha256"`
		PlanText       string `json:"planText"`
		Plan           string `json:"plan"`
	}
	if !decodeSizedJSON(w, r, &input, maxPlanSize*2) {
		return
	}
	plan, err := base64.StdEncoding.DecodeString(input.Plan)
	if err != nil || len(plan) == 0 || len(plan) > maxPlanSize || len(input.PlanText) == 0 || len(input.PlanText) > maxPlanTextSize || !validWorkerID(input.WorkerID) {
		writeError(w, http.StatusBadRequest, "invalid_plan", "Некорректный или слишком большой план")
		return
	}
	hash := sha256.Sum256(plan)
	if hex.EncodeToString(hash[:]) != input.PlanSHA256 {
		writeError(w, http.StatusBadRequest, "plan_hash_mismatch", "Хеш плана не совпадает")
		return
	}
	encrypted, err := seal.Encrypt(m.encryptionKey, plan)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "encryption_failed", "Не удалось защитить план")
		return
	}
	input.PlanText = redact.Text(input.PlanText)
	if err := m.store.SavePlan(r.Context(), id, input.WorkerID, store.PlanResult{PlanText: input.PlanText, PlanFile: encrypted, PlanSHA256: input.PlanSHA256, ArtifactSHA256: input.ArtifactSHA256}); err != nil {
		writeError(w, http.StatusConflict, "plan_rejected", "План отклонён: задача или конфигурация изменилась")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"waitingApproval": true})
}

func (m *Management) runnerStep(w http.ResponseWriter, r *http.Request) {
	id, ok := m.runnerJobID(w, r)
	if !ok {
		return
	}
	var input struct {
		WorkerID string `json:"workerId"`
		Name     string `json:"name"`
		Status   string `json:"status"`
		Detail   string `json:"detail"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if !validWorkerID(input.WorkerID) {
		writeError(w, http.StatusBadRequest, "invalid_step", "Некорректный шаг")
		return
	}
	var err error
	if input.Status == store.JobRunning {
		_, err = m.store.StartStep(r.Context(), id, input.WorkerID, input.Name)
	} else {
		err = m.store.CompleteStep(r.Context(), id, input.WorkerID, input.Name, input.Status, input.Detail)
	}
	if err != nil {
		writeError(w, http.StatusConflict, "step_rejected", "Шаг отклонён")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (m *Management) runnerEvent(w http.ResponseWriter, r *http.Request) {
	id, ok := m.runnerJobID(w, r)
	if !ok {
		return
	}
	var input struct {
		Level   string `json:"level"`
		Message string `json:"message"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if strings.TrimSpace(input.Message) == "" {
		writeError(w, http.StatusBadRequest, "invalid_event", "Некорректное событие")
		return
	}
	if err := m.store.AppendEvent(r.Context(), id, input.Level, input.Message); err != nil {
		writeError(w, http.StatusConflict, "event_rejected", "Событие отклонено")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (m *Management) runnerComplete(w http.ResponseWriter, r *http.Request) {
	id, ok := m.runnerJobID(w, r)
	if !ok {
		return
	}
	var input struct {
		WorkerID  string `json:"workerId"`
		Status    string `json:"status"`
		ErrorCode string `json:"errorCode"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if !validWorkerID(input.WorkerID) || len(input.ErrorCode) > 100 {
		writeError(w, http.StatusBadRequest, "invalid_completion", "Некорректное завершение")
		return
	}
	if err := m.store.CompleteJob(r.Context(), id, input.WorkerID, input.Status, input.ErrorCode); err != nil {
		writeError(w, http.StatusConflict, "completion_rejected", "Завершение отклонено")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
