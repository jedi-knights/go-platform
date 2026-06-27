package audit

import (
	"errors"
	"fmt"
	"time"
)

// SchemaVersion is the current audit envelope schema version. ADR-0018
// establishes this as a stable contract; field additions are non-breaking,
// renames and removals require a bump.
const SchemaVersion = "1.0"

// ActorType identifies the kind of principal an event is attributed to.
// The value is copied from the access-token claim of the same name introduced
// in identity-platform-go ADR-0015.
type ActorType string

// Recognised actor types. The set is closed at the contract level —
// new actor types require an ADR amendment, not just a new string.
const (
	ActorTypeUser    ActorType = "user"
	ActorTypeService ActorType = "service"
	ActorTypeAgent   ActorType = "agent"
)

// ResourceKind classifies what surface was acted upon. It is an open enum
// at the contract level so new kinds can be added without a package release;
// the metering shim treats unknown values opaquely and Lago filters
// discriminate. The constants below are the portfolio's initial vocabulary.
type ResourceKind string

// Initial portfolio resource kinds. See identity-platform-go ADR-0019 for the
// SKU shapes each kind enables.
const (
	ResourceKindTool        ResourceKind = "tool"        // one MCP tool
	ResourceKindServer      ResourceKind = "server"      // one MCP server as a whole
	ResourceKindEndpoint    ResourceKind = "endpoint"    // one HTTP endpoint
	ResourceKindAPI         ResourceKind = "api"         // one whole API service
	ResourceKindToken       ResourceKind = "token"       // token-lifecycle action
	ResourceKindApplication ResourceKind = "application" // a web application
	ResourceKindFeature     ResourceKind = "feature"     // a feature within a web application
)

// Decision is the authorization outcome recorded on the event.
type Decision string

// Decision values. A missing or empty value is treated as Allow by consumers
// (an event was emitted because something happened), but emitters should set
// the field explicitly.
const (
	DecisionAllow Decision = "allow"
	DecisionDeny  Decision = "deny"
)

