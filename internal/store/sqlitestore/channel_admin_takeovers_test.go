//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestSQLiteChannelAdminTakeoverLifecycleSurvivesStoreRecreation(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "takeovers.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	now := time.Now().UTC().Truncate(time.Millisecond)
	first := NewSQLiteChannelAdminTakeoverStore(db)
	created, err := first.Activate(ctx, store.ChannelAdminTakeover{
		ChannelName: "cloudmini-net-page", ChatID: "customer-1", AgentKey: "linh-nhi-support-lead",
		AdminMessageID: "admin-mid", LastAdminMessage: "Admin đang xử lý.",
		TakenOverAt: now, ExpiresAt: now.Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}

	// A new store instance models process restart and must read the same lease.
	restarted := NewSQLiteChannelAdminTakeoverStore(db)
	active, err := restarted.GetActive(ctx, "cloudmini-net-page", "customer-1", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("GetActive after recreation: %v", err)
	}
	if active.ID != created.ID || active.AdminMessageID != "admin-mid" {
		t.Fatalf("active takeover = %#v, created = %#v", active, created)
	}

	items, total, err := restarted.ListActive(ctx, store.ChannelAdminTakeoverListOptions{
		ChannelName: "cloudmini-net-page", Now: now.Add(time.Minute), Limit: 10,
	})
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("ListActive = %d/%d, err=%v", len(items), total, err)
	}

	released, err := restarted.Release(ctx, created.ID, "admin-user", "done", now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	if released.ReleasedAt == nil || released.ReleasedBy != "admin-user" {
		t.Fatalf("released takeover = %#v", released)
	}
	if _, err := restarted.GetActive(ctx, "cloudmini-net-page", "customer-1", now.Add(3*time.Minute)); !errors.Is(err, store.ErrChannelAdminTakeoverNotFound) {
		t.Fatalf("GetActive after release error = %v", err)
	}

	reactivated, err := restarted.Activate(ctx, store.ChannelAdminTakeover{
		ChannelName: "cloudmini-net-page", ChatID: "customer-1", AgentKey: "linh-nhi-support-lead",
		AdminMessageID: "admin-mid-2", LastAdminMessage: "Admin tiếp tục xử lý.",
		TakenOverAt: now.Add(4 * time.Minute), ExpiresAt: now.Add(14 * time.Minute),
	})
	if err != nil {
		t.Fatalf("reactivate: %v", err)
	}
	if reactivated.ID != created.ID || reactivated.ReleasedAt != nil || reactivated.AdminMessageID != "admin-mid-2" {
		t.Fatalf("reactivated takeover = %#v", reactivated)
	}
}

func TestSQLiteChannelAdminTakeoverExpiresAndRequiresTenant(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "takeovers.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	s := NewSQLiteChannelAdminTakeoverStore(db)
	now := time.Now().UTC().Truncate(time.Millisecond)
	if _, err := s.Activate(context.Background(), store.ChannelAdminTakeover{
		ChannelName: "fb", ChatID: "customer", AgentKey: "agent",
		TakenOverAt: now, ExpiresAt: now.Add(time.Minute),
	}); err == nil {
		t.Fatal("Activate without tenant context succeeded")
	}
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	created, err := s.Activate(ctx, store.ChannelAdminTakeover{
		ChannelName: "fb", ChatID: "customer", AgentKey: "agent",
		TakenOverAt: now, ExpiresAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if _, err := s.GetActive(ctx, "fb", "customer", now.Add(2*time.Minute)); !errors.Is(err, store.ErrChannelAdminTakeoverNotFound) {
		t.Fatalf("expired GetActive error = %v", err)
	}
	if _, err := s.Release(ctx, created.ID, "admin", "late release", now.Add(2*time.Minute)); !errors.Is(err, store.ErrChannelAdminTakeoverNotFound) {
		t.Fatalf("expired Release error = %v", err)
	}
}
