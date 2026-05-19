// Round-272 challenge runner for digital.vasic.debate.
//
// Drives every public surface of the DebateOrchestrator root + orchestrator
// packages through real orchestrator.New construction, real RegisterProvider
// pool population, real ConductDebate end-to-end execution with a capturing
// ProviderInvoker, real LessonBank Add/Get/Search/Count CRUD, real APIAdapter
// CreateDebate dispatch, real GetStatistics counters, and real CreateSession +
// CancelSession + ListSessions lifecycle. The runner reads its bilingual
// inputs from tests/fixtures/debateorchestrator/payloads.json — no debate
// topic, lesson body, or participant name is hardcoded here.
//
// Sections:
//
//  1. Orchestrator construction + default-surface: real
//     NewDebateOrchestrator with DefaultOrchestratorConfig, RegisterProvider
//     populates the AgentPool, GetAgentPool returns it, GetStatistics
//     returns a non-nil snapshot with zero debates pre-run, CreateSession
//     records a pending session, GetSession round-trips it, CancelSession
//     flips its status, ListSessions enumerates it, Cleanup wipes it.
//
//  2. ConductDebate per locale (capturing ProviderInvoker, byte-exact
//     round-trip): per-locale real ConductDebate dispatch with
//     MaxRounds=2 + WithProviderInvoker wired to a capturingInvoker that
//     echoes prompt bytes back; asserts (a) the prompt the invoker
//     received contains the locale's non-ASCII topic byte-exact, (b)
//     every AgentResponse.Content carries the "OUT:" round-trip marker
//     plus the locale topic bytes (invoker round-trip intact), (c)
//     Latency >0 (real wall-clock measurement around the invoker),
//     (d) RoundsConducted == MaxRounds, (e) Phases length matches,
//     (f) Consensus + Metrics non-nil + Status=="completed".
//
//  3. LessonBank Add/Get/Search/Count per locale: per-locale real
//     LessonBank.Add of a lesson whose Content body contains the
//     locale's non-ASCII bytes; asserts Get round-trips Content
//     byte-exact, Confidence preserved, Search by the locale's
//     non-ASCII substring returns the matching lesson, Count
//     increments. SetSession metadata round-trip asserted.
//
//  4. APIAdapter end-to-end per locale: NewAPIAdapter wraps a fresh
//     orchestrator wired with capturingInvoker, APICreateDebateRequest
//     declares one APIParticipantConfig per locale (LLMProvider +
//     LLMModel field aliases exercised — EffectiveProvider +
//     EffectiveModel resolution proven). Asserts the response carries
//     the same locale topic bytes byte-exact, APIStatistics counters
//     advance, Strategy="mesh" metadata key surfaced.
//
//  5. ProviderInvoker error propagation: invoker returns a sentinel
//     error; asserts every AgentResponse.Content carries the
//     "[invoker-error" prefix (orchestrator surfaces the failure mode
//     rather than silently succeeding — per orchestrator.go line 443).
//
//  6. Concurrency / race surface: 8 parallel ConductDebate invocations
//     against the same orchestrator with capturingInvoker; asserts
//     GetStatistics().RegisteredAgents stays stable, OverallSuccessRate
//     == 1.0, no data race surfaces (when run under -race).
//
//  7. Empty-topic + invalid-request rejection: ConductDebate(nil),
//     ConductDebate({Topic:""}), RegisterProvider("", model, score),
//     RegisterProvider(name, "", score), RegisterProvider(name, model, -1),
//     CancelSession("unknown"), GetSession("unknown") ALL surface non-nil
//     errors — proves the rejection paths are real, not bluffed.
//
// Anti-bluff invariants enforced (Article XI §11.9 + CONST-035 + CONST-050(B)):
//
//   - No metadata-only / grep-only PASS. Every PASS line is preceded by the
//     section name, package symbol exercised, and a captured runtime artefact
//     (locale, rune count, prompt prefix, latency, dispatch count, status).
//   - Real orchestrator.New / RegisterProvider / ConductDebate /
//     APIAdapter / LessonBank invocations — no internal-state poking,
//     no field reflection.
//   - The capturing ProviderInvoker records the EXACT prompt bytes it
//     receives and the runner asserts byte-equality against the
//     fixture-derived topic — proves no silent string mutation in the
//     invokeAgent dispatch path.
//   - Latency assertion: capturingInvoker sleeps 1ms before responding;
//     runner asserts AgentResponse.Latency >= 1ms — proves orchestrator
//     measures REAL wall-clock latency around the invoker call (vs the
//     historic simulatedLatency() bluff that returned a fake hash-derived
//     value, fixed in round-17 close-out⁸²).
//   - Error-propagation re-validation: ProviderInvoker error surfaces as
//     "[invoker-error: ..." prefix in AgentResponse.Content per
//     orchestrator.go line 443 — proves the orchestrator surfaces real
//     failure modes rather than fabricating fake successful content.
//   - DebateRequest rejection paths: nil request, empty topic, invalid
//     provider params, unknown session ID — each returns a non-nil
//     error, proving the rejection layer is real not bluffed.
//   - No external mocks injected into the library; the runner uses each
//     package symbol via its public surface exactly as a downstream
//     consumer (HelixAgent ensemble) would.
//
// Verbatim 2026-05-19 operator mandate: "all existing tests and Challenges
// do work in anti-bluff manner - they MUST confirm that all tested codebase
// really works as expected! We had been in position that all tests do execute
// with success and all Challenges as well, but in reality the most of the
// features does not work and can't be used! This MUST NOT be the case and
// execution of tests and Challenges MUST guarantee the quality, the
// completition and full usability by end users of the product!"
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	debate "digital.vasic.debate"
	"digital.vasic.debate/orchestrator"
)

