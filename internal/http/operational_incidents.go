package http

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// OperationalIncidentsHandler provides a form-friendly CRUD API for the
// normalized Cloudmini incident registry. The API never accepts or emits a
// free-form AGENTS.md block; each incident is validated as typed data.
type OperationalIncidentsHandler struct {
	store       store.OperationalIncidentStore
	tenantStore store.TenantStore
}

func NewOperationalIncidentsHandler(s store.OperationalIncidentStore, tenants store.TenantStore) *OperationalIncidentsHandler {
	return &OperationalIncidentsHandler{store: s, tenantStore: tenants}
}

func (h *OperationalIncidentsHandler) RegisterRoutes(mux *http.ServeMux) {
	admin := func(next http.HandlerFunc) http.HandlerFunc {
		return requireAuth(permissions.RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
			if requireTenantAdmin(w, r, h.tenantStore) {
				next(w, r)
			}
		})
	}
	mux.HandleFunc("GET /v1/cloudmini/operational-incidents", admin(h.handleList))
	mux.HandleFunc("POST /v1/cloudmini/operational-incidents", admin(h.handleCreate))
	mux.HandleFunc("PUT /v1/cloudmini/operational-incidents/{id}", admin(h.handleUpdate))
	mux.HandleFunc("DELETE /v1/cloudmini/operational-incidents/{id}", admin(h.handleDelete))
}

func (h *OperationalIncidentsHandler) handleList(w http.ResponseWriter, r *http.Request) {
	incidents, err := h.store.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": incidents})
}

func (h *OperationalIncidentsHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	var incident store.OperationalIncident
	if err := decodeIncident(w, r, &incident); err != nil {
		return
	}
	incident.ID = ""
	if err := incident.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	created, err := h.store.Create(r.Context(), incident)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (h *OperationalIncidentsHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	var incident store.OperationalIncident
	if err := decodeIncident(w, r, &incident); err != nil {
		return
	}
	incident.ID = r.PathValue("id")
	if err := incident.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	updated, err := h.store.Update(r.Context(), incident.ID, incident)
	if errors.Is(err, store.ErrOperationalIncidentNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "incident not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *OperationalIncidentsHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id must be a UUID"})
		return
	}
	err := h.store.Delete(r.Context(), id)
	if errors.Is(err, store.ErrOperationalIncidentNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "incident not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeIncident(w http.ResponseWriter, r *http.Request, out *store.OperationalIncident) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid incident JSON"})
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body must contain exactly one incident"})
		return errors.New("multiple JSON values in incident body")
	}
	return nil
}
