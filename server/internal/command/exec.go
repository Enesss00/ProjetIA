package command

import (
	"fmt"
	"net"
	"sort"
	"strings"

	"ghostnet/internal/sim"
)

// Executor runs parsed commands against an engine. One Executor is bound to
// one game.
type Executor struct {
	e *sim.Engine
}

// New returns an executor for engine e.
func New(e *sim.Engine) *Executor { return &Executor{e: e} }

// L is shorthand for a styled line.
func L(s sim.Style, f string, a ...any) sim.Line {
	return sim.Line{S: s, Text: fmt.Sprintf(f, a...)}
}

// Run executes a raw input line. cmdID is a monotonically increasing id the
// client assigns so terminal output can be correlated. It never panics on
// bad input; it always produces terminal feedback.
func (x *Executor) Run(cmdID int, raw string) {
	p, err := Parse(raw)
	if err != nil {
		x.e.EchoCmd(cmdID, truncate(raw, 40))
		x.e.Term(cmdID, []sim.Line{L(sim.StErr, "input rejected: %v (max %d bytes)", err, MaxLineBytes)})
		return
	}
	x.e.EchoCmd(cmdID, p.Raw)
	if p.Verb == "" {
		return
	}
	h, ok := handlers[p.Verb]
	if !ok {
		x.e.Term(cmdID, []sim.Line{
			L(sim.StErr, "unknown command: %s", p.Verb),
			L(sim.StDim, "type 'help' for the command list"),
		})
		return
	}
	h(x, cmdID, p.Args)
}

type handler func(x *Executor, cmdID int, args []string)

var handlers map[string]handler

func init() {
	handlers = map[string]handler{
		"help":      (*Executor).help,
		"status":    (*Executor).status,
		"scan":      (*Executor).scan,
		"logs":      (*Executor).logs,
		"isolate":   (*Executor).isolate,
		"unisolate": (*Executor).unisolate,
		"block":     (*Executor).block,
		"unblock":   (*Executor).unblock,
		"patch":     (*Executor).patch,
		"restart":   (*Executor).restart,
		"sensors":   (*Executor).sensors,
		"forensics": (*Executor).forensics,
		"report":    (*Executor).report,
		"clear":     func(x *Executor, id int, _ []string) { x.e.Term(id, []sim.Line{{Text: "\x00clear"}}) },
	}
}

func (x *Executor) out(id int, lines ...sim.Line) { x.e.Term(id, lines) }

func (x *Executor) help(id int, _ []string) {
	x.out(id,
		L(sim.StHead, "GHOST NET terminal — commands"),
		L(sim.StNormal, "  status                 board overview"),
		L(sim.StNormal, "  scan net               enumerate the network (5s)"),
		L(sim.StNormal, "  scan <host>            fingerprint one host (2s)"),
		L(sim.StNormal, "  logs [--tail] [host]   recent sensor logs"),
		L(sim.StNormal, "  sensors                sensor / IDS health"),
		L(sim.StNormal, "  forensics <host>       investigate a host (3s)"),
		L(sim.StNormal, "  isolate <host>         cut a host off the network"),
		L(sim.StNormal, "  unisolate <host>       reconnect a host"),
		L(sim.StNormal, "  block <ip>             drop traffic from an IP"),
		L(sim.StNormal, "  unblock <ip>           re-allow an IP"),
		L(sim.StNormal, "  patch <host> <service> harden a service (4s)"),
		L(sim.StNormal, "  restart <host>         reboot a host (3s)"),
		L(sim.StNormal, "  report                 after-action incident report"),
		L(sim.StDim, "tip: 'forensics' reveals persistence that survives a restart."),
	)
}

func (x *Executor) status(id int, _ []string) {
	st := x.e.State()
	owned := 0
	for _, h := range st.Net.Hosts {
		if st.IsOwned(h.ID) {
			owned++
		}
	}
	lines := []sim.Line{
		L(sim.StHead, "SOC status @ %s", sim.FmtTick(st.Tick)),
		L(sim.StNormal, "  score %d   exfiltration %d%%   hosts owned (truth) %d/%d", st.Score(), st.Atk.Exfil, owned, len(st.Net.Hosts)),
	}
	// Player-visible suspicion board (fog of war: we show alerts, not truth).
	type row struct {
		id  string
		sus int
		iso bool
	}
	var rows []row
	for _, h := range st.Net.Hosts {
		rt := st.Hosts[h.ID]
		rows = append(rows, row{h.ID, rt.Suspicion, rt.Isolated})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].sus != rows[j].sus {
			return rows[i].sus > rows[j].sus
		}
		return rows[i].id < rows[j].id
	})
	lines = append(lines, L(sim.StDim, "  host      alerts  state"))
	for _, r := range rows {
		state, stStyle := "nominal", sim.StDim
		if r.iso {
			state, stStyle = "ISOLATED", sim.StWarn
		} else if r.sus > 0 {
			state, stStyle = "flagged", sim.StWarn
		}
		lines = append(lines, L(stStyle, "  %-9s %5d   %s", r.id, r.sus, state))
	}
	if n := len(st.Jobs); n > 0 {
		lines = append(lines, L(sim.StAccent, "  %d operation(s) in progress", n))
	}
	x.out(id, lines...)
}

