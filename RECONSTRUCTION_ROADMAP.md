# DebateOrchestrator — Reconstruction Roadmap

Phase 1 (this commit) ships enough surface to compile and exercise the
HelixAgent integration. Phase 2 replaces every `NotYetImplemented`
stub with a real implementation backed by captured evidence per
CONST-035 / Article XI §11.9.

## Per-package follow-up

| Package         | Status                              | Phase-2 follow-up                                                                                                                |
|-----------------|-------------------------------------|---------------------------------------------------------------------------------------------------------------------------------|
| `debate` (root) | REAL (in-memory)                    | Add durable storage backend (PostgreSQL or BoltDB), semantic search via embedding provider, persistence config validation.       |
| `agents`        | REAL                                | Expand `DomainType` enum as new specialisms appear (security, performance, refactoring, …). No structural rework needed.        |
| `topology`      | REAL                                | Add traversal helpers (e.g. `Neighbours(agentID)`) once orchestrator needs them for non-mesh routing.                            |
| `gates`         | REAL (permissive baseline)          | Wire to real policy engine (OPA / Rego or in-process predicate evaluator), capture wire evidence, add deny-path tests.            |
| `orchestrator`  | REAL (deterministic content)        | Replace `synthesiseContent` with real `LLMInvoker` dispatch via `ProviderInvoker`; wire `ProviderRegistry.GetProvider` to live provider SDK; honour `PreferredProviders` ordering; add streaming output path. |
| `comprehensive` | REAL `ExecuteDebate` + STUB stream  | Implement `StreamDebate` over a `chan *StreamEvent` driven by the orchestrator's per-agent invocation timeline; add chunked event flushing.                                                                  |
| `validation`    | STUB                                | Implement multi-pass validator (syntax/security/performance/style), per-pass evidence capture, configurable strict mode.                                                                                       |
| `audit`         | STUB                                | Implement append-only audit log (file + optional remote sink), tamper-evident chaining (hash linkage), redaction policy.                                                                                       |
| `evaluation`    | STUB                                | Implement benchmark catalog (HumanEval, MBPP, SWE-Bench, …) with real execution harness, evidence files, score persistence.                                                                                   |
| `reflexion`     | STUB                                | Implement episodic memory ring buffer, reflection-prompt template + LLM call, loop convergence detection, accumulated-wisdom persistence.                                                                     |
| `testing`       | STUB                                | Implement LLM-driven test generator, sandboxed `os/exec` executor with cgroup/rlimit enforcement, differential contrastive diff engine, basic structural validator.                                          |
| `tools`         | STUB (`ListAvailableTools` empty)   | Wire `QueryRAG` → vector store, `GetCodeDefinition` → LSP, `GenerateEmbedding` → embedding provider, `FormatCode` → language-specific formatters, `InvokeMCPTool` → MCP client, populate `ListAvailableTools`. |
| `protocol`      | STUB (test-only, mostly stub)       | Implement real transports (`NewFileTransport`, `NewPipeTransport`), wire `Protocol.Execute` + `HandleFederatedRequest` to real dispatch, implement `Standard.Initialize` handshake, implement `HelixAgentClient.Connect` against the live HelixAgent endpoint, populate `GetCapabilities`. |
| `voting`        | STUB (test-only, mostly stub)       | Implement `WeightedVotingSystem.Tally` (weighted aggregation honouring `Config.Method`, `MinAgents`, `TieBreaker`), add real internal-state reset in `Reset`, add additional voting methods + tie-breakers as the orchestrator demands them. |

## Cross-cutting Phase-2 work

1. **Provider wiring.** Introduce a `provider.LLMProvider` interface in
   `digital.vasic.debate/orchestrator` and consume it inside
   `ConductDebate` so synthesised content is replaced by real LLM
   calls. Keep deterministic fallback behind an explicit
   `cfg.Deterministic = true` flag for tests.
2. **Persistence.** Add `internal/storage` package with PostgreSQL +
   in-memory backends behind a common interface. Wire `LessonBank`,
   audit, and reflexion to it.
3. **Streaming.** Define `chan *comprehensive.StreamEvent` plumbing
   through `Orchestrator.ConductDebate` so `StreamDebate` becomes a
   thin adapter.
4. **Challenges.** Every package's `_test.go` covers structural
   guarantees and stub-honesty today. Phase 2 adds Challenge scripts
   under `challenges/banks/debate_orchestrator/` that exercise
   end-to-end flows against real infrastructure.
5. **Documentation.** Update `README.md` tier table after each STUB →
   REAL transition; never claim a package is REAL without a passing
   Challenge run capturing real evidence.
