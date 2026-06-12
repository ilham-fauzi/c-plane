package store

import (
	"context"

	"github.com/ilham/c-plane/internal/model"
)

type Store interface {
	Close() error

	CreateHost(ctx context.Context, host model.Host) (model.Host, error)
	ListHosts(ctx context.Context) ([]model.Host, error)
	RegisterHostAgent(ctx context.Context, hostID, installTokenHash, agentTokenHash, agentVersion string) error
	UpdateHostHeartbeat(ctx context.Context, hostID, agentVersion string) error

	CreateRepository(ctx context.Context, repo model.Repository) (model.Repository, error)
	ListRepositories(ctx context.Context) ([]model.Repository, error)

	CreateApp(ctx context.Context, app model.App) (model.App, error)
	ListApps(ctx context.Context) ([]model.App, error)
	GetApp(ctx context.Context, id string) (model.App, error)

	CreateDeploymentJob(ctx context.Context, job model.DeploymentJob) (model.DeploymentJob, error)
	ListDeploymentJobs(ctx context.Context) ([]model.DeploymentJob, error)
	GetDeploymentJob(ctx context.Context, id string) (model.DeploymentJob, error)
	UpdateDeploymentJobStatus(ctx context.Context, id, status string) error
	ApproveDeploymentJob(ctx context.Context, id, actor string) error
	CancelDeploymentJob(ctx context.Context, id string) error
	ListPendingJobs(ctx context.Context, hostID string) ([]model.DeploymentJob, error)

	CreateRelease(ctx context.Context, release model.Release) (model.Release, error)
	ListReleasesByApp(ctx context.Context, appID string) ([]model.Release, error)
	GetRelease(ctx context.Context, id string) (model.Release, error)
	MarkReleaseRollbackUnavailable(ctx context.Context, id string) error

	AddJobLog(ctx context.Context, log model.JobLog) (model.JobLog, error)
	ListAuditEvents(ctx context.Context) ([]model.AuditEvent, error)
	RecordAudit(ctx context.Context, event model.AuditEvent) error
}