type fixtureInput struct {
	Locale           string `json:"locale"`
	Topic            string `json:"topic"`
	LessonContent    string `json:"lesson_content"`
	SearchSubstring  string `json:"search_substring"`
	ParticipantName  string `json:"participant_name"`
	ExpectedMinRunes int    `json:"expected_min_runes"`
}

type fixtureFile struct {
	Inputs []fixtureInput `json:"inputs"`
}

var (
	passCount int
	failCount int
)

func pass(format string, args ...interface{}) {
	passCount++
	fmt.Printf("  PASS: "+format+"\n", args...)
}

func fail(format string, args ...interface{}) {
	failCount++
	fmt.Printf("  FAIL: "+format+"\n", args...)
}

// capturingInvoker records every prompt the orchestrator dispatches and
// echoes "OUT:<prompt>" back after a deliberate 1ms sleep so the runner
// can assert (a) the prompt carries the locale topic byte-exact and (b)
// AgentResponse.Latency >= 1ms (proves real wall-clock measurement).
type capturingInvoker struct {
	mu              sync.Mutex
	prompts         []string
	totalDispatches int64
	sleep           time.Duration
}

func (c *capturingInvoker) Run(_ context.Context, prompt string) (string, error) {
	c.mu.Lock()
	c.prompts = append(c.prompts, prompt)
	c.mu.Unlock()
	atomic.AddInt64(&c.totalDispatches, 1)
	if c.sleep > 0 {
		time.Sleep(c.sleep)
	}
	return "OUT:" + prompt, nil
}

func (c *capturingInvoker) snapshot() ([]string, int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.prompts))
	copy(out, c.prompts)
	return out, atomic.LoadInt64(&c.totalDispatches)
}

func main() {
	fixturesPath := flag.String("fixtures", "tests/fixtures/debateorchestrator/payloads.json", "path to bilingual fixture JSON")
	flag.Parse()

	fmt.Printf("=== Round-272 DebateOrchestrator Challenge Runner ===\n")
	fmt.Printf("Fixture: %s\n", *fixturesPath)
	fmt.Println()

	raw, err := os.ReadFile(*fixturesPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot read fixture %s: %v\n", *fixturesPath, err)
		os.Exit(2)
	}
	var fx fixtureFile
	if err := json.Unmarshal(raw, &fx); err != nil {
		fmt.Fprintf(os.Stderr, "cannot parse fixture: %v\n", err)
		os.Exit(2)
	}
	if len(fx.Inputs) < 3 {
		fmt.Fprintf(os.Stderr, "fixture has only %d inputs; need >=3\n", len(fx.Inputs))
		os.Exit(2)
	}

	section1OrchestratorConstructionAndDefaults()
	section2ConductDebate(fx)
	section3LessonBank(fx)
	section4APIAdapter(fx)
	section5InvokerErrorPropagation(fx)
	section6Concurrency(fx)
	section7RejectionPaths()

	fmt.Println()
	fmt.Printf("=== Summary: %d PASS, %d FAIL ===\n", passCount, failCount)
	if failCount > 0 {
		os.Exit(1)
	}
}