// Event is the structured agent-audit envelope defined by ADR-0018 and
// extended by ADR-0019. Every service in the portfolio that mints, exchanges,
// validates, introspects, or acts on tokens emits events conforming to this
// envelope.
//
// Fields are JSON-marshaled with lower_snake_case tags so consumers across
// languages (Go services, Python MCP servers, the metering shim, Lago) see
// identical wire shapes.
//
// The zero value is not a usable event; call [Validate] before passing to a
// sink, or use a constructor that populates EventID and Timestamp.
type Event struct {
	// SchemaVersion is the contract version of this envelope. Defaults to
	// the package [SchemaVersion] constant when emitted via [Emitter].
	SchemaVersion string `json:"schema_version"`

	// EventID is a ULID — globally unique and lexicographically time-ordered.
	// The metering shim maps this 1:1 to Lago's transaction_id, so replays
	// and reconciliation runs are idempotent.
	EventID string `json:"event_id"`

	// EventType is a short, registered identifier for the kind of action
	// recorded. See ADR-0018 for the initial registry (agent_authenticated,
	// agent_registered, token_issued, token_exchanged, token_introspected,
	// token_revoked, policy_evaluated, tool_invoked, delegation_denied,
	// consent_granted, consent_denied). New types are added by ADR amendment.
	EventType string `json:"event_type"`

	// Timestamp is the RFC 3339 instant the event was created, with at
	// least millisecond precision.
	Timestamp time.Time `json:"timestamp"`

	// Service is the name of the emitting service (e.g., auth-server,
	// jk-mcp-nwsl). Distinguishes events across the portfolio.
	Service string `json:"service"`

	// TraceID is the OpenTelemetry trace identifier when present. Links the
	// event to spans recorded by the planned go-platform/otel package.
	TraceID string `json:"trace_id,omitempty"`

	// CorrelationID is a caller-supplied request identifier. Optional;
	// used to stitch a single end-user action across multiple services.
	CorrelationID string `json:"correlation_id,omitempty"`

	// ActorType identifies the principal kind (ADR-0015). Required.
	ActorType ActorType `json:"actor_type"`

	// ActorID is the stable identifier of the principal. For an agent this
	// is the agent_id claim; for a service it is the client_id; for a user
	// it is the sub claim.
	ActorID string `json:"actor_id"`

	// SubjectID is the resource-owner subject when distinct from the actor
	// (delegation chains). Empty when actor and subject are the same.
	SubjectID string `json:"subject_id,omitempty"`

	// ClientID is the OAuth client_id used to obtain the token, when
	// applicable. Useful for joining audit events against client-registry
	// records.
	ClientID string `json:"client_id,omitempty"`

	// Resource is a URI-like, human-readable resource identifier
	// (e.g., "tool:get_standings"). Required. The structured taxonomy
	// fields below let consumers (especially Lago filters) discriminate
	// without parsing this string.
	Resource string `json:"resource"`

	// ResourceKind classifies what kind of surface the resource is
	// (ADR-0019 extension). Required.
	ResourceKind ResourceKind `json:"resource_kind,omitempty"`

	// ResourceID is the leaf identifier of the resource (e.g.,
	// "get_standings"). Optional — defaults to Resource minus prefix.
	ResourceID string `json:"resource_id,omitempty"`

	// ResourceParent is the containing surface (e.g., "jk-mcp-nwsl" for a
	// tool, "auth-server" for an endpoint). Enables per-server and per-API
	// billable metrics in Lago via filter on this field.
	ResourceParent string `json:"resource_parent,omitempty"`

	// ResourcePath is the hierarchical path (e.g.,
	// "jk-mcp-nwsl/tool/get_standings"). Enables prefix-filter metrics so a
	// single Lago billable metric covers an entire server or feature set.
	ResourcePath string `json:"resource_path,omitempty"`

	// Action is a short verb (invoke, issue, exchange, introspect, register,
	// revoke, etc.). Required.
	Action string `json:"action"`

	// Decision is the authorization outcome. Required.
	Decision Decision `json:"decision"`

	// Reason is a stable identifier of the decision rule when present
	// (e.g., "policy:read-only-agent", "error:token_expired"). Free-form
	// text is discouraged — prefer stable identifiers so downstream
	// consumers can aggregate by reason.
	Reason string `json:"reason,omitempty"`

	// Attrs is a free-form, event-type-specific property bag. The metering
	// shim forwards everything in Attrs to Lago event properties verbatim,
	// so new SKU dimensions can be added without a code change in this
	// package or its consumers.
	Attrs map[string]any `json:"attrs,omitempty"`
}

// ErrInvalidEvent is returned when [Event.Validate] rejects an event.
// Use [errors.Is] to distinguish validation errors from sink errors.
var ErrInvalidEvent = errors.New("audit: invalid event")

// Validate reports the first contract violation on the event, or nil if
// the envelope is well-formed enough to emit. It checks required fields and
// well-known value sets; it does not verify that the actor or resource
// actually exists.
//
// Validate is intentionally cheap and pure so it can be called on the hot
// path before handing the event to a sink.
func (e *Event) Validate() error {
	if e == nil {
		return wrap("nil event")
	}
	if e.EventID == "" {
		return wrap("event_id is required")
	}
	if e.EventType == "" {
		return wrap("event_type is required")
	}
	if e.Timestamp.IsZero() {
		return wrap("timestamp is required")
	}
	if e.Service == "" {
		return wrap("service is required")
	}
	if e.ActorType == "" {
		return wrap("actor_type is required")
	}
	switch e.ActorType {
	case ActorTypeUser, ActorTypeService, ActorTypeAgent:
		// recognised
	default:
		return wrap("actor_type %q is not one of user, service, agent", e.ActorType)
	}
	if e.ActorID == "" {
		return wrap("actor_id is required")
	}
	if e.Resource == "" {
		return wrap("resource is required")
	}
	if e.Action == "" {
		return wrap("action is required")
	}
	if e.Decision != "" && e.Decision != DecisionAllow && e.Decision != DecisionDeny {
		return wrap("decision %q is not one of allow, deny", e.Decision)
	}
	return nil
}

// wrap formats a validation message under [ErrInvalidEvent].
func wrap(format string, args ...any) error {
	return &invalidEventError{msg: fmt.Sprintf(format, args...)}
}

type invalidEventError struct {
	msg string
}

func (e *invalidEventError) Error() string { return "audit: invalid event: " + e.msg }
func (e *invalidEventError) Unwrap() error { return ErrInvalidEvent }
