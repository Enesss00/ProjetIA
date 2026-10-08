package report

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"ghostnet/internal/sim"
)

// Anthropic is an optional LLM reporter. It asks a model to write a tighter,
// more analyst-flavoured incident report, grounded ONLY in the deterministic
// facts the template already computed. On any error — no key, network down,
// bad response, timeout — it returns the deterministic template report. The
// template is therefore always the floor: the game never depends on the LLM.
type Anthropic struct {
	APIKey string
	Model  string
	Client *http.Client
}

// NewAnthropicFromEnv returns an Anthropic reporter if ANTHROPIC_API_KEY is
// set, otherwise nil (the caller then uses the Template directly).
func NewAnthropicFromEnv() *Anthropic {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return nil
	}
	model := os.Getenv("GHOSTNET_LLM_MODEL")
	if model == "" {
		model = "claude-opus-5-5"
	}
	return &Anthropic{
		APIKey: key,
		Model:  model,
		Client: &http.Client{Timeout: 12 * time.Second},
	}
}

// Generate returns an LLM-enriched report, or the deterministic template on any
// failure. The template output is always included as grounding so the model
// cannot invent facts that contradict the simulation.
func (a *Anthropic) Generate(events []sim.Event, final *sim.State) string {
	base := Template{}.Generate(events, final)
	enriched, err := a.enrich(base)
	if err != nil || enriched == "" {
		return base
	}
	return enriched + "\n\n---\n_Narrative by LLM; facts above are from the deterministic simulation._\n"
}

type msg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type req struct {
	Model     string `json:"model"`
	MaxTokens int    `json:"max_tokens"`
	System    string `json:"system"`
	Messages  []msg  `json:"messages"`
}

type respBody struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (a *Anthropic) enrich(base string) (string, error) {
	const system = "You are a senior SOC incident responder. Rewrite the incident " +
		"report below as a crisp after-action summary for a blue-team lead. Use ONLY " +
		"the facts given — never invent hosts, times, or events. Keep it under 250 " +
		"words, Markdown, with a one-line verdict, what went wrong, and two concrete " +
		"next steps. Do not include any preamble."

	body, err := json.Marshal(req{
		Model:     a.Model,
		MaxTokens: 700,
		System:    system,
		Messages:  []msg{{Role: "user", Content: base}},
	})
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("x-api-key", a.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	client := a.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("anthropic status %d", resp.StatusCode)
	}
	var rb respBody
	if err := json.Unmarshal(data, &rb); err != nil {
		return "", err
	}
	if rb.Error != nil {
		return "", fmt.Errorf("anthropic error: %s", rb.Error.Message)
	}
	var out bytes.Buffer
	for _, c := range rb.Content {
		if c.Type == "text" {
			out.WriteString(c.Text)
		}
	}
	return out.String(), nil
}
