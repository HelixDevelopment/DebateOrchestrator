package topology

import (
	"context"
	"testing"
)

func TestTopologyString(t *testing.T) {
	cases := map[TopologyType]string{
		TopologyGraphMesh:  "graph_mesh",
		TopologyChain:      "chain",
		TopologyStar:       "star",
		TopologyTree:       "tree",
		TopologyType("xx"): "unknown",
	}
	for tt, want := range cases {
		if got := tt.String(); got != want {
			t.Fatalf("TopologyType(%q).String() = %q, want %q", string(tt), got, want)
		}
	}
}

func TestTopologyLegacyAliases(t *testing.T) {
	if GraphMesh != TopologyGraphMesh {
		t.Fatalf("GraphMesh alias drift: got %q, want %q", GraphMesh, TopologyGraphMesh)
	}
	if Chain != TopologyChain {
		t.Fatalf("Chain alias drift: got %q, want %q", Chain, TopologyChain)
	}
	if Star != TopologyStar {
		t.Fatalf("Star alias drift: got %q, want %q", Star, TopologyStar)
	}
}

func TestSelectTopologyTypeIsHonestSelector(t *testing.T) {
	// Small agent count → Chain.
	if got := SelectTopologyType(2, TopologyRequirements{}); got != TopologyChain {
		t.Fatalf("SelectTopologyType(2, {}) = %q, want %q", got, TopologyChain)
	}
	// Ordering requirement → Star.
	if got := SelectTopologyType(8, TopologyRequirements{RequireOrdering: true}); got != TopologyStar {
		t.Fatalf("SelectTopologyType(8, {ordering}) = %q, want %q", got, TopologyStar)
	}
	// Large agent count → Tree.
	if got := SelectTopologyType(20, TopologyRequirements{}); got != TopologyTree {
		t.Fatalf("SelectTopologyType(20, {}) = %q, want %q", got, TopologyTree)
	}
	// Mid-range default → GraphMesh.
	if got := SelectTopologyType(8, TopologyRequirements{}); got != TopologyGraphMesh {
		t.Fatalf("SelectTopologyType(8, {}) = %q, want %q", got, TopologyGraphMesh)
	}
}

func TestTopologyRuntimeInitializeAndRoute(t *testing.T) {
	cfg := DefaultTopologyConfig(TopologyGraphMesh)
	topo, err := NewTopology(TopologyGraphMesh, cfg)
	if err != nil {
		t.Fatalf("NewTopology: unexpected error %v", err)
	}
	defer topo.Close()

	agents := []*Agent{
		CreateAgentFromSpec("a1", RoleModerator, "mock", "m", 8.0, "reasoning"),
		CreateAgentFromSpec("a2", RoleProposer, "mock", "m", 7.5, "code"),
	}
	if err := topo.Initialize(context.Background(), agents); err != nil {
		t.Fatalf("Initialize: unexpected error %v", err)
	}

	if got := topo.GetAgents(); len(got) != 2 {
		t.Fatalf("GetAgents: got %d, want 2", len(got))
	}
	if a, err := topo.GetAgent("a1"); err != nil || a == nil || a.Role != RoleModerator {
		t.Fatalf("GetAgent(a1) = %+v, %v", a, err)
	}
	if got := topo.GetAgentsByRole(RoleProposer); len(got) != 1 {
		t.Fatalf("GetAgentsByRole(Proposer) = %d, want 1", len(got))
	}

	msg := &Message{
		ID:       "m1",
		ToAgents: []string{"a1", "a2", "ghost"},
	}
	routed, err := topo.RouteMessage(msg)
	if err != nil {
		t.Fatalf("RouteMessage: unexpected error %v", err)
	}
	if len(routed) != 2 {
		t.Fatalf("RouteMessage: got %d recipients, want 2 (ghost should be dropped)", len(routed))
	}

	if err := topo.AssignRole("a1", RoleCritic); err != nil {
		t.Fatalf("AssignRole: unexpected error %v", err)
	}
	if a, _ := topo.GetAgent("a1"); a.Role != RoleCritic {
		t.Fatalf("AssignRole did not take effect: got %q", a.Role)
	}
}
