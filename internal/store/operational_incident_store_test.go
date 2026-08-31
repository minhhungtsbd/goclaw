package store

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

type operationalIncidentConfigStore struct {
	mu     sync.Mutex
	values map[string]string
}

func (s *operationalIncidentConfigStore) Get(context.Context, string) (string, error) {
	return "", fmt.Errorf("Get must not be used for optional incident config")
}
func (s *operationalIncidentConfigStore) Set(_ context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = value
	return nil
}
func (s *operationalIncidentConfigStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, key)
	return nil
}
func (s *operationalIncidentConfigStore) List(context.Context) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make(map[string]string, len(s.values))
	for key, value := range s.values {
		result[key] = value
	}
	return result, nil
}

func newOperationalIncidentTestStore() OperationalIncidentStore {
	return NewOperationalIncidentStore(&operationalIncidentConfigStore{values: make(map[string]string)})
}

func validOperationalIncident(name string) OperationalIncident {
	return OperationalIncident{
		Name: name, Service: "PrivateV4", CIDRs: []string{"37.221.109.0/24"},
		Severity: "temporary_issue", Enabled: true, RequiresLiveCheck: true,
	}
}

func TestOperationalIncidentStoreCRUD(t *testing.T) {
	ctx := context.Background()
	incidents := newOperationalIncidentTestStore()
	created, err := incidents.Create(ctx, validOperationalIncident("Michigan"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == "" || created.CreatedAt == "" || created.UpdatedAt == "" {
		t.Fatalf("created metadata missing: %#v", created)
	}
	update := validOperationalIncident("Michigan degraded")
	update.Severity = "degraded"
	updated, err := incidents.Update(ctx, created.ID, update)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.CreatedAt != created.CreatedAt || updated.Name != update.Name {
		t.Fatalf("updated incident = %#v", updated)
	}
	if err := incidents.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	items, err := incidents.List(ctx)
	if err != nil || len(items) != 0 {
		t.Fatalf("List after delete = %#v, %v", items, err)
	}
}

func TestOperationalIncidentStoreSerializesConcurrentCreates(t *testing.T) {
	ctx := context.Background()
	incidents := newOperationalIncidentTestStore()
	var wg sync.WaitGroup
	for n := 0; n < 20; n++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			if _, err := incidents.Create(ctx, validOperationalIncident(fmt.Sprintf("incident-%d", index))); err != nil {
				t.Errorf("Create(%d): %v", index, err)
			}
		}(n)
	}
	wg.Wait()
	items, err := incidents.List(ctx)
	if err != nil || len(items) != 20 {
		t.Fatalf("concurrent list count = %d, err = %v", len(items), err)
	}
}

func TestOperationalIncidentValidateRejectsIPv6(t *testing.T) {
	incident := validOperationalIncident("IPv6")
	incident.CIDRs = []string{"2001:db8::/32"}
	if err := incident.Validate(); err == nil {
		t.Fatal("IPv6 incident must be rejected because runtime request matching is IPv4-only")
	}
}

func TestOperationalIncidentStoreRejectsMalformedPersistedData(t *testing.T) {
	configs := &operationalIncidentConfigStore{values: map[string]string{
		OperationalIncidentsConfigKey: `[{"name":"missing id","service":"PrivateV4","cidrs":["37.221.109.0/24"],"severity":"temporary_issue","enabled":true}]`,
	}}
	incidents := NewOperationalIncidentStore(configs)
	if _, err := incidents.List(context.Background()); err == nil {
		t.Fatal("malformed persisted incident was accepted")
	}
}

func TestOperationalIncidentValidateRejectsContradictoryClaims(t *testing.T) {
	incident := validOperationalIncident("contradictory")
	incident.CustomerMessage = "Hệ thống sẽ hoàn tiền ngay."
	incident.AllowedClaims = []string{"Hoàn tiền ngay"}
	incident.ForbiddenClaims = []string{"hoàn tiền ngay"}
	if err := incident.Validate(); err == nil {
		t.Fatal("contradictory allowed/forbidden claims were accepted")
	}
}
