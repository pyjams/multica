package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// activeTasks tracks running tasks so they can be cancelled.
var activeTasks = struct {
	sync.Mutex
	m map[string]context.CancelFunc
}{m: make(map[string]context.CancelFunc)}

func registerTask(id string, cancel context.CancelFunc) {
	activeTasks.Lock()
	activeTasks.m[id] = cancel
	activeTasks.Unlock()
}

func cancelTask(id string) {
	activeTasks.Lock()
	if fn, ok := activeTasks.m[id]; ok {
		fn()
		delete(activeTasks.m, id)
	}
	activeTasks.Unlock()
}

func deregisterTask(id string) {
	activeTasks.Lock()
	delete(activeTasks.m, id)
	activeTasks.Unlock()
}

// RunConfig holds the resolved execution parameters for a task.
type RunConfig struct {
	CLI         string            // "claude", "codex", "opencode", or a full path
	Cwd         string            // working directory
	Model       string            // model name (optional)
	MaxTurns    int               // 0 = default
	Env         map[string]string // extra env vars (merged with os.Environ)
	Timeout     time.Duration     // 0 = 20 minutes default
	SystemPrompt string
}

// resolveRunConfig extracts RunConfig from an agent's runtime_config JSON.
// Falls back to environment variables for CLI path and API keys.
func resolveRunConfig(runtimeConfigJSON string) RunConfig {
	cfg := RunConfig{
		CLI:     "claude",
		Timeout: 20 * time.Minute,
	}

	var rc map[string]interface{}
	if err := json.Unmarshal([]byte(runtimeConfigJSON), &rc); err == nil {
		if v, ok := rc["cli"].(string); ok && v != "" {
			cfg.CLI = v
		}
		if v, ok := rc["cwd"].(string); ok && v != "" {
			cfg.Cwd = v
		}
		if v, ok := rc["model"].(string); ok && v != "" {
			cfg.Model = v
		}
		if v, ok := rc["max_turns"].(float64); ok && v > 0 {
			cfg.MaxTurns = int(v)
		}
		if v, ok := rc["system_prompt"].(string); ok && v != "" {
			cfg.SystemPrompt = v
		}
		if v, ok := rc["timeout_minutes"].(float64); ok && v > 0 {
			cfg.Timeout = time.Duration(v) * time.Minute
		}
		if envMap, ok := rc["env"].(map[string]interface{}); ok {
			cfg.Env = make(map[string]string)
			for k, val := range envMap {
				if s, ok := val.(string); ok {
					cfg.Env[k] = s
				}
			}
		}
	}

	// Override CLI path from env vars if set
	switch strings.ToLower(cfg.CLI) {
	case "claude":
		if p := os.Getenv("CLAUDE_PATH"); p != "" {
			cfg.CLI = p
		}
	case "codex":
		if p := os.Getenv("CODEX_PATH"); p != "" {
			cfg.CLI = p
		}
	case "opencode":
		if p := os.Getenv("OPENCODE_PATH"); p != "" {
			cfg.CLI = p
		}
	}

	// If CLI is not an absolute path, resolve via PATH
	if !strings.ContainsAny(cfg.CLI, "/\\") {
		if resolved, err := exec.LookPath(cfg.CLI); err == nil {
			cfg.CLI = resolved
		}
	}

	return cfg
}

// buildPrompt constructs the prompt sent to the CLI from issue + agent context.
func buildPrompt(issueTitle, issueDesc, agentInstructions string) string {
	var sb strings.Builder

	if agentInstructions != "" {
		sb.WriteString("## Agent Instructions\n\n")
		sb.WriteString(agentInstructions)
		sb.WriteString("\n\n---\n\n")
	}

	sb.WriteString("## Task\n\n")
	sb.WriteString(issueTitle)

	if issueDesc != "" {
		sb.WriteString("\n\n")
		sb.WriteString(issueDesc)
	}

	return sb.String()
}

// executeTask runs the CLI for the given task, updating the DB as it progresses.
// It runs in a goroutine and returns immediately.
func (h *Handler) executeTask(taskID, agentID, issueID, workspaceID string) {
	ctx, cancel := context.WithCancel(context.Background())
	registerTask(taskID, cancel)

	go func() {
		defer deregisterTask(taskID)
		defer cancel()

		err := h.runTask(ctx, taskID, agentID, issueID, workspaceID)
		if err != nil {
			slog.Error("task execution failed", "task_id", taskID, "err", err)
			ts := now()
			h.db.Exec(
				`UPDATE agent_tasks SET status = 'failed', completed_at = ?, error = ? WHERE id = ?`,
				ts, err.Error(), taskID,
			)
			h.db.Exec(`UPDATE agents SET status = 'idle', updated_at = ? WHERE id = ?`, ts, agentID)
			h.broadcastEvent(workspaceID, "task_updated", map[string]interface{}{
				"task_id": taskID, "status": "failed",
			})
		}
	}()
}

