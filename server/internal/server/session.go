package server

import (
	"sync"
	"time"

	"ghostnet/internal/command"
	"ghostnet/internal/journal"
	"ghostnet/internal/netgen"
	"ghostnet/internal/protocol"
	"ghostnet/internal/report"
	"ghostnet/internal/sim"
)

// tickInterval is the wall-clock period of one simulation tick at speed 1.
const tickInterval = time.Second / sim.TicksPerSecond

// Session is one running (or finished) game. It owns the engine and ticks it
// on its own goroutine; connections subscribe to receive event deltas. The
// simulation logic never reads the wall clock — the ticker only decides *when*
// to advance, never *what* happens.
type Session struct {
	token string
	mu    sync.Mutex

	eng  *sim.Engine
	jnl  *journal.Journal
	exec *command.Executor

	paused bool
	speed  int
	ended  bool
	report string

	subs     map[*conn]struct{}
	stop     chan struct{}
	stopped  bool
	dbPath   string
	reporter report.Reporter

	// idleSince is when the last subscriber left (zero while subscribed).
	idleSince time.Time
}

func newSession(token string, n *ClientNewArgs, dbPath string, reporter report.Reporter) (*Session, error) {
	net := netgen.Generate(n.Seed, netgen.Options{Profile: n.Profile, Workstations: n.Workstations})
	jnl, err := journal.New(n.Seed, dbPath)
	if err != nil {
		return nil, err
	}
	s := &Session{
		token:     token,
		jnl:       jnl,
		speed:     1,
		subs:      map[*conn]struct{}{},
		stop:      make(chan struct{}),
		dbPath:    dbPath,
		reporter:  reporter,
		idleSince: time.Now(),
	}
	// The engine's event sink records to the journal and fans out to subs.
	s.eng = sim.NewEngine(sim.Config{Net: net}, func(ev sim.Event) {
		s.jnl.Append(ev)
	})
	s.exec = command.New(s.eng)
	go s.loop()
	return s, nil
}

// ClientNewArgs mirrors protocol.ClientNew after validation.
type ClientNewArgs struct {
	Seed         uint64
	Profile      string
	Workstations int
}

// loop advances the simulation on a ticker and pushes deltas to subscribers.
func (s *Session) loop() {
	t := time.NewTicker(tickInterval)
	defer t.Stop()
	lastSeq := 0
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			s.mu.Lock()
			if s.ended {
				s.mu.Unlock()
				continue
			}
			steps := 0
			if !s.paused {
				steps = s.speed
			}
			for i := 0; i < steps; i++ {
				if !s.eng.Tick() {
					break
				}
			}
			// Build a delta from newly-journalled events.
			cur := s.eng.Seq()
			var evs []sim.Event
			if cur > lastSeq {
				evs = s.jnl.Since(lastSeq)
				lastSeq = cur
			}
			st := s.eng.State()
			justEnded := st.Outcome != sim.OutcomeRunning && !s.ended
			if justEnded {
				s.ended = true
				s.report = s.reporter.Generate(s.jnl.All(), st)
			}
			subs := s.snapshotSubs()
			rep := s.report
			score := st.Score()
			outcome := st.Outcome
			s.mu.Unlock()

			if len(evs) > 0 {
				msg, err := protocol.Encode(protocol.SDelta, protocol.Delta{
					Since: evs[0].Seq - 1, Seq: cur, Tick: st.Tick, Events: evs,
				})
				if err == nil {
					for _, c := range subs {
						c.trySend(msg)
					}
				}
			}
			if justEnded {
				msg, err := protocol.Encode(protocol.SEnded, protocol.Ended{
					Outcome: outcome, Score: score, Report: rep,
				})
				if err == nil {
					for _, c := range subs {
						c.trySend(msg)
					}
				}
			}
		}
	}
}

func (s *Session) snapshotSubs() []*conn {
	out := make([]*conn, 0, len(s.subs))
	for c := range s.subs {
		out = append(out, c)
	}
	return out
}

// subscribe attaches a connection and sends it a full snapshot immediately.
func (s *Session) subscribe(c *conn) {
	s.mu.Lock()
	s.subs[c] = struct{}{}
	s.idleSince = time.Time{}
	msg := s.snapshotLocked()
	ended := s.ended
	endMsg := s.endedMsgLocked()
	s.mu.Unlock()
	c.trySend(msg)
	if ended && endMsg != nil {
		c.trySend(endMsg)
	}
}

func (s *Session) unsubscribe(c *conn) {
	s.mu.Lock()
	delete(s.subs, c)
	if len(s.subs) == 0 {
		s.idleSince = time.Now()
	}
	s.mu.Unlock()
}

func (s *Session) snapshotLocked() []byte {
	st := s.eng.State()
	msg, _ := protocol.Encode(protocol.SSnapshot, protocol.Snapshot{
		Seq: s.eng.Seq(), Tick: st.Tick, Paused: s.paused, Speed: s.speed, State: st,
	})
	return msg
}

func (s *Session) endedMsgLocked() []byte {
	if !s.ended {
		return nil
	}
	st := s.eng.State()
	msg, _ := protocol.Encode(protocol.SEnded, protocol.Ended{
		Outcome: st.Outcome, Score: st.Score(), Report: s.report,
	})
	return msg
}

// resync sends a fresh snapshot to one connection.
func (s *Session) resync(c *conn) {
	s.mu.Lock()
	msg := s.snapshotLocked()
	s.mu.Unlock()
	c.trySend(msg)
}

// runCmd executes a player command line.
func (s *Session) runCmd(id int, line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	s.exec.Run(id, line)
}

func (s *Session) setPaused(p bool) {
	s.mu.Lock()
	s.paused = p
	s.mu.Unlock()
}

func (s *Session) setSpeed(sp int) {
	if sp < 1 {
		sp = 1
	}
	if sp > 8 {
		sp = 8
	}
	s.mu.Lock()
	s.speed = sp
	s.mu.Unlock()
}

// replayStateAt returns a snapshot of the state at a given tick, for replay.
func (s *Session) replayStateAt(tick int) []byte {
	st := s.jnl.StateAt(tick)
	msg, _ := protocol.Encode(protocol.SSnapshot, protocol.Snapshot{
		Seq: st.Seq, Tick: st.Tick, Paused: true, Speed: 0, State: st,
	})
	return msg
}

func (s *Session) close() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	close(s.stop)
	s.mu.Unlock()
	_ = s.jnl.Close()
}
