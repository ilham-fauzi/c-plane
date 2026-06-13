package sqlitestore

const schema = `
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS hosts (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	status TEXT NOT NULL,
	last_seen_at TIMESTAMP,
	agent_version TEXT NOT NULL DEFAULT '',
	mqtt_username TEXT NOT NULL DEFAULT '',
	agent_token_hash TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMP NOT NULL,
	updated_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS repos (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	provider TEXT NOT NULL,
	url TEXT NOT NULL,
	default_branch TEXT NOT NULL DEFAULT 'main',
	webhook_secret_hash TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMP NOT NULL,
	updated_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS apps (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	repo_id TEXT NOT NULL,
	host_id TEXT NOT NULL,
	environment_id TEXT NOT NULL,
	root_path TEXT NOT NULL,
	recipe_path TEXT NOT NULL,
	successful_releases_keep INTEGER NOT NULL DEFAULT 5,
	failed_releases_ttl_hours INTEGER NOT NULL DEFAULT 72,
	created_at TIMESTAMP NOT NULL,
	updated_at TIMESTAMP NOT NULL,
	FOREIGN KEY (repo_id) REFERENCES repos(id),
	FOREIGN KEY (host_id) REFERENCES hosts(id)
);

CREATE TABLE IF NOT EXISTS deployment_jobs (
	id TEXT PRIMARY KEY,
	app_id TEXT NOT NULL,
	host_id TEXT NOT NULL,
	repo_id TEXT NOT NULL,
	action TEXT NOT NULL,
	status TEXT NOT NULL,
	ref TEXT NOT NULL DEFAULT '',
	commit_sha TEXT NOT NULL DEFAULT '',
	artifact_url TEXT NOT NULL DEFAULT '',
	artifact_checksum TEXT NOT NULL DEFAULT '',
	release_id TEXT NOT NULL DEFAULT '',
	metadata_json TEXT NOT NULL DEFAULT '',
	requested_by TEXT NOT NULL DEFAULT '',
	approved_by TEXT NOT NULL DEFAULT '',
	started_at TIMESTAMP,
	finished_at TIMESTAMP,
	created_at TIMESTAMP NOT NULL,
	updated_at TIMESTAMP NOT NULL,
	FOREIGN KEY (app_id) REFERENCES apps(id),
	FOREIGN KEY (host_id) REFERENCES hosts(id),
	FOREIGN KEY (repo_id) REFERENCES repos(id)
);

CREATE INDEX IF NOT EXISTS deployment_jobs_host_status_idx ON deployment_jobs(host_id, status);

CREATE TABLE IF NOT EXISTS releases (
	id TEXT PRIMARY KEY,
	app_id TEXT NOT NULL,
	deployment_job_id TEXT NOT NULL,
	release_key TEXT NOT NULL,
	commit_sha TEXT NOT NULL DEFAULT '',
	artifact_checksum TEXT NOT NULL DEFAULT '',
	path TEXT NOT NULL,
	status TEXT NOT NULL,
	available_for_rollback BOOLEAN NOT NULL DEFAULT 0,
	retained_until TIMESTAMP,
	activated_at TIMESTAMP,
	created_at TIMESTAMP NOT NULL,
	FOREIGN KEY (app_id) REFERENCES apps(id),
	FOREIGN KEY (deployment_job_id) REFERENCES deployment_jobs(id)
);

CREATE INDEX IF NOT EXISTS releases_app_created_idx ON releases(app_id, created_at DESC);

CREATE TABLE IF NOT EXISTS job_logs (
	id TEXT PRIMARY KEY,
	job_id TEXT NOT NULL,
	message TEXT NOT NULL,
	created_at TIMESTAMP NOT NULL,
	FOREIGN KEY (job_id) REFERENCES deployment_jobs(id)
);

CREATE TABLE IF NOT EXISTS audit_events (
	id TEXT PRIMARY KEY,
	actor_type TEXT NOT NULL,
	actor_id TEXT NOT NULL,
	action TEXT NOT NULL,
	resource_type TEXT NOT NULL,
	resource_id TEXT NOT NULL,
	metadata_json TEXT NOT NULL DEFAULT '',
	ip_address TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMP NOT NULL
);
`