func (h *Handler) runTask(ctx context.Context, taskID, agentID, issueID, workspaceID string) error {
	// Load agent
	var agentName, agentInstructions, runtimeConfigJSON string
	err := h.db.QueryRowContext(ctx,
		`SELECT name, instructions, runtime_config FROM agents WHERE id = ?`, agentID,
	).Scan(&agentName, &agentInstructions, &runtimeConfigJSON)
	if err != nil {
		return fmt.Errorf("load agent: %w", err)
	}

	// Load issue
	var issueTitle string
	var issueDesc sql.NullString
	err = h.db.QueryRowContext(ctx,
		`SELECT title, description FROM issues WHERE id = ?`, issueID,
	).Scan(&issueTitle, &issueDesc)
	if err != nil {
		return fmt.Errorf("load issue: %w", err)
	}

	cfg := resolveRunConfig(runtimeConfigJSON)
	prompt := buildPrompt(issueTitle, issueDesc.String, agentInstructions)

	// Mark task as running
	ts := now()
	h.db.Exec(`UPDATE agent_tasks SET status = 'running', started_at = ? WHERE id = ?`, ts, taskID)
	h.db.Exec(`UPDATE agents SET status = 'working', updated_at = ? WHERE id = ?`, ts, agentID)
	h.broadcastEvent(workspaceID, "task_updated", map[string]interface{}{
		"task_id": taskID, "status": "running",
	})

	slog.Info("executing task", "task_id", taskID, "cli", cfg.CLI, "cwd", cfg.Cwd)

	// Determine which backend to use
	cliName := strings.ToLower(filepath.Base(cfg.CLI))
	// Remove .exe suffix for comparison on Windows
	cliName = strings.TrimSuffix(cliName, ".exe")

	var finalStatus, finalOutput, finalError string

	switch cliName {
	case "claude":
		finalStatus, finalOutput, finalError = h.runClaude(ctx, taskID, workspaceID, cfg, prompt)
	default:
		finalStatus, finalOutput, finalError = h.runGenericCLI(ctx, taskID, workspaceID, cfg, prompt)
	}

	// Update task status
	ts = now()
	if finalError != "" {
		h.db.Exec(`UPDATE agent_tasks SET status = ?, completed_at = ?, error = ?, result = ? WHERE id = ?`,
			finalStatus, ts, finalError, `"`+escapeJSON(finalOutput)+`"`, taskID)
	} else {
		h.db.Exec(`UPDATE agent_tasks SET status = ?, completed_at = ?, result = ? WHERE id = ?`,
			finalStatus, ts, `"`+escapeJSON(finalOutput)+`"`, taskID)
	}

	h.db.Exec(`UPDATE agents SET status = 'idle', updated_at = ? WHERE id = ?`, ts, agentID)
	h.broadcastEvent(workspaceID, "task_updated", map[string]interface{}{
		"task_id": taskID, "status": finalStatus,
	})

	return nil
}

