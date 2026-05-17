// Package protocol hosts the wire-protocol surface for debate
// orchestration: request/response message envelopes, transport
// implementations (file-based and pipe-based IPC), the HelixAgent
// client adapter, and the in-process debate Protocol runner.
//
// Transport, ExecuteRequest, HandleFederatedRequest, Standard.Initialize,
// and HelixAgentClient.Connect are real, working implementations
// (Phase 2 promotion from the previous honest-stub posture).
package protocol

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"digital.vasic.debate/topology"
)

// ProtocolVersion is the wire-protocol version advertised in
// initialise handshakes.
const ProtocolVersion = "1.0"

// =============================================================================
// Sentinel errors (debate-runner surface)
// =============================================================================

// ErrInvalidConfig is returned by Protocol.Execute when the bound
// Config fails validation (empty Topic, non-positive MaxRounds, etc.).
var ErrInvalidConfig = errors.New("debate/protocol: invalid config")

// ErrNoAgentsConfigured is returned by Protocol.Execute when no
// AgentInvoker has been registered via RegisterAgent before Execute
// is called.
var ErrNoAgentsConfigured = errors.New("debate/protocol: no agents configured")

// ErrInvalidRequest is returned by ExecuteRequest /
// HandleFederatedRequest when the supplied *Request fails baseline
// validation (nil request, empty Method, …).
var ErrInvalidRequest = errors.New("debate/protocol: invalid request")

// ErrUnknownMethod is returned by ExecuteRequest when no handler is
// registered for the request's Method.
var ErrUnknownMethod = errors.New("debate/protocol: unknown method")

// ErrUnsupportedFederatedMethod is returned by HandleFederatedRequest
// when the request's Method is not in the federated method allow-list.
var ErrUnsupportedFederatedMethod = errors.New(
	"debate/protocol: unsupported federated method")

// ErrNoEndpoint is returned by HelixAgentClient.Connect when the
// client has no Endpoint configured to dial.
var ErrNoEndpoint = errors.New("debate/protocol: no endpoint configured")

// ErrTransportClosed is returned by Transport.Send / Transport.Recv
// after the Transport has been Close()d.
var ErrTransportClosed = errors.New("debate/protocol: transport closed")

// =============================================================================
// Wire-protocol surface (request/response/transport envelopes)
// =============================================================================

// Standard implements the standard request/response surface.
type Standard struct {
	// Name identifies the standard instance.
	Name string
}

