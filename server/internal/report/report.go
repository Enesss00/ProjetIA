// Package report produces an after-action incident report from a finished
// game's event journal. The deterministic template is the mandatory fallback;
// an optional LLM reporter can enrich it when an API key is configured.
package report

import (
	"fmt"
	"sort"
	"strings"

	"ghostnet/internal/sim"
)

// Reporter turns a journal + final state into a human-readable report.
type Reporter interface {
	Generate(events []sim.Event, final *sim.State) string
}

// Template is the deterministic reporter. It never calls the network, so it
// always works and always produces the same report for the same game.
type Template struct{}

// Generate builds the report.
func (Template) Generate(events []sim.Event, final *sim.State) string {
	var b strings.Builder
	w := func(f string, a ...any) { fmt.Fprintf(&b, f, a...) }

	w("# GHOST NET — Incident Report\n\n")
	w("Seed: %d   Attacker profile: %s\n", final.Net.Seed, final.Net.Attacker)
	w("Outcome: %s   Final score: %d\n\n", outcomeLabel(final.Outcome), final.Score())

	// Key timings.
	w("## Response timeline\n")
	entry, entryTick := entryPoint(events)
	w("- Attacker first action:  %s\n", sim.FmtTick(final.Atk.FirstMove))
	if entry != "" {
		w("- Initial access:         %s on %s\n", sim.FmtTick(entryTick), entry)
	} else {
		w("- Initial access:         none — the attacker never gained a foothold\n")
	}
	w("- First true detection:   %s\n", sim.FmtTick(final.FirstDetect))
	w("- First containment:      %s\n", sim.FmtTick(final.FirstAction))
	if final.FirstDetect >= 0 && entryTick >= 0 {
		dwell := final.FirstDetect - entryTick
		if dwell < 0 {
			dwell = 0
		}
		w("- Dwell time (access→detection): %s\n", sim.FmtTick(dwell))
	}
	w("\n")

	// Attack chain.
	w("## Attack chain\n")
	chain := attackChain(events)
	if len(chain) == 0 {
		w("- No successful attacker actions were recorded.\n")
	}
	for _, c := range chain {
		w("- %s  %s\n", sim.FmtTick(c.tick), c.text)
	}
	w("\n")

	// Compromised assets.
	w("## Impact\n")
	var owned []string
	for _, h := range final.Net.Hosts {
		if rt := final.Hosts[h.ID]; rt != nil && rt.Compromised {
			tag := ""
			if h.Jewel {
				tag = " (CROWN JEWEL)"
			}
			owned = append(owned, fmt.Sprintf("%s%s", h.ID, tag))
		}
	}
	sort.Strings(owned)
	if len(owned) == 0 {
		w("- No hosts remained compromised at end of game.\n")
	} else {
		w("- Hosts still compromised: %s\n", strings.Join(owned, ", "))
	}
	w("- Data exfiltrated: %d%% of the crown-jewel dataset\n\n", final.Atk.Exfil)

	// Mistakes.
	w("## Analyst notes\n")
	if len(final.Mistakes) == 0 {
		w("- No scoring mistakes recorded. Clean defense.\n")
	}
	for _, m := range final.Mistakes {
		w("- %s\n", m)
	}
	w("\n")

	// Recommendations (deterministic heuristics).
	w("## Recommendations\n")
	for _, r := range recommendations(final, entry) {
		w("- %s\n", r)
	}
	return b.String()
}

type chainItem struct {
	tick int
	text string
}

func attackChain(events []sim.Event) []chainItem {
	var out []chainItem
	for _, ev := range events {
		switch ev.Type {
		case sim.EvAtkOwn:
			src := ev.Peer
			if src == "" {
				src = "the internet"
			}
			out = append(out, chainItem{ev.Tick, fmt.Sprintf("compromised %s from %s", ev.Host, src)})
		case sim.EvAtkPersist:
			out = append(out, chainItem{ev.Tick, "installed persistence on " + ev.Host})
		case sim.EvSensorOff:
			where := ev.Host
			if where == "" {
				where = "IDS zone " + ev.Text
			}
			out = append(out, chainItem{ev.Tick, "disabled a sensor: " + where})
		case sim.EvAtkExfil:
			out = append(out, chainItem{ev.Tick, fmt.Sprintf("exfiltrated data (+%d%%)", ev.N)})
		}
	}
	return out
}

func entryPoint(events []sim.Event) (string, int) {
	for _, ev := range events {
		if ev.Type == sim.EvAtkOwn && ev.Peer == "" {
			return ev.Host, ev.Tick
		}
	}
	// Fallback: first compromise at all.
	for _, ev := range events {
		if ev.Type == sim.EvAtkOwn {
			return ev.Host, ev.Tick
		}
	}
	return "", -1
}

func recommendations(final *sim.State, entry string) []string {
	var recs []string
	if entry != "" {
		recs = append(recs, "Patch or firewall the initial-access vector at "+entry+" before the next shift.")
	}
	silent := 0
	for _, h := range final.Net.Hosts {
		if rt := final.Hosts[h.ID]; rt != nil && h.Agent && !rt.AgentAlive {
			silent++
		}
	}
	if silent > 0 {
		recs = append(recs, fmt.Sprintf("%d log agent(s) were silenced — add tamper alerts on agent heartbeat.", silent))
	}
	if final.Atk.Exfil > 0 {
		recs = append(recs, "Add egress filtering / DLP on the server zone to slow exfiltration.")
	}
	if final.FirstDetect < 0 {
		recs = append(recs, "The intrusion was never detected — review IDS coverage and alert thresholds.")
	} else if final.FirstAction < 0 {
		recs = append(recs, "Alerts fired but no containment followed — tighten the response playbook.")
	}
	if len(recs) == 0 {
		recs = append(recs, "Defense was effective; keep the current sensor and firewall posture.")
	}
	return recs
}

func outcomeLabel(o string) string {
	switch o {
	case sim.OutcomeDefended:
		return "DEFENDED (held the full shift)"
	case sim.OutcomeEvicted:
		return "VICTORY (attacker evicted)"
	case sim.OutcomeBreached:
		return "BREACH (crown-jewel data stolen)"
	default:
		return o
	}
}
