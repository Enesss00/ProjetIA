package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"ghostnet/internal/command"
	"ghostnet/internal/protocol"

	"github.com/coder/websocket"
)

// conn wraps one WebSocket client. All writes go through the buffered send
// channel so a slow client can never block the simulation; if the buffer
// fills, the connection is dropped.
type conn struct {
	ws   *websocket.Conn
	send chan []byte
	sess *Session
	h    *Hub

	// token-bucket rate limiter for client commands
	tokens   float64
	lastFill time.Time
}

const (
	sendBuffer   = 64
	writeTimeout = 5 * time.Second
	readTimeout  = 70 * time.Second // > client ping interval
)

func newConn(ws *websocket.Conn, h *Hub) *conn {
	return &conn{
		ws:       ws,
		send:     make(chan []byte, sendBuffer),
		h:        h,
		tokens:   protocol.CmdBurst,
		lastFill: time.Time{},
	}
}

// trySend queues a message, dropping it (and closing the conn) if the client
// is too slow. It never blocks the caller (the simulation loop).
func (c *conn) trySend(msg []byte) {
	select {
	case c.send <- msg:
	default:
		// Buffer full: the client cannot keep up. Close it; it will resync
		// on reconnect.
		_ = c.ws.Close(websocket.StatusPolicyViolation, "slow consumer")
	}
}

// writePump drains the send channel to the socket.
func (c *conn) writePump(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-c.send:
			if !ok {
				return
			}
			wctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.ws.Write(wctx, websocket.MessageText, msg)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

// readPump reads client messages until error or close. It enforces the size
// limit (via SetReadLimit) and the per-connection command rate limit, and it
// recovers from any panic in message handling so one bad message can never
// crash the server.
func (c *conn) readPump(ctx context.Context) {
	c.ws.SetReadLimit(protocol.MaxClientMsgBytes)
	for {
		typ, data, err := c.ws.Read(ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageText {
			continue
		}
		c.handle(ctx, data)
	}
}

func (c *conn) handle(ctx context.Context, data []byte) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("recovered from panic handling client message: %v", r)
			c.sendError("internal error processing your message")
		}
	}()

	var env protocol.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		c.sendError("malformed message")
		return
	}
	switch env.T {
	case protocol.CNew:
		c.onNew(ctx, env.Body)
	case protocol.CCmd:
		c.onCmd(env.Body)
	case protocol.CPause:
		c.onPause(env.Body)
	case protocol.CSpeed:
		c.onSpeed(env.Body)
	case protocol.CResync:
		if c.sess != nil {
			c.sess.resync(c)
		}
	case protocol.CReplaySeek:
		c.onSeek(env.Body)
	default:
		c.sendError("unknown message type")
	}
}

func (c *conn) onNew(ctx context.Context, body json.RawMessage) {
	var n protocol.ClientNew
	if err := json.Unmarshal(body, &n); err != nil {
		c.sendError("bad 'new' arguments")
		return
	}
	args := &ClientNewArgs{Seed: n.Seed, Profile: n.Profile, Workstations: n.Workstations}
	sess, err := c.h.startGame(args)
	if err != nil {
		c.sendError("could not start game: " + err.Error())
		return
	}
	if c.sess != nil {
		c.sess.unsubscribe(c)
	}
	c.sess = sess
	sess.subscribe(c)
}

func (c *conn) onCmd(body json.RawMessage) {
	if c.sess == nil {
		c.sendError("no active game — send 'new' first")
		return
	}
	if !c.allow() {
		c.sendError("slow down: command rate limit exceeded")
		return
	}
	var m protocol.ClientCmd
	if err := json.Unmarshal(body, &m); err != nil {
		c.sendError("bad 'cmd' arguments")
		return
	}
	if len(m.Line) > command.MaxLineBytes {
		m.Line = m.Line[:command.MaxLineBytes]
	}
	c.sess.runCmd(m.ID, m.Line)
}

func (c *conn) onPause(body json.RawMessage) {
	if c.sess == nil {
		return
	}
	var m protocol.ClientPause
	if json.Unmarshal(body, &m) == nil {
		c.sess.setPaused(m.Paused)
	}
}

func (c *conn) onSpeed(body json.RawMessage) {
	if c.sess == nil {
		return
	}
	var m protocol.ClientSpeed
	if json.Unmarshal(body, &m) == nil {
		c.sess.setSpeed(m.Speed)
	}
}

func (c *conn) onSeek(body json.RawMessage) {
	if c.sess == nil {
		return
	}
	var m protocol.ClientSeek
	if json.Unmarshal(body, &m) != nil {
		return
	}
	c.trySend(c.sess.replayStateAt(m.Tick))
}

// allow implements the token-bucket rate limiter. It uses time only to pace
// client input (never the simulation), which is legitimate.
func (c *conn) allow() bool {
	now := time.Now()
	if c.lastFill.IsZero() {
		c.lastFill = now
	}
	c.tokens += now.Sub(c.lastFill).Seconds() * protocol.MaxCmdRate
	c.lastFill = now
	if c.tokens > protocol.CmdBurst {
		c.tokens = protocol.CmdBurst
	}
	if c.tokens < 1 {
		return false
	}
	c.tokens--
	return true
}

func (c *conn) sendError(text string) {
	msg, err := protocol.Encode(protocol.SError, map[string]string{"text": text})
	if err != nil {
		return
	}
	c.trySend(msg)
}

var errClosed = errors.New("closed")
