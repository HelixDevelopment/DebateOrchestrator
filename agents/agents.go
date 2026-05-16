// Package agents provides agent-domain typing primitives used by the
// DebateOrchestrator subsystem. This is a REAL implementation (no stubs).
package agents

// DomainType classifies the subject-matter domain an agent operates over.
// Domains let the orchestrator route prompts to specialist agents.
type DomainType int

// Enumerated DomainType values. Add new domains at the end to preserve
// stable iota ordering for any persisted data.
const (
	// DomainGeneral is the catch-all domain for agents without a more
	// specific specialisation.
	DomainGeneral DomainType = iota
	// DomainCode marks an agent specialised for code-centric tasks.
	DomainCode
)

// String returns a human-readable name for the DomainType.
func (d DomainType) String() string {
	switch d {
	case DomainGeneral:
		return "general"
	case DomainCode:
		return "code"
	default:
		return "unknown"
	}
}
