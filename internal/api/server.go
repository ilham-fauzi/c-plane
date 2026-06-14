package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ilham/c-plane/internal/id"
	"github.com/ilham/c-plane/internal/model"
	"github.com/ilham/c-plane/internal/store"
	"github.com/ilham/c-plane/internal/store/sqlitestore"
)

type Server struct {
	store store.Store
	mux   *http.ServeMux
}

const agentInstallerURL = "https://raw.githubusercontent.com/ilham-fauzi/c-plane/main/scripts/install-agent.sh"

type setupAppMetadata struct {
	AppName      string `json:"app_name"`
	RootPath     string `json:"root_path"`
	Domain       string `json:"domain,omitempty"`
	Runtime      string `json:"runtime"`
	RecipePath   string `json:"recipe_path"`
	NginxEnabled bool   `json:"nginx_enabled"`
}

type deployMetadata struct {
	AppName    string `json:"app_name"`
	RootPath   string `json:"root_path"`
	RecipePath string `json:"recipe_path"`
	RepoURL    string `json:"repo_url"`
	Ref        string `json:"ref"`
}

type completeJobRequest struct {
	ReleaseKey       string `json:"release_key"`
	ReleasePath      string `json:"release_path"`
	CommitSHA        string `json:"commit_sha"`
	ArtifactChecksum string `json:"artifact_checksum"`
}

func NewServer(store store.Store) http.Handler {
	server := &Server{
		store: store,
		mux:   http.NewServeMux(),
	}
	server.routes()
	return server.withMiddleware(server.mux)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /", s.handleDashboard)
	s.mux.HandleFunc("POST /dashboard/hosts", s.handleDashboardCreateHost)
	s.mux.HandleFunc("POST /dashboard/hosts/{id}/delete", s.handleDashboardDeleteHost)
	s.mux.HandleFunc("POST /dashboard/repos", s.handleDashboardCreateRepository)
	s.mux.HandleFunc("POST /dashboard/repos/{id}/delete", s.handleDashboardDeleteRepository)
	s.mux.HandleFunc("POST /dashboard/apps", s.handleDashboardCreateApp)
	s.mux.HandleFunc("POST /dashboard/setup-apps", s.handleDashboardSetupApp)
	s.mux.HandleFunc("POST /dashboard/deployments", s.handleDashboardCreateDeployment)
	s.mux.HandleFunc("GET /healthz", s.handleHealth)

	s.mux.HandleFunc("GET /api/hosts", s.handleListHosts)
	s.mux.HandleFunc("POST /api/hosts", s.handleCreateHost)
	s.mux.HandleFunc("DELETE /api/hosts/{id}", s.handleDeleteHost)

	s.mux.HandleFunc("GET /api/repos", s.handleListRepositories)
	s.mux.HandleFunc("POST /api/repos", s.handleCreateRepository)
	s.mux.HandleFunc("DELETE /api/repos/{id}", s.handleDeleteRepository)

	s.mux.HandleFunc("GET /api/apps", s.handleListApps)
	s.mux.HandleFunc("POST /api/apps", s.handleCreateApp)
	s.mux.HandleFunc("GET /api/apps/{id}/releases", s.handleListAppReleases)

	s.mux.HandleFunc("GET /api/deployments", s.handleListDeployments)
	s.mux.HandleFunc("POST /api/deployments", s.handleCreateDeployment)
	s.mux.HandleFunc("GET /api/deployments/{id}", s.handleGetDeployment)
	s.mux.HandleFunc("POST /api/deployments/{id}/approve", s.handleApproveDeployment)
	s.mux.HandleFunc("POST /api/deployments/{id}/cancel", s.handleCancelDeployment)

	s.mux.HandleFunc("POST /api/releases/{id}/rollback", s.handleRollbackRelease)

	s.mux.HandleFunc("POST /api/agent/register", s.handleAgentRegister)
	s.mux.HandleFunc("POST /api/agent/heartbeat", s.handleAgentHeartbeat)
	s.mux.HandleFunc("GET /api/agent/jobs/pending", s.handleAgentPendingJobs)
	s.mux.HandleFunc("GET /api/agent/jobs/{id}", s.handleAgentGetJob)
	s.mux.HandleFunc("POST /api/agent/jobs/{id}/start", s.handleAgentStartJob)
	s.mux.HandleFunc("POST /api/agent/jobs/{id}/logs", s.handleAgentJobLogs)
	s.mux.HandleFunc("POST /api/agent/jobs/{id}/complete", s.handleAgentCompleteJob)
	s.mux.HandleFunc("POST /api/agent/jobs/{id}/fail", s.handleAgentFailJob)

	s.mux.HandleFunc("GET /api/audit-events", s.handleListAuditEvents)
}

