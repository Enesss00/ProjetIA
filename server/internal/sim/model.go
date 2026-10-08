// Package sim contains the deterministic GHOST NET simulation: the network
// model, the event types, the pure Apply reducer and the tick engine.
//
// Everything here is a game model. "Attacks" are abstract events with a
// duration, a success probability and a noise level; nothing touches a real
// network.
package sim

import "sort"

// TicksPerSecond is the fixed simulation rate.
const TicksPerSecond = 10

// Zone is a network segment.
type Zone string

// Zones of the simulated company network.
const (
	ZoneDMZ  Zone = "dmz"
	ZoneCorp Zone = "corp"
	ZoneSrv  Zone = "srv"
)

// Zones lists every zone in stable order.
var Zones = []Zone{ZoneDMZ, ZoneCorp, ZoneSrv}

// Service is a network service exposed by a host.
type Service struct {
	Name    string `json:"name"`
	Port    int    `json:"port"`
	Version string `json:"version"`
	// Weak means the service accepts a guessable credential (game flag).
	Weak bool `json:"weak"`
	// Vuln means the service version carries a known flaw (game flag).
	Vuln bool `json:"vuln"`
}

// Host is a static machine description produced by the generator.
type Host struct {
	ID       string    `json:"id"`
	IP       string    `json:"ip"`
	Zone     Zone      `json:"zone"`
	Role     string    `json:"role"`
	Value    int       `json:"value"` // business value 1..10
	Jewel    bool      `json:"jewel"` // holds the data the attacker wants
	Agent    bool      `json:"agent"` // has a log agent installed
	Services []Service `json:"services"`
	// X, Y are layout coordinates for the 3D city (grid units).
	X int `json:"x"`
	Y int `json:"y"`
}

// Network is the generated topology.
type Network struct {
	Seed     uint64 `json:"seed"`
	Hosts    []Host `json:"hosts"`
	IDS      []Zone `json:"ids"`      // zones monitored by an IDS sensor
	Attacker string `json:"attacker"` // attacker profile name
	// AttackerIPs is the pool of external addresses the attacker may use.
	AttackerIPs []string `json:"attackerIps"`
}

// HostIndex returns the index of the host with the given id or ip, or -1.
func (n *Network) HostIndex(key string) int {
	for i := range n.Hosts {
		if n.Hosts[i].ID == key || n.Hosts[i].IP == key {
			return i
		}
	}
	return -1
}

// Reachable reports whether traffic may flow from zone a to zone b according
// to the static firewall policy. "" stands for the internet.
func Reachable(from, to Zone) bool {
	switch from {
	case "":
		return to == ZoneDMZ
	case ZoneDMZ:
		return to == ZoneDMZ || to == ZoneSrv
	case ZoneCorp:
		return true
	case ZoneSrv:
		return to == ZoneSrv || to == ZoneCorp
	}
	return false
}

// HostRT is the mutable runtime state of a host.
type HostRT struct {
	Compromised  bool            `json:"compromised"`
	CompromiseAt int             `json:"compromiseAt"`
	Persistent   bool            `json:"persistent"`
	Isolated     bool            `json:"isolated"`
	DownUntil    int             `json:"downUntil"`
	AgentAlive   bool            `json:"agentAlive"`
	Hardened     map[string]bool `json:"hardened"` // service name -> patched
	Investigated bool            `json:"investigated"`
	// Suspicion counts player-visible alerts concerning this host.
	Suspicion int `json:"suspicion"`
}

// SortedKeys returns map keys in stable order.
func SortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
