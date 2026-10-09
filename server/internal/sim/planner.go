package sim

import "ghostnet/internal/rng"

// planner is the attacker brain. On each tick it, if idle, scores the set of
// currently-available actions over the attack graph and commits to the best
// one. When a path is blocked (foothold lost, host isolated, service patched)
// the next planning step naturally picks a different action — that is the
// replanning. Profiles bias the scoring toward speed, stealth or patience.
type planner struct {
	profile  string
	rng      *rng.RNG
	cooldown int // ticks before the attacker may act again
	patience int // consecutive failed plans before giving up
}

func newPlanner(profile string, r *rng.RNG) *planner {
	p := &planner{profile: profile, rng: r}
	// Patience is how many seconds the attacker keeps probing for a new way in
	// once fully contained before giving up. Kept short so a player who locks
	// the attacker out gets a decisive VICTORY quickly instead of dead air.
	switch profile {
	case "smash":
		p.patience = 5
	case "stealth":
		p.patience = 7
	default: // apt
		p.patience = 8
	}
	return p
}

type candidate struct {
	a     AtkAction
	score float64
}

// step runs one planning tick.
func (p *planner) step(e *Engine) {
	st := e.st
	if st.Atk.Action != nil || st.Atk.GaveUp {
		return
	}
	if p.cooldown > 0 {
		p.cooldown--
		return
	}
	cands := p.enumerate(st)
	if len(cands) == 0 {
		// No move available: the attacker is contained. Make the search for a
		// new way in VISIBLE (perimeter probing shows in the alert feed, even
		// if host sensors are down) so the player sees it is winning, then it
		// gives up and the game ends in a decisive victory.
		e.emit(Event{Type: EvAlert, Host: "", Text: "attacker probing the perimeter for a new way in — containment holding", N: 1})
		p.patience--
		if p.patience <= 0 {
			e.emit(Event{Type: EvAtkGiveUp})
			return
		}
		p.cooldown = TicksPerSecond
		return
	}
	best := cands[0]
	for _, c := range cands[1:] {
		if c.score > best.score {
			best = c
		}
	}
	// Small seeded jitter avoids lockstep and keeps replays varied but exact.
	best.a.Start = st.Tick
	best.a.End = st.Tick + p.duration(best.a.Kind)
	e.emit(Event{Type: EvAtkAction, Text: best.a.Kind, Host: best.a.Target, Peer: best.a.Source, Service: best.a.Service, N: best.a.End})
	p.cooldown = p.gap()
}

// enumerate lists every action the attacker can attempt right now.
func (p *planner) enumerate(st *State) []candidate {
	var out []candidate
	owned := st.Atk.Owned
	hasFoothold := len(owned) > 0

	// 1. Recon from the internet on DMZ hosts not yet known.
	for i := range st.Net.Hosts {
		h := &st.Net.Hosts[i]
		if h.Zone == ZoneDMZ && !st.Atk.Known[h.ID] && st.Up(h.ID) && !st.Blocked[st.Atk.IP] {
			out = append(out, p.weigh(st, AtkAction{Kind: "recon", Target: h.ID, Source: ""}))
		}
	}
	// 2. Initial access on known-but-unowned DMZ hosts.
	for i := range st.Net.Hosts {
		h := &st.Net.Hosts[i]
		if h.Zone != ZoneDMZ || st.IsOwned(h.ID) || !st.Atk.Known[h.ID] || !st.Up(h.ID) || st.Blocked[st.Atk.IP] {
			continue
		}
		if svc, kind := p.pickEntry(st, h); svc != "" {
			out = append(out, p.weigh(st, AtkAction{Kind: kind, Target: h.ID, Source: "", Service: svc}))
		}
	}
	// 3. From each foothold: recon + lateral + persist + sensor + exfil.
	for _, src := range owned {
		sh, _ := st.Host(src)
		if sh == nil || !st.Up(src) {
			continue
		}
		srt := st.Hosts[src]
		if !srt.Persistent {
			out = append(out, p.weigh(st, AtkAction{Kind: "persist", Target: src, Source: src}))
		}
		// Disable the agent on this host to blind the defender.
		if srt.AgentAlive {
			out = append(out, p.weigh(st, AtkAction{Kind: "sensor", Target: src, Source: src}))
		}
		for i := range st.Net.Hosts {
			t := &st.Net.Hosts[i]
			if t.ID == src || st.IsOwned(t.ID) || !Reachable(sh.Zone, t.Zone) || !st.Up(t.ID) {
				continue
			}
			if svc, kind := p.pickEntry(st, t); svc != "" {
				k := "lateral"
				_ = kind
				out = append(out, p.weigh(st, AtkAction{Kind: k, Target: t.ID, Source: src, Service: svc}))
			}
		}
		// Exfil if sitting on the crown jewel.
		if sh.Jewel {
			out = append(out, p.weigh(st, AtkAction{Kind: "exfil", Target: src, Source: src}))
		}
	}
	// 4. Blind the zone IDS (stealth/apt only, once foothold exists).
	if hasFoothold && p.profile != "smash" {
		for _, z := range st.Net.IDS {
			if st.IDSAlive[string(z)] {
				out = append(out, p.weigh(st, AtkAction{Kind: "sensor", Target: "", Source: owned[len(owned)-1], Service: string(z)}))
			}
		}
	}
	return out
}

