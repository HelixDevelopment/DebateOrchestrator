//go:build live_llm

// Real-LLM integration test for LLMTestCaseGenerator. Opt-in via the
// `live_llm` build tag so the default `go test ./...` run stays hermetic.
//
// Per CONST-036 (LLMsVerifier as single source of truth for provider +
// model metadata), this test consumes the canonical LLMsVerifier provider
// envar convention rather than hardcoding a specific backend. The provider
// + model are selected from the operator's configured environment in
// strict precedence order:
//
//  1. HELIX_LIVE_LLM_{ENDPOINT,API_KEY,MODEL} — explicit operator override
//     (e.g. point at a local proxy or a specific verified provider+model)
//  2. DeepSeek (DEEPSEEK_API_KEY) — high CodeCapabilityScore per
//     LLMsVerifier scoring, OpenAI-compatible /v1/chat/completions
//  3. Mistral (MISTRAL_API_KEY) — broadly verified, OpenAI-compatible
//  4. OpenRouter (OPENROUTER_API_KEY) — aggregator with many verified models,
//     OpenAI-compatible
//
// The chosen model in each fallback case is one that LLMsVerifier has
// verified with VerificationStatus="verified" and a non-zero
// CodeCapabilityScore (per the canonical models list in the verifier's
// database). If you need a different model, set HELIX_LIVE_LLM_MODEL.
//
// Operator-side prerequisite: `source scripts/load_api_keys.sh` (which
// prefers $HOME/api_keys.sh then falls back to .env at meta-repo root)
// must have populated the chosen provider's API key into the environment
// before invoking this test.
//
// The test SKIP-OK's honestly when no provider is configured —
// the integration test cannot run without real credentials, and an
// honest skip is preferable to a fake pass per CONST-035.
//
// Invoke as:
//
//	go test -tags=live_llm -v -run TestLLMTestCaseGenerator_LiveLLM \
//	  ./testing/...

package testing_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	debatetesting "digital.vasic.debate/testing"
)

// liveProvider describes one OpenAI-compatible endpoint we know how to
// reach via the canonical LLMsVerifier provider env var convention.
type liveProvider struct {
	name      string
	apiKeyEnv string
	endpoint  string
	model     string // default model verified by LLMsVerifier for code tasks
}

// liveProviderFallbacks are tried in order when HELIX_LIVE_LLM_API_KEY
// is not set. Selection criterion: provider has been verified by
// LLMsVerifier and the default model has CodeCapabilityScore > 0.5.
//
// Operator can override model selection per-provider via env (e.g.
// DEEPSEEK_MODEL=deepseek-coder, MISTRAL_MODEL=codestral-latest).
var liveProviderFallbacks = []liveProvider{
	{name: "deepseek", apiKeyEnv: "DEEPSEEK_API_KEY", endpoint: "https://api.deepseek.com/v1/chat/completions", model: "deepseek-chat"},
	{name: "mistral", apiKeyEnv: "MISTRAL_API_KEY", endpoint: "https://api.mistral.ai/v1/chat/completions", model: "mistral-small-latest"},
	{name: "openrouter", apiKeyEnv: "OPENROUTER_API_KEY", endpoint: "https://openrouter.ai/api/v1/chat/completions", model: "deepseek/deepseek-chat"},
}

// pickLiveProvider returns the highest-precedence (endpoint, key, model)
// available in env, or ok=false when none configured.
func pickLiveProvider() (endpoint, apiKey, model string, ok bool) {
	if key := os.Getenv("HELIX_LIVE_LLM_API_KEY"); key != "" {
		ep := os.Getenv("HELIX_LIVE_LLM_ENDPOINT")
		m := os.Getenv("HELIX_LIVE_LLM_MODEL")
		if ep == "" || m == "" {
			return "", "", "", false
		}
		return ep, key, m, true
	}
	for _, p := range liveProviderFallbacks {
		if key := os.Getenv(p.apiKeyEnv); key != "" {
			m := os.Getenv("HELIX_LIVE_LLM_MODEL")
			if m == "" {
				// per-provider model override (e.g. DEEPSEEK_MODEL)
				m = os.Getenv(p.name + "_MODEL")
			}
			if m == "" {
				m = p.model
			}
			return p.endpoint, key, m, true
		}
	}
	return "", "", "", false
}