func (x *Executor) scan(id int, args []string) {
	if len(args) == 0 {
		x.out(id, L(sim.StErr, "usage: scan net | scan <host>"))
		return
	}
	target := args[0]
	if target == "net" || target == "network" {
		if !x.e.StartJob("scan-net", "", "", id) {
			x.out(id, L(sim.StWarn, "a network scan is already running"))
			return
		}
		x.out(id, L(sim.StDim, "scanning network… (5s)"))
		return
	}
	if !x.hostExists(id, target) {
		return
	}
	if !x.e.StartJob("scan-host", target, "", id) {
		x.out(id, L(sim.StWarn, "already scanning %s", target))
		return
	}
	x.out(id, L(sim.StDim, "fingerprinting %s… (2s)", target))
}

func (x *Executor) logs(id int, args []string) {
	st := x.e.State()
	tail := false
	host := ""
	for _, a := range args {
		switch {
		case a == "--tail" || a == "-t":
			tail = true
		case strings.HasPrefix(a, "--host="):
			host = strings.TrimPrefix(a, "--host=")
		case !strings.HasPrefix(a, "-"):
			host = a
		}
	}
	var sel []sim.LogLine
	for _, l := range st.Logs {
		if host == "" || l.Host == host {
			sel = append(sel, l)
		}
	}
	if len(sel) == 0 {
		x.out(id, L(sim.StDim, "no logs match"))
		return
	}
	n := 12
	if tail {
		n = 20
	}
	if len(sel) > n {
		sel = sel[len(sel)-n:]
	}
	lines := []sim.Line{L(sim.StHead, "sensor logs (%d shown)", len(sel))}
	for _, l := range sel {
		where := l.Host
		if where == "" {
			where = "net"
		}
		lines = append(lines, L(sim.StDim, "  %s  %-9s %s", sim.FmtTick(l.Tick), where, l.Text))
	}
	x.out(id, lines...)
}

func (x *Executor) isolate(id int, args []string) {
	if !x.oneHost(id, args, "isolate") {
		return
	}
	h := args[0]
	st := x.e.State()
	if _, rt := st.Host(h); rt != nil && rt.Isolated {
		x.out(id, L(sim.StWarn, "%s is already isolated", h))
		return
	}
	x.e.Isolate(h)
	x.out(id, L(sim.StOK, "%s isolated — all traffic to/from it is dropped", h),
		L(sim.StDim, "  note: isolation counts as downtime while it lasts"))
}

func (x *Executor) unisolate(id int, args []string) {
	if !x.oneHost(id, args, "unisolate") {
		return
	}
	h := args[0]
	st := x.e.State()
	hh, rt := st.Host(h)
	if rt != nil && !rt.Isolated {
		x.out(id, L(sim.StWarn, "%s is not isolated", h))
		return
	}
	x.e.Unisolate(h)
	msg := L(sim.StOK, "%s reconnected", h)
	x.out(id, msg)
	if rt != nil && rt.Compromised && hh != nil {
		x.e.Penalty(-40, "reconnected still-compromised host "+h)
		x.out(id, L(sim.StWarn, "  warning: %s is still compromised — you just gave the attacker its path back", h))
	}
}

func (x *Executor) block(id int, args []string) {
	if len(args) != 1 {
		x.out(id, L(sim.StErr, "usage: block <ip>"))
		return
	}
	ip := args[0]
	if net.ParseIP(ip) == nil {
		x.out(id, L(sim.StErr, "not a valid IP address: %s", truncate(ip, 40)))
		return
	}
	st := x.e.State()
	if st.Blocked[ip] {
		x.out(id, L(sim.StWarn, "%s is already blocked", ip))
		return
	}
	x.e.Block(ip)
	x.out(id, L(sim.StOK, "firewall rule added: drop from %s", ip))
	// Penalise blocking a legitimate internal host's own IP.
	if i := st.Net.HostIndex(ip); i >= 0 {
		x.e.Penalty(-25, "blocked a legitimate host IP ("+ip+")")
		x.out(id, L(sim.StWarn, "  that IP belongs to %s — you just firewalled your own asset", st.Net.Hosts[i].ID))
	}
}

