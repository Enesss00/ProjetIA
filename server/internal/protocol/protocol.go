// Package protocol defines the versioned JSON messages exchanged over the
// WebSocket. The client never trusts its own view: the server is authoritative
// and sends snapshots (full state) and deltas (event batches).
package protocol

import (
	"encoding/json"

	"ghostnet/internal/sim"
)

// Version is the protocol version. Bump on breaking changes.
const Version = 1

// Limits protecting the server from abusive clients.
const (
	MaxClientMsgBytes = 4096 // a client message larger than this is rejected
	MaxCmdRate        = 20   // commands per second per client (token bucket)
	CmdBurst          = 10
)

// Client -> server message types.
const (
	CNew        = "new"    // start a new game (Seed, optional Profile, Workstations)
	CCmd        = "cmd"    // run a terminal command (ID, Line)
	CPause      = "pause"  // toggle pause (Paused)
	CSpeed      = "speed"  // set speed multiplier (Speed: 1,2,4)
	CResync     = "resync" // request a fresh snapshot
	CReplaySeek = "seek"   // replay: jump to a tick (Tick)
)

// Server -> client message types.
const (
	SHello    = "hello"    // protocol/version handshake
	SSnapshot = "snapshot" // full state
	SDelta    = "delta"    // batch of events since last seq
	SError    = "error"    // recoverable error (Text)
	SEnded    = "ended"    // game over (Outcome, Report)
)

// Envelope is the common wrapper. T is the type; the body is in the matching
// field. Unknown fields are ignored by decoders on both sides.
type Envelope struct {
	V    int             `json:"v"`
	T    string          `json:"t"`
	Body json.RawMessage `json:"b,omitempty"`
}

// ClientNew starts a game.
type ClientNew struct {
	Seed         uint64 `json:"seed"`
	Profile      string `json:"profile,omitempty"`
	Workstations int    `json:"workstations,omitempty"`
}

// ClientCmd runs a command line.
type ClientCmd struct {
	ID   int    `json:"id"`
	Line string `json:"line"`
}

// ClientSpeed sets the tick speed multiplier.
type ClientSpeed struct {
	Speed int `json:"speed"`
}

// ClientPause pauses or resumes.
type ClientPause struct {
	Paused bool `json:"paused"`
}

// ClientSeek jumps replay to a tick.
type ClientSeek struct {
	Tick int `json:"tick"`
}

// Hello is the handshake.
type Hello struct {
	Version int    `json:"version"`
	Build   string `json:"build"`
}

// Snapshot carries the full state plus meta the client needs to render.
type Snapshot struct {
	Seq    int        `json:"seq"`
	Tick   int        `json:"tick"`
	Paused bool       `json:"paused"`
	Speed  int        `json:"speed"`
	State  *sim.State `json:"state"`
}

// Delta carries events with sequence > Since, up to and including Seq.
type Delta struct {
	Since  int         `json:"since"`
	Seq    int         `json:"seq"`
	Tick   int         `json:"tick"`
	Events []sim.Event `json:"events"`
}

// Ended signals game over.
type Ended struct {
	Outcome string `json:"outcome"`
	Score   int    `json:"score"`
	Report  string `json:"report"`
}

// Encode wraps a typed body into an envelope's JSON bytes.
func Encode(t string, body any) ([]byte, error) {
	var raw json.RawMessage
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		raw = b
	}
	return json.Marshal(Envelope{V: Version, T: t, Body: raw})
}
