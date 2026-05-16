package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// echoMCPTool is a real MCPTool implementation that echoes its args
// back as the result. Used by TestToolIntegration_MCPTool_RegisterAndInvoke.
type echoMCPTool struct {
	name string
}

func (e *echoMCPTool) Name() string { return e.name }

func (e *echoMCPTool) Invoke(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make(map[string]interface{}, len(args)+1)
	for k, v := range args {
		out[k] = v
	}
	out["__echoed_by__"] = e.name
	return out, nil
}

// freshIntegration produces a clean ToolIntegration not shared with
// any other test (the package-level defaultIntegration is shared and
// state from one test would leak into another).
func freshIntegration() *ToolIntegration {
	return NewServiceBridge().GetToolIntegration()
}

// ---------------------------------------------------------------------
// RAG
// ---------------------------------------------------------------------

func TestToolIntegration_RAG_AddAndQuery(t *testing.T) {
	ti := freshIntegration()
	ctx := context.Background()

	docs := []string{
		"The quick brown fox jumps over the lazy dog",
		"Go is an open-source programming language",
		"Retrieval augmented generation improves grounding",
	}
	for i, d := range docs {
		if err := ti.AddRAGDocument(d, map[string]interface{}{"i": i}); err != nil {
			t.Fatalf("AddRAGDocument[%d]: %v", i, err)
		}
	}

	raw, err := ti.QueryRAG(ctx, "Go", 10)
	if err != nil {
		t.Fatalf("QueryRAG: %v", err)
	}
	results, ok := raw.([]RAGResult)
	if !ok {
		t.Fatalf("QueryRAG returned %T, want []RAGResult", raw)
	}
	if len(results) == 0 {
		t.Fatal("QueryRAG: expected at least one hit, got 0")
	}
	for i, r := range results {
		if r.Content == "" {
			t.Errorf("result[%d]: empty Content", i)
		}
		if r.Score <= 0 {
			t.Errorf("result[%d]: Score %v not > 0", i, r.Score)
		}
	}
}

func TestToolIntegration_RAG_NoMatch(t *testing.T) {
	ti := freshIntegration()
	ctx := context.Background()

	if err := ti.AddRAGDocument("only this", nil); err != nil {
		t.Fatalf("AddRAGDocument: %v", err)
	}

	raw, err := ti.QueryRAG(ctx, "definitely-absent-token-xyzzy", 10)
	if err != nil {
		t.Fatalf("QueryRAG: %v", err)
	}
	results, ok := raw.([]RAGResult)
	if !ok {
		t.Fatalf("QueryRAG returned %T, want []RAGResult", raw)
	}
	if len(results) != 0 {
		t.Fatalf("expected empty slice for no-match, got %d hits", len(results))
	}
}

func TestToolIntegration_RAG_LimitRespected(t *testing.T) {
	ti := freshIntegration()
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		if err := ti.AddRAGDocument(
			fmt.Sprintf("doc %d contains needle for matching", i),
			nil,
		); err != nil {
			t.Fatalf("AddRAGDocument[%d]: %v", i, err)
		}
	}

	raw, err := ti.QueryRAG(ctx, "needle", 3)
	if err != nil {
		t.Fatalf("QueryRAG: %v", err)
	}
	results, _ := raw.([]RAGResult)
	if len(results) != 3 {
		t.Fatalf("limit=3 not respected: got %d results", len(results))
	}
}

// ---------------------------------------------------------------------
// Symbols
// ---------------------------------------------------------------------

