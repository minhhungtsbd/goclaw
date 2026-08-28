package http

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/nextlevelbuilder/goclaw/internal/permissions"
)

const agyHostTicketTTL = 2 * time.Minute

var agyHostProfileRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// AGYHostHandler proxies the host-native AGY bridge. The bridge itself accepts
// only a private bearer token, while this handler applies GoClaw RBAC and gives
// browsers a short-lived one-time ticket for terminal WebSockets.
type AGYHostHandler struct {
	bridgeURL string
	bridgeToken string
	mu sync.Mutex
	tickets map[string]agyHostTicket
	client *http.Client
	upgrader websocket.Upgrader
}

type agyHostTicket struct { profile string; expiresAt time.Time }

func NewAGYHostHandlerFromEnv() (*AGYHostHandler, error) {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("GOCLAW_AGY_HOST_BRIDGE_URL")), "/")
	token := strings.TrimSpace(os.Getenv("GOCLAW_AGY_HOST_TOKEN"))
	if base == "" && token == "" { return nil, nil }
	if base == "" || token == "" { return nil, errors.New("GOCLAW_AGY_HOST_BRIDGE_URL and GOCLAW_AGY_HOST_TOKEN must both be set") }
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" { return nil, errors.New("invalid GOCLAW_AGY_HOST_BRIDGE_URL") }
	return &AGYHostHandler{bridgeURL: base, bridgeToken: token, tickets: make(map[string]agyHostTicket), client: &http.Client{Timeout: 15*time.Second}, upgrader: websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}}, nil
}

func (h *AGYHostHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/agy-host/{profile}/status", requireAuth(permissions.RoleAdmin, h.handleStatus))
	mux.HandleFunc("GET /v1/agy-host/{profile}/models", requireAuth(permissions.RoleAdmin, h.handleModels))
	mux.HandleFunc("POST /v1/agy-host/{profile}/terminal-ticket", requireAuth(permissions.RoleAdmin, h.handleTicket))
	// Browser WebSockets cannot attach Authorization headers. The endpoint accepts
	// only an unguessable, one-time ticket minted by the authenticated route above.
	mux.HandleFunc("GET /v1/agy-host/{profile}/terminal", h.handleTerminal)
}

func (h *AGYHostHandler) profile(r *http.Request) (string, bool) { p := r.PathValue("profile"); return p, agyHostProfileRE.MatchString(p) }

func (h *AGYHostHandler) bridgeRequest(r *http.Request, suffix string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, h.bridgeURL+suffix, nil)
	if err == nil { req.Header.Set("Authorization", "Bearer "+h.bridgeToken) }
	return req, err
}

func (h *AGYHostHandler) proxyJSON(w http.ResponseWriter, r *http.Request, endpoint string) {
	req, err := h.bridgeRequest(r, endpoint)
	if err != nil { writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()}); return }
	resp, err := h.client.Do(req)
	if err != nil { writeJSON(w, http.StatusBadGateway, map[string]string{"error": "AGY host bridge unavailable"}); return }
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (h *AGYHostHandler) handleStatus(w http.ResponseWriter, r *http.Request) { if p, ok := h.profile(r); ok { h.proxyJSON(w, r, "/v1/profiles/"+p+"/status") } else { http.Error(w, "invalid profile", http.StatusBadRequest) } }
func (h *AGYHostHandler) handleModels(w http.ResponseWriter, r *http.Request) { if p, ok := h.profile(r); ok { h.proxyJSON(w, r, "/v1/profiles/"+p+"/models") } else { http.Error(w, "invalid profile", http.StatusBadRequest) } }

func (h *AGYHostHandler) handleTicket(w http.ResponseWriter, r *http.Request) {
	p, ok := h.profile(r); if !ok { http.Error(w, "invalid profile", http.StatusBadRequest); return }
	raw := make([]byte, 32); if _, err := rand.Read(raw); err != nil { http.Error(w, "ticket generation failed", http.StatusInternalServerError); return }
	ticket := hex.EncodeToString(raw)
	h.mu.Lock(); h.purgeExpiredLocked(time.Now()); h.tickets[ticket] = agyHostTicket{profile: p, expiresAt: time.Now().Add(agyHostTicketTTL)}; h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"ticket": ticket, "path": "/v1/agy-host/"+p+"/terminal?ticket="+ticket})
}

func (h *AGYHostHandler) consumeTicket(profile, ticket string) bool {
	h.mu.Lock(); defer h.mu.Unlock(); h.purgeExpiredLocked(time.Now()); value, ok := h.tickets[ticket]; if !ok || value.profile != profile { return false }; delete(h.tickets, ticket); return true
}
func (h *AGYHostHandler) purgeExpiredLocked(now time.Time) { for key, value := range h.tickets { if !value.expiresAt.After(now) { delete(h.tickets, key) } } }

func (h *AGYHostHandler) handleTerminal(w http.ResponseWriter, r *http.Request) {
	p, ok := h.profile(r); if !ok || !h.consumeTicket(p, r.URL.Query().Get("ticket")) { http.Error(w, "invalid or expired terminal ticket", http.StatusUnauthorized); return }
	client, err := h.upgrader.Upgrade(w, r, nil); if err != nil { return }; defer client.Close()
	bridge, err := url.Parse(h.bridgeURL); if err != nil { return }
	if bridge.Scheme == "https" { bridge.Scheme = "wss" } else { bridge.Scheme = "ws" }
	bridge.Path = strings.TrimRight(bridge.Path, "/") + "/v1/profiles/" + p + "/terminal"
	server, _, err := websocket.DefaultDialer.Dial(bridge.String(), http.Header{"Authorization": []string{"Bearer " + h.bridgeToken}})
	if err != nil { _ = client.WriteMessage(websocket.TextMessage, []byte("Unable to connect to AGY host bridge.")); return }
	defer server.Close()
	done := make(chan struct{}, 2)
	pipe := func(dst, src *websocket.Conn) { defer func(){ done <- struct{}{} }(); for { typ, data, err := src.ReadMessage(); if err != nil { return }; if err := dst.WriteMessage(typ, data); err != nil { return } } }
	go pipe(server, client); go pipe(client, server); <-done
}
