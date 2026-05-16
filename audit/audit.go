// Package audit hosts the ProvenanceTracker. The current implementation
// is an honest NotYetImplemented stub; constructors return real values
// but Record returns an explicit error. Real provenance capture is
// tracked in RECONSTRUCTION_ROADMAP.md.
package audit

import (
	"context"
	"errors"
)

// ProvenanceTracker records who-did-what-when audit events.
type ProvenanceTracker struct{}

// NewProvenanceTracker constructs a ProvenanceTracker.
func NewProvenanceTracker() *ProvenanceTracker {
	return &ProvenanceTracker{}
}

// Record persists an audit event.
func (t *ProvenanceTracker) Record(ctx context.Context, event interface{}) error {
	// TODO(reconstruction-phase-2): real implementation pending
	if err := ctx.Err(); err != nil {
		return err
	}
	_ = event
	return errors.New("debate/audit: Record NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")
}
