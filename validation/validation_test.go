package validation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// helper: strict pipeline.
func strictPipeline() *ValidationPipeline {
	return NewValidationPipeline(PipelineConfig{Passes: 4, Strict: true})
}

// helper: non-strict pipeline.
func nonStrictPipeline() *ValidationPipeline {
	return NewValidationPipeline(PipelineConfig{Passes: 4, Strict: false})
}

// 1. Structural pass fails on mismatched braces.
func TestValidationPipeline_Structural_DetectsMismatchedBraces(t *testing.T) {
	p := nonStrictPipeline()
	bad := &Artifact{
		Type:    ArtifactCode,
		Content: "package x\n\nfunc Foo() {\n\tif true {\n\t\treturn\n\t}\n// missing closing brace\n",
	}
	res, err := p.Validate(context.Background(), bad)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	structural := res.PassResults["structural"]
	if structural == nil {
		t.Fatal("structural pass missing from results")
	}
	if structural.Passed {
		t.Fatalf("structural pass expected to fail, got Passed=true; Issues=%v", structural.Issues)
	}
	if len(structural.Issues) == 0 {
		t.Fatal("structural pass should report issues")
	}
	foundBrace := false
	for _, iss := range structural.Issues {
		if strings.Contains(iss, "curly") {
			foundBrace = true
			break
		}
	}
	if !foundBrace {
		t.Fatalf("expected a curly brace issue, got: %v", structural.Issues)
	}
}

// 2. Semantic pass fails on TODO marker in strict mode.
func TestValidationPipeline_Semantic_DetectsTODO(t *testing.T) {
	p := strictPipeline()
	bad := &Artifact{
		Type: ArtifactCode,
		Content: `package x

// TODO: implement Bar
func Foo() int { return 0 }

func main() { _ = Foo() }
`,
	}
	res, err := p.Validate(context.Background(), bad)
	if err == nil {
		t.Fatalf("strict mode should return ErrPipelineFailed when TODO triggers a failure; got nil err and res=%+v", res)
	}
	if !errors.Is(err, ErrPipelineFailed) {
		t.Fatalf("expected ErrPipelineFailed, got %v", err)
	}
	if res == nil {
		t.Fatal("expected partial result")
	}
	// In strict mode, structural passes (no brace issues), then semantic fails.
	sem := res.PassResults["semantic"]
	if sem == nil {
		t.Fatal("semantic pass should have run")
	}
	if sem.Passed {
		t.Fatalf("semantic pass should have failed in strict mode; Issues=%v", sem.Issues)
	}
	foundTodo := false
	for _, iss := range sem.Issues {
		if strings.Contains(iss, "TODO") {
			foundTodo = true
			break
		}
	}
	if !foundTodo {
		t.Fatalf("expected TODO issue, got: %v", sem.Issues)
	}
}

// 3. Consistency pass detects orphan function declaration.
func TestValidationPipeline_Consistency_DetectsOrphanFunc(t *testing.T) {
	p := nonStrictPipeline()
	// Use unexported names: consistency pass heuristically only flags
	// unexported functions as orphans (exported names are presumed
	// package API and may have no in-artefact caller).
	bad := &Artifact{
		Type: ArtifactCode,
		Content: `package x

func used() int { return 1 }
func orphan() int { return 2 }

func Driver() int { return used() }
`,
	}
	res, err := p.Validate(context.Background(), bad)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cons := res.PassResults["consistency"]
	if cons == nil {
		t.Fatal("consistency pass missing")
	}
	if cons.Passed {
		t.Fatalf("consistency pass expected to flag orphan; Issues=%v", cons.Issues)
	}
	foundOrphan := false
	for _, iss := range cons.Issues {
		if strings.Contains(iss, "orphan") {
			foundOrphan = true
			break
		}
	}
	if !foundOrphan {
		t.Fatalf("expected orphan issue, got: %v", cons.Issues)
	}
}

