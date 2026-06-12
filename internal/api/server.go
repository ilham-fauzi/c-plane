package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ilham/c-plane/internal/id"
	"github.com/ilham/c-plane/internal/model"
	"github.com/ilham/c-plane/internal/store"
	"github.com/ilham/c-plane/internal/store/sqlitestore"
)

type Server struct {
	store store.Store
	mux   *http.ServeMux
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
	s.mux.HandleFunc("GET /healthz", s.handleHealth)

	s.mux.HandleFunc("GET /api/hosts", s.handleListHosts)
	s.mux.HandleFunc("POST /api/hosts", s.handleCreateHost)

	s.mux.HandleFunc("GET /api/repos", s.handleListRepositories)
	s.mux.HandleFunc("POST /api/repos", s.handleCreateRepository)

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

	hostID := id.New("srv")
	installToken := id.New("install")
	host := model.Host{
		ID:             hostID,
		Name:           input.Name,
		Status:         "offline",
		MQTTUsername:   hostID,
		AgentTokenHash: hashToken(installToken),
		InstallToken:   installToken,
		InstallCommand: "curl -fsSLo install-agent.sh https://deploy.example.com/install-agent.sh && sudo bash install-agent.sh --api-url https://deploy.example.com --host-id " + hostID + " --token " + installToken,
	}
	created, err := s.store.CreateHost(r.Context(), host)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	created.InstallToken = installToken
	created.InstallCommand = host.InstallCommand
	s.recordAudit(r, "user", actor(r), "host.registered", "host", created.ID, "")
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
	job.ID = id.New("job")
	job.HostID = app.HostID
	job.RepoID = app.RepoID
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
