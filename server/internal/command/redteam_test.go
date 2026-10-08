package command_test

import (
	"math/rand"
	"strings"
	"testing"

	"ghostnet/internal/command"
	"ghostnet/internal/netgen"
	"ghostnet/internal/sim"
)

// newExec builds a fresh engine+executor for a seed.
func newExec(seed uint64) (*sim.Engine, *command.Executor) {
	net := netgen.Generate(seed, netgen.Options{})
	eng := sim.NewEngine(sim.Config{Net: net}, func(sim.Event) {})
	return eng, command.New(eng)
}

// assertInvariants checks state can never be corrupted by player input.
func assertInvariants(t *testing.T, eng *sim.Engine) {
	t.Helper()
	st := eng.State()
	jewels := 0
	for _, h := range st.Net.Hosts {
		if h.Jewel {
			jewels++
		}
	}
	if jewels != 1 {
		t.Fatalf("crown-jewel count corrupted: %d", jewels)
	}
	if st.Atk.Exfil < 0 || st.Atk.Exfil > sim.ExfilTotal {
		t.Fatalf("exfil out of bounds: %d", st.Atk.Exfil)
	}
	_ = st.Hash() // must not panic
}

// TestAbsurdCommands throws a catalogue of hostile inputs at the executor and
// asserts nothing panics and no invariant breaks.
func TestAbsurdCommands(t *testing.T) {
	eng, x := newExec(42)
	nasty := []string{
		"",
		"   ",
		"\x00\x00\x00",
		"\x1b[2J\x1b[31mboom",
		"isolate",
		"isolate web-01 db-01 ws-01 extra extra extra extra extra extra extra",
		"isolate ../../etc/passwd",
		"isolate " + strings.Repeat("A", 5000),
		"block",
		"block not.an.ip",
		"block 256.256.256.256",
		"block 10.0.0.0/8",
		"block ::1",
		"block 0.0.0.0",
		"patch",
		"patch web-01",
		"patch web-01 nonexistent-service",
		"patch nonexistent-host ssh",
		"scan",
		"scan net net net",
		"scan \x07\x08\x7f",
		"unisolate ghost",
		"restart ' OR 1=1 --",
		"forensics 🔥🔥🔥",
		"logs --tail --host=injection\x1b[0m",
		"UNKNOWNVERB with args",
		"HELP",
		"ＳＴＡＴＵＳ", // full-width unicode
		"status\u202e",
		strings.Repeat("scan net\n", 1),
	}
	for _, line := range nasty {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic on input %q: %v", line, r)
				}
			}()
			x.Run(1, line)
		}()
		assertInvariants(t, eng)
	}
	// Advance a bit to let any queued jobs resolve, then re-check.
	for i := 0; i < 200; i++ {
		eng.Tick()
	}
	assertInvariants(t, eng)
}

// TestCommandSpamStaysConsistent fires a large random stream of commands while
// the engine ticks, asserting invariants throughout — the "high-frequency
// repeated commands" case.
func TestCommandSpamStaysConsistent(t *testing.T) {
	eng, x := newExec(1337)
	verbs := []string{"status", "scan net", "scan web-01", "isolate ws-01", "unisolate ws-01",
		"block 203.0.113.5", "unblock 203.0.113.5", "patch web-01 ssh", "restart db-01",
		"forensics db-01", "sensors", "logs --tail", "help", "", "garbage!!"}
	r := rand.New(rand.NewSource(99))
	id := 0
	for step := 0; step < 3000 && eng.State().Outcome == sim.OutcomeRunning; step++ {
		if r.Intn(2) == 0 {
			id++
			x.Run(id, verbs[r.Intn(len(verbs))])
		}
		eng.Tick()
		if step%250 == 0 {
			assertInvariants(t, eng)
		}
	}
	assertInvariants(t, eng)
}

// TestDeterminismThroughCommandPath proves that driving the engine through the
// full parser+executor path (not just engine methods) is still deterministic.
func TestDeterminismThroughCommandPath(t *testing.T) {
	script := []struct {
		tick int
		line string
	}{
		{20, "scan net"},
		{60, "patch web-01 ssh"},
		{120, "isolate web-01"},
		{200, "block 203.0.113.5"},
		{300, "forensics db-01"},
		{400, "restart db-01"},
	}
	run := func() (string, int) {
		net := netgen.Generate(2024, netgen.Options{})
		eng := sim.NewEngine(sim.Config{Net: net}, func(sim.Event) {})
		x := command.New(eng)
		id, si := 0, 0
		for eng.State().Outcome == sim.OutcomeRunning {
			for si < len(script) && script[si].tick == eng.State().Tick {
				id++
				x.Run(id, script[si].line)
				si++
			}
			if !eng.Tick() {
				break
			}
		}
		return eng.State().Hash(), eng.State().Score()
	}
	h1, s1 := run()
	h2, s2 := run()
	if h1 != h2 || s1 != s2 {
		t.Fatalf("non-deterministic through command path: (%s,%d) vs (%s,%d)", h1[:8], s1, h2[:8], s2)
	}
}
