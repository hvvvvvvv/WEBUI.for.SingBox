package event

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"guiforcores/bridge/logging"

	"github.com/gorilla/websocket"
)

type SessionValidator interface {
	ValidateSession(token string) bool
	ValidateSessionWithoutTouch(token string) bool
}

const (
	clientSendQueueSize = 64
	writeTimeout        = 10 * time.Second
	pingInterval        = 30 * time.Second
	readTimeout         = 60 * time.Second
)

type client struct {
	hub       *Hub
	conn      *websocket.Conn
	token     string
	send      chan []byte
	done      chan struct{}
	closeOnce sync.Once
}

func (c *client) close() {
	c.closeOnce.Do(func() {
		close(c.done)
		if c.conn != nil {
			_ = c.conn.Close()
		}
	})
}

type Hub struct {
	sessions SessionValidator
	upgrader websocket.Upgrader

	mu      sync.RWMutex
	clients map[*client]struct{}
	closed  bool
}

type message struct {
	Event string `json:"event"`
	Data  []any  `json:"data"`
}

func NewHub(sessions SessionValidator) *Hub {
	return &Hub{
		sessions: sessions,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(*http.Request) bool { return true },
		},
		clients: make(map[*client]struct{}),
	}
}

func (h *Hub) ServeWebSocket(w http.ResponseWriter, r *http.Request, token string) {
	logger := logging.FromContext(r.Context()).With("component", "websocket", "remote_addr", r.RemoteAddr)
	if !h.sessions.ValidateSession(token) {
		logger.WarnContext(r.Context(), "websocket unauthorized", "operation", "connect", "result", "failure")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.ErrorContext(r.Context(), "websocket upgrade failed", "operation", "connect", "result", "failure", "error", err)
		return
	}
	logger.DebugContext(r.Context(), "websocket connected", "operation", "connect", "result", "success")

	client := &client{
		hub:   h,
		conn:  conn,
		token: token,
		send:  make(chan []byte, clientSendQueueSize),
		done:  make(chan struct{}),
	}
	if !h.addClient(client) {
		client.close()
		return
	}
	defer h.removeClient(client)
	defer logger.DebugContext(r.Context(), "websocket disconnected", "operation", "disconnect", "result", "success")
	go client.writeLoop()

	_ = conn.SetReadDeadline(time.Now().Add(readTimeout))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(readTimeout))
	})

	for {
		_, _, err := conn.ReadMessage()
		if err != nil || !h.sessions.ValidateSession(token) {
			return
		}
	}
}

func (h *Hub) Publish(eventName string, data ...any) {
	payload, err := json.Marshal(message{Event: eventName, Data: data})
	if err != nil {
		slog.Error("event serialization failed", "component", "websocket", "operation", "publish", "event", eventName, "result", "failure", "error", err)
		return
	}

	h.mu.RLock()
	clients := make([]*client, 0, len(h.clients))
	for client := range h.clients {
		clients = append(clients, client)
	}
	h.mu.RUnlock()
	slog.Debug("event published", "component", "websocket", "operation", "publish", "event", eventName, "client_count", len(clients), "result", "success")

	for _, client := range clients {
		if !h.sessions.ValidateSessionWithoutTouch(client.token) {
			h.removeClient(client)
			continue
		}
		select {
		case <-client.done:
		case client.send <- payload:
		default:
			h.removeClient(client)
		}
	}
}

func (h *Hub) addClient(client *client) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return false
	}
	h.clients[client] = struct{}{}
	return true
}

func (h *Hub) removeClient(client *client) {
	h.mu.Lock()
	delete(h.clients, client)
	h.mu.Unlock()
	client.close()
}

func (c *client) writeLoop() {
	ticker := time.NewTicker(pingInterval)
	defer func() {
		ticker.Stop()
		c.hub.removeClient(c)
	}()

	for {
		select {
		case payload := <-c.send:
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				return
			}
		case <-ticker.C:
			if !c.hub.sessions.ValidateSessionWithoutTouch(c.token) {
				return
			}
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
				return
			}
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-c.done:
			return
		}
	}
}

func (h *Hub) Close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	clients := make([]*client, 0, len(h.clients))
	for client := range h.clients {
		clients = append(clients, client)
	}
	clear(h.clients)
	h.mu.Unlock()

	for _, client := range clients {
		client.close()
	}
}
