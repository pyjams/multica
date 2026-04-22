package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const ctxUserID contextKey = "userID"

var (
	jwtSecret     []byte
	jwtSecretOnce sync.Once
)

func getJWTSecret() []byte {
	jwtSecretOnce.Do(func() {
		s := os.Getenv("JWT_SECRET")
		if s == "" {
			s = "multica-lite-local-secret"
		}
		jwtSecret = []byte(s)
	})
	return jwtSecret
}

func signToken(userID string) (string, error) {
	claims := jwt.MapClaims{
		"sub": userID,
		"exp": time.Now().Add(365 * 24 * time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(getJWTSecret())
}

func parseToken(tokenStr string) (string, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return getJWTSecret(), nil
	})
	if err != nil {
		return "", err
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return "", fmt.Errorf("invalid token")
	}
	sub, ok := claims["sub"].(string)
	if !ok {
		return "", fmt.Errorf("missing sub claim")
	}
	return sub, nil
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func generateToken(prefix string) (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b), nil
}

// authMiddleware validates JWT or PAT tokens and injects userID into context.
func (h *Handler) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			writeError(w, http.StatusUnauthorized, "missing authorization header")
			return
		}

		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		if tokenStr == authHeader {
			writeError(w, http.StatusUnauthorized, "invalid authorization format")
			return
		}

		var userID string

		if strings.HasPrefix(tokenStr, "mul_") {
			hash := hashToken(tokenStr)
			err := h.db.QueryRowContext(r.Context(),
				`SELECT user_id FROM personal_access_tokens WHERE token_hash = ?`, hash,
			).Scan(&userID)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "invalid token")
				return
			}
			// best-effort update last_used_at
			go h.db.Exec(`UPDATE personal_access_tokens SET last_used_at = ? WHERE token_hash = ?`, now(), hash)
		} else if strings.HasPrefix(tokenStr, "mdt_") {
			// daemon token: resolve to runtime owner's user
			err := h.db.QueryRowContext(r.Context(),
				`SELECT owner_id FROM agent_runtimes WHERE daemon_token = ?`, tokenStr,
			).Scan(&userID)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "invalid daemon token")
				return
			}
		} else {
			var err error
			userID, err = parseToken(tokenStr)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "invalid token")
				return
			}
		}

		ctx := context.WithValue(r.Context(), ctxUserID, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// daemonAuthMiddleware accepts daemon tokens OR regular JWT tokens.
func (h *Handler) daemonAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			writeError(w, http.StatusUnauthorized, "missing authorization header")
			return
		}

		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		if tokenStr == authHeader {
			writeError(w, http.StatusUnauthorized, "invalid authorization format")
			return
		}

		var userID string

		if strings.HasPrefix(tokenStr, "mdt_") {
			err := h.db.QueryRowContext(r.Context(),
				`SELECT owner_id FROM agent_runtimes WHERE daemon_token = ?`, tokenStr,
			).Scan(&userID)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "invalid daemon token")
				return
			}
		} else {
			var err error
			userID, err = parseToken(tokenStr)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "invalid token")
				return
			}
		}

		ctx := context.WithValue(r.Context(), ctxUserID, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func userIDFromCtx(r *http.Request) string {
	v, _ := r.Context().Value(ctxUserID).(string)
	return v
}

// --- Auth handlers ---

func (h *Handler) handleSendCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	// In lite mode: any code works. Just acknowledge.
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "Verification code sent (lite mode: use any 6-digit code)",
	})
}

func (h *Handler) handleVerifyCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	// In lite mode: ensure user exists for this email (create if not)
	var userID string
	err := h.db.QueryRowContext(r.Context(),
		`SELECT id FROM users WHERE email = ?`, req.Email,
	).Scan(&userID)
	if err != nil {
		// Create new user for this email
		userID = newID()
		name := strings.Split(req.Email, "@")[0]
		ts := now()
		if _, err := h.db.ExecContext(r.Context(),
			`INSERT INTO users (id, name, email, avatar_url, created_at, updated_at) VALUES (?, ?, ?, NULL, ?, ?)`,
			userID, name, req.Email, ts, ts,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create user")
			return
		}
		// Add to default workspace as owner
		ts2 := now()
		if _, err := h.db.ExecContext(r.Context(),
			`INSERT OR IGNORE INTO workspace_members (id, workspace_id, user_id, role, created_at) VALUES (?, ?, ?, 'owner', ?)`,
			newID(), h.defaultWorkspaceID, userID, ts2,
		); err != nil {
			// ignore - already member
		}
	}

	token, err := signToken(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to sign token")
		return
	}

	var user User
	h.db.QueryRowContext(r.Context(),
		`SELECT id, name, email, avatar_url, created_at, updated_at FROM users WHERE id = ?`, userID,
	).Scan(&user.ID, &user.Name, &user.Email, &user.AvatarURL, &user.CreatedAt, &user.UpdatedAt)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"token": token,
		"user":  user,
	})
}

// handleAutoLogin returns a token for the default local user (localhost-only).
func (h *Handler) handleAutoLogin(w http.ResponseWriter, r *http.Request) {
	// Only allow from localhost
	if !isLocalhost(r) {
		writeError(w, http.StatusForbidden, "auto-login only available from localhost")
		return
	}

	token, err := signToken(h.defaultUserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to sign token")
		return
	}

	var user User
	h.db.QueryRowContext(r.Context(),
		`SELECT id, name, email, avatar_url, created_at, updated_at FROM users WHERE id = ?`, h.defaultUserID,
	).Scan(&user.ID, &user.Name, &user.Email, &user.AvatarURL, &user.CreatedAt, &user.UpdatedAt)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"token": token,
		"user":  user,
	})
}

func isLocalhost(r *http.Request) bool {
	host := r.RemoteAddr
	return strings.HasPrefix(host, "127.") ||
		strings.HasPrefix(host, "[::1]") ||
		strings.HasPrefix(host, "::1") ||
		strings.Contains(host, "localhost")
}
