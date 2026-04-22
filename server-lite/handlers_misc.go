package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// --- Inbox ---

func (h *Handler) handleListInbox(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromCtx(r)
	wsID := h.workspaceID(r)
	rows, err := h.db.QueryContext(r.Context(),
		`SELECT id, workspace_id, user_id, type, read, archived, reference_id, reference_type, title, body, created_at
         FROM inbox_items WHERE workspace_id = ? AND user_id = ? AND archived = 0
         ORDER BY created_at DESC`, wsID, uid,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	items := []InboxItem{}
	for rows.Next() {
		var item InboxItem
		var refID, refType sql.NullString
		var read, archived int
		rows.Scan(&item.ID, &item.WorkspaceID, &item.UserID, &item.Type,
			&read, &archived, &refID, &refType, &item.Title, &item.Body, &item.CreatedAt)
		item.Read = read == 1
		item.Archived = archived == 1
		item.ReferenceID = nullStr(refID)
		item.ReferenceType = nullStr(refType)
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handler) handleCountUnreadInbox(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromCtx(r)
	wsID := h.workspaceID(r)
	var count int
	h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM inbox_items WHERE workspace_id = ? AND user_id = ? AND read = 0 AND archived = 0`,
		wsID, uid,
	).Scan(&count)
	writeJSON(w, http.StatusOK, map[string]interface{}{"count": count})
}

func (h *Handler) handleMarkInboxRead(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	h.db.ExecContext(r.Context(), `UPDATE inbox_items SET read = 1 WHERE id = ?`, id)
	var item InboxItem
	h.db.QueryRowContext(r.Context(),
		`SELECT id, workspace_id, user_id, type, read, archived, reference_id, reference_type, title, body, created_at
         FROM inbox_items WHERE id = ?`, id,
	).Scan(&item.ID, &item.WorkspaceID, &item.UserID, &item.Type,
		new(int), new(int), new(*string), new(*string), &item.Title, &item.Body, &item.CreatedAt)
	item.Read = true
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) handleArchiveInbox(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	h.db.ExecContext(r.Context(), `UPDATE inbox_items SET archived = 1 WHERE id = ?`, id)
	writeJSON(w, http.StatusOK, map[string]interface{}{"id": id, "archived": true})
}

func (h *Handler) handleMarkAllInboxRead(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromCtx(r)
	wsID := h.workspaceID(r)
	result, _ := h.db.ExecContext(r.Context(),
		`UPDATE inbox_items SET read = 1 WHERE workspace_id = ? AND user_id = ? AND read = 0`,
		wsID, uid,
	)
	n, _ := result.RowsAffected()
	writeJSON(w, http.StatusOK, map[string]interface{}{"count": n})
}

func (h *Handler) handleArchiveAllInbox(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromCtx(r)
	wsID := h.workspaceID(r)
	result, _ := h.db.ExecContext(r.Context(),
		`UPDATE inbox_items SET archived = 1 WHERE workspace_id = ? AND user_id = ?`, wsID, uid,
	)
	n, _ := result.RowsAffected()
	writeJSON(w, http.StatusOK, map[string]interface{}{"count": n})
}

func (h *Handler) handleArchiveAllReadInbox(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromCtx(r)
	wsID := h.workspaceID(r)
	result, _ := h.db.ExecContext(r.Context(),
		`UPDATE inbox_items SET archived = 1 WHERE workspace_id = ? AND user_id = ? AND read = 1`,
		wsID, uid,
	)
	n, _ := result.RowsAffected()
	writeJSON(w, http.StatusOK, map[string]interface{}{"count": n})
}

func (h *Handler) handleArchiveCompletedInbox(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromCtx(r)
	wsID := h.workspaceID(r)
	result, _ := h.db.ExecContext(r.Context(),
		`UPDATE inbox_items SET archived = 1 WHERE workspace_id = ? AND user_id = ? AND type = 'task_completed'`,
		wsID, uid,
	)
	n, _ := result.RowsAffected()
	writeJSON(w, http.StatusOK, map[string]interface{}{"count": n})
}

// --- Pins ---

func (h *Handler) handleListPins(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromCtx(r)
	wsID := h.workspaceID(r)
	rows, err := h.db.QueryContext(r.Context(),
		`SELECT id, workspace_id, user_id, item_type, item_id, position, created_at
         FROM pins WHERE workspace_id = ? AND user_id = ? ORDER BY position ASC`, wsID, uid,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	pins := []PinnedItem{}
	for rows.Next() {
		var p PinnedItem
		rows.Scan(&p.ID, &p.WorkspaceID, &p.UserID, &p.ItemType, &p.ItemID, &p.Position, &p.CreatedAt)
		pins = append(pins, p)
	}
	writeJSON(w, http.StatusOK, pins)
}

func (h *Handler) handleCreatePin(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromCtx(r)
	wsID := h.workspaceID(r)
	var req struct {
		ItemType string `json:"item_type"`
		ItemID   string `json:"item_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	var maxPos int
	h.db.QueryRowContext(r.Context(),
		`SELECT COALESCE(MAX(position), 0) FROM pins WHERE workspace_id = ? AND user_id = ?`,
		wsID, uid,
	).Scan(&maxPos)

	id := newID()
	ts := now()
	h.db.ExecContext(r.Context(),
		`INSERT OR IGNORE INTO pins (id, workspace_id, user_id, item_type, item_id, position, created_at)
         VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, wsID, uid, req.ItemType, req.ItemID, maxPos+1, ts,
	)

	var p PinnedItem
	h.db.QueryRowContext(r.Context(),
		`SELECT id, workspace_id, user_id, item_type, item_id, position, created_at
         FROM pins WHERE workspace_id = ? AND user_id = ? AND item_type = ? AND item_id = ?`,
		wsID, uid, req.ItemType, req.ItemID,
	).Scan(&p.ID, &p.WorkspaceID, &p.UserID, &p.ItemType, &p.ItemID, &p.Position, &p.CreatedAt)
	writeJSON(w, http.StatusCreated, p)
}

func (h *Handler) handleDeletePin(w http.ResponseWriter, r *http.Request) {
	itemType := chi.URLParam(r, "itemType")
	itemID := chi.URLParam(r, "itemId")
	uid := userIDFromCtx(r)
	wsID := h.workspaceID(r)
	h.db.ExecContext(r.Context(),
		`DELETE FROM pins WHERE workspace_id = ? AND user_id = ? AND item_type = ? AND item_id = ?`,
		wsID, uid, itemType, itemID,
	)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleReorderPins(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Pins []struct {
			ID       string `json:"id"`
			Position int    `json:"position"`
		} `json:"pins"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	for _, p := range req.Pins {
		h.db.ExecContext(r.Context(), `UPDATE pins SET position = ? WHERE id = ?`, p.Position, p.ID)
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Personal Access Tokens ---

func (h *Handler) handleListTokens(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromCtx(r)
	rows, err := h.db.QueryContext(r.Context(),
		`SELECT id, user_id, name, last_used_at, created_at FROM personal_access_tokens WHERE user_id = ?`, uid,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	tokens := []PersonalAccessToken{}
	for rows.Next() {
		var t PersonalAccessToken
		var lastUsed sql.NullString
		rows.Scan(&t.ID, &t.UserID, &t.Name, &lastUsed, &t.CreatedAt)
		t.LastUsedAt = nullStr(lastUsed)
		tokens = append(tokens, t)
	}
	writeJSON(w, http.StatusOK, tokens)
}

func (h *Handler) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromCtx(r)
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	raw, err := generateToken("mul_")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}
	hash := hex.EncodeToString(func() []byte {
		h := sha256.Sum256([]byte(raw))
		return h[:]
	}())

	id := newID()
	ts := now()
	h.db.ExecContext(r.Context(),
		`INSERT INTO personal_access_tokens (id, user_id, name, token_hash, created_at)
         VALUES (?, ?, ?, ?, ?)`,
		id, uid, req.Name, hash, ts,
	)
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id": id, "user_id": uid, "name": req.Name,
		"token": raw, "created_at": ts,
	})
}

func (h *Handler) handleRevokeToken(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	h.db.ExecContext(r.Context(), `DELETE FROM personal_access_tokens WHERE id = ?`, id)
	w.WriteHeader(http.StatusNoContent)
}

// --- File upload stub ---

func (h *Handler) handleUploadFile(w http.ResponseWriter, r *http.Request) {
	// Lite mode: return a stub response
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":         newID(),
		"url":        "",
		"filename":   "",
		"size":       0,
		"created_at": now(),
	})
}

// --- Chat stubs ---

func (h *Handler) handleListChatSessions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []interface{}{})
}

func (h *Handler) handleCreateChatSession(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id": newID(), "status": "active", "created_at": now(),
	})
}

func (h *Handler) handleGetChatSession(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "not found")
}

func (h *Handler) handleArchiveChatSession(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleListChatMessages(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []interface{}{})
}

func (h *Handler) handleSendChatMessage(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "chat not supported in lite mode")
}
