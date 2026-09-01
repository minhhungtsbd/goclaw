package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type takeoverHTTPStore struct {
	items      []store.ChannelAdminTakeover
	releasedID uuid.UUID
	releasedBy string
	reason     string
	releaseErr error
}

func (s *takeoverHTTPStore) Activate(context.Context, store.ChannelAdminTakeover) (*store.ChannelAdminTakeover, error) {
	return nil, errors.New("not implemented")
}
func (s *takeoverHTTPStore) GetActive(context.Context, string, string, time.Time) (*store.ChannelAdminTakeover, error) {
	return nil, errors.New("not implemented")
}
func (s *takeoverHTTPStore) ListActive(_ context.Context, opts store.ChannelAdminTakeoverListOptions) ([]store.ChannelAdminTakeover, int, error) {
	return s.items, len(s.items), nil
}
func (s *takeoverHTTPStore) Release(_ context.Context, id uuid.UUID, releasedBy, reason string, now time.Time) (*store.ChannelAdminTakeover, error) {
	s.releasedID, s.releasedBy, s.reason = id, releasedBy, reason
	if s.releaseErr != nil {
		return nil, s.releaseErr
	}
	return &store.ChannelAdminTakeover{ID: id, ReleasedAt: &now, ReleasedBy: releasedBy, ReleaseReason: reason}, nil
}

func TestChannelAdminTakeoversHandleListUsesRealResponseShape(t *testing.T) {
	id := uuid.Must(uuid.NewV7())
	h := NewChannelAdminTakeoversHandler(&takeoverHTTPStore{items: []store.ChannelAdminTakeover{{
		ID: id, ChannelName: "cloudmini-net-page", ChatID: "customer-1",
	}}}, nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/channel-admin-takeovers?channel_name=cloudmini-net-page", nil)
	rec := httptest.NewRecorder()
	h.handleList(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []store.ChannelAdminTakeover `json:"items"`
		Total int                          `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Total != 1 || len(body.Items) != 1 || body.Items[0].ID != id {
		t.Fatalf("response = %#v", body)
	}
}

func TestChannelAdminTakeoversHandleReleaseRecordsActor(t *testing.T) {
	id := uuid.Must(uuid.NewV7())
	s := &takeoverHTTPStore{}
	h := NewChannelAdminTakeoversHandler(s, nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/channel-admin-takeovers/"+id.String()+"/release", strings.NewReader(`{"reason":"Admin xử lý xong"}`))
	req.SetPathValue("id", id.String())
	req = req.WithContext(store.WithUserID(req.Context(), "admin-user"))
	rec := httptest.NewRecorder()
	h.handleRelease(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if s.releasedID != id || s.releasedBy != "admin-user" || s.reason != "Admin xử lý xong" {
		t.Fatalf("release args = %s, %q, %q", s.releasedID, s.releasedBy, s.reason)
	}
}

func TestChannelAdminTakeoversHandleReleaseNotFound(t *testing.T) {
	id := uuid.Must(uuid.NewV7())
	h := NewChannelAdminTakeoversHandler(&takeoverHTTPStore{releaseErr: store.ErrChannelAdminTakeoverNotFound}, nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/channel-admin-takeovers/"+id.String()+"/release", nil)
	req.SetPathValue("id", id.String())
	rec := httptest.NewRecorder()
	h.handleRelease(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

var _ store.ChannelAdminTakeoverStore = (*takeoverHTTPStore)(nil)
