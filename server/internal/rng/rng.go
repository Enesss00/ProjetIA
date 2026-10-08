// Package rng provides a small deterministic PRNG (SplitMix64).
// It never reads wall-clock time or global state, so the same seed always
// yields the same sequence on every platform.
package rng

// RNG is a SplitMix64 generator. The zero value is valid (seed 0).
type RNG struct{ state uint64 }

// New returns a generator seeded with seed.
func New(seed uint64) *RNG { return &RNG{state: seed} }

// Uint64 returns the next pseudo-random value.
func (r *RNG) Uint64() uint64 {
	r.state += 0x9e3779b97f4a7c15
	z := r.state
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// Intn returns a value in [0,n). It returns 0 when n <= 0.
func (r *RNG) Intn(n int) int {
	if n <= 0 {
		return 0
	}
	return int(r.Uint64() % uint64(n))
}

// Float64 returns a value in [0,1).
func (r *RNG) Float64() float64 { return float64(r.Uint64()>>11) / (1 << 53) }

// Chance reports true with probability p (clamped to [0,1]).
func (r *RNG) Chance(p float64) bool { return r.Float64() < p }

// Fork derives an independent child stream labelled by salt.
func (r *RNG) Fork(salt uint64) *RNG { return New(r.Uint64() ^ (salt * 0xd6e8feb86659fd93)) }

// State exposes the internal state (used for determinism hashing).
func (r *RNG) State() uint64 { return r.state }
