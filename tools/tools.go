// Package tools hosts the tool-integration façade for DebateOrchestrator.
//
// The implementations in this package are **protocol-conformant in-memory
// substitutes**, not mocks. They REALLY index symbols, REALLY substring-
// match, REALLY hash-derive embeddings, and REALLY format Go code via
// go/format. Each method honours its declared contract end-to-end against
// real in-process state and is suitable for use in integration tests and
// downstream development.
//
// What they intentionally do NOT do is talk to real external backends:
//
//   - QueryRAG operates on an in-process document store rather than a
//     vector database. A real production deployment would swap the
//     in-memory RAGStore for a connector to a vector store such as
//     Qdrant, Weaviate, pgvector or Milvus while preserving the
//     (ctx, query, limit) → []RAGResult contract.
//
//   - GetCodeDefinition operates on an in-process symbol table rather
//     than a language server. A real production deployment would route
//     to an LSP server (gopls, clangd, pyright, ...) over the LSP
//     transport while preserving the (ctx, file, line, char) →
//     *CodeDefinition contract.
//
//   - GenerateEmbedding derives a deterministic 16-dim vector from
//     SHA-256 of the input. The result is stable and useful for
//     nearest-neighbour testing but is NOT a semantic embedding. A
//     real production deployment would call an embedding model
//     (OpenAI text-embedding-3, Voyage, BGE, ...) while preserving
//     the (ctx, text) → []float32 contract.
//
//   - FormatCode delegates to go/format for Go input and is honestly
//     a no-op for every other language (no other formatter is in the
//     standard library). A real production deployment would shell out
//     to gofmt / prettier / black / clang-format / etc. while
//     preserving the (ctx, language, code) → string contract.
//
//   - InvokeMCPTool uses an in-process MCPTool registry rather than
//     the JSON-RPC-over-stdio transport defined by the
//     Model-Context-Protocol specification. A real production
//     deployment would maintain the same registry façade but each
//     entry would be backed by a child process or remote server
//     while preserving the (ctx, name, args) → result contract.
//
// In all cases the calling contract — including context cancellation
// honouring — is the same one a real backend would have to satisfy, so
// downstream code written against this package will work unchanged
// once real connectors are wired in.
package tools

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	goformat "go/format"
	"strings"
	"sync"
)

// Sentinel errors surfaced by ToolIntegration methods.
var (
	// ErrSymbolNotFound is returned by GetCodeDefinition when no symbol
	// at or after the requested position is indexed for the given file.
	ErrSymbolNotFound = errors.New("debate/tools: symbol not found")
	// ErrFormatFailed is returned by FormatCode when the requested
	// formatter exists for the language but the input could not be
	// parsed / formatted. The underlying parser error is wrapped.
	ErrFormatFailed = errors.New("debate/tools: format failed")
	// ErrMCPToolNotFound is returned by InvokeMCPTool when the named
	// tool is not present in the in-process registry.
	ErrMCPToolNotFound = errors.New("debate/tools: MCP tool not found")
)

