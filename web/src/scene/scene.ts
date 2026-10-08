import * as THREE from 'three';
import { EffectComposer } from 'three/examples/jsm/postprocessing/EffectComposer.js';
import { RenderPass } from 'three/examples/jsm/postprocessing/RenderPass.js';
import { UnrealBloomPass } from 'three/examples/jsm/postprocessing/UnrealBloomPass.js';
import { OrbitControls } from 'three/examples/jsm/controls/OrbitControls.js';
import type { GameState, Host } from '../net/protocol';
import type { Flash, Store } from '../net/store';

const COLORS = {
  dmz: 0x00e5ff,
  corp: 0x8b5cff,
  srv: 0xff4fd8,
  owned: 0xff2d2d,
  isolated: 0x2a3a44,
  jewel: 0xffd54a,
  grid: 0x0a2330,
};

interface Building {
  host: Host;
  mesh: THREE.Mesh;
  mat: THREE.MeshStandardMaterial;
  base: THREE.Color;
  halo: THREE.Sprite;
  shake: number;
}

/**
 * CityScene renders the network as a neon night city. Each host is a building,
 * coloured by zone; compromise turns it red and spreads a pulse; isolation
 * dims it; the crown jewel glows gold. Attacker actions spawn beams and radar
 * sweeps. The scene reads only from the Store; it owns no game truth.
 */
export class CityScene {
  private renderer: THREE.WebGLRenderer;
  private scene = new THREE.Scene();
  private camera: THREE.PerspectiveCamera;
  private composer: EffectComposer;
  private controls: OrbitControls;
  private buildings = new Map<string, Building>();
  private beams: { line: THREE.Line; life: number; max: number }[] = [];
  private sweeps: { ring: THREE.Mesh; life: number }[] = [];
  private store: Store;
  private clock = new THREE.Clock();
  private raf = 0;
  private group = new THREE.Group();

  constructor(canvas: HTMLCanvasElement, store: Store) {
    this.store = store;
    this.renderer = new THREE.WebGLRenderer({
      canvas,
      antialias: true,
      powerPreference: 'high-performance',
    });
    this.renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
    this.scene.fog = new THREE.FogExp2(0x02060b, 0.018);

    this.camera = new THREE.PerspectiveCamera(55, 1, 0.1, 500);
    this.camera.position.set(0, 24, 34);

    this.controls = new OrbitControls(this.camera, canvas);
    this.controls.enableDamping = true;
    this.controls.dampingFactor = 0.08;
    this.controls.maxPolarAngle = Math.PI / 2.1;
    this.controls.minDistance = 10;
    this.controls.maxDistance = 90;
    this.controls.autoRotate = true;
    this.controls.autoRotateSpeed = 0.35;

    this.scene.add(this.group);
    this.buildLights();
    this.buildGround();

    const rt = new RenderPass(this.scene, this.camera);
    const bloom = new UnrealBloomPass(new THREE.Vector2(1, 1), 0.9, 0.6, 0.18);
    this.composer = new EffectComposer(this.renderer);
    this.composer.addPass(rt);
    this.composer.addPass(bloom);

    this.resize();
    window.addEventListener('resize', this.resize);
  }

  private buildLights(): void {
    this.scene.add(new THREE.AmbientLight(0x223344, 1.1));
    const key = new THREE.DirectionalLight(0x4466aa, 0.6);
    key.position.set(10, 30, 10);
    this.scene.add(key);
  }

  private buildGround(): void {
    const grid = new THREE.GridHelper(120, 60, COLORS.grid, COLORS.grid);
    (grid.material as THREE.Material).opacity = 0.35;
    (grid.material as THREE.Material).transparent = true;
    this.scene.add(grid);
    const floor = new THREE.Mesh(
      new THREE.PlaneGeometry(120, 120),
      new THREE.MeshStandardMaterial({ color: 0x02060b, roughness: 1, metalness: 0 }),
    );
    floor.rotation.x = -Math.PI / 2;
    floor.position.y = -0.02;
    this.scene.add(floor);
  }

