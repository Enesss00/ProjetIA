import type { GameState } from '../net/protocol';
import { fmtTick } from './util';

/**
 * Hud renders the overlay panels: mission clock, score, exfiltration gauge,
 * live alert feed and sensor health. It reads straight from the store state.
 */
export class Hud {
  private clock = document.getElementById('hud-clock')!;
  private score = document.getElementById('hud-score')!;
  private exfilBar = document.getElementById('hud-exfil-bar')!;
  private exfilVal = document.getElementById('hud-exfil-val')!;
  private profile = document.getElementById('hud-profile')!;
  private atkState = document.getElementById('hud-atk-state')!;
  private alerts = document.getElementById('hud-alerts')!;
  private sensors = document.getElementById('hud-sensors')!;
  private lastAlertId = 0;
  private lastTick = 0;

  reset(): void {
    this.alerts.innerHTML = '';
    this.lastAlertId = 0;
    this.lastTick = 0;
  }

  update(s: GameState): void {
    // Detect a backward jump (replay seek / fresh snapshot) and rebuild the
    // append-only alert feed from scratch so it matches the shown tick.
    if (s.tick < this.lastTick) {
      this.alerts.innerHTML = '';
      this.lastAlertId = 0;
    }
    this.lastTick = s.tick;
    this.clock.textContent = fmtTick(s.tick);
    this.score.textContent = String(score(s));
    const ex = Math.max(0, Math.min(100, s.atk.exfil));
    this.exfilBar.style.width = ex + '%';
    this.exfilVal.textContent = ex + '%';
    this.exfilBar.classList.toggle('danger', ex >= 60);
    this.profile.textContent = s.net.attacker.toUpperCase();

    // Live intruder status so the player always knows what the attacker is
    // doing — especially when sensors are down or the attacker is contained.
    const owned = s.net.hosts.filter((h) => s.hosts[h.id]?.compromised).length;
    let label: string;
    let cls: string;
    if (s.atk.gaveUp || s.outcome === 'evicted') {
      label = '✓ CONTAINED';
      cls = 'good';
    } else if (s.atk.action) {
      label = actionLabel(s.atk.action.kind, s.atk.action.target);
      cls = 'bad';
    } else if (owned > 0) {
      label = `foothold on ${owned} host${owned > 1 ? 's' : ''}`;
      cls = 'warn';
    } else {
      label = 'searching for a way in…';
      cls = 'warn';
    }
    this.atkState.textContent = label;
    this.atkState.className = 'atk-' + cls;

    // Append new alerts (newest on top).
    const list = s.alerts ?? [];
    for (const a of list) {
      if (a.id <= this.lastAlertId) continue;
      this.lastAlertId = a.id;
      const row = document.createElement('div');
      row.className = 'alert sev' + a.sev;
      row.innerHTML =
        `<span class="t">${fmtTick(a.tick)}</span>` +
        `<span class="h">${esc(a.host || 'net')}</span>` +
        `<span class="m">${esc(a.text)}</span>`;
      this.alerts.prepend(row);
      while (this.alerts.childElementCount > 60) this.alerts.lastElementChild?.remove();
    }

    // Sensor health summary.
    let silent = 0;
    let agents = 0;
    for (const h of s.net.hosts) {
      if (!h.agent) continue;
      agents++;
      if (!s.hosts[h.id]?.agentAlive) silent++;
    }
    const idsDead = Object.entries(s.idsAlive ?? {}).filter(([, v]) => !v).length;
    this.sensors.innerHTML =
      `<span class="${silent ? 'bad' : 'good'}">agents ${agents - silent}/${agents}</span>` +
      `<span class="${idsDead ? 'bad' : 'good'}">IDS ${idsDead ? '−' + idsDead : 'ok'}</span>`;
  }
}

export function score(s: GameState): number {
  // Mirror of server scoring for live display (server remains authoritative).
  const d = s.damage;
  if (!d) return 0;
  const v =
    1000 -
    Math.floor(d.compromiseSec / 2) -
    Math.floor(d.downtimeSec / 4) -
    s.atk.exfil * 6 +
    d.adjust;
  return Math.max(0, v);
}

function esc(s: string): string {
  const d = document.createElement('div');
  d.textContent = s;
  return d.innerHTML;
}

// actionLabel turns an attacker action into a short human-readable status.
function actionLabel(kind: string, target: string): string {
  switch (kind) {
    case 'recon':
      return `scanning ${target}`;
    case 'exploit':
      return `exploiting ${target}`;
    case 'bruteforce':
      return `brute-forcing ${target}`;
    case 'lateral':
      return `pivoting to ${target}`;
    case 'persist':
      return `entrenching on ${target}`;
    case 'sensor':
      return 'disabling sensors';
    case 'exfil':
      return `⚠ stealing data (${target})`;
    case 'phish':
      return `phishing a user (${target})`;
    default:
      return kind;
  }
}
