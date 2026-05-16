// Package topology enumerates the agent-interconnection topologies
// supported by the DebateOrchestrator. This is a REAL implementation.
package topology

// TopologyType identifies the network topology that connects agents
// during a debate. The topology determines which agent can send
// messages to which other agent.
type TopologyType int

// Enumerated TopologyType values. Order is part of the contract.
const (
	// GraphMesh is a fully-connected mesh where every agent may talk
	// to every other agent. Highest fidelity, highest token cost.
	GraphMesh TopologyType = iota
	// Chain is a linear pipeline: agent_0 -> agent_1 -> ... -> agent_n.
	// Lowest token cost; loses cross-pollination.
	Chain
	// Star routes every message through a moderator hub.
	// Good for adversarial-versus-defender patterns.
	Star
)

// String returns the canonical lowercase name for a TopologyType.
func (t TopologyType) String() string {
	switch t {
	case GraphMesh:
		return "graph_mesh"
	case Chain:
		return "chain"
	case Star:
		return "star"
	default:
		return "unknown"
	}
}
