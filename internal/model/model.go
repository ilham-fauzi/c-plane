package model

import "time"

type Host struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Status         string     `json:"status"`
	LastSeenAt     *time.Time `json:"last_seen_at,omitempty"`
	AgentVersion   string     `json:"agent_version,omitempty"`
	MQTTUsername   string     `json:"mqtt_username,omitempty"`
	AgentTokenHash string     `json:"-"`
	InstallToken   string     `json:"install_token,omitempty"`
	InstallCommand string     `json:"install_command,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type Repository struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	Provider          string    `json:"provider"`
	URL               string    `json:"url"`
	DefaultBranch     string    `json:"default_branch"`
	WebhookSecretHash string    `json:"-"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type App struct {
	ID                     string    `json:"id"`
	Name                   string    `json:"name"`
	RepoID                 string    `json:"repo_id"`
	HostID                 string    `json:"host_id"`
	EnvironmentID          string    `json:"environment_id"`
	RootPath               string    `json:"root_path"`
	RecipePath             string    `json:"recipe_path"`
	SuccessfulReleasesKeep int       `json:"successful_releases_keep"`
	FailedReleasesTTLHours int       `json:"failed_releases_ttl_hours"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type DeploymentJob struct {
	ID               string     `json:"id"`
	AppID            string     `json:"app_id"`
	HostID           string     `json:"host_id"`
	RepoID           string     `json:"repo_id"`
	Action           string     `json:"action"`
	Status           string     `json:"status"`
	Ref              string     `json:"ref,omitempty"`
	CommitSHA        string     `json:"commit_sha,omitempty"`
	ArtifactURL      string     `json:"artifact_url,omitempty"`
	ArtifactChecksum string     `json:"artifact_checksum,omitempty"`
	ReleaseID        string     `json:"release_id,omitempty"`
	MetadataJSON     string     `json:"metadata_json,omitempty"`
	RequestedBy      string     `json:"requested_by,omitempty"`
	ApprovedBy       string     `json:"approved_by,omitempty"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	FinishedAt       *time.Time `json:"finished_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type Release struct {
	ID                   string     `json:"id"`
	AppID                string     `json:"app_id"`
	DeploymentJobID      string     `json:"deployment_job_id"`
	ReleaseKey           string     `json:"release_key"`
	CommitSHA            string     `json:"commit_sha,omitempty"`
	ArtifactChecksum     string     `json:"artifact_checksum,omitempty"`
	Path                 string     `json:"path"`
	Status               string     `json:"status"`
	AvailableForRollback bool       `json:"available_for_rollback"`
	RetainedUntil        *time.Time `json:"retained_until,omitempty"`
	ActivatedAt          *time.Time `json:"activated_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
}

type AuditEvent struct {
	ID           string    `json:"id"`
	ActorType    string    `json:"actor_type"`
	ActorID      string    `json:"actor_id"`
	Action       string    `json:"action"`
	ResourceType string    `json:"resource_type"`
	ResourceID   string    `json:"resource_id"`
	MetadataJSON string    `json:"metadata_json,omitempty"`
	IPAddress    string    `json:"ip_address,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type JobLog struct {
	ID        string    `json:"id"`
	JobID     string    `json:"job_id"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}