func (x *Executor) unblock(id int, args []string) {
	if len(args) != 1 {
		x.out(id, L(sim.StErr, "usage: unblock <ip>"))
		return
	}
	ip := args[0]
	if net.ParseIP(ip) == nil {
		x.out(id, L(sim.StErr, "not a valid IP address: %s", truncate(ip, 40)))
		return
	}
	if !x.e.State().Blocked[ip] {
		x.out(id, L(sim.StWarn, "%s is not blocked", ip))
		return
	}
	x.e.Unblock(ip)
	x.out(id, L(sim.StOK, "firewall rule removed for %s", ip))
}

func (x *Executor) patch(id int, args []string) {
	if len(args) != 2 {
		x.out(id, L(sim.StErr, "usage: patch <host> <service>"))
		return
	}
	host, svc := args[0], strings.ToLower(args[1])
	hh := x.hostOrNil(host)
	if hh == nil {
		x.noHost(id, host)
		return
	}
	found := false
	for _, s := range hh.Services {
		if s.Name == svc {
			found = true
		}
	}
	if !found {
		x.out(id, L(sim.StErr, "%s has no service %q (try 'scan %s')", host, truncate(svc, 20), host))
		return
	}
	if !x.e.StartJob("patch", host, svc, id) {
		x.out(id, L(sim.StWarn, "already patching %s", host))
		return
	}
	x.out(id, L(sim.StDim, "patching %s/%s… (4s)", host, svc))
}

func (x *Executor) restart(id int, args []string) {
	if !x.oneHost(id, args, "restart") {
		return
	}
	if !x.e.StartJob("restart", args[0], "", id) {
		x.out(id, L(sim.StWarn, "already restarting %s", args[0]))
		return
	}
	x.out(id, L(sim.StDim, "restarting %s… (3s, host offline meanwhile)", args[0]))
}

func (x *Executor) sensors(id int, _ []string) {
	st := x.e.State()
	lines := []sim.Line{L(sim.StHead, "sensor health")}
	lines = append(lines, L(sim.StDim, "  IDS zones:"))
	for _, z := range sim.Zones {
		alive, monitored := st.IDSAlive[string(z)], false
		for _, m := range st.Net.IDS {
			if m == z {
				monitored = true
			}
		}
		if !monitored {
			lines = append(lines, L(sim.StDim, "    %-5s  (no IDS)", z))
			continue
		}
		if alive {
			lines = append(lines, L(sim.StOK, "    %-5s  online", z))
		} else {
			lines = append(lines, L(sim.StErr, "    %-5s  OFFLINE", z))
		}
	}
	lines = append(lines, L(sim.StDim, "  host agents:"))
	hosts := append([]sim.Host(nil), st.Net.Hosts...)
	sort.Slice(hosts, func(i, j int) bool { return hosts[i].ID < hosts[j].ID })
	for _, h := range hosts {
		rt := st.Hosts[h.ID]
		if !h.Agent {
			lines = append(lines, L(sim.StDim, "    %-9s no agent", h.ID))
		} else if rt.AgentAlive {
			lines = append(lines, L(sim.StOK, "    %-9s reporting", h.ID))
		} else {
			lines = append(lines, L(sim.StErr, "    %-9s SILENT", h.ID))
		}
	}
	x.out(id, lines...)
}

func (x *Executor) forensics(id int, args []string) {
	if !x.oneHost(id, args, "forensics") {
		return
	}
	if !x.e.StartJob("forensics", args[0], "", id) {
		x.out(id, L(sim.StWarn, "already investigating %s", args[0]))
		return
	}
	x.out(id, L(sim.StDim, "running forensics on %s… (3s)", args[0]))
}

func (x *Executor) report(id int, _ []string) {
	x.out(id, L(sim.StAccent, "requesting after-action report… (see the report panel)"),
		L(sim.StDim, "  the full incident report is generated at end of game"))
}

// helpers ----------------------------------------------------------------

func (x *Executor) oneHost(id int, args []string, verb string) bool {
	if len(args) != 1 {
		x.out(id, L(sim.StErr, "usage: %s <host>", verb))
		return false
	}
	return x.hostExists(id, args[0])
}

func (x *Executor) hostExists(id int, name string) bool {
	if x.hostOrNil(name) == nil {
		x.noHost(id, name)
		return false
	}
	return true
}

func (x *Executor) hostOrNil(name string) *sim.Host {
	st := x.e.State()
	if i := st.Net.HostIndex(name); i >= 0 {
		return &st.Net.Hosts[i]
	}
	return nil
}

func (x *Executor) noHost(id int, name string) {
	x.out(id, L(sim.StErr, "no such host: %s", truncate(name, 40)),
		L(sim.StDim, "  run 'scan net' to list hosts"))
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
