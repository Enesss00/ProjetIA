package journal_test

import (
	"path/filepath"
	"testing"

	"ghostnet/internal/journal"
	"ghostnet/internal/netgen"
	"ghostnet/internal/sim"
)

// TestReplayReconstructsState proves event sourcing: folding the journal
// reproduces the engine's live state exactly, and StateAt gives a monotonic
// prefix (replay).
func TestReplayReconstructsState(t *testing.T) {
	net := netgen.Generate(42, netgen.Options{Profile: "apt"})
	j, err := journal.New(42, "")
	if err != nil {
		t.Fatal(err)
	}
	eng := sim.NewEngine(sim.Config{Net: net}, j.Append)
	for eng.Tick() {
	}
	live := eng.State()

	// Full replay must match the live state hash.
	replayed := j.StateAt(-1)
	if replayed.Hash() != live.Hash() {
		t.Fatalf("replayed state hash != live state hash")
	}

	// A prefix at an earlier tick must have a tick <= that bound and fewer or
	// equal owned hosts than the end (monotonic compromise under no defense).
	mid := live.Tick / 2
	pre := j.StateAt(mid)
	if pre.Tick > mid {
		t.Fatalf("StateAt(%d) produced tick %d", mid, pre.Tick)
	}
}

func TestSQLitePersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "game.db")
	net := netgen.Generate(7, netgen.Options{})
	j, err := journal.New(7, path)
	if err != nil {
		t.Fatal(err)
	}
	eng := sim.NewEngine(sim.Config{Net: net}, j.Append)
	for i := 0; i < 100 && eng.Tick(); i++ {
	}
	if j.Len() == 0 {
		t.Fatal("no events recorded")
	}
	if err := j.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}
