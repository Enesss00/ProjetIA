// Mirror of the Go protocol package. Kept deliberately small and defensive:
// everything coming off the wire is treated as unknown until validated.

export const PROTOCOL_VERSION = 1;

export type Style = '' | 'dim' | 'ok' | 'warn' | 'err' | 'accent' | 'head';

export interface Line {
  s?: Style;
  t: string;
}

export interface Service {
  name: string;
  port: number;
  version: string;
  weak: boolean;
  vuln: boolean;
}

export interface Host {
  id: string;
  ip: string;
  zone: string;
  role: string;
  value: number;
  jewel: boolean;
  agent: boolean;
  services: Service[];
  x: number;
  y: number;
}

export interface Network {
  seed: number;
  hosts: Host[];
  ids: string[];
  attacker: string;
  attackerIps: string[];
}

export interface HostRT {
  compromised: boolean;
  compromiseAt: number;
  persistent: boolean;
  isolated: boolean;
  downUntil: number;
  agentAlive: boolean;
  hardened: Record<string, boolean> | null;
  investigated: boolean;
  suspicion: number;
}

export interface AtkAction {
  kind: string;
  target: string;
  source: string;
  service?: string;
  start: number;
  end: number;
}

export interface Attacker {
  ip: string;
  owned: string[];
  action?: AtkAction | null;
  exfil: number;
  gaveUp: boolean;
  firstMove: number;
}

export interface Alert {
  id: number;
  tick: number;
  host: string;
  text: string;
  sev: number;
}

export interface LogLine {
  tick: number;
  host: string;
  text: string;
}

export interface Job {
  kind: string;
  target: string;
  arg?: string;
  cmd: number;
  start: number;
  end: number;
}

export interface EventMsg {
  seq: number;
  tick: number;
  type: string;
  host?: string;
  peer?: string;
  ip?: string;
  service?: string;
  n?: number;
  text?: string;
  lines?: Line[];
  net?: Network;
}

export interface Damage {
  compromiseSec: number;
  downtimeSec: number;
  adjust: number;
}

export interface GameState {
  tick: number;
  seq: number;
  net: Network;
  damage: Damage;
  hosts: Record<string, HostRT>;
  blocked: Record<string, boolean> | null;
  idsAlive: Record<string, boolean> | null;
  atk: Attacker;
  alerts: Alert[] | null;
  logs: LogLine[] | null;
  jobs: Job[] | null;
  term: EventMsg[] | null;
  outcome: string;
  firstDetect: number;
  firstAction: number;
  mistakes: string[] | null;
}

export interface Envelope {
  v: number;
  t: string;
  b?: unknown;
}

export interface Snapshot {
  seq: number;
  tick: number;
  paused: boolean;
  speed: number;
  state: GameState;
}

export interface Delta {
  since: number;
  seq: number;
  tick: number;
  events: EventMsg[];
}

export interface Ended {
  outcome: string;
  score: number;
  report: string;
}

// Message type tags.
export const C = {
  New: 'new',
  Cmd: 'cmd',
  Pause: 'pause',
  Speed: 'speed',
  Resync: 'resync',
  Seek: 'seek',
} as const;

export const S = {
  Hello: 'hello',
  Snapshot: 'snapshot',
  Delta: 'delta',
  Error: 'error',
  Ended: 'ended',
} as const;

export function encode(t: string, b: unknown): string {
  return JSON.stringify({ v: PROTOCOL_VERSION, t, b });
}

/** Parse an incoming frame into an envelope, or null if it is not valid. */
export function parseEnvelope(raw: unknown): Envelope | null {
  if (typeof raw !== 'string') return null;
  let obj: unknown;
  try {
    obj = JSON.parse(raw);
  } catch {
    return null;
  }
  if (typeof obj !== 'object' || obj === null) return null;
  const e = obj as Record<string, unknown>;
  if (typeof e['t'] !== 'string') return null;
  return { v: typeof e['v'] === 'number' ? e['v'] : 0, t: e['t'], b: e['b'] };
}
