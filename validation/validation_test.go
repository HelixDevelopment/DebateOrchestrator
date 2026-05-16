package validation

import (
	"context"
	"strings"
	"testing"
)

func TestPipelineExecuteIsHonestStub(t *testing.T) {
	p := NewValidationPipeline(DefaultPipelineConfig())
	if p == nil {
		t.Fatal("nil pipeline")
	}
	if p.Config().Passes != 3 {
		t.Fatalf("Config.Passes = %d", p.Config().Passes)
	}
	if _, err := p.Execute(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "NotYetImplemented") {
		t.Fatalf("expected NotYetImplemented stub error, got %v", err)
	}
	if _, err := p.Validate(context.Background(), &Artifact{Type: ArtifactCode}); err == nil || !strings.Contains(err.Error(), "NotYetImplemented") {
		t.Fatalf("expected NotYetImplemented stub error from Validate, got %v", err)
	}
}
