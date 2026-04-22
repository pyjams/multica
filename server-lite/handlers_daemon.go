package main

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// DaemonRegister creates or updates an agent runtime entry.
func (h *Handler) handleDaemonRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DaemonID   string                 `json:"daemon_id"`
		Name       string                 `json:"name"`
		Provider   string                 `json:"provider"`
		DeviceInfo string                 `json:"device_info"`
		Metadata   map[string]interface{} `json:"metadata"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	uid := userIDFromCtx(r)
	wsID := h.workspaceID(r)

	metaJSON := "{}"
	if req.Metadata != nil {
		b, _ := json.Marshal(req.Metadata)
		metaJSON = string(b)
	}
	if req.Provider == "" {
		req.Provider = "local"
	}

	// Generate a daemon token for this runtime
	daemonToken, err := generateToken("mdt_")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	ts := now()
	var runtimeID string

	// Check if runtime exists for this daemon_id
	err = h.db.QueryRowContext(r.Context(),
		`SELECT id FROM agent_runtimes WHERE daemon_id = ?`, req.DaemonID,
	).Scan(&runtimeID)

	if err != nil {
		// Create new runtime
		runtimeID = newID()
		if _, err := h.db.ExecContext(r.Context(),
			`INSERT INTO agent_runtimes (id, workspace_id, daemon_id, name, runtime_mode, provider, status,
                                         device_info, metadata, owner_id, daemon_token, last_seen_at, created_at, updated_at)
             VALUES (?, ?, ?, ?, 'local', ?, 'online', ?, ?, ?, ?, ?, ?, ?)`,
			runtimeID, wsID, req.DaemonID, req.Name, req.Provider,
			req.DeviceInfo, metaJSON, uid, daemonToken, ts, ts, ts,
		); err != nil {
			slog.Error("daemon register", "err", err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else {
		// Update existing runtime
		h.db.ExecContext(r.Context(),
			`UPDATE agent_runtimes SET status = 'online', last_seen_at = ?, updated_at = ?,
             name = ?, device_info = ?, metadata = ?
             WHERE id = ?`,
			ts, ts, req.Name, req.DeviceInfo, metaJSON, runtimeID,
		)
		// Get existing token
		h.db.QueryRowContext(r.Context(),
			`SELECT daemon_token FROM agent_runtimes WHERE id = ?`, runtimeID,
		).Scan(&daemonToken)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"runtime_id": runtimeID,
		"token":      daemonToken,
	})
}

func (h *Handler) handleDaemonDeregister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DaemonID string `json:"daemon_id"`
	}
	decodeJSON(r, &req)
	ts := now()
	h.db.ExecContext(r.Context(),
		`UPDATE agent_runtimes SET status = 'offline', updated_at = ? WHERE daemon_id = ?`, ts, req.DaemonID,
	)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleDaemonHeartbeat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DaemonID  string `json:"daemon_id"`
		RuntimeID string `json:"runtime_id"`
	}
	decodeJSON(r, &req)
	ts := now()
	h.db.ExecContext(r.Context(),
		`UPDATE agent_runtimes SET last_seen_at = ?, status = 'online', updated_at = ? WHERE id = ?`,
		ts, ts, req.RuntimeID,
	)
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

func (h *Handler) handleListPendingTasks(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	rows, err := h.db.QueryContext(r.Context(),
		`SELECT t.id, t.agent_id, t.runtime_id, t.issue_id, t.status, t.priority,
                t.dispatched_at, t.started_at, t.completed_at, t.result, t.error, t.created_at
         FROM agent_tasks t
         JOIN agents a ON a.id = t.agent_id
         WHERE t.runtime_id = ? AND t.status = 'queued'
         ORDER BY t.priority DESC, t.created_at ASC
         LIMIT 10`, runtimeID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	tasks := []map[string]interface{}{}
	for rows.Next() {
		t := scanTask(rows)
		if t == nil {
			continue
		}
		// Fetch agent instructions and issue details
		var agentInstructions, agentName string
		h.db.QueryRowContext(r.Context(),
			`SELECT name, instructions FROM agents WHERE id = ?`, t.AgentID,
		).Scan(&agentName, &agentInstructions)

		var issueTitle string
		var issueDesc, projectID *string
		h.db.QueryRowContext(r.Context(),
			`SELECT title, description, project_id FROM issues WHERE id = ?`, t.IssueID,
		).Scan(&issueTitle, &issueDesc, &projectID)

		task := map[string]interface{}{
			"id": t.ID, "agent_id": t.AgentID, "runtime_id": t.RuntimeID,
			"issue_id": t.IssueID, "status": t.Status, "priority": t.Priority,
			"dispatched_at": t.DispatchedAt, "started_at": t.StartedAt,
			"completed_at": t.CompletedAt, "result": t.Result,
			"error": t.Error, "created_at": t.CreatedAt,
			"agent_name":         agentName,
			"agent_instructions": agentInstructions,
			"issue_title":        issueTitle,
			"issue_description":  issueDesc,
		}
		tasks = append(tasks, task)
	}
	writeJSON(w, http.StatusOK, tasks)
}

func (h *Handler) handleClaimTask(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	var req struct {
		TaskID string `json:"task_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	ts := now()
	result, err := h.db.ExecContext(r.Context(),
		`UPDATE agent_tasks SET status = 'dispatched', dispatched_at = ?, runtime_id = ?, updated_at = ?
         WHERE id = ? AND status = 'queued'`,
		ts, runtimeID, ts, req.TaskID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		writeError(w, http.StatusConflict, "task already claimed or not found")
		return
	}

	row := h.db.QueryRowContext(r.Context(),
		`SELECT id, agent_id, runtime_id, issue_id, status, priority,
                dispatched_at, started_at, completed_at, result, error, created_at
         FROM agent_tasks WHERE id = ?`, req.TaskID,
	)
	t := scanTask(row)
	writeJSON(w, http.StatusOK, t)
}

