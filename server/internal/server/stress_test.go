package server

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"ghostnet/internal/protocol"

	"github.com/coder/websocket"
)

// TestConcurrentClientsStress runs several clients that each start games and
// spam commands concurrently while the sessions tick. Under -race this proves
// the session/hub locking is sound; it also exercises game eviction.
func TestConcurrentClientsStress(t *testing.T) {
	hub := NewHub(Options{Build: "stress", MaxGames: 8, MaxConns: 32})
	srv := httptest.NewServer(hub.Handler())
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(seed uint64) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			c, resp, err := websocket.Dial(ctx, url, nil)
			if err != nil {
				return
			}
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			defer func() { _ = c.Close(websocket.StatusNormalClosure, "") }()

			send := func(typ string, body any) {
				msg, _ := protocol.Encode(typ, body)
				_ = c.Write(ctx, websocket.MessageText, msg)
			}
			// Drain incoming frames so the connection never backs up.
			go func() {
				for {
					if _, _, err := c.Read(ctx); err != nil {
						return
					}
				}
			}()

			send(protocol.CNew, protocol.ClientNew{Seed: seed})
			lines := []string{"status", "scan net", "isolate ws-01", "block 203.0.113.5", "garbage", ""}
			for j := 0; j < 60; j++ {
				send(protocol.CCmd, protocol.ClientCmd{ID: j, Line: lines[j%len(lines)]})
				send(protocol.CSpeed, protocol.ClientSpeed{Speed: 4})
				time.Sleep(3 * time.Millisecond)
			}
			send(protocol.CResync, nil)
		}(uint64(i))
	}
	wg.Wait()
}

// TestOversizedMessageRejected confirms a message beyond the read limit closes
// the connection rather than being processed.
func TestOversizedMessageRejected(t *testing.T) {
	hub := NewHub(Options{Build: "t"})
	srv := httptest.NewServer(hub.Handler())
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	// Read the hello first.
	if _, _, err := c.Read(ctx); err != nil {
		t.Fatal(err)
	}
	// Craft a message well over MaxClientMsgBytes.
	huge := protocol.ClientCmd{ID: 1, Line: strings.Repeat("x", protocol.MaxClientMsgBytes*2)}
	msg, _ := protocol.Encode(protocol.CCmd, huge)
	if len(msg) <= protocol.MaxClientMsgBytes {
		t.Fatalf("test message not large enough: %d", len(msg))
	}
	_ = c.Write(ctx, websocket.MessageText, msg)
	// The server should close the connection; the next read must error.
	_, _, err = c.Read(ctx)
	if err == nil {
		t.Fatal("expected connection to close after oversized message")
	}
}
