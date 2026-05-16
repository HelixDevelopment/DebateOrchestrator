// Package topology enumerates the agent-interconnection topologies
// supported by the DebateOrchestrator and exposes the runtime
// surface (agents, messages, roles, phase identifiers, and a
// Topology runtime) consumed by the protocol layer.
//
// The constructors return real-but-empty values so callers can
// compose struct literals; the in-process control surface (Initialize,
// RouteMessage, AssignRole, …) keeps in-memory state honestly but
// every method that would normally cross the network or otherwise
// invoke heavy machinery returns an explicit
// `errors.New("debate/topology: <Method> NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")`
// error per the established stub convention. Full implementations are
// tracked in RECONSTRUCTION_ROADMAP.md.
package topology

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// TopologyType identifies the network topology that connects agents
// during a debate. Encoded as a string so callers can use the value
// directly as a sub-test name or log key.
type TopologyType string

// Canonical TopologyType values. The string form is part of the
// public contract — generators, logs, and tests rely on it.
const (
	// TopologyGraphMesh is a fully-connected mesh where every agent
	// may talk to every other agent. Highest fidelity, highest
	// token cost.
	TopologyGraphMesh TopologyType = "graph_mesh"
	// TopologyChain is a linear pipeline:
	// agent_0 -> agent_1 -> ... -> agent_n.
	// Lowest token cost; loses cross-pollination.
	TopologyChain TopologyType = "chain"
	// TopologyStar routes every message through a moderator hub.
	// Good for adversarial-versus-defender patterns.
	TopologyStar TopologyType = "star"
	// TopologyTree organises agents in a hierarchical tree —
	// each level summarises the level below.
	TopologyTree TopologyType = "tree"
)

// Backwards-compatible aliases preserved for callers that imported
// the original short identifiers (pre-reconstruction). Defined as
// `var` (not `const`) so they remain valid even if the underlying
// type evolves further.
var (
	// GraphMesh is the legacy alias for TopologyGraphMesh.
	GraphMesh = TopologyGraphMesh
	// Chain is the legacy alias for TopologyChain.
	Chain = TopologyChain
	// Star is the legacy alias for TopologyStar.
	Star = TopologyStar
)

// String returns the canonical name for a TopologyType.
func (t TopologyType) String() string {
	switch t {
	case TopologyGraphMesh, TopologyChain, TopologyStar, TopologyTree:
		return string(t)
	default:
		return "unknown"
	}
}

// AgentRole names a role an Agent may play during a debate.
type AgentRole string

// Canonical AgentRole values. Roles are intentionally strings so
// callers can extend the registry without recompiling this package.
const (
	// RoleModerator runs the debate loop and routes turns.
	RoleModerator AgentRole = "moderator"
	// RoleProposer offers the initial design / solution.
	RoleProposer AgentRole = "proposer"
	// RoleGenerator generates candidate elaborations.
	RoleGenerator AgentRole = "generator"
	// RoleCritic challenges proposals.
	RoleCritic AgentRole = "critic"
	// RoleReviewer audits the running debate transcript.
	RoleReviewer AgentRole = "reviewer"
	// RoleOptimizer refines accepted proposals.
	RoleOptimizer AgentRole = "optimizer"
	// RoleRedTeam runs adversarial attacks.
	RoleRedTeam AgentRole = "red_team"
	// RoleBlueTeam defends against adversarial attacks.
	RoleBlueTeam AgentRole = "blue_team"
	// RoleValidator validates outputs against acceptance criteria.
	RoleValidator AgentRole = "validator"
	// RoleArchitect contributes high-level architectural rationale.
	RoleArchitect AgentRole = "architect"
)

// DebatePhase names a phase of the canonical 8-phase debate protocol.
type DebatePhase string

// Canonical DebatePhase values, in execution order.
const (
	// PhaseDehallucination grounds agents in shared facts.
	PhaseDehallucination DebatePhase = "dehallucination"
	// PhaseSelfEvolvement lets agents update their priors.
	PhaseSelfEvolvement DebatePhase = "self_evolvement"
	// PhaseProposal collects candidate proposals.
	PhaseProposal DebatePhase = "proposal"
	// PhaseCritique challenges the proposals.
	PhaseCritique DebatePhase = "critique"
	// PhaseReview audits the critiques.
	PhaseReview DebatePhase = "review"
	// PhaseOptimization refines the accepted proposals.
	PhaseOptimization DebatePhase = "optimization"
	// PhaseAdversarial runs red-vs-blue evaluation.
	PhaseAdversarial DebatePhase = "adversarial"
	// PhaseConvergence collapses to a single decision.
	PhaseConvergence DebatePhase = "convergence"
)