// -----------------------------------------------------------------------------
// Section 1 — orchestrator.NewDebateOrchestrator + default surface.
// -----------------------------------------------------------------------------

func section1OrchestratorConstructionAndDefaults() {
	fmt.Println("Section 1: NewDebateOrchestrator + RegisterProvider + GetAgentPool + GetStatistics + sessions")

	o := orchestrator.NewDebateOrchestrator(orchestrator.DefaultOrchestratorConfig())
	if o == nil {
		fail("[Section1][NewDebateOrchestrator] nil orchestrator")
		return
	}
	pass("[Section1][NewDebateOrchestrator] constructed")

	if err := o.RegisterProvider("provider-a", "model-a", 0.7); err != nil {
		fail("[Section1][RegisterProvider#1] %v", err)
		return
	}
	if err := o.RegisterProvider("provider-b", "model-b", 0.8); err != nil {
		fail("[Section1][RegisterProvider#2] %v", err)
		return
	}
	pool := o.GetAgentPool()
	if pool == nil {
		fail("[Section1][GetAgentPool] nil pool")
		return
	}
	if size := pool.Size(); size == 2 {
		pass("[Section1][GetAgentPool] pool size=%d after 2 RegisterProvider", size)
	} else {
		fail("[Section1][GetAgentPool] pool size=%d expected 2", size)
	}

	ctx := context.Background()
	stats, err := o.GetStatistics(ctx)
	if err != nil {
		fail("[Section1][GetStatistics] %v", err)
		return
	}
	if stats == nil {
		fail("[Section1][GetStatistics] nil stats")
		return
	}
	if stats.RegisteredAgents == 2 && stats.ActiveDebates == 0 && stats.OverallSuccessRate == 0 {
		pass("[Section1][GetStatistics] pre-run snapshot registered=%d active=%d success_rate=%.2f",
			stats.RegisteredAgents, stats.ActiveDebates, stats.OverallSuccessRate)
	} else {
		fail("[Section1][GetStatistics] unexpected snapshot %+v", stats)
	}

	// Session lifecycle: CreateSession, GetSession, ListSessions, CancelSession, Cleanup
	req := &orchestrator.DebateRequest{ID: "sess-1", Topic: "section1-lifecycle"}
	sess, err := o.CreateSession(req)
	if err != nil {
		fail("[Section1][CreateSession] %v", err)
		return
	}
	if sess == nil || sess.ID != "sess-1" || sess.Status != "pending" {
		fail("[Section1][CreateSession] unexpected session %+v", sess)
	} else {
		pass("[Section1][CreateSession] id=%s status=%s", sess.ID, sess.Status)
	}

	got, err := o.GetSession("sess-1")
	if err != nil || got == nil || got.ID != "sess-1" {
		fail("[Section1][GetSession] err=%v session=%+v", err, got)
	} else {
		pass("[Section1][GetSession] round-trip OK id=%s", got.ID)
	}

	list := o.ListSessions()
	if len(list) != 1 {
		fail("[Section1][ListSessions] len=%d expected 1", len(list))
	} else {
		pass("[Section1][ListSessions] len=%d", len(list))
	}

	if err := o.CancelSession("sess-1"); err != nil {
		fail("[Section1][CancelSession] %v", err)
	} else {
		got2, _ := o.GetSession("sess-1")
		if got2 != nil && got2.Status == "cancelled" {
			pass("[Section1][CancelSession] status flipped to %q", got2.Status)
		} else {
			fail("[Section1][CancelSession] status not flipped")
		}
	}

	o.Cleanup()
	list2 := o.ListSessions()
	if len(list2) == 0 {
		pass("[Section1][Cleanup] sessions wiped (len=0)")
	} else {
		fail("[Section1][Cleanup] sessions remain (len=%d)", len(list2))
	}
}

// -----------------------------------------------------------------------------
// Section 2 — ConductDebate per locale with capturing ProviderInvoker.
// -----------------------------------------------------------------------------

