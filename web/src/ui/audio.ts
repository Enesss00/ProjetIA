// A tiny synthetic sound engine built on the Web Audio API. No samples, no
// assets: every sound is generated from oscillators, so it stays discreet and
// adds no network weight. Audio is created lazily on the first user gesture
// (browsers block audio before interaction) and can be muted. Every call is
// guarded so a missing/!blocked AudioContext never throws into the game loop.

export class Audio {
  private ctx: AudioContext | null = null;
  private master: GainNode | null = null;
  private muted = false;

  /** Must be called from a user gesture to unlock audio. Idempotent. */
  resume(): void {
    try {
      if (!this.ctx) {
        const Ctor =
          window.AudioContext ??
          (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
        if (!Ctor) return;
        this.ctx = new Ctor();
        this.master = this.ctx.createGain();
        this.master.gain.value = this.muted ? 0 : 0.5;
        this.master.connect(this.ctx.destination);
        this.startHum();
      }
      void this.ctx.resume();
    } catch {
      /* audio unavailable; game continues silently */
    }
  }

  setMuted(m: boolean): void {
    this.muted = m;
    if (this.master && this.ctx) {
      try {
        this.master.gain.setTargetAtTime(m ? 0 : 0.5, this.ctx.currentTime, 0.05);
      } catch {
        /* ignore */
      }
    }
  }

  isMuted(): boolean {
    return this.muted;
  }

  /** A short blip whose pitch rises with alert severity. */
  alert(severity: number): void {
    const freq = severity >= 3 ? 220 : severity === 2 ? 440 : 660;
    this.blip(freq, severity >= 3 ? 'sawtooth' : 'square', 0.12, 0.08);
    if (severity >= 3) this.blip(freq * 1.5, 'sawtooth', 0.12, 0.12);
  }

  /** A descending glitch when a host is compromised. */
  compromise(): void {
    this.sweep(600, 120, 0.3, 0.1);
  }

  /** A tense pulse as data leaves the network. */
  exfil(): void {
    this.blip(140, 'triangle', 0.18, 0.1);
  }

  /** A soft confirm for a successful defensive action. */
  confirm(): void {
    this.blip(880, 'sine', 0.08, 0.06);
  }

  private startHum(): void {
    if (!this.ctx || !this.master) return;
    try {
      const osc = this.ctx.createOscillator();
      const g = this.ctx.createGain();
      osc.type = 'sine';
      osc.frequency.value = 55;
      g.gain.value = 0.04;
      osc.connect(g);
      g.connect(this.master);
      osc.start();
    } catch {
      /* ignore */
    }
  }

  private blip(freq: number, type: OscillatorType, dur: number, gain: number): void {
    if (!this.ctx || !this.master || this.muted) return;
    try {
      const t = this.ctx.currentTime;
      const osc = this.ctx.createOscillator();
      const g = this.ctx.createGain();
      osc.type = type;
      osc.frequency.setValueAtTime(freq, t);
      g.gain.setValueAtTime(0, t);
      g.gain.linearRampToValueAtTime(gain, t + 0.005);
      g.gain.exponentialRampToValueAtTime(0.0001, t + dur);
      osc.connect(g);
      g.connect(this.master);
      osc.start(t);
      osc.stop(t + dur + 0.02);
    } catch {
      /* ignore */
    }
  }

  private sweep(from: number, to: number, dur: number, gain: number): void {
    if (!this.ctx || !this.master || this.muted) return;
    try {
      const t = this.ctx.currentTime;
      const osc = this.ctx.createOscillator();
      const g = this.ctx.createGain();
      osc.type = 'sawtooth';
      osc.frequency.setValueAtTime(from, t);
      osc.frequency.exponentialRampToValueAtTime(to, t + dur);
      g.gain.setValueAtTime(gain, t);
      g.gain.exponentialRampToValueAtTime(0.0001, t + dur);
      osc.connect(g);
      g.connect(this.master);
      osc.start(t);
      osc.stop(t + dur + 0.02);
    } catch {
      /* ignore */
    }
  }
}
