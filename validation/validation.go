// Package validation hosts the multi-pass validation pipeline. The
// current implementation is an honest stub: NewValidationPipeline
// returns a real configured struct, but Execute/Validate return an
// explicit NotYetImplemented error so callers cannot mistake it for
// working validation. Full multi-pass implementation is tracked in
// RECONSTRUCTION_ROADMAP.md.
package validation

import (
	"context"
	"errors"
)

// PipelineConfig configures a ValidationPipeline.
type PipelineConfig struct {
	// Passes is the number of validation passes to run.
	Passes int
	// Strict toggles fail-fast on the first error.
	Strict bool
}

// DefaultPipelineConfig returns a conservative default configuration.
func DefaultPipelineConfig() PipelineConfig {
	return PipelineConfig{Passes: 3, Strict: false}
}

// ArtifactType classifies the kind of artefact handed to the pipeline.
type ArtifactType string

// Artifact types recognised by the pipeline.
const (
	ArtifactCode          ArtifactType = "code"
	ArtifactDocumentation ArtifactType = "documentation"
	ArtifactConfig        ArtifactType = "config"
	ArtifactPrompt        ArtifactType = "prompt"
)

// Artifact is the input payload for the validation pipeline.
type Artifact struct {
	// Type classifies the artefact.
	Type ArtifactType
	// Content is the raw artefact body (source code, markdown, etc.).
	Content string
	// Language is the source language (when applicable).
	Language string
	// Metadata is free-form provenance carried through the pipeline.
	Metadata map[string]interface{}
}

// PassResult is the outcome of a single validation pass.
type PassResult struct {
	// Passed is true iff the pass judged the artefact acceptable.
	Passed bool
	// Score is the per-pass quality score in [0,1].
	Score float64
	// Issues is the per-pass issue list (empty on clean pass).
	Issues []string
}

// PipelineResult is the aggregate outcome across every pass.
type PipelineResult struct {
	// PassResults maps each pass name to its per-pass outcome.
	PassResults map[string]*PassResult
	// OverallPassed is true iff every pass passed.
	OverallPassed bool
	// OverallScore is the aggregate quality score in [0,1].
	OverallScore float64
	// FailedPass is the name of the first failing pass (empty on full pass).
	FailedPass string
}

// ValidationPipeline is the multi-pass validator entry point.
type ValidationPipeline struct {
	cfg PipelineConfig
}

// NewValidationPipeline constructs a ValidationPipeline. The returned
// struct is real and inspectable; only Execute / Validate are
// currently stubbed. Construction cannot fail with empty config so the
// signature returns a single value.
func NewValidationPipeline(cfg PipelineConfig) *ValidationPipeline {
	if cfg.Passes < 0 {
		cfg.Passes = 0
	}
	return &ValidationPipeline{cfg: cfg}
}

// Config returns the configuration the pipeline was constructed with.
func (p *ValidationPipeline) Config() PipelineConfig { return p.cfg }

// Execute runs the validation passes against opaque input.
func (p *ValidationPipeline) Execute(ctx context.Context, input interface{}) (interface{}, error) {
	// TODO(reconstruction-phase-2): real implementation pending
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_ = input
	return nil, errors.New("debate/validation: Execute NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")
}

// Validate runs the validation passes against a typed Artifact and
// returns the structured PipelineResult.
func (p *ValidationPipeline) Validate(ctx context.Context, artifact *Artifact) (*PipelineResult, error) {
	// TODO(reconstruction-phase-2): real implementation pending
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_ = artifact
	return nil, errors.New("debate/validation: Validate NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")
}
