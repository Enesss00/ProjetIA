package sim

import (
	"fmt"
	"sort"
)

// This file holds the player-side effects: the completion of timed jobs and
// the immediate commands. Each returns terminal lines via events. All are
// driven from the command interpreter (command package) through the Engine's
// public methods below, so the server never mutates state directly.

// --- timed job completions ---------------------------------------------

func (e *Engine) finishScanNet(j Job) {
	lines := []Line{{S: StHead, Text: "network scan complete"}}
	hosts := append([]Host(nil), e.st.Net.Hosts...)
	sort.Slice(hosts, func(i, k int) bool { return hosts[i].ID < hosts[k].ID })
	for _, h := range hosts {
		st := "up"
		stStyle := StOK
		if !e.st.Up(h.ID) {
			st, stStyle = "unreachable", StDim
		}
		flag := ""
		if e.st.IsOwned(h.ID) && e.st.Hosts[h.ID].Investigated {
			flag = "  ⚠ compromised"
		}
		lines = append(lines, Line{S: stStyle, Text: fmt.Sprintf("  %-9s %-14s %-5s %-11s %s%s", h.ID, h.IP, string(h.Zone), st, h.Role, flag)})
	}
	e.termLines(j.CmdID, lines)
}

func (e *Engine) finishScanHost(j Job) {
	h, rt := e.st.Host(j.Target)
	if h == nil {
		e.termLines(j.CmdID, []Line{{S: StErr, Text: "scan: host vanished"}})
		return
	}
	lines := []Line{{S: StHead, Text: "host scan: " + h.ID + " (" + h.IP + ")"}}
	lines = append(lines, Line{S: StDim, Text: fmt.Sprintf("  zone=%s role=%s value=%d agent=%v", h.Zone, h.Role, h.Value, rt.AgentAlive)})
	for _, s := range h.Services {
		tag := ""
		st := StNormal
		if rt.Hardened[s.Name] {
			tag, st = "  [patched]", StOK
		} else if s.Vuln {
			tag, st = "  [vulnerable version]", StWarn
		} else if s.Weak {
			tag, st = "  [weak credentials]", StWarn
		}
		lines = append(lines, Line{S: st, Text: fmt.Sprintf("  %-5s :%-5d %s%s", s.Name, s.Port, s.Version, tag)})
	}
	e.termLines(j.CmdID, lines)
}

func (e *Engine) finishPatch(j Job) {
	h, rt := e.st.Host(j.Target)
	if h == nil {
		return
	}
	e.emit(Event{Type: EvHarden, Host: h.ID, Service: j.Arg})
	e.termLines(j.CmdID, []Line{{S: StOK, Text: fmt.Sprintf("patched %s/%s — service hardened", h.ID, j.Arg)}})
	_ = rt
}

func (e *Engine) finishRestart(j Job) {
	h, _ := e.st.Host(j.Target)
	if h == nil {
		return
	}
	// Restarting evicts a non-persistent foothold.
	if e.st.IsOwned(h.ID) && !e.st.Hosts[h.ID].Persistent {
		e.emit(Event{Type: EvAtkLost, Host: h.ID})
		e.termLines(j.CmdID, []Line{{S: StOK, Text: "restarted " + h.ID + " — foothold cleared"}})
	} else if e.st.IsOwned(h.ID) {
		e.termLines(j.CmdID, []Line{{S: StWarn, Text: "restarted " + h.ID + " — attacker persistence survived the reboot"}})
	} else {
		e.termLines(j.CmdID, []Line{{S: StOK, Text: "restarted " + h.ID}})
	}
}

func (e *Engine) finishForensics(j Job) {
	h, rt := e.st.Host(j.Target)
	if h == nil {
		return
	}
	e.emit(Event{Type: EvInvestigat, Host: h.ID})
	lines := []Line{{S: StHead, Text: "forensics: " + h.ID}}
	if rt.Compromised {
		lines = append(lines,
			Line{S: StErr, Text: fmt.Sprintf("  HOST COMPROMISED since %s", FmtTick(rt.CompromiseAt))},
			Line{S: StWarn, Text: "  persistence: " + yn(rt.Persistent)},
		)
		// Reveal the persistence so the player knows restart won't help.
	} else {
		lines = append(lines, Line{S: StOK, Text: "  no active compromise detected"})
	}
	lines = append(lines, Line{S: StDim, Text: fmt.Sprintf("  alerts raised: %d", rt.Suspicion)})
	e.termLines(j.CmdID, lines)
}

func yn(b bool) string {
	if b {
		return "yes (reboot will NOT clear it — reimage/isolate)"
	}
	return "no"
}

// --- immediate commands (invoked by the interpreter) --------------------

// StartJob queues a timed job and echoes a "working" line. It returns false
// if a job of the same kind on the same target is already running.
func (e *Engine) StartJob(kind, target, arg string, cmdID int) bool {
	for _, j := range e.st.Jobs {
		if j.Kind == kind && j.Target == target {
			return false
		}
	}
	e.emit(Event{Type: EvJobStart, Text: kind, Host: target, Service: arg, N: cmdID})
	return true
}

// Isolate cuts a host off the network.
func (e *Engine) Isolate(id string) { e.emit(Event{Type: EvIsolate, Host: id}) }

// Unisolate reconnects a host.
func (e *Engine) Unisolate(id string) { e.emit(Event{Type: EvUnisolate, Host: id}) }

// Block drops traffic from an IP. mistake records a score penalty if the IP
// belongs to a legitimate host (over-blocking causes downtime indirectly).
func (e *Engine) Block(ip string) {
	e.emit(Event{Type: EvBlock, IP: ip})
}

// Unblock re-allows an IP.
func (e *Engine) Unblock(ip string) { e.emit(Event{Type: EvUnblock, IP: ip}) }

// Penalty records a one-shot scoring adjustment with a reason (for the report).
func (e *Engine) Penalty(n int, reason string) { e.emit(Event{Type: EvScore, N: n, Text: reason}) }

// Term emits free-form terminal output for a command id.
func (e *Engine) Term(cmdID int, lines []Line) { e.termLines(cmdID, lines) }

// EchoCmd records the raw command line the player typed.
func (e *Engine) EchoCmd(cmdID int, text string) {
	e.emit(Event{Type: EvCmd, N: cmdID, Text: text})
}

// Seq returns the current event sequence number.
func (e *Engine) Seq() int { return e.seq }
