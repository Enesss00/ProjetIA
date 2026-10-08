# WebSocket protocol

Transport: a single WebSocket at `/ws`. All messages are JSON, UTF-8, and
versioned. The server is authoritative; the client sends intents and renders
what it is told.

## Envelope

Every message is wrapped:

```json
{ "v": 1, "t": "<type>", "b": { ...body... } }
```

- `v` — protocol version (currently `1`). Bump on breaking changes.
- `t` — message type tag (see below).
- `b` — the typed body (omitted for bodyless messages).

Decoders on both sides ignore unknown fields and unknown types, so additive
changes are backward-compatible.

## Limits

| Limit | Value |
|-------|-------|
| Max client message size | 4096 bytes (connection closed if exceeded) |
| Command rate | 20/s per connection, burst 10 (token bucket) |
| Max command line | 1024 bytes (truncated) |

## Client → server

| `t` | body | meaning |
|-----|------|---------|
| `new` | `{seed, profile?, workstations?}` | start a game |
| `cmd` | `{id, line}` | run a terminal command; `id` correlates output |
| `pause` | `{paused}` | pause / resume the tick |
| `speed` | `{speed}` | set speed multiplier (1–8) |
| `resync` | — | request a fresh full snapshot |
| `seek` | `{tick}` | replay: reconstruct and send state at `tick` |

## Server → client

| `t` | body | meaning |
|-----|------|---------|
| `hello` | `{version, build}` | handshake, sent on connect |
| `snapshot` | `{seq, tick, paused, speed, state}` | full authoritative state |
| `delta` | `{since, seq, tick, events[]}` | events with `seq` in `(since, seq]` |
| `error` | `{text}` | recoverable error (bad input, rate limit, no game) |
| `ended` | `{outcome, score, report}` | game over + incident report |

## Snapshots and deltas

On subscribe (and on `resync`), the server sends a `snapshot` containing the
entire `State`. Thereafter it streams `delta` batches of the raw events the
engine emitted. The client folds deltas onto the snapshot using the same event
semantics as the server. A periodic `resync` replaces the client state wholesale,
so any drift is transient and self-healing.

Because the authoritative state and the event stream are the same data the
journal stores, replay (`seek`) is just the server folding the journal to a
tick and sending the resulting snapshot.

## Events

Events are the unit of state change and the on-disk journal format. Each event
has a stable `type` string (e.g. `atk.own`, `alert`, `host.isolate`) and a flat
set of optional fields; unused fields are omitted. Event type strings and field
meanings are **append-only**: never rename a type or repurpose a field — add a
new type instead, so old journals keep replaying. See `server/internal/sim/event.go`.