  /** (Re)build the city from a fresh network. */
  setNetwork(state: GameState): void {
    for (const b of this.buildings.values()) {
      this.group.remove(b.mesh);
      this.group.remove(b.halo);
    }
    this.buildings.clear();
    this.beams.forEach((b) => this.scene.remove(b.line));
    this.beams = [];

    for (const host of state.net.hosts) {
      const h = 2 + host.value * 0.7;
      const geo = new THREE.BoxGeometry(2.2, h, 2.2);
      const base = new THREE.Color(COLORS[host.zone as keyof typeof COLORS] ?? 0x3388aa);
      const mat = new THREE.MeshStandardMaterial({
        color: base,
        emissive: base,
        emissiveIntensity: 0.5,
        roughness: 0.35,
        metalness: 0.6,
      });
      const mesh = new THREE.Mesh(geo, mat);
      mesh.position.set(host.x, h / 2, host.y);
      this.group.add(mesh);

      const halo = this.makeHalo(host.jewel ? COLORS.jewel : base.getHex());
      halo.position.set(host.x, h + 1.2, host.y);
      halo.scale.setScalar(host.jewel ? 3 : 1.8);
      this.group.add(halo);

      this.buildings.set(host.id, { host, mesh, mat, base, halo, shake: 0 });
    }
  }

  private makeHalo(color: number): THREE.Sprite {
    const c = document.createElement('canvas');
    c.width = c.height = 64;
    const ctx = c.getContext('2d')!;
    const g = ctx.createRadialGradient(32, 32, 0, 32, 32, 32);
    const col = new THREE.Color(color);
    g.addColorStop(0, `rgba(${col.r * 255},${col.g * 255},${col.b * 255},0.9)`);
    g.addColorStop(1, 'rgba(0,0,0,0)');
    ctx.fillStyle = g;
    ctx.fillRect(0, 0, 64, 64);
    const tex = new THREE.CanvasTexture(c);
    const mat = new THREE.SpriteMaterial({
      map: tex,
      blending: THREE.AdditiveBlending,
      depthWrite: false,
    });
    return new THREE.Sprite(mat);
  }

  private spawnBeam(from: THREE.Vector3, to: THREE.Vector3, color: number, life: number): void {
    const mid = from.clone().lerp(to, 0.5);
    mid.y += 6;
    const curve = new THREE.QuadraticBezierCurve3(from, mid, to);
    const geo = new THREE.BufferGeometry().setFromPoints(curve.getPoints(24));
    const mat = new THREE.LineBasicMaterial({
      color,
      transparent: true,
      opacity: 1,
      blending: THREE.AdditiveBlending,
    });
    const line = new THREE.Line(geo, mat);
    this.scene.add(line);
    this.beams.push({ line, life, max: life });
  }

  private spawnSweep(center: THREE.Vector3, color: number): void {
    const ring = new THREE.Mesh(
      new THREE.RingGeometry(0.4, 0.7, 48),
      new THREE.MeshBasicMaterial({
        color,
        transparent: true,
        opacity: 0.9,
        side: THREE.DoubleSide,
        blending: THREE.AdditiveBlending,
      }),
    );
    ring.rotation.x = -Math.PI / 2;
    ring.position.copy(center);
    ring.position.y = 0.2;
    this.scene.add(ring);
    this.sweeps.push({ ring, life: 1 });
  }

  private pos(id: string): THREE.Vector3 | null {
    const b = this.buildings.get(id);
    if (!b) return null;
    return new THREE.Vector3(b.host.x, 1, b.host.y);
  }

  private consumeFlashes(): void {
    for (const f of this.store.flashes) {
      this.onFlash(f);
    }
    this.store.flashes = [];
  }