// MessageType classifies a message exchanged between agents.
type MessageType string

// Canonical MessageType values.
const (
	// MessageTypeProposal is a proposal-bearing message.
	MessageTypeProposal MessageType = "proposal"
	// MessageTypeCritique is a critique message.
	MessageTypeCritique MessageType = "critique"
	// MessageTypeReview is a review message.
	MessageTypeReview MessageType = "review"
	// MessageTypeVote is a vote message.
	MessageTypeVote MessageType = "vote"
	// MessageTypeNotice is a control-plane notice (start/end of phase).
	MessageTypeNotice MessageType = "notice"
)

// Agent is the canonical representation of a debate participant.
// Concrete invocation is delegated to the protocol layer via
// protocol.AgentInvoker; this struct only carries identity and
// scoring metadata.
type Agent struct {
	// ID uniquely identifies the agent within a topology instance.
	ID string
	// Role is the agent's assigned debate role.
	Role AgentRole
	// Provider names the backing LLM provider (e.g. "openai").
	Provider string
	// Model names the backing model identifier.
	Model string
	// Score is the heuristic preference score for the agent.
	Score float64
	// Specialization is a free-form specialisation tag (e.g. "code").
	Specialization string
}

// CreateAgentFromSpec builds an Agent from positional specification
// arguments. This is the convenience constructor used by tests and
// fixture builders.
func CreateAgentFromSpec(id string, role AgentRole, provider, model string,
	score float64, specialization string) *Agent {
	return &Agent{
		ID:             id,
		Role:           role,
		Provider:       provider,
		Model:          model,
		Score:          score,
		Specialization: specialization,
	}
}

// Message is a single message exchanged between agents.
type Message struct {
	// ID uniquely identifies the message within a topology.
	ID string
	// FromAgent is the sender agent ID.
	FromAgent string
	// ToAgents is the list of recipient agent IDs.
	ToAgents []string
	// Content is the message payload.
	Content string
	// MessageType classifies the message.
	MessageType MessageType
	// Phase is the debate phase the message belongs to.
	Phase DebatePhase
	// Round is the debate round number (1-based).
	Round int
	// Timestamp is the message creation timestamp.
	Timestamp interface{} // typed as interface{} to avoid forcing time on callers
}

// TopologyConfig configures a Topology at construction time.
type TopologyConfig struct {
	// Type is the topology type.
	Type TopologyType
	// MaxAgents is the maximum number of agents permitted.
	MaxAgents int
	// EnableDynamicRoles permits AssignRole after Initialize.
	EnableDynamicRoles bool
}

// DefaultTopologyConfig returns the default configuration for the
// given TopologyType.
func DefaultTopologyConfig(t TopologyType) TopologyConfig {
	return TopologyConfig{
		Type:               t,
		MaxAgents:          32,
		EnableDynamicRoles: true,
	}
}

// TopologyRequirements describes the constraints used by
// SelectTopologyType to choose an appropriate topology.
type TopologyRequirements struct {
	// MaxLatency is the maximum acceptable per-message latency (ms).
	MaxLatency int
	// RequireOrdering enforces strict message ordering.
	RequireOrdering bool
	// MaxParallelism caps the per-step parallelism.
	MaxParallelism int
	// EnableDynamicRoles permits dynamic role re-assignment.
	EnableDynamicRoles bool
}

// SelectTopologyType picks a TopologyType for the supplied agent
// count and requirements. The current implementation is a real-but-
// minimal selector: small agent counts collapse to Chain, ordering
// requirements pick Star, everything else picks GraphMesh. Tree is
// chosen for very large agent counts. Real implementation tracked in
// RECONSTRUCTION_ROADMAP.md.
func SelectTopologyType(agentCount int, req TopologyRequirements) TopologyType {
	if agentCount <= 3 {
		return TopologyChain
	}
	if req.RequireOrdering {
		return TopologyStar
	}
	if agentCount >= 16 {
		return TopologyTree
	}
	return TopologyGraphMesh
}