func (h *Handler) handleGetTaskStatus(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	row := h.db.QueryRowContext(r.Context(),
		`SELECT id, agent_id, runtime_id, issue_id, status, priority,
                dispatched_at, started_at, completed_at, result, error, created_at
         FROM agent_tasks WHERE id = ?`, taskID,
	)
	t := scanTask(row)
	if t == nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (h *Handler) handleStartTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	ts := now()
	h.db.ExecContext(r.Context(),
		`UPDATE agent_tasks SET status = 'running', started_at = ? WHERE id = ?`, ts, taskID,
	)
	// Update agent status
	h.db.ExecContext(r.Context(),
		`UPDATE agents SET status = 'working', updated_at = ? WHERE id = (SELECT agent_id FROM agent_tasks WHERE id = ?)`,
		ts, taskID,
	)
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

func (h *Handler) handleReportProgress(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

func (h *Handler) handleCompleteTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	var req struct {
		Result interface{} `json:"result"`
	}
	decodeJSON(r, &req)

	ts := now()
	resultJSON := "null"
	if req.Result != nil {
		b, _ := json.Marshal(req.Result)
		resultJSON = string(b)
	}
	h.db.ExecContext(r.Context(),
		`UPDATE agent_tasks SET status = 'completed', completed_at = ?, result = ? WHERE id = ?`,
		ts, resultJSON, taskID,
	)
	// Reset agent status
	h.db.ExecContext(r.Context(),
		`UPDATE agents SET status = 'idle', updated_at = ? WHERE id = (SELECT agent_id FROM agent_tasks WHERE id = ?)`,
		ts, taskID,
	)
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

func (h *Handler) handleFailTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	var req struct {
		Error string `json:"error"`
	}
	decodeJSON(r, &req)

	ts := now()
	h.db.ExecContext(r.Context(),
		`UPDATE agent_tasks SET status = 'failed', completed_at = ?, error = ? WHERE id = ?`,
		ts, req.Error, taskID,
	)
	h.db.ExecContext(r.Context(),
		`UPDATE agents SET status = 'error', updated_at = ? WHERE id = (SELECT agent_id FROM agent_tasks WHERE id = ?)`,
		ts, taskID,
	)
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

func (h *Handler) handleReportTaskUsage(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

func (h *Handler) handleReportTaskMessages(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	var req struct {
		Messages []struct {
			Role     string                 `json:"role"`
			Content  string                 `json:"content"`
			Metadata map[string]interface{} `json:"metadata"`
		} `json:"messages"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	ts := now()
	for _, msg := range req.Messages {
		metaJSON := "{}"
		if msg.Metadata != nil {
			b, _ := json.Marshal(msg.Metadata)
			metaJSON = string(b)
		}
		h.db.ExecContext(r.Context(),
			`INSERT INTO task_messages (id, task_id, role, content, metadata, created_at)
             VALUES (?, ?, ?, ?, ?, ?)`,
			newID(), taskID, msg.Role, msg.Content, metaJSON, ts,
		)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

func (h *Handler) handleListTaskMessages(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	rows, err := h.db.QueryContext(r.Context(),
		`SELECT id, task_id, role, content, metadata, created_at
         FROM task_messages WHERE task_id = ? ORDER BY created_at ASC`, taskID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	messages := []TaskMessage{}
	for rows.Next() {
		var m TaskMessage
		var metaJSON string
		rows.Scan(&m.ID, &m.TaskID, &m.Role, &m.Content, &metaJSON, &m.CreatedAt)
		json.Unmarshal([]byte(metaJSON), &m.Metadata)
		if m.Metadata == nil {
			m.Metadata = map[string]interface{}{}
		}
		messages = append(messages, m)
	}
	writeJSON(w, http.StatusOK, messages)
}

func (h *Handler) handleReportRuntimeUsage(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

func (h *Handler) handleReportPingResult(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

func (h *Handler) handleReportUpdateResult(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}