func TestToolIntegration_Symbol_Index_Find(t *testing.T) {
	ti := freshIntegration()
	ctx := context.Background()

	type sym struct {
		file        string
		line, char  int
		name, body  string
	}
	syms := []sym{
		{"main.go", 10, 0, "Foo", "func Foo() {}"},
		{"main.go", 20, 4, "Bar", "func Bar() {}"},
		{"main.go", 30, 0, "Baz", "func Baz() {}"},
	}
	for _, s := range syms {
		if err := ti.IndexSymbol(s.file, s.line, s.char, s.name, s.body); err != nil {
			t.Fatalf("IndexSymbol: %v", err)
		}
	}

	// Query at (line=15, char=0) — closest symbol "at or after" is Bar @ (20,4).
	raw, err := ti.GetCodeDefinition(ctx, "main.go", 15, 0)
	if err != nil {
		t.Fatalf("GetCodeDefinition: %v", err)
	}
	def, ok := raw.(*CodeDefinition)
	if !ok {
		t.Fatalf("GetCodeDefinition returned %T, want *CodeDefinition", raw)
	}
	if def.Symbol != "Bar" {
		t.Fatalf("expected Symbol=Bar, got %q", def.Symbol)
	}
	if def.File != "main.go" || def.Line != 20 || def.Char != 4 {
		t.Errorf("wrong position: file=%q line=%d char=%d", def.File, def.Line, def.Char)
	}
	if def.Body == "" {
		t.Error("expected non-empty Body")
	}

	// Exact-match query at (10, 0) should return Foo.
	raw, err = ti.GetCodeDefinition(ctx, "main.go", 10, 0)
	if err != nil {
		t.Fatalf("GetCodeDefinition: %v", err)
	}
	def, _ = raw.(*CodeDefinition)
	if def == nil || def.Symbol != "Foo" {
		t.Fatalf("exact-position query: got %+v, want Foo", def)
	}
}

func TestToolIntegration_Symbol_NotFound(t *testing.T) {
	ti := freshIntegration()
	ctx := context.Background()

	if err := ti.IndexSymbol("a.go", 1, 0, "A", "body"); err != nil {
		t.Fatalf("IndexSymbol: %v", err)
	}

	// Wrong file → not found.
	_, err := ti.GetCodeDefinition(ctx, "other.go", 0, 0)
	if !errors.Is(err, ErrSymbolNotFound) {
		t.Fatalf("expected ErrSymbolNotFound for wrong file, got %v", err)
	}

	// Right file but past all known positions → not found.
	_, err = ti.GetCodeDefinition(ctx, "a.go", 999, 0)
	if !errors.Is(err, ErrSymbolNotFound) {
		t.Fatalf("expected ErrSymbolNotFound for past-end position, got %v", err)
	}
}

// ---------------------------------------------------------------------
// Embeddings
// ---------------------------------------------------------------------

func TestToolIntegration_Embedding_Deterministic(t *testing.T) {
	ti := freshIntegration()
	ctx := context.Background()

	rawA, err := ti.GenerateEmbedding(ctx, "hello world")
	if err != nil {
		t.Fatalf("GenerateEmbedding A1: %v", err)
	}
	rawB, err := ti.GenerateEmbedding(ctx, "hello world")
	if err != nil {
		t.Fatalf("GenerateEmbedding A2: %v", err)
	}
	a := rawA.([]float32)
	b := rawB.([]float32)
	if len(a) != len(b) {
		t.Fatalf("length mismatch: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("non-determinism at idx %d: %v vs %v", i, a[i], b[i])
		}
	}

	rawC, err := ti.GenerateEmbedding(ctx, "something else entirely")
	if err != nil {
		t.Fatalf("GenerateEmbedding C: %v", err)
	}
	c := rawC.([]float32)

	// Different inputs MUST produce different vectors (with overwhelming
	// SHA-256 probability).
	identical := true
	for i := range a {
		if a[i] != c[i] {
			identical = false
			break
		}
	}
	if identical {
		t.Fatal("different inputs produced identical embeddings")
	}
}

func TestToolIntegration_Embedding_Length16(t *testing.T) {
	ti := freshIntegration()
	ctx := context.Background()

	inputs := []string{"", "x", "longer input text for embedding", strings.Repeat("a", 1024)}
	for _, in := range inputs {
		raw, err := ti.GenerateEmbedding(ctx, in)
		if err != nil {
			t.Fatalf("GenerateEmbedding %q: %v", in, err)
		}
		vec, ok := raw.([]float32)
		if !ok {
			t.Fatalf("GenerateEmbedding returned %T", raw)
		}
		if len(vec) != 16 {
			t.Fatalf("vector length %d, want 16 (input=%q)", len(vec), in)
		}
		for i, v := range vec {
			if v < -1.0 || v > 1.0 {
				t.Errorf("vec[%d]=%v out of [-1,1] for input=%q", i, v, in)
			}
		}
	}
}

