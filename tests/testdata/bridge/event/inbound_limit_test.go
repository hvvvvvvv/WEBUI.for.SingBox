package event

import (
	"bytes"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestHubRejectsOversizedInboundMessage(t *testing.T) {
	hub := NewHub(newTestSessions())
	t.Cleanup(hub.Close)
	connection := openTestWebSocket(t, hub)
	waitForClients(t, hub, 1)
	if err := connection.WriteMessage(websocket.TextMessage, bytes.Repeat([]byte("x"), 4097)); err != nil {
		t.Fatal(err)
	}
	_ = connection.SetReadDeadline(time.Now().Add(time.Second))
	_, _, err := connection.ReadMessage()
	if !websocket.IsCloseError(err, websocket.CloseMessageTooBig) {
		t.Fatalf("oversized data should close with 1009, got %v", err)
	}
	waitForClients(t, hub, 0)
}
