// Package protocol hosts the wire-protocol surface for debate
// orchestration: request/response message envelopes, transport
// constructors, and the HelixAgent client adapter. The current
// implementation is an honest stub at the transport/execution layer
// — constructors return real-but-empty values so callers can compose
// struct literals, but every transport and protocol method returns an
// explicit NotYetImplemented error so callers cannot mistake a stub
// for a working endpoint. Full implementation is tracked in
// RECONSTRUCTION_ROADMAP.md.
package protocol

import (
	"context"
	"errors"
	"time"
)

// Protocol is the top-level protocol handler.
type Protocol struct {
	// Name identifies the protocol instance.
	Name string
}

// Standard implements the standard request/response surface.
type Standard struct {
	// Name identifies the standard instance.
	Name string
}

// Config configures a Protocol at construction time.
type Config struct {
	// Name identifies the protocol configuration.
	Name string
	// Version is the protocol version string.
	Version string
	// Timeout is the per-request timeout.
	Timeout time.Duration
}

// FileConfig configures a file-backed transport.
type FileConfig struct {
	// Path is the on-disk path the transport will use.
	Path string
}

// DebateContext carries per-debate metadata across protocol calls.
type DebateContext struct {
	// ID is the debate identifier.
	ID string
	// Topic is the debate topic.
	Topic string
	// Metadata is free-form per-debate metadata.
	Metadata map[string]interface{}
}

// DebateResult captures the outcome of a debate.
type DebateResult struct {
	// ID is the debate identifier the result corresponds to.
	ID string
	// Success indicates whether the debate completed successfully.
	Success bool
	// Content is the result content (typically the conclusion).
	Content string
}

// Message is a single chat-style message exchanged with an agent.
type Message struct {
	// Role is the speaker role ("system", "user", "assistant", …).
	Role string
	// Content is the message content.
	Content string
}

// ChatMessage is an alias for Message preserved for callers that
// import it under the chat-domain name.
type ChatMessage = Message

// PromptMessage is a prompt-template message (named role optional).
type PromptMessage struct {
	// Role is the speaker role.
	Role string
	// Content is the prompt content.
	Content string
	// Name is an optional speaker name (e.g. function name).
	Name string
}

// Prompt is a named prompt template composed of PromptMessages.
type Prompt struct {
	// Name identifies the prompt template.
	Name string
	// Messages is the ordered list of prompt messages.
	Messages []PromptMessage
}

// Resource references an addressable resource (URI + descriptive metadata).
type Resource struct {
	// URI is the canonical resource identifier.
	URI string
	// Name is a human-readable resource name.
	Name string
	// MimeType is the resource MIME type.
	MimeType string
}

// ResourceContent is the inline content of a fetched Resource.
type ResourceContent struct {
	// URI is the resource identifier the content was fetched from.
	URI string
	// Text is the resource body as text.
	Text string
}

// Tool describes a callable tool exposed by an agent.
type Tool struct {
	// Name is the tool name.
	Name string
	// Description is the human-readable tool description.
	Description string
	// InputSchema is the JSON-schema-like description of expected input.
	InputSchema map[string]interface{}
}

// ToolResult is the outcome of a tool invocation.
type ToolResult struct {
	// Content is the tool output payload.
	Content string
	// IsError indicates the tool returned an error result.
	IsError bool
}

// ContentBlock is a single typed content block (text, image, …).
type ContentBlock struct {
	// Type is the content-block type discriminator.
	Type string
	// Text is the textual payload (when Type == "text").
	Text string
}

// Request is a protocol request envelope.
type Request struct {
	// ID is the request identifier.
	ID string
	// Method is the method/operation name.
	Method string
	// Params is the free-form parameter bag.
	Params map[string]interface{}
}

// Response is a protocol response envelope.
type Response struct {
	// ID is the corresponding request identifier.
	ID string
	// Result is the free-form result payload.
	Result interface{}
	// Error is non-nil on protocol-level failure.
	Error error
}

// InitializeResult captures the result of a protocol initialise handshake.
type InitializeResult struct {
	// ProtocolVersion is the negotiated protocol version.
	ProtocolVersion string
	// ServerInfo is the server identification string.
	ServerInfo string
}

// PhaseResponse captures a per-phase response from a federated agent.
type PhaseResponse struct {
	// Phase is the phase identifier.
	Phase string
	// Content is the per-phase content.
	Content string
}

// HelixAgentClient is the adapter for talking to a HelixAgent peer.
type HelixAgentClient struct{}

// AgentInvoker is the function signature used to invoke an agent in
// process — kept as a function type so tests can pass closures.
type AgentInvoker func(ctx context.Context, prompt string) (string, error)

// NewProtocol constructs a Protocol from the supplied configuration.
// The returned protocol is a real, empty handler; Execute is currently
// stubbed.
func NewProtocol(cfg Config) (*Protocol, error) {
	return &Protocol{Name: cfg.Name}, nil
}

// NewStandard constructs a Standard with default identity.
func NewStandard() *Standard {
	return &Standard{Name: "standard"}
}

// NewFileTransport constructs a file-backed transport.
func NewFileTransport(cfg FileConfig) (interface{}, error) {
	// TODO(reconstruction-phase-2): real implementation pending
	_ = cfg
	return nil, errors.New("debate/protocol: NewFileTransport NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")
}

// NewPipeTransport constructs a pipe-backed transport.
func NewPipeTransport() (interface{}, error) {
	// TODO(reconstruction-phase-2): real implementation pending
	return nil, errors.New("debate/protocol: NewPipeTransport NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")
}

// DefaultDebateConfig returns the canonical default debate config.
func DefaultDebateConfig() Config {
	return Config{Name: "default", Version: "1.0", Timeout: 30 * time.Second}
}

// GetString fetches a string value from a free-form parameter map.
// Returns the empty string when the key is absent or the value is not
// a string.
func GetString(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// Name returns the package identifier.
func Name() string {
	return "digital.vasic.debate/protocol"
}

// GetCapabilities returns the set of capabilities advertised by the
// package. The current implementation returns an empty slice; real
// capability negotiation is tracked in RECONSTRUCTION_ROADMAP.md.
func GetCapabilities() []string {
	return []string{}
}

// Execute dispatches a protocol request.
func (p *Protocol) Execute(ctx context.Context, req *Request) (*Response, error) {
	// TODO(reconstruction-phase-2): real implementation pending
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_ = req
	return nil, errors.New("debate/protocol: Execute NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")
}

// HandleFederatedRequest dispatches a federated protocol request.
func (p *Protocol) HandleFederatedRequest(ctx context.Context, req *Request) (*Response, error) {
	// TODO(reconstruction-phase-2): real implementation pending
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_ = req
	return nil, errors.New("debate/protocol: HandleFederatedRequest NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")
}

// Initialize performs the protocol initialise handshake.
func (s *Standard) Initialize(ctx context.Context) (*InitializeResult, error) {
	// TODO(reconstruction-phase-2): real implementation pending
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, errors.New("debate/protocol: Initialize NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")
}

// Connect establishes a connection to the HelixAgent peer.
func (c *HelixAgentClient) Connect(ctx context.Context) error {
	// TODO(reconstruction-phase-2): real implementation pending
	if err := ctx.Err(); err != nil {
		return err
	}
	return errors.New("debate/protocol: Connect NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")
}
