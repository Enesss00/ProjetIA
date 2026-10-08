package sim

import (
	"fmt"

	"ghostnet/internal/rng"
)

// MaxTicks caps a game at 10 simulated minutes.
const MaxTicks = 10 * 60 * TicksPerSecond

// Engine drives the simulation forward. It owns the authoritative state, a
// seeded RNG and the attacker planner. All state changes go through emitted
// events, so the journal fully describes the game.
type Engine struct {
	st     *State
	rng    *rng.RNG
	plan   *planner
	seq    int
	emitFn func(Event)
}

// Config starts a new game.
type Config struct {
	Net *Network
}

// NewEngine builds an engine for a freshly generated network and emits the
// game.start event. log is called for every event (journal + broadcast).
func NewEngine(cfg Config, log func(Event)) *Engine {
	e := &Engine{st: NewState(), rng: rng.New(cfg.Net.Seed ^ 0x51), emitFn: log}
	e.plan = newPlanner(cfg.Net.Attacker, e.rng.Fork(1))
	start := cfg.Net
	e.emit(Event{Type: EvGameStart, Net: start})
	e.emit(Event{Type: EvAtkIP, IP: cfg.Net.AttackerIPs[0]})
	e.termLines(0, bootLines(cfg.Net))
	return e
}

// State returns the live state (read-only use by the server).
func (e *Engine) State() *State { return e.st }

func (e *Engine) emit(ev Event) {
	e.seq++
	ev.Seq = e.seq
	ev.Tick = e.st.Tick
	e.st.Apply(ev)
	if e.emitFn != nil {
		e.emitFn(ev)
	}
}

// Tick advances the simulation by one tick. It returns false once the game
// has ended.
func (e *Engine) Tick() bool {
	if e.st.Outcome != OutcomeRunning {
		return false
	}
	e.st.Tick++

	e.resolveJobs()
	e.resolveAttacker()
	e.plan.step(e)

	if e.st.Tick%TicksPerSecond == 0 {
		e.emit(Event{Type: EvSecond})
	}
	e.checkEnd()
	return e.st.Outcome == OutcomeRunning
}

func (e *Engine) checkEnd() {
	switch {
	case e.st.Atk.Exfil >= ExfilTotal:
		e.end(OutcomeBreached)
	case e.st.Atk.GaveUp:
		e.end(OutcomeEvicted)
	case e.st.Tick >= MaxTicks:
		e.end(OutcomeDefended)
	}
}

func (e *Engine) end(outcome string) {
	switch outcome {
	case OutcomeDefended:
		e.emit(Event{Type: EvScore, N: 400, Text: "network held for the full shift"})
	case OutcomeEvicted:
		e.emit(Event{Type: EvScore, N: 600, Text: "attacker evicted"})
	case OutcomeBreached:
		e.emit(Event{Type: EvScore, N: -300, Text: "crown-jewel data exfiltrated"})
	}
	e.emit(Event{Type: EvGameEnd, Text: outcome})
}

// resolveJobs completes any player jobs whose end tick has arrived.
func (e *Engine) resolveJobs() {
	for _, j := range append([]Job(nil), e.st.Jobs...) {
		if j.End > e.st.Tick {
			continue
		}
		switch j.Kind {
		case "scan-net":
			e.finishScanNet(j)
		case "scan-host":
			e.finishScanHost(j)
		case "patch":
			e.finishPatch(j)
		case "restart":
			e.finishRestart(j)
		case "forensics":
			e.finishForensics(j)
		}
		e.emit(Event{Type: EvJobEnd, N: j.CmdID})
	}
}

// Attacker side ----------------------------------------------------------

func (e *Engine) resolveAttacker() {
	a := e.st.Atk.Action
	if a == nil || a.End > e.st.Tick {
		return
	}
	// Validate the action still makes sense (target may have been isolated,
	// blocked, patched, or the source foothold lost).
	if !e.actionStillValid(a) {
		e.emit(Event{Type: EvAtkFail, Host: a.Target, Text: a.Kind})
		return
	}
	pSucc, pDet := e.plan.odds(e.st, a)
	detected := e.rng.Chance(pDet)
	success := e.rng.Chance(pSucc)

	switch a.Kind {
	case "recon":
		e.resolveRecon(a, detected)
	case "exploit", "bruteforce":
		e.resolveInitial(a, success, detected)
	case "lateral":
		e.resolveLateral(a, success, detected)
	case "persist":
		e.resolvePersist(a, detected)
	case "sensor":
		e.resolveSensor(a, success, detected)
	case "exfil":
		e.resolveExfil(a, success, detected)
	}
}

func (e *Engine) actionStillValid(a *AtkAction) bool {
	if a.Source != "" && !e.st.IsOwned(a.Source) {
		return false
	}
	if a.Source != "" && !e.st.Up(a.Source) {
		return false
	}
	switch a.Kind {
	case "recon", "exploit", "bruteforce", "lateral", "exfil":
		if a.Target != "" && !e.st.Up(a.Target) {
			return false
		}
	}
	if a.Source == "" && e.st.Blocked[e.st.Atk.IP] {
		return false
	}
	if a.Service != "" {
		if rt := e.st.Hosts[a.Target]; rt != nil && rt.Hardened[a.Service] {
			return false
		}
	}
	return true
}

