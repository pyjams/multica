package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
)

// Handler holds shared dependencies for all HTTP handlers.
type Handler struct {
	db                 *sql.DB
	hub                *Hub
	defaultUserID      string
	defaultWorkspaceID string
}

func newHandler(db *sql.DB, hub *Hub, userID, workspaceID string) *Handler {
	return &Handler{
		db:                 db,
		hub:                hub,
		defaultUserID:      userID,
		defaultWorkspaceID: workspaceID,
	}
}

// workspaceID resolves workspace_id from the X-Workspace-ID header or query param.
func (h *Handler) workspaceID(r *http.Request) string {
	if wsID := r.Header.Get("X-Workspace-ID"); wsID != "" {
		return wsID
	}
	if wsID := r.URL.Query().Get("workspace_id"); wsID != "" {
		return wsID
	}
	return h.defaultWorkspaceID
}

// --- HTTP helpers ---

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeJSON(r *http.Request, v interface{}) error {
	return json.NewDecoder(r.Body).Decode(v)
}

func nullStr(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	return &s.String
}

func nullFloat(f sql.NullFloat64) *float64 {
	if !f.Valid {
		return nil
	}
	return &f.Float64
}