// FileConfig configures a file-backed transport. The transport
// reads inbound responses from InPath and writes outbound requests to
// OutPath using newline-delimited JSON framing. If only Path is set
// (legacy single-file callers), the transport opens that path for
// both read and write — useful for offline replay against a captured
// session log.
type FileConfig struct {
	// Path is the legacy single-on-disk path the transport uses for
	// both directions. Used when InPath and OutPath are empty.
	Path string
	// InPath is the on-disk path responses are read from.
	InPath string
	// OutPath is the on-disk path requests are written to.
	OutPath string
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

// HelixAgentClient is the adapter for talking to a HelixAgent peer
// over TCP. Endpoint is dialled by Connect; the resulting net.Conn is
// retained on the struct so subsequent reads/writes can flow through
// the same connection.
type HelixAgentClient struct {
	// Endpoint is the TCP address (host:port) the client dials.
	Endpoint string
	// DialTimeout caps the dial duration. Zero defers to ctx.
	DialTimeout time.Duration

	mu     sync.Mutex
	conn   net.Conn
	closed bool
}

// NewHelixAgentClient constructs a HelixAgentClient bound to the
// supplied TCP endpoint.
func NewHelixAgentClient(endpoint string) *HelixAgentClient {
	return &HelixAgentClient{Endpoint: endpoint}
}

// =============================================================================
// Debate-protocol surface (Config, Protocol runner, AgentInvoker, results)
// =============================================================================

// Config configures the debate Protocol at construction time. It
// carries both wire-protocol identity (Name/Version) and per-debate
// runner knobs (Topic/Context/MaxRounds/Timeout/…) so the legacy
// transport callers and the new debate runner can share it.
type Config struct {
	// Name identifies the protocol/debate configuration.
	Name string
	// Version is the protocol version string.
	Version string
	// Timeout is the overall per-debate timeout.
	Timeout time.Duration

	// Topic is the human-readable debate topic.
	Topic string
	// Context is supplementary debate context.
	Context string
	// MaxRounds caps the number of debate rounds.
	MaxRounds int
	// EnableEarlyExit allows the debate to terminate when consensus
	// is reached before MaxRounds.
	EnableEarlyExit bool
	// MinConsensusScore is the consensus threshold for early exit.
	MinConsensusScore float64
	// TopologyType is the topology type the debate executes against
	// (informational — the actual Topology is passed to NewProtocol).
	TopologyType topology.TopologyType
	// Metadata is free-form per-debate metadata.
	Metadata map[string]interface{}
}

// DebateContext carries per-debate, per-phase metadata across
// AgentInvoker calls.
type DebateContext struct {
	// ID is the debate identifier.
	ID string
	// Topic is the debate topic.
	Topic string
	// Round is the current 1-based round number.
	Round int
	// CurrentPhase is the current debate phase.
	CurrentPhase topology.DebatePhase
	// Metadata is free-form per-debate metadata.
	Metadata map[string]interface{}
}

// PhaseResponse captures a per-phase response from a federated agent.
type PhaseResponse struct {
	// AgentID identifies the responding agent.
	AgentID string
	// Role is the responding agent's role.
	Role topology.AgentRole
	// Provider is the responding agent's provider.
	Provider string
	// Model is the responding agent's model identifier.
	Model string
	// Phase is the phase identifier the response belongs to.
	Phase topology.DebatePhase
	// Content is the per-phase response content.
	Content string
	// Confidence is the agent's self-reported confidence.
	Confidence float64
	// Vote is the agent's vote in a convergence phase.
	Vote string
	// Score is the agent's heuristic score.
	Score float64
	// Latency is the wall-clock latency of the agent invocation.
	Latency time.Duration
	// Timestamp is the response timestamp.
	Timestamp time.Time
	// Arguments is the agent's structured argument list.
	Arguments []string
	// Suggestions is the agent's structured suggestion list.
	Suggestions []string
	// Metadata is free-form per-response metadata.
	Metadata map[string]interface{}
}

// PhaseResult captures the outcome of a single debate phase.
type PhaseResult struct {
	// Phase is the phase identifier.
	Phase topology.DebatePhase
	// Round is the 1-based round number this phase executed in.
	Round int
	// Responses is the list of per-agent responses collected.
	Responses []*PhaseResponse
	// Duration is the wall-clock duration of the phase.
	Duration time.Duration
}

// DebateMetrics aggregates per-debate counters.
type DebateMetrics struct {
	// TotalResponses is the total number of agent responses
	// collected across all phases.
	TotalResponses int
	// TotalInvocations is the total number of AgentInvoker calls.
	TotalInvocations int
}

// ConsensusResult captures the convergence outcome of a debate.
type ConsensusResult struct {
	// Choice is the consensus choice.
	Choice string
	// Confidence is the consensus confidence.
	Confidence float64
	// Contributors lists the agent IDs that contributed to consensus.
	Contributors []string
}

// DebateResult captures the outcome of a debate run.
type DebateResult struct {
	// ID is the debate identifier the result corresponds to.
	ID string
	// Topic is the debate topic (echo of Config.Topic).
	Topic string
	// Success indicates whether the debate completed successfully.
	Success bool
	// Content is the result content (typically the conclusion).
	Content string
	// RoundsCompleted is the number of rounds actually completed.
	RoundsCompleted int
	// TopologyUsed is the topology type the debate ran against.
	TopologyUsed topology.TopologyType
	// Phases is the per-phase result list in execution order.
	Phases []*PhaseResult
	// Metrics is the aggregate metrics for the debate.
	Metrics *DebateMetrics
	// Duration is the wall-clock duration of the entire debate.
	Duration time.Duration
	// EarlyExit is true if the debate exited before MaxRounds.
	EarlyExit bool
	// EarlyExitReason is the reason the debate exited early.
	EarlyExitReason string
	// FinalConsensus is the consensus outcome, if any.
	FinalConsensus *ConsensusResult
}

// AgentInvoker is the abstraction the debate Protocol uses to
// invoke a single agent at a given phase. Implementations may bind
// to a live LLM, a canned response generator, or any other backing
// surface.
type AgentInvoker interface {
	// Invoke runs the agent for the supplied prompt within the
	// supplied debate context and returns the agent's per-phase
	// response.
	Invoke(ctx context.Context, agent *topology.Agent, prompt string,
		debateCtx DebateContext) (*PhaseResponse, error)
}

// AgentInvokerFunc is a function adapter for AgentInvoker.
type AgentInvokerFunc func(ctx context.Context, agent *topology.Agent,
	prompt string, debateCtx DebateContext) (*PhaseResponse, error)

// Invoke satisfies AgentInvoker by delegating to the wrapped function.
func (f AgentInvokerFunc) Invoke(ctx context.Context, agent *topology.Agent,
	prompt string, debateCtx DebateContext) (*PhaseResponse, error) {
	return f(ctx, agent, prompt, debateCtx)
}

// RequestHandler handles a single wire-protocol request dispatched
// through Protocol.ExecuteRequest. Handlers receive the unwrapped
// parameter bag and return an arbitrary Result payload (placed
// verbatim into the *Response.Result field).
type RequestHandler func(ctx context.Context,
	params map[string]interface{}) (interface{}, error)

// Protocol is the in-process debate Protocol runner.
type Protocol struct {
	// Name identifies the protocol instance.
	Name string
	// Config is the configuration this Protocol was built from.
	Config Config
	// Topology is the agent topology the Protocol runs against.
	Topology topology.Topology
	// Invoker is the AgentInvoker used to dispatch per-agent calls
	// when no per-agent invoker has been registered. May be nil if
	// callers exclusively use RegisterAgent.
	Invoker AgentInvoker

	// mu guards agents + handlers.
	mu sync.RWMutex
	// agents maps agent ID -> AgentInvoker. Populated via
	// RegisterAgent; consulted by Execute to dispatch per-agent calls.
	agents map[string]AgentInvoker
	// handlers maps method-name -> RequestHandler. Populated via
	// RegisterHandler; consulted by ExecuteRequest /
	// HandleFederatedRequest to route inbound wire-protocol requests.
	handlers map[string]RequestHandler
}

// RegisterHandler registers a RequestHandler under the supplied
// method name. Subsequent calls with the same name replace the prior
// handler. Empty names and nil handlers are rejected with
// ErrInvalidRequest.
func (p *Protocol) RegisterHandler(method string, h RequestHandler) error {
	if method == "" {
		return fmt.Errorf("%w: RegisterHandler empty method", ErrInvalidRequest)
	}
	if h == nil {
		return fmt.Errorf("%w: RegisterHandler nil handler for %q",
			ErrInvalidRequest, method)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.handlers == nil {
		p.handlers = make(map[string]RequestHandler)
	}
	p.handlers[method] = h
	return nil
}

// handler returns the registered RequestHandler for the supplied
// method, or (nil, false) if no handler is registered.
func (p *Protocol) handler(method string) (RequestHandler, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	h, ok := p.handlers[method]
	return h, ok
}

// RegisterAgent registers an AgentInvoker under the supplied agent
// ID. Subsequent calls with the same ID replace the prior invoker.
// Empty IDs and nil invokers are rejected.
func (p *Protocol) RegisterAgent(id string, invoker AgentInvoker) error {
	if id == "" {
		return fmt.Errorf("%w: RegisterAgent empty id", ErrInvalidConfig)
	}
	if invoker == nil {
		return fmt.Errorf("%w: RegisterAgent nil invoker for %q",
			ErrInvalidConfig, id)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.agents == nil {
		p.agents = make(map[string]AgentInvoker)
	}
	p.agents[id] = invoker
	return nil
}

// Agents returns the registered agent IDs in stable sorted order so
// Execute (and tests) iterate agents deterministically.
func (p *Protocol) Agents() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	ids := make([]string, 0, len(p.agents))
	for id := range p.agents {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// NewProtocol constructs a debate Protocol bound to the supplied
// Config, Topology, and AgentInvoker.
//
// Both signatures are supported via variadic opts so callers can
// continue to call `NewProtocol(cfg)` (legacy transport wiring) or
// the new `NewProtocol(cfg, topo, invoker)` form (debate runner).
// The variadic form is decoded positionally — opts[0] = Topology,
// opts[1] = AgentInvoker. Extra arguments are ignored to keep the
// stub forward-compatible.
func NewProtocol(cfg Config, opts ...interface{}) *Protocol {
	p := &Protocol{
		Name:     cfg.Name,
		Config:   cfg,
		agents:   make(map[string]AgentInvoker),
		handlers: make(map[string]RequestHandler),
	}
	if len(opts) >= 1 {
		if topo, ok := opts[0].(topology.Topology); ok {
			p.Topology = topo
		}
	}
	if len(opts) >= 2 {
		if inv, ok := opts[1].(AgentInvoker); ok {
			p.Invoker = inv
		}
	}
	return p
}

// executionPhases is the canonical, executed-in-order phase list the
// debate runner drives for every round. Implements the full 8-phase
// MASTER protocol per topology.DebatePhase: Dehallucination →
// SelfEvolvement → Proposal → Critique → Review → Optimization →
// Adversarial → Convergence. Per-phase responses stay mechanically
// distinguishable because the agent-prompt composer includes the
// CurrentPhase as a discriminator (see buildAgentPrompt in this file).
//
// Close-out⁷⁵: the prior 4-phase subset (Proposal/Critique/Optimization/
// Convergence) was a partial implementation that left
// TestDebateFullProtocol_8Phases honestly FAILing. Expanded to the
// full 8 phases per the topology contract.
var executionPhases = []topology.DebatePhase{
	topology.PhaseDehallucination,
	topology.PhaseSelfEvolvement,
	topology.PhaseProposal,
	topology.PhaseCritique,
	topology.PhaseReview,
	topology.PhaseOptimization, // "refinement"
	topology.PhaseAdversarial,
	topology.PhaseConvergence, // "consensus"
}

// newDebateID returns a random 128-bit hex identifier suitable as a
// debate ID. Falls back to a time-based ID if the system RNG is
// unavailable so Execute never silently produces a colliding ID.
func newDebateID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err == nil {
		return hex.EncodeToString(buf[:])
	}
	return fmt.Sprintf("debate-%d", time.Now().UnixNano())
}

// Execute runs the debate Protocol end-to-end and returns the
// aggregated DebateResult.
//
// Real orchestration runtime: validates the bound Config, initialises
// a fresh DebateContext, then drives MaxRounds * len(executionPhases)
// iterations. Each phase invokes every registered AgentInvoker with a
// prompt composed from topic + phase + prior-round transcript;
// per-agent responses are appended to the per-phase PhaseResult.
// After every round a deterministic substring-similarity heuristic
// produces a ConsensusResult; when EnableEarlyExit is set and the
// confidence meets/exceeds MinConsensusScore the runtime exits
// before MaxRounds. ctx.Done() is honoured at every loop boundary —
// cancellation aborts and returns ctx.Err().
func (p *Protocol) Execute(ctx context.Context) (*DebateResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// (1) Validate config.
	if strings.TrimSpace(p.Config.Topic) == "" {
		return nil, fmt.Errorf("%w: Topic empty", ErrInvalidConfig)
	}
	if p.Config.MaxRounds <= 0 {
		return nil, fmt.Errorf("%w: MaxRounds=%d (must be > 0)",
			ErrInvalidConfig, p.Config.MaxRounds)
	}

	// (2) Snapshot registered agents (deterministic order).
	agentIDs := p.Agents()
	if len(agentIDs) == 0 {
		return nil, ErrNoAgentsConfigured
	}
	p.mu.RLock()
	invokers := make(map[string]AgentInvoker, len(agentIDs))
	for _, id := range agentIDs {
		invokers[id] = p.agents[id]
	}
	p.mu.RUnlock()

	// (3) Build DebateContext + result skeleton.
	debateID := newDebateID()
	debateCtx := DebateContext{
		ID:       debateID,
		Topic:    p.Config.Topic,
		Round:    0,
		Metadata: copyMetadata(p.Config.Metadata),
	}
	result := &DebateResult{
		ID:           debateID,
		Topic:        p.Config.Topic,
		TopologyUsed: p.Config.TopologyType,
		Phases:       make([]*PhaseResult, 0,
			p.Config.MaxRounds*len(executionPhases)),
		Metrics: &DebateMetrics{},
	}

	debateStart := time.Now()

	// Track responses across phases / rounds so prompt construction
	// for later phases can incorporate earlier transcript.
	var allResponses []*PhaseResponse
	var lastConsensus *ConsensusResult
	var earlyExit bool
	var earlyExitReason string
	roundsCompleted := 0

	// (4) Drive rounds.
RoundsLoop:
	for round := 1; round <= p.Config.MaxRounds; round++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		debateCtx.Round = round

		roundResponses := make([]*PhaseResponse, 0,
			len(executionPhases)*len(agentIDs))

		for _, phase := range executionPhases {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			debateCtx.CurrentPhase = phase

			phaseStart := time.Now()
			phaseResult := &PhaseResult{
				Phase:     phase,
				Round:     round,
				Responses: make([]*PhaseResponse, 0, len(agentIDs)),
			}

			for _, id := range agentIDs {
				if err := ctx.Err(); err != nil {
					phaseResult.Duration = time.Since(phaseStart)
					result.Phases = append(result.Phases, phaseResult)
					return result, err
				}
				invoker := invokers[id]
				prompt := buildPhasePrompt(
					p.Config.Topic, p.Config.Context, phase, round,
					allResponses)
				agent := &topology.Agent{ID: id}
				if p.Topology != nil {
					if a, err := p.Topology.GetAgent(id); err == nil && a != nil {
						agent = a
					}
				}

				invokeStart := time.Now()
				resp, invErr := invoker.Invoke(ctx, agent, prompt, debateCtx)
				invokeLatency := time.Since(invokeStart)
				result.Metrics.TotalInvocations++

				if invErr != nil {
					// Honour ctx.Err() over the wrapped invoker err
					// so callers see context.Canceled when the
					// cancellation is the real cause.
					if ctxErr := ctx.Err(); ctxErr != nil {
						phaseResult.Duration = time.Since(phaseStart)
						result.Phases = append(result.Phases, phaseResult)
						return result, ctxErr
					}
					phaseResult.Duration = time.Since(phaseStart)
					result.Phases = append(result.Phases, phaseResult)
					return result, fmt.Errorf(
						"debate/protocol: agent %q invoke failed at "+
							"round %d phase %s: %w",
						id, round, phase, invErr)
				}
				if resp == nil {
					phaseResult.Duration = time.Since(phaseStart)
					result.Phases = append(result.Phases, phaseResult)
					return result, fmt.Errorf(
						"debate/protocol: agent %q returned nil "+
							"response at round %d phase %s",
						id, round, phase)
				}

				// Fill missing-but-derivable response fields so
				// downstream consumers always see real data.
				if resp.AgentID == "" {
					resp.AgentID = id
				}
				if resp.Phase == "" {
					resp.Phase = phase
				}
				if resp.Timestamp.IsZero() {
					resp.Timestamp = time.Now()
				}
				if resp.Latency == 0 {
					resp.Latency = invokeLatency
				}

				phaseResult.Responses = append(phaseResult.Responses, resp)
				roundResponses = append(roundResponses, resp)
				allResponses = append(allResponses, resp)
				result.Metrics.TotalResponses++
			}

			phaseResult.Duration = time.Since(phaseStart)
			result.Phases = append(result.Phases, phaseResult)
		}

		// (5) Round-level consensus.
		lastConsensus = computeConsensus(roundResponses)
		roundsCompleted = round

		// (6) Early-exit gate.
		if p.Config.EnableEarlyExit &&
			lastConsensus != nil &&
			lastConsensus.Confidence >= p.Config.MinConsensusScore {
			earlyExit = true
			earlyExitReason = fmt.Sprintf(
				"consensus confidence %.4f >= threshold %.4f at round %d",
				lastConsensus.Confidence, p.Config.MinConsensusScore, round)
			break RoundsLoop
		}
	}

	result.RoundsCompleted = roundsCompleted
	result.Duration = time.Since(debateStart)
	result.EarlyExit = earlyExit
	result.EarlyExitReason = earlyExitReason
	result.FinalConsensus = lastConsensus
	if lastConsensus != nil {
		result.Content = lastConsensus.Choice
	}
	result.Success = roundsCompleted > 0 && len(result.Phases) > 0

	return result, nil
}

// buildPhasePrompt composes the per-agent prompt for a given phase.
// Real but minimal: emits topic, context, phase identifier, round
// number, and a compact transcript of the prior responses so each
// phase incorporates earlier work. No LLM dependency.
func buildPhasePrompt(topic, debateCtx string, phase topology.DebatePhase,
	round int, prior []*PhaseResponse) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Debate topic: %s\n", topic)
	if debateCtx != "" {
		fmt.Fprintf(&sb, "Context: %s\n", debateCtx)
	}
	fmt.Fprintf(&sb, "Round: %d\n", round)
	fmt.Fprintf(&sb, "Phase: %s\n", string(phase))
	if len(prior) > 0 {
		sb.WriteString("Prior responses:\n")
		// Cap to last 16 to keep prompt bounded.
		start := 0
		if len(prior) > 16 {
			start = len(prior) - 16
		}
		for _, r := range prior[start:] {
			fmt.Fprintf(&sb, "- [%s/%s] %s\n",
				r.AgentID, string(r.Phase), r.Content)
		}
	}
	return sb.String()
}

// computeConsensus produces a deterministic ConsensusResult from a
// slice of round-level PhaseResponses. The heuristic: tally the
// trimmed Content of every response, declare the most frequent
// content the Choice, compute Confidence as
// freq(choice) / total_responses, and list every contributor that
// produced the chosen content. Empty input returns nil.
func computeConsensus(responses []*PhaseResponse) *ConsensusResult {
	if len(responses) == 0 {
		return nil
	}
	counts := make(map[string]int, len(responses))
	contribs := make(map[string][]string, len(responses))
	for _, r := range responses {
		if r == nil {
			continue
		}
		key := strings.TrimSpace(r.Content)
		if key == "" {
			continue
		}
		counts[key]++
		contribs[key] = append(contribs[key], r.AgentID)
	}
	if len(counts) == 0 {
		return nil
	}

	// Sort keys for determinism, then pick highest-count with
	// lexicographically smallest content as tie-breaker.
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	bestKey := keys[0]
	bestCount := counts[bestKey]
	for _, k := range keys[1:] {
		if counts[k] > bestCount {
			bestKey = k
			bestCount = counts[k]
		}
	}

	total := 0
	for _, c := range counts {
		total += c
	}
	confidence := 0.0
	if total > 0 {
		confidence = float64(bestCount) / float64(total)
	}

	return &ConsensusResult{
		Choice:       bestKey,
		Confidence:   confidence,
		Contributors: append([]string(nil), contribs[bestKey]...),
	}
}

// copyMetadata returns a defensive shallow copy of a metadata map so
// the debate runtime cannot accidentally mutate caller-owned state.
func copyMetadata(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return nil
	}
	out := make(map[string]interface{}, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// ExecuteRequest dispatches a wire-protocol request through the
// Protocol envelope. The request's Method is looked up in the
// registered RequestHandler map; the handler is invoked with the
// supplied ctx + req.Params; the result (or error) is wrapped in a
// *Response and returned.
//
// Returns ErrInvalidRequest when req is nil or has an empty Method;
// ErrUnknownMethod when no handler is registered for the method;
// ctx.Err() when the supplied context is cancelled before dispatch.
func (p *Protocol) ExecuteRequest(ctx context.Context, req *Request) (*Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, fmt.Errorf("%w: nil request", ErrInvalidRequest)
	}
	if strings.TrimSpace(req.Method) == "" {
		return nil, fmt.Errorf("%w: empty Method", ErrInvalidRequest)
	}
	h, ok := p.handler(req.Method)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownMethod, req.Method)
	}
	result, err := h(ctx, req.Params)
	if err != nil {
		// Surface the handler error both as wire-error and a Go-level
		// error so callers can switch on whichever they need.
		return &Response{ID: req.ID, Error: err}, err
	}
	return &Response{ID: req.ID, Result: result}, nil
}

