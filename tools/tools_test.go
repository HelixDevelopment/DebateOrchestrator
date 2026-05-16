package tools

import (
	"context"
	"strings"
	"testing"
)

func TestListAvailableToolsHonestlyEmpty(t *testing.T) {
	if got := GetToolIntegration().ListAvailableTools(); len(got) != 0 {
		t.Fatalf("expected empty tool list, got %v", got)
	}
}

func TestStubsReturnNotYetImplemented(t *testing.T) {
	ctx := context.Background()
	ti := GetToolIntegration()
	cases := []struct {
		name string
		err  error
	}{
		{"QueryRAG", mustErr(ti.QueryRAG(ctx, "q", 1))},
		{"GetCodeDefinition", mustErr(ti.GetCodeDefinition(ctx, "f", 0, 0))},
		{"GenerateEmbedding", mustErr(ti.GenerateEmbedding(ctx, "x"))},
		{"FormatCode", mustErr(ti.FormatCode(ctx, "go", "x"))},
		{"InvokeMCPTool", mustErr(ti.InvokeMCPTool(ctx, "n", nil))},
	}
	for _, c := range cases {
		if c.err == nil || !strings.Contains(c.err.Error(), "NotYetImplemented") {
			t.Fatalf("%s: %v", c.name, c.err)
		}
	}
}

func TestServiceBridge(t *testing.T) {
	sb := NewServiceBridge(nil, nil, nil, nil, nil, nil)
	if sb == nil {
		t.Fatal("nil bridge")
	}
	if sb.GetToolIntegration() == nil {
		t.Fatal("nil integration from bridge")
	}
	if _, err := sb.EnrichDebateContext(context.Background(), &DebateRequest{Query: "q"}); err == nil ||
		!strings.Contains(err.Error(), "NotYetImplemented") {
		t.Fatalf("EnrichDebateContext: %v", err)
	}
}

func mustErr(_ interface{}, err error) error { return err }
