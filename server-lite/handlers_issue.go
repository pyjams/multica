package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) handleListIssues(w http.ResponseWriter, r *http.Request) {
	wsID := h.workspaceID(r)
	q := r.URL.Query()

	query := `SELECT id, workspace_id, project_id, number, identifier, title, description,
                     status, priority, assignee_type, assignee_id, creator_type, creator_id,
                     parent_issue_id, position, due_date, created_at, updated_at
              FROM issues WHERE workspace_id = ?`
	args := []interface{}{wsID}

	if s := q.Get("status"); s != "" {
		query += ` AND status = ?`
		args = append(args, s)
	}
	if p := q.Get("priority"); p != "" {
		query += ` AND priority = ?`
		args = append(args, p)
	}
	if aid := q.Get("assignee_id"); aid != "" {
		query += ` AND assignee_id = ?`
		args = append(args, aid)
	}
	if q.Get("open_only") == "true" {
		query += ` AND status NOT IN ('done','cancelled')`
	}

	query += ` ORDER BY position ASC, created_at DESC`

	limit := 50
	offset := 0
	if l := q.Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil {
			limit = n
		}
	}
	if o := q.Get("offset"); o != "" {
		if n, err := strconv.Atoi(o); err == nil {
			offset = n
		}
	}

	// Count total
	countQuery := `SELECT COUNT(*) FROM issues WHERE workspace_id = ?`
	countArgs := []interface{}{wsID}
	if s := q.Get("status"); s != "" {
		countQuery += ` AND status = ?`
		countArgs = append(countArgs, s)
	}
	var total int
	h.db.QueryRowContext(r.Context(), countQuery, countArgs...).Scan(&total)

	query += fmt.Sprintf(` LIMIT %d OFFSET %d`, limit, offset)

	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	issues := []Issue{}
	for rows.Next() {
		issue := scanIssue(rows)
		if issue != nil {
			issues = append(issues, *issue)
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"issues": issues,
		"total":  total,
	})
}

