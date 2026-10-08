# AGENTS.md — GHOST NET

Source of truth for anyone (human or agent) working on this repo. Read it before editing.

## What this is

GHOST NET is a 3D network-defense game. The player is a SOC analyst defending a
procedurally generated company network against an adaptive AI attacker that
plays real MITRE ATT&CK-style kill chains. Everything is a closed simulation:
**no real network access, no real scanning, no offensive code**. "Attacks" are
events in a model with costs, success and detection probabilities.

## Repository layout

```
server/            Go: deterministic engine, attacker AI, WS server, journal, report
  cmd/ghostnet/    CLI: serve | verify | replay
  internal/rng     seeded SplitMix64 PRNG (no wall clock, ever)
  internal/netgen  procedural network generator (seed -> topology/vulns/profile)
  internal/sim     model, events, pure Apply reducer, tick engine, attacker planner
  internal/command robust command parser + executor (the hardened input surface)
  internal/protocol versioned JSON WS messages (snapshots + deltas)
  internal/journal  event-sourced log (in-memory + SQLite), replay reconstruction
  internal/report   deterministic incident report (LLM optional, fallback mandatory)
  internal/server   WS hub: sessions, rate limits, conn caps, panic recovery
web/               TypeScript + Vite + Three.js + xterm.js (strict, no heavy framework)
  src/net          protocol types, WS client (reconnect/resync), state store
  src/scene        Three.js neon-city renderer
  src/ui           terminal, HUD, helpers
  e2e/             Playwright end-to-end + screenshot generator
  tests/           Vitest unit tests
docs/              architecture, protocol, attack taxonomy, screenshots
```

## Build / run / test commands

| Task | Command |
|------|---------|
| Run the whole game | `make dev` (→ http://localhost:8080) |
| Full gate (lint+types+tests) | `make verify` |
| Go tests (race) | `make test-go` |
| Web unit tests | `make test-web` |
| End-to-end | `make e2e` |
| Fuzz (parser + WS) | `make fuzz` |
| Determinism check | `make replay-check` |
| Headless demo + report | `make demo` |
| Lint both sides | `make lint` |
| Format | `make fmt` |

In this sandbox, prefix Go commands with `GOTOOLCHAIN=local` (toolchain is 1.24.7).

## Non-negotiable invariants

1. **Determinism.** The engine uses only the seeded RNG. No `time.Now()`, no map
   iteration that affects order, no goroutine races in game logic. Same seed +
   same commands ⇒ identical journal and score, tick for tick. `make replay-check`
   must stay green.
2. **Event sourcing.** All state changes are timestamped events. `State.Apply`
   is pure (no RNG, no clock, no I/O). State is rebuilt by folding events; replay
   and tests depend on this. Never mutate state outside `Apply`.
3. **Server authority.** The client validates nothing on its own. The server
   validates every command and message. Never trust client input.
4. **No panics escape.** Every client message is handled under `recover`. Bounded
   message size, per-connection rate limit, connection and game caps, game TTL.
5. **Offline-playable.** The game never needs the internet. The LLM report is
   optional; the deterministic template is the mandatory fallback.
6. **Event types are a format.** Never rename an `EvType` or repurpose a field;
   add a new event type instead (old journals must still load).

## Conventions

- Go: standard `gofmt`; errors wrapped with `%w`; exported symbols documented.
- TS: `strict` + `noUnusedLocals` + `noUncheckedIndexedAccess`; no `any` (warn).
- Keep dependencies minimal and pinned to exact versions.
- The terminal protocol carries **semantic styles**, never raw ANSI; the client
  maps styles to colours. The server must never emit escape sequences.

## Forbidden

- Real network/port scanning, real exploits, or any offensive capability.
- Wall-clock time or non-seeded randomness inside `internal/sim`.
- Client-side-only validation of anything that affects the game.
- Swallowing attacker/defender actions outside the event journal.
