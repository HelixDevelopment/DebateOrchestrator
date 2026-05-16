package topology

import "testing"

func TestTopologyString(t *testing.T) {
	cases := map[TopologyType]string{
		GraphMesh:       "graph_mesh",
		Chain:           "chain",
		Star:            "star",
		TopologyType(7): "unknown",
	}
	for tt, want := range cases {
		if got := tt.String(); got != want {
			t.Fatalf("TopologyType(%d).String() = %q, want %q", tt, got, want)
		}
	}
}
