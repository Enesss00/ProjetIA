package sim

// EvType names an event kind. Event types are part of the journal format:
// never rename one, add a new type instead.
type EvType string

// Event types.
const (
	EvGameStart  EvType = "game.start"
	EvGameEnd    EvType = "game.end"
	EvCmd        EvType = "cmd"       // player input line (Text), id in N
	EvTerm       EvType = "term"      // terminal output for command N (Lines)
	EvJobStart   EvType = "job.start" // Text=kind Host=target N=end tick
	EvJobEnd     EvType = "job.end"
	EvIsolate    EvType = "host.isolate"
	EvUnisolate  EvType = "host.unisolate"
	EvDown       EvType = "host.down" // N = down until tick
	EvHarden     EvType = "host.harden"
	EvInvestigat EvType = "host.investigate"
	EvBlock      EvType = "fw.block"
	EvUnblock    EvType = "fw.unblock"
	EvSensorOff  EvType = "sensor.off"
	EvSensorOn   EvType = "sensor.on"
	EvAtkIP      EvType = "atk.ip"
	EvAtkAction  EvType = "atk.action" // Text=kind Host=target Peer=source N=end
	EvAtkLearn   EvType = "atk.learn"  // attacker discovered Host
	EvAtkFail    EvType = "atk.fail"   // Text=reason
	EvAtkOwn     EvType = "atk.own"    // Host compromised from Peer
	EvAtkPersist EvType = "atk.persist"
	EvAtkLost    EvType = "atk.lost"  // attacker lost foothold on Host
	EvAtkExfil   EvType = "atk.exfil" // N = progress units added
	EvAtkGiveUp  EvType = "atk.giveup"
	EvAlert      EvType = "alert" // Host, Text, N=severity 1..3
	EvLog        EvType = "log"   // Host, Text
	EvSecond     EvType = "sec"   // once per game second, accrues damage
	EvScore      EvType = "score" // N = delta, Text = reason
)

// Event is the single unit of state change. Fields are a flat union so the
// journal stays a simple table; unused fields stay zero and are omitted.
type Event struct {
	Seq     int      `json:"seq"`
	Tick    int      `json:"tick"`
	Type    EvType   `json:"type"`
	Host    string   `json:"host,omitempty"`
	Peer    string   `json:"peer,omitempty"`
	IP      string   `json:"ip,omitempty"`
	Service string   `json:"service,omitempty"`
	N       int      `json:"n,omitempty"`
	Text    string   `json:"text,omitempty"`
	Lines   []Line   `json:"lines,omitempty"`
	Net     *Network `json:"net,omitempty"`
}

// Style is a semantic style for terminal output. The client maps styles to
// colours; the server never emits raw escape sequences.
type Style string

// Terminal styles.
const (
	StNormal Style = ""
	StDim    Style = "dim"
	StOK     Style = "ok"
	StWarn   Style = "warn"
	StErr    Style = "err"
	StAccent Style = "accent"
	StHead   Style = "head"
)

// Line is one styled terminal line.
type Line struct {
	S    Style  `json:"s,omitempty"`
	Text string `json:"t"`
}
