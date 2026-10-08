package sim

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Outcomes of a finished game.
const (
	OutcomeRunning  = ""
	OutcomeDefended = "defended" // timer ran out, data intact
	OutcomeEvicted  = "evicted"  // attacker gave up
	OutcomeBreached = "breached" // crown jewel exfiltrated
	OutcomeAborted  = "aborted"
)

// Limits of a game.
const (
	ExfilTotal  = 100 // progress units to steal the crown jewel
	MaxAlerts   = 500 // retained alerts (older ones are dropped)
	MaxLogs     = 2000
	MaxTermKeep = 400
)

// AtkAction is the attacker action in progress.
type AtkAction struct {
	Kind    string `json:"kind"`
	Target  string `json:"target"`
	Source  string `json:"source"` // host id or "" for internet
	Service string `json:"service,omitempty"`
	Start   int    `json:"start"`
	End     int    `json:"end"`
}

// Attacker is the attacker's state, including what it believes.
type Attacker struct {
	IP        string          `json:"ip"`
	Known     map[string]bool `json:"known"`
	Owned     []string        `json:"owned"` // in compromise order
	Action    *AtkAction      `json:"action,omitempty"`
	Exfil     int             `json:"exfil"`
	Failures  map[string]int  `json:"failures"` // per "kind:target" key
	GaveUp    bool            `json:"gaveUp"`
	FirstMove int             `json:"firstMove"` // tick of first action, -1 if none
}

// Alert is a player-visible detection.
type Alert struct {
	ID       int    `json:"id"`
	Tick     int    `json:"tick"`
	Host     string `json:"host"`
	Text     string `json:"text"`
	Severity int    `json:"sev"`
}

// LogLine is a player-visible log entry.
type LogLine struct {
	Tick int    `json:"tick"`
	Host string `json:"host"`
	Text string `json:"text"`
}

// Job is a player operation in progress.
type Job struct {
	Kind   string `json:"kind"`
	Target string `json:"target"`
	Arg    string `json:"arg,omitempty"`
	CmdID  int    `json:"cmd"`
	Start  int    `json:"start"`
	End    int    `json:"end"`
}

// Damage accumulates score penalties.
type Damage struct {
	CompromiseSec int `json:"compromiseSec"` // sum(value) per second
	DowntimeSec   int `json:"downtimeSec"`   // sum(value) per second
	Adjust        int `json:"adjust"`        // one-shot bonuses/maluses
}

// State is the full simulation state. It is rebuilt by folding events.
type State struct {
	Tick        int                `json:"tick"`
	Seq         int                `json:"seq"`
	Net         Network            `json:"net"`
	Hosts       map[string]*HostRT `json:"hosts"`
	Blocked     map[string]bool    `json:"blocked"`
	IDSAlive    map[string]bool    `json:"idsAlive"`
	Atk         Attacker           `json:"atk"`
	Alerts      []Alert            `json:"alerts"`
	AlertSeq    int                `json:"alertSeq"`
	Logs        []LogLine          `json:"logs"`
	Jobs        []Job              `json:"jobs"`
	Term        []Event            `json:"term"`
	Damage      Damage             `json:"damage"`
	Outcome     string             `json:"outcome"`
	FirstDetect int                `json:"firstDetect"` // tick of first true-positive alert, -1
	FirstAction int                `json:"firstAction"` // tick of first containment action on an owned host, -1
	Mistakes    []string           `json:"mistakes"`
}

// NewState returns an empty state (before game.start).
func NewState() *State {
	return &State{
		Hosts:       map[string]*HostRT{},
		Blocked:     map[string]bool{},
		IDSAlive:    map[string]bool{},
		Atk:         Attacker{Known: map[string]bool{}, Failures: map[string]int{}, FirstMove: -1},
		FirstDetect: -1,
		FirstAction: -1,
	}
}

// Host returns the static and runtime records for id, or nil.
func (s *State) Host(id string) (*Host, *HostRT) {
	i := s.Net.HostIndex(id)
	if i < 0 {
		return nil, nil
	}
	h := &s.Net.Hosts[i]
	return h, s.Hosts[h.ID]
}

