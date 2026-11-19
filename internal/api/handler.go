package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"net/mail"
	"strings"
	"time"

	gorillacsrf "github.com/gorilla/csrf"

	"proxmox-orchestrator/internal/auth"
	"proxmox-orchestrator/internal/store"
)

const sessionLifetime = 12 * time.Hour

type healthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
}

type Management struct {
	store     store.Store
	secure    bool
	limiter   *loginLimiter
	dummyHash string
}

func NewHandler(service string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, healthResponse{Status: "ok", Service: service})
	})
	mux.HandleFunc("GET /api/v1/readyz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, healthResponse{Status: "ready", Service: service})
	})
	return securityHeaders(mux)
}

func NewManagementHandler(database store.Store, authKey []byte, production bool) (http.Handler, error) {
	dummyHash, err := auth.HashPassword("not-a-real-user-password")
	if err != nil {
		return nil, err
	}
	m := &Management{store: database, secure: production, limiter: newLoginLimiter(), dummyHash: dummyHash}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, healthResponse{Status: "ok", Service: "management"})
	})
	mux.HandleFunc("GET /api/v1/readyz", m.ready)
	mux.HandleFunc("GET /api/v1/auth/csrf", m.csrfToken)
	mux.HandleFunc("GET /api/v1/setup/status", m.setupStatus)
	mux.HandleFunc("POST /api/v1/setup", m.setup)
	mux.HandleFunc("POST /api/v1/auth/login", m.login)
	mux.Handle("POST /api/v1/auth/logout", m.requireSession(http.HandlerFunc(m.logout)))
	mux.Handle("GET /api/v1/auth/session", m.requireSession(http.HandlerFunc(m.session)))
	mux.Handle("GET /api/v1/overview", m.requireSession(http.HandlerFunc(m.overview)))
	csrfOptions := []gorillacsrf.Option{
		gorillacsrf.Secure(production),
		gorillacsrf.HttpOnly(true),
		gorillacsrf.SameSite(gorillacsrf.SameSiteStrictMode),
		gorillacsrf.Path("/"),
		gorillacsrf.CookieName("orchestrator_csrf"),
		gorillacsrf.RequestHeader("X-CSRF-Token"),
		gorillacsrf.ErrorHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.Printf("CSRF rejected: %v", gorillacsrf.FailureReason(r))
			writeError(w, http.StatusForbidden, "csrf_rejected", "Сессия формы устарела. Обновите страницу.")
		})),
	}
	if !production {
		csrfOptions = append(csrfOptions, gorillacsrf.TrustedOrigins([]string{"localhost:7100", "127.0.0.1:7100"}))
	}
	protection := gorillacsrf.Protect(authKey, csrfOptions...)
	protected := protection(mux)
	plaintext := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !production && r.TLS == nil {
			r = gorillacsrf.PlaintextHTTPRequest(r)
		}
		protected.ServeHTTP(w, r)
	})
	return securityHeaders(plaintext), nil
}

func (m *Management) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := m.store.Ready(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database_unavailable", "База данных недоступна")
		return
	}
	writeJSON(w, http.StatusOK, healthResponse{Status: "ready", Service: "management"})
}

func (m *Management) csrfToken(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"token": gorillacsrf.Token(r)})
}

func (m *Management) setupStatus(w http.ResponseWriter, r *http.Request) {
	initialized, err := m.store.Initialized(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "database_unavailable", "Не удалось проверить состояние настройки")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"required": !initialized, "authenticated": m.currentUser(r) != nil})
}