// runClaude executes the Claude Code CLI with stream-json output.
func (h *Handler) runClaude(ctx context.Context, taskID, workspaceID string, cfg RunConfig, prompt string) (status, output, errStr string) {
	runCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--verbose",
		"--permission-mode", "bypassPermissions",
	}
	if cfg.Model != "" {
		args = append(args, "--model", cfg.Model)
	}
	if cfg.MaxTurns > 0 {
		args = append(args, "--max-turns", fmt.Sprintf("%d", cfg.MaxTurns))
	}
	if cfg.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", cfg.SystemPrompt)
	}

	cmd := exec.CommandContext(runCtx, cfg.CLI, args...)
	if cfg.Cwd != "" {
		cmd.Dir = cfg.Cwd
	}
	cmd.Env = mergeEnv(cfg.Env)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "failed", "", "stdin pipe: " + err.Error()
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "failed", "", "stdout pipe: " + err.Error()
	}
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return "failed", "", "start claude: " + err.Error()
	}

	// Write prompt as stream-json input
	input := map[string]interface{}{
		"type": "user",
		"message": map[string]interface{}{
			"role": "user",
			"content": []map[string]string{
				{"type": "text", "text": prompt},
			},
		},
	}
	inputJSON, _ := json.Marshal(input)
	fmt.Fprintf(stdin, "%s\n", inputJSON)
	stdin.Close()

	var outputBuf strings.Builder
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 512*1024), 10*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var msg map[string]interface{}
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}

		msgType, _ := msg["type"].(string)

		switch msgType {
		case "assistant":
			text := extractClaudeText(msg)
			if text != "" {
				outputBuf.WriteString(text)
				h.saveTaskMessage(taskID, "assistant", text)
				h.broadcastEvent(workspaceID, "task_message", map[string]interface{}{
					"task_id": taskID, "role": "assistant", "content": text,
				})
			}
		case "result":
			if resultText, ok := msg["result"].(string); ok && resultText != "" {
				outputBuf.Reset()
				outputBuf.WriteString(resultText)
			}
		}
	}

	exitErr := cmd.Wait()
	if runCtx.Err() == context.DeadlineExceeded {
		return "failed", outputBuf.String(), fmt.Sprintf("timed out after %s", cfg.Timeout)
	}
	if runCtx.Err() == context.Canceled {
		return "cancelled", outputBuf.String(), "cancelled by user"
	}
	if exitErr != nil {
		return "failed", outputBuf.String(), exitErr.Error()
	}
	return "completed", outputBuf.String(), ""
}

// runGenericCLI runs any CLI tool, passing the prompt via stdin and capturing stdout.
func (h *Handler) runGenericCLI(ctx context.Context, taskID, workspaceID string, cfg RunConfig, prompt string) (status, output, errStr string) {
	runCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, cfg.CLI)
	if cfg.Cwd != "" {
		cmd.Dir = cfg.Cwd
	}
	cmd.Env = mergeEnv(cfg.Env)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "failed", "", "stdin pipe: " + err.Error()
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "failed", "", "stdout pipe: " + err.Error()
	}
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return "failed", "", "start " + cfg.CLI + ": " + err.Error()
	}

	io.WriteString(stdin, prompt)
	stdin.Close()

	var outputBuf strings.Builder
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 512*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		outputBuf.WriteString(line + "\n")
		h.saveTaskMessage(taskID, "assistant", line)
		h.broadcastEvent(workspaceID, "task_message", map[string]interface{}{
			"task_id": taskID, "role": "assistant", "content": line,
		})
	}

	exitErr := cmd.Wait()
	if runCtx.Err() == context.DeadlineExceeded {
		return "failed", outputBuf.String(), fmt.Sprintf("timed out after %s", cfg.Timeout)
	}
	if runCtx.Err() == context.Canceled {
		return "cancelled", outputBuf.String(), "cancelled by user"
	}
	if exitErr != nil {
		stderr := stderrBuf.String()
		if stderr != "" {
			return "failed", outputBuf.String(), exitErr.Error() + ": " + stderr
		}
		return "failed", outputBuf.String(), exitErr.Error()
	}
	return "completed", outputBuf.String(), ""
}

func (h *Handler) saveTaskMessage(taskID, role, content string) {
	h.db.Exec(
		`INSERT INTO task_messages (id, task_id, role, content, metadata, created_at) VALUES (?, ?, ?, ?, '{}', ?)`,
		newID(), taskID, role, content, now(),
	)
}

// extractClaudeText pulls text content from a Claude stream-json assistant message.
func extractClaudeText(msg map[string]interface{}) string {
	msgData, ok := msg["message"].(map[string]interface{})
	if !ok {
		return ""
	}
	content, ok := msgData["content"].([]interface{})
	if !ok {
		return ""
	}
	var sb strings.Builder
	for _, c := range content {
		block, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		if block["type"] == "text" {
			if text, ok := block["text"].(string); ok {
				sb.WriteString(text)
			}
		}
	}
	return sb.String()
}

// mergeEnv merges extra vars into the current process environment.
// Filters out vars that could interfere with child processes.
func mergeEnv(extra map[string]string) []string {
	base := os.Environ()
	env := make([]string, 0, len(base)+len(extra))
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		// Don't forward Electron/Node-specific vars to the CLI
		if strings.HasPrefix(key, "ELECTRON_") ||
			strings.HasPrefix(key, "CLAUDECODE_") ||
			strings.HasPrefix(key, "CLAUDE_CODE_") {
			continue
		}
		env = append(env, entry)
	}
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}

func escapeJSON(s string) string {
	b, _ := json.Marshal(s)
	// json.Marshal wraps in quotes — strip them
	if len(b) >= 2 {
		return string(b[1 : len(b)-1])
	}
	return s
}
