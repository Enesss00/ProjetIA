package rng

import "testing"

func TestDeterministic(t *testing.T) {
	a, b := New(42), New(42)
	for i := 0; i < 1000; i++ {
		if a.Uint64() != b.Uint64() {
			t.Fatalf("diverged at %d", i)
		}
	}
}

func TestRanges(t *testing.T) {
	r := New(7)
	for i := 0; i < 10000; i++ {
		if v := r.Intn(5); v < 0 || v >= 5 {
			t.Fatalf("Intn out of range: %d", v)
		}
		if f := r.Float64(); f < 0 || f >= 1 {
			t.Fatalf("Float64 out of range: %f", f)
		}
	}
	if r.Intn(0) != 0 || r.Intn(-3) != 0 {
		t.Fatal("Intn must return 0 for n<=0")
	}
}