func (e *Engine) resolveRecon(a *AtkAction, detected bool) {
	e.emit(Event{Type: EvAtkLearn, Host: a.Target})
	if detected {
		e.alert(a.Target, fmt.Sprintf("port scan against %s", e.ip(a.Target)), 1, true)
	}
	e.logLine(a.Target, "connection probes on multiple ports")
}

func (e *Engine) resolveInitial(a *AtkAction, success, detected bool) {
	label := "exploit attempt"
	if a.Kind == "bruteforce" {
		label = "repeated auth failures"
	}
	if detected {
		e.alert(a.Target, fmt.Sprintf("%s on %s/%s", label, e.ip(a.Target), a.Service), sev(success), true)
	} else {
		e.logLine(a.Target, label+" on "+a.Service)
	}
	if success {
		e.own(a.Target, a.Source)
	} else {
		e.emit(Event{Type: EvAtkFail, Host: a.Target, Text: a.Kind})
	}
}

func (e *Engine) resolveLateral(a *AtkAction, success, detected bool) {
	if detected {
		e.alert(a.Target, fmt.Sprintf("lateral movement from %s to %s", e.ip(a.Source), e.ip(a.Target)), 2, true)
	} else {
		e.logLine(a.Target, "unusual internal session from "+e.ip(a.Source))
	}
	if success {
		e.own(a.Target, a.Source)
	} else {
		e.emit(Event{Type: EvAtkFail, Host: a.Target, Text: a.Kind})
	}
}

func (e *Engine) resolvePersist(a *AtkAction, detected bool) {
	e.emit(Event{Type: EvAtkPersist, Host: a.Target})
	if detected {
		e.alert(a.Target, "persistence mechanism installed on "+a.Target, 2, true)
	} else {
		e.logLine(a.Target, "new scheduled task created")
	}
}

func (e *Engine) resolveSensor(a *AtkAction, success, detected bool) {
	if !success {
		e.emit(Event{Type: EvAtkFail, Host: a.Target, Text: a.Kind})
		return
	}
	if a.Target != "" {
		e.emit(Event{Type: EvSensorOff, Host: a.Target})
		if detected {
			e.alert(a.Target, "log agent on "+a.Target+" stopped reporting", 2, true)
		}
	} else if a.Service != "" { // IDS zone in Service
		e.emit(Event{Type: EvSensorOff, Text: a.Service})
		if detected {
			e.alert("", "IDS sensor for zone "+a.Service+" went silent", 2, true)
		}
	}
}

func (e *Engine) resolveExfil(a *AtkAction, success, detected bool) {
	if !success {
		e.emit(Event{Type: EvAtkFail, Host: a.Target, Text: a.Kind})
		return
	}
	units := 12 + e.rng.Intn(10)
	e.emit(Event{Type: EvAtkExfil, N: units})
	if detected {
		e.alert(a.Target, fmt.Sprintf("large outbound transfer from %s (%d%%)", a.Target, e.st.Atk.Exfil), 3, true)
	} else {
		e.logLine(a.Target, "outbound data flow to external host")
	}
}

func (e *Engine) own(target, source string) {
	e.emit(Event{Type: EvAtkOwn, Host: target, Peer: source})
	e.logLine(target, "successful authenticated session established")
}

// Helpers ----------------------------------------------------------------

func (e *Engine) alert(host, text string, severity int, truePositive bool) {
	// Suppress alerts when the relevant sensor is dead (fog of war).
	if !e.sensorCovers(host) {
		return
	}
	peer := ""
	if truePositive {
		peer = "tp"
	}
	e.emit(Event{Type: EvAlert, Host: host, Text: text, N: severity, Peer: peer})
}

// sensorCovers reports whether the player would see telemetry for host.
func (e *Engine) sensorCovers(host string) bool {
	if host == "" {
		return true // IDS-level notice; handled by caller context
	}
	h, rt := e.st.Host(host)
	if h == nil {
		return false
	}
	if rt.AgentAlive {
		return true
	}
	return e.st.IDSAlive[string(h.Zone)]
}

func (e *Engine) logLine(host, text string) {
	if host != "" && !e.sensorCovers(host) {
		return
	}
	e.emit(Event{Type: EvLog, Host: host, Text: text})
}

func (e *Engine) ip(id string) string {
	if h, _ := e.st.Host(id); h != nil {
		return h.IP
	}
	return id
}

func sev(success bool) int {
	if success {
		return 3
	}
	return 1
}

func bootLines(n *Network) []Line {
	return []Line{
		{S: StHead, Text: "GHOST NET // SOC terminal v1"},
		{S: StDim, Text: fmt.Sprintf("seed %d — %d hosts — attacker profile: %s", n.Seed, len(n.Hosts), n.Attacker)},
		{S: StNormal, Text: "An intrusion is in progress. Type 'help' for commands, 'status' for the board."},
	}
}

// termLines records terminal output not tied to a specific command.
func (e *Engine) termLines(cmdID int, lines []Line) {
	e.emit(Event{Type: EvTerm, N: cmdID, Lines: lines})
}
