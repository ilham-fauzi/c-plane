package sqlitestore

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ilham/c-plane/internal/model"
)

func TestListHostsMarksStaleOnlineHostsOffline(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "cplane.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	staleSeenAt := nowUTC().Add(-2 * hostHeartbeatStaleAfter)
	freshSeenAt := nowUTC()
	_, err = store.CreateHost(context.Background(), model.Host{
		ID:             "srv_stale",
		Name:           "stale-host",
		Status:         "online",
		LastSeenAt:     &staleSeenAt,
		MQTTUsername:   "srv_stale",
		AgentTokenHash: "token",
	})
	if err != nil {
		t.Fatalf("create stale host: %v", err)
	}
	_, err = store.CreateHost(context.Background(), model.Host{
		ID:             "srv_fresh",
		Name:           "fresh-host",
		Status:         "online",
		LastSeenAt:     &freshSeenAt,
		MQTTUsername:   "srv_fresh",
		AgentTokenHash: "token",
	})
	if err != nil {
		t.Fatalf("create fresh host: %v", err)
	}

	hosts, err := store.ListHosts(context.Background())
	if err != nil {
		t.Fatalf("list hosts: %v", err)
	}

	statuses := map[string]string{}
	for _, host := range hosts {
		statuses[host.ID] = host.Status
	}
	if statuses["srv_stale"] != "offline" {
		t.Fatalf("expected stale host offline, got %q", statuses["srv_stale"])
	}
	if statuses["srv_fresh"] != "online" {
		t.Fatalf("expected fresh host online, got %q", statuses["srv_fresh"])
	}
}

func TestDeleteHostAllowsStaleOnlineHost(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "cplane.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	staleSeenAt := nowUTC().Add(-2 * hostHeartbeatStaleAfter)
	_, err = store.CreateHost(context.Background(), model.Host{
		ID:             "srv_stale",
		Name:           "stale-host",
		Status:         "online",
		LastSeenAt:     &staleSeenAt,
		MQTTUsername:   "srv_stale",
		AgentTokenHash: "token",
	})
	if err != nil {
		t.Fatalf("create host: %v", err)
	}

	if err := store.DeleteHost(context.Background(), "srv_stale"); err != nil {
		t.Fatalf("delete stale host: %v", err)
	}
}