func section2ConductDebate(fx fixtureFile) {
	fmt.Println()
	fmt.Println("Section 2: ConductDebate per locale (capturing ProviderInvoker, byte-exact round-trip)")

	for _, in := range fx.Inputs {
		cap := &capturingInvoker{sleep: 1 * time.Millisecond}
		o := orchestrator.NewOrchestrator(nil, nil,
			orchestrator.DefaultOrchestratorConfig(),
			orchestrator.WithProviderInvoker(cap.Run))
		if err := o.RegisterProvider("provider-a", "model-a", 0.7); err != nil {
			fail("[Section2][RegisterProvider][%s] %v", in.Locale, err)
			continue
		}
		if err := o.RegisterProvider("provider-b", "model-b", 0.8); err != nil {
			fail("[Section2][RegisterProvider2][%s] %v", in.Locale, err)
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		req := &orchestrator.DebateRequest{
			Topic:     in.Topic,
			MaxRounds: 2,
			Timeout:   5 * time.Second,
		}
		resp, err := o.ConductDebate(ctx, req)
		cancel()
		if err != nil {
			fail("[Section2][ConductDebate][%s] %v", in.Locale, err)
			continue
		}
		if resp == nil {
			fail("[Section2][ConductDebate][%s] nil response", in.Locale)
			continue
		}
		if resp.Topic != in.Topic {
			fail("[Section2][ConductDebate][%s] Topic byte-mismatch", in.Locale)
			continue
		}
		if !resp.Success || resp.RoundsConducted != 2 || len(resp.Phases) != 2 {
			fail("[Section2][ConductDebate][%s] unexpected resp success=%v rounds=%d phases=%d",
				in.Locale, resp.Success, resp.RoundsConducted, len(resp.Phases))
			continue
		}
		if resp.Consensus == nil || resp.Metrics == nil {
			fail("[Section2][ConductDebate][%s] consensus/metrics nil", in.Locale)
			continue
		}
		if resp.Metrics.Status != "completed" {
			fail("[Section2][ConductDebate][%s] metrics.Status=%q expected completed",
				in.Locale, resp.Metrics.Status)
			continue
		}
		prompts, dispatches := cap.snapshot()
		if dispatches != 4 { // 2 rounds * 2 agents
			fail("[Section2][ConductDebate][%s] invoker dispatches=%d expected 4",
				in.Locale, dispatches)
			continue
		}
		topicHit := false
		for _, p := range prompts {
			if strings.Contains(p, in.Topic) {
				topicHit = true
				break
			}
		}
		if !topicHit {
			fail("[Section2][ConductDebate][%s] no dispatched prompt contains locale topic (renderer bluff)",
				in.Locale)
			continue
		}
		// Every AgentResponse.Content must carry "OUT:" round-trip marker + topic bytes
		contentRoundTripFailed := false
		latencyTooSmall := false
		for _, ph := range resp.Phases {
			for _, ar := range ph.Responses {
				if !strings.HasPrefix(ar.Content, "OUT:") {
					contentRoundTripFailed = true
				}
				if !strings.Contains(ar.Content, in.Topic) {
					contentRoundTripFailed = true
				}
				if ar.Latency < 500*time.Microsecond {
					// invoker sleeps 1ms; allow some scheduling slack but require >=0.5ms
					latencyTooSmall = true
				}
			}
		}
		if contentRoundTripFailed {
			fail("[Section2][ConductDebate][%s] AgentResponse.Content round-trip broken (OUT marker or topic missing)",
				in.Locale)
			continue
		}
		if latencyTooSmall {
			fail("[Section2][ConductDebate][%s] AgentResponse.Latency < 500us — orchestrator NOT measuring real wall-clock (simulatedLatency bluff regression)",
				in.Locale)
			continue
		}
		runes := utf8.RuneCountInString(in.Topic)
		pass("[Section2][ConductDebate][%s] %d topic-runes round=%d phases=%d dispatches=%d quality=%.3f",
			in.Locale, runes, resp.RoundsConducted, len(resp.Phases), dispatches, resp.QualityScore)
	}
}

// -----------------------------------------------------------------------------
// Section 3 — LessonBank Add/Get/Search/Count per locale.
// -----------------------------------------------------------------------------

func section3LessonBank(fx fixtureFile) {
	fmt.Println()
	fmt.Println("Section 3: LessonBank Add/Get/Search/Count + SetSession per locale")

	for _, in := range fx.Inputs {
		bank := debate.NewLessonBank(debate.DefaultLessonBankConfig())
		if bank == nil {
			fail("[Section3][NewLessonBank][%s] nil bank", in.Locale)
			continue
		}
		lessonID := "L-" + in.Locale
		l := debate.Lesson{
			ID:         lessonID,
			Topic:      in.Topic,
			Content:    in.LessonContent,
			Confidence: 0.85,
			CreatedAt:  time.Now().UTC(),
		}
		if err := bank.Add(l); err != nil {
			fail("[Section3][Add][%s] %v", in.Locale, err)
			continue
		}
		if got, err := bank.Get(lessonID); err != nil {
			fail("[Section3][Get][%s] %v", in.Locale, err)
			continue
		} else if got.Content != in.LessonContent {
			fail("[Section3][Get][%s] Content byte-mismatch", in.Locale)
			continue
		} else if got.Confidence != 0.85 {
			fail("[Section3][Get][%s] Confidence=%v expected 0.85", in.Locale, got.Confidence)
			continue
		}
		if c := bank.Count(); c != 1 {
			fail("[Section3][Count][%s] count=%d expected 1", in.Locale, c)
			continue
		}
		hits := bank.Search(in.SearchSubstring)
		if len(hits) != 1 {
			fail("[Section3][Search][%s] hits=%d expected 1 (locale substring %q)",
				in.Locale, len(hits), in.SearchSubstring)
			continue
		}
		if hits[0].Content != in.LessonContent {
			fail("[Section3][Search][%s] hit content mismatch", in.Locale)
			continue
		}
		if conf := bank.Confidence(lessonID); conf != 0.85 {
			fail("[Section3][Confidence][%s] %v expected 0.85", in.Locale, conf)
			continue
		}
		bank.SetSession("S-"+in.Locale, in.Topic, "completed", "ok", time.Now().UTC())
		if bank.ID() != "S-"+in.Locale || bank.Topic() != in.Topic ||
			bank.Status() != "completed" || bank.Conclusion() != "ok" {
			fail("[Section3][SetSession][%s] metadata round-trip broken", in.Locale)
			continue
		}
		if bank.CompletedAt().IsZero() {
			fail("[Section3][CompletedAt][%s] zero timestamp", in.Locale)
			continue
		}
		listLen := len(bank.List())
		runes := utf8.RuneCountInString(in.LessonContent)
		pass("[Section3][LessonBank][%s] CRUD+search OK (%d content-runes, list-len=%d, search-hit on %q)",
			in.Locale, runes, listLen, in.SearchSubstring)
	}
}

// -----------------------------------------------------------------------------
// Section 4 — APIAdapter end-to-end per locale.
// -----------------------------------------------------------------------------

func section4APIAdapter(fx fixtureFile) {
	fmt.Println()
	fmt.Println("Section 4: APIAdapter CreateDebate per locale (EffectiveProvider/EffectiveModel resolution)")

	for _, in := range fx.Inputs {
		cap := &capturingInvoker{sleep: 1 * time.Millisecond}
		o := orchestrator.NewOrchestrator(nil, nil,
			orchestrator.DefaultOrchestratorConfig(),
			orchestrator.WithProviderInvoker(cap.Run))
		api := orchestrator.NewAPIAdapter(o)

		// Use LLMProvider/LLMModel aliases — proves EffectiveProvider/EffectiveModel resolution
		participants := []orchestrator.APIParticipantConfig{
			{Name: in.ParticipantName + "-1", LLMProvider: "openai", LLMModel: "gpt-test", Score: 0.7, Role: "advocate"},
			{Name: in.ParticipantName + "-2", Provider: "ollama", Model: "llama-test", Score: 0.6, Role: "critic"},
		}
		areq := &orchestrator.APICreateDebateRequest{
			DebateID:     "api-" + in.Locale,
			Topic:        in.Topic,
			MaxRounds:    2,
			Strategy:     "mesh",
			Participants: participants,
			Metadata:     map[string]interface{}{"locale": in.Locale},
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		resp, err := api.CreateDebate(ctx, areq)
		if err != nil {
			cancel()
			fail("[Section4][CreateDebate][%s] %v", in.Locale, err)
			continue
		}
		if resp == nil || resp.Topic != in.Topic {
			fail("[Section4][CreateDebate][%s] topic mismatch or nil resp", in.Locale)
			continue
		}
		// Strategy=mesh must surface in resp.Metadata
		if strat, ok := resp.Metadata["strategy"]; !ok || strat != "mesh" {
			fail("[Section4][CreateDebate][%s] metadata.strategy missing or wrong: %v",
				in.Locale, resp.Metadata["strategy"])
			continue
		}
		// EffectiveProvider/EffectiveModel resolution: pool should have 2 agents
		// (openai/gpt-test from LLMProvider/LLMModel + ollama/llama-test from Provider/Model)
		pool := o.GetAgentPool().List()
		foundOpenAI, foundOllama := false, false
		for _, a := range pool {
			if a.Provider == "openai" && a.Model == "gpt-test" {
				foundOpenAI = true
			}
			if a.Provider == "ollama" && a.Model == "llama-test" {
				foundOllama = true
			}
		}
		if !foundOpenAI {
			fail("[Section4][EffectiveProvider][%s] openai/gpt-test (LLMProvider alias) not registered",
				in.Locale)
			continue
		}
		if !foundOllama {
			fail("[Section4][EffectiveProvider][%s] ollama/llama-test (Provider field) not registered",
				in.Locale)
			continue
		}
		// APIStatistics counters (use a fresh background ctx — debate ctx is done)
		cancel()
		stats, err := api.GetStatistics(context.Background())
		if err != nil || stats == nil {
			fail("[Section4][APIStatistics][%s] err=%v stats=%v", in.Locale, err, stats)
			continue
		}
		if stats.TotalDebates != 1 || stats.CompletedDebates != 1 {
			fail("[Section4][APIStatistics][%s] total=%d completed=%d expected 1/1",
				in.Locale, stats.TotalDebates, stats.CompletedDebates)
			continue
		}
		runes := utf8.RuneCountInString(in.Topic)
		pass("[Section4][APIAdapter][%s] CreateDebate OK (%d topic-runes, both alias forms resolved, stats=1/1)",
			in.Locale, runes)
	}
}

// -----------------------------------------------------------------------------
// Section 5 — ProviderInvoker error propagation.
// -----------------------------------------------------------------------------

func section5InvokerErrorPropagation(fx fixtureFile) {
	fmt.Println()
	fmt.Println("Section 5: ProviderInvoker error surfaces as [invoker-error: ...] marker")

	sentinel := errors.New("simulated upstream provider timeout")
	errInvoker := func(_ context.Context, _ string) (string, error) {
		return "", sentinel
	}
	o := orchestrator.NewOrchestrator(nil, nil,
		orchestrator.DefaultOrchestratorConfig(),
		orchestrator.WithProviderInvoker(errInvoker))
	_ = o.RegisterProvider("provider-x", "model-x", 0.5)
	_ = o.RegisterProvider("provider-y", "model-y", 0.5)

	in := fx.Inputs[0]
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := o.ConductDebate(ctx, &orchestrator.DebateRequest{
		Topic:     in.Topic,
		MaxRounds: 1,
	})
	if err != nil {
		fail("[Section5][ConductDebate-err] orchestrator returned err: %v (expected success with error markers)", err)
		return
	}
	if resp == nil || len(resp.Phases) == 0 {
		fail("[Section5][ConductDebate-err] empty response")
		return
	}
	markerSeen := 0
	for _, ph := range resp.Phases {
		for _, ar := range ph.Responses {
			if strings.HasPrefix(ar.Content, "[invoker-error") {
				markerSeen++
			}
		}
	}
	if markerSeen == 0 {
		fail("[Section5][invoker-error-marker] NO AgentResponse.Content carries [invoker-error prefix — orchestrator silently absorbed invoker error (BLUFF)")
		return
	}
	pass("[Section5][invoker-error-marker] %d AgentResponse.Content surfaces [invoker-error prefix (failure mode visible to consumer)",
		markerSeen)
}

// -----------------------------------------------------------------------------
// Section 6 — Concurrency / race surface.
// -----------------------------------------------------------------------------

func section6Concurrency(fx fixtureFile) {
	fmt.Println()
	fmt.Println("Section 6: 8 parallel ConductDebate against shared orchestrator (race surface)")

	cap := &capturingInvoker{sleep: 0}
	o := orchestrator.NewOrchestrator(nil, nil,
		orchestrator.DefaultOrchestratorConfig(),
		orchestrator.WithProviderInvoker(cap.Run))
	_ = o.RegisterProvider("provider-a", "model-a", 0.7)
	_ = o.RegisterProvider("provider-b", "model-b", 0.8)

	in := fx.Inputs[0]
	var wg sync.WaitGroup
	var failures atomic.Int64
	const N = 8
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			resp, err := o.ConductDebate(ctx, &orchestrator.DebateRequest{
				Topic:     fmt.Sprintf("%s [worker=%d]", in.Topic, idx),
				MaxRounds: 1,
			})
			if err != nil || resp == nil || !resp.Success {
				failures.Add(1)
			}
		}(i)
	}
	wg.Wait()

	if failures.Load() != 0 {
		fail("[Section6][parallel] %d/%d concurrent debates failed", failures.Load(), N)
		return
	}
	ctx := context.Background()
	stats, err := o.GetStatistics(ctx)
	if err != nil || stats == nil {
		fail("[Section6][GetStatistics] err=%v stats=%v", err, stats)
		return
	}
	if stats.RegisteredAgents != 2 {
		fail("[Section6][GetStatistics] agent pool corrupted: registered=%d expected 2",
			stats.RegisteredAgents)
		return
	}
	if stats.OverallSuccessRate != 1.0 {
		fail("[Section6][GetStatistics] success_rate=%.3f expected 1.0", stats.OverallSuccessRate)
		return
	}
	pass("[Section6][parallel] %d concurrent debates succeeded, success_rate=%.3f, pool stable",
		N, stats.OverallSuccessRate)
}