// pickEntry chooses a service to attack on host h and the action kind, or ""
// if nothing is viable. It prefers unpatched flawed services.
func (p *planner) pickEntry(st *State, h *Host) (string, string) {
	rt := st.Hosts[h.ID]
	var weak, vuln, any string
	for _, s := range h.Services {
		if rt.Hardened[s.Name] {
			continue
		}
		any = s.Name
		if s.Weak && weak == "" {
			weak = s.Name
		}
		if s.Vuln && vuln == "" {
			vuln = s.Name
		}
	}
	switch {
	case vuln != "":
		return vuln, "exploit"
	case weak != "":
		return weak, "bruteforce"
	case any != "" && p.profile == "smash":
		// A smash attacker will still hammer a hardened-looking service.
		return any, "bruteforce"
	}
	return "", ""
}

// weigh assigns a score to a candidate action, blending profile preference,
// target value, detection risk and how many times it already failed.
func (p *planner) weigh(st *State, a AtkAction) candidate {
	pSucc, pDet := p.odds(st, &a)
	base := pSucc
	val := 1.0
	if h, _ := st.Host(a.Target); h != nil {
		val = 0.4 + float64(h.Value)/10
		if h.Jewel {
			val += 0.8
		}
	}
	risk := pDet
	var w float64
	switch p.profile {
	case "smash":
		w = base*1.3 + val*1.2 - risk*0.1
	case "stealth":
		w = base*0.9 + val*0.7 - risk*1.6
	default: // apt: methodical, values depth and persistence
		w = base + val*1.0 - risk*0.7
	}
	switch a.Kind {
	case "recon":
		w += 0.3
	case "persist":
		w += 0.5
	case "exfil":
		w += 2.0
	case "sensor":
		w += 0.6 - risk*0.5
	}
	fails := st.Atk.Failures[a.Kind+":"+a.Target]
	w -= float64(fails) * 0.6
	w += p.rng.Float64() * 0.15
	return candidate{a: a, score: w}
}

// odds returns (success, detection) probabilities for an action given state.
func (p *planner) odds(st *State, a *AtkAction) (float64, float64) {
	var succ, det float64
	switch a.Kind {
	case "recon":
		succ, det = 1.0, 0.55
	case "exploit":
		succ, det = 0.72, 0.5
	case "bruteforce":
		succ, det = 0.55, 0.7
	case "lateral":
		succ, det = 0.6, 0.45
	case "persist":
		succ, det = 0.9, 0.35
	case "sensor":
		succ, det = 0.65, 0.4
	case "exfil":
		succ, det = 0.8, 0.75
	}
	// Flawed services are easier and quieter to abuse.
	if h, _ := st.Host(a.Target); h != nil && a.Service != "" {
		for _, s := range h.Services {
			if s.Name != a.Service {
				continue
			}
			if s.Vuln {
				succ += 0.2
				det -= 0.1
			}
			if s.Weak {
				succ += 0.12
			}
		}
	}
	// Profile tuning on detection.
	switch p.profile {
	case "stealth":
		det *= 0.55
	case "apt":
		det *= 0.75
	case "smash":
		det *= 1.1
		succ += 0.05
	}
	// A dead agent on the target lowers detection sharply.
	if rt := st.Hosts[a.Target]; rt != nil && !rt.AgentAlive {
		det *= 0.4
	}
	return clamp(succ), clamp(det)
}

func clamp(f float64) float64 {
	if f < 0.02 {
		return 0.02
	}
	if f > 0.98 {
		return 0.98
	}
	return f
}

func (p *planner) duration(kind string) int {
	base := map[string]int{
		"recon": 3, "exploit": 4, "bruteforce": 6, "lateral": 5,
		"persist": 3, "sensor": 3, "exfil": 4,
	}[kind]
	if base == 0 {
		base = 3
	}
	switch p.profile {
	case "smash":
		base = base * 7 / 10
	case "stealth":
		base = base * 14 / 10
	}
	return base*TicksPerSecond + p.rng.Intn(TicksPerSecond)
}

func (p *planner) gap() int {
	switch p.profile {
	case "smash":
		return TicksPerSecond + p.rng.Intn(TicksPerSecond)
	case "stealth":
		return 4*TicksPerSecond + p.rng.Intn(4*TicksPerSecond)
	default:
		return 2*TicksPerSecond + p.rng.Intn(3*TicksPerSecond)
	}
}