// 4. Quality pass reports long-line issues and lower score.
func TestValidationPipeline_Quality_LongLinesReduceScore(t *testing.T) {
	p := nonStrictPipeline()
	longLine := strings.Repeat("a", 250)
	bad := &Artifact{
		Type:    ArtifactCode,
		Content: "package x\n\nvar X = \"" + longLine + "\"\n\nfunc Use() string { return X }\n",
	}
	res, err := p.Validate(context.Background(), bad)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	q := res.PassResults["quality"]
	if q == nil {
		t.Fatal("quality pass missing")
	}
	if len(q.Issues) == 0 {
		t.Fatal("quality pass should report long-line issue")
	}
	if q.Score >= 1.0 {
		t.Fatalf("quality score should be reduced below 1.0; got %v", q.Score)
	}
	if q.Passed {
		t.Fatalf("quality pass should have failed due to long line; Issues=%v", q.Issues)
	}
}

// 5. All 4 passes green on clean Go snippet.
func TestValidationPipeline_AllPassesGreenCode(t *testing.T) {
	p := nonStrictPipeline()
	clean := &Artifact{
		Type: ArtifactCode,
		Content: `package x

func helper() int { return 42 }

func Driver() int { return helper() }
`,
	}
	res, err := p.Validate(context.Background(), clean)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.OverallPassed {
		t.Fatalf("expected OverallPassed=true; FailedPass=%q passes=%+v", res.FailedPass, res.Passes)
	}
	wantPasses := []string{"structural", "semantic", "consistency", "quality"}
	for _, name := range wantPasses {
		pr := res.PassResults[name]
		if pr == nil {
			t.Fatalf("missing pass: %s", name)
		}
		if !pr.Passed {
			t.Fatalf("pass %q should be green; Issues=%v", name, pr.Issues)
		}
	}
	if len(res.Passes) != 4 {
		t.Fatalf("expected 4 ordered passes, got %d", len(res.Passes))
	}
	if res.QualityScore <= 0 {
		t.Fatalf("expected positive QualityScore, got %v", res.QualityScore)
	}
}

// 6. Strict mode stops at first failure.
func TestValidationPipeline_StrictStopsOnFirstFail(t *testing.T) {
	p := strictPipeline()
	// Mismatched braces -> structural fails first.
	bad := &Artifact{
		Type:    ArtifactCode,
		Content: "package x\nfunc Broken() {\n",
	}
	res, err := p.Validate(context.Background(), bad)
	if err == nil {
		t.Fatal("expected ErrPipelineFailed in strict mode")
	}
	if !errors.Is(err, ErrPipelineFailed) {
		t.Fatalf("expected ErrPipelineFailed, got %v", err)
	}
	if res == nil {
		t.Fatal("expected partial result in strict mode")
	}
	if len(res.Passes) != 1 {
		t.Fatalf("strict mode should stop after first fail; got %d passes: %+v", len(res.Passes), res.Passes)
	}
	if res.Passes[0].Name != "structural" {
		t.Fatalf("first pass should be structural; got %q", res.Passes[0].Name)
	}
	if res.FailedPass != "structural" {
		t.Fatalf("FailedPass should be structural; got %q", res.FailedPass)
	}
}

// 7. Non-strict mode runs all 4 passes.
func TestValidationPipeline_NonStrictRunsAll(t *testing.T) {
	p := nonStrictPipeline()
	// Bad content that fails structural (and possibly others).
	bad := &Artifact{
		Type:    ArtifactCode,
		Content: "package x\nfunc Broken() {\n",
	}
	res, err := p.Validate(context.Background(), bad)
	if err != nil {
		t.Fatalf("non-strict should not error: %v", err)
	}
	if len(res.Passes) != 4 {
		t.Fatalf("non-strict should run all 4 passes; got %d: %+v", len(res.Passes), res.Passes)
	}
	wantNames := map[string]bool{"structural": false, "semantic": false, "consistency": false, "quality": false}
	for _, pr := range res.Passes {
		if _, ok := wantNames[pr.Name]; ok {
			wantNames[pr.Name] = true
		}
	}
	for n, seen := range wantNames {
		if !seen {
			t.Fatalf("missing pass: %s", n)
		}
	}
	if res.OverallPassed {
		t.Fatal("OverallPassed should be false (structural failed)")
	}
}

