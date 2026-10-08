import { describe, it, expect } from 'vitest';
import { Store } from '../src/net/store';
import { parseEnvelope } from '../src/net/protocol';
import { fmtTick, parseSeed } from '../src/ui/util';
import type { GameState, Snapshot } from '../src/net/protocol';

function baseState(): GameState {
  return {
    tick: 0,
    seq: 0,
    net: {
      seed: 1,
      hosts: [
        {
          id: 'web-01',
          ip: '10.0.1.11',
          zone: 'dmz',
          role: 'web',
          value: 4,
          jewel: false,
          agent: true,
          services: [],
          x: 0,
          y: 0,
        },
      ],
      ids: ['dmz'],
      attacker: 'apt',
      attackerIps: ['203.0.113.5'],
    },
    damage: { compromiseSec: 0, downtimeSec: 0, adjust: 0 },
    hosts: {
      'web-01': {
        compromised: false,
        compromiseAt: 0,
        persistent: false,
        isolated: false,
        downUntil: 0,
        agentAlive: true,
        hardened: {},
        investigated: false,
        suspicion: 0,
      },
    },
    blocked: {},
    idsAlive: { dmz: true },
    atk: { ip: '203.0.113.5', owned: [], action: null, exfil: 0, gaveUp: false, firstMove: -1 },
    alerts: [],
    logs: [],
    jobs: [],
    term: [],
    outcome: '',
    firstDetect: -1,
    firstAction: -1,
    mistakes: [],
  };
}

function snap(s: GameState): Snapshot {
  return { seq: 0, tick: 0, paused: false, speed: 1, state: s };
}

describe('Store', () => {
  it('applies a compromise delta', () => {
    const store = new Store();
    store.applySnapshot(snap(baseState()));
    store.applyDelta({
      since: 0,
      seq: 1,
      tick: 5,
      events: [{ seq: 1, tick: 5, type: 'atk.own', host: 'web-01', peer: '' }],
    });
    expect(store.state?.hosts['web-01']?.compromised).toBe(true);
    expect(store.state?.atk.owned).toContain('web-01');
  });

  it('ignores stale deltas (seq <= lastSeq)', () => {
    const store = new Store();
    store.applySnapshot(snap(baseState()));
    store.lastSeq = 10;
    store.applyDelta({
      since: 0,
      seq: 3,
      tick: 3,
      events: [{ seq: 3, tick: 3, type: 'atk.own', host: 'web-01' }],
    });
    expect(store.state?.hosts['web-01']?.compromised).toBe(false);
  });

  it('records a visual flash on an attacker action', () => {
    const store = new Store();
    store.applySnapshot(snap(baseState()));
    store.applyDelta({
      since: 0,
      seq: 1,
      tick: 2,
      events: [
        { seq: 1, tick: 2, type: 'atk.action', text: 'exploit', host: 'web-01', peer: '', n: 20 },
      ],
    });
    expect(store.flashes.length).toBe(1);
    expect(store.flashes[0]?.kind).toBe('exploit');
  });

  it('never throws on a malformed delta body', () => {
    const store = new Store();
    store.applySnapshot(snap(baseState()));
    expect(() =>
      store.applyDelta({
        since: 0,
        seq: 1,
        tick: 1,
        events: [{ seq: 1, tick: 1, type: 'bogus-type' }],
      }),
    ).not.toThrow();
  });
});

describe('protocol parseEnvelope', () => {
  it('rejects non-string and bad JSON', () => {
    expect(parseEnvelope(42)).toBeNull();
    expect(parseEnvelope('{not json')).toBeNull();
    expect(parseEnvelope('{"v":1}')).toBeNull(); // no type
    expect(parseEnvelope('{"t":"hello"}')?.t).toBe('hello');
  });
});

describe('util', () => {
  it('formats ticks as mm:ss', () => {
    expect(fmtTick(0)).toBe('00:00');
    expect(fmtTick(615)).toBe('01:01');
    expect(fmtTick(-1)).toBe('--:--');
    expect(fmtTick(NaN)).toBe('--:--');
  });
  it('parses numeric and text seeds deterministically', () => {
    expect(parseSeed('1337')).toBe(1337);
    expect(parseSeed('hello')).toBe(parseSeed('hello'));
    expect(parseSeed('hello')).not.toBe(parseSeed('world'));
    expect(parseSeed('  42  ')).toBe(42);
  });
});
