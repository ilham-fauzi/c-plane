package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/ilham/c-plane/internal/model"
	"github.com/ilham/c-plane/internal/store"
	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

var _ store.Store = (*Store)(nil)

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return err
	}
	return s.addColumnIfMissing(ctx, "deployment_jobs", "metadata_json", "TEXT NOT NULL DEFAULT ''")
}

func (s *Store) addColumnIfMissing(ctx context.Context, table, column, definition string) error {
	_, err := s.db.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN "+column+" "+definition)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		return nil
	}
	return err
}

func (s *Store) CreateHost(ctx context.Context, host model.Host) (model.Host, error) {
	now := nowUTC()
	host.CreatedAt = now
	host.UpdatedAt = now
	if host.Status == "" {
		host.Status = "offline"
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO hosts (id, name, status, last_seen_at, agent_version, mqtt_username, agent_token_hash, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		host.ID, host.Name, host.Status, host.LastSeenAt, host.AgentVersion, host.MQTTUsername, host.AgentTokenHash, host.CreatedAt, host.UpdatedAt)
	return host, err
}

func (s *Store) ListHosts(ctx context.Context) ([]model.Host, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, status, last_seen_at, agent_version, mqtt_username, agent_token_hash, created_at, updated_at
		FROM hosts ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	hosts := make([]model.Host, 0)
	for rows.Next() {
		var host model.Host
		if err := rows.Scan(&host.ID, &host.Name, &host.Status, &host.LastSeenAt, &host.AgentVersion, &host.MQTTUsername, &host.AgentTokenHash, &host.CreatedAt, &host.UpdatedAt); err != nil {
			return nil, err
		}
		hosts = append(hosts, host)
	}
	return hosts, rows.Err()
}

func (s *Store) RegisterHostAgent(ctx context.Context, hostID, installTokenHash, agentTokenHash, agentVersion string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE hosts
		SET status = 'online',
		    last_seen_at = ?,
		    agent_version = ?,
		    agent_token_hash = ?,
		    updated_at = ?
		WHERE id = ? AND agent_token_hash = ?`,
		nowUTC(), agentVersion, agentTokenHash, nowUTC(), hostID, installTokenHash)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) UpdateHostHeartbeat(ctx context.Context, hostID, agentVersion string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE hosts SET status = 'online', last_seen_at = ?, agent_version = ?, updated_at = ? WHERE id = ?`,
		nowUTC(), agentVersion, nowUTC(), hostID)
	return err
}

func (s *Store) CreateRepository(ctx context.Context, repo model.Repository) (model.Repository, error) {
	now := nowUTC()
	repo.CreatedAt = now
	repo.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO repos (id, name, provider, url, default_branch, webhook_secret_hash, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		repo.ID, repo.Name, repo.Provider, repo.URL, repo.DefaultBranch, repo.WebhookSecretHash, repo.CreatedAt, repo.UpdatedAt)
	return repo, err
}

func (s *Store) ListRepositories(ctx context.Context) ([]model.Repository, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, provider, url, default_branch, webhook_secret_hash, created_at, updated_at
		FROM repos ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	repos := make([]model.Repository, 0)
	for rows.Next() {
		var repo model.Repository
		if err := rows.Scan(&repo.ID, &repo.Name, &repo.Provider, &repo.URL, &repo.DefaultBranch, &repo.WebhookSecretHash, &repo.CreatedAt, &repo.UpdatedAt); err != nil {
			return nil, err
		}
		repos = append(repos, repo)
	}
	return repos, rows.Err()
}

