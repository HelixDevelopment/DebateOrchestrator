package comprehensive

// RolePrompts groups the canonical system-prompt fragments for every
// Role the comprehensive debate engine understands. The values are
// stable, decoupled from any specific consuming project, and reusable.
//
// Each method returns a non-empty, language-neutral prompt the caller
// can compose with topic-specific context. The text is intentionally
// concise: callers are expected to add per-debate guidance (topic,
// constraints, format) around the role declaration.
//
// CONST-046 note: these strings are role declarations, not user-facing
// content — they are LLM-system prompts whose audience is the model.
// Operator-facing UI text MUST still go through the i18n / LLM-generated
// channels described in CONST-046.
type RolePrompts struct{}

// Architect returns the prompt for the Architect role.
func (RolePrompts) Architect() string {
	return "You are the ARCHITECT. Design the high-level shape of the solution: components, contracts, data flow. Prefer interfaces over implementations; surface explicit trade-offs."
}

// Moderator returns the prompt for the Moderator role.
func (RolePrompts) Moderator() string {
	return "You are the MODERATOR. Keep the debate on-topic, balance airtime across participants, and surface unresolved disagreements explicitly."
}

// Generator returns the prompt for the Generator role.
func (RolePrompts) Generator() string {
	return "You are the GENERATOR. Produce concrete candidate solutions (code, plans, configurations) that implement the architect's design."
}

// BlueTeam returns the prompt for the Blue Team role.
func (RolePrompts) BlueTeam() string {
	return "You are the BLUE TEAM. Defend the proposal: explain why it is correct, robust, and meets the stated requirements; cite evidence."
}

// Critic returns the prompt for the Critic role.
func (RolePrompts) Critic() string {
	return "You are the CRITIC. Identify weaknesses, missed edge cases, contradictions, and unstated assumptions. Be specific and falsifiable."
}

// Tester returns the prompt for the Tester role.
func (RolePrompts) Tester() string {
	return "You are the TESTER. Propose concrete test cases, inputs, expected outputs, and failure modes. Cover happy path, edge cases, and adversarial inputs."
}

// Validator returns the prompt for the Validator role.
func (RolePrompts) Validator() string {
	return "You are the VALIDATOR. Verify that the proposal satisfies every requirement, contract, and invariant. Cite each requirement when judging it satisfied or unmet."
}

// Security returns the prompt for the Security role.
func (RolePrompts) Security() string {
	return "You are the SECURITY reviewer. Identify authentication, authorisation, data-exposure, injection, supply-chain, and resource-abuse risks. Propose concrete mitigations."
}

// Performance returns the prompt for the Performance role.
func (RolePrompts) Performance() string {
	return "You are the PERFORMANCE reviewer. Identify CPU, memory, I/O, network, and concurrency hot spots. Quantify with Big-O and propose measurable optimisations."
}

// RedTeam returns the prompt for the Red Team role.
func (RolePrompts) RedTeam() string {
	return "You are the RED TEAM. Attack the proposal: craft adversarial inputs, abuse-of-feature scenarios, and worst-case operator behaviour. Be ruthless and specific."
}

// Refactoring returns the prompt for the Refactoring role.
func (RolePrompts) Refactoring() string {
	return "You are the REFACTORING reviewer. Identify code smells, duplication, and structural debt; propose specific, behaviour-preserving rewrites."
}

// ForRole returns the canonical prompt for a Role constant, or empty
// string if the role is not recognised.
func (rp RolePrompts) ForRole(r Role) string {
	switch r {
	case RoleArchitect:
		return rp.Architect()
	case RoleModerator:
		return rp.Moderator()
	case RoleGenerator:
		return rp.Generator()
	case RoleBlueTeam:
		return rp.BlueTeam()
	case RoleCritic:
		return rp.Critic()
	case RoleTester:
		return rp.Tester()
	case RoleValidator:
		return rp.Validator()
	case RoleSecurity:
		return rp.Security()
	case RolePerformance:
		return rp.Performance()
	case RoleRedTeam:
		return rp.RedTeam()
	case RoleRefactoring:
		return rp.Refactoring()
	default:
		return ""
	}
}
