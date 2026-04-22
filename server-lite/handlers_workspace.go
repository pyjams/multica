package main

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) handleGetMe(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromCtx(r)
	var u User
	err := h.db.QueryRowContext(r.Context(),
		`SELECT id, name, email, avatar_url, created_at, updated_at FROM users WHERE id = ?`, uid,
	).Scan(&u.ID, &u.Name, &u.Email, &u.AvatarURL, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (h *Handler) handleUpdateMe(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromCtx(r)
	var req struct {
		Name      *string `json:"name"`
		AvatarURL *string `json:"avatar_url"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	ts := now()
	if req.Name != nil {
		h.db.ExecContext(r.Context(), `UPDATE users SET name = ?, updated_at = ? WHERE id = ?`, *req.Name, ts, uid)
	}
	if req.AvatarURL != nil {
		h.db.ExecContext(r.Context(), `UPDATE users SET avatar_url = ?, updated_at = ? WHERE id = ?`, *req.AvatarURL, ts, uid)
	}

	var u User
	h.db.QueryRowContext(r.Context(),
		`SELECT id, name, email, avatar_url, created_at, updated_at FROM users WHERE id = ?`, uid,
	).Scan(&u.ID, &u.Name, &u.Email, &u.AvatarURL, &u.CreatedAt, &u.UpdatedAt)
	writeJSON(w, http.StatusOK, u)
}

func (h *Handler) handleListWorkspaces(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromCtx(r)
	rows, err := h.db.QueryContext(r.Context(),
		`SELECT w.id, w.name, w.slug, w.description, w.context, w.settings, w.repos, w.issue_prefix, w.created_at, w.updated_at
         FROM workspaces w
         JOIN workspace_members m ON m.workspace_id = w.id
         WHERE m.user_id = ?`, uid,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	workspaces := []Workspace{}
	for rows.Next() {
		var ws Workspace
		var settingsJSON, reposJSON string
		var desc, ctx sql.NullString
		if err := rows.Scan(&ws.ID, &ws.Name, &ws.Slug, &desc, &ctx,
			&settingsJSON, &reposJSON, &ws.IssuePrefix, &ws.CreatedAt, &ws.UpdatedAt); err != nil {
			continue
		}
		ws.Description = nullStr(desc)
		ws.Context = nullStr(ctx)
		json.Unmarshal([]byte(settingsJSON), &ws.Settings)
		json.Unmarshal([]byte(reposJSON), &ws.Repos)
		if ws.Settings == nil {
			ws.Settings = map[string]interface{}{}
		}
		if ws.Repos == nil {
			ws.Repos = []WorkspaceRepo{}
		}
		workspaces = append(workspaces, ws)
	}
	writeJSON(w, http.StatusOK, workspaces)
}

func (h *Handler) handleGetWorkspace(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ws, err := h.getWorkspaceByID(r, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	writeJSON(w, http.StatusOK, ws)
}

func (h *Handler) getWorkspaceByID(r *http.Request, id string) (*Workspace, error) {
	var ws Workspace
	var settingsJSON, reposJSON string
	var desc, ctx sql.NullString
	err := h.db.QueryRowContext(r.Context(),
		`SELECT id, name, slug, description, context, settings, repos, issue_prefix, created_at, updated_at FROM workspaces WHERE id = ?`, id,
	).Scan(&ws.ID, &ws.Name, &ws.Slug, &desc, &ctx, &settingsJSON, &reposJSON, &ws.IssuePrefix, &ws.CreatedAt, &ws.UpdatedAt)
	if err != nil {
		return nil, err
	}
	ws.Description = nullStr(desc)
	ws.Context = nullStr(ctx)
	json.Unmarshal([]byte(settingsJSON), &ws.Settings)
	json.Unmarshal([]byte(reposJSON), &ws.Repos)
	if ws.Settings == nil {
		ws.Settings = map[string]interface{}{}
	}
	if ws.Repos == nil {
		ws.Repos = []WorkspaceRepo{}
	}
	return &ws, nil
}

func (h *Handler) handleCreateWorkspace(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromCtx(r)
	var req struct {
		Name        string `json:"name"`
		Slug        string `json:"slug"`
		Description string `json:"description"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	wsID := newID()
	memberID := newID()
	ts := now()

	if _, err := h.db.ExecContext(r.Context(),
		`INSERT INTO workspaces (id, name, slug, description, context, settings, repos, issue_prefix, issue_counter, created_at, updated_at)
         VALUES (?, ?, ?, ?, NULL, '{}', '[]', 'MUL', 0, ?, ?)`,
		wsID, req.Name, req.Slug, req.Description, ts, ts,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create workspace")
		return
	}

	h.db.ExecContext(r.Context(),
		`INSERT INTO workspace_members (id, workspace_id, user_id, role, created_at) VALUES (?, ?, ?, 'owner', ?)`,
		memberID, wsID, uid, ts,
	)

	ws, _ := h.getWorkspaceByID(r, wsID)
	writeJSON(w, http.StatusCreated, ws)
}

func (h *Handler) handleUpdateWorkspace(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Name        *string                `json:"name"`
		Description *string                `json:"description"`
		Context     *string                `json:"context"`
		Settings    map[string]interface{} `json:"settings"`
		Repos       []WorkspaceRepo        `json:"repos"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	ts := now()
	if req.Name != nil {
		h.db.ExecContext(r.Context(), `UPDATE workspaces SET name = ?, updated_at = ? WHERE id = ?`, *req.Name, ts, id)
	}
	if req.Description != nil {
		h.db.ExecContext(r.Context(), `UPDATE workspaces SET description = ?, updated_at = ? WHERE id = ?`, *req.Description, ts, id)
	}
	if req.Context != nil {
		h.db.ExecContext(r.Context(), `UPDATE workspaces SET context = ?, updated_at = ? WHERE id = ?`, *req.Context, ts, id)
	}
	if req.Settings != nil {
		b, _ := json.Marshal(req.Settings)
		h.db.ExecContext(r.Context(), `UPDATE workspaces SET settings = ?, updated_at = ? WHERE id = ?`, string(b), ts, id)
	}
	if req.Repos != nil {
		b, _ := json.Marshal(req.Repos)
		h.db.ExecContext(r.Context(), `UPDATE workspaces SET repos = ?, updated_at = ? WHERE id = ?`, string(b), ts, id)
	}

	ws, err := h.getWorkspaceByID(r, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	writeJSON(w, http.StatusOK, ws)
}

func (h *Handler) handleDeleteWorkspace(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	h.db.ExecContext(r.Context(), `DELETE FROM workspaces WHERE id = ?`, id)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleListMembers(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "id")
	rows, err := h.db.QueryContext(r.Context(),
		`SELECT m.id, m.workspace_id, m.user_id, m.role, m.created_at,
                u.name, u.email, u.avatar_url
         FROM workspace_members m
         JOIN users u ON u.id = m.user_id
         WHERE m.workspace_id = ?`, wsID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	members := []Member{}
	for rows.Next() {
		var m Member
		rows.Scan(&m.ID, &m.WorkspaceID, &m.UserID, &m.Role, &m.CreatedAt, &m.Name, &m.Email, &m.AvatarURL)
		members = append(members, m)
	}
	writeJSON(w, http.StatusOK, members)
}

func (h *Handler) handleLeaveWorkspace(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "id")
	uid := userIDFromCtx(r)
	h.db.ExecContext(r.Context(),
		`DELETE FROM workspace_members WHERE workspace_id = ? AND user_id = ?`, wsID, uid,
	)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleGetAssigneeFrequency(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []interface{}{})
}
