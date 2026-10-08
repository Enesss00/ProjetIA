import './style.css';
import { GameClient } from './net/client';
import { Store } from './net/store';
import type { Delta, Ended, Snapshot } from './net/protocol';
import { CityScene } from './scene/scene';
import { GameTerminal } from './ui/terminal';
import { Hud } from './ui/hud';
import { Audio } from './ui/audio';
import { parseSeed, seedFromURL } from './ui/util';

function wsURL(): string {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  return `${proto}//${location.host}/ws`;
}

class App {
  private store = new Store();
  private client: GameClient;
  private scene: CityScene;
  private term: GameTerminal | null = null;
  private hud = new Hud();
  private audio = new Audio();
  private started = false;
  private currentSeed = 0;
  private currentProfile = '';

  private menu = document.getElementById('menu')!;
  private game = document.getElementById('game')!;
  private endScreen = document.getElementById('end')!;
  private statusDot = document.getElementById('status-dot')!;

  constructor() {
    const canvas = document.getElementById('scene') as HTMLCanvasElement;
    this.scene = new CityScene(canvas, this.store);

    this.client = new GameClient(wsURL(), {
      onHello: () => undefined,
      onSnapshot: (s) => this.onSnapshot(s),
      onDelta: (d) => this.onDelta(d),
      onEnded: (e) => this.onEnded(e),
      onError: (t) => this.term?.writeLines([{ s: 'err', t: '! ' + t }]),
      onStatus: (c) => this.statusDot.classList.toggle('on', c),
    });

    this.store.onTerm = (ev) => {
      this.term?.handleEvent(ev);
      this.term?.readyPrompt();
    };
    this.store.onChange = () => {
      if (this.store.state) this.hud.update(this.store.state);
    };

    this.wireMenu();
    this.wireControls();
    this.scene.start();
    this.client.connect();

    // Deep-link: ?seed=... starts immediately (challenge mode).
    const link = seedFromURL();
    if (link) {
      (document.getElementById('seed-input') as HTMLInputElement).value = String(link.seed);
      this.startGame(link.seed, link.profile ?? '');
    }
  }

  private wireMenu(): void {
    const seedInput = document.getElementById('seed-input') as HTMLInputElement;
    const profileSel = document.getElementById('profile-select') as HTMLSelectElement;
    document.getElementById('btn-start')!.addEventListener('click', () => {
      this.audio.resume(); // unlock audio on this user gesture
      const seed = parseSeed(seedInput.value || String(Math.floor(Math.random() * 1e9)));
      this.startGame(seed, profileSel.value);
    });
    document.getElementById('btn-random')!.addEventListener('click', () => {
      seedInput.value = String(Math.floor(Math.random() * 1e9));
    });
    document.getElementById('btn-again')!.addEventListener('click', () => {
      this.endScreen.classList.add('hidden');
      this.menu.classList.remove('hidden');
    });
    document.getElementById('btn-replay')!.addEventListener('click', () => this.enterReplay());
  }

  private wireControls(): void {
    document.getElementById('btn-pause')!.addEventListener('click', (e) => {
      this.store.paused = !this.store.paused;
      this.client.pause(this.store.paused);
      (e.target as HTMLElement).textContent = this.store.paused ? '▶ resume' : '⏸ pause';
    });
    for (const sp of [1, 2, 4]) {
      document.getElementById('spd-' + sp)!.addEventListener('click', () => {
        this.client.speed(sp);
        document.querySelectorAll('.spd').forEach((b) => b.classList.remove('active'));
        document.getElementById('spd-' + sp)!.classList.add('active');
      });
    }
    document.getElementById('btn-share')!.addEventListener('click', () => this.share());
    const mute = document.getElementById('btn-mute')!;
    mute.addEventListener('click', () => {
      this.audio.setMuted(!this.audio.isMuted());
      mute.textContent = this.audio.isMuted() ? '🔇 sound' : '🔊 sound';
    });
  }

  private startGame(seed: number, profile: string): void {
    this.currentSeed = seed;
    this.currentProfile = profile;
    this.menu.classList.add('hidden');
    this.endScreen.classList.add('hidden');
    this.game.classList.remove('hidden');
    this.hud.reset();
    this.started = false;
    // Create the terminal only once the game panel is visible, so xterm sees
    // real dimensions and renders correctly.
    if (!this.term) {
      this.term = new GameTerminal(document.getElementById('terminal')!, {
        onCommand: (line) => {
          this.audio.confirm();
          this.client.command(this.term!.nextCmdId(), line);
        },
      });
    }
    this.client.newGame(seed, profile || undefined);
    setTimeout(() => this.term?.fitNow(), 50);
  }

  private onSnapshot(s: Snapshot): void {
    const first = !this.started || this.store.state?.net.seed !== s.state.net.seed;
    this.store.applySnapshot(s);
    if (first) {
      this.scene.setNetwork(s.state);
      this.term?.banner([
        { s: 'head', t: 'GHOST NET // SOC terminal' },
        { s: 'dim', t: `seed ${s.state.net.seed} — attacker ${s.state.net.attacker}` },
        { s: '', t: "type 'help' for commands, 'status' for the board" },
      ]);
      this.started = true;
    }
    this.hud.update(s.state);
  }

  private onDelta(d: Delta): void {
    const lastSeq = this.store.lastSeq;
    this.store.applyDelta(d);
    // Discreet synthetic cues for notable events in this batch.
    for (const ev of d.events) {
      if (ev.seq <= lastSeq) continue;
      switch (ev.type) {
        case 'alert':
          this.audio.alert(ev.n ?? 1);
          break;
        case 'atk.own':
          this.audio.compromise();
          break;
        case 'atk.exfil':
          this.audio.exfil();
          break;
        default:
          break;
      }
    }
  }

  private onEnded(e: Ended): void {
    document.getElementById('end-outcome')!.textContent = outcomeTitle(e.outcome);
    document.getElementById('end-outcome')!.className = 'outcome ' + e.outcome;
    document.getElementById('end-score')!.textContent = 'score ' + e.score;
    document.getElementById('end-report')!.textContent = e.report || '(no report)';
    this.endScreen.classList.remove('hidden');
  }

  private enterReplay(): void {
    this.endScreen.classList.add('hidden');
    this.game.classList.remove('hidden');
    const bar = document.getElementById('replay-bar')!;
    bar.classList.remove('hidden');
    const slider = document.getElementById('replay-slider') as HTMLInputElement;
    const maxTick = this.store.state?.tick ?? 6000;
    slider.max = String(maxTick);
    slider.value = String(maxTick);
    this.scene.setAutoRotate(false);
    slider.oninput = () => this.client.seek(Number(slider.value));
  }

  private share(): void {
    const url = new URL(location.href);
    url.searchParams.set('seed', String(this.currentSeed));
    if (this.currentProfile) url.searchParams.set('profile', this.currentProfile);
    navigator.clipboard?.writeText(url.toString()).then(
      () => this.toast('challenge link copied'),
      () => this.toast(url.toString()),
    );
  }

  private toast(msg: string): void {
    const t = document.getElementById('toast')!;
    t.textContent = msg;
    t.classList.add('show');
    setTimeout(() => t.classList.remove('show'), 2500);
  }
}

function outcomeTitle(o: string): string {
  switch (o) {
    case 'defended':
      return 'SHIFT SURVIVED';
    case 'evicted':
      return 'ATTACKER EVICTED';
    case 'breached':
      return 'NETWORK BREACHED';
    default:
      return o.toUpperCase();
  }
}

new App();
