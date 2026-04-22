package main

import (
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) handleListProjects(w http.ResponseWriter, r *http.Request) {
	wsID := h.workspaceID(r)
	status := r.URL.Query().Get("status")

	query := `SELECT p.id, p.workspace_id, p.title, p.description, p.icon, p.status, p.priority,
                     p.lead_type, p.lead_id, p.created_at, p.updated_at,
                     COUNT(i.id) as issue_count,
                     SUM(CASE WHEN i.status = 'done' THEN 1 ELSE 0 END) as done_count
              FROM projects p
              LEFT JOIN issues i ON i.project_id = p.id
              WHERE p.workspace_id = ?`
	args := []interface{}{wsID}

	if status != "" {
		query += ` AND p.status = ?`
		args = append(args, status)
	}
	query += ` GROUP BY p.id ORDER BY p.created_at DESC`

	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	projects := []Project{}
	for rows.Next() {
		p := scanProject(rows)
		if p != nil {
			projects = append(projects, *p)
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"projects": projects,
		"total":    len(projects),
	})
}

func (h *Handler) handleGetProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := h.getProject(r, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (h *Handler) getProject(r *http.Request, id string) (*Project, error) {
	row := h.db.QueryRowContext(r.Context(),
		`SELECT p.id, p.workspace_id, p.title, p.description, p.icon, p.status, p.priority,
                p.lead_type, p.lead_id, p.created_at, p.updated_at,
                COUNT(i.id) as issue_count,
                SUM(CASE WHEN i.status = 'done' THEN 1 ELSE 0 END) as done_count
         FROM projects p
         LEFT JOIN issues i ON i.project_id = p.id
         WHERE p.id = ?
         GROUP BY p.id`, id,
	)
	p := scanProject(row)
	if p == nil {
		return nil, sql.ErrNoRows
	}
	return p, nil
}

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanProject(row rowScanner) *Project {
	var p Project
	var desc, icon, leadType, leadID sql.NullString
	var doneCount sql.NullInt64
	err := row.Scan(&p.ID, &p.WorkspaceID, &p.Title, &desc, &icon,
		&p.Status, &p.Priority, &leadType, &leadID,
		&p.CreatedAt, &p.UpdatedAt, &p.IssueCount, &doneCount)
	if err != nil {
		return nil
	}
	p.Description = nullStr(desc)
	p.Icon = nullStr(icon)
	p.LeadType = nullStr(leadType)
	p.LeadID = nullStr(leadID)
	if doneCount.Valid {
		p.DoneCount = int(doneCount.Int64)
	}
	return &p
}

func (h *Handler) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	wsID := h.workspaceID(r)
	var req struct {
		Title       string  `json:"title"`
		Description *string `json:"description"`
		Icon        *string `json:"icon"`
		Status      string  `json:"status"`
		Priority    string  `json:"priority"`
		LeadType    *string `json:"lead_type"`
		LeadID      *string `json:"lead_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	if req.Status == "" {
		req.Status = "planned"
	}
	if req.Priority == "" {
		req.Priority = "none"
	}

	id := newID()
	ts := now()
	if _, err := h.db.ExecContext(r.Context(),
		`INSERT INTO projects (id, workspace_id, title, description, icon, status, priority, lead_type, lead_id, created_at, updated_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, wsID, req.Title, req.Description, req.Icon, req.Status, req.Priority,
		req.LeadType, req.LeadID, ts, ts,
	); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	p, _ := h.getProject(r, id)
	writeJSON(w, http.StatusCreated, p)
}

func (h *Handler) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Title       *string `json:"title"`
		Description *string `json:"description"`
		Icon        *string `json:"icon"`
		Status      *string `json:"status"`
		Priority    *string `json:"priority"`
		LeadType    *string `json:"lead_type"`
		LeadID      *string `json:"lead_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	ts := now()
	if req.Title != nil {
		h.db.ExecContext(r.Context(), `UPDATE projects SET title = ?, updated_at = ? WHERE id = ?`, *req.Title, ts, id)
	}
	if req.Description != nil {
		h.db.ExecContext(r.Context(), `UPDATE projects SET description = ?, updated_at = ? WHERE id = ?`, *req.Description, ts, id)
	}
	if req.Icon != nil {
		h.db.ExecContext(r.Context(), `UPDATE projects SET icon = ?, updated_at = ? WHERE id = ?`, *req.Icon, ts, id)
	}
	if req.Status != nil {
		h.db.ExecContext(r.Context(), `UPDATE projects SET status = ?, updated_at = ? WHERE id = ?`, *req.Status, ts, id)
	}
	if req.Priority != nil {
		h.db.ExecContext(r.Context(), `UPDATE projects SET priority = ?, updated_at = ? WHERE id = ?`, *req.Priority, ts, id)
	}
	if req.LeadType != nil {
		h.db.ExecContext(r.Context(), `UPDATE projects SET lead_type = ?, updated_at = ? WHERE id = ?`, *req.LeadType, ts, id)
	}
	if req.LeadID != nil {
		h.db.ExecContext(r.Context(), `UPDATE projects SET lead_id = ?, updated_at = ? WHERE id = ?`, *req.LeadID, ts, id)
	}

	p, err := h.getProject(r, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (h *Handler) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	h.db.ExecContext(r.Context(), `DELETE FROM projects WHERE id = ?`, id)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleSearchProjects(w http.ResponseWriter, r *http.Request) {
	wsID := h.workspaceID(r)
	q := r.URL.Query().Get("q")
	rows, err := h.db.QueryContext(r.Context(),
		`SELECT p.id, p.workspace_id, p.title, p.description, p.icon, p.status, p.priority,
                p.lead_type, p.lead_id, p.created_at, p.updated_at,
                COUNT(i.id), SUM(CASE WHEN i.status = 'done' THEN 1 ELSE 0 END)
         FROM projects p
         LEFT JOIN issues i ON i.project_id = p.id
         WHERE p.workspace_id = ? AND (p.title LIKE ? OR p.description LIKE ?)
         GROUP BY p.id
         LIMIT 20`,
		wsID, "%"+q+"%", "%"+q+"%",
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	projects := []map[string]interface{}{}
	for rows.Next() {
		p := scanProject(rows)
		if p == nil {
			continue
		}
		item := map[string]interface{}{
			"id": p.ID, "workspace_id": p.WorkspaceID, "title": p.Title,
			"description": p.Description, "icon": p.Icon, "status": p.Status,
			"priority": p.Priority, "lead_type": p.LeadType, "lead_id": p.LeadID,
			"created_at": p.CreatedAt, "updated_at": p.UpdatedAt,
			"issue_count": p.IssueCount, "done_count": p.DoneCount,
			"match_source": "title",
		}
		projects = append(projects, item)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"projects": projects,
		"total":    len(projects),
	})
}
