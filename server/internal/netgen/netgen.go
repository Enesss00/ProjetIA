// Package netgen builds a simulated company network from a seed.
// The same seed always produces the same network.
package netgen

import (
	"fmt"

	"ghostnet/internal/rng"
	"ghostnet/internal/sim"
)

// Profiles are the attacker personalities.
var Profiles = []string{"smash", "stealth", "apt"}

// Options tune the generator.
type Options struct {
	// Workstations is the number of corp workstations (2..8). 0 = seed-derived.
	Workstations int
	// Profile forces the attacker profile; "" = seed-derived.
	Profile string
}

type svcTmpl struct {
	name     string
	port     int
	versions []string
	auth     bool // can have a weak credential
}

var (
	svcSSH  = svcTmpl{"ssh", 22, []string{"7.4p1", "8.2p1", "9.6p1"}, true}
	svcRDP  = svcTmpl{"rdp", 3389, []string{"10.0.17763", "10.0.19045"}, true}
	svcHTTP = svcTmpl{"http", 443, []string{"nginx/1.18", "nginx/1.24", "apache/2.4.49"}, false}
	svcSMTP = svcTmpl{"smtp", 25, []string{"postfix/3.4", "exim/4.92"}, false}
	svcVPN  = svcTmpl{"vpn", 1194, []string{"ovpn/2.4.7", "ovpn/2.6.8"}, true}
	svcSMB  = svcTmpl{"smb", 445, []string{"smb/3.0", "smb/3.1.1"}, true}
	svcSQL  = svcTmpl{"sql", 5432, []string{"pg/11.2", "pg/15.4"}, true}
)

// Generate builds the network for seed.
func Generate(seed uint64, opt Options) *sim.Network {
	r := rng.New(seed)
	n := &sim.Network{Seed: seed}

	profile := opt.Profile
	if !validProfile(profile) {
		profile = Profiles[r.Intn(len(Profiles))]
	}
	n.Attacker = profile

	ws := opt.Workstations
	if ws < 2 || ws > 8 {
		ws = 4
	}

	// DMZ: web front + one of mail/vpn.
	dmz2 := []string{"mail", "vpn"}[r.Intn(2)]
	add := func(id string, zone sim.Zone, role string, value int, svcs ...svcTmpl) *sim.Host {
		h := sim.Host{ID: id, Zone: zone, Role: role, Value: value, Agent: true}
		for _, t := range svcs {
			h.Services = append(h.Services, sim.Service{Name: t.name, Port: t.port, Version: t.versions[r.Intn(len(t.versions))]})
		}
		n.Hosts = append(n.Hosts, h)
		return &n.Hosts[len(n.Hosts)-1]
	}
	add("web-01", sim.ZoneDMZ, "web", 4, svcHTTP, svcSSH)
	if dmz2 == "mail" {
		add("mail-01", sim.ZoneDMZ, "mail", 3, svcSMTP, svcSSH)
	} else {
		add("vpn-01", sim.ZoneDMZ, "vpn", 5, svcVPN, svcSSH)
	}
	for i := 1; i <= ws; i++ {
		add(fmt.Sprintf("ws-%02d", i), sim.ZoneCorp, "workstation", 2, svcRDP, svcSMB)
	}
	add("files-01", sim.ZoneSrv, "fileserver", 6, svcSMB, svcSSH)
	add("db-01", sim.ZoneSrv, "database", 10, svcSQL, svcSSH)
	n.Hosts[len(n.Hosts)-1].Jewel = true

	// Addresses and city layout.
	octet := map[sim.Zone]int{sim.ZoneDMZ: 1, sim.ZoneCorp: 2, sim.ZoneSrv: 3}
	next := map[sim.Zone]int{}
	for i := range n.Hosts {
		h := &n.Hosts[i]
		next[h.Zone]++
		k := next[h.Zone]
		h.IP = fmt.Sprintf("10.0.%d.%d", octet[h.Zone], 10+k*r.Intn(3)+k)
		switch h.Zone {
		case sim.ZoneDMZ:
			h.X, h.Y = -2+(k-1)*4, -6
		case sim.ZoneCorp:
			h.X, h.Y = -10+((k-1)%2)*4, -1+((k-1)/2)*4
		case sim.ZoneSrv:
			h.X, h.Y = 7+(k-1)*4, 3
		}
	}
	dedupeIPs(n)

	// Weaknesses: guarantee at least one entry point in the DMZ and one way
	// into the server zone, then sprinkle random ones.
	entry := r.Intn(2) // which DMZ host
	setFlaw(r, &n.Hosts[entry], true)
	for i := range n.Hosts {
		h := &n.Hosts[i]
		if i == entry {
			continue
		}
		p := 0.25
		if h.Zone == sim.ZoneCorp {
			p = 0.4
		}
		if r.Chance(p) {
			setFlaw(r, h, false)
		}
	}
	// The crown jewel must always be crackable from inside, so a determined
	// attacker always has a complete kill chain and the player must actually
	// defend. (Detection risk, not reachability, is the difficulty knob.)
	for i := range n.Hosts {
		if n.Hosts[i].Jewel {
			setFlaw(r, &n.Hosts[i], false)
		}
	}

	// One workstation or the file server is always a viable stepping stone.
	steps := []int{}
	for i := range n.Hosts {
		if n.Hosts[i].Zone == sim.ZoneCorp || n.Hosts[i].Role == "fileserver" {
			steps = append(steps, i)
		}
	}
	setFlaw(r, &n.Hosts[steps[r.Intn(len(steps))]], false)

	// Sensors: every host has an agent; workstations may miss one.
	for i := range n.Hosts {
		if n.Hosts[i].Zone == sim.ZoneCorp && r.Chance(0.3) {
			n.Hosts[i].Agent = false
		}
	}
	n.IDS = []sim.Zone{sim.ZoneDMZ, sim.ZoneSrv}
	if r.Chance(0.5) {
		n.IDS = append(n.IDS, sim.ZoneCorp)
	}

	// Attacker infrastructure (documentation address ranges).
	for i := 0; i < 4; i++ {
		base := []string{"203.0.113", "198.51.100"}[r.Intn(2)]
		n.AttackerIPs = append(n.AttackerIPs, fmt.Sprintf("%s.%d", base, 2+r.Intn(250)))
	}
	dedupeAtk(n)
	return n
}

