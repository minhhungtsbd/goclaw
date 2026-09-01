package http

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type ChannelAdminTakeoversHandler struct {
	store   store.ChannelAdminTakeoverStore
	tenants store.TenantStore
}

func NewChannelAdminTakeoversHandler(s store.ChannelAdminTakeoverStore, tenants store.TenantStore) *ChannelAdminTakeoversHandler {
	return &ChannelAdminTakeoversHandler{store: s, tenants: tenants}
}

func (h *ChannelAdminTakeoversHandler) RegisterRoutes(mux *http.ServeMux) {
	admin := func(next http.HandlerFunc) http.HandlerFunc {
		return requireAuth(permissions.RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
			if requireTenantAdmin(w, r, h.tenants) {
				next(w, r)
			}
		})
	}
	mux.HandleFunc("GET /v1/channel-admin-takeovers", admin(h.handleList))
	mux.HandleFunc("POST /v1/channel-admin-takeovers/{id}/release", admin(h.handleRelease))
}

func (h *ChannelAdminTakeoversHandler) handleList(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	limit := takeoverQueryInt(r, "limit", 50)
	offset := takeoverQueryInt(r, "offset", 0)
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	items, total, err := h.store.ListActive(r.Context(), store.ChannelAdminTakeoverListOptions{
		ChannelName: strings.TrimSpace(r.URL.Query().Get("channel_name")),
		Limit:       limit,
		Offset:      offset,
		Now:         time.Now().UTC(),
	})
	if err != nil {
		slog.Error("channel admin takeover list failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgTakeoverListFailed)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": total, "limit": limit, "offset": offset,
	})
}

func (h *ChannelAdminTakeoversHandler) handleRelease(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgTakeoverInvalidID)})
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgTakeoverInvalidBody)})
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgTakeoverMultipleJSON)})
		return
	}
	body.Reason = strings.TrimSpace(body.Reason)
	if utf8.RuneCountInString(body.Reason) > 1000 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgTakeoverReasonTooLong)})
		return
	}
	releasedBy := strings.TrimSpace(store.UserIDFromContext(r.Context()))
	if releasedBy == "" {
		releasedBy = "admin"
	}
	item, err := h.store.Release(r.Context(), id, releasedBy, body.Reason, time.Now().UTC())
	if errors.Is(err, store.ErrChannelAdminTakeoverNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgTakeoverNotFound)})
		return
	}
	if err != nil {
		slog.Error("channel admin takeover release failed", "takeover_id", id, "error", err)
		writeJSON(w, http.StatusConflict, map[string]string{"error": i18n.T(locale, i18n.MsgTakeoverReleaseFailed)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"takeover": item})
}

func takeoverQueryInt(r *http.Request, key string, fallback int) int {
	value, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil {
		return fallback
	}
	return value
}