// federatedAllowedMethods is the closed allow-list of method names
// HandleFederatedRequest will accept. Anything else returns
// ErrUnsupportedFederatedMethod. Keeping it narrow is a security
// posture choice: federated calls execute on a remote node, so the
// surface area exposed to peers stays explicit.
var federatedAllowedMethods = map[string]struct{}{
	"federated.participate": {},
}

// HandleFederatedRequest dispatches a federated protocol request
// arriving from a peer DebateOrchestrator node. The request's Method
// MUST be in the federated allow-list (currently:
// "federated.participate"); otherwise ErrUnsupportedFederatedMethod
// is returned without touching the handler map.
//
// On allowed methods the request is routed through the same handler
// map as ExecuteRequest — register a RequestHandler for
// "federated.participate" to wire in the per-node participation
// logic. The handler error (if any) is surfaced both in the returned
// *Response.Error and as the Go-level error.
func (p *Protocol) HandleFederatedRequest(ctx context.Context, req *Request) (*Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, fmt.Errorf("%w: nil request", ErrInvalidRequest)
	}
	if strings.TrimSpace(req.Method) == "" {
		return nil, fmt.Errorf("%w: empty Method", ErrInvalidRequest)
	}
	if _, allowed := federatedAllowedMethods[req.Method]; !allowed {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedFederatedMethod, req.Method)
	}
	// Route through the regular handler map.
	h, ok := p.handler(req.Method)
	if !ok {
		return nil, fmt.Errorf("%w: %q (federated allow-listed but no handler registered)",
			ErrUnknownMethod, req.Method)
	}
	result, err := h(ctx, req.Params)
	if err != nil {
		return &Response{ID: req.ID, Error: err}, err
	}
	return &Response{ID: req.ID, Result: result}, nil
}

