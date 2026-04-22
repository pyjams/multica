package main

import (
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) handleListComments(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	rows, err := h.db.QueryContext(r.Context(),
		`SELECT id, issue_id, creator_type, creator_id, content, type, parent_id, created_at, updated_at
         FROM comments WHERE issue_id = ? ORDER BY created_at ASC`, issueID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	comments := []Comment{}
	for rows.Next() {
		c := scanComment(rows)
		if c != nil {
			comments = append(comments, *c)
		}
	}
	writeJSON(w, http.StatusOK, comments)
}

func (h *Handler) handleCreateComment(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	uid := userIDFromCtx(r)

	var req struct {
		Content  string  `json:"content"`
		Type     string  `json:"type"`
		ParentID *string `json:"parent_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.Type == "" {
		req.Type = "comment"
	}

	id := newID()
	ts := now()
	if _, err := h.db.ExecContext(r.Context(),
		`INSERT INTO comments (id, issue_id, creator_type, creator_id, content, type, parent_id, created_at, updated_at)
         VALUES (?, ?, 'member', ?, ?, ?, ?, ?, ?)`,
		id, issueID, uid, req.Content, req.Type, req.ParentID, ts, ts,
	); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	c, _ := h.getComment(r, id)
	writeJSON(w, http.StatusCreated, c)
}

func (h *Handler) handleUpdateComment(w http.ResponseWriter, r *http.Request) {
	commentID := chi.URLParam(r, "commentId")
	var req struct {
		Content string `json:"content"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	ts := now()
	h.db.ExecContext(r.Context(),
		`UPDATE comments SET content = ?, updated_at = ? WHERE id = ?`, req.Content, ts, commentID,
	)

	c, err := h.getComment(r, commentID)
	if err != nil {
		writeError(w, http.StatusNotFound, "comment not found")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (h *Handler) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	commentID := chi.URLParam(r, "commentId")
	h.db.ExecContext(r.Context(), `DELETE FROM comments WHERE id = ?`, commentID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) getComment(r *http.Request, id string) (*Comment, error) {
	row := h.db.QueryRowContext(r.Context(),
		`SELECT id, issue_id, creator_type, creator_id, content, type, parent_id, created_at, updated_at
         FROM comments WHERE id = ?`, id,
	)
	c := scanComment(row)
	if c == nil {
		return nil, sql.ErrNoRows
	}
	return c, nil
}

func scanComment(row rowScanner) *Comment {
	var c Comment
	var parentID sql.NullString
	err := row.Scan(&c.ID, &c.IssueID, &c.CreatorType, &c.CreatorID,
		&c.Content, &c.Type, &parentID, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil
	}
	c.ParentID = nullStr(parentID)
	return &c
}

func (h *Handler) handleAddReaction(w http.ResponseWriter, r *http.Request) {
	commentID := chi.URLParam(r, "commentId")
	var req struct {
		Emoji string `json:"emoji"`
	}
	decodeJSON(r, &req)
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id": newID(), "comment_id": commentID, "emoji": req.Emoji,
		"actor_type": "member", "actor_id": userIDFromCtx(r),
		"created_at": now(),
	})
}

func (h *Handler) handleRemoveReaction(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}
