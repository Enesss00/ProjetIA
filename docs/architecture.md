# Architecture

GHOST NET is split into an authoritative Go server (the simulation) and a thin
TypeScript client (rendering + input). The server is the single source of
truth; the client never decides game outcomes.

## Determinism & event sourcing

The simulation is a **deterministic state machine** driven by a fixed 10 Hz
tick. Its only entropy source is a seeded SplitMix64 PRNG (`internal/rng`).
There is no wall-clock time and no order-dependent map iteration inside
`internal/sim`, so a given `(seed, command stream)` always yields the same
game.

Every state change is an **event** (`sim.Event`). The current state is the
left fold of all events via `State.Apply`, which is a pure function:

```
state(n) = Apply(state(n-1), event(n))
```

Consequences that fall out for free:

- **Replay** = fold the journal up to any tick (`journal.StateAt`).
- **Tests** = run two games and compare `State.Hash()` (a SHA-256 of the
  canonical JSON, with sorted map keys).
- **Persistence** = append events to SQLite; reconstruct by replay.

```
                 commands (validated)
                        │
   seed ─▶ netgen ─▶ Engine.Tick ──emit──▶ Event ──▶ State.Apply ─▶ State
                        │                     │
                        │                     └────▶ Journal (memory + SQLite)
                        ▼                                   │
                 attacker planner                           ▼
                                                   Delta / Snapshot ─▶ client
```

## Server packages

| Package | Responsibility |
|---------|----------------|
| `rng` | Seeded SplitMix64. `Fork` derives independent sub-streams. |
| `netgen` | Seed → topology, services, vulnerabilities, sensor placement, attacker profile. Guarantees a solvable kill chain and exactly one crown jewel. |
| `sim` | The model (`Host`, `Network`, `Event`, `State`), the pure `Apply` reducer, the `Engine` tick loop, and the attacker `planner`. |
| `command` | Hardened parser (`Parse`) and `Executor` that turns input into engine calls. |
| `protocol` | Versioned JSON WS messages; `Snapshot` (full state) and `Delta` (event batch). |
| `journal` | Append-only event log, in memory and mirrored to SQLite; `StateAt` for replay. |
| `report` | `Reporter` interface; `Template` deterministic report (LLM optional, fallback mandatory). |
| `server` | WS `Hub` and `Session`: ticking, broadcast, reconnection/resync, rate limits, caps, panic recovery. |

## Tick loop

Each `Session` owns one `Engine` and ticks it on a `time.Ticker` (the only use
of wall time, and only to decide *when* to advance, never *what* happens). On
each tick the engine:

1. resolves finished player jobs (scan/patch/restart/forensics),
2. resolves the attacker's in-flight action, then lets the planner pick a new
   one if idle,
3. accrues per-second damage,
4. checks end conditions.

New events since the last broadcast are shipped to subscribers as a `Delta`.
A new or reconnecting client gets a full `Snapshot` first, then deltas.

## Robustness

- Bounded WS message size (`SetReadLimit`), per-connection token-bucket rate
  limiting, caps on concurrent connections and games, and a per-game idle TTL
  with a janitor.
- Every inbound client message is handled under `recover`; a malformed message
  yields a clean `error` frame, never a crash.
- Slow clients are dropped (bounded send buffer) and resync on reconnect.
- The client tolerates any unexpected server message (unknown types ignored,
  malformed bodies caught) and rebuilds from snapshots, so it never hard-crashes.

## Client

- `net/` — protocol types, the reconnecting WS client, and a `Store` that
  applies deltas onto snapshots for the fields the UI renders (self-healing via
  a periodic resync).
- `scene/` — a Three.js renderer: buildings per host, bloom post-processing,
  beams/sweeps for attacker actions, colour/emissive state for compromise and
  isolation.
- `ui/` — the xterm.js terminal (semantic styles → colours, local line editing
  and history) and the HUD (clock, score, exfil gauge, alert feed, sensors).
