/** Format a tick count (10 ticks/sec) as mm:ss. */
export function fmtTick(t: number): string {
  if (t < 0 || !Number.isFinite(t)) return '--:--';
  const sec = Math.floor(t / 10);
  const m = Math.floor(sec / 60);
  const s = sec % 60;
  return String(m).padStart(2, '0') + ':' + String(s).padStart(2, '0');
}

/** Parse a seed from arbitrary user text into a non-negative integer. */
export function parseSeed(text: string): number {
  const trimmed = text.trim();
  if (/^\d+$/.test(trimmed)) {
    const n = Number(trimmed);
    if (Number.isSafeInteger(n) && n >= 0) return n;
  }
  // Hash arbitrary strings into a stable 32-bit seed (FNV-1a).
  let h = 0x811c9dc5;
  for (let i = 0; i < trimmed.length; i++) {
    h ^= trimmed.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  return h >>> 0;
}

/** Read a seed from the URL (?seed=...), or null. */
export function seedFromURL(): { seed: number; profile?: string } | null {
  const p = new URLSearchParams(window.location.search);
  const s = p.get('seed');
  if (s === null) return null;
  const out: { seed: number; profile?: string } = { seed: parseSeed(s) };
  const prof = p.get('profile');
  if (prof && ['smash', 'stealth', 'apt'].includes(prof)) out.profile = prof;
  return out;
}