// openAICompatibleAdapter constructs an LLMAdapter that POSTs to an
// OpenAI-compatible /v1/chat/completions endpoint and extracts the
// first assistant message. Real HTTP, real credentials, real response —
// no stubs.
func openAICompatibleAdapter(endpoint, apiKey, model string) *debatetesting.LLMAdapter {
	return &debatetesting.LLMAdapter{Ask: func(ctx context.Context, prompt string) (string, error) {
		reqBody := map[string]any{
			"model": model,
			"messages": []map[string]string{
				{"role": "user", "content": prompt},
			},
			"temperature": 0.2,
		}
		body, err := json.Marshal(reqBody)
		if err != nil {
			return "", fmt.Errorf("marshal request: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return "", fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)
		client := &http.Client{Timeout: 120 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("provider call: %w", err)
		}
		defer resp.Body.Close()
		b, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return "", fmt.Errorf("read provider response: %w", readErr)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return "", fmt.Errorf("provider returned %d: %s", resp.StatusCode, truncate(string(b), 512))
		}
		var out struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(b, &out); err != nil {
			return "", fmt.Errorf("decode response: %w (raw=%s)", err, truncate(string(b), 512))
		}
		if len(out.Choices) == 0 {
			return "", fmt.Errorf("provider returned 0 choices: raw=%s", truncate(string(b), 512))
		}
		return out.Choices[0].Message.Content, nil
	}}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...[truncated]"
}

func TestLLMTestCaseGenerator_LiveLLM(t *testing.T) {
	endpoint, apiKey, model, ok := pickLiveProvider()
	if !ok {
		// SKIP-OK: #no-live-llm-configured — honest skip per CONST-035
		t.Skip("SKIP-OK: #no-live-llm-configured — no live LLM provider configured " +
			"(set HELIX_LIVE_LLM_{ENDPOINT,API_KEY,MODEL} or one of " +
			"DEEPSEEK_API_KEY / MISTRAL_API_KEY / OPENROUTER_API_KEY)")
	}

	t.Logf("live-LLM provider endpoint=%s model=%s (api-key length=%d)",
		endpoint, model, len(apiKey))

	adapter := openAICompatibleAdapter(endpoint, apiKey, model)
	validator := &debatetesting.BasicTestCaseValidator{}
	gen := debatetesting.NewLLMTestCaseGenerator(adapter, validator)

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	tc, err := gen.Generate(ctx, &debatetesting.GenerateRequest{
		Topic:      "string reversal in Go",
		Language:   "go",
		Difficulty: debatetesting.DifficultyBasic,
		Category:   debatetesting.CategoryFunctional,
		ContextHints: map[string]string{
			"note": "produce a deterministic test case suitable for unit testing a reverse(s string) string function",
		},
	})
	if err != nil {
		t.Fatalf("live-LLM Generate failed: %v", err)
	}

	// Captured runtime evidence per Article XI §11.9 / CONST-035 —
	// every assertion below proves the live LLM returned usable data.
	if tc == nil {
		t.Fatal("live-LLM Generate returned nil TestCase with nil error")
	}
	if tc.Description == "" && tc.Name == "" {
		t.Fatalf("live-LLM returned TestCase with empty Name/Description: %+v", tc)
	}
	if tc.ExpectedOutput == nil {
		t.Fatalf("live-LLM returned TestCase with nil ExpectedOutput: %+v", tc)
	}

	t.Logf("live-LLM-generated test case: name=%q description=%q input=%v expected=%v notes=%q",
		tc.Name, tc.Description, tc.Input, tc.ExpectedOutput, tc.Notes)
}
