# DebateOrchestrator

Go module `digital.vasic.debate` — multi-agent debate orchestration
primitives consumed by HelixAgent. This package is a Phase-1
reconstruction: enough working surface area to compile and exercise the
HelixAgent integration end-to-end, with honest `NotYetImplemented`
stubs everywhere a real implementation is still pending.

## Honesty contract (CONST-035)

Every package in this module falls into one of two tiers:

- **REAL** — runs real code, returns real results, captures real
  timings. Where data is synthesised (e.g. agent content is hashed
  from `(topic, agent_id)` rather than coming from an LLM call), the
  synthesised content is clearly labelled inline and there is no
  silent fall-through to fake data.
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
| `orchestrator`  | REAL | Core `Orchestrator.ConductDebate`, `AgentPool`, `APIAdapter`, sessions.    |
| `comprehensive` | REAL core + STUB streaming | `ExecuteDebate` real; `StreamDebate` stubbed.                     |
| `validation`    | STUB | `NewValidationPipeline` real; `Execute` honest error.                       |
| `audit`         | STUB | `NewProvenanceTracker` real; `Record` honest error.                         |
| `evaluation`    | STUB | `NewBenchmarkBridge` real; `RunBenchmark` honest error.                     |
| `reflexion`     | STUB | All five constructors real; every execution method honest error.            |
| `testing`       | STUB | Constructors + options real; every execution method honest error.           |
| `tools`         | STUB | `ListAvailableTools` honestly empty; every other method honest error.       |

## Build / test

```bash
cd Dependencies/HelixDevelopment/DebateOrchestrator
go mod tidy
go vet ./...
go build ./...
go test ./...
```

All four commands must exit `0`. No external module dependencies are
required — the module is stdlib-only by design.

## How to extend a stub safely

1. Open `RECONSTRUCTION_ROADMAP.md` and find the package's row.
2. Replace the stubbed method body with real code.
3. Remove the `// TODO(reconstruction-phase-2): real implementation pending`
   line.
4. Tighten or replace the package's `_test.go` to assert the new real
   behaviour with captured evidence (per CONST-035).
5. Update the README tier table when the package flips from STUB to REAL.

See `RECONSTRUCTION_ROADMAP.md` for the detailed per-package follow-up list.