func (s *Server) withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleDashboardCreateHost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	created, err := s.createHost(r, r.FormValue("name"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeInstallCommandPage(w, created)
}

func (s *Server) handleDashboardDeleteHost(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteHost(r.Context(), r.PathValue("id")); err != nil {
		writeStoreError(w, err)
		return
	}
	s.recordAudit(r, "user", actor(r), "host.deleted", "host", r.PathValue("id"), "")
	redirectDashboard(w, r)
}

func (s *Server) handleDashboardCreateRepository(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	repo := model.Repository{
		ID:            id.New("repo"),
		Name:          strings.TrimSpace(r.FormValue("name")),
		Provider:      strings.TrimSpace(r.FormValue("provider")),
		URL:           strings.TrimSpace(r.FormValue("url")),
		DefaultBranch: strings.TrimSpace(r.FormValue("default_branch")),
	}
	if repo.Name == "" || repo.URL == "" {
		writeError(w, http.StatusBadRequest, "name and url are required")
		return
	}
	if repo.Provider == "" {
		repo.Provider = "github"
	}
	if repo.DefaultBranch == "" {
		repo.DefaultBranch = "main"
	}
	created, err := s.store.CreateRepository(r.Context(), repo)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.recordAudit(r, "user", actor(r), "repo.created", "repo", created.ID, "")
	redirectDashboard(w, r)
}

func (s *Server) handleDashboardDeleteRepository(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteRepository(r.Context(), r.PathValue("id")); err != nil {
		writeStoreError(w, err)
		return
	}
	s.recordAudit(r, "user", actor(r), "repo.deleted", "repo", r.PathValue("id"), "")
	redirectDashboard(w, r)
}

func (s *Server) handleDashboardCreateApp(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	keep, err := strconv.Atoi(blank(r.FormValue("successful_releases_keep"), "5"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "successful_releases_keep must be a number")
		return
	}
	app := model.App{
		ID:                     id.New("app"),
		Name:                   strings.TrimSpace(r.FormValue("name")),
		RepoID:                 strings.TrimSpace(r.FormValue("repo_id")),
		HostID:                 strings.TrimSpace(r.FormValue("host_id")),
		EnvironmentID:          strings.TrimSpace(r.FormValue("environment_id")),
		RootPath:               strings.TrimSpace(r.FormValue("root_path")),
		RecipePath:             strings.TrimSpace(r.FormValue("recipe_path")),
		SuccessfulReleasesKeep: keep,
	}
	if app.Name == "" || app.RepoID == "" || app.HostID == "" || app.RootPath == "" || app.RecipePath == "" {
		writeError(w, http.StatusBadRequest, "name, repo_id, host_id, root_path, and recipe_path are required")
		return
	}
	if app.EnvironmentID == "" {
		app.EnvironmentID = "production"
	}
	if app.SuccessfulReleasesKeep < 3 {
		writeError(w, http.StatusBadRequest, "successful_releases_keep must be at least 3")
		return
	}
	created, err := s.store.CreateApp(r.Context(), app)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.recordAudit(r, "user", actor(r), "app.created", "app", created.ID, "")
	redirectDashboard(w, r)
}

func (s *Server) handleDashboardSetupApp(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	keep, err := strconv.Atoi(blank(r.FormValue("successful_releases_keep"), "5"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "successful_releases_keep must be a number")
		return
	}
	app := model.App{
		ID:                     id.New("app"),
		Name:                   strings.TrimSpace(r.FormValue("name")),
		RepoID:                 strings.TrimSpace(r.FormValue("repo_id")),
		HostID:                 strings.TrimSpace(r.FormValue("host_id")),
		EnvironmentID:          strings.TrimSpace(r.FormValue("environment_id")),
		RootPath:               strings.TrimSpace(r.FormValue("root_path")),
		RecipePath:             strings.TrimSpace(r.FormValue("recipe_path")),
		SuccessfulReleasesKeep: keep,
	}
	if app.Name == "" || app.RepoID == "" || app.HostID == "" || app.RootPath == "" {
		writeError(w, http.StatusBadRequest, "name, repo_id, host_id, and root_path are required")
		return
	}
	if app.EnvironmentID == "" {
		app.EnvironmentID = "production"
	}
	if app.RecipePath == "" {
		app.RecipePath = "/opt/c-plane/apps/" + app.Name + "/deploy.yaml"
	}
	if app.SuccessfulReleasesKeep < 3 {
		writeError(w, http.StatusBadRequest, "successful_releases_keep must be at least 3")
		return
	}
	createdApp, err := s.store.CreateApp(r.Context(), app)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	metadata := setupAppMetadata{
		AppName:      createdApp.Name,
		RootPath:     createdApp.RootPath,
		Domain:       strings.TrimSpace(r.FormValue("domain")),
		Runtime:      blank(r.FormValue("runtime"), "static"),
		RecipePath:   createdApp.RecipePath,
		NginxEnabled: r.FormValue("nginx_enabled") == "1",
	}
	rawMetadata, err := json.Marshal(metadata)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	job := model.DeploymentJob{
		ID:           id.New("job"),
		AppID:        createdApp.ID,
		HostID:       createdApp.HostID,
		RepoID:       createdApp.RepoID,
		Action:       "setup_app",
		Status:       "queued",
		Ref:          blank(r.FormValue("ref"), "main"),
		MetadataJSON: string(rawMetadata),
		RequestedBy:  actor(r),
	}
	createdJob, err := s.store.CreateDeploymentJob(r.Context(), job)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.recordAudit(r, "user", actor(r), "app.setup_requested", "app", createdApp.ID, `{"job_id":"`+createdJob.ID+`"}`)
	redirectDashboard(w, r)
}

func (s *Server) handleDashboardCreateDeployment(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	appID := strings.TrimSpace(r.FormValue("app_id"))
	if appID == "" {
		writeError(w, http.StatusBadRequest, "app_id is required")
		return
	}
	app, err := s.store.GetApp(r.Context(), appID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	repo, err := s.store.GetRepository(r.Context(), app.RepoID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	ref := strings.TrimSpace(r.FormValue("ref"))
	if ref == "" {
		ref = repo.DefaultBranch
	}
	metadata, err := json.Marshal(deployMetadata{
		AppName:    app.Name,
		RootPath:   app.RootPath,
		RecipePath: app.RecipePath,
		RepoURL:    repo.URL,
		Ref:        ref,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	job := model.DeploymentJob{
		ID:           id.New("job"),
		AppID:        app.ID,
		HostID:       app.HostID,
		RepoID:       app.RepoID,
		Action:       "deploy",
		Status:       "queued",
		Ref:          ref,
		CommitSHA:    strings.TrimSpace(r.FormValue("commit_sha")),
		MetadataJSON: string(metadata),
		RequestedBy:  actor(r),
	}
	created, err := s.store.CreateDeploymentJob(r.Context(), job)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.recordAudit(r, "user", actor(r), "deployment.created", "deployment_job", created.ID, "")
	redirectDashboard(w, r)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	hosts, err := s.store.ListHosts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	repos, err := s.store.ListRepositories(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	apps, err := s.store.ListApps(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jobs, err := s.store.ListDeploymentJobs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	events, err := s.store.ListAuditEvents(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	// Determine onboarding state/highlight
	var step1Class, step2Class, step3Class, step4Class string
	if len(hosts) == 0 {
		step1Class = "active-step"
	} else if len(repos) == 0 {
		step2Class = "active-step"
	} else if len(apps) == 0 {
		step3Class = "active-step"
	} else {
		step4Class = "active-step"
	}

	_, _ = fmt.Fprintf(w, `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>C-Plane Control Plane</title>
  <style>
    :root {
      --font-sans: 'Inter', system-ui, -apple-system, sans-serif;
      --bg-primary: #f8fafc;
      --bg-secondary: #ffffff;
      --text-primary: #0f172a;
      --text-secondary: #475569;
      --border-color: #e2e8f0;
      --primary: #2563eb;
      --primary-hover: #1d4ed8;
      --primary-light: #eff6ff;
      --primary-text: #1e40af;
      --success: #10b981;
      --success-light: #ecfdf5;
      --success-text: #047857;
      --shadow-sm: 0 1px 2px 0 rgba(0, 0, 0, 0.05);
      --shadow-md: 0 4px 6px -1px rgba(0, 0, 0, 0.1), 0 2px 4px -1px rgba(0, 0, 0, 0.06);
      --radius: 8px;
    }
    @media (prefers-color-scheme: dark) {
      :root {
        --bg-primary: #0f172a;
        --bg-secondary: #1e293b;
        --text-primary: #f8fafc;
        --text-secondary: #94a3b8;
        --border-color: #334155;
        --primary: #3b82f6;
        --primary-hover: #60a5fa;
        --primary-light: #1e3a8a;
        --primary-text: #93c5fd;
        --success: #34d399;
        --success-light: #064e3b;
        --success-text: #a7f3d0;
      }
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: var(--font-sans);
      background: var(--bg-primary);
      color: var(--text-primary);
      -webkit-font-smoothing: antialiased;
    }
    main {
      width: min(1200px, calc(100vw - 32px));
      margin: 0 auto;
      padding: 32px 0 64px;
    }
    header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 20px;
      margin-bottom: 24px;
      border-bottom: 1px solid var(--border-color);
      padding-bottom: 20px;
    }
    h1 {
      margin: 0;
      font-size: 26px;
      font-weight: 800;
      letter-spacing: -0.025em;
    }
    .subtitle {
      margin: 4px 0 0;
      color: var(--text-secondary);
      font-size: 14px;
    }
    .status {
      display: flex;
      align-items: center;
      gap: 8px;
      font-size: 13px;
      font-weight: 600;
      padding: 6px 12px;
      border-radius: 9999px;
      background: var(--success-light);
      color: var(--success-text);
      text-decoration: none;
    }
    .status-dot {
      width: 8px;
      height: 8px;
      background-color: var(--success);
      border-radius: 50%%;
    }
    .tabs-nav {
      display: flex;
      gap: 8px;
      margin-bottom: 24px;
      border-bottom: 1px solid var(--border-color);
      padding-bottom: 8px;
      overflow-x: auto;
    }
    .tab-btn {
      background: transparent;
      border: 0;
      color: var(--text-secondary);
      font-size: 14px;
      font-weight: 600;
      padding: 8px 16px;
      cursor: pointer;
      border-radius: var(--radius);
      transition: all 0.2s ease;
      white-space: nowrap;
    }
    .tab-btn:hover {
      background: var(--primary-light);
      color: var(--primary-text);
    }
    .tab-btn.active {
      background: var(--primary);
      color: #fff;
    }
    .tab-content {
      display: none;
    }
    .tab-content.active {
      display: block;
      animation: fadeIn 0.2s ease-in-out;
    }
    @keyframes fadeIn {
      from { opacity: 0; transform: translateY(4px); }
      to { opacity: 1; transform: translateY(0); }
    }
    .dashboard-grid {
      display: grid;
      grid-template-columns: 3fr 1fr;
      gap: 24px;
    }
    @media (max-width: 900px) {
      .dashboard-grid { grid-template-columns: 1fr; }
    }
    .metrics {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
      gap: 16px;
      margin-bottom: 24px;
    }
    .metric-card {
      background: var(--bg-secondary);
      border: 1px solid var(--border-color);
      border-radius: var(--radius);
      padding: 16px;
      box-shadow: var(--shadow-sm);
    }
    .metric-card span {
      display: block;
      font-size: 12px;
      font-weight: 600;
      color: var(--text-secondary);
      text-transform: uppercase;
      letter-spacing: 0.05em;
    }
    .metric-card strong {
      display: block;
      font-size: 28px;
      font-weight: 800;
      margin-top: 4px;
    }
    .card {
      background: var(--bg-secondary);
      border: 1px solid var(--border-color);
      border-radius: var(--radius);
      padding: 24px;
      margin-bottom: 24px;
      box-shadow: var(--shadow-sm);
    }
    .card-title {
      font-size: 18px;
      font-weight: 700;
      margin: 0 0 16px;
      letter-spacing: -0.01em;
    }
    .onboarding {
      background: var(--primary-light);
      border: 1px solid var(--border-color);
      border-radius: var(--radius);
      padding: 24px;
      margin-bottom: 24px;
    }
    .onboarding-title {
      font-size: 18px;
      font-weight: 700;
      color: var(--primary-text);
      margin: 0 0 12px;
    }
    .steps-list {
      display: flex;
      flex-direction: column;
      gap: 12px;
    }
    .step-item {
      display: flex;
      align-items: flex-start;
      gap: 12px;
      padding: 12px;
      background: var(--bg-secondary);
      border: 1px solid var(--border-color);
      border-radius: var(--radius);
      transition: all 0.2s ease;
    }
    .step-item.active-step {
      border-color: var(--primary);
      box-shadow: var(--shadow-md);
      transform: scale(1.01);
    }
    .step-number {
      display: flex;
      align-items: center;
      justify-content: center;
      width: 24px;
      height: 24px;
      border-radius: 50%%;
      background: var(--border-color);
      color: var(--text-secondary);
      font-size: 12px;
      font-weight: 700;
      margin-top: 2px;
    }
    .active-step .step-number {
      background: var(--primary);
      color: #fff;
    }
    .step-content {
      flex: 1;
    }
    .step-title {
      font-size: 14px;
      font-weight: 700;
      margin: 0 0 4px;
    }
    .step-desc {
      font-size: 13px;
      color: var(--text-secondary);
      margin: 0;
    }
    table {
      width: 100%%;
      border-collapse: collapse;
      font-size: 14px;
    }
    th, td {
      padding: 12px 16px;
      border-bottom: 1px solid var(--border-color);
      text-align: left;
      vertical-align: middle;
    }
    th {
      color: var(--text-secondary);
      font-size: 12px;
      font-weight: 600;
      text-transform: uppercase;
      letter-spacing: 0.05em;
    }
    tr:last-child td {
      border-bottom: 0;
    }
    .empty {
      padding: 32px 16px;
      text-align: center;
      color: var(--text-secondary);
      font-size: 14px;
    }
    .pill {
      display: inline-block;
      border-radius: 9999px;
      padding: 2px 10px;
      font-size: 12px;
      font-weight: 600;
      text-transform: capitalize;
    }
    .pill-info { background: var(--primary-light); color: var(--primary-text); }
    .pill-success { background: var(--success-light); color: var(--success-text); }
    .form-container {
      display: grid;
      gap: 16px;
      max-width: 500px;
    }
    .form-group {
      display: flex;
      flex-direction: column;
      gap: 6px;
    }
    .form-group label {
      font-size: 12px;
      font-weight: 700;
      color: var(--text-secondary);
      text-transform: uppercase;
      letter-spacing: 0.05em;
    }
    input, select {
      width: 100%%;
      padding: 10px 14px;
      font-family: var(--font-sans);
      font-size: 14px;
      background: var(--bg-secondary);
      border: 1px solid var(--border-color);
      border-radius: var(--radius);
      color: var(--text-primary);
      outline: none;
      transition: border-color 0.2s ease;
    }
    input:focus, select:focus {
      border-color: var(--primary);
    }
    .checkbox-group {
      flex-direction: row;
      align-items: center;
      gap: 8px;
    }
    .checkbox-group input {
      width: auto;
    }
    button, .btn {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      padding: 10px 20px;
      background: var(--primary);
      border: 0;
      color: #fff;
      font-weight: 600;
      font-size: 14px;
      border-radius: var(--radius);
      cursor: pointer;
      transition: background 0.2s ease;
      text-decoration: none;
    }
    button:hover, .btn:hover {
      background: var(--primary-hover);
    }
    .btn-secondary {
      background: transparent;
      border: 1px solid var(--border-color);
      color: var(--text-primary);
    }
    .btn-secondary:hover {
      background: var(--bg-primary);
    }
    .btn-danger {
      background: #dc2626;
    }
    .btn-danger:hover {
      background: #b91c1c;
    }
    .btn-small {
      padding: 6px 10px;
      font-size: 12px;
    }
    .inline-form {
      display: inline-flex;
      margin: 0;
    }
    .flex-header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      margin-bottom: 16px;
    }
    .flex-header h2 {
      margin: 0;
    }
    code {
      font-family: monospace;
      background: var(--bg-primary);
      padding: 2px 6px;
      border-radius: 4px;
      font-size: 13px;
    }
  </style>
</head>
<body>
  <main>
    <header>
      <div>
        <h1>C-Plane Control Plane</h1>
        <p class="subtitle">Deploy code instantly to self-managed VPS targets</p>
      </div>
      <a class="status" href="/healthz">
        <span class="status-dot"></span>
        API online
      </a>
    </header>

    <div class="metrics">
      <div class="metric-card"><span>Hosts</span><strong>%d</strong></div>
      <div class="metric-card"><span>Repositories</span><strong>%d</strong></div>
      <div class="metric-card"><span>Apps</span><strong>%d</strong></div>
      <div class="metric-card"><span>Deployments</span><strong>%d</strong></div>
      <div class="metric-card"><span>Audit Events</span><strong>%d</strong></div>
    </div>

    <nav class="tabs-nav">
      <button class="tab-btn active" onclick="switchTab('dashboard')">Dashboard</button>
      <button class="tab-btn" onclick="switchTab('hosts')">Hosts (%d)</button>
      <button class="tab-btn" onclick="switchTab('repos')">Repositories (%d)</button>
      <button class="tab-btn" onclick="switchTab('apps')">Applications (%d)</button>
      <button class="tab-btn" onclick="switchTab('deployments')">Deployments (%d)</button>
      <button class="tab-btn" onclick="switchTab('activity')">Activity Log</button>
    </nav>

    <!-- TAB: DASHBOARD -->
    <div id="tab-dashboard" class="tab-content active">
      <div class="dashboard-grid">
        <div class="left-panel">
          <div class="onboarding">
            <h2 class="onboarding-title">🎯 Getting Started Checklist</h2>
            <div class="steps-list">
              <div class="step-item %s">
                <div class="step-number">1</div>
                <div class="step-content">
                  <h3 class="step-title">Add Host Server</h3>
                  <p class="step-desc">Register your target VPS server. Go to the <strong>Hosts</strong> tab and click "Add Host" to generate your installation command.</p>
                </div>
              </div>
              <div class="step-item %s">
                <div class="step-number">2</div>
                <div class="step-content">
                  <h3 class="step-title">Connect Git Repository</h3>
                  <p class="step-desc">Connect your application source code. Go to the <strong>Repositories</strong> tab and link a GitHub/GitLab URL.</p>
                </div>
              </div>
              <div class="step-item %s">
                <div class="step-number">3</div>
                <div class="step-content">
                  <h3 class="step-title">Setup Server Application</h3>
                  <p class="step-desc">Go to the <strong>Applications</strong> tab, configure the project directory, and initialize the application on your target host.</p>
                </div>
              </div>
              <div class="step-item %s">
                <div class="step-number">4</div>
                <div class="step-content">
                  <h3 class="step-title">Trigger Deployment</h3>
                  <p class="step-desc">Trigger deploys automatically via Git webhooks, or manually queue jobs from the <strong>Deployments</strong> tab.</p>
                </div>
              </div>
            </div>
          </div>

          <div class="card">
            <h2 class="card-title">Recent Deployments</h2>
            %s
          </div>
        </div>

        <div class="right-panel">
          <div class="card">
            <h2 class="card-title" style="font-size:16px;">Quick Actions</h2>
            <div style="display:flex; flex-direction:column; gap:10px;">
              <button onclick="switchTab('hosts')" class="btn">Register New Host</button>
              <button onclick="switchTab('repos')" class="btn btn-secondary">Connect Repository</button>
              <button onclick="switchTab('apps')" class="btn btn-secondary">Setup New App</button>
              <button onclick="switchTab('deployments')" class="btn btn-secondary">Trigger Deploy</button>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- TAB: HOSTS -->
    <div id="tab-hosts" class="tab-content">
      <div class="card">
        <h2 class="card-title">Add Target Host</h2>
        %s
      </div>
      <div class="card">
        <div class="flex-header">
          <h2>Registered Hosts</h2>
        </div>
        %s
      </div>
    </div>

    <!-- TAB: REPOSITORIES -->
    <div id="tab-repos" class="tab-content">
      <div class="card">
        <h2 class="card-title">Connect Git Repository</h2>
        %s
      </div>
      <div class="card">
        <div class="flex-header">
          <h2>Connected Repositories</h2>
        </div>
        %s
      </div>
    </div>

    <!-- TAB: APPLICATIONS -->
    <div id="tab-apps" class="tab-content">
      <div class="card">
        <h2 class="card-title">Setup Server Application</h2>
        %s
      </div>
      <div class="card">
        <div class="flex-header">
          <h2>Configured Applications</h2>
        </div>
        %s
      </div>
    </div>

    <!-- TAB: DEPLOYMENTS -->
    <div id="tab-deployments" class="tab-content">
      <div class="card">
        <h2 class="card-title">Trigger Manual Deploy</h2>
        %s
      </div>
      <div class="card">
        <div class="flex-header">
          <h2>Deployment History</h2>
        </div>
        %s
      </div>
    </div>

    <!-- TAB: ACTIVITY LOG -->
    <div id="tab-activity" class="tab-content">
      <div class="card">
        <div class="flex-header">
          <h2>Audit & Activity Events</h2>
        </div>
        %s
      </div>
    </div>
  </main>

  <script>
    function switchTab(tabId) {
      document.querySelectorAll('.tab-content').forEach(el => el.classList.remove('active'));
      document.querySelectorAll('.tab-btn').forEach(el => el.classList.remove('active'));
      
      const targetContent = document.getElementById('tab-' + tabId);
      if (targetContent) {
        targetContent.classList.add('active');
      }
      
      const targetBtn = Array.from(document.querySelectorAll('.tab-btn')).find(b => b.getAttribute('onclick').includes(tabId));
      if (targetBtn) {
        targetBtn.classList.add('active');
      }
      
      localStorage.setItem('cplane_active_tab', tabId);
    }
    
    document.addEventListener('DOMContentLoaded', () => {
      const activeTab = localStorage.getItem('cplane_active_tab') || 'dashboard';
      switchTab(activeTab);
    });
  </script>
</body>
</html>`,
		len(hosts), len(repos), len(apps), len(jobs), len(events),
		len(hosts), len(repos), len(apps), len(jobs),
		step1Class, step2Class, step3Class, step4Class,
		renderJobs(jobs),
		renderHostForm(), renderHosts(hosts),
		renderRepoForm(), renderRepositories(repos),
		renderSetupAppForm(hosts, repos), renderApps(apps),
		renderDeployForm(apps), renderJobs(jobs),
		renderAuditEvents(events))
}

func renderHosts(hosts []model.Host) string {
	if len(hosts) == 0 {
		return `<div class="empty">No hosts registered yet. Register a host using the form above.</div>`
	}
	var b strings.Builder
	b.WriteString(`<table><thead><tr><th>Name</th><th>Status</th><th>Last Seen</th><th>Agent Version</th><th>Action</th></tr></thead><tbody>`)
	for _, host := range hosts {
		statusPill := `<span class="pill pill-info">offline</span>`
		if host.Status == "online" {
			statusPill = `<span class="pill pill-success">online</span>`
		}
		fmt.Fprintf(&b, `<tr><td><strong>%s</strong><br><code>%s</code></td><td>%s</td><td><code>%s</code></td><td>%s</td><td><form method="post" action="/dashboard/hosts/%s/delete" class="inline-form"><button type="submit" class="btn-danger btn-small">Delete</button></form></td></tr>`,
			escape(host.Name), escape(host.ID), statusPill, escape(formatOptionalTime(host.LastSeenAt, "Never")), escape(blank(host.AgentVersion, "never connected")), escape(host.ID))
	}
	b.WriteString(`</tbody></table>`)
	return b.String()
}

func renderRepositories(repos []model.Repository) string {
	if len(repos) == 0 {
		return `<div class="empty">No repositories connected yet.</div>`
	}
	var b strings.Builder
	b.WriteString(`<table><thead><tr><th>Name</th><th>Provider</th><th>URL</th><th>Branch</th><th>Action</th></tr></thead><tbody>`)
	for _, repo := range repos {
		fmt.Fprintf(&b, `<tr><td><strong>%s</strong><br><code>%s</code></td><td><span class="pill pill-info">%s</span></td><td><code>%s</code></td><td><code>%s</code></td><td><form method="post" action="/dashboard/repos/%s/delete" class="inline-form"><button type="submit" class="btn-danger btn-small">Delete</button></form></td></tr>`,
			escape(repo.Name), escape(repo.ID), escape(repo.Provider), escape(repo.URL), escape(repo.DefaultBranch), escape(repo.ID))
	}
	b.WriteString(`</tbody></table>`)
	return b.String()
}

func renderApps(apps []model.App) string {
	if len(apps) == 0 {
		return `<div class="empty">No applications set up yet.</div>`
	}
	var b strings.Builder
	b.WriteString(`<table><thead><tr><th>Name</th><th>Target Host</th><th>Root Path</th></tr></thead><tbody>`)
	for _, app := range apps {
		fmt.Fprintf(&b, `<tr><td><strong>%s</strong><br><code>%s</code></td><td><code>%s</code></td><td><code>%s</code></td></tr>`,
			escape(app.Name), escape(app.ID), escape(app.HostID), escape(blank(app.RootPath, "not set")))
	}
	b.WriteString(`</tbody></table>`)
	return b.String()
}

func renderHostForm() string {
	return `<form method="post" action="/dashboard/hosts" class="form-container">
  <div class="form-group">
    <label>Host Name</label>
    <input name="name" placeholder="e.g. production-vps-1" required>
  </div>
  <button type="submit">Create Host & Get Install Command</button>
</form>`
}

func renderRepoForm() string {
	return `<form method="post" action="/dashboard/repos" class="form-container">
  <div class="form-group">
    <label>Name</label>
    <input name="name" placeholder="e.g. backend-api" required>
  </div>
  <div class="form-group">
    <label>Provider</label>
    <select name="provider">
      <option value="github">GitHub</option>
      <option value="gitlab">GitLab</option>
      <option value="generic">Generic Git</option>
    </select>
  </div>
  <div class="form-group">
    <label>Repository URL</label>
    <input name="url" placeholder="https://github.com/org/repo" required>
  </div>
  <div class="form-group">
    <label>Default Branch</label>
    <input name="default_branch" value="main" required>
  </div>
  <button type="submit">Connect Repository</button>
</form>`
}

func renderDeployForm(apps []model.App) string {
	if len(apps) == 0 {
		return `<div class="empty">Create an app before triggering a deploy.</div>`
	}
	var b strings.Builder
	b.WriteString(`<form method="post" action="/dashboard/deployments" class="form-container">
  <div class="form-group">
    <label>Application</label>
    <select name="app_id" required>`)
	for _, app := range apps {
		fmt.Fprintf(&b, `<option value="%s">%s</option>`, escape(app.ID), escape(app.Name))
	}
	b.WriteString(`</select>
  </div>
  <div class="form-group">
    <label>Ref, Branch, or Tag</label>
    <input name="ref" value="main" required>
  </div>
  <div class="form-group">
    <label>Commit SHA (Optional)</label>
    <input name="commit_sha" placeholder="e.g. a1b2c3d4">
  </div>
  <button type="submit">Queue Deployment Job</button>
</form>`)
	return b.String()
}

func renderSetupAppForm(hosts []model.Host, repos []model.Repository) string {
	if len(hosts) == 0 || len(repos) == 0 {
		return `<div class="empty">Register at least one host and connect one repository before setting up an application.</div>`
	}
	var b strings.Builder
	b.WriteString(`<form method="post" action="/dashboard/setup-apps" class="form-container">
  <div class="form-group">
    <label>App Name</label>
    <input name="name" placeholder="e.g. web-portal" required>
  </div>
  <div class="form-group">
    <label>Source Repository</label>
    <select name="repo_id" required>`)
	for _, repo := range repos {
		fmt.Fprintf(&b, `<option value="%s">%s</option>`, escape(repo.ID), escape(repo.Name))
	}
	b.WriteString(`</select>
  </div>
  <div class="form-group">
    <label>Target Host</label>
    <select name="host_id" required>`)
	for _, host := range hosts {
		fmt.Fprintf(&b, `<option value="%s">%s</option>`, escape(host.ID), escape(host.Name))
	}
	b.WriteString(`</select>
  </div>
  <div class="form-group">
    <label>Environment</label>
    <input name="environment_id" value="production" required>
  </div>
  <div class="form-group">
    <label>Project Root Path on Host</label>
    <input name="root_path" placeholder="e.g. /var/www/web-portal" required>
  </div>
  <div class="form-group">
    <label>Domain Name (Optional)</label>
    <input name="domain" placeholder="e.g. portal.example.com">
  </div>
  <div class="form-group">
    <label>Runtime Stack</label>
    <select name="runtime">
      <option value="static">Static Site</option>
      <option value="node">Node.js</option>
      <option value="go">Go</option>
      <option value="php">PHP</option>
      <option value="custom">Custom (Recipe controlled)</option>
    </select>
  </div>
  <div class="form-group">
    <label>Deploy Ref</label>
    <input name="ref" value="main" required>
  </div>
  <div class="form-group checkbox-group">
    <input type="checkbox" id="nginx_enabled" name="nginx_enabled" value="1" checked>
    <label for="nginx_enabled">Automatically manage Nginx configuration</label>
  </div>
  <button type="submit">Initialize & Setup Application</button>
</form>`)
	return b.String()
}

func renderJobs(jobs []model.DeploymentJob) string {
	if len(jobs) == 0 {
		return `<div class="empty">No deployment history.</div>`
	}
	var b strings.Builder
	b.WriteString(`<table><thead><tr><th>Job ID</th><th>Action</th><th>Ref</th><th>Status</th></tr></thead><tbody>`)
	for _, job := range jobs {
		statusClass := "pill-info"
		if job.Status == "success" {
			statusClass = "pill-success"
		}
		fmt.Fprintf(&b, `<tr><td><code>%s</code></td><td><strong>%s</strong></td><td><code>%s</code></td><td><span class="pill %s">%s</span></td></tr>`,
			escape(job.ID), escape(job.Action), escape(blank(job.Ref, job.CommitSHA)), statusClass, escape(job.Status))
	}
	b.WriteString(`</tbody></table>`)
	return b.String()
}

func renderAuditEvents(events []model.AuditEvent) string {
	if len(events) == 0 {
		return `<div class="empty">No audit events recorded yet.</div>`
	}
	var b strings.Builder
	b.WriteString(`<table><thead><tr><th>Time</th><th>Action</th><th>Actor</th><th>Resource</th></tr></thead><tbody>`)
	for _, event := range events {
		fmt.Fprintf(&b, `<tr><td><code>%s</code></td><td><strong>%s</strong></td><td>%s (<code>%s</code>)</td><td>%s (<code>%s</code>)</td></tr>`,
			escape(formatTime(event.CreatedAt)), escape(event.Action), escape(event.ActorType), escape(event.ActorID), escape(event.ResourceType), escape(event.ResourceID))
	}
	b.WriteString(`</tbody></table>`)
	return b.String()
}

func blank(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func formatOptionalTime(value *time.Time, fallback string) string {
	if value == nil || value.IsZero() {
		return fallback
	}
	return formatTime(*value)
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return "Never"
	}
	return value.UTC().Format("2006-01-02 15:04:05 UTC")
}

func escape(value string) string {
	return html.EscapeString(value)
}

func (s *Server) createHost(r *http.Request, name string) (model.Host, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return model.Host{}, fmt.Errorf("name is required")
	}
	hostID := id.New("srv")
	installToken := id.New("install")
	baseURL := externalBaseURL(r)
	installCommand := "curl -fsSLo install-agent.sh " + agentInstallerURL + " && sudo bash install-agent.sh --api-url " + baseURL + " --host-id " + hostID + " --token " + installToken + " --run-as-root"
	host := model.Host{
		ID:             hostID,
		Name:           name,
		Status:         "offline",
		MQTTUsername:   hostID,
		AgentTokenHash: hashToken(installToken),
		InstallToken:   installToken,
		InstallCommand: installCommand,
	}
	created, err := s.store.CreateHost(r.Context(), host)
	if err != nil {
		return model.Host{}, err
	}
	created.InstallToken = installToken
	created.InstallCommand = installCommand
	s.recordAudit(r, "user", actor(r), "host.registered", "host", created.ID, "")
	return created, nil
}

func writeInstallCommandPage(w http.ResponseWriter, host model.Host) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_, _ = fmt.Fprintf(w, `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Install Agent - C-Plane</title>
  <style>
    :root { font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; background: #f5f7fa; color: #15181d; }
    body { margin: 0; }
    main { width: min(880px, calc(100vw - 32px)); margin: 0 auto; padding: 36px 0; }
    .panel { border: 1px solid #d7dce2; border-radius: 8px; background: #fff; padding: 22px; box-shadow: 0 10px 28px rgba(18, 24, 31, 0.06); }
    h1 { margin: 0 0 8px; font-size: 26px; letter-spacing: 0; }
    p { margin: 0 0 16px; color: #56616f; line-height: 1.5; }
    pre { white-space: pre-wrap; word-break: break-word; background: #101418; color: #f3f5f7; border-radius: 8px; padding: 16px; overflow: auto; }
    a { display: inline-flex; min-height: 36px; align-items: center; border: 1px solid #cbd4df; border-radius: 6px; padding: 0 12px; color: #1e2b3a; text-decoration: none; font-weight: 700; }
  </style>
</head>
<body>
  <main>
    <div class="panel">
      <h1>Install Agent</h1>
      <p>Host <strong>%s</strong> was created. Copy this command to the target server. This token is shown only once.</p>
      <pre>%s</pre>
      <a href="/">Back to dashboard</a>
    </div>
  </main>
</body>
</html>`, escape(host.Name), escape(host.InstallCommand))
}

func redirectDashboard(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func externalBaseURL(r *http.Request) string {
	proto := r.Header.Get("X-Forwarded-Proto")
	if proto == "" {
		if r.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	if host == "" {
		host = "localhost"
	}
	return proto + "://" + host
}

func (s *Server) handleCreateHost(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if strings.TrimSpace(input.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	created, err := s.createHost(r, input.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleListHosts(w http.ResponseWriter, r *http.Request) {
	hosts, err := s.store.ListHosts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, hosts)
}

func (s *Server) handleDeleteHost(w http.ResponseWriter, r *http.Request) {
	hostID := r.PathValue("id")
	if err := s.store.DeleteHost(r.Context(), hostID); err != nil {
		writeStoreError(w, err)
		return
	}
	s.recordAudit(r, "user", actor(r), "host.deleted", "host", hostID, "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCreateRepository(w http.ResponseWriter, r *http.Request) {
	var repo model.Repository
	if !decodeJSON(w, r, &repo) {
		return
	}
	if repo.Name == "" || repo.URL == "" {
		writeError(w, http.StatusBadRequest, "name and url are required")
		return
	}
	if repo.Provider == "" {
		repo.Provider = "github"
	}
	if repo.DefaultBranch == "" {
		repo.DefaultBranch = "main"
	}
	repo.ID = id.New("repo")
	created, err := s.store.CreateRepository(r.Context(), repo)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.recordAudit(r, "user", actor(r), "repo.created", "repo", created.ID, "")
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleListRepositories(w http.ResponseWriter, r *http.Request) {
	repos, err := s.store.ListRepositories(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, repos)
}

func (s *Server) handleDeleteRepository(w http.ResponseWriter, r *http.Request) {
	repoID := r.PathValue("id")
	if err := s.store.DeleteRepository(r.Context(), repoID); err != nil {
		writeStoreError(w, err)
		return
	}
	s.recordAudit(r, "user", actor(r), "repo.deleted", "repo", repoID, "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCreateApp(w http.ResponseWriter, r *http.Request) {
	var app model.App
	if !decodeJSON(w, r, &app) {
		return
	}
	if app.Name == "" || app.RepoID == "" || app.HostID == "" {
		writeError(w, http.StatusBadRequest, "name, repo_id, and host_id are required")
		return
	}
	if app.EnvironmentID == "" {
		app.EnvironmentID = "default"
	}
	if app.SuccessfulReleasesKeep > 0 && app.SuccessfulReleasesKeep < 3 {
		writeError(w, http.StatusBadRequest, "successful_releases_keep must be at least 3")
		return
	}
	app.ID = id.New("app")
	created, err := s.store.CreateApp(r.Context(), app)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.recordAudit(r, "user", actor(r), "app.created", "app", created.ID, "")
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleListApps(w http.ResponseWriter, r *http.Request) {
	apps, err := s.store.ListApps(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, apps)
}

func (s *Server) handleCreateDeployment(w http.ResponseWriter, r *http.Request) {
	var job model.DeploymentJob
	if !decodeJSON(w, r, &job) {
		return
	}
	if job.AppID == "" {
		writeError(w, http.StatusBadRequest, "app_id is required")
		return
	}
	app, err := s.store.GetApp(r.Context(), job.AppID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	repo, err := s.store.GetRepository(r.Context(), app.RepoID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if strings.TrimSpace(job.Ref) == "" {
		job.Ref = repo.DefaultBranch
	}
	if strings.TrimSpace(job.Action) == "" {
		job.Action = "deploy"
	}
	metadata, err := json.Marshal(deployMetadata{
		AppName:    app.Name,
		RootPath:   app.RootPath,
		RecipePath: app.RecipePath,
		RepoURL:    repo.URL,
		Ref:        job.Ref,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	job.ID = id.New("job")
	job.HostID = app.HostID
	job.RepoID = app.RepoID
	job.MetadataJSON = string(metadata)
	if job.RequestedBy == "" {
		job.RequestedBy = actor(r)
	}
	created, err := s.store.CreateDeploymentJob(r.Context(), job)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.recordAudit(r, "user", actor(r), "deployment.created", "deployment_job", created.ID, "")
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleListDeployments(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.store.ListDeploymentJobs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (s *Server) handleGetDeployment(w http.ResponseWriter, r *http.Request) {
	job, err := s.store.GetDeploymentJob(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleApproveDeployment(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")
	if err := s.store.ApproveDeploymentJob(r.Context(), jobID, actor(r)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.recordAudit(r, "user", actor(r), "deployment.approved", "deployment_job", jobID, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "approved"})
}

func (s *Server) handleCancelDeployment(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")
	if err := s.store.CancelDeploymentJob(r.Context(), jobID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.recordAudit(r, "user", actor(r), "deployment.canceled", "deployment_job", jobID, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "canceled"})
}

func (s *Server) handleListAppReleases(w http.ResponseWriter, r *http.Request) {
	releases, err := s.store.ListReleasesByApp(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, releases)
}

func (s *Server) handleRollbackRelease(w http.ResponseWriter, r *http.Request) {
	release, err := s.store.GetRelease(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !release.AvailableForRollback {
		writeError(w, http.StatusConflict, "release is not available for rollback")
		return
	}
	app, err := s.store.GetApp(r.Context(), release.AppID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	job := model.DeploymentJob{
		ID:          id.New("job"),
		AppID:       app.ID,
		HostID:      app.HostID,
		RepoID:      app.RepoID,
		Action:      "rollback",
		Status:      "queued",
		ReleaseID:   release.ID,
		CommitSHA:   release.CommitSHA,
		RequestedBy: actor(r),
	}
	created, err := s.store.CreateDeploymentJob(r.Context(), job)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.recordAudit(r, "user", actor(r), "release.rollback_requested", "release", release.ID, `{"job_id":"`+created.ID+`"}`)
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleAgentRegister(w http.ResponseWriter, r *http.Request) {
	var input struct {
		HostID       string `json:"host_id"`
		InstallToken string `json:"install_token"`
		AgentVersion string `json:"agent_version"`
		Hostname     string `json:"hostname"`
		OS           string `json:"os"`
		Arch         string `json:"arch"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.HostID == "" || input.InstallToken == "" {
		writeError(w, http.StatusBadRequest, "host_id and install_token are required")
		return
	}

	agentToken := id.New("agent")
	if err := s.store.RegisterHostAgent(r.Context(), input.HostID, hashToken(input.InstallToken), hashToken(agentToken), input.AgentVersion); err != nil {
		writeStoreError(w, err)
		return
	}
	s.recordAudit(r, "agent", input.HostID, "agent.registered", "host", input.HostID, `{"hostname":"`+input.Hostname+`","os":"`+input.OS+`","arch":"`+input.Arch+`"}`)
	writeJSON(w, http.StatusOK, map[string]string{
		"agent_token":   agentToken,
		"mqtt_url":      "mqtts://deploy.example.com:8883",
		"mqtt_username": input.HostID,
		"mqtt_password": "",
	})
}

func (s *Server) handleAgentHeartbeat(w http.ResponseWriter, r *http.Request) {
	var input struct {
		HostID       string `json:"host_id"`
		AgentVersion string `json:"agent_version"`
		Status       string `json:"status"`
		RunningJobs  int    `json:"running_jobs"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.HostID == "" {
		writeError(w, http.StatusBadRequest, "host_id is required")
		return
	}
	if err := s.store.UpdateHostHeartbeat(r.Context(), input.HostID, input.AgentVersion); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.recordAudit(r, "agent", input.HostID, "agent.heartbeat", "host", input.HostID, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAgentPendingJobs(w http.ResponseWriter, r *http.Request) {
	hostID := r.URL.Query().Get("host_id")
	if hostID == "" {
		writeError(w, http.StatusBadRequest, "host_id is required")
		return
	}
	jobs, err := s.store.ListPendingJobs(r.Context(), hostID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (s *Server) handleAgentGetJob(w http.ResponseWriter, r *http.Request) {
	job, err := s.store.GetDeploymentJob(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleAgentStartJob(w http.ResponseWriter, r *http.Request) {
	s.agentSetJobStatus(w, r, "running", "agent.job_started")
}

func (s *Server) handleAgentCompleteJob(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")
	job, err := s.store.GetDeploymentJob(r.Context(), jobID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var input completeJobRequest
	if r.Body != nil && r.ContentLength != 0 {
		if !decodeJSON(w, r, &input) {
			return
		}
	}
	if input.ReleasePath != "" || input.ReleaseKey != "" {
		now := time.Now().UTC()
		releaseKey := blank(input.ReleaseKey, jobID)
		_, err := s.store.CreateRelease(r.Context(), model.Release{
			ID:                   id.New("rel"),
			AppID:                job.AppID,
			DeploymentJobID:      job.ID,
			ReleaseKey:           releaseKey,
			CommitSHA:            blank(input.CommitSHA, job.CommitSHA),
			ArtifactChecksum:     input.ArtifactChecksum,
			Path:                 input.ReleasePath,
			Status:               "active",
			AvailableForRollback: true,
			ActivatedAt:          &now,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	s.agentSetJobStatus(w, r, "success", "agent.job_completed")
}

func (s *Server) handleAgentFailJob(w http.ResponseWriter, r *http.Request) {
	s.agentSetJobStatus(w, r, "failed", "agent.job_failed")
}

func (s *Server) agentSetJobStatus(w http.ResponseWriter, r *http.Request, status, auditAction string) {
	jobID := r.PathValue("id")
	if err := s.store.UpdateDeploymentJobStatus(r.Context(), jobID, status); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.recordAudit(r, "agent", r.URL.Query().Get("host_id"), auditAction, "deployment_job", jobID, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

func (s *Server) handleAgentJobLogs(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Message string `json:"message"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.Message == "" {
		writeError(w, http.StatusBadRequest, "message is required")
		return
	}
	created, err := s.store.AddJobLog(r.Context(), model.JobLog{
		ID:      id.New("log"),
		JobID:   r.PathValue("id"),
		Message: input.Message,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleListAuditEvents(w http.ResponseWriter, r *http.Request) {
	events, err := s.store.ListAuditEvents(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (s *Server) recordAudit(r *http.Request, actorType, actorID, action, resourceType, resourceID, metadata string) {
	if actorID == "" {
		actorID = "unknown"
	}
	_ = s.store.RecordAudit(r.Context(), model.AuditEvent{
		ID:           id.New("audit"),
		ActorType:    actorType,
		ActorID:      actorID,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		MetadataJSON: metadata,
		IPAddress:    clientIP(r),
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeStoreError(w http.ResponseWriter, err error) {
	if sqlitestore.IsNotFound(err) {
		writeError(w, http.StatusNotFound, "resource not found")
		return
	}
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func actor(r *http.Request) string {
	if value := r.Header.Get("X-CPlane-Actor"); value != "" {
		return value
	}
	return "local"
}

func clientIP(r *http.Request) string {
	if value := r.Header.Get("X-Forwarded-For"); value != "" {
		return strings.TrimSpace(strings.Split(value, ",")[0])
	}
	return r.RemoteAddr
}