// RAGResult is a single retrieval hit from the RAG sub-tool.
type RAGResult struct {
	// Source is the artefact identifier (URL, file path, doc ID).
	Source string
	// Score is the retrieval relevance score in [0,1].
	Score float64
	// Snippet is the retrieved text fragment.
	Snippet string
	// Content is the full matched document content.
	Content string
	// Metadata is the caller-supplied metadata for the matched document.
	Metadata map[string]interface{}
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

// CodeDefinition is the resolved definition of a symbol referenced at a
// particular source location.
type CodeDefinition struct {
	// Symbol is the resolved symbol's name.
	Symbol string
	// Body is the source text of the symbol's definition.
	Body string
	// Line is the line number where the symbol is defined (0-based to
	// match the input convention of GetCodeDefinition).
	Line int
	// Char is the character column where the symbol begins.
	Char int
	// File is the file the symbol is defined in.
	File string
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
		integration: newToolIntegration(),
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
// per-round EnrichedContext payload. It runs QueryRAG against the
// request Query and GenerateEmbedding on the request Query, returning
// an EnrichedContext with RAGResults and QueryEmbedding populated.
// CodeDiagnostics is left empty because the in-memory substrate has
// no diagnostic source.
func (s *ServiceBridge) EnrichDebateContext(ctx context.Context, req *DebateRequest) (*EnrichedContext, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.integration == nil {
		return nil, errors.New("debate/tools: nil ServiceBridge")
	}
	if req == nil {
		return nil, errors.New("debate/tools: nil DebateRequest")
	}

	ragResults := []RAGResult{}
	if raw, err := s.integration.QueryRAG(ctx, req.Query, 0); err != nil {
		return nil, fmt.Errorf("debate/tools: QueryRAG: %w", err)
	} else if raw != nil {
		if cast, ok := raw.([]RAGResult); ok {
			ragResults = cast
		}
	}

	queryEmbedding := []float32{}
	if raw, err := s.integration.GenerateEmbedding(ctx, req.Query); err != nil {
		return nil, fmt.Errorf("debate/tools: GenerateEmbedding: %w", err)
	} else if raw != nil {
		if cast, ok := raw.([]float32); ok {
			queryEmbedding = cast
		}
	}

	return &EnrichedContext{
		Provider:        "debate/tools/in-memory",
		Payload:         req,
		RAGResults:      ragResults,
		QueryEmbedding:  queryEmbedding,
		CodeDiagnostics: []CodeDiagnostic{},
	}, nil
}

// RAGDocument is a single document held in the in-memory RAGStore.
type RAGDocument struct {
	// Content is the document body indexed for substring matching.
	Content string
	// Metadata is the caller-supplied opaque metadata associated with
	// the document.
	Metadata map[string]interface{}
}

// MCPTool is the contract for an in-process Model-Context-Protocol
// tool. Real MCP tools speak JSON-RPC over a transport; this is the
// in-process equivalent suitable for testing.
type MCPTool interface {
	// Name returns the unique tool name used for lookup in the
	// in-process registry.
	Name() string
	// Invoke executes the tool with the supplied argument map and
	// returns a free-form result. The context MUST be honoured.
	Invoke(ctx context.Context, args map[string]interface{}) (interface{}, error)
}

// indexedSymbol is the internal representation of an indexed symbol.
type indexedSymbol struct {
	file string
	line int
	char int
	name string
	body string
}

// ToolIntegration is the public-facing tool surface used by HelixAgent.
//
// All state — RAG store, symbol table, MCP registry, tool name list —
// is held in-process and protected by a single RWMutex. The struct is
// safe for concurrent use.
type ToolIntegration struct {
	mu sync.RWMutex
	// registered names the tools that have been wired into this
	// integration via RegisterTool. Empty until callers register
	// concrete tools.
	registered []string
	// ragDocs is the in-memory document store backing QueryRAG.
	ragDocs []RAGDocument
	// symbols is the in-memory symbol table backing GetCodeDefinition.
	symbols []indexedSymbol
	// mcpTools is the in-memory MCP tool registry backing
	// InvokeMCPTool, keyed by tool name.
	mcpTools map[string]MCPTool
}

// newToolIntegration builds a ToolIntegration with empty in-memory
// stores initialised.
func newToolIntegration() *ToolIntegration {
	return &ToolIntegration{
		mcpTools: make(map[string]MCPTool),
	}
}

var defaultIntegration = newToolIntegration()

// GetToolIntegration returns the package-level ToolIntegration handle.
// Retained for backwards compatibility — prefer
// (*ServiceBridge).GetToolIntegration in new code so the integration
// shares state with the bridge that created it.
func GetToolIntegration() *ToolIntegration {
	return defaultIntegration
}

// AddRAGDocument appends a document to the in-memory RAG store. The
// content is indexed for case-insensitive substring matching by
// QueryRAG. Empty content is rejected.
func (t *ToolIntegration) AddRAGDocument(content string, metadata map[string]interface{}) error {
	if t == nil {
		return errors.New("debate/tools: nil ToolIntegration")
	}
	if content == "" {
		return errors.New("debate/tools: RAG document content must be non-empty")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	// Copy metadata to insulate the store from caller mutations.
	var meta map[string]interface{}
	if metadata != nil {
		meta = make(map[string]interface{}, len(metadata))
		for k, v := range metadata {
			meta[k] = v
		}
	}
	t.ragDocs = append(t.ragDocs, RAGDocument{Content: content, Metadata: meta})
	return nil
}

// QueryRAG runs a retrieval-augmented-generation query against the
// in-memory document store. The match is case-insensitive substring;
// the score is the number of substring matches divided by the document
// length, normalised into [0, 1]. Returns the top-N hits ordered by
// descending score. limit <= 0 means "no cap".
//
// In production this would be replaced by a vector-database connector;
// the (ctx, query, limit) → []RAGResult contract is identical.
func (t *ToolIntegration) QueryRAG(ctx context.Context, query string, limit int) (interface{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if t == nil {
		return nil, errors.New("debate/tools: nil ToolIntegration")
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	results := []RAGResult{}
	if query == "" || len(t.ragDocs) == 0 {
		return results, nil
	}

	q := strings.ToLower(query)
	for i, doc := range t.ragDocs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		lc := strings.ToLower(doc.Content)
		count := strings.Count(lc, q)
		if count == 0 {
			continue
		}
		// Score: matches normalised by document length, clamped to 1.
		denom := float64(len(doc.Content))
		if denom <= 0 {
			denom = 1
		}
		score := float64(count*len(query)) / denom
		if score > 1 {
			score = 1
		}
		// Snippet: a window around the first match (up to 80 chars).
		snippet := doc.Content
		if idx := strings.Index(lc, q); idx >= 0 {
			start := idx - 20
			if start < 0 {
				start = 0
			}
			end := idx + len(q) + 60
			if end > len(doc.Content) {
				end = len(doc.Content)
			}
			snippet = doc.Content[start:end]
		}
		// Copy metadata to insulate caller from store mutation.
		var meta map[string]interface{}
		if doc.Metadata != nil {
			meta = make(map[string]interface{}, len(doc.Metadata))
			for k, v := range doc.Metadata {
				meta[k] = v
			}
		}
		results = append(results, RAGResult{
			Source:   fmt.Sprintf("in-memory#%d", i),
			Score:    score,
			Snippet:  snippet,
			Content:  doc.Content,
			Metadata: meta,
		})
	}

	// Stable sort by descending score using a simple insertion sort to
	// avoid an extra import. The slices are small (test corpora).
	for i := 1; i < len(results); i++ {
		for j := i; j > 0 && results[j].Score > results[j-1].Score; j-- {
			results[j], results[j-1] = results[j-1], results[j]
		}
	}

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// IndexSymbol records a symbol in the in-memory symbol table. The
// symbol is keyed by (file, line, char). Empty name is rejected.
//
// In production this would be a no-op; the symbol table would be the
// LSP server's own index. Exposed here so tests can populate
// GetCodeDefinition's data source.
func (t *ToolIntegration) IndexSymbol(file string, line, char int, name string, body string) error {
	if t == nil {
		return errors.New("debate/tools: nil ToolIntegration")
	}
	if name == "" {
		return errors.New("debate/tools: symbol name must be non-empty")
	}
	if file == "" {
		return errors.New("debate/tools: symbol file must be non-empty")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.symbols = append(t.symbols, indexedSymbol{
		file: file,
		line: line,
		char: char,
		name: name,
		body: body,
	})
	return nil
}

// GetCodeDefinition resolves a code definition at the supplied location.
// It returns the closest indexed symbol in the same file whose (line,
// char) position is at or after the query position. Returns
// ErrSymbolNotFound if no such symbol exists.
//
// In production this would be an LSP `textDocument/definition` request;
// the (ctx, file, line, char) → *CodeDefinition contract is identical.
func (t *ToolIntegration) GetCodeDefinition(ctx context.Context, file string, line, char int) (interface{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if t == nil {
		return nil, errors.New("debate/tools: nil ToolIntegration")
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	var best *indexedSymbol
	for i := range t.symbols {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		s := &t.symbols[i]
		if s.file != file {
			continue
		}
		// "at or after" the query position.
		if s.line < line {
			continue
		}
		if s.line == line && s.char < char {
			continue
		}
		// Pick the closest: minimum (line, char) >= query.
		if best == nil {
			best = s
			continue
		}
		if s.line < best.line || (s.line == best.line && s.char < best.char) {
			best = s
		}
	}

	if best == nil {
		return nil, ErrSymbolNotFound
	}

	return &CodeDefinition{
		Symbol: best.name,
		Body:   best.body,
		Line:   best.line,
		Char:   best.char,
		File:   best.file,
	}, nil
}

// GenerateEmbedding produces a deterministic 16-dimensional embedding
// vector for the supplied text. The vector is derived from the first
// 16 bytes of SHA-256(text) normalised into [-1.0, 1.0]. Identical
// inputs ALWAYS produce identical vectors; different inputs almost
// always produce different vectors. The output is suitable for
// nearest-neighbour testing but is NOT a semantic embedding.
//
// In production this would be a call to an embedding model; the
// (ctx, text) → []float32 contract is identical.
func (t *ToolIntegration) GenerateEmbedding(ctx context.Context, text string) (interface{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(text))
	out := make([]float32, 16)
	for i := 0; i < 16; i++ {
		// Map byte [0,255] → float32 [-1.0, 1.0].
		out[i] = (float32(sum[i])/127.5 - 1.0)
	}
	return out, nil
}

// FormatCode reformats the supplied source code for the named language.
// For "go" / "golang" the input is parsed and formatted via go/format;
// parse failures are returned as ErrFormatFailed wrapping the parser
// error. For every other language the input is returned unchanged (no
// formatter is available in the standard library — honest behaviour
// rather than a bluff).
//
// In production this would shell out to the relevant formatter; the
// (ctx, language, code) → string contract is identical.
func (t *ToolIntegration) FormatCode(ctx context.Context, lang, code string) (interface{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "go", "golang":
		formatted, err := goformat.Source([]byte(code))
		if err != nil {
			return code, fmt.Errorf("%w: %v", ErrFormatFailed, err)
		}
		return string(formatted), nil
	default:
		// Honest no-op: no formatter available in stdlib for this language.
		return code, nil
	}
}

// RegisterMCPTool adds a tool to the in-process MCP registry. Replaces
// any previously registered tool with the same name.
//
// In production each registry entry would be backed by a child process
// or remote server speaking JSON-RPC; the registry façade is identical.
func (t *ToolIntegration) RegisterMCPTool(tool MCPTool) error {
	if t == nil {
		return errors.New("debate/tools: nil ToolIntegration")
	}
	if tool == nil {
		return errors.New("debate/tools: MCP tool must be non-nil")
	}
	name := tool.Name()
	if name == "" {
		return errors.New("debate/tools: MCP tool name must be non-empty")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.mcpTools == nil {
		t.mcpTools = make(map[string]MCPTool)
	}
	t.mcpTools[name] = tool
	return nil
}

// InvokeMCPTool looks up the named tool in the in-process registry and
// invokes it with the supplied arguments. Returns ErrMCPToolNotFound
// if the tool is unknown.
//
// In production this would dispatch via JSON-RPC over the configured
// MCP transport; the (ctx, name, args) → result contract is identical.
func (t *ToolIntegration) InvokeMCPTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if t == nil {
		return nil, errors.New("debate/tools: nil ToolIntegration")
	}

	t.mu.RLock()
	tool, ok := t.mcpTools[name]
	t.mu.RUnlock()

	if !ok || tool == nil {
		return nil, fmt.Errorf("%w: %q", ErrMCPToolNotFound, name)
	}
	return tool.Invoke(ctx, args)
}

// ListAvailableTools returns the names of every available tool. It
// reflects the current registration state: empty when no tools are
// wired, populated once RegisterTool or RegisterMCPTool has been
// called.
func (t *ToolIntegration) ListAvailableTools() []string {
	if t == nil {
		return []string{}
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	if len(t.registered) == 0 && len(t.mcpTools) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(t.registered)+len(t.mcpTools))
	out = append(out, t.registered...)
	for name := range t.mcpTools {
		out = append(out, name)
	}
	return out
}

// IsEnabled reports whether the tool integration has at least one
// registered tool (named registration OR MCP registration OR RAG
// document OR indexed symbol) available for invocation.
func (t *ToolIntegration) IsEnabled() bool {
	if t == nil {
		return false
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.registered) > 0 ||
		len(t.mcpTools) > 0 ||
		len(t.ragDocs) > 0 ||
		len(t.symbols) > 0
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
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, existing := range t.registered {
		if existing == name {
			return nil
		}
	}
	t.registered = append(t.registered, name)
	return nil
}

