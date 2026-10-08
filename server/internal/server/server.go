// Package server hosts the GHOST NET game over WebSocket and serves the
// static web client. It is defensive by construction: bounded message sizes,
// per-connection rate limiting, a cap on concurrent connections and games,
// idle-game reclamation, and panic recovery so no client input can crash it.
package server

import (
	"context"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"ghostnet/internal/protocol"
	"ghostnet/internal/report"

	"github.com/coder/websocket"
)

// Options configure the server.
type Options struct {
	Build       string // build string reported in the hello handshake
	DBPath      string // directory for per-game SQLite files ("" = memory only)
	Reporter    report.Reporter
	LLM         report.Reporter // optional LLM report enricher; nil disables it
	MaxConns    int             // 0 = default
	MaxGames    int             // 0 = default
	GameTTL     time.Duration
	StaticDir   string   // directory of the built web client ("" = none)
	OriginHosts []string // allowed WS origin hosts ("" = same-origin only)
}

// Hub owns all live sessions and connection accounting.
type Hub struct {
	opt      Options
	mu       sync.Mutex
	games    []*Session
	connN    atomic.Int64
	reporter report.Reporter
	seq      atomic.Int64
}

// NewHub builds a hub.
func NewHub(opt Options) *Hub {
	if opt.MaxConns == 0 {
		opt.MaxConns = 256
	}
	if opt.MaxGames == 0 {
		opt.MaxGames = 128
	}
	if opt.GameTTL == 0 {
		opt.GameTTL = 30 * time.Minute
	}
	if opt.Reporter == nil {
		opt.Reporter = report.Template{}
	}
	h := &Hub{opt: opt, reporter: opt.Reporter}
	go h.janitor()
	return h
}

// Handler returns the HTTP handler (WS endpoint + static files + health).
func (h *Hub) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", h.serveWS)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	if h.opt.StaticDir != "" {
		fs := http.FileServer(http.Dir(h.opt.StaticDir))
		mux.Handle("/", noCache(fs))
	}
	return mux
}

func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}

func (h *Hub) serveWS(w http.ResponseWriter, r *http.Request) {
	if h.connN.Load() >= int64(h.opt.MaxConns) {
		http.Error(w, "server busy", http.StatusServiceUnavailable)
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: h.opt.OriginHosts,
	})
	if err != nil {
		return
	}
	h.connN.Add(1)
	defer h.connN.Add(-1)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	c := newConn(ws, h)
	// Handshake.
	if msg, err := protocol.Encode(protocol.SHello, protocol.Hello{Version: protocol.Version, Build: h.opt.Build}); err == nil {
		c.trySend(msg)
	}

	go c.writePump(ctx)
	c.readPump(ctx) // blocks until the client disconnects

	if c.sess != nil {
		c.sess.unsubscribe(c)
	}
	_ = ws.Close(websocket.StatusNormalClosure, "")
}

// startGame creates a new session, reclaiming the oldest if at capacity.
func (h *Hub) startGame(args *ClientNewArgs) (*Session, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.games) >= h.opt.MaxGames {
		// Evict the oldest finished game, else the oldest game outright.
		h.evictOldestLocked()
	}
	token := h.nextToken()
	dbPath := ""
	if h.opt.DBPath != "" {
		dbPath = h.opt.DBPath + "/game-" + token + ".db"
	}
	sess, err := newSession(token, args, dbPath, h.reporter, h.opt.LLM)
	if err != nil {
		return nil, err
	}
	h.games = append(h.games, sess)
	return sess, nil
}

func (h *Hub) evictOldestLocked() {
	if len(h.games) == 0 {
		return
	}
	idx := 0
	for i, g := range h.games {
		g.mu.Lock()
		ended := g.ended
		g.mu.Unlock()
		if ended {
			idx = i
			break
		}
	}
	victim := h.games[idx]
	h.games = append(h.games[:idx], h.games[idx+1:]...)
	go victim.close()
}

func (h *Hub) nextToken() string {
	n := h.seq.Add(1)
	const digits = "abcdefghijklmnopqrstuvwxyz0123456789"
	var buf [8]byte
	for i := range buf {
		buf[i] = digits[n%int64(len(digits))]
		n /= int64(len(digits))
	}
	return string(buf[:])
}

// janitor reclaims sessions with no subscribers past the TTL.
func (h *Hub) janitor() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for range t.C {
		h.mu.Lock()
		kept := h.games[:0]
		var dead []*Session
		for _, g := range h.games {
			g.mu.Lock()
			expired := !g.idleSince.IsZero() && time.Since(g.idleSince) > h.opt.GameTTL
			g.mu.Unlock()
			if expired {
				dead = append(dead, g)
			} else {
				kept = append(kept, g)
			}
		}
		h.games = kept
		h.mu.Unlock()
		for _, g := range dead {
			g.close()
		}
	}
}

var _ = errClosed
var _ = log.Printf