func validProfile(p string) bool {
	for _, q := range Profiles {
		if p == q {
			return true
		}
	}
	return false
}

// setFlaw marks one service weak or vulnerable. When external is true, the
// flaw is put on an internet-facing service.
func setFlaw(r *rng.RNG, h *sim.Host, external bool) {
	cand := []int{}
	for i, s := range h.Services {
		if external && !sim.Reachable("", h.Zone) {
			continue
		}
		_ = s
		cand = append(cand, i)
	}
	if len(cand) == 0 {
		return
	}
	s := &h.Services[cand[r.Intn(len(cand))]]
	if isAuth(s.Name) && r.Chance(0.6) {
		s.Weak = true
	} else {
		s.Vuln = true
	}
}

func isAuth(name string) bool {
	for _, t := range []svcTmpl{svcSSH, svcRDP, svcVPN, svcSMB, svcSQL} {
		if t.name == name {
			return true
		}
	}
	return false
}

func dedupeIPs(n *sim.Network) {
	seen := map[string]bool{}
	for i := range n.Hosts {
		h := &n.Hosts[i]
		for seen[h.IP] {
			var a, b, c, d int
			_, _ = fmt.Sscanf(h.IP, "%d.%d.%d.%d", &a, &b, &c, &d)
			h.IP = fmt.Sprintf("%d.%d.%d.%d", a, b, c, d%250+1)
		}
		seen[h.IP] = true
	}
}

func dedupeAtk(n *sim.Network) {
	seen := map[string]bool{}
	out := n.AttackerIPs[:0]
	for _, ip := range n.AttackerIPs {
		if !seen[ip] {
			seen[ip] = true
			out = append(out, ip)
		}
	}
	n.AttackerIPs = out
}