func (s *Store) CreateApp(ctx context.Context, app model.App) (model.App, error) {
	now := nowUTC()
	app.CreatedAt = now
	app.UpdatedAt = now
	if app.SuccessfulReleasesKeep == 0 {
		app.SuccessfulReleasesKeep = 5
	}
	if app.FailedReleasesTTLHours == 0 {
		app.FailedReleasesTTLHours = 72
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO apps (id, name, repo_id, host_id, environment_id, root_path, recipe_path, successful_releases_keep, failed_releases_ttl_hours, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		app.ID, app.Name, app.RepoID, app.HostID, app.EnvironmentID, app.RootPath, app.RecipePath, app.SuccessfulReleasesKeep, app.FailedReleasesTTLHours, app.CreatedAt, app.UpdatedAt)
	return app, err
}

func (s *Store) ListApps(ctx context.Context) ([]model.App, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, repo_id, host_id, environment_id, root_path, recipe_path, successful_releases_keep, failed_releases_ttl_hours, created_at, updated_at
		FROM apps ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	apps := make([]model.App, 0)
	for rows.Next() {
		app, err := scanApp(rows)
		if err != nil {
			return nil, err
		}
		apps = append(apps, app)
	}
	return apps, rows.Err()
}

func (s *Store) GetApp(ctx context.Context, appID string) (model.App, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, repo_id, host_id, environment_id, root_path, recipe_path, successful_releases_keep, failed_releases_ttl_hours, created_at, updated_at
		FROM apps WHERE id = ?`, appID)
	return scanApp(row)
}

func (s *Store) CreateDeploymentJob(ctx context.Context, job model.DeploymentJob) (model.DeploymentJob, error) {
	now := nowUTC()
	job.CreatedAt = now
	job.UpdatedAt = now
	if job.Action == "" {
		job.Action = "deploy"
	}
	if job.Status == "" {
		job.Status = "queued"
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO deployment_jobs (
			id, app_id, host_id, repo_id, action, status, ref, commit_sha, artifact_url,
			artifact_checksum, release_id, metadata_json, requested_by, approved_by, started_at, finished_at, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.AppID, job.HostID, job.RepoID, job.Action, job.Status, job.Ref, job.CommitSHA, job.ArtifactURL,
		job.ArtifactChecksum, job.ReleaseID, job.MetadataJSON, job.RequestedBy, job.ApprovedBy, job.StartedAt, job.FinishedAt, job.CreatedAt, job.UpdatedAt)
	return job, err
}

func (s *Store) ListDeploymentJobs(ctx context.Context) ([]model.DeploymentJob, error) {
	return s.queryJobs(ctx, `
		SELECT id, app_id, host_id, repo_id, action, status, ref, commit_sha, artifact_url, artifact_checksum, release_id, metadata_json, requested_by, approved_by, started_at, finished_at, created_at, updated_at
		FROM deployment_jobs ORDER BY created_at DESC`)
}

func (s *Store) GetDeploymentJob(ctx context.Context, id string) (model.DeploymentJob, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, app_id, host_id, repo_id, action, status, ref, commit_sha, artifact_url, artifact_checksum, release_id, metadata_json, requested_by, approved_by, started_at, finished_at, created_at, updated_at
		FROM deployment_jobs WHERE id = ?`, id)
	return scanJob(row)
}

func (s *Store) UpdateDeploymentJobStatus(ctx context.Context, id, status string) error {
	now := nowUTC()
	var startedAt any
	var finishedAt any
	if status == "running" {
		startedAt = now
	}
	if status == "success" || status == "failed" || status == "canceled" {
		finishedAt = now
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE deployment_jobs
		SET status = ?,
		    started_at = COALESCE(?, started_at),
		    finished_at = COALESCE(?, finished_at),
		    updated_at = ?
		WHERE id = ?`, status, startedAt, finishedAt, now, id)
	return err
}

func (s *Store) ApproveDeploymentJob(ctx context.Context, id, actor string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE deployment_jobs SET approved_by = ?, updated_at = ? WHERE id = ?`, actor, nowUTC(), id)
	return err
}

func (s *Store) CancelDeploymentJob(ctx context.Context, id string) error {
	return s.UpdateDeploymentJobStatus(ctx, id, "canceled")
}

func (s *Store) ListPendingJobs(ctx context.Context, hostID string) ([]model.DeploymentJob, error) {
	return s.queryJobs(ctx, `
		SELECT id, app_id, host_id, repo_id, action, status, ref, commit_sha, artifact_url, artifact_checksum, release_id, metadata_json, requested_by, approved_by, started_at, finished_at, created_at, updated_at
		FROM deployment_jobs WHERE host_id = ? AND status IN ('queued', 'signaled') ORDER BY created_at ASC`, hostID)
}

func (s *Store) CreateRelease(ctx context.Context, release model.Release) (model.Release, error) {
	release.CreatedAt = nowUTC()
	if release.Status == "" {
		release.Status = "preparing"
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO releases (id, app_id, deployment_job_id, release_key, commit_sha, artifact_checksum, path, status, available_for_rollback, retained_until, activated_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		release.ID, release.AppID, release.DeploymentJobID, release.ReleaseKey, release.CommitSHA, release.ArtifactChecksum, release.Path,
		release.Status, release.AvailableForRollback, release.RetainedUntil, release.ActivatedAt, release.CreatedAt)
	return release, err
}

func (s *Store) ListReleasesByApp(ctx context.Context, appID string) ([]model.Release, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, app_id, deployment_job_id, release_key, commit_sha, artifact_checksum, path, status, available_for_rollback, retained_until, activated_at, created_at
		FROM releases WHERE app_id = ? ORDER BY created_at DESC`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanReleases(rows)
}

func (s *Store) GetRelease(ctx context.Context, id string) (model.Release, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, app_id, deployment_job_id, release_key, commit_sha, artifact_checksum, path, status, available_for_rollback, retained_until, activated_at, created_at
		FROM releases WHERE id = ?`, id)
	return scanRelease(row)
}

