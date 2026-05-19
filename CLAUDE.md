# DebateOrchestrator — AI Agent Manual (CLAUDE.md)

## INHERITED FROM Helix Constitution

This submodule is consumed by Helix-family projects (HelixCode, HelixAgent,
ATMOSphere) that include the Helix Constitution submodule at the consuming
project's `constitution/` path. All rules in `constitution/CLAUDE.md`
(and the `constitution/Constitution.md` it references — universal
anti-bluff covenant §11.4, no-guessing mandate §11.4.6, credentials-
handling mandate §11.4.10, host-session safety §12, data safety §9,
mutation-paired gates §1.1, pre-force-push merge-first §11.4.41) apply
unconditionally to every change landed here.

When this file disagrees with the constitution submodule, the constitution
wins. Locate the constitution submodule from any nested depth via its
`find_constitution.sh` helper.

Canonical reference: <https://github.com/HelixDevelopment/HelixConstitution>

See `CONSTITUTION.md` (this submodule) for the by-reference list of all
cascaded CONST-NNN anchors.

---

## Agent identity

You are an AI agent working on **DebateOrchestrator** — the 8-phase MASTER
protocol implementation (Dehallucination / SelfEvolvement / Proposal /
Critique / Review / Optimization / Adversarial / Convergence) for multi-
agent debate orchestration. Consumed by HelixCode + HelixAgent for
multi-LLM consensus + dissent.

Your mandate: write real, working, tested code. NO simulations, NO
placeholders, NO "for now" implementations. Every protocol phase MUST
actually execute against real LLM endpoints (per CONST-039) when invoked.

---

## Mandatory rules (cascade)

### Real LLM calls only (CONST-035 + CONST-037)
Protocol phases MUST issue real HTTP requests to configured LLM providers.
NEVER `return "simulated response"` or hardcoded outputs.

### No-fakes-beyond-unit-tests (CONST-050(A))
Mocks PERMITTED only in `*_test.go` files invoked WITHOUT the integration
build tag. Production code (`debate.go`, `orchestrator/`, `protocol/`,
`agents/`, `topology/`, `voting/`, `validation/`, `evaluation/`,
`reflexion/`, `tools/`, `gates/`, `audit/`) MUST NOT import any mock
package.

### Anti-bluff posture (CONST-035 / §11.4)
Every test PASS MUST carry positive runtime evidence — at minimum a
`RecordedActions` non-empty + at least one passing `Assertion`. The
challenges/pkg/runner `ValidateAntiBluff` guard enforces this at runtime.

### Force-push discipline (CONST-061 / §11.4.41)
Any force-push to this submodule's remote requires the 4-step merge-first
pipeline AFTER per-operation CONST-043 operator authorisation. Both gates
required.

### Decoupling (CONST-051(B))
This submodule is project-not-aware. NEVER hardcode consuming-project
paths, hostnames, asset names, or naming schemes. Accept parent-project
context via constructor / env var / config file.

---

## Architecture

- `debate.go` — core debate type + helpers
- `orchestrator/` — top-level debate orchestrator
- `protocol/` — 8-phase MASTER protocol (Execute, RegisterAgent, phase
  sequencer). `executionPhases` MUST list all 8 phases (close-out⁷⁵ fix).
- `agents/` — agent abstractions (registration, role assignment)
- `topology/` — debate topologies (star, mesh, etc.)
- `voting/` — vote aggregation
- `validation/` — phase output validation
- `evaluation/` — post-debate scoring
- `reflexion/` — memory + reflection layer
- `tools/` — tool-augmented debate
- `gates/` — phase-transition gates
- `audit/` — audit trail
- `comprehensive/` — StreamDebate end-to-end
- `testing/` — test harness (REAL transport stubs per CONST-050(A))

---

## Build + test commands

```bash
go test -v -count=1 ./...                                 # unit
go test -v -tags=integration -count=1 ./...               # integration
go test -v -race -coverprofile=cover.out ./...            # race+cover
go test -v -run TestProtocol_Execute_AllPhases ./protocol # single
```

---

## Anti-pattern guard

If `grep -rn "simulated\|for now\|TODO implement\|placeholder" --include='*.go' . | grep -v _test.go` returns ANY match in production code, the file is bluffing and the next change MUST tighten the bluff per the §11.4 covenant — fix root cause, do NOT silently absorb.

## CONST-068: Shell-script target-shell-parseability mandate (cascaded from constitution submodule §11.4.67)

> Verbatim user mandate (2026-05-19): *"any issue we spot must be fixed, bash scripts as well if they are broken!"* + *"Make sure that this is mandatory rule!"*

> Verbatim 2026-05-19 operator mandate: *"all existing tests and Challenges do work in anti-bluff manner - they MUST confirm that all tested codebase really works as expected! We had been in position that all tests do execute with success and all Challenges as well, but in reality the most of the features does not work and can't be used! This MUST NOT be the case and execution of tests and Challenges MUST guarantee the quality, the completition and full usability by end users of the product!"*

Every committed shell script MUST be parseable by its target interpreter (`sh -n` for `/bin/sh`, `bash -n` for `/bin/bash`, etc.) AND MUST declare a shebang matching its actual syntax usage. Bash-only constructs (`>(...)`, `<(...)`, `[[ ]]`, `<<<`, arrays, `${var^^}`, etc.) used in scripts that may be invoked via `sh script.sh` MUST be wrapped in `eval` so the parser sees only a string (target shells like mksh parse the entire script before executing — runtime guards cannot save a parse-time rejection). Honest shebangs only: `#!/bin/bash` only if bash actually expected; `#!/bin/sh` requires POSIX-clean body. Fix at source per §11.4.1, never at callsites. Composes with §11.4.1 / §11.4.4 / §11.4.6 / §11.4.50 / §11.4.51. Pre-build gate `CM-SCRIPT-TARGET-SHELL-PARSEABLE` runs `sh -n` on every in-scope script. No escape hatch — no `--skip-parseability-check`, `--bash-only-script`, `--runtime-guard-suffices` flag.

**Cascade requirement:** This anchor (verbatim or by `CONST-068` ID reference) MUST appear in every owned submodule's `CONSTITUTION.md`, `CLAUDE.md`, and `AGENTS.md`. See constitution submodule `Constitution.md` §11.4.67 for the full mandate.
