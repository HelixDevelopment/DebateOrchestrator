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

## CONST-068: Shell-script target-shell-parseability mandate (cascaded from constitution submodule §11.4.67)

> Verbatim user mandate (2026-05-19): *"any issue we spot must be fixed, bash scripts as well if they are broken!"* + *"Make sure that this is mandatory rule!"*

> Verbatim 2026-05-19 operator mandate: *"all existing tests and Challenges do work in anti-bluff manner - they MUST confirm that all tested codebase really works as expected! We had been in position that all tests do execute with success and all Challenges as well, but in reality the most of the features does not work and can't be used! This MUST NOT be the case and execution of tests and Challenges MUST guarantee the quality, the completition and full usability by end users of the product!"*

Every committed shell script MUST be parseable by its target interpreter (`sh -n` for `/bin/sh`, `bash -n` for `/bin/bash`, etc.) AND MUST declare a shebang matching its actual syntax usage. Bash-only constructs (`>(...)`, `<(...)`, `[[ ]]`, `<<<`, arrays, `${var^^}`, etc.) used in scripts that may be invoked via `sh script.sh` MUST be wrapped in `eval` so the parser sees only a string (target shells like mksh parse the entire script before executing — runtime guards cannot save a parse-time rejection). Honest shebangs only: `#!/bin/bash` only if bash actually expected; `#!/bin/sh` requires POSIX-clean body. Fix at source per §11.4.1, never at callsites. Composes with §11.4.1 / §11.4.4 / §11.4.6 / §11.4.50 / §11.4.51. Pre-build gate `CM-SCRIPT-TARGET-SHELL-PARSEABLE` runs `sh -n` on every in-scope script. No escape hatch — no `--skip-parseability-check`, `--bash-only-script`, `--runtime-guard-suffices` flag.

**Cascade requirement:** This anchor (verbatim or by `CONST-068` ID reference) MUST appear in every owned submodule's `CONSTITUTION.md`, `CLAUDE.md`, and `AGENTS.md`. See constitution submodule `Constitution.md` §11.4.67 for the full mandate.

## §11.4.68 — Positive Sink-Side / Downstream Evidence Mandate (cascaded from constitution submodule §11.4.68)

> Verbatim user mandate (2026-05-20): *"We still do not hear any audio played from D3 device! Arvus Web Dashboard when we play music from D3 shows nothing for Codec In Use! This MUST BE investigated and fixed! How come we passed the tests with Arvus validation? What were values for the Codec In Use field? Empty means nothing! This is not working! It MUST BE FIXED, TESTED AND VERIFIED WITH FULL AUTOMATION TESTING ASAP!!!"*

A test that asserts audio or video routing PASS MUST capture and verify **positive sink-side or downstream evidence** — never config-only, never metadata-only, never PCM-open-state-only. At least one of the closed enumeration MUST be captured for every audio/video routing PASS: (1) sink-side codec-state with non-empty Codec-In-Use matching the expected codec regex; (2) strictly-positive PCM frames-written delta from `/proc/asound/.../status hw_ptr`; (3) ALSA ELD/EDID-Like-Data showing negotiated channel count + format; (4) ffprobe-on-captured-mp4 with non-zero frame count + expected codec/resolution/fps; (5) recording-analyzer event match per §11.4.2/§11.4.5; (6) tinycap RMS amplitude above the line-level floor. Empty / `<unreachable>` / `<N.E.>` / `<None>` placeholders are NOT positive evidence; a missing-but-required sink is `OPERATOR-BLOCKED` (release-blocker), never SKIP, never PASS. No escape hatch — no `--skip-sink-evidence`, `--allow-empty-codec`, `--sink-unreachable-is-pass`, `--metadata-only-suffices` flag exists.

**Cascade requirement:** This anchor (verbatim or by `§11.4.68` reference) MUST appear in every owned submodule's `CONSTITUTION.md`, `CLAUDE.md`, and `AGENTS.md`. Severity-equivalent to a §11.4 PASS-bluff at the sink-side-evidence layer.
**Canonical authority:** constitution submodule `Constitution.md` §11.4.68 for the full mandate.


