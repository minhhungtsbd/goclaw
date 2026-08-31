package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// OperationalIncident is a structured, tenant-scoped service notice injected
// into an agent's runtime context. It deliberately separates incident
// severity from customer-facing claims so a model cannot promote a temporary
// incident into a permanent outage.
type OperationalIncident struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Service            string   `json:"service"`
	Region             string   `json:"region,omitempty"`
	CIDRs              []string `json:"cidrs"`
	Severity           string   `json:"severity"` // temporary_issue, degraded, permanent_outage
	StartsAt           string   `json:"starts_at,omitempty"`
	EndsAt             string   `json:"ends_at,omitempty"`
	Enabled            bool     `json:"enabled"`
	RequiresLiveCheck  bool     `json:"requires_live_check"`
	AllowsAdminHandoff bool     `json:"allows_admin_handoff"`
	CustomerMessage    string   `json:"customer_message,omitempty"`
	AllowedClaims      []string `json:"allowed_claims,omitempty"`
	ForbiddenClaims    []string `json:"forbidden_claims,omitempty"`
	AgentKeys          []string `json:"agent_keys,omitempty"` // empty = all tenant agents
	CreatedAt          string   `json:"created_at,omitempty"`
	UpdatedAt          string   `json:"updated_at,omitempty"`
}

const OperationalIncidentsConfigKey = "cloudmini.operational_incidents"

var ErrOperationalIncidentNotFound = errors.New("operational incident not found")

// Validate checks the normalized incident fields before persistence.
func (i *OperationalIncident) Validate() error {
	if i == nil {
		return fmt.Errorf("incident is required")
	}
	i.ID = strings.TrimSpace(i.ID)
	i.Name = strings.TrimSpace(i.Name)
	i.Service = strings.TrimSpace(i.Service)
	i.Region = strings.TrimSpace(i.Region)
	i.Severity = strings.TrimSpace(i.Severity)
	i.StartsAt = strings.TrimSpace(i.StartsAt)
	i.EndsAt = strings.TrimSpace(i.EndsAt)
	i.CustomerMessage = strings.TrimSpace(i.CustomerMessage)
	if i.ID == "" {
		i.ID = uuid.NewString()
	} else if _, err := uuid.Parse(i.ID); err != nil {
		return fmt.Errorf("id must be a UUID")
	}
	if i.Name == "" || len(i.Name) > 160 {
		return fmt.Errorf("name is required and must be at most 160 characters")
	}
	if i.Service == "" || len(i.Service) > 120 {
		return fmt.Errorf("service is required and must be at most 120 characters")
	}
	switch i.Severity {
	case "temporary_issue", "degraded", "permanent_outage":
	default:
		return fmt.Errorf("severity must be temporary_issue, degraded, or permanent_outage")
	}
	if len(i.CIDRs) == 0 || len(i.CIDRs) > 100 {
		return fmt.Errorf("at least one CIDR is required")
	}
	for n, raw := range i.CIDRs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil {
			return fmt.Errorf("cidrs[%d] is invalid: %w", n, err)
		}
		if !prefix.Addr().Is4() {
			return fmt.Errorf("cidrs[%d] must be an IPv4 network", n)
		}
		i.CIDRs[n] = prefix.Masked().String()
	}
	i.CIDRs = uniqueNonEmpty(i.CIDRs)
	if i.StartsAt != "" {
		if _, err := time.Parse(time.RFC3339, i.StartsAt); err != nil {
			return fmt.Errorf("starts_at must be RFC3339")
		}
	}
	if i.EndsAt != "" {
		if _, err := time.Parse(time.RFC3339, i.EndsAt); err != nil {
			return fmt.Errorf("ends_at must be RFC3339")
		}
	}
	if i.StartsAt != "" && i.EndsAt != "" {
		start, _ := time.Parse(time.RFC3339, i.StartsAt)
		end, _ := time.Parse(time.RFC3339, i.EndsAt)
		if !end.After(start) {
			return fmt.Errorf("ends_at must be after starts_at")
		}
	}
	for _, value := range append(append([]string{}, i.AllowedClaims...), i.ForbiddenClaims...) {
		if strings.TrimSpace(value) == "" || len(value) > 300 {
			return fmt.Errorf("claims must be non-empty and at most 300 characters")
		}
	}
	i.AllowedClaims = uniqueNonEmpty(i.AllowedClaims)
	i.ForbiddenClaims = uniqueNonEmpty(i.ForbiddenClaims)
	for _, allowed := range i.AllowedClaims {
		for _, forbidden := range i.ForbiddenClaims {
			if strings.EqualFold(allowed, forbidden) {
				return fmt.Errorf("the same claim cannot be both allowed and forbidden")
			}
		}
	}
	for _, forbidden := range i.ForbiddenClaims {
		if i.CustomerMessage != "" && strings.Contains(strings.ToLower(i.CustomerMessage), strings.ToLower(forbidden)) {
			return fmt.Errorf("customer_message contains a forbidden claim")
		}
	}
	i.AgentKeys = uniqueNonEmpty(i.AgentKeys)
	if len(i.CustomerMessage) > 1000 {
		return fmt.Errorf("customer_message must be at most 1000 characters")
	}
	return nil
}

func uniqueNonEmpty(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || slices.Contains(result, value) {
			continue
		}
		result = append(result, value)
	}
	return result
}