func (h *Handler) handleGetIssue(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, err := h.getIssue(r, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	writeJSON(w, http.StatusOK, issue)
}

func (h *Handler) getIssue(r *http.Request, id string) (*Issue, error) {
	row := h.db.QueryRowContext(r.Context(),
		`SELECT id, workspace_id, project_id, number, identifier, title, description,
                status, priority, assignee_type, assignee_id, creator_type, creator_id,
                parent_issue_id, position, due_date, created_at, updated_at
         FROM issues WHERE id = ?`, id,
	)
	issue := scanIssue(row)
	if issue == nil {
		return nil, sql.ErrNoRows
	}
	return issue, nil
}

func scanIssue(row rowScanner) *Issue {
	var issue Issue
	var projectID, assigneeType, assigneeID, parentID, desc, dueDate sql.NullString
	err := row.Scan(
		&issue.ID, &issue.WorkspaceID, &projectID, &issue.Number, &issue.Identifier,
		&issue.Title, &desc, &issue.Status, &issue.Priority,
		&assigneeType, &assigneeID, &issue.CreatorType, &issue.CreatorID,
		&parentID, &issue.Position, &dueDate, &issue.CreatedAt, &issue.UpdatedAt,
	)
	if err != nil {
		return nil
	}
	issue.ProjectID = nullStr(projectID)
	issue.AssigneeType = nullStr(assigneeType)
	issue.AssigneeID = nullStr(assigneeID)
	issue.ParentIssueID = nullStr(parentID)
	issue.Description = nullStr(desc)
	issue.DueDate = nullStr(dueDate)
	issue.Reactions = []interface{}{}
	return &issue
}

func (h *Handler) handleCreateIssue(w http.ResponseWriter, r *http.Request) {
	wsID := h.workspaceID(r)
	uid := userIDFromCtx(r)

	var req struct {
		Title        string  `json:"title"`
		Description  *string `json:"description"`
		Status       string  `json:"status"`
		Priority     string  `json:"priority"`
		AssigneeType *string `json:"assignee_type"`
		AssigneeID   *string `json:"assignee_id"`
		ParentIssueID *string `json:"parent_issue_id"`
		ProjectID    *string `json:"project_id"`
		DueDate      *string `json:"due_date"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	if req.Status == "" {
		req.Status = "backlog"
	}
	if req.Priority == "" {
		req.Priority = "none"
	}

	// Get workspace prefix and increment counter
	var prefix string
	var counter int
	h.db.QueryRowContext(r.Context(),
		`SELECT issue_prefix, issue_counter FROM workspaces WHERE id = ?`, wsID,
	).Scan(&prefix, &counter)

	counter++
	h.db.ExecContext(r.Context(),
		`UPDATE workspaces SET issue_counter = ? WHERE id = ?`, counter, wsID,
	)

	id := newID()
	identifier := fmt.Sprintf("%s-%d", prefix, counter)
	ts := now()

	if _, err := h.db.ExecContext(r.Context(),
		`INSERT INTO issues (id, workspace_id, project_id, number, identifier, title, description,
                              status, priority, assignee_type, assignee_id, creator_type, creator_id,
                              parent_issue_id, position, due_date, created_at, updated_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'member', ?, ?, ?, ?, ?, ?)`,
		id, wsID, req.ProjectID, counter, identifier, req.Title, req.Description,
		req.Status, req.Priority, req.AssigneeType, req.AssigneeID,
		uid, req.ParentIssueID, float64(counter), req.DueDate, ts, ts,
	); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	issue, _ := h.getIssue(r, id)
	h.broadcastEvent(wsID, "issue_created", issue)
	writeJSON(w, http.StatusCreated, issue)
}

func (h *Handler) handleUpdateIssue(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Title         *string  `json:"title"`
		Description   *string  `json:"description"`
		Status        *string  `json:"status"`
		Priority      *string  `json:"priority"`
		AssigneeType  *string  `json:"assignee_type"`
		AssigneeID    *string  `json:"assignee_id"`
		Position      *float64 `json:"position"`
		DueDate       *string  `json:"due_date"`
		ParentIssueID *string  `json:"parent_issue_id"`
		ProjectID     *string  `json:"project_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	ts := now()
	setField := func(col string, val interface{}) {
		h.db.ExecContext(r.Context(), fmt.Sprintf(`UPDATE issues SET %s = ?, updated_at = ? WHERE id = ?`, col), val, ts, id)
	}

	if req.Title != nil {
		setField("title", *req.Title)
	}
	if req.Description != nil {
		setField("description", *req.Description)
	}
	if req.Status != nil {
		setField("status", *req.Status)
	}
	if req.Priority != nil {
		setField("priority", *req.Priority)
	}
	if req.AssigneeType != nil {
		setField("assignee_type", *req.AssigneeType)
	}
	if req.AssigneeID != nil {
		setField("assignee_id", *req.AssigneeID)
	}
	if req.Position != nil {
		setField("position", *req.Position)
	}
	if req.DueDate != nil {
		setField("due_date", *req.DueDate)
	}
	if req.ParentIssueID != nil {
		setField("parent_issue_id", *req.ParentIssueID)
	}
	if req.ProjectID != nil {
		setField("project_id", *req.ProjectID)
	}

	issue, err := h.getIssue(r, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	h.broadcastEvent(issue.WorkspaceID, "issue_updated", issue)
	writeJSON(w, http.StatusOK, issue)
}

func (h *Handler) handleDeleteIssue(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	h.db.ExecContext(r.Context(), `DELETE FROM issues WHERE id = ?`, id)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleBatchUpdateIssues(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IssueIDs []string               `json:"issue_ids"`
		Updates  map[string]interface{} `json:"updates"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	ts := now()
	count := 0
	for _, id := range req.IssueIDs {
		for col, val := range req.Updates {
			if col == "status" || col == "priority" || col == "assignee_id" || col == "assignee_type" {
				h.db.ExecContext(r.Context(), fmt.Sprintf(`UPDATE issues SET %s = ?, updated_at = ? WHERE id = ?`, col), val, ts, id)
			}
		}
		count++
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"updated": count})
}

func (h *Handler) handleBatchDeleteIssues(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IssueIDs []string `json:"issue_ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	count := 0
	for _, id := range req.IssueIDs {
		h.db.ExecContext(r.Context(), `DELETE FROM issues WHERE id = ?`, id)
		count++
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"deleted": count})
}

func (h *Handler) handleListChildIssues(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rows, err := h.db.QueryContext(r.Context(),
		`SELECT id, workspace_id, project_id, number, identifier, title, description,
                status, priority, assignee_type, assignee_id, creator_type, creator_id,
                parent_issue_id, position, due_date, created_at, updated_at
         FROM issues WHERE parent_issue_id = ?`, id,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	issues := []Issue{}
	for rows.Next() {
		issue := scanIssue(rows)
		if issue != nil {
			issues = append(issues, *issue)
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"issues": issues})
}

func (h *Handler) handleSearchIssues(w http.ResponseWriter, r *http.Request) {
	wsID := h.workspaceID(r)
	q := r.URL.Query().Get("q")
	includeClosed := r.URL.Query().Get("include_closed") == "true"

	query := `SELECT id, workspace_id, project_id, number, identifier, title, description,
                     status, priority, assignee_type, assignee_id, creator_type, creator_id,
                     parent_issue_id, position, due_date, created_at, updated_at
              FROM issues
              WHERE workspace_id = ? AND (title LIKE ? OR description LIKE ?)`
	args := []interface{}{wsID, "%" + q + "%", "%" + q + "%"}
	if !includeClosed {
		query += ` AND status NOT IN ('done','cancelled')`
	}
	query += ` LIMIT 20`

	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	issues := []map[string]interface{}{}
	for rows.Next() {
		issue := scanIssue(rows)
		if issue == nil {
			continue
		}
		item := map[string]interface{}{
			"id": issue.ID, "workspace_id": issue.WorkspaceID, "number": issue.Number,
			"identifier": issue.Identifier, "title": issue.Title, "description": issue.Description,
			"status": issue.Status, "priority": issue.Priority, "assignee_type": issue.AssigneeType,
			"assignee_id": issue.AssigneeID, "creator_type": issue.CreatorType, "creator_id": issue.CreatorID,
			"parent_issue_id": issue.ParentIssueID, "project_id": issue.ProjectID,
			"position": issue.Position, "due_date": issue.DueDate,
			"created_at": issue.CreatedAt, "updated_at": issue.UpdatedAt,
			"reactions": []interface{}{}, "match_source": "title",
		}
		issues = append(issues, item)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"issues": issues,
		"total":  len(issues),
	})
}

func (h *Handler) handleListIssueSubscribers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []interface{}{})
}

func (h *Handler) handleSubscribeIssue(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleUnsubscribeIssue(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleListTimeline(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []interface{}{})
}

func (h *Handler) handleAddIssueReaction(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Emoji string `json:"emoji"`
	}
	decodeJSON(r, &req)
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id": newID(), "issue_id": id, "emoji": req.Emoji,
		"actor_type": "member", "actor_id": userIDFromCtx(r),
		"created_at": now(),
	})
}

