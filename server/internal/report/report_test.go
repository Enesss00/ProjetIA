package report_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ghostnet/internal/netgen"
	"ghostnet/internal/report"
	"ghostnet/internal/sim"
)

func finishedGame(seed uint64) ([]sim.Event, *sim.State) {
	net := netgen.Generate(seed, netgen.Options{Profile: "apt"})
	var events []sim.Event
	eng := sim.NewEngine(sim.Config{Net: net}, func(ev sim.Event) { events = append(events, ev) })
	for eng.Tick() {
	}
	return events, eng.State()
}

func TestTemplateReportIsDeterministic(t *testing.T) {
	e1, s1 := finishedGame(42)
	e2, s2 := finishedGame(42)
	r1 := report.Template{}.Generate(e1, s1)
	r2 := report.Template{}.Generate(e2, s2)
	if r1 != r2 {
		t.Fatal("template report is not deterministic for the same seed")
	}
	for _, want := range []string{"Incident Report", "Attack chain", "Recommendations"} {
		if !strings.Contains(r1, want) {
			t.Errorf("report missing section %q", want)
		}
	}
}

// TestLLMFallsBackToTemplate proves the mandatory fallback: when the API fails,
// the enriched report equals the deterministic template report.
func TestLLMFallsBackToTemplate(t *testing.T) {
	events, st := finishedGame(7)
	base := report.Template{}.Generate(events, st)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	// An Anthropic reporter pointed at a failing endpoint must return template.
	a := &report.Anthropic{APIKey: "x", Model: "test", Client: srv.Client()}
	// Redirect by using a client whose transport rewrites the host.
	a.Client = &http.Client{Timeout: 3 * time.Second, Transport: rewrite{srv.URL}}
	got := a.Generate(events, st)
	if got != base {
		t.Fatalf("expected fallback to template on API error")
	}
}

func TestNewAnthropicFromEnvNilWithoutKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	if report.NewAnthropicFromEnv() != nil {
		t.Fatal("expected nil reporter without API key")
	}
}

// rewrite sends every request to a fixed base URL (the test server).
type rewrite struct{ base string }

func (rw rewrite) RoundTrip(r *http.Request) (*http.Response, error) {
	u := rw.base
	req, err := http.NewRequest(r.Method, u, r.Body)
	if err != nil {
		return nil, err
	}
	return http.DefaultTransport.RoundTrip(req)
}