// OperationalIncidentStore persists the tenant's normalized incident list.
// Implementations use SystemConfigStore so PostgreSQL and SQLite share the
// same tenant-isolated storage path without a second schema migration.
type OperationalIncidentStore interface {
	List(ctx context.Context) ([]OperationalIncident, error)
	Save(ctx context.Context, incidents []OperationalIncident) error
	Create(ctx context.Context, incident OperationalIncident) (OperationalIncident, error)
	Update(ctx context.Context, id string, incident OperationalIncident) (OperationalIncident, error)
	Delete(ctx context.Context, id string) error
}

type SystemConfigOperationalIncidentStore struct {
	configs SystemConfigStore
	mu      sync.Mutex
}

func NewOperationalIncidentStore(configs SystemConfigStore) OperationalIncidentStore {
	if configs == nil {
		return nil
	}
	return &SystemConfigOperationalIncidentStore{configs: configs}
}

func (s *SystemConfigOperationalIncidentStore) List(ctx context.Context) ([]OperationalIncident, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listUnlocked(ctx)
}

func (s *SystemConfigOperationalIncidentStore) listUnlocked(ctx context.Context) ([]OperationalIncident, error) {
	configs, err := s.configs.List(ctx)
	if err != nil {
		return nil, err
	}
	raw := configs[OperationalIncidentsConfigKey]
	if strings.TrimSpace(raw) == "" {
		return []OperationalIncident{}, nil
	}
	var incidents []OperationalIncident
	if err := json.Unmarshal([]byte(raw), &incidents); err != nil {
		return nil, fmt.Errorf("decode operational incidents: %w", err)
	}
	seen := make(map[string]struct{}, len(incidents))
	for n := range incidents {
		if strings.TrimSpace(incidents[n].ID) == "" {
			return nil, fmt.Errorf("decode operational incidents: incident %d has no id", n)
		}
		if err := incidents[n].Validate(); err != nil {
			return nil, fmt.Errorf("decode operational incidents: incident %d: %w", n, err)
		}
		key := strings.ToLower(incidents[n].ID)
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("decode operational incidents: duplicate id %s", incidents[n].ID)
		}
		seen[key] = struct{}{}
	}
	return incidents, nil
}

func (s *SystemConfigOperationalIncidentStore) Save(ctx context.Context, incidents []OperationalIncident) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveUnlocked(ctx, incidents)
}

func (s *SystemConfigOperationalIncidentStore) saveUnlocked(ctx context.Context, incidents []OperationalIncident) error {
	seen := make(map[string]struct{}, len(incidents))
	for n := range incidents {
		if err := incidents[n].Validate(); err != nil {
			return fmt.Errorf("incident %d: %w", n, err)
		}
		key := strings.ToLower(incidents[n].ID)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("incident %d: duplicate id %s", n, incidents[n].ID)
		}
		seen[key] = struct{}{}
	}
	raw, err := json.Marshal(incidents)
	if err != nil {
		return fmt.Errorf("encode operational incidents: %w", err)
	}
	if len(raw) > 1<<20 {
		return fmt.Errorf("operational incidents exceed 1 MiB")
	}
	return s.configs.Set(ctx, OperationalIncidentsConfigKey, string(raw))
}

func (s *SystemConfigOperationalIncidentStore) Create(ctx context.Context, incident OperationalIncident) (OperationalIncident, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	incident.ID = ""
	now := time.Now().UTC().Format(time.RFC3339Nano)
	incident.CreatedAt = now
	incident.UpdatedAt = now
	if err := incident.Validate(); err != nil {
		return OperationalIncident{}, err
	}
	incidents, err := s.listUnlocked(ctx)
	if err != nil {
		return OperationalIncident{}, err
	}
	incidents = append(incidents, incident)
	if err := s.saveUnlocked(ctx, incidents); err != nil {
		return OperationalIncident{}, err
	}
	return incident, nil
}

func (s *SystemConfigOperationalIncidentStore) Update(ctx context.Context, id string, incident OperationalIncident) (OperationalIncident, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id = strings.TrimSpace(id)
	if _, err := uuid.Parse(id); err != nil {
		return OperationalIncident{}, fmt.Errorf("id must be a UUID")
	}
	incidents, err := s.listUnlocked(ctx)
	if err != nil {
		return OperationalIncident{}, err
	}
	for n := range incidents {
		if !strings.EqualFold(incidents[n].ID, id) {
			continue
		}
		incident.ID = incidents[n].ID
		incident.CreatedAt = incidents[n].CreatedAt
		incident.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if err := incident.Validate(); err != nil {
			return OperationalIncident{}, err
		}
		incidents[n] = incident
		if err := s.saveUnlocked(ctx, incidents); err != nil {
			return OperationalIncident{}, err
		}
		return incident, nil
	}
	return OperationalIncident{}, ErrOperationalIncidentNotFound
}

func (s *SystemConfigOperationalIncidentStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id = strings.TrimSpace(id)
	if _, err := uuid.Parse(id); err != nil {
		return fmt.Errorf("id must be a UUID")
	}
	incidents, err := s.listUnlocked(ctx)
	if err != nil {
		return err
	}
	for n := range incidents {
		if strings.EqualFold(incidents[n].ID, id) {
			incidents = append(incidents[:n], incidents[n+1:]...)
			return s.saveUnlocked(ctx, incidents)
		}
	}
	return ErrOperationalIncidentNotFound
}