func (m *Management) setup(w http.ResponseWriter, r *http.Request) {
	var input struct {
		DisplayName string `json:"displayName"`
		Email       string `json:"email"`
		Password    string `json:"password"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	address, err := mail.ParseAddress(input.Email)
	if err != nil || address.Address != input.Email || input.DisplayName == "" || len(input.DisplayName) > 100 || len(input.Password) < 12 || len(input.Password) > 128 {
		writeError(w, http.StatusBadRequest, "invalid_setup", "Проверьте имя, email и пароль: пароль должен содержать 12–128 символов")
		return
	}
	hash, err := auth.HashPassword(input.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "password_hash_failed", "Не удалось защитить пароль")
		return
	}
	user := store.User{ID: store.NewUserID(), Email: input.Email, DisplayName: input.DisplayName, Role: "administrator", PasswordHash: hash, CreatedAt: time.Now().UTC()}
	if err := m.store.CreateInitialAdmin(r.Context(), user); err != nil {
		if errors.Is(err, store.ErrAlreadyInitialized) {
			writeError(w, http.StatusConflict, "already_initialized", "Администратор уже создан")
			return
		}
		writeError(w, http.StatusInternalServerError, "setup_failed", "Не удалось завершить настройку")
		return
	}
	_ = m.store.Audit(r.Context(), "setup.completed", &user.ID, sourceIP(r), "initial administrator created")
	m.issueSession(w, r, user)
}

func (m *Management) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	key := sourceIP(r) + "|" + strings.ToLower(strings.TrimSpace(input.Email))
	if retry := m.limiter.retryAfter(key); retry > 0 {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "login_limited", "Слишком много попыток. Повторите через минуту.")
		return
	}
	user, err := m.store.UserByEmail(r.Context(), input.Email)
	if err != nil {
		auth.VerifyPassword(m.dummyHash, input.Password)
		m.limiter.failed(key)
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "Неверный email или пароль")
		return
	}
	if !auth.VerifyPassword(user.PasswordHash, input.Password) {
		m.limiter.failed(key)
		_ = m.store.Audit(r.Context(), "auth.login_failed", &user.ID, sourceIP(r), "invalid credentials")
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "Неверный email или пароль")
		return
	}
	m.limiter.succeeded(key)
	_ = m.store.Audit(r.Context(), "auth.login_succeeded", &user.ID, sourceIP(r), "session created")
	m.issueSession(w, r, user)
}

func (m *Management) issueSession(w http.ResponseWriter, r *http.Request, user store.User) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		writeError(w, http.StatusInternalServerError, "session_failed", "Не удалось создать сессию")
		return
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	if err := m.store.CreateSession(r.Context(), hash, user.ID, time.Now().Add(sessionLifetime)); err != nil {
		writeError(w, http.StatusInternalServerError, "session_failed", "Не удалось сохранить сессию")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "orchestrator_session", Value: token, Path: "/", HttpOnly: true, Secure: m.secure, SameSite: http.SameSiteStrictMode, MaxAge: int(sessionLifetime.Seconds())})
	writeJSON(w, http.StatusOK, map[string]any{"user": sanitizeUser(&user)})
}

func (m *Management) session(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"user": sanitizeUser(m.currentUser(r))})
}

func (m *Management) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("orchestrator_session"); err == nil {
		hash := sha256.Sum256([]byte(cookie.Value))
		_ = m.store.DeleteSession(r.Context(), hash)
	}
	http.SetCookie(w, &http.Cookie{Name: "orchestrator_session", Value: "", Path: "/", HttpOnly: true, Secure: m.secure, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (m *Management) overview(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "protected", "proxmoxConnected": false, "message": "Read-only интеграция будет добавлена на этапе 3"})
}

type userContextKey struct{}

func (m *Management) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("orchestrator_session")
		if err != nil {
			writeError(w, http.StatusUnauthorized, "authentication_required", "Требуется вход")
			return
		}
		hash := sha256.Sum256([]byte(cookie.Value))
		session, err := m.store.Session(r.Context(), hash)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "authentication_required", "Сессия недействительна или истекла")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userContextKey{}, session.User)))
	})
}

func (m *Management) currentUser(r *http.Request) *store.User {
	if user, ok := r.Context().Value(userContextKey{}).(store.User); ok {
		return &user
	}
	cookie, err := r.Cookie("orchestrator_session")
	if err != nil {
		return nil
	}
	hash := sha256.Sum256([]byte(cookie.Value))
	session, err := m.store.Session(r.Context(), hash)
	if err != nil {
		return nil
	}
	return &session.User
}

func sanitizeUser(user *store.User) *store.User {
	if user == nil {
		return nil
	}
	copy := *user
	copy.PasswordHash = ""
	return &copy
}

func sourceIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "json_required", "Ожидается JSON")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Некорректное тело запроса")
		return false
	}
	return true
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode API response: %v", err)
	}
}
