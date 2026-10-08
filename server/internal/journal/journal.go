// Package journal is the event-sourced log of a game. Every state change is
// an event; the current state is the fold of all events, and a replay is just
// re-applying a prefix. The journal keeps events in memory (authoritative for
// the running game) and optionally mirrors them to SQLite for persistence and
// post-mortem inspection.
package journal

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"

	"ghostnet/internal/sim"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (no cgo)
)

// Journal records the events of one game.
type Journal struct {
	mu     sync.RWMutex
	seed   uint64
	events []sim.Event
	db     *sql.DB
	gameID int64
}

// New returns an in-memory journal for a game with the given seed. If path is
// non-empty, events are also persisted to a SQLite database at that path.
func New(seed uint64, path string) (*Journal, error) {
	j := &Journal{seed: seed}
	if path == "" {
		return j, nil
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite: serialise writes
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	res, err := db.Exec(`INSERT INTO games(seed) VALUES(?)`, int64(seed))
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("insert game: %w", err)
	}
	j.gameID, _ = res.LastInsertId()
	j.db = db
	return j, nil
}

func migrate(db *sql.DB) error {
	const schema = `
CREATE TABLE IF NOT EXISTS games(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  seed INTEGER NOT NULL,
  created_at TEXT DEFAULT (datetime('now'))
);
CREATE TABLE IF NOT EXISTS events(
  game_id INTEGER NOT NULL,
  seq INTEGER NOT NULL,
  tick INTEGER NOT NULL,
  type TEXT NOT NULL,
  payload TEXT NOT NULL,
  PRIMARY KEY(game_id, seq)
);`
	_, err := db.Exec(schema)
	return err
}

// Append records an event. It is safe for concurrent use.
func (j *Journal) Append(ev sim.Event) {
	j.mu.Lock()
	j.events = append(j.events, ev)
	db, gid := j.db, j.gameID
	j.mu.Unlock()
	if db == nil {
		return
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		return
	}
	// Best-effort persistence; a DB hiccup must never crash the game.
	_, _ = db.Exec(`INSERT OR IGNORE INTO events(game_id,seq,tick,type,payload) VALUES(?,?,?,?,?)`,
		gid, ev.Seq, ev.Tick, string(ev.Type), string(payload))
}

// Len returns the number of events recorded.
func (j *Journal) Len() int {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return len(j.events)
}

// Seed returns the game seed.
func (j *Journal) Seed() uint64 { return j.seed }

// Since returns a copy of all events with Seq > since, in order.
func (j *Journal) Since(since int) []sim.Event {
	j.mu.RLock()
	defer j.mu.RUnlock()
	var out []sim.Event
	for _, ev := range j.events {
		if ev.Seq > since {
			out = append(out, ev)
		}
	}
	return out
}

// All returns a copy of every event.
func (j *Journal) All() []sim.Event {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return append([]sim.Event(nil), j.events...)
}

// StateAt reconstructs the state after applying events up to and including
// tick t (t<0 means the full journal). This powers replay and determinism
// checks.
func (j *Journal) StateAt(t int) *sim.State {
	j.mu.RLock()
	evs := append([]sim.Event(nil), j.events...)
	j.mu.RUnlock()
	st := sim.NewState()
	for _, ev := range evs {
		if t >= 0 && ev.Tick > t {
			break
		}
		st.Apply(ev)
	}
	return st
}

// Close releases the database handle, if any.
func (j *Journal) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.db != nil {
		err := j.db.Close()
		j.db = nil
		return err
	}
	return nil
}
