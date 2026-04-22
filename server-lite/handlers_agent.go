package main

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) handleListAgents(w http.ResponseWriter, r *http.Request) {
	wsID := h.workspaceID(r)
	includeArchived := r.URL.Query().Get("include_archived") == "true"

	query := `SELECT id, workspace_id, runtime_id, name, description, instructions, avatar_url,
                     runtime_mode, runtime_config, visibility, status, max_concurrent_tasks,
                     owner_id, archived_at, archived_by, created_at, updated_at
              FROM agents WHERE workspace_id = ?`
	if !includeArchived {
		query += ` AND archived_at IS NULL`
	}
	query += ` ORDER BY created_at DESC`

	rows, err := h.db.QueryContext(r.Context(), query, wsID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	agents := []Agent{}
	for rows.Next() {
		a := h.scanAgent(r, rows)
		if a != nil {
			agents = append(agents, *a)
		}
	}
	writeJSON(w, http.StatusOK, agents)
}

func (h *Handler) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	a, err := h.getAgent(r, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (h *Handler) getAgent(r *http.Request, id string) (*Agent, error) {
	row := h.db.QueryRowContext(r.Context(),
		`SELECT id, workspace_id, runtime_id, name, description, instructions, avatar_url,
                runtime_mode, runtime_config, visibility, status, max_concurrent_tasks,
                owner_id, archived_at, archived_by, created_at, updated_at
         FROM agents WHERE id = ?`, id,
	)
	a := h.scanAgent(r, row)
	if a == nil {
		return nil, sql.ErrNoRows
	}
	return a, nil
}

func (h *Handler) scanAgent(r *http.Request, row rowScanner) *Agent {
	var a Agent
	var avatarURL, ownerID, archivedAt, archivedBy sql.NullString
	var runtimeConfigJSON string
	err := row.Scan(&a.ID, &a.WorkspaceID, &a.RuntimeID, &a.Name, &a.Description,
		&a.Instructions, &avatarURL, &a.RuntimeMode, &runtimeConfigJSON,
		&a.Visibility, &a.Status, &a.MaxConcurrentTasks,
		&ownerID, &archivedAt, &archivedBy, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil
	}
	a.AvatarURL = nullStr(avatarURL)
	a.OwnerID = nullStr(ownerID)
	a.ArchivedAt = nullStr(archivedAt)
	a.ArchivedBy = nullStr(archivedBy)
	json.Unmarshal([]byte(runtimeConfigJSON), &a.RuntimeConfig)
	if a.RuntimeConfig == nil {
		a.RuntimeConfig = map[string]interface{}{}
	}

	// Load skills
	skillRows, err := h.db.QueryContext(r.Context(),
		`SELECT s.id, s.workspace_id, s.name, s.description, s.content, s.config, s.created_by, s.created_at, s.updated_at
         FROM skills s JOIN agent_skills ags ON ags.skill_id = s.id WHERE ags.agent_id = ?`, a.ID,
	)
	if err == nil {
		defer skillRows.Close()
		for skillRows.Next() {
			s := scanSkillRow(skillRows)
			if s != nil {
				a.Skills = append(a.Skills, *s)
			}
		}
	}
	if a.Skills == nil {
		a.Skills = []Skill{}
	}
	return &a
}

func (h *Handler) handleCreateAgent(w http.ResponseWriter, r *http.Request) {
	wsID := h.workspaceID(r)
	uid := userIDFromCtx(r)

	var req struct {
		Name               string                 `json:"name"`
		Description        string                 `json:"description"`
		Instructions       string                 `json:"instructions"`
		AvatarURL          *string                `json:"avatar_url"`
		RuntimeID          string                 `json:"runtime_id"`
		RuntimeConfig      map[string]interface{} `json:"runtime_config"`
		Visibility         string                 `json:"visibility"`
		MaxConcurrentTasks int                    `json:"max_concurrent_tasks"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.Visibility == "" {
		req.Visibility = "workspace"
	}
	if req.MaxConcurrentTasks == 0 {
		req.MaxConcurrentTasks = 1
	}
	configJSON := "{}"
	if req.RuntimeConfig != nil {
		b, _ := json.Marshal(req.RuntimeConfig)
		configJSON = string(b)
	}

	id := newID()
	ts := now()
	if _, err := h.db.ExecContext(r.Context(),
		`INSERT INTO agents (id, workspace_id, runtime_id, name, description, instructions, avatar_url,
                              runtime_mode, runtime_config, visibility, status, max_concurrent_tasks,
                              owner_id, created_at, updated_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, 'local', ?, ?, 'offline', ?, ?, ?, ?)`,
		id, wsID, req.RuntimeID, req.Name, req.Description, req.Instructions, req.AvatarURL,
		configJSON, req.Visibility, req.MaxConcurrentTasks, uid, ts, ts,
	); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	a, _ := h.getAgent(r, id)
	writeJSON(w, http.StatusCreated, a)
}

func (h *Handler) handleUpdateAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Name               *string                `json:"name"`
		Description        *string                `json:"description"`
		Instructions       *string                `json:"instructions"`
		AvatarURL          *string                `json:"avatar_url"`
		RuntimeID          *string                `json:"runtime_id"`
		RuntimeConfig      map[string]interface{} `json:"runtime_config"`
		Visibility         *string                `json:"visibility"`
		Status             *string                `json:"status"`
		MaxConcurrentTasks *int                   `json:"max_concurrent_tasks"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	ts := now()
	if req.Name != nil {
		h.db.ExecContext(r.Context(), `UPDATE agents SET name = ?, updated_at = ? WHERE id = ?`, *req.Name, ts, id)
	}
	if req.Description != nil {
		h.db.ExecContext(r.Context(), `UPDATE agents SET description = ?, updated_at = ? WHERE id = ?`, *req.Description, ts, id)
	}
	if req.Instructions != nil {
		h.db.ExecContext(r.Context(), `UPDATE agents SET instructions = ?, updated_at = ? WHERE id = ?`, *req.Instructions, ts, id)
	}
	if req.Status != nil {
		h.db.ExecContext(r.Context(), `UPDATE agents SET status = ?, updated_at = ? WHERE id = ?`, *req.Status, ts, id)
	}
	if req.RuntimeConfig != nil {
		b, _ := json.Marshal(req.RuntimeConfig)
		h.db.ExecContext(r.Context(), `UPDATE agents SET runtime_config = ?, updated_at = ? WHERE id = ?`, string(b), ts, id)
	}
	if req.Visibility != nil {
		h.db.ExecContext(r.Context(), `UPDATE agents SET visibility = ?, updated_at = ? WHERE id = ?`, *req.Visibility, ts, id)
	}
	if req.MaxConcurrentTasks != nil {
		h.db.ExecContext(r.Context(), `UPDATE agents SET max_concurrent_tasks = ?, updated_at = ? WHERE id = ?`, *req.MaxConcurrentTasks, ts, id)
	}

	a, err := h.getAgent(r, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (h *Handler) handleArchiveAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	uid := userIDFromCtx(r)
	ts := now()
	h.db.ExecContext(r.Context(),
		`UPDATE agents SET archived_at = ?, archived_by = ?, updated_at = ? WHERE id = ?`, ts, uid, ts, id,
	)
	a, _ := h.getAgent(r, id)
	writeJSON(w, http.StatusOK, a)
}

func (h *Handler) handleRestoreAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ts := now()
	h.db.ExecContext(r.Context(),
		`UPDATE agents SET archived_at = NULL, archived_by = NULL, updated_at = ? WHERE id = ?`, ts, id,
	)
	a, _ := h.getAgent(r, id)
	writeJSON(w, http.StatusOK, a)
}

func (h *Handler) handleListAgentTasks(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")
	rows, err := h.db.QueryContext(r.Context(),
		`SELECT id, agent_id, runtime_id, issue_id, status, priority,
                dispatched_at, started_at, completed_at, result, error, created_at
         FROM agent_tasks WHERE agent_id = ? ORDER BY created_at DESC LIMIT 50`, agentID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	tasks := []AgentTask{}
	for rows.Next() {
		t := scanTask(rows)
		if t != nil {
			tasks = append(tasks, *t)
		}
	}
	writeJSON(w, http.StatusOK, tasks)
}

func scanTask(row rowScanner) *AgentTask {
	var t AgentTask
	var dispatchedAt, startedAt, completedAt, resultJSON, errStr sql.NullString
	err := row.Scan(&t.ID, &t.AgentID, &t.RuntimeID, &t.IssueID, &t.Status, &t.Priority,
		&dispatchedAt, &startedAt, &completedAt, &resultJSON, &errStr, &t.CreatedAt)
	if err != nil {
		return nil
	}
	t.DispatchedAt = nullStr(dispatchedAt)
	t.StartedAt = nullStr(startedAt)
	t.CompletedAt = nullStr(completedAt)
	t.Error = nullStr(errStr)
	if resultJSON.Valid {
		json.Unmarshal([]byte(resultJSON.String), &t.Result)
	}
	return &t
}

func (h *Handler) handleGetActiveTask(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	rows, err := h.db.QueryContext(r.Context(),
		`SELECT id, agent_id, runtime_id, issue_id, status, priority,
                dispatched_at, started_at, completed_at, result, error, created_at
         FROM agent_tasks WHERE issue_id = ? AND status IN ('queued','dispatched','running')
         ORDER BY created_at DESC`, issueID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	tasks := []AgentTask{}
	for rows.Next() {
		t := scanTask(rows)
		if t != nil {
			tasks = append(tasks, *t)
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"tasks": tasks})
}

func (h *Handler) handleListTasksByIssue(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	rows, err := h.db.QueryContext(r.Context(),
		`SELECT id, agent_id, runtime_id, issue_id, status, priority,
                dispatched_at, started_at, completed_at, result, error, created_at
         FROM agent_tasks WHERE issue_id = ? ORDER BY created_at DESC`, issueID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	tasks := []AgentTask{}
	for rows.Next() {
		t := scanTask(rows)
		if t != nil {
			tasks = append(tasks, *t)
		}
	}
	writeJSON(w, http.StatusOK, tasks)
}

func (h *Handler) handleCancelTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	// Cancel the running process if any
	cancelTask(taskID)
	ts := now()
	h.db.ExecContext(r.Context(),
		`UPDATE agent_tasks SET status = 'cancelled', completed_at = ? WHERE id = ?`, ts, taskID,
	)
	var t AgentTask
	h.db.QueryRowContext(r.Context(),
		`SELECT id, agent_id, runtime_id, issue_id, status, priority,
                dispatched_at, started_at, completed_at, result, error, created_at
         FROM agent_tasks WHERE id = ?`, taskID,
	).Scan(&t.ID, &t.AgentID, &t.RuntimeID, &t.IssueID, &t.Status, &t.Priority,
		&t.DispatchedAt, &t.StartedAt, &t.CompletedAt, &t.Result, &t.Error, &t.CreatedAt)
	writeJSON(w, http.StatusOK, t)
}

func (h *Handler) handleCancelTaskByUser(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	ts := now()
	h.db.ExecContext(r.Context(),
		`UPDATE agent_tasks SET status = 'cancelled', completed_at = ? WHERE id = ?`, ts, taskID,
	)
	w.WriteHeader(http.StatusNoContent)
}

// --- Runtimes ---

func (h *Handler) handleListRuntimes(w http.ResponseWriter, r *http.Request) {
	wsID := h.workspaceID(r)
	owner := r.URL.Query().Get("owner")
	uid := userIDFromCtx(r)

	query := `SELECT id, workspace_id, daemon_id, name, runtime_mode, provider, status,
                     device_info, metadata, owner_id, last_seen_at, created_at, updated_at
              FROM agent_runtimes WHERE workspace_id = ?`
	args := []interface{}{wsID}
	if owner == "me" {
		query += ` AND owner_id = ?`
		args = append(args, uid)
	}
	query += ` ORDER BY created_at DESC`

	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	runtimes := []AgentRuntime{}
	for rows.Next() {
		rt := scanRuntime(rows)
		if rt != nil {
			runtimes = append(runtimes, *rt)
		}
	}
	writeJSON(w, http.StatusOK, runtimes)
}

func scanRuntime(row rowScanner) *AgentRuntime {
	var rt AgentRuntime
	var daemonID, ownerID, lastSeenAt sql.NullString
	var metadataJSON string
	err := row.Scan(&rt.ID, &rt.WorkspaceID, &daemonID, &rt.Name, &rt.RuntimeMode,
		&rt.Provider, &rt.Status, &rt.DeviceInfo, &metadataJSON,
		&ownerID, &lastSeenAt, &rt.CreatedAt, &rt.UpdatedAt)
	if err != nil {
		return nil
	}
	rt.DaemonID = nullStr(daemonID)
	rt.OwnerID = nullStr(ownerID)
	rt.LastSeenAt = nullStr(lastSeenAt)
	json.Unmarshal([]byte(metadataJSON), &rt.Metadata)
	if rt.Metadata == nil {
		rt.Metadata = map[string]interface{}{}
	}
	return &rt
}

func (h *Handler) handleDeleteRuntime(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	h.db.ExecContext(r.Context(), `DELETE FROM agent_runtimes WHERE id = ?`, runtimeID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleGetRuntimeUsage(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []interface{}{})
}

func (h *Handler) handleGetRuntimeActivity(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []interface{}{})
}

func (h *Handler) handleInitiatePing(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	id := newID()
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id": id, "runtime_id": runtimeID, "status": "pending",
		"created_at": now(), "updated_at": now(),
	})
}

func (h *Handler) handleGetPing(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	pingID := chi.URLParam(r, "pingId")
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id": pingID, "runtime_id": runtimeID, "status": "completed",
		"output": "pong", "duration_ms": 10,
		"created_at": now(), "updated_at": now(),
	})
}

func (h *Handler) handleInitiateUpdate(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	id := newID()
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id": id, "runtime_id": runtimeID, "status": "pending",
		"target_version": "", "created_at": now(), "updated_at": now(),
	})
}

