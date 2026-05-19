# DebateOrchestrator

Go module `digital.vasic.debate` — multi-agent debate orchestration
primitives consumed by HelixAgent. The orchestrator coordinates
multi-LLM consensus + dissent across configurable agent pools,
captures real wall-clock latency per agent response, propagates
ProviderInvoker errors explicitly (no silent absorption), and
persists session-level metadata + lesson knowledge in a thread-safe
in-memory `LessonBank`.

## Status

**FUNCTIONAL.** 14 packages compile + `go test -race ./...` all
green. Core surfaces (`Orchestrator`, `APIAdapter`, `LessonBank`,
`ProviderInvoker` wiring, session lifecycle) ship REAL
implementations. Five auxiliary packages (`validation`, `audit`,
`evaluation`, `reflexion`, `tools`) ship constructor-real +
execution-honest-error stubs that surface explicit
`NotYetImplemented` errors per ACK-STUB §11.4 — they never
silently succeed.

## Honesty contract (CONST-035)

Every package in this module falls into one of two tiers:

- **REAL** — runs real code, returns real results, captures real
  timings. Where data is synthesised (e.g. agent content is hashed
  from `(topic, agent_id)` rather than coming from an LLM call), the
  synthesised content is clearly labelled inline with
  `[synthesised ... deterministic-stub-content awaiting provider
  wiring]` so any downstream consumer scanning Content for the
  marker can detect that no real LLM call was made. Wiring a real
  `ProviderInvoker` via `WithProviderInvoker(...)` removes the
  marker and produces real LLM dispatch with real latency
  measurement.
- **STUB** — constructors return real, inspectable structs but
  execution methods return an explicit
  `errors.New("debate/<pkg>: <Method> NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")`.
  Every stub method body is marked
  `// TODO(reconstruction-phase-2): real implementation pending`.

A stub MUST NOT silently succeed. A REAL package MUST capture and
return real data. Bluffing is a release-blocker per Article XI §11.9.

## Package tier table

| Package         | Tier | Notes                                                                       |
|-----------------|------|-----------------------------------------------------------------------------|
| `debate` (root) | REAL | In-memory `LessonBank` with thread-safe CRUD and substring search.          |
| `agents`        | REAL | `DomainType` enum + `String()` helper.                                      |
| `topology`      | REAL | `TopologyType` enum + `String()` helper.                                    |
| `gates`         | REAL | Permissive `ApprovalGate` baseline. Real policy evaluation pending.         |
| `orchestrator`  | REAL | Core `Orchestrator.ConductDebate`, `AgentPool`, `APIAdapter`, sessions, ProviderInvoker. |
| `comprehensive` | REAL core + STUB streaming | `ExecuteDebate` real; `StreamDebate` stubbed.                     |
| `validation`    | STUB | `NewValidationPipeline` real; `Execute` honest error.                       |
| `audit`         | STUB | `NewProvenanceTracker` real; `Record` honest error.                         |
| `evaluation`    | STUB | `NewBenchmarkBridge` real; `RunBenchmark` honest error.                     |
| `reflexion`     | STUB | All five constructors real; every execution method honest error.            |
| `testing`       | STUB | Constructors + options real; every execution method honest error.           |
| `tools`         | STUB | `ListAvailableTools` honestly empty; every other method honest error.       |

## Anti-bluff guarantees (round-272)

Round-272 ships a deep-doc + Challenge enrichment that closes the
runtime-evidence gap on every public symbol of the root + orchestrator
packages. The guarantees, each enforced by a runtime assertion in
`challenges/runner/main.go`:

1. **End-user-usability** (Article XI §11.9 / CONST-035) — every
   PASS line carries a captured runtime artefact (locale, rune
   count, dispatch count, latency value, status string, sentinel
   match). No metadata-only / grep-only / absence-of-error PASS.

2. **Byte-exact non-ASCII round-trip across 5 locales** —
   `ConductDebate` is exercised with Cyrillic (sr), Japanese (ja),
   Arabic (ar RTL), Han (zh-CN), and English (en) topics; every
   dispatched prompt is asserted to contain the locale topic byte-
   exact, every AgentResponse.Content carries the round-trip
   marker + locale bytes. Same invariant for `LessonBank.Add`/
   `Get`/`Search` (Content body + non-ASCII search substring).

