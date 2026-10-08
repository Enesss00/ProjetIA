import type { Alert, Delta, EventMsg, GameState, LogLine, Snapshot } from './protocol';

export interface Flash {
  kind: string; // attacker action kind, for visual cues
  source: string;
  target: string;
  tick: number;
}

/**
 * Store holds the authoritative snapshot and applies event deltas for the
 * fields the UI renders. It mirrors a subset of the server's reducer; it never
 * invents state, and a periodic resync replaces it wholesale, so any drift is
 * transient. Terminal output events are surfaced via a callback rather than
 * stored, so the terminal owns its own scrollback.
 */
export class Store {
  state: GameState | null = null;
  paused = false;
  speed = 1;
  lastSeq = 0;
  // Visual flashes consumed by the scene each frame.
  flashes: Flash[] = [];

  onTerm: ((ev: EventMsg) => void) | null = null;
  onChange: (() => void) | null = null;

  applySnapshot(s: Snapshot): void {
    this.state = s.state;
    this.paused = s.paused;
    this.speed = s.speed;
    this.lastSeq = s.seq;
    this.normalize();
    this.onChange?.();
  }

  applyDelta(d: Delta): void {
    if (!this.state) return;
    // Ignore stale/duplicate deltas.
    for (const ev of d.events) {
      if (ev.seq <= this.lastSeq) continue;
      this.applyEvent(ev);
      this.lastSeq = ev.seq;
    }
    this.state.tick = d.tick;
    this.onChange?.();
  }

  private normalize(): void {
    const s = this.state;
    if (!s) return;
    s.alerts ??= [];
    s.logs ??= [];
    s.jobs ??= [];
    s.blocked ??= {};
    s.idsAlive ??= {};
    s.mistakes ??= [];
  }

  private applyEvent(ev: EventMsg): void {
    const s = this.state;
    if (!s) return;
    const rt = ev.host ? s.hosts[ev.host] : undefined;
    switch (ev.type) {
      case 'cmd':
      case 'term':
        this.onTerm?.(ev);
        break;
      case 'host.isolate':
        if (rt) rt.isolated = true;
        break;
      case 'host.unisolate':
        if (rt) rt.isolated = false;
        break;
      case 'host.down':
        if (rt && (ev.n ?? 0) > rt.downUntil) rt.downUntil = ev.n ?? 0;
        break;
      case 'host.harden':
        if (rt && ev.service) {
          rt.hardened ??= {};
          rt.hardened[ev.service] = true;
        }
        break;
      case 'host.investigate':
        if (rt) rt.investigated = true;
        break;
      case 'fw.block':
        if (ev.ip) (s.blocked ??= {})[ev.ip] = true;
        break;
      case 'fw.unblock':
        if (ev.ip && s.blocked) delete s.blocked[ev.ip];
        break;
      case 'sensor.off':
        this.setSensor(ev, false);
        break;
      case 'sensor.on':
        this.setSensor(ev, true);
        break;
      case 'atk.ip':
        if (ev.ip) s.atk.ip = ev.ip;
        break;
      case 'atk.action':
        s.atk.action = {
          kind: ev.text ?? '',
          target: ev.host ?? '',
          source: ev.peer ?? '',
          service: ev.service,
          start: ev.tick,
          end: ev.n ?? ev.tick,
        };
        this.flashes.push({
          kind: ev.text ?? '',
          source: ev.peer ?? '',
          target: ev.host ?? '',
          tick: ev.tick,
        });
        break;
      case 'atk.fail':
      case 'atk.learn':
      case 'atk.persist':
        if (ev.type === 'atk.persist' && rt) rt.persistent = true;
        s.atk.action = null;
        break;
      case 'atk.own':
        s.atk.action = null;
        if (rt && !rt.compromised) {
          rt.compromised = true;
          rt.compromiseAt = ev.tick;
          if (!s.atk.owned.includes(ev.host ?? '')) s.atk.owned.push(ev.host ?? '');
        }
        break;
      case 'atk.lost':
        if (rt) {
          rt.compromised = false;
          rt.persistent = false;
        }
        s.atk.owned = s.atk.owned.filter((id) => id !== ev.host);
        break;
      case 'atk.exfil':
        s.atk.action = null;
        s.atk.exfil = Math.min(100, s.atk.exfil + (ev.n ?? 0));
        break;
      case 'atk.giveup':
        s.atk.gaveUp = true;
        s.atk.action = null;
        break;
      case 'alert': {
        const a: Alert = {
          id: (s.alerts?.length ?? 0) + 1,
          tick: ev.tick,
          host: ev.host ?? '',
          text: ev.text ?? '',
          sev: ev.n ?? 1,
        };
        (s.alerts ??= []).push(a);
        if (s.alerts.length > 500) s.alerts.splice(0, s.alerts.length - 500);
        if (rt) rt.suspicion++;
        break;
      }
      case 'log': {
        const l: LogLine = { tick: ev.tick, host: ev.host ?? '', text: ev.text ?? '' };
        (s.logs ??= []).push(l);
        if (s.logs.length > 2000) s.logs.splice(0, s.logs.length - 2000);
        break;
      }
      case 'game.end':
        s.outcome = ev.text ?? '';
        break;
      default:
        break;
    }
  }

  private setSensor(ev: EventMsg, on: boolean): void {
    const s = this.state;
    if (!s) return;
    if (ev.host) {
      const rt = s.hosts[ev.host];
      if (rt) rt.agentAlive = on;
    } else if (ev.text) {
      (s.idsAlive ??= {})[ev.text] = on;
    }
  }
}