// =============================================================================
// Construction helpers (transport / standard / config)
// =============================================================================

// NewStandard constructs a Standard with default identity.
func NewStandard() *Standard {
	return &Standard{Name: "standard"}
}

// Transport is the common interface implemented by every wire
// transport (file-backed, pipe-backed, future TCP/etc.). All transports
// frame messages as newline-delimited JSON: Send appends one
// JSON-encoded *Request + "\n" to the outbound stream; Recv reads one
// JSON-encoded *Response from the inbound stream.
type Transport interface {
	// Send writes the supplied request to the outbound stream. Returns
	// ErrTransportClosed after Close; honours ctx via the underlying
	// io.Writer or polling loop.
	Send(ctx context.Context, req *Request) error
	// Recv reads the next response from the inbound stream. Blocks
	// until data is available, the inbound stream is closed (returns
	// io.EOF), the ctx is cancelled (returns ctx.Err()), or the
	// transport is closed (returns ErrTransportClosed).
	Recv(ctx context.Context) (*Response, error)
	// Close releases the underlying file/pipe handles. Idempotent.
	Close() error
}

// FileTransport is a real file-backed Transport. Inbound responses
// are read from inPath using newline-delimited JSON framing; outbound
// requests are appended to outPath using the same framing. When the
// caller-supplied FileConfig.Path is set (legacy single-file mode)
// both directions use the same file — useful for offline replay.
type FileTransport struct {
	inPath  string
	outPath string

	mu         sync.Mutex
	closed     bool
	outFile    *os.File
	inFile     *os.File
	inReader   *bufio.Reader
	pollPeriod time.Duration
}