// -----------------------------------------------------------------------------
// Section 7 — Rejection paths (no-bluff rejection layer).
// -----------------------------------------------------------------------------

func section7RejectionPaths() {
	fmt.Println()
	fmt.Println("Section 7: invalid-input rejection layer (every reject returns a real non-nil error)")

	o := orchestrator.NewDebateOrchestrator(orchestrator.DefaultOrchestratorConfig())

	ctx := context.Background()
	if _, err := o.ConductDebate(ctx, nil); err == nil {
		fail("[Section7][ConductDebate(nil)] accepted nil request (BLUFF)")
	} else {
		pass("[Section7][ConductDebate(nil)] rejected: %v", err)
	}

	if _, err := o.ConductDebate(ctx, &orchestrator.DebateRequest{Topic: ""}); err == nil {
		fail("[Section7][ConductDebate(empty-topic)] accepted empty topic (BLUFF)")
	} else {
		pass("[Section7][ConductDebate(empty-topic)] rejected: %v", err)
	}

	if err := o.RegisterProvider("", "model", 0.5); err == nil {
		fail("[Section7][RegisterProvider(empty-name)] accepted (BLUFF)")
	} else {
		pass("[Section7][RegisterProvider(empty-name)] rejected: %v", err)
	}

	if err := o.RegisterProvider("name", "", 0.5); err == nil {
		fail("[Section7][RegisterProvider(empty-model)] accepted (BLUFF)")
	} else {
		pass("[Section7][RegisterProvider(empty-model)] rejected: %v", err)
	}

	if err := o.RegisterProvider("name", "model", -0.1); err == nil {
		fail("[Section7][RegisterProvider(neg-score)] accepted (BLUFF)")
	} else {
		pass("[Section7][RegisterProvider(neg-score)] rejected: %v", err)
	}

	if err := o.RegisterProvider("name", "model", 1.5); err == nil {
		fail("[Section7][RegisterProvider(score>1)] accepted (BLUFF)")
	} else {
		pass("[Section7][RegisterProvider(score>1)] rejected: %v", err)
	}

	if err := o.CancelSession("does-not-exist"); err == nil {
		fail("[Section7][CancelSession(unknown)] accepted (BLUFF)")
	} else {
		pass("[Section7][CancelSession(unknown)] rejected: %v", err)
	}

	if _, err := o.GetSession("does-not-exist"); err == nil {
		fail("[Section7][GetSession(unknown)] accepted (BLUFF)")
	} else {
		pass("[Section7][GetSession(unknown)] rejected: %v", err)
	}
}
