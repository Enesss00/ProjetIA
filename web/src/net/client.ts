import { C, S, encode, parseEnvelope, type Delta, type Ended, type Snapshot } from './protocol';

export interface ClientHandlers {
  onHello?: (version: number, build: string) => void;
  onSnapshot: (s: Snapshot) => void;
  onDelta: (d: Delta) => void;
  onEnded: (e: Ended) => void;
  onError: (text: string) => void;
  onStatus: (connected: boolean) => void;
}

/**
 * GameClient owns the WebSocket. It reconnects with backoff and, on every
 * (re)connection, replays the last 'new' request and asks for a resync so the
 * client state is rebuilt from an authoritative snapshot. The client never
 * trusts a malformed frame: parse failures are ignored, not thrown.
 */
export class GameClient {
  private ws: WebSocket | null = null;
  private url: string;
  private h: ClientHandlers;
  private backoff = 500;
  private closed = false;
  private pending: string[] = [];
  private lastNew: string | null = null;
  private pingTimer: number | null = null;

  constructor(url: string, h: ClientHandlers) {
    this.url = url;
    this.h = h;
  }

  connect(): void {
    this.closed = false;
    this.open();
  }

  private open(): void {
    let ws: WebSocket;
    try {
      ws = new WebSocket(this.url);
    } catch {
      this.scheduleReconnect();
      return;
    }
    this.ws = ws;
    ws.onopen = () => {
      this.backoff = 500;
      this.h.onStatus(true);
      // Re-establish the game and flush queued input.
      if (this.lastNew) ws.send(this.lastNew);
      for (const m of this.pending) ws.send(m);
      this.pending = [];
      if (this.lastNew) this.sendRaw(encode(C.Resync, null));
      this.startPing();
    };
    ws.onmessage = (ev) => this.dispatch(ev.data);
    ws.onclose = () => {
      this.stopPing();
      this.h.onStatus(false);
      this.scheduleReconnect();
    };
    ws.onerror = () => {
      try {
        ws.close();
      } catch {
        /* ignore */
      }
    };
  }

  private startPing(): void {
    this.stopPing();
    // Lightweight app-level keepalive (resync is cheap and idempotent).
    this.pingTimer = window.setInterval(() => this.sendRaw(encode(C.Resync, null)), 30000);
  }

  private stopPing(): void {
    if (this.pingTimer !== null) {
      clearInterval(this.pingTimer);
      this.pingTimer = null;
    }
  }

  private scheduleReconnect(): void {
    if (this.closed) return;
    const delay = Math.min(this.backoff, 8000);
    this.backoff = Math.min(this.backoff * 2, 8000);
    window.setTimeout(() => {
      if (!this.closed) this.open();
    }, delay);
  }

  private dispatch(raw: unknown): void {
    const env = parseEnvelope(raw);
    if (!env) return;
    try {
      switch (env.t) {
        case S.Hello: {
          const b = env.b as { version: number; build: string };
          this.h.onHello?.(b?.version ?? 0, b?.build ?? '');
          break;
        }
        case S.Snapshot:
          this.h.onSnapshot(env.b as Snapshot);
          break;
        case S.Delta:
          this.h.onDelta(env.b as Delta);
          break;
        case S.Ended:
          this.h.onEnded(env.b as Ended);
          break;
        case S.Error: {
          const b = env.b as { text?: string };
          this.h.onError(b?.text ?? 'error');
          break;
        }
        default:
          break; // unknown server message: ignore, never crash
      }
    } catch {
      // A malformed body must never take down the UI.
    }
  }

  newGame(seed: number, profile?: string, workstations?: number): void {
    const body: Record<string, unknown> = { seed };
    if (profile) body['profile'] = profile;
    if (workstations) body['workstations'] = workstations;
    this.lastNew = encode(C.New, body);
    this.sendRaw(this.lastNew);
  }

  command(id: number, line: string): void {
    this.sendRaw(encode(C.Cmd, { id, line }));
  }

  pause(paused: boolean): void {
    this.sendRaw(encode(C.Pause, { paused }));
  }

  speed(speed: number): void {
    this.sendRaw(encode(C.Speed, { speed }));
  }

  seek(tick: number): void {
    this.sendRaw(encode(C.Seek, { tick }));
  }

  resync(): void {
    this.sendRaw(encode(C.Resync, null));
  }

  private sendRaw(msg: string): void {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(msg);
    } else {
      // Queue non-'new' messages until reconnection (bounded).
      if (this.pending.length < 64) this.pending.push(msg);
    }
  }

  close(): void {
    this.closed = true;
    this.stopPing();
    if (this.ws) this.ws.close();
  }
}