3. **Real wall-clock latency measurement** —
   `AgentResponse.Latency` is asserted >= 500µs when the
   capturingInvoker sleeps 1ms. This guards against regression to
   the historic `simulatedLatency()` bluff (round-17 close-out⁸²)
   that returned a fake hash-derived value. The current code path
   is `orchestrator.invokeAgent` → `time.Since(start)` around the
   real invoker call.

4. **Error propagation (no silent absorption)** —
   `ProviderInvoker` returning an error MUST surface in
   `AgentResponse.Content` with the `[invoker-error` prefix per
   `orchestrator.go` line 443. Section 5 of the runner exercises
   this with a sentinel-error invoker and asserts the prefix
   appears, proving the orchestrator refuses to fabricate fake
   successful content on invoker failure.

5. **Rejection layer is real** — every invalid input
   (`ConductDebate(nil)`, empty topic, empty provider name, empty
   model, neg score, score>1, unknown session ID) returns a real
   non-nil error with an informative message. Section 7 covers
   8 distinct rejection paths.

6. **APIAdapter alias resolution** — `APIParticipantConfig` accepts
   both `Provider/Model` AND `LLMProvider/LLMModel` field forms;
   Section 4 asserts both registrations land in the agent pool
   (proves `EffectiveProvider`/`EffectiveModel` resolution, not
   silent drops).

7. **Concurrency safety under `-race`** — Section 6 spawns 8
   concurrent `ConductDebate` calls against a shared orchestrator,
   asserts pool stability + 100% success rate; the unit-test suite
   runs `-race` and detects no data race across sessions map,
   pool, atomic counters, or LessonBank mutex.

8. **Paired-mutation gate** — `debateorchestrator_describe_challenge.sh
   --anti-bluff-mutate` plants a `ConductDebate ->
   ConductDebate_MUTATED` rename in a tmp ledger copy and asserts
   the gate exits 99 (mutation detected). Proves the gate catches
   ledger-vs-source drift instead of rubber-stamping it.

The deep-doc ledger lives at `docs/test-coverage.md` and is the
authoritative symbol→exerciser map for this module. The
2026-05-19 operator mandate is preserved verbatim in the runner's
package doc comment and in the ledger preamble.

## Build / test

```bash
cd dependencies/HelixDevelopment/DebateOrchestrator
go mod tidy
go vet ./...
go build ./...
go test -race -count=1 ./...
# Deep-doc + multi-locale challenge runner:
go run ./challenges/runner -fixtures tests/fixtures/debateorchestrator/payloads.json
# Paired-mutation describe-challenge gate:
bash challenges/scripts/debateorchestrator_describe_challenge.sh
bash challenges/scripts/debateorchestrator_describe_challenge.sh --anti-bluff-mutate   # MUST exit 99
```

All five commands must exit as documented. No external module
dependencies are required — the module is stdlib-only by design.

## How to extend a stub safely

1. Open `RECONSTRUCTION_ROADMAP.md` and find the package's row.
2. Replace the stubbed method body with real code.
3. Remove the `// TODO(reconstruction-phase-2): real implementation pending`
   line.
4. Tighten or replace the package's `_test.go` to assert the new real
   behaviour with captured evidence (per CONST-035).
5. Update the README tier table when the package flips from STUB to REAL.
6. Add a new section (or extend an existing section) of
   `challenges/runner/main.go` that exercises the newly-real surface
   with byte-preservation + sentinel + latency assertions across
   the 5 fixture locales.
7. Update `docs/test-coverage.md` so the new symbols appear in the
   symbol→exerciser map. Verify
   `bash challenges/scripts/debateorchestrator_describe_challenge.sh`
   still exits 0 and `--anti-bluff-mutate` still exits 99.

See `RECONSTRUCTION_ROADMAP.md` for the detailed per-package follow-up list.