func (s *Store) MarkReleaseRollbackUnavailable(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE releases SET available_for_rollback = 0 WHERE id = ?`, id)
	return err
}

func (s *Store) AddJobLog(ctx context.Context, log model.JobLog) (model.JobLog, error) {
	log.CreatedAt = nowUTC()
	_, err := s.db.ExecContext(ctx, `INSERT INTO job_logs (id, job_id, message, created_at) VALUES (?, ?, ?, ?)`, log.ID, log.JobID, log.Message, log.CreatedAt)
	return log, err
}

func (s *Store) ListAuditEvents(ctx context.Context) ([]model.AuditEvent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, actor_type, actor_id, action, resource_type, resource_id, metadata_json, ip_address, created_at
		FROM audit_events ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]model.AuditEvent, 0)
	for rows.Next() {
		var event model.AuditEvent
		if err := rows.Scan(&event.ID, &event.ActorType, &event.ActorID, &event.Action, &event.ResourceType, &event.ResourceID, &event.MetadataJSON, &event.IPAddress, &event.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Store) RecordAudit(ctx context.Context, event model.AuditEvent) error {
	if event.CreatedAt.IsZero() {
		event.CreatedAt = nowUTC()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO audit_events (id, actor_type, actor_id, action, resource_type, resource_id, metadata_json, ip_address, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID, event.ActorType, event.ActorID, event.Action, event.ResourceType, event.ResourceID, event.MetadataJSON, event.IPAddress, event.CreatedAt)
	if err != nil {
		return err
	}
	if event.Action == "agent.heartbeat" && event.ResourceType == "host" && event.ResourceID != "" {
		return s.pruneHeartbeatAudit(ctx, event.ResourceID, 5)
	}
	return nil
}

func (s *Store) pruneHeartbeatAudit(ctx context.Context, hostID string, keep int) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM audit_events
		WHERE id IN (
			SELECT id
			FROM audit_events
			WHERE action = 'agent.heartbeat'
			  AND resource_type = 'host'
			  AND resource_id = ?
			ORDER BY created_at DESC, id DESC
			LIMIT -1 OFFSET ?
		)`, hostID, keep)
	return err
}

func (s *Store) queryJobs(ctx context.Context, query string, args ...any) ([]model.DeploymentJob, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	jobs := make([]model.DeploymentJob, 0)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanApp(row scanner) (model.App, error) {
	var app model.App
	err := row.Scan(&app.ID, &app.Name, &app.RepoID, &app.HostID, &app.EnvironmentID, &app.RootPath, &app.RecipePath, &app.SuccessfulReleasesKeep, &app.FailedReleasesTTLHours, &app.CreatedAt, &app.UpdatedAt)
	return app, err
}

func scanJob(row scanner) (model.DeploymentJob, error) {
	var job model.DeploymentJob
	err := row.Scan(&job.ID, &job.AppID, &job.HostID, &job.RepoID, &job.Action, &job.Status, &job.Ref, &job.CommitSHA, &job.ArtifactURL, &job.ArtifactChecksum, &job.ReleaseID, &job.MetadataJSON, &job.RequestedBy, &job.ApprovedBy, &job.StartedAt, &job.FinishedAt, &job.CreatedAt, &job.UpdatedAt)
	return job, err
}

func scanReleases(rows *sql.Rows) ([]model.Release, error) {
	releases := make([]model.Release, 0)
	for rows.Next() {
		release, err := scanRelease(rows)
		if err != nil {
			return nil, err
		}
		releases = append(releases, release)
	}
	return releases, rows.Err()
}

func scanRelease(row scanner) (model.Release, error) {
	var release model.Release
	err := row.Scan(&release.ID, &release.AppID, &release.DeploymentJobID, &release.ReleaseKey, &release.CommitSHA, &release.ArtifactChecksum, &release.Path, &release.Status, &release.AvailableForRollback, &release.RetainedUntil, &release.ActivatedAt, &release.CreatedAt)
	return release, err
}

func nowUTC() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}

func IsNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}