func (h *Handler) handleGetUpdate(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	updateID := chi.URLParam(r, "updateId")
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id": updateID, "runtime_id": runtimeID, "status": "completed",
		"target_version": "", "created_at": now(), "updated_at": now(),
	})
}

// --- Skills ---

func (h *Handler) handleListSkills(w http.ResponseWriter, r *http.Request) {
	wsID := h.workspaceID(r)
	rows, err := h.db.QueryContext(r.Context(),
		`SELECT id, workspace_id, name, description, content, config, created_by, created_at, updated_at
         FROM skills WHERE workspace_id = ? ORDER BY created_at DESC`, wsID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	skills := []Skill{}
	for rows.Next() {
		s := scanSkillRow(rows)
		if s != nil {
			skills = append(skills, *s)
		}
	}
	writeJSON(w, http.StatusOK, skills)
}

func scanSkillRow(row rowScanner) *Skill {
	var s Skill
	var createdBy sql.NullString
	var configJSON string
	err := row.Scan(&s.ID, &s.WorkspaceID, &s.Name, &s.Description, &s.Content,
		&configJSON, &createdBy, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil
	}
	s.CreatedBy = nullStr(createdBy)
	json.Unmarshal([]byte(configJSON), &s.Config)
	if s.Config == nil {
		s.Config = map[string]interface{}{}
	}
	s.Files = []SkillFile{}
	return &s
}

func (h *Handler) handleGetSkill(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s, err := h.getSkill(r, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "skill not found")
		return
	}
	writeJSON(w, http.StatusOK, s)
}

