package orchestrator

import (
	"sync"

	"digital.vasic.debate/agents"
)

// AgentPool is a thread-safe registry of available agents.
type AgentPool struct {
	mu     sync.RWMutex
	agents []*Agent
}

// NewAgentPool constructs an empty pool.
func NewAgentPool() *AgentPool {
	return &AgentPool{agents: make([]*Agent, 0, 8)}
}

// Add appends an agent to the pool. Nil agents are ignored.
func (p *AgentPool) Add(agent *Agent) {
	if agent == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.agents = append(p.agents, agent)
}

// Size returns the current pool size.
func (p *AgentPool) Size() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.agents)
}

// List returns a snapshot of every agent in the pool. The returned
// slice is a copy; mutating it does not affect the pool.
func (p *AgentPool) List() []*Agent {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]*Agent, len(p.agents))
	copy(out, p.agents)
	return out
}

// GetByDomain returns every agent in the pool whose Domain matches d.
func (p *AgentPool) GetByDomain(d agents.DomainType) []*Agent {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]*Agent, 0, len(p.agents))
	for _, a := range p.agents {
		if a.Domain == d {
			out = append(out, a)
		}
	}
	return out
}