// Up reports whether a host is reachable on the network at the current tick.
func (s *State) Up(id string) bool {
	h, rt := s.Host(id)
	if h == nil || rt.Isolated || rt.DownUntil > s.Tick {
		return false
	}
	return !s.Blocked[h.IP]
}

// IsOwned reports whether the attacker holds a foothold on host id.
func (s *State) IsOwned(id string) bool {
	_, rt := s.Host(id)
	return rt != nil && rt.Compromised
}

// Score is the player's current score.
func (s *State) Score() int {
	v := 1000 - s.Damage.CompromiseSec/2 - s.Damage.DowntimeSec/4 - s.Atk.Exfil*6 + s.Damage.Adjust
	if v < 0 {
		return 0
	}
	return v
}

// Hash returns a stable digest of the state, used for determinism checks.
func (s *State) Hash() string {
	b, err := json.Marshal(s) // encoding/json sorts map keys
	if err != nil {
		return "error:" + err.Error()
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Apply folds one event into the state. It must stay pure: no randomness,
// no clock, no I/O. Unknown events are ignored so old journals still load.
func (s *State) Apply(ev Event) {
	if ev.Tick > s.Tick {
		s.Tick = ev.Tick
	}
	s.Seq = ev.Seq
	switch ev.Type {
	case EvGameStart:
		if ev.Net == nil {
			return
		}
		s.Net = *ev.Net
		for _, h := range s.Net.Hosts {
			s.Hosts[h.ID] = &HostRT{AgentAlive: h.Agent, Hardened: map[string]bool{}}
		}
		for _, z := range s.Net.IDS {
			s.IDSAlive[string(z)] = true
		}
	case EvGameEnd:
		s.Outcome = ev.Text
	case EvCmd:
		s.pushTerm(ev)
	case EvTerm:
		s.pushTerm(ev)
	case EvJobStart:
		s.Jobs = append(s.Jobs, Job{Kind: ev.Text, Target: ev.Host, Arg: ev.Service, CmdID: ev.N, Start: ev.Tick, End: ev.Tick + jobDuration(ev.Text)})
	case EvJobEnd:
		for i, j := range s.Jobs {
			if j.CmdID == ev.N {
				s.Jobs = append(s.Jobs[:i], s.Jobs[i+1:]...)
				break
			}
		}
	case EvIsolate:
		if rt := s.Hosts[ev.Host]; rt != nil {
			rt.Isolated = true
			s.containment(ev)
		}
	case EvUnisolate:
		if rt := s.Hosts[ev.Host]; rt != nil {
			rt.Isolated = false
		}
	case EvDown:
		if rt := s.Hosts[ev.Host]; rt != nil && ev.N > rt.DownUntil {
			rt.DownUntil = ev.N
		}
	case EvHarden:
		if rt := s.Hosts[ev.Host]; rt != nil {
			rt.Hardened[ev.Service] = true
		}
	case EvInvestigat:
		if rt := s.Hosts[ev.Host]; rt != nil {
			rt.Investigated = true
		}
	case EvBlock:
		s.Blocked[ev.IP] = true
		if ev.IP == s.Atk.IP && s.FirstAction < 0 && s.Atk.FirstMove >= 0 {
			s.FirstAction = ev.Tick
		}
	case EvUnblock:
		delete(s.Blocked, ev.IP)
	case EvSensorOff, EvSensorOn:
		if ev.Type == EvSensorOff {
			s.Atk.Action = nil
		}
		on := ev.Type == EvSensorOn
		if ev.Host != "" {
			if rt := s.Hosts[ev.Host]; rt != nil {
				rt.AgentAlive = on
			}
		} else if ev.Text != "" {
			s.IDSAlive[ev.Text] = on
		}
	case EvAtkIP:
		s.Atk.IP = ev.IP
	case EvAtkAction:
		s.Atk.Action = &AtkAction{Kind: ev.Text, Target: ev.Host, Source: ev.Peer, Service: ev.Service, Start: ev.Tick, End: ev.N}
		if s.Atk.FirstMove < 0 {
			s.Atk.FirstMove = ev.Tick
		}
	case EvAtkLearn:
		s.Atk.Known[ev.Host] = true
		s.Atk.Action = nil
	case EvAtkFail:
		s.Atk.Action = nil
		s.Atk.Failures[ev.Text+":"+ev.Host]++
	case EvAtkOwn:
		s.Atk.Action = nil
		if rt := s.Hosts[ev.Host]; rt != nil && !rt.Compromised {
			rt.Compromised = true
			rt.CompromiseAt = ev.Tick
			s.Atk.Owned = append(s.Atk.Owned, ev.Host)
		}
	case EvAtkPersist:
		s.Atk.Action = nil
		if rt := s.Hosts[ev.Host]; rt != nil {
			rt.Persistent = true
		}
	case EvAtkLost:
		if rt := s.Hosts[ev.Host]; rt != nil {
			rt.Compromised = false
			rt.Persistent = false
		}
		for i, id := range s.Atk.Owned {
			if id == ev.Host {
				s.Atk.Owned = append(s.Atk.Owned[:i], s.Atk.Owned[i+1:]...)
				break
			}
		}
		if a := s.Atk.Action; a != nil && (a.Source == ev.Host || a.Target == ev.Host) {
			s.Atk.Action = nil
		}
	case EvAtkExfil:
		s.Atk.Action = nil
		s.Atk.Exfil += ev.N
		if s.Atk.Exfil > ExfilTotal {
			s.Atk.Exfil = ExfilTotal
		}
	case EvAtkGiveUp:
		s.Atk.GaveUp = true
		s.Atk.Action = nil
	case EvAlert:
		s.AlertSeq++
		s.Alerts = append(s.Alerts, Alert{ID: s.AlertSeq, Tick: ev.Tick, Host: ev.Host, Text: ev.Text, Severity: ev.N})
		if len(s.Alerts) > MaxAlerts {
			s.Alerts = s.Alerts[len(s.Alerts)-MaxAlerts:]
		}
		if rt := s.Hosts[ev.Host]; rt != nil {
			rt.Suspicion++
		}
		if s.FirstDetect < 0 && ev.Peer == "tp" {
			s.FirstDetect = ev.Tick
		}
	case EvLog:
		s.Logs = append(s.Logs, LogLine{Tick: ev.Tick, Host: ev.Host, Text: ev.Text})
		if len(s.Logs) > MaxLogs {
			s.Logs = s.Logs[len(s.Logs)-MaxLogs:]
		}
	case EvSecond:
		for _, h := range s.Net.Hosts {
			rt := s.Hosts[h.ID]
			if rt.Compromised {
				s.Damage.CompromiseSec += h.Value
			}
			if rt.Isolated || rt.DownUntil > ev.Tick {
				s.Damage.DowntimeSec += h.Value
			}
		}
	case EvScore:
		s.Damage.Adjust += ev.N
		if ev.N < 0 && ev.Text != "" {
			s.Mistakes = append(s.Mistakes, fmt.Sprintf("[%s] %s", FmtTick(ev.Tick), ev.Text))
		}
	}
}

func (s *State) containment(ev Event) {
	if rt := s.Hosts[ev.Host]; rt != nil && rt.Compromised && s.FirstAction < 0 {
		s.FirstAction = ev.Tick
	}
}

func (s *State) pushTerm(ev Event) {
	s.Term = append(s.Term, ev)
	if len(s.Term) > MaxTermKeep {
		s.Term = s.Term[len(s.Term)-MaxTermKeep:]
	}
}

// FmtTick renders a tick as mm:ss game time.
func FmtTick(t int) string {
	if t < 0 {
		return "--:--"
	}
	sec := t / TicksPerSecond
	return fmt.Sprintf("%02d:%02d", sec/60, sec%60)
}