// 8. Execute accepts raw string input.
func TestValidationPipeline_Execute_StringInput(t *testing.T) {
	p := nonStrictPipeline()
	out, err := p.Execute(context.Background(), "package x\n\nfunc F() int { return F() }\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res, ok := out.(*PipelineResult)
	if !ok {
		t.Fatalf("expected *PipelineResult, got %T", out)
	}
	if len(res.Passes) == 0 {
		t.Fatal("expected at least one pass result")
	}
}

// 9. Execute rejects unsupported input type.
func TestValidationPipeline_Execute_InvalidType(t *testing.T) {
	p := nonStrictPipeline()
	_, err := p.Execute(context.Background(), 42)
	if err == nil {
		t.Fatal("expected ErrInvalidInput, got nil")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}

// 10. Cancelled ctx aborts pipeline early.
func TestValidationPipeline_CtxCancel(t *testing.T) {
	p := nonStrictPipeline()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel
	res, err := p.Validate(ctx, &Artifact{Type: ArtifactCode, Content: "package x\n"})
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	_ = res // may be nil or partial; either acceptable.
}

// Extra sanity: pipeline reports non-zero Duration.
func TestValidationPipeline_DurationReported(t *testing.T) {
	p := nonStrictPipeline()
	start := time.Now()
	res, err := p.Validate(context.Background(), &Artifact{
		Type:    ArtifactCode,
		Content: "package x\nfunc A() int { return 1 }\nfunc B() int { return A() }\n",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Duration <= 0 {
		t.Fatalf("Duration should be positive; got %v", res.Duration)
	}
	if res.Duration > time.Since(start)+time.Second {
		t.Fatalf("Duration sanity-check failed: %v", res.Duration)
	}
}

// Extra sanity: config artefact accepts JSON.
func TestValidationPipeline_ConfigJSONAccepted(t *testing.T) {
	p := nonStrictPipeline()
	a := &Artifact{Type: ArtifactConfig, Content: `{"host":"example","port":8080}`}
	res, err := p.Validate(context.Background(), a)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.PassResults["structural"].Passed {
		t.Fatalf("JSON config should pass structural; Issues=%v", res.PassResults["structural"].Issues)
	}
}

// Extra sanity: config artefact rejects garbage.
func TestValidationPipeline_ConfigGarbageRejected(t *testing.T) {
	p := nonStrictPipeline()
	a := &Artifact{Type: ArtifactConfig, Content: "this is not json or yaml at all !@#$%"}
	res, err := p.Validate(context.Background(), a)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.PassResults["structural"].Passed {
		t.Fatalf("garbage config should fail structural; Issues=%v", res.PassResults["structural"].Issues)
	}
}

// Extra sanity: documentation requires header.
func TestValidationPipeline_DocRequiresHeader(t *testing.T) {
	p := nonStrictPipeline()
	a := &Artifact{Type: ArtifactDocumentation, Content: "just a paragraph with several words but no header"}
	res, err := p.Validate(context.Background(), a)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.PassResults["structural"].Passed {
		t.Fatalf("doc without header should fail structural; Issues=%v", res.PassResults["structural"].Issues)
	}
}

// Extra sanity: prompt placeholder triggers semantic failure.
func TestValidationPipeline_PromptPlaceholderRejected(t *testing.T) {
	p := nonStrictPipeline()
	a := &Artifact{Type: ArtifactPrompt, Content: "Summarise the following text: {{input}} please."}
	res, err := p.Validate(context.Background(), a)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.PassResults["semantic"].Passed {
		t.Fatalf("prompt with unfilled placeholder should fail semantic; Issues=%v", res.PassResults["semantic"].Issues)
	}
}

// Extra sanity: metadata.version validated.
func TestValidationPipeline_MetadataVersionValidated(t *testing.T) {
	p := nonStrictPipeline()
	a := &Artifact{
		Type:     ArtifactCode,
		Content:  "package x\nfunc A() int { return 1 }\nfunc B() int { return A() }\n",
		Metadata: map[string]interface{}{"version": ""},
	}
	res, err := p.Validate(context.Background(), a)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.PassResults["consistency"].Passed {
		t.Fatalf("empty metadata.version should fail consistency; Issues=%v", res.PassResults["consistency"].Issues)
	}
}