## §11.4.70 — Subagent-Driven Execution Is The Default (cascaded from constitution submodule §11.4.70)

> Verbatim user mandate (2026-05-20): *"Always do if possible Subagent-driven! Add this into our root (constitution Submodule) Constitution.md, CLAUDE.md and AGENTS.md. This should be the default choice ALWAYS!"*

When executing implementation plans (or any task-decomposed execution flow), the **default execution model is subagent-driven** per `superpowers:subagent-driven-development`. Inline execution is permitted ONLY when (a) the task is trivial AND fits a single sub-300-line edit, OR (b) the operator explicitly requests inline at brainstorm-handoff time. Subagent-driven is the default because it gives isolated context per task, naturally enforces two-stage review, is parallel-PWU compatible (§11.4.58), creates an anti-bluff seam (§11.4), and survives operator absence. No escape hatch — `--inline-execution-required`, `--no-subagents`, `--monolithic-execution` are NOT permitted flags. Skipping subagent-driven for non-trivial work without recorded operator authorisation is itself a §11.4 PASS-bluff.

**Cascade requirement:** This anchor (verbatim or by `§11.4.70` reference) MUST appear in every owned submodule's `CONSTITUTION.md`, `CLAUDE.md`, and `AGENTS.md`. Severity-equivalent to a §11.4 PASS-bluff at the execution-model layer.
**Canonical authority:** constitution submodule `Constitution.md` §11.4.70 for the full mandate.


## §11.4.71 — Pre-Push Fetch + Investigate + Integrate Mandate (cascaded from constitution submodule §11.4.71)

> Verbatim user mandate (2026-05-20): *"before pushing changes to any upstream for any repository - main repo or Submodule, we MUST fetch and pull all changes. Once these are obtained WE MUST investigate what is different compared to head position we were on last time before fetching and pulling new changes! We MUST understand what is done and for what purpose, easpecially how that does affect our project and our System in general! Any mandatory changes or improvements required by fresh changes we just have brough in MUST BE incorporated, covered with all supported types of the tests which will produce as a result of its success execution REAL PROOFS of working for all componetns and functionalities covered and work fully in anti-bluff manner!"*

The everyday-push variant of §11.4.41. EVERY push (every repository — main + every submodule) MUST follow the 5-step cycle: (1) fetch all remotes (`git fetch --all --prune --tags`, capture stdout); (2) pull all upstream branches whose tip differs, resolving conflicts per consumer judgment (never auto-`--ours`/`--theirs`); (3) investigate the diff vs OUR previous HEAD — read EVERY foreign commit's body, understand what/why/how-it-affects-our-system; (4) integrate mandatory changes with §11.4.4(b) four-layer coverage + §11.4.43 TDD-fix discipline, every PASS carrying §11.4.5 captured-evidence (REAL PROOFS, not metadata-only); (5) only then push, verifying with `git ls-remote` post-push. No escape hatch — no `--skip-fetch`, `--no-investigate`, `--fast-push`, `--trust-upstream` flag.

**Cascade requirement:** This anchor (verbatim or by `§11.4.71` reference) MUST appear in every owned submodule's `CONSTITUTION.md`, `CLAUDE.md`, and `AGENTS.md`. Severity-equivalent to a §11.4 PASS-bluff at the push-discipline layer.
**Canonical authority:** constitution submodule `Constitution.md` §11.4.71 for the full mandate.


## §11.4.72 — Audio Top-Priority Mandate (cascaded from constitution submodule §11.4.72)

> Verbatim user mandate (2026-05-20): *"Make sure all fixes for audio are always top priority in main working stream!"*

The conductor (main working stream — Claude Code session, AI agent, or human operator) MUST treat audio fixes as the highest-priority class on the serial dispatch queue. Any time the conductor faces a choice between dispatching an audio task vs a non-audio task on the SAME serial resource, the audio task wins. Parallel BACKGROUND subagents (research, refactors, infrastructure documentation) MAY run concurrently with audio work but do NOT preempt audio on the main-stream serial dispatch queue. No escape hatch — there is no "but this non-audio task is faster" or "but this research is more interesting" override; audio-stack regressions are user-perceptible and high-impact while research and refactors can wait.

