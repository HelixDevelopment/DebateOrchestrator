// Package tools hosts the tool-integration façade. Constructors return
// real values, ListAvailableTools honestly returns an empty slice, and
// the remaining methods are honest NotYetImplemented stubs. Real
// integration is tracked in RECONSTRUCTION_ROADMAP.md.
package tools

import (
	"context"
	"errors"
)

// RAGResult is a single retrieval hit from the RAG sub-tool.
type RAGResult struct {
	// Source is the artefact identifier (URL, file path, doc ID).
	Source string
	// Score is the retrieval relevance score in [0,1].
	Score float64
	// Snippet is the retrieved text fragment.
	Snippet string
}

// CodeDiagnostic is a single LSP-style code diagnostic.
type CodeDiagnostic struct {
	// File is the source file the diagnostic refers to.
	File string
	// Line is the 1-based line number.
	Line int
	// Severity is the human-readable severity ("error", "warning", ...).
	Severity string
	// Message is the diagnostic body.
	Message string
}

// EnrichedContext is the structured context payload produced by tool
// integrations. The current implementation is intentionally minimal —
// callers should treat additional fields as opaque until expanded.
type EnrichedContext struct {
	// Provider is the source tool's identifier.
	Provider string
	// Payload is the raw response body.
	Payload interface{}
	// RAGResults is the set of retrieval-augmented-generation hits.
	RAGResults []RAGResult
	// QueryEmbedding is the embedding vector for the originating query.
	QueryEmbedding []float32
	// CodeDiagnostics is the set of LSP-style diagnostics surfaced.
	CodeDiagnostics []CodeDiagnostic
}

// DebateRequest is the input payload to EnrichDebateContext.
type DebateRequest struct {
	// Query is the debate prompt / user request.
	Query string
	// Context is the surrounding free-form context (round metadata, etc.).
	Context string
}

// ServiceBridge is the implementation-side handle that hosts ToolIntegration.
// The constructor accepts the variadic dependency list HelixAgent
// supplies (mcp, lsp, rag, embedding, formatter, cognitive services).
// All dependencies are currently ignored — captured for future wiring.
type ServiceBridge struct {
	deps        []interface{}
	integration *ToolIntegration
}

// NewServiceBridge constructs a ServiceBridge. The variadic argument
// list is preserved for future option support; current options are stored
// but ignored.
func NewServiceBridge(deps ...interface{}) *ServiceBridge {
	return &ServiceBridge{
		deps:        deps,
		integration: &ToolIntegration{},
	}
}

// GetToolIntegration returns the ToolIntegration owned by the bridge.
// Always non-nil for a ServiceBridge produced by NewServiceBridge.
func (s *ServiceBridge) GetToolIntegration() *ToolIntegration {
	if s == nil {
		return nil
	}
	return s.integration
}

// EnrichDebateContext queries every wired sub-tool to assemble a
// per-round EnrichedContext payload. Honest stub.
func (s *ServiceBridge) EnrichDebateContext(ctx context.Context, req *DebateRequest) (*EnrichedContext, error) {
	// TODO(reconstruction-phase-2): real implementation pending
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_ = req
	return nil, errors.New("debate/tools: ServiceBridge.EnrichDebateContext NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")
}

// ToolIntegration is the public-facing tool surface used by HelixAgent.
type ToolIntegration struct {
	// registered names the tools that have been wired into this
	// integration. Empty until callers register concrete tools.
	registered []string
}

var defaultIntegration = &ToolIntegration{}

// GetToolIntegration returns the package-level ToolIntegration handle.
// Retained for backwards compatibility — prefer
// (*ServiceBridge).GetToolIntegration in new code so the integration
// shares state with the bridge that created it.
func GetToolIntegration() *ToolIntegration {
	return defaultIntegration
}

// QueryRAG runs a retrieval-augmented-generation query.
func (t *ToolIntegration) QueryRAG(ctx context.Context, q string, limit int) (interface{}, error) {
	// TODO(reconstruction-phase-2): real implementation pending
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_, _ = q, limit
	return nil, errors.New("debate/tools: QueryRAG NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")
}

// GetCodeDefinition resolves a code definition at the supplied location.
func (t *ToolIntegration) GetCodeDefinition(ctx context.Context, file string, line, char int) (interface{}, error) {
	// TODO(reconstruction-phase-2): real implementation pending
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_, _, _ = file, line, char
	return nil, errors.New("debate/tools: GetCodeDefinition NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")
}

// GenerateEmbedding produces an embedding vector for the supplied text.
func (t *ToolIntegration) GenerateEmbedding(ctx context.Context, text string) (interface{}, error) {
	// TODO(reconstruction-phase-2): real implementation pending
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_ = text
	return nil, errors.New("debate/tools: GenerateEmbedding NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")
}

// FormatCode reformats the supplied source code for the named language.
func (t *ToolIntegration) FormatCode(ctx context.Context, lang, code string) (interface{}, error) {
	// TODO(reconstruction-phase-2): real implementation pending
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_, _ = lang, code
	return nil, errors.New("debate/tools: FormatCode NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")
}

// InvokeMCPTool calls a Model-Context-Protocol tool by name.
func (t *ToolIntegration) InvokeMCPTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	// TODO(reconstruction-phase-2): real implementation pending
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_, _ = name, args
	return nil, errors.New("debate/tools: InvokeMCPTool NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")
}

// ListAvailableTools returns the names of every available tool. It
// reflects the current registration state: empty when no tools are
// wired, populated once RegisterTool has been called.
func (t *ToolIntegration) ListAvailableTools() []string {
	if t == nil || len(t.registered) == 0 {
		return []string{}
	}
	out := make([]string, len(t.registered))
	copy(out, t.registered)
	return out
}

// IsEnabled reports whether the tool integration has at least one
// registered tool available for invocation. Returns false when no
// tools have been wired (i.e. honest empty state).
func (t *ToolIntegration) IsEnabled() bool {
	if t == nil {
		return false
	}
	return len(t.registered) > 0
}

// RegisterTool records the provided tool name. Empty names are
// rejected. Duplicate registrations are deduplicated.
func (t *ToolIntegration) RegisterTool(name string) error {
	if t == nil {
		return errors.New("debate/tools: nil ToolIntegration")
	}
	if name == "" {
		return errors.New("debate/tools: tool name must be non-empty")
	}
	for _, existing := range t.registered {
		if existing == name {
			return nil
		}
	}
	t.registered = append(t.registered, name)
	return nil
}