  private onFlash(f: Flash): void {
    const target = this.pos(f.target);
    const source = f.source ? this.pos(f.source) : new THREE.Vector3(-26, 1, -18);
    switch (f.kind) {
      case 'recon':
        if (target) this.spawnSweep(target, COLORS.dmz);
        break;
      case 'exploit':
      case 'bruteforce':
        if (target) {
          const b = this.buildings.get(f.target);
          if (b) b.shake = 1;
          if (source) this.spawnBeam(source, target, 0xff8a3d, 1.2);
        }
        break;
      case 'lateral':
        if (source && target) this.spawnBeam(source, target, COLORS.owned, 1.4);
        break;
      case 'exfil':
        if (target) this.spawnBeam(target, new THREE.Vector3(-26, 1, -18), COLORS.jewel, 1.6);
        break;
      case 'sensor':
        if (target) this.spawnSweep(target, 0xff2d2d);
        break;
      default:
        break;
    }
  }

  private syncBuildings(): void {
    const s = this.store.state;
    if (!s) return;
    for (const [id, b] of this.buildings) {
      const rt = s.hosts[id];
      if (!rt) continue;
      let col = b.base;
      let emissive = 0.5;
      if (rt.isolated) {
        col = new THREE.Color(COLORS.isolated);
        emissive = 0.15;
      } else if (rt.compromised) {
        col = new THREE.Color(COLORS.owned);
        emissive = 0.9 + 0.5 * Math.sin(this.clock.elapsedTime * 6);
      } else if (rt.suspicion > 0) {
        col = b.base.clone().lerp(new THREE.Color(0xffae42), 0.4);
        emissive = 0.7;
      }
      b.mat.color.lerp(col, 0.1);
      b.mat.emissive.lerp(col, 0.1);
      b.mat.emissiveIntensity += (emissive - b.mat.emissiveIntensity) * 0.2;
      // Brute-force vibration.
      if (b.shake > 0) {
        b.mesh.position.x = b.host.x + (Math.random() - 0.5) * 0.25 * b.shake;
        b.mesh.position.z = b.host.y + (Math.random() - 0.5) * 0.25 * b.shake;
        b.shake = Math.max(0, b.shake - 0.02);
      } else {
        b.mesh.position.x = b.host.x;
        b.mesh.position.z = b.host.y;
      }
    }
  }

  start(): void {
    const loop = () => {
      this.raf = requestAnimationFrame(loop);
      const dt = this.clock.getDelta();
      this.consumeFlashes();
      this.syncBuildings();
      // Age beams.
      for (const beam of this.beams) {
        beam.life -= dt;
        (beam.line.material as THREE.LineBasicMaterial).opacity = Math.max(0, beam.life / beam.max);
      }
      this.beams = this.beams.filter((b) => {
        if (b.life <= 0) {
          this.scene.remove(b.line);
          b.line.geometry.dispose();
          return false;
        }
        return true;
      });
      // Expand sweeps.
      for (const sw of this.sweeps) {
        sw.life -= dt * 1.2;
        sw.ring.scale.setScalar(1 + (1 - sw.life) * 14);
        (sw.ring.material as THREE.MeshBasicMaterial).opacity = Math.max(0, sw.life);
      }
      this.sweeps = this.sweeps.filter((sw) => {
        if (sw.life <= 0) {
          this.scene.remove(sw.ring);
          sw.ring.geometry.dispose();
          return false;
        }
        return true;
      });
      this.controls.update();
      this.composer.render();
    };
    loop();
  }

  setAutoRotate(on: boolean): void {
    this.controls.autoRotate = on;
  }

  private resize = (): void => {
    const canvas = this.renderer.domElement;
    const w = canvas.clientWidth || window.innerWidth;
    const h = canvas.clientHeight || window.innerHeight;
    this.renderer.setSize(w, h, false);
    this.composer.setSize(w, h);
    this.camera.aspect = w / h;
    this.camera.updateProjectionMatrix();
  };

  dispose(): void {
    cancelAnimationFrame(this.raf);
    window.removeEventListener('resize', this.resize);
    this.renderer.dispose();
  }
}
