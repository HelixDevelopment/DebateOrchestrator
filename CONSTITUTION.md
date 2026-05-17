# DebateOrchestrator — Constitution

## INHERITED FROM Helix Constitution

This submodule is consumed by Helix-family projects (HelixCode, HelixAgent,
ATMOSphere, etc.) that include the Helix Constitution submodule at the
consuming project's `constitution/` path. All rules in
`constitution/Constitution.md` apply unconditionally to every change landed
here. The submodule-specific rules below extend them — they never weaken any
universal clause.

When this file disagrees with the constitution submodule, the constitution
wins. Locate the constitution submodule from any nested depth via its
`find_constitution.sh` helper.

Canonical reference: <https://github.com/HelixDevelopment/HelixConstitution>

---

## Cascaded constitutional anchors (by-reference per CONST-047)

The following anchors are cascaded from the constitution submodule and
apply IN FULL to this submodule. Verbatim text lives in the constitution
submodule; consult it as the source of truth.

### Anti-bluff covenant (CONST-035 / Article XI §11.4)
Every PASS in this submodule MUST carry positive runtime evidence captured
during execution. Metadata-only / absence-of-error / grep-based PASS without
runtime evidence are critical defects. See `constitution/Constitution.md`
§11.4 + §11.4.1..§11.4.41 for the full sub-rule set.

### CONST-042 — No-Secret-Leak
No credentials, API keys, tokens, certificates, or other secret material may
be committed. `.env` files (mode 0600) + `.gitignore` enforce.

### CONST-043 — No-Force-Push
Force-push, force-with-lease, history rewrite, and branch deletion of
`main`/`master` require explicit per-operation operator approval.

### CONST-044 — Continuation Document Maintenance
If this submodule maintains its own `docs/CONTINUATION.md` (or equivalent),
it MUST be kept in sync with actual programme state.

### CONST-047 — Recursive Submodule Application
Every engineering deliverable applied to consuming projects applies — fully
and recursively — to this submodule.

### CONST-048 — Full-Automation-Coverage
No feature is deliverable until covered by automation tests proving the six
invariants (anti-bluff + capability proof + impl matches docs + no open
bugs + docs in sync + four-layer test floor).

### CONST-049 — Constitution-Submodule Update Workflow
The 7-step pipeline (fetch+pull first → classify → validate → push all
upstreams → conflict-resolve → cascade verify → bump pointer) applies to
any change against the canonical-root governance trio.

### CONST-050 — No-Fakes-Beyond-Unit-Tests + 100% Test-Type Coverage
Mocks / stubs / placeholders / TODO / FIXME PERMITTED only in unit tests.
All other test types exercise the real system against real infrastructure.

### CONST-051 — Submodules-As-Equal-Codebase + Decoupling + Dependency-Layout
This submodule MUST stay fully decoupled, project-not-aware, reusable,
modular, completely testable. No nested own-org submodule chains.

### CONST-052 — Lowercase-Snake_Case-Naming
All directories, submodules, and files (with technology-preserving
exceptions) MUST use lowercase snake_case names.

### CONST-053 — .gitignore + No-Versioned-Build-Artifacts
Build artefacts, cache files, temp files, sensitive-data files, generated
reports/logs MUST NOT be tracked.

### CONST-054 — Submodule-Dependency-Manifest
This submodule SHOULD ship `helix-deps.yaml` at its root listing its
own-org dependencies (if any).

### CONST-055 — Post-Constitution-Pull Validation
Whenever the constitution submodule is fetched + pulled with any content
change, the consuming project runs the full validation sweep.

### CONST-056 — Mandatory install_upstreams on clone/add
Every clone/add followed by `install_upstreams` invocation if `upstreams/`
recipe directory is present.

### CONST-057 — Type-aware Closure-Status Vocabulary
Bug → `Fixed (→ Fixed.md)`, Feature → `Implemented (→ Fixed.md)`,
Task → `Completed (→ Fixed.md)`.

### CONST-058 — Reopened-Source Attribution
Every Reopened item carries `**Reopened-Details:**` with By / On / Reason
/ Evidence sub-facts.

### CONST-059 — Canonical-Root Inheritance Clarity
This file IS a consumer extension; the constitution submodule's three
governance files ARE the canonical root.

### CONST-060 — Fetch-before-edit
First git-touching action of every session MUST be `git fetch --all
--prune` + `git log --oneline HEAD..@{u}` + recursive submodule fetch.

### CONST-061 — Pre-Force-Push Merge-First Mandate (cascaded from §11.4.41)

> Verbatim user mandate (2026-05-17): *"make sure we bring everything from
> branches to our side before forc push is done! Afer everything is safely
> and fully merged and all potential conflicts (if any) resolved, then do
> force push! make sure nothing isnlost, broken or corrupted on bith sides!"*

Any force-push authorised under CONST-043 MUST be preceded by a 4-step
merge-first pipeline: (1) fetch every remote; (2) integrate every
divergent commit locally; (3) audit the integrated tree (no conflict
markers, no silent drops, tests still pass, captured evidence still
validates); (4) only THEN `git push --force-with-lease`. Two-gate
composition with CONST-043 — both required. Three failure modes
prevented: remote-side content loss, stale-state acts, conflict-driven
corruption. See `constitution/Constitution.md` §11.4.41 for the full
mandate.

---

## Submodule-specific notes

This submodule is the **DebateOrchestrator** — implements the 8-phase
MASTER protocol (Dehallucination / SelfEvolvement / Proposal / Critique /
Review / Optimization / Adversarial / Convergence) for multi-agent debate
orchestration. Consumed by HelixCode and HelixAgent.

### Decoupling invariant (per CONST-051(B))
NEVER inject consuming-project context (paths, hostnames, asset names)
into this submodule. When parent-project info is required, accept it via
constructor parameter, env var, or config file.

### Test-type coverage (per CONST-050(B))
Unit tests under `*_test.go` MAY use mocks. Integration / E2E / chaos /
stress / scaling / performance / benchmark / UI / UX / Challenges /
helix_qa tests MUST exercise the real protocol implementation against
real LLM endpoints (per CONST-039).