// ---------------------------------------------------------------------
// FormatCode
// ---------------------------------------------------------------------

func TestToolIntegration_FormatCode_Go_Real(t *testing.T) {
	ti := freshIntegration()
	ctx := context.Background()

	const unformatted = `package x;func f(){}`
	raw, err := ti.FormatCode(ctx, "go", unformatted)
	if err != nil {
		t.Fatalf("FormatCode: %v", err)
	}
	out, ok := raw.(string)
	if !ok {
		t.Fatalf("FormatCode returned %T, want string", raw)
	}

	if !strings.Contains(out, "package x") {
		t.Fatalf("missing 'package x' in output:\n%s", out)
	}
	if !strings.Contains(out, "\n") {
		t.Fatalf("expected newlines in formatted output, got %q", out)
	}
	if !strings.Contains(out, "func f()") {
		t.Fatalf("missing 'func f()' in output:\n%s", out)
	}
	// Stronger: gofmt would not keep both 'package' and 'func' on the
	// same line.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines after formatting, got %d:\n%s", len(lines), out)
	}
}

func TestToolIntegration_FormatCode_Go_ParseError(t *testing.T) {
	ti := freshIntegration()
	ctx := context.Background()

	const broken = `this is not valid go at all`
	raw, err := ti.FormatCode(ctx, "go", broken)
	if !errors.Is(err, ErrFormatFailed) {
		t.Fatalf("expected ErrFormatFailed, got %v", err)
	}
	// Unchanged original is returned alongside the error so the caller
	// can surface the input.
	if raw.(string) != broken {
		t.Fatalf("expected original returned on parse failure")
	}
}

func TestToolIntegration_FormatCode_OtherLanguage(t *testing.T) {
	ti := freshIntegration()
	ctx := context.Background()

	for _, lang := range []string{"python", "javascript", "rust", "lolcode"} {
		const src = "def f():\n    return 1\n"
		raw, err := ti.FormatCode(ctx, lang, src)
		if err != nil {
			t.Fatalf("FormatCode(%q): %v", lang, err)
		}
		if raw.(string) != src {
			t.Fatalf("FormatCode(%q) modified input: %q", lang, raw)
		}
	}
}

// ---------------------------------------------------------------------
// MCPTool
// ---------------------------------------------------------------------

func TestToolIntegration_MCPTool_RegisterAndInvoke(t *testing.T) {
	ti := freshIntegration()
	ctx := context.Background()

	tool := &echoMCPTool{name: "echo"}
	if err := ti.RegisterMCPTool(tool); err != nil {
		t.Fatalf("RegisterMCPTool: %v", err)
	}

	args := map[string]interface{}{"hello": "world", "n": 42}
	raw, err := ti.InvokeMCPTool(ctx, "echo", args)
	if err != nil {
		t.Fatalf("InvokeMCPTool: %v", err)
	}
	out, ok := raw.(map[string]interface{})
	if !ok {
		t.Fatalf("InvokeMCPTool returned %T", raw)
	}
	if out["hello"] != "world" || out["n"] != 42 {
		t.Fatalf("args not echoed: %v", out)
	}
	if out["__echoed_by__"] != "echo" {
		t.Fatalf("invocation did not reach the tool: %v", out)
	}
}