**Cascade requirement:** This anchor (verbatim or by `§11.4.72` reference) MUST appear in every owned submodule's `CONSTITUTION.md`, `CLAUDE.md`, and `AGENTS.md`. Severity-equivalent to a process violation at the dispatch-priority layer.
**Canonical authority:** constitution submodule `Constitution.md` §11.4.72 for the full mandate.


## §11.4.73 — Main-Specification Document Versioning + Revision Discipline (cascaded from constitution submodule §11.4.73)

> Verbatim user mandate (2026-05-20): *"Make sure everything we add now in previous and upcoming requests IS ALWAYS applied to the main specification — if we have one. Since all these are not major changes we could increase Specification version per change for secondary version instead of the primary. Primary version MUST BE increased for much bigger levels of changes! Add this into root (constitution Submodule) Constitution.md, CLAUDE.md and AGENTS.md as mandatory rule / constraint applicable ONLY IF we have something like the main specification document or we do recognize something like the main specification document. Document MUST BE updated ALWAYS to follow the versioning rules we are appling here + revision and other properties we have!"*

Applies **only when a project recognises a main specification document**. When it does: (1) every additive operator requirement, refinement, or accepted recommendation MUST be applied to the spec before or as part of the implementing work; (2) spec versioning has two axes — *primary* (V1/V2/V3, bumped for major rewrites by explicit operator decision, old versions archived) and *secondary* (the §11.4.61 metadata-table `Revision` integer, bumped for every other change); (3) the metadata table MUST stay current (`Revision`, `Last modified`, `Status summary`, `Fixed`); (4) propagated copies of the rule MUST reference the active `specification.V<primary>.md`, not a stale archive; (5) on primary bump the old file moves to `<spec-dir>/archive/` with `Status: superseded`. Classification: universal, applicable conditionally per the scope condition.

**Cascade requirement:** This anchor (verbatim or by `§11.4.73` reference) MUST appear in every owned submodule's `CONSTITUTION.md`, `CLAUDE.md`, and `AGENTS.md`. Severity-equivalent to a release blocker when a project has a main spec and lets it drift.
**Canonical authority:** constitution submodule `Constitution.md` §11.4.73 for the full mandate.


## §11.4.74 — Submodule-Catalogue-First Discovery + Extend-Don't-Reimplement (cascaded from constitution submodule §11.4.74)

> Verbatim user mandate (2026-05-20): *"We MUST ALWAYS check which already developed features / functionalities do exist as a part of our comprehensive Submodules catalogue located in vasic-digital and HelixDevelopment organizations on GitHub and GitLab both! Project MUST BE aware of all its existence so we do not implement same things multiple times if they are already done as some of existing universal, reusable general development purpose Submodules! For any missing features that some Submodules we incorporate may be missing we MUST IMPLEMENT the properly and extend those Submodules furter! We do control all of the and we CAN and MUST maintain and extend the regularly! All development cycle rules we have MUST BE applied to them and fully respected!"*

Before scaffolding ANY new module, package, helper, or utility, the contributor (human or AI agent) MUST: (1) survey the canonical Submodule catalogue — `vasic-digital` and `HelixDevelopment` on both GitHub AND GitLab; (2) inventory existing Submodules; (3) reuse before reimplement — if a Submodule provides the functionality (or 80%+ of it), add it as a Git submodule rather than write fresh; (4) extend in-place when 80%+ matches but features are missing — add the missing features TO THAT SUBMODULE (PR upstream + bump pointer), never as a duplicating consuming-project helper; (5) apply all development-cycle rules to those Submodules; (6) document the survey result in the feature's tracker entry with a `Catalogue-Check:` field (`reuse <org/repo>@<sha>` / `extend <org/repo>@<sha>` / `no-match <date>`). Classification: universal.

**Cascade requirement:** This anchor (verbatim or by `§11.4.74` reference) MUST appear in every owned submodule's `CONSTITUTION.md`, `CLAUDE.md`, and `AGENTS.md`. Severity-equivalent to a process violation; duplicate implementations landed without catalogue check are release blockers.
**Canonical authority:** constitution submodule `Constitution.md` §11.4.74 for the full mandate.