// Topology is the runtime interface that protocols use to talk to a
// configured agent network.
type Topology interface {
	// Initialize binds the topology to a set of agents.
	Initialize(ctx context.Context, agents []*Agent) error
	// GetAgents returns all agents currently in the topology.
	GetAgents() []*Agent
	// GetAgent returns a single agent by ID.
	GetAgent(id string) (*Agent, error)
	// GetAgentsByRole returns agents matching the supplied role.
	GetAgentsByRole(role AgentRole) []*Agent
	// AssignRole reassigns the role for an agent.
	AssignRole(agentID string, role AgentRole) error
	// RouteMessage routes a message according to the topology's rules.
	RouteMessage(msg *Message) ([]string, error)
	// GetMetrics returns runtime metrics (stub).
	GetMetrics() interface{}
	// GetChannels returns the topology's outbound message channels
	// (stub).
	GetChannels() interface{}
	// Close releases topology resources.
	Close() error
}

// topologyImpl is the in-memory honest-stub implementation of Topology.
type topologyImpl struct {
	cfg    TopologyConfig
	mu     sync.RWMutex
	agents map[string]*Agent
	order  []string
	closed bool
}

// NewTopology constructs a Topology of the requested type.
func NewTopology(t TopologyType, cfg TopologyConfig) (Topology, error) {
	// TODO(reconstruction-phase-2): real implementation pending
	cfg.Type = t
	return &topologyImpl{
		cfg:    cfg,
		agents: make(map[string]*Agent),
	}, nil
}

// Initialize binds the topology to a list of agents.
func (t *topologyImpl) Initialize(ctx context.Context, agents []*Agent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return errors.New("debate/topology: Initialize on closed topology")
	}
	if t.cfg.MaxAgents > 0 && len(agents) > t.cfg.MaxAgents {
		return fmt.Errorf("debate/topology: Initialize agent count %d exceeds MaxAgents %d",
			len(agents), t.cfg.MaxAgents)
	}
	t.agents = make(map[string]*Agent, len(agents))
	t.order = make([]string, 0, len(agents))
	for _, a := range agents {
		if a == nil {
			continue
		}
		t.agents[a.ID] = a
		t.order = append(t.order, a.ID)
	}
	return nil
}

// GetAgents returns a snapshot of all agents in the topology.
func (t *topologyImpl) GetAgents() []*Agent {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]*Agent, 0, len(t.order))
	for _, id := range t.order {
		if a, ok := t.agents[id]; ok {
			out = append(out, a)
		}
	}
	return out
}

// GetAgent returns a single agent by ID.
func (t *topologyImpl) GetAgent(id string) (*Agent, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	a, ok := t.agents[id]
	if !ok {
		return nil, fmt.Errorf("debate/topology: GetAgent unknown ID %q", id)
	}
	return a, nil
}

// GetAgentsByRole returns agents matching the supplied role.
func (t *topologyImpl) GetAgentsByRole(role AgentRole) []*Agent {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]*Agent, 0)
	for _, id := range t.order {
		a, ok := t.agents[id]
		if !ok {
			continue
		}
		if a.Role == role {
			out = append(out, a)
		}
	}
	return out
}

// AssignRole reassigns the role for an existing agent.
func (t *topologyImpl) AssignRole(agentID string, role AgentRole) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.cfg.EnableDynamicRoles {
		return errors.New("debate/topology: AssignRole disabled by config")
	}
	a, ok := t.agents[agentID]
	if !ok {
		return fmt.Errorf("debate/topology: AssignRole unknown agent %q", agentID)
	}
	a.Role = role
	return nil
}

// RouteMessage produces the routed-to-agent list for the supplied
// message. The current routing rule is a real-but-minimal one: the
// recipient list on the message is returned verbatim, filtered to
// known agents. Real per-topology routing is tracked in
// RECONSTRUCTION_ROADMAP.md.
func (t *topologyImpl) RouteMessage(msg *Message) ([]string, error) {
	if msg == nil {
		return nil, errors.New("debate/topology: RouteMessage nil message")
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]string, 0, len(msg.ToAgents))
	for _, id := range msg.ToAgents {
		if _, ok := t.agents[id]; ok {
			out = append(out, id)
		}
	}
	return out, nil
}

// GetMetrics returns runtime metrics. Honest stub.
func (t *topologyImpl) GetMetrics() interface{} {
	// TODO(reconstruction-phase-2): real implementation pending
	t.mu.RLock()
	defer t.mu.RUnlock()
	return map[string]interface{}{
		"agent_count": len(t.agents),
		"closed":      t.closed,
	}
}

// GetChannels returns the topology's outbound message channels.
// Honest stub: returns nil so callers can detect the not-yet-real
// state.
func (t *topologyImpl) GetChannels() interface{} {
	// TODO(reconstruction-phase-2): real implementation pending
	return nil
}

// Close releases topology resources.
func (t *topologyImpl) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed = true
	t.agents = nil
	t.order = nil
	return nil
}