// NewFileTransport constructs a real file-backed Transport bound to
// the supplied FileConfig. If InPath / OutPath are set independently
// the transport reads from InPath and writes to OutPath. If only
// Path is set both directions resolve to that file (offline-replay
// mode). The outbound file is opened with O_CREATE|O_APPEND|O_WRONLY
// so multiple senders never truncate prior framed messages.
func NewFileTransport(cfg FileConfig) (Transport, error) {
	in := cfg.InPath
	out := cfg.OutPath
	if in == "" {
		in = cfg.Path
	}
	if out == "" {
		out = cfg.Path
	}
	if in == "" || out == "" {
		return nil, fmt.Errorf(
			"%w: FileConfig requires InPath+OutPath or Path",
			ErrInvalidRequest)
	}
	// Open outbound file for append + create.
	outFile, err := os.OpenFile(out,
		os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf(
			"debate/protocol: open outbound %q: %w", out, err)
	}
	// Open inbound file. Create if missing so Recv has a target to
	// poll while the other side is producing.
	inFile, err := os.OpenFile(in,
		os.O_CREATE|os.O_RDONLY, 0o600)
	if err != nil {
		_ = outFile.Close()
		return nil, fmt.Errorf(
			"debate/protocol: open inbound %q: %w", in, err)
	}
	return &FileTransport{
		inPath:     in,
		outPath:    out,
		outFile:    outFile,
		inFile:     inFile,
		inReader:   bufio.NewReader(inFile),
		pollPeriod: 5 * time.Millisecond,
	}, nil
}

