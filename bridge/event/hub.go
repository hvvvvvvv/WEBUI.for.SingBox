package event

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

type SessionValidator interface {
	ValidateSession(token string) bool
	ValidateSessionWithoutTouch(token string) bool
}

type callback struct {
	id string
	fn func(data ...any)
}

type Hub struct {
	sessions SessionValidator
	upgrader websocket.Upgrader

	mu        sync.RWMutex
	clients   map[*websocket.Conn]string
	listeners map[string][]callback
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
		clients:   make(map[*websocket.Conn]string),
		listeners: make(map[string][]callback),
	}
}

func (h *Hub) ServeWebSocket(w http.ResponseWriter, r *http.Request, token string) {
	if !h.sessions.ValidateSession(token) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	h.mu.Lock()
	h.clients[conn] = token
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.clients, conn)
		h.mu.Unlock()
		_ = conn.Close()
	}()

	for {
		_, payload, err := conn.ReadMessage()
		if err != nil || !h.sessions.ValidateSession(token) {
			return
		}
		var incoming message
		if json.Unmarshal(payload, &incoming) == nil {
			h.dispatch(incoming.Event, incoming.Data...)
		}
	}
}

func (h *Hub) Publish(eventName string, data ...any) {
	payload, _ := json.Marshal(message{Event: eventName, Data: data})

	h.mu.RLock()
	clients := make(map[*websocket.Conn]string, len(h.clients))
	for conn, token := range h.clients {
		clients[conn] = token
	}
	h.mu.RUnlock()

	for conn, token := range clients {
		if !h.sessions.ValidateSessionWithoutTouch(token) {
			h.mu.Lock()
			delete(h.clients, conn)
			h.mu.Unlock()
			_ = conn.Close()
			continue
		}
		_ = conn.WriteMessage(websocket.TextMessage, payload)
	}
}

func (h *Hub) Subscribe(eventName, id string, fn func(data ...any)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.listeners[eventName] = append(h.listeners[eventName], callback{id: id, fn: fn})
}

func (h *Hub) Unsubscribe(eventName, id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	callbacks := h.listeners[eventName]
	for index, item := range callbacks {
		if item.id == id {
			h.listeners[eventName] = append(callbacks[:index], callbacks[index+1:]...)
			break
		}
	}
	if len(h.listeners[eventName]) == 0 {
		delete(h.listeners, eventName)
	}
}

func (h *Hub) dispatch(eventName string, data ...any) {
	h.mu.RLock()
	callbacks := append([]callback(nil), h.listeners[eventName]...)
	h.mu.RUnlock()
	for _, item := range callbacks {
		item.fn(data...)
	}
}

func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for conn := range h.clients {
		_ = conn.Close()
	}
	clear(h.clients)
	clear(h.listeners)
}
