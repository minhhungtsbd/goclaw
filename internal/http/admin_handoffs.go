package http

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/adminhandoff"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type AdminHandoffsHandler struct {
	store   store.AdminHandoffManagementStore
	service *adminhandoff.Service
	tenants store.TenantStore
}

func NewAdminHandoffsHandler(s store.AdminHandoffManagementStore, service *adminhandoff.Service, tenants store.TenantStore) *AdminHandoffsHandler {
	return &AdminHandoffsHandler{store: s, service: service, tenants: tenants}
}

func (h *AdminHandoffsHandler) RegisterRoutes(mux *http.ServeMux) {
	auth := func(next http.HandlerFunc) http.HandlerFunc {
		return requireAuth(permissions.RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
			if requireTenantAdmin(w, r, h.tenants) {
				next(w, r)
			}
		})
	}
	mux.HandleFunc("GET /v1/admin-handoffs", auth(h.handleList))
	mux.HandleFunc("GET /v1/admin-handoffs/{id}", auth(h.handleGet))
	mux.HandleFunc("POST /v1/admin-handoffs/{id}/complete", auth(h.handleComplete))
	mux.HandleFunc("POST /v1/admin-handoffs/{id}/manual", auth(h.handleManual))
	mux.HandleFunc("POST /v1/admin-handoffs/{id}/dismiss", auth(h.handleDismiss))
}

func (h *AdminHandoffsHandler) handleList(w http.ResponseWriter, r *http.Request) {
	limit := adminHandoffQueryInt(r, "limit", 25)
	offset := adminHandoffQueryInt(r, "offset", 0)
	items, total, err := h.store.List(r.Context(), store.AdminHandoffListOptions{
		Search: r.URL.Query().Get("search"), Status: r.URL.Query().Get("status"),
		Priority: r.URL.Query().Get("priority"), Service: r.URL.Query().Get("service"),
		Limit: limit, Offset: offset,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
}

func (h *AdminHandoffsHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	id, ok := handoffID(w, r)
	if !ok {
		return
	}
	handoff, err := h.store.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	events, err := h.store.ListEvents(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"handoff": handoff, "events": events})
}

func (h *AdminHandoffsHandler) handleComplete(w http.ResponseWriter, r *http.Request) {
	id, ok := handoffID(w, r)
	if !ok {
		return
	}
	handoff, err := h.service.Complete(r.Context(), id, requestActor(r))
	writeActionResult(w, handoff, err)
}

func (h *AdminHandoffsHandler) handleManual(w http.ResponseWriter, r *http.Request) {
	id, ok := handoffID(w, r)
	if !ok {
		return
	}
	var body struct {
		Content    string `json:"content"`
		CloseAfter bool   `json:"close_after"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	handoff, err := h.service.Manual(r.Context(), id, body.Content, body.CloseAfter, requestActor(r))
	writeActionResult(w, handoff, err)
}

func (h *AdminHandoffsHandler) handleDismiss(w http.ResponseWriter, r *http.Request) {
	id, ok := handoffID(w, r)
	if !ok {
		return
	}
	handoff, err := h.service.Dismiss(r.Context(), id, requestActor(r))
	writeActionResult(w, handoff, err)
}

func handoffID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid handoff id"})
		return uuid.Nil, false
	}
	return id, true
}

func requestActor(r *http.Request) adminhandoff.Actor {
	return adminhandoff.Actor{Type: "web", ID: store.UserIDFromContext(r.Context())}
}

func writeActionResult(w http.ResponseWriter, handoff *store.AdminHandoff, err error) {
	if err != nil {
		status := http.StatusConflict
		if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "exceeds") {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"handoff": handoff})
}

func adminHandoffQueryInt(r *http.Request, key string, fallback int) int {
	value, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil {
		return fallback
	}
	return value
}
