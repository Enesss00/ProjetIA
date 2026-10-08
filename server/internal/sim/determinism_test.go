package sim_test

import (
	"testing"

	"ghostnet/internal/netgen"
	. "ghostnet/internal/sim"
)

// runGame plays a full seeded game with a fixed script of player inputs
// injected at given ticks, and returns the final state hash, score and the
// number of events produced. It is used to prove determinism.
type scriptCmd struct {
	tick int
	fn   func(e *Engine, id int)
}

func runGame(seed uint64, script []scriptCmd) (hash string, score, events int) {
	net := netgen.Generate(seed, netgen.Options{})
	count := 0
	e := NewEngine(Config{Net: net}, func(Event) { count++ })
	cmdID := 1000
	si := 0
	for e.State().Outcome == OutcomeRunning {
		for si < len(script) && script[si].tick == e.State().Tick {
			script[si].fn(e, cmdID)
			cmdID++
			si++
		}
		if !e.Tick() {
			break
		}
	}
	return e.State().Hash(), e.State().Score(), count
}

func TestReplayDeterminism(t *testing.T) {
	seeds := []uint64{1, 2, 42, 1337, 999999}
	script := []scriptCmd{
		{30, func(e *Engine, id int) { e.StartJob("scan-net", "", "", id) }},
		{90, func(e *Engine, id int) { e.Isolate("web-01") }},
		{150, func(e *Engine, id int) { e.Block("203.0.113.5") }},
		{200, func(e *Engine, id int) { e.StartJob("forensics", "db-01", "", id) }},
	}
	for _, s := range seeds {
		h1, sc1, ev1 := runGame(s, script)
		h2, sc2, ev2 := runGame(s, script)
		if h1 != h2 {
			t.Errorf("seed %d: state hash differs across runs", s)
		}
		if sc1 != sc2 || ev1 != ev2 {
			t.Errorf("seed %d: score/events differ: (%d,%d) vs (%d,%d)", s, sc1, ev1, sc2, ev2)
		}
	}
}

func TestGameAlwaysTerminates(t *testing.T) {
	for s := uint64(0); s < 60; s++ {
		net := netgen.Generate(s, netgen.Options{})
		e := NewEngine(Config{Net: net}, nil)
		ticks := 0
		for e.Tick() {
			ticks++
			if ticks > MaxTicks+10 {
				t.Fatalf("seed %d did not terminate", s)
			}
		}
		if e.State().Outcome == OutcomeRunning {
			t.Fatalf("seed %d ended while still running", s)
		}
	}
}

func TestGeneratorInvariants(t *testing.T) {
	for s := uint64(0); s < 200; s++ {
		net := netgen.Generate(s, netgen.Options{})
		if len(net.Hosts) < 5 {
			t.Fatalf("seed %d: too few hosts", s)
		}
		jewels, ips := 0, map[string]bool{}
		entry := false
		for _, h := range net.Hosts {
			if h.Jewel {
				jewels++
			}
			if ips[h.IP] {
				t.Fatalf("seed %d: duplicate IP %s", s, h.IP)
			}
			ips[h.IP] = true
			if h.Zone == ZoneDMZ {
				for _, sv := range h.Services {
					if sv.Weak || sv.Vuln {
						entry = true
					}
				}
			}
		}
		if jewels != 1 {
			t.Fatalf("seed %d: expected exactly one crown jewel, got %d", s, jewels)
		}
		if !entry {
			t.Fatalf("seed %d: no viable DMZ entry point", s)
		}
	}
}
