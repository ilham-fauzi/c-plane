package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ilham/c-plane/internal/model"
	"github.com/ilham/c-plane/internal/store/sqlitestore"
)

func TestDeploymentLifecycle(t *testing.T) {
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "cplane.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	handler := NewServer(store)

	host := postJSON[model.Host](t, handler, "/api/hosts", map[string]string{"name": "local-dev"})
	if host.ID == "" || host.InstallToken == "" {
		t.Fatalf("host registration did not return install data: %#v", host)
	}
	register := postJSON[map[string]string](t, handler, "/api/agent/register", map[string]string{
		"host_id":       host.ID,
		"install_token": host.InstallToken,
		"agent_version": "0.1.0",
		"hostname":      "local-dev",
		"os":            "linux",
		"arch":          "amd64",
	})
	if register["agent_token"] == "" {
		t.Fatalf("agent registration did not return runtime token: %#v", register)
	}

	repo := postJSON[model.Repository](t, handler, "/api/repos", map[string]string{
		"name": "demo-api",
		"url":  "https://github.com/example/demo-api",
	})
	if repo.Provider != "github" || repo.DefaultBranch != "main" {
		t.Fatalf("repo defaults not applied: %#v", repo)
	}

	app := postJSON[model.App](t, handler, "/api/apps", map[string]any{
		"name":                     "demo-api-prod",
		"repo_id":                  repo.ID,
		"host_id":                  host.ID,
		"environment_id":           "production",
		"root_path":                "/var/apps/demo-api",
		"recipe_path":              "/opt/c-plane/apps/demo-api/deploy.yaml",
		"successful_releases_keep": 5,
	})
	if app.FailedReleasesTTLHours != 72 {
		t.Fatalf("app retention defaults not applied: %#v", app)
	}

	job := postJSON[model.DeploymentJob](t, handler, "/api/deployments", map[string]any{
		"app_id":     app.ID,
		"ref":        "main",
		"commit_sha": "abc123",
	})
	if job.HostID != host.ID || job.RepoID != repo.ID || job.Status != "queued" {
		t.Fatalf("deployment job not hydrated from app: %#v", job)
	}

	pending := getJSON[[]model.DeploymentJob](t, handler, "/api/agent/jobs/pending?host_id="+host.ID)
	if len(pending) != 1 || pending[0].ID != job.ID {
		t.Fatalf("pending jobs mismatch: %#v", pending)
	}
}

func TestDashboardRoot(t *testing.T) {
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "cplane.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	handler := NewServer(store)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "C-Plane") {
		t.Fatalf("expected dashboard body to mention C-Plane")
	}
	if !strings.Contains(rec.Body.String(), "No hosts registered yet") {
		t.Fatalf("expected dashboard to render host empty state")
	}
	if !strings.Contains(rec.Body.String(), "Add Host") || !strings.Contains(rec.Body.String(), "Trigger Deploy") {
		t.Fatalf("expected dashboard to render CICD action forms")
	}
}

func TestDashboardCreateHostShowsInstallCommand(t *testing.T) {
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "cplane.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	handler := NewServer(store)
	form := url.Values{"name": {"sumopod-prod"}}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/hosts", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "portal.kaligede.my.id")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Install Agent") || !strings.Contains(body, "--api-url https://portal.kaligede.my.id") {
		t.Fatalf("expected install command page, got %s", body)
	}
}

func TestEmptyListsReturnArrays(t *testing.T) {
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "cplane.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	handler := NewServer(store)
	for _, path := range []string{"/api/hosts", "/api/repos", "/api/apps", "/api/deployments", "/api/audit-events"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s expected 200, got %d", path, rec.Code)
		}
		if strings.TrimSpace(rec.Body.String()) != "[]" {
			t.Fatalf("GET %s expected empty array, got %q", path, strings.TrimSpace(rec.Body.String()))
		}
	}
}

func TestAppRetentionMinimum(t *testing.T) {
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "cplane.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	handler := NewServer(store)

	resp := postRaw(t, handler, "/api/apps", map[string]any{
		"name":                     "bad-app",
		"repo_id":                  "repo_missing",
		"host_id":                  "srv_missing",
		"successful_releases_keep": 2,
	})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request, got %d: %s", resp.Code, resp.Body.String())
	}
}

func postJSON[T any](t *testing.T, handler http.Handler, path string, body any) T {
	t.Helper()
	resp := postRaw(t, handler, path, body)
	if resp.Code < 200 || resp.Code >= 300 {
		t.Fatalf("POST %s failed: %d %s", path, resp.Code, resp.Body.String())
	}
	var out T
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return out
}

func postRaw(t *testing.T, handler http.Handler, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func getJSON[T any](t *testing.T, handler http.Handler, path string) T {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code < 200 || rec.Code >= 300 {
		t.Fatalf("GET %s failed: %d", path, rec.Code)
	}
	var out T
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return out
}
