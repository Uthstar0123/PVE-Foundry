package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

	"proxmox-orchestrator/internal/store"
)

func managementServer(t *testing.T) (*httptest.Server, *store.Memory) {
	t.Helper()
	database := store.NewMemory()
	handler, err := NewManagementHandler(database, bytes.Repeat([]byte{7}, 32), false)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	server.Client().Jar = jar
	t.Cleanup(server.Close)
	return server, database
}

func csrfFor(t *testing.T, client *http.Client, base string) string {
	t.Helper()
	response, err := client.Get(base + "/api/v1/auth/csrf")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var body map[string]string
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body["token"]
}

func postJSON(t *testing.T, client *http.Client, url, token string, body any) *http.Response {
	t.Helper()
	encoded, _ := json.Marshal(body)
	request, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(encoded))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Referer", url)
	if token != "" {
		request.Header.Set("X-CSRF-Token", token)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func setupInput() map[string]string {
	return map[string]string{"displayName": "Admin", "email": "admin@example.local", "password": "correct horse battery"}
}

func TestSetupLoginLogoutAndProtectedAPI(t *testing.T) {
	server, _ := managementServer(t)
	client := server.Client()
	token := csrfFor(t, client, server.URL)

	response := postJSON(t, client, server.URL+"/api/v1/setup", token, setupInput())
	if response.StatusCode != http.StatusOK {
		t.Fatalf("setup status: %d", response.StatusCode)
	}
	response.Body.Close()
	var sessionCookie *http.Cookie
	for _, cookie := range response.Cookies() {
		if cookie.Name == "orchestrator_session" {
			sessionCookie = cookie
		}
	}
	if sessionCookie == nil || !sessionCookie.HttpOnly || sessionCookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("secure session cookie was not issued")
	}

	response, err := client.Get(server.URL + "/api/v1/overview")
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("protected request: status=%d err=%v", response.StatusCode, err)
	}
	response.Body.Close()

	response = postJSON(t, client, server.URL+"/api/v1/auth/logout", token, map[string]any{})
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("logout status: %d", response.StatusCode)
	}
	response, _ = client.Get(server.URL + "/api/v1/overview")
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("session survived logout: %d", response.StatusCode)
	}

	token = csrfFor(t, client, server.URL)
	response = postJSON(t, client, server.URL+"/api/v1/auth/login", token, map[string]string{"email": "admin@example.local", "password": "correct horse battery"})
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("login status: %d", response.StatusCode)
	}
}

func TestSetupCanOnlyRunOnce(t *testing.T) {
	server, _ := managementServer(t)
	client := server.Client()
	token := csrfFor(t, client, server.URL)
	response := postJSON(t, client, server.URL+"/api/v1/setup", token, setupInput())
	response.Body.Close()
	response = postJSON(t, client, server.URL+"/api/v1/setup", token, setupInput())
	response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate setup status: %d", response.StatusCode)
	}
}

func TestMutationsRequireCSRF(t *testing.T) {
	server, _ := managementServer(t)
	response := postJSON(t, server.Client(), server.URL+"/api/v1/setup", "", setupInput())
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("CSRF bypass status: %d", response.StatusCode)
	}
}

func TestProtectedAPIRequiresSession(t *testing.T) {
	server, _ := managementServer(t)
	response, err := server.Client().Get(server.URL + "/api/v1/overview")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("protected status: %d", response.StatusCode)
	}
}

func TestLoginRateLimit(t *testing.T) {
	server, _ := managementServer(t)
	client := server.Client()
	token := csrfFor(t, client, server.URL)
	response := postJSON(t, client, server.URL+"/api/v1/setup", token, setupInput())
	response.Body.Close()
	var lastStatus int
	for i := 0; i < 6; i++ {
		response = postJSON(t, client, server.URL+"/api/v1/auth/login", token, map[string]string{"email": "admin@example.local", "password": "wrong-password"})
		response.Body.Close()
		lastStatus = response.StatusCode
	}
	if lastStatus != http.StatusTooManyRequests || response.Header.Get("Retry-After") == "" {
		t.Fatalf("rate limit status: %d", lastStatus)
	}
}

func TestSecretsAndPasswordHashNeverAppearInResponses(t *testing.T) {
	server, _ := managementServer(t)
	client := server.Client()
	token := csrfFor(t, client, server.URL)
	response := postJSON(t, client, server.URL+"/api/v1/setup", token, setupInput())
	defer response.Body.Close()
	var body strings.Builder
	buffer := make([]byte, 4096)
	for {
		n, err := response.Body.Read(buffer)
		body.Write(buffer[:n])
		if err != nil {
			break
		}
	}
	if strings.Contains(body.String(), "correct horse") || strings.Contains(body.String(), "argon2") || strings.Contains(body.String(), "passwordHash") {
		t.Fatalf("sensitive data leaked: %s", body.String())
	}
}