// Send appends the JSON-encoded request + "\n" to the outbound file.
// Returns ErrTransportClosed after Close; ctx.Err() if the supplied
// context is already cancelled.
func (t *FileTransport) Send(ctx context.Context, req *Request) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return ErrTransportClosed
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("debate/protocol: marshal request: %w", err)
	}
	payload = append(payload, '\n')
	if _, err := t.outFile.Write(payload); err != nil {
		return fmt.Errorf(
			"debate/protocol: write to %q: %w", t.outPath, err)
	}
	return nil
}

// Recv reads the next JSON-encoded response from the inbound file.
// Blocks (polling at pollPeriod) until a complete framed message is
// available, the ctx is cancelled, or the transport is closed.
func (t *FileTransport) Recv(ctx context.Context) (*Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for {
		t.mu.Lock()
		if t.closed {
			t.mu.Unlock()
			return nil, ErrTransportClosed
		}
		reader := t.inReader
		t.mu.Unlock()

		line, err := reader.ReadBytes('\n')
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf(
				"debate/protocol: read from %q: %w", t.inPath, err)
		}
		line = bytes.TrimRight(line, "\n")
		if len(line) > 0 {
			var resp Response
			if jerr := json.Unmarshal(line, &resp); jerr != nil {
				return nil, fmt.Errorf(
					"debate/protocol: unmarshal response: %w", jerr)
			}
			return &resp, nil
		}
		// No data yet — poll, but honour ctx.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(t.pollPeriod):
			// loop and retry
		}
	}
}