func (h *Handler) handleRemoveIssueReaction(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleListAttachments(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []interface{}{})
}

func (h *Handler) handleGetIssueUsage(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total_input_tokens": 0, "total_output_tokens": 0,
		"total_cache_read_tokens": 0, "total_cache_write_tokens": 0, "task_count": 0,
	})
}

// handleRunIssue creates a task for the assigned agent and starts executing it immediately.
// No daemon needed — the CLI is spawned directly by the server process.
func (h *Handler) handleRunIssue(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")

	// Load issue and its assigned agent
	var agentID, assigneeType sql.NullString
	var wsID string
	err := h.db.QueryRowContext(r.Context(),
		`SELECT workspace_id, assignee_type, assignee_id FROM issues WHERE id = ?`, issueID,
	).Scan(&wsID, &assigneeType, &agentID)
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	if !agentID.Valid || agentID.String == "" || assigneeType.String != "agent" {
		writeError(w, http.StatusBadRequest, "issue has no agent assigned")
		return
	}

	// Check the agent exists and is not archived
	var runtimeConfigJSON string
	err = h.db.QueryRowContext(r.Context(),
		`SELECT runtime_config FROM agents WHERE id = ? AND archived_at IS NULL`, agentID.String,
	).Scan(&runtimeConfigJSON)
	if err != nil {
		writeError(w, http.StatusBadRequest, "agent not found or archived")
		return
	}

	// Check CLI is reachable before creating the task
	cfg := resolveRunConfig(runtimeConfigJSON)
	if _, err := exec.LookPath(cfg.CLI); err != nil {
		// CLI not found — return a helpful error with env var hints
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf(
			"CLI %q not found in PATH. Set CLAUDE_PATH, CODEX_PATH or OPENCODE_PATH env var, "+
				"or set runtime_config.cli to the full path to the executable.",
			cfg.CLI,
		))
		return
	}

	// Create the task
	taskID := newID()
	ts := now()
	if _, err := h.db.ExecContext(r.Context(),
		`INSERT INTO agent_tasks (id, agent_id, runtime_id, issue_id, status, priority, created_at)
         VALUES (?, ?, 'local', ?, 'running', 0, ?)`,
		taskID, agentID.String, issueID, ts,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create task")
		return
	}

	// Save the user's prompt (issue context) as the first task message
	h.db.ExecContext(r.Context(),
		`INSERT INTO task_messages (id, task_id, role, content, metadata, created_at) VALUES (?, ?, 'user', ?, '{}', ?)`,
		newID(), taskID, "Running agent on issue: "+issueID, ts,
	)

	task, _ := h.getTaskByID(taskID)

	// Start execution in the background — no daemon, no polling
	h.executeTask(taskID, agentID.String, issueID, wsID)

	writeJSON(w, http.StatusCreated, task)
}

func (h *Handler) getTaskByID(taskID string) (*AgentTask, error) {
	row := h.db.QueryRowContext(context.Background(),
		`SELECT id, agent_id, runtime_id, issue_id, status, priority,
                dispatched_at, started_at, completed_at, result, error, created_at
         FROM agent_tasks WHERE id = ?`, taskID,
	)
	t := scanTask(row)
	if t == nil {
		return nil, sql.ErrNoRows
	}
	return t, nil
}

// broadcastEvent sends a WebSocket event to all workspace clients.
func (h *Handler) broadcastEvent(workspaceID, eventType string, payload interface{}) {
	data, err := json.Marshal(map[string]interface{}{
		"type":    eventType,
		"payload": payload,
	})
	if err != nil {
		return
	}
	h.hub.Broadcast(workspaceID, data)
}
