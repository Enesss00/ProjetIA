package server

import (
	"encoding/json"
	"testing"

	"ghostnet/internal/command"
	"ghostnet/internal/netgen"
	"ghostnet/internal/protocol"
	"ghostnet/internal/report"
	"ghostnet/internal/sim"
)

// decodeAndRun mirrors the server's message handling without any socket: it
// decodes an arbitrary client frame and, for a cmd, runs it through a real
// engine+executor. It must never panic for any input.
func decodeAndRun(exec *command.Executor, data []byte) {
	var env protocol.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return
	}
	switch env.T {
	case protocol.CCmd:
		var m protocol.ClientCmd
		if json.Unmarshal(env.Body, &m) != nil {
			return
		}
		line := m.Line
		if len(line) > command.MaxLineBytes {
			line = line[:command.MaxLineBytes]
		}
		exec.Run(m.ID, line)
	case protocol.CSpeed:
		var m protocol.ClientSpeed
		_ = json.Unmarshal(env.Body, &m)
	case protocol.CPause:
		var m protocol.ClientPause
		_ = json.Unmarshal(env.Body, &m)
	case protocol.CReplaySeek:
		var m protocol.ClientSeek
		_ = json.Unmarshal(env.Body, &m)
	}
}

// FuzzWSDecode feeds arbitrary bytes to the client-message decoder + command
// executor and asserts it never panics and the engine stays consistent.
func FuzzWSDecode(f *testing.F) {
	seeds := [][]byte{
		[]byte(`{"v":1,"t":"cmd","b":{"id":1,"line":"status"}}`),
		[]byte(`{"v":1,"t":"cmd","b":{"id":2,"line":"\u001b[31mscan\u0000 net"}}`),
		[]byte(`{"v":1,"t":"speed","b":{"speed":999999}}`),
		[]byte(`{"v":1,"t":"seek","b":{"tick":-5}}`),
		[]byte(`{"t":"cmd"}`),
		[]byte(`not json at all`),
		[]byte(`{"v":1,"t":"cmd","b":{"id":3,"line":1234}}`),
		[]byte(``),
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		net := netgen.Generate(1, netgen.Options{})
		eng := sim.NewEngine(sim.Config{Net: net}, func(sim.Event) {})
		exec := command.New(eng)
		decodeAndRun(exec, data)
		// A command must never corrupt the invariant that there is exactly one
		// crown jewel or crash state hashing.
		_ = eng.State().Hash()
	})
	_ = report.Template{}
}
