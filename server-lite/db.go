package main

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

func openDB(dataDir string) (*sql.DB, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	dbPath := filepath.Join(dataDir, "multica-lite.db")
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schemaSQL); err != nil {
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	return db, nil
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func newID() string {
	return uuid.New().String()
}

// seedDefaultData creates the initial workspace and user if they don't exist.
func seedDefaultData(db *sql.DB) (userID, workspaceID string, err error) {
	var count int
	if err = db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		return
	}

	if count > 0 {
		if err = db.QueryRow("SELECT id FROM users LIMIT 1").Scan(&userID); err != nil {
			return
		}
		if err = db.QueryRow("SELECT id FROM workspaces LIMIT 1").Scan(&workspaceID); err != nil {
			return
		}
		return
	}

	userID = newID()
	workspaceID = newID()
	memberID := newID()
	ts := now()

	_, err = db.Exec(
		`INSERT INTO users (id, name, email, avatar_url, created_at, updated_at) VALUES (?, ?, ?, NULL, ?, ?)`,
		userID, "Local User", "local@multica.local", ts, ts,
	)
	if err != nil {
		return
	}

	_, err = db.Exec(
		`INSERT INTO workspaces (id, name, slug, description, context, settings, repos, issue_prefix, issue_counter, created_at, updated_at)
         VALUES (?, ?, ?, NULL, NULL, '{}', '[]', ?, 0, ?, ?)`,
		workspaceID, "My Workspace", "my-workspace", "MUL", ts, ts,
	)
	if err != nil {
		return
	}

	_, err = db.Exec(
		`INSERT INTO workspace_members (id, workspace_id, user_id, role, created_at) VALUES (?, ?, ?, 'owner', ?)`,
		memberID, workspaceID, userID, ts,
	)
	return
}