// Close releases the file handles. Idempotent.
func (t *FileTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	var firstErr error
	if t.outFile != nil {
		if err := t.outFile.Close(); err != nil {
			firstErr = err
		}
	}
	if t.inFile != nil {
		if err := t.inFile.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// PipeTransport is a real os.Pipe-backed Transport — useful for
// in-process IPC and unit testing of the framing layer.
type PipeTransport struct {
	reader *os.File
	writer *os.File

	mu       sync.Mutex
	closed   bool
	inReader *bufio.Reader
}

// NewPipeTransport constructs a real Transport backed by an os.Pipe.
// Send writes to the pipe's write end; Recv reads from the read end.
// Both ends live on the same struct so Send + Recv against the same
// PipeTransport instance form a loopback suitable for tests.
func NewPipeTransport() (Transport, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("debate/protocol: os.Pipe: %w", err)
	}
	return &PipeTransport{
		reader:   r,
		writer:   w,
		inReader: bufio.NewReader(r),
	}, nil
}

// Send writes the JSON-encoded request + "\n" to the pipe.
func (t *PipeTransport) Send(ctx context.Context, req *Request) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return ErrTransportClosed
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("debate/protocol: marshal request: %w", err)
	}
	payload = append(payload, '\n')
	if _, err := t.writer.Write(payload); err != nil {
		return fmt.Errorf("debate/protocol: write pipe: %w", err)
	}
	return nil
}