func (h *Handler) getSkill(r *http.Request, id string) (*Skill, error) {
	row := h.db.QueryRowContext(r.Context(),
		`SELECT id, workspace_id, name, description, content, config, created_by, created_at, updated_at
         FROM skills WHERE id = ?`, id,
	)
	s := scanSkillRow(row)
	if s == nil {
		return nil, sql.ErrNoRows
	}
	// Load files
	fileRows, _ := h.db.QueryContext(r.Context(),
		`SELECT id, skill_id, path, content, created_at, updated_at FROM skill_files WHERE skill_id = ?`, id,
	)
	if fileRows != nil {
		defer fileRows.Close()
		for fileRows.Next() {
			var f SkillFile
			fileRows.Scan(&f.ID, &f.SkillID, &f.Path, &f.Content, &f.CreatedAt, &f.UpdatedAt)
			s.Files = append(s.Files, f)
		}
	}
	return s, nil
}

func (h *Handler) handleCreateSkill(w http.ResponseWriter, r *http.Request) {
	wsID := h.workspaceID(r)
	uid := userIDFromCtx(r)
	var req struct {
		Name        string                 `json:"name"`
		Description string                 `json:"description"`
		Content     string                 `json:"content"`
		Config      map[string]interface{} `json:"config"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	configJSON := "{}"
	if req.Config != nil {
		b, _ := json.Marshal(req.Config)
		configJSON = string(b)
	}

	id := newID()
	ts := now()
	h.db.ExecContext(r.Context(),
		`INSERT INTO skills (id, workspace_id, name, description, content, config, created_by, created_at, updated_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, wsID, req.Name, req.Description, req.Content, configJSON, uid, ts, ts,
	)
	s, _ := h.getSkill(r, id)
	writeJSON(w, http.StatusCreated, s)
}

func (h *Handler) handleUpdateSkill(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Name        *string                `json:"name"`
		Description *string                `json:"description"`
		Content     *string                `json:"content"`
	}
	decodeJSON(r, &req)
	ts := now()
	if req.Name != nil {
		h.db.ExecContext(r.Context(), `UPDATE skills SET name = ?, updated_at = ? WHERE id = ?`, *req.Name, ts, id)
	}
	if req.Description != nil {
		h.db.ExecContext(r.Context(), `UPDATE skills SET description = ?, updated_at = ? WHERE id = ?`, *req.Description, ts, id)
	}
	if req.Content != nil {
		h.db.ExecContext(r.Context(), `UPDATE skills SET content = ?, updated_at = ? WHERE id = ?`, *req.Content, ts, id)
	}
	s, err := h.getSkill(r, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "skill not found")
		return
	}
	writeJSON(w, http.StatusOK, s)
}

