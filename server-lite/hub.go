package main

import (
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
)

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type wsClient struct {
	hub         *Hub
	conn        *websocket.Conn
	send        chan []byte
	userID      string
	workspaceID string
}

type Hub struct {
	mu         sync.RWMutex
	rooms      map[string]map[*wsClient]bool
	register   chan *wsClient
	unregister chan *wsClient
	broadcast  chan broadcastMsg
}

type broadcastMsg struct {
	workspaceID string
	data        []byte
}

func newHub() *Hub {
	return &Hub{
		rooms:      make(map[string]map[*wsClient]bool),
		register:   make(chan *wsClient, 16),
		unregister: make(chan *wsClient, 16),
		broadcast:  make(chan broadcastMsg, 64),
	}
}

func (hub *Hub) Run() {
	for {
		select {
		case c := <-hub.register:
			hub.mu.Lock()
			if hub.rooms[c.workspaceID] == nil {
				hub.rooms[c.workspaceID] = make(map[*wsClient]bool)
			}
			hub.rooms[c.workspaceID][c] = true
			hub.mu.Unlock()

		case c := <-hub.unregister:
			hub.mu.Lock()
			if room, ok := hub.rooms[c.workspaceID]; ok {
				if _, ok := room[c]; ok {
					delete(room, c)
					close(c.send)
				}
			}
			hub.mu.Unlock()

		case msg := <-hub.broadcast:
			hub.mu.RLock()
			clients := hub.rooms[msg.workspaceID]
			for c := range clients {
				select {
				case c.send <- msg.data:
				default:
					// slow client – drop
				}
			}
			hub.mu.RUnlock()
		}
	}
}

func (hub *Hub) Broadcast(workspaceID string, data []byte) {
	hub.broadcast <- broadcastMsg{workspaceID: workspaceID, data: data}
}

func (c *wsClient) writePump() {
	defer c.conn.Close()
	for msg := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			break
		}
	}
}

func (c *wsClient) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

func (h *Handler) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	tokenStr := r.URL.Query().Get("token")
	workspaceID := r.URL.Query().Get("workspace_id")

	if tokenStr == "" || workspaceID == "" {
		http.Error(w, "missing token or workspace_id", http.StatusBadRequest)
		return
	}

	// Resolve user from token
	var userID string
	if strings.HasPrefix(tokenStr, "mul_") {
		hash := hashToken(tokenStr)
		if err := h.db.QueryRowContext(r.Context(),
			`SELECT user_id FROM personal_access_tokens WHERE token_hash = ?`, hash,
		).Scan(&userID); err != nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
	} else {
		token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
			return getJWTSecret(), nil
		})
		if err != nil || !token.Valid {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		userID, _ = claims["sub"].(string)
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("ws upgrade", "err", err)
		return
	}

	c := &wsClient{
		hub:         h.hub,
		conn:        conn,
		send:        make(chan []byte, 32),
		userID:      userID,
		workspaceID: workspaceID,
	}
	h.hub.register <- c
	go c.writePump()
	go c.readPump()
}
