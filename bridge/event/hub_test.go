package event

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type testSessions struct {
	valid atomic.Bool
}

func newTestSessions() *testSessions {
	sessions := &testSessions{}
	sessions.valid.Store(true)
	return sessions
}

func (s *testSessions) ValidateSession(string) bool {
	return s.valid.Load()
}

func (s *testSessions) ValidateSessionWithoutTouch(string) bool {
	return s.valid.Load()
}

func openTestWebSocket(t *testing.T, hub *Hub) *websocket.Conn {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.ServeWebSocket(w, r, "token")
	}))
	t.Cleanup(server.Close)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func waitForClients(t *testing.T, hub *Hub, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		hub.mu.RLock()
		count := len(hub.clients)
		hub.mu.RUnlock()
		if count == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("client count did not become %d", want)
}

func TestHubPublishesThroughSingleClientWriter(t *testing.T) {
	hub := NewHub(newTestSessions())
	t.Cleanup(hub.Close)
	conn := openTestWebSocket(t, hub)
	waitForClients(t, hub, 1)

	hub.Publish("resourceChanged", map[string]any{"stateRevision": 2})
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var received message
	if err := json.Unmarshal(payload, &received); err != nil {
		t.Fatal(err)
	}
	if received.Event != "resourceChanged" || len(received.Data) != 1 {
		t.Fatalf("received message = %#v", received)
	}
}

func TestHubQueueOverflowDisconnectsClient(t *testing.T) {
	hub := NewHub(newTestSessions())
	client := &client{
		hub:   hub,
		token: "token",
		send:  make(chan []byte, 1),
		done:  make(chan struct{}),
	}
	if !hub.addClient(client) {
		t.Fatal("failed to add client")
	}

	hub.Publish("first")
	hub.Publish("overflow")
	waitForClients(t, hub, 0)
	select {
	case <-client.done:
	default:
		t.Fatal("overflowed client was not closed")
	}
}

func TestHubInvalidSessionDisconnectsClient(t *testing.T) {
	sessions := newTestSessions()
	hub := NewHub(sessions)
	t.Cleanup(hub.Close)
	conn := openTestWebSocket(t, hub)
	waitForClients(t, hub, 1)

	sessions.valid.Store(false)
	hub.Publish("resourceChanged")
	waitForClients(t, hub, 0)
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("invalid session connection remained open")
	}
}

func TestHubConcurrentPublishAndClose(t *testing.T) {
	hub := NewHub(newTestSessions())
	for index := 0; index < 32; index++ {
		if !hub.addClient(&client{
			hub:   hub,
			token: "token",
			send:  make(chan []byte, clientSendQueueSize),
			done:  make(chan struct{}),
		}) {
			t.Fatal("failed to add client")
		}
	}

	var wait sync.WaitGroup
	for index := 0; index < 16; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			for sequence := 0; sequence < 100; sequence++ {
				hub.Publish("event", index, sequence)
			}
		}(index)
	}
	wait.Add(1)
	go func() {
		defer wait.Done()
		hub.Close()
	}()
	wait.Wait()
	hub.Close()

	hub.mu.RLock()
	defer hub.mu.RUnlock()
	if len(hub.clients) != 0 || !hub.closed {
		t.Fatalf("hub was not fully closed: clients=%d closed=%v", len(hub.clients), hub.closed)
	}
}