// Recv reads the next JSON-encoded response from the pipe. The read
// runs on a goroutine so ctx cancellation is observed even when the
// underlying pipe blocks; on ctx cancellation the goroutine is left
// to drain naturally when data arrives or the pipe is closed.
func (t *PipeTransport) Recv(ctx context.Context) (*Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil, ErrTransportClosed
	}
	reader := t.inReader
	t.mu.Unlock()

	type readResult struct {
		line []byte
		err  error
	}
	done := make(chan readResult, 1)
	go func() {
		line, err := reader.ReadBytes('\n')
		done <- readResult{line: line, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-done:
		if r.err != nil && r.err != io.EOF {
			return nil, fmt.Errorf("debate/protocol: read pipe: %w", r.err)
		}
		line := bytes.TrimRight(r.line, "\n")
		if len(line) == 0 {
			if r.err == io.EOF {
				return nil, io.EOF
			}
			return nil, fmt.Errorf("debate/protocol: empty frame")
		}
		var resp Response
		if jerr := json.Unmarshal(line, &resp); jerr != nil {
			return nil, fmt.Errorf(
				"debate/protocol: unmarshal response: %w", jerr)
		}
		return &resp, nil
	}
}

// Close releases the pipe ends. Idempotent.
func (t *PipeTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	var firstErr error
	if t.writer != nil {
		if err := t.writer.Close(); err != nil {
			firstErr = err
		}
	}
	if t.reader != nil {
		if err := t.reader.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// DefaultDebateConfig returns the canonical default debate config.
// All knobs the debate runner consults have sensible defaults so
// callers can construct a working Config via a single function call
// plus targeted overrides.
func DefaultDebateConfig() Config {
	return Config{
		Name:              "default",
		Version:           "1.0",
		Timeout:           30 * time.Second,
		MaxRounds:         3,
		EnableEarlyExit:   true,
		MinConsensusScore: 0.7,
		TopologyType:      topology.TopologyGraphMesh,
	}
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

// Initialize performs the protocol initialise self-handshake. The
// Standard struct represents this node's protocol identity; the
// returned InitializeResult advertises the negotiated protocol
// version and the server identification string (package version +
// Standard.Name when set).
func (s *Standard) Initialize(ctx context.Context) (*InitializeResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name := "DebateOrchestrator"
	if s != nil && s.Name != "" {
		name = "DebateOrchestrator/" + s.Name
	}
	return &InitializeResult{
		ProtocolVersion: ProtocolVersion,
		ServerInfo:      name + " v" + ProtocolVersion,
	}, nil
}

// Connect performs a real TCP dial to the configured Endpoint. The
// resulting net.Conn is retained on the client for subsequent
// reads/writes. Returns ErrNoEndpoint when Endpoint is empty; wraps
// dial errors with the endpoint string for diagnostics.
//
// The ctx deadline (if any) bounds the dial. If c.DialTimeout is set
// it caps the dial duration; otherwise the dial honours only the
// ctx deadline.
func (c *HelixAgentClient) Connect(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c == nil {
		return fmt.Errorf("%w: nil receiver", ErrNoEndpoint)
	}
	if strings.TrimSpace(c.Endpoint) == "" {
		return ErrNoEndpoint
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrTransportClosed
	}
	// If already connected, return without redialing.
	if c.conn != nil {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	var dialer net.Dialer
	if c.DialTimeout > 0 {
		dialer.Timeout = c.DialTimeout
	}
	conn, err := dialer.DialContext(ctx, "tcp", c.Endpoint)
	if err != nil {
		return fmt.Errorf(
			"debate/protocol: dial %q: %w", c.Endpoint, err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// Race-defence: if Close ran between unlock and lock, abandon
	// the freshly-dialled conn.
	if c.closed {
		_ = conn.Close()
		return ErrTransportClosed
	}
	c.conn = conn
	return nil
}

// Close shuts down the active connection (if any) and marks the
// client as closed so subsequent Connect attempts fail fast.
// Idempotent.
func (c *HelixAgentClient) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		return err
	}
	return nil
}

// Conn returns the underlying net.Conn for callers that need to
// write/read raw bytes. Returns nil before Connect succeeds or after
// Close.
func (c *HelixAgentClient) Conn() net.Conn {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn
}