func (h *Handler) handleDeleteSkill(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	h.db.ExecContext(r.Context(), `DELETE FROM skills WHERE id = ?`, id)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleImportSkill(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "skill import not supported in lite mode")
}

func (h *Handler) handleListAgentSkills(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")
	rows, err := h.db.QueryContext(r.Context(),
		`SELECT s.id, s.workspace_id, s.name, s.description, s.content, s.config, s.created_by, s.created_at, s.updated_at
         FROM skills s JOIN agent_skills ags ON ags.skill_id = s.id WHERE ags.agent_id = ?`, agentID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	skills := []Skill{}
	for rows.Next() {
		s := scanSkillRow(rows)
		if s != nil {
			skills = append(skills, *s)
		}
	}
	writeJSON(w, http.StatusOK, skills)
}

func (h *Handler) handleSetAgentSkills(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")
	var req struct {
		SkillIDs []string `json:"skill_ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	h.db.ExecContext(r.Context(), `DELETE FROM agent_skills WHERE agent_id = ?`, agentID)
	for _, skillID := range req.SkillIDs {
		h.db.ExecContext(r.Context(),
			`INSERT OR IGNORE INTO agent_skills (agent_id, skill_id) VALUES (?, ?)`, agentID, skillID,
		)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleListSkillFiles(w http.ResponseWriter, r *http.Request) {
	skillID := chi.URLParam(r, "id")
	rows, err := h.db.QueryContext(r.Context(),
		`SELECT id, skill_id, path, content, created_at, updated_at FROM skill_files WHERE skill_id = ?`, skillID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	files := []SkillFile{}
	for rows.Next() {
		var f SkillFile
		rows.Scan(&f.ID, &f.SkillID, &f.Path, &f.Content, &f.CreatedAt, &f.UpdatedAt)
		files = append(files, f)
	}
	writeJSON(w, http.StatusOK, files)
}

func (h *Handler) handleUpsertSkillFile(w http.ResponseWriter, r *http.Request) {
	skillID := chi.URLParam(r, "id")
	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	ts := now()
	id := newID()
	h.db.ExecContext(r.Context(),
		`INSERT INTO skill_files (id, skill_id, path, content, created_at, updated_at)
         VALUES (?, ?, ?, ?, ?, ?)
         ON CONFLICT(skill_id, path) DO UPDATE SET content = excluded.content, updated_at = excluded.updated_at`,
		id, skillID, req.Path, req.Content, ts, ts,
	)
	var f SkillFile
	h.db.QueryRowContext(r.Context(),
		`SELECT id, skill_id, path, content, created_at, updated_at FROM skill_files WHERE skill_id = ? AND path = ?`,
		skillID, req.Path,
	).Scan(&f.ID, &f.SkillID, &f.Path, &f.Content, &f.CreatedAt, &f.UpdatedAt)
	writeJSON(w, http.StatusOK, f)
}

func (h *Handler) handleDeleteSkillFile(w http.ResponseWriter, r *http.Request) {
	fileID := chi.URLParam(r, "fileId")
	h.db.ExecContext(r.Context(), `DELETE FROM skill_files WHERE id = ?`, fileID)
	w.WriteHeader(http.StatusNoContent)
}
