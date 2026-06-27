package otel

import "go.opentelemetry.io/otel/attribute"

// Portfolio-specific attribute keys. These are not part of any
// OpenTelemetry semantic-convention namespace — they encode the
// vocabulary of identity-platform-go ADR-0018 (audit envelope),
// ADR-0015 (agent principal type), and ADR-0019 (resource taxonomy)
// onto span attributes so traces and audit events can be joined.
//
// Stable strings, not Go constants of the OTel attribute.Key type, so
// callers can reference them in `OTEL_RESOURCE_ATTRIBUTES`, in
// dashboard queries, and in tests without importing this package.
const (
	// AttrActorType identifies the principal kind on a span
	// ("user" | "service" | "agent"). Matches the Event.ActorType field
	// in go-platform/audit.
	AttrActorType = "actor.type"

	// AttrActorID is the stable identifier of the principal that
	// triggered the span. For agents this is the agent_id claim from
	// the OAuth token; for services it is the client_id; for users it
	// is the OIDC sub.
	AttrActorID = "actor.id"

	// AttrSubjectID is the resource-owner subject when distinct from
	// the actor (delegation chains, RFC 8693 token exchange).
	AttrSubjectID = "actor.subject_id"

	// AttrAgentID is set on spans for which actor.type=agent. Mirrors
	// the agent_id claim from ADR-0015.
	AttrAgentID = "actor.agent_id"

	// AttrClientID is the OAuth client_id used to obtain the token.
	AttrClientID = "actor.client_id"

	// AttrResource is the human-readable resource identifier matching
	// the audit Resource field (e.g. "tool:get_standings",
	// "token:access").
	AttrResource = "resource"

	// AttrResourceKind classifies the surface
	// ("tool" | "endpoint" | "server" | "api" | "token" |
	// "application" | "feature"). Matches the ADR-0019 enum.
	AttrResourceKind = "resource.kind"

	// AttrResourceID is the leaf identifier (e.g. "get_standings").
	AttrResourceID = "resource.id"

	// AttrResourceParent is the containing surface (e.g.
	// "jk-mcp-nwsl").
	AttrResourceParent = "resource.parent"

	// AttrResourcePath is the hierarchical path (e.g.
	// "jk-mcp-nwsl/tool/get_standings"). This is the field Lago
	// billable-metric filters most commonly use; recording it on the
	// span lets engineers join traces to invoices.
	AttrResourcePath = "resource.path"

	// AttrAction is the verb of the operation ("invoke", "issue",
	// "exchange", "introspect", "register", "revoke").
	AttrAction = "action"

	// AttrDecision is the authorization outcome ("allow" | "deny").
	AttrDecision = "decision"

	// AttrPolicyName names the policy rule that produced the decision
	// (e.g. "policy:read-only-agent"). Set when the decision comes from
	// the authorization-policy-service.
	AttrPolicyName = "decision.policy_name"

	// AttrToolName is the name of the MCP tool the agent invoked. Set
	// when the span records a tool call.
	AttrToolName = "tool.name"

	// AttrAuditEventID links the span to the audit event emitted for
	// the same operation. Set when an audit emit happens during the
	// span; the value is the ULID Event.EventID.
	AttrAuditEventID = "audit.event_id"

	// AttrAuditEventType is the audit Event.EventType for cross-stream
	// filtering.
	AttrAuditEventType = "audit.event_type"
)

// Actor returns a slice of attribute.KeyValue describing the principal
// for the given span. Pass empty strings for fields that do not apply
// and the helper omits them — useful for code paths where some claims
// are absent (legacy clients pre-ADR-0015, service-to-service calls
// without a subject).
func Actor(actorType, actorID, subjectID, agentID, clientID string) []attribute.KeyValue {
	out := make([]attribute.KeyValue, 0, 5)
	if actorType != "" {
		out = append(out, attribute.String(AttrActorType, actorType))
	}
	if actorID != "" {
		out = append(out, attribute.String(AttrActorID, actorID))
	}
	if subjectID != "" {
		out = append(out, attribute.String(AttrSubjectID, subjectID))
	}
	if agentID != "" {
		out = append(out, attribute.String(AttrAgentID, agentID))
	}
	if clientID != "" {
		out = append(out, attribute.String(AttrClientID, clientID))
	}
	return out
}

// Resource returns the resource-taxonomy attribute set per ADR-0019.
// Pass empty strings to omit fields. Same shape as the audit envelope
// so a downstream consumer can correlate spans and audit rows by
// resource_path alone.
func Resource(resource, kind, id, parent, path string) []attribute.KeyValue {
	out := make([]attribute.KeyValue, 0, 5)
	if resource != "" {
		out = append(out, attribute.String(AttrResource, resource))
	}
	if kind != "" {
		out = append(out, attribute.String(AttrResourceKind, kind))
	}
	if id != "" {
		out = append(out, attribute.String(AttrResourceID, id))
	}
	if parent != "" {
		out = append(out, attribute.String(AttrResourceParent, parent))
	}
	if path != "" {
		out = append(out, attribute.String(AttrResourcePath, path))
	}
	return out
}

// AuditLink links the current span to an emitted audit event by ULID
// and event_type. Call after [audit.Emitter.Emit] so the trace carries
// a pointer to the audit row for the same operation.
func AuditLink(eventID, eventType string) []attribute.KeyValue {
	out := make([]attribute.KeyValue, 0, 2)
	if eventID != "" {
		out = append(out, attribute.String(AttrAuditEventID, eventID))
	}
	if eventType != "" {
		out = append(out, attribute.String(AttrAuditEventType, eventType))
	}
	return out
}
