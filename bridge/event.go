package bridge

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

var Hub = NewEventHub()

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type eventCallback struct {
	id string
	fn func(data ...any)
}

type EventHub struct {
	mu        sync.RWMutex
	clients   map[*websocket.Conn]bool
	listeners map[string][]eventCallback
}

func NewEventHub() *EventHub {
	return &EventHub{
		clients:   make(map[*websocket.Conn]bool),
		listeners: make(map[string][]eventCallback),
	}
}

type wsMessage struct {
	Event string `json:"event"`
	Data  []any  `json:"data"`
}

func (h *EventHub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	h.mu.Lock()
	h.clients[conn] = true
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.clients, conn)
		h.mu.Unlock()
		conn.Close()
	}()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var msg wsMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			continue
		}
		// Client sent an event — dispatch to Go-side listeners
		h.dispatch(msg.Event, msg.Data...)
	}
}

// Emit sends an event to all connected WebSocket clients (Go → Frontend)
func (h *EventHub) Emit(event string, data ...any) {
	msg, _ := json.Marshal(wsMessage{Event: event, Data: data})
	h.mu.RLock()
	for conn := range h.clients {
		conn.WriteMessage(websocket.TextMessage, msg)
	}
	h.mu.RUnlock()
}

// On registers a Go-side listener for events from the frontend (Frontend → Go)
func (h *EventHub) On(event string, id string, fn func(data ...any)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.listeners[event] = append(h.listeners[event], eventCallback{id: id, fn: fn})
}

// Off removes a Go-side listener by event name and id
func (h *EventHub) Off(event string, id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	cbs := h.listeners[event]
	for i, cb := range cbs {
		if cb.id == id {
			h.listeners[event] = append(cbs[:i], cbs[i+1:]...)
			break
		}
	}
	if len(h.listeners[event]) == 0 {
		delete(h.listeners, event)
	}
}

func (h *EventHub) dispatch(event string, data ...any) {
	h.mu.RLock()
	cbs := make([]eventCallback, len(h.listeners[event]))
	copy(cbs, h.listeners[event])
	h.mu.RUnlock()

	for _, cb := range cbs {
		cb.fn(data...)
	}
}
