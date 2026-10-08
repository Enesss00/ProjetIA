import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import type { EventMsg, Line, Style } from '../net/protocol';

const ANSI: Record<Style, string> = {
  '': '\x1b[38;2;200;230;240m',
  dim: '\x1b[38;2;110;140;155m',
  ok: '\x1b[38;2;80;230;170m',
  warn: '\x1b[38;2;255;190;70m',
  err: '\x1b[38;2;255;80;90m',
  accent: '\x1b[38;2;120;200;255m',
  head: '\x1b[1;38;2;0;229;255m',
};
const RESET = '\x1b[0m';

export interface TerminalHandlers {
  onCommand: (line: string) => void;
}

/**
 * GameTerminal wraps xterm.js. It renders server-sent styled lines (the server
 * never sends raw escape sequences; we map semantic styles to colours here)
 * and provides a local line editor with history. All colouring is applied on
 * the client, so untrusted content can never inject escapes.
 */
export class GameTerminal {
  private term: Terminal;
  private fit: FitAddon;
  private input = '';
  private history: string[] = [];
  private histIdx = -1;
  private h: TerminalHandlers;
  private cmdId = 0;
  private prompt = '\x1b[38;2;0;229;255msoc\x1b[0m:\x1b[38;2;139;92;255m~\x1b[0m$ ';

  constructor(el: HTMLElement, h: TerminalHandlers) {
    this.h = h;
    this.term = new Terminal({
      fontFamily: '"JetBrains Mono", "Fira Code", ui-monospace, monospace',
      fontSize: 13,
      theme: {
        background: '#02060b',
        foreground: '#c8e6f0',
        cursor: '#00e5ff',
        selectionBackground: '#1b4b5a',
      },
      cursorBlink: true,
      scrollback: 2000,
      convertEol: true,
    });
    this.fit = new FitAddon();
    this.term.loadAddon(this.fit);
    this.term.open(el);
    this.safeFit();
    this.term.onData((d) => this.onData(d));
    window.addEventListener('resize', () => this.safeFit());
  }

  private safeFit(): void {
    try {
      this.fit.fit();
    } catch {
      /* terminal not visible yet */
    }
  }

  fitNow(): void {
    this.safeFit();
  }

  writeLines(lines: Line[]): void {
    for (const l of lines) {
      if (l.t === '\x00clear') {
        this.term.clear();
        continue;
      }
      const style = ANSI[(l.s ?? '') as Style] ?? ANSI[''];
      this.term.writeln(style + sanitize(l.t) + RESET);
    }
  }

  /** Handle a terminal event from the server (cmd echo or term output). */
  handleEvent(ev: EventMsg): void {
    if (ev.type === 'cmd') {
      this.term.writeln(this.prompt + sanitize(ev.text ?? ''));
    } else if (ev.lines) {
      this.writeLines(ev.lines);
    }
  }

  banner(lines: Line[]): void {
    this.writeLines(lines);
    this.drawPrompt();
  }

  private drawPrompt(): void {
    this.term.write('\r' + this.prompt + this.input);
  }

  private onData(d: string): void {
    for (const ch of d) {
      const code = ch.codePointAt(0) ?? 0;
      if (ch === '\r') {
        this.submit();
      } else if (ch === '\x7f') {
        if (this.input.length > 0) {
          this.input = this.input.slice(0, -1);
          this.term.write('\b \b');
        }
      } else if (d === '\x1b[A') {
        this.recall(-1);
        return;
      } else if (d === '\x1b[B') {
        this.recall(1);
        return;
      } else if (code === 0x1b) {
        // swallow other escape sequences (arrows handled above)
        return;
      } else if (code >= 0x20 && this.input.length < 1024) {
        this.input += ch;
        this.term.write(ch);
      }
    }
  }

  private recall(dir: number): void {
    if (this.history.length === 0) return;
    if (this.histIdx === -1) this.histIdx = this.history.length;
    this.histIdx = Math.max(0, Math.min(this.history.length, this.histIdx + dir));
    const next = this.history[this.histIdx] ?? '';
    // Clear current line and rewrite.
    this.term.write('\r\x1b[K' + this.prompt + next);
    this.input = next;
  }

  private submit(): void {
    const line = this.input;
    this.term.write('\r\n');
    this.input = '';
    this.histIdx = -1;
    if (line.trim().length > 0) {
      this.history.push(line);
      if (this.history.length > 100) this.history.shift();
      this.cmdId++;
      this.h.onCommand(line);
    } else {
      this.drawPrompt();
    }
  }

  nextCmdId(): number {
    return this.cmdId;
  }

  /** After server output arrives, redraw the prompt so the cursor is ready. */
  readyPrompt(): void {
    this.drawPrompt();
  }
}

/** Strip anything that could be an escape sequence from server/display text. */
function sanitize(s: string): string {
  return s.replace(/[\x00-\x08\x0b-\x1f\x7f]/g, '');
}