func TestToolIntegration_MCPTool_NotFound(t *testing.T) {
	ti := freshIntegration()
	ctx := context.Background()

	_, err := ti.InvokeMCPTool(ctx, "no-such-tool", nil)
	if !errors.Is(err, ErrMCPToolNotFound) {
		t.Fatalf("expected ErrMCPToolNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------
// EnrichDebateContext
// ---------------------------------------------------------------------

func TestToolIntegration_EnrichContext_Real(t *testing.T) {
	sb := NewServiceBridge()
	ti := sb.GetToolIntegration()
	ctx := context.Background()

	for _, d := range []string{
		"alpha bravo charlie",
		"the cat sat on the mat",
		"alpha foxtrot golf",
	} {
		if err := ti.AddRAGDocument(d, nil); err != nil {
			t.Fatalf("AddRAGDocument: %v", err)
		}
	}

	enr, err := sb.EnrichDebateContext(ctx, &DebateRequest{Query: "alpha"})
	if err != nil {
		t.Fatalf("EnrichDebateContext: %v", err)
	}
	if enr == nil {
		t.Fatal("nil EnrichedContext")
	}
	if len(enr.RAGResults) == 0 {
		t.Fatal("RAGResults empty, expected matches for 'alpha'")
	}
	if len(enr.QueryEmbedding) != 16 {
		t.Fatalf("QueryEmbedding length %d, want 16", len(enr.QueryEmbedding))
	}
	if enr.Provider == "" {
		t.Error("expected non-empty Provider")
	}
}

// ---------------------------------------------------------------------
// Context cancellation across methods
// ---------------------------------------------------------------------

func TestToolIntegration_CtxCancel_Across_Methods(t *testing.T) {
	ti := freshIntegration()

	// Pre-populate so the methods have actual work to attempt.
	_ = ti.AddRAGDocument("populated", nil)
	_ = ti.IndexSymbol("a.go", 0, 0, "A", "body")
	_ = ti.RegisterMCPTool(&echoMCPTool{name: "echo"})

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	cases := []struct {
		name string
		fn   func(context.Context) (interface{}, error)
	}{
		{"QueryRAG", func(c context.Context) (interface{}, error) { return ti.QueryRAG(c, "x", 1) }},
		{"GetCodeDefinition", func(c context.Context) (interface{}, error) { return ti.GetCodeDefinition(c, "a.go", 0, 0) }},
		{"GenerateEmbedding", func(c context.Context) (interface{}, error) { return ti.GenerateEmbedding(c, "x") }},
		{"FormatCode", func(c context.Context) (interface{}, error) { return ti.FormatCode(c, "go", "package x") }},
		{"InvokeMCPTool", func(c context.Context) (interface{}, error) { return ti.InvokeMCPTool(c, "echo", nil) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := c.fn(cancelled)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("%s: expected context.Canceled, got %v", c.name, err)
			}
		})
	}

	// Also a positive test: with a live context every method succeeds.
	live := context.Background()
	for _, c := range cases {
		if _, err := c.fn(live); err != nil && !errors.Is(err, ErrSymbolNotFound) {
			// GetCodeDefinition for (a.go, 0, 0) finds the symbol, so no
			// error expected. Other methods are pre-populated to succeed.
			t.Fatalf("live ctx %s: unexpected error %v", c.name, err)
		}
	}
}

// ---------------------------------------------------------------------
// Concurrency smoke (race-detector substrate)
// ---------------------------------------------------------------------

func TestToolIntegration_Concurrent_AddAndQuery(t *testing.T) {
	ti := freshIntegration()
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = ti.AddRAGDocument(fmt.Sprintf("payload %d needle", i), nil)
		}(i)
	}
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = ti.QueryRAG(ctx, "needle", 5)
		}()
	}
	wg.Wait()
}

// ---------------------------------------------------------------------
// Preserved existing tests
// ---------------------------------------------------------------------

func TestListAvailableToolsHonestlyEmpty(t *testing.T) {
	ti := freshIntegration()
	if got := ti.ListAvailableTools(); len(got) != 0 {
		t.Fatalf("expected empty tool list, got %v", got)
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
	if _, err := sb.EnrichDebateContext(context.Background(), &DebateRequest{Query: "q"}); err != nil {
		t.Fatalf("EnrichDebateContext: %v", err)
	}
}
