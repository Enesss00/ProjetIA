package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ghostnet/internal/protocol"

	"github.com/coder/websocket"
)

// TestLiveWS exercises the full server over a real WebSocket: handshake, new
// game, snapshot, a deliberately malformed message (ANSI + NUL injection) that
// must not crash the server, valid and invalid commands, and delivery of a
// delta.
func TestLiveWS(t *testing.T) {
	hub := NewHub(Options{Build: "test"})
	srv := httptest.NewServer(hub.Handler())
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	defer func() { _ = c.Close(websocket.StatusNormalClosure, "") }()

	read := func() protocol.Envelope {
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var e protocol.Envelope
		_ = json.Unmarshal(data, &e)
		return e
	}
	write := func(typ string, body any) {
		msg, _ := protocol.Encode(typ, body)
		if err := c.Write(ctx, websocket.MessageText, msg); err != nil {
			t.Fatal(err)
		}
	}

	if e := read(); e.T != protocol.SHello {
		t.Fatalf("want hello, got %s", e.T)
	}
	write(protocol.CNew, protocol.ClientNew{Seed: 7, Profile: "apt"})
	if e := read(); e.T != protocol.SSnapshot {
		t.Fatalf("want snapshot, got %s", e.T)
	}
	// Malformed command (ANSI + NUL) must be tolerated, not fatal.
	_ = c.Write(ctx, websocket.MessageText, []byte(`{"v":1,"t":"cmd","b":{"id":1,"line":"\u001b[31mstatus\u0000"}}`))
	write(protocol.CCmd, protocol.ClientCmd{ID: 2, Line: "scan net"})
	write(protocol.CCmd, protocol.ClientCmd{ID: 3, Line: "boguscommand!!"})

	deadline := time.After(3 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("no delta received")
		default:
		}
		if read().T == protocol.SDelta {
			return
		}
	}
}
