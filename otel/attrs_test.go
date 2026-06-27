package otel_test

import (
	"testing"

	"go.opentelemetry.io/otel/attribute"

	platformotel "github.com/jedi-knights/go-platform/otel"
)

func TestActor_OmitsEmptyFields(t *testing.T) {
	attrs := platformotel.Actor("agent", "agent-claude", "user-omar", "agent-claude", "")
	want := map[string]string{
		platformotel.AttrActorType: "agent",
		platformotel.AttrActorID:   "agent-claude",
		platformotel.AttrSubjectID: "user-omar",
		platformotel.AttrAgentID:   "agent-claude",
	}
	assertAttrs(t, attrs, want)
}

func TestActor_AllEmpty(t *testing.T) {
	attrs := platformotel.Actor("", "", "", "", "")
	if len(attrs) != 0 {
		t.Errorf("expected empty slice, got %v", attrs)
	}
}

func TestResource_ProducesTaxonomy(t *testing.T) {
	attrs := platformotel.Resource("tool:get_standings", "tool", "get_standings",
		"jk-mcp-nwsl", "jk-mcp-nwsl/tool/get_standings")
	want := map[string]string{
		platformotel.AttrResource:       "tool:get_standings",
		platformotel.AttrResourceKind:   "tool",
		platformotel.AttrResourceID:     "get_standings",
		platformotel.AttrResourceParent: "jk-mcp-nwsl",
		platformotel.AttrResourcePath:   "jk-mcp-nwsl/tool/get_standings",
	}
	assertAttrs(t, attrs, want)
}

func TestAuditLink_OmitsEmpty(t *testing.T) {
	attrs := platformotel.AuditLink("01JABC123", "")
	if len(attrs) != 1 {
		t.Fatalf("expected 1 attribute, got %d", len(attrs))
	}
	if string(attrs[0].Key) != platformotel.AttrAuditEventID {
		t.Errorf("key = %q, want %q", attrs[0].Key, platformotel.AttrAuditEventID)
	}
}

// assertAttrs validates that attrs contains exactly the expected key/value pairs.
func assertAttrs(t *testing.T, attrs []attribute.KeyValue, want map[string]string) {
	t.Helper()
	if len(attrs) != len(want) {
		t.Fatalf("expected %d attributes, got %d: %v", len(want), len(attrs), attrs)
	}
	got := make(map[string]string, len(attrs))
	for _, a := range attrs {
		got[string(a.Key)] = a.Value.AsString()
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("attr[%q] = %q, want %q", k, got[k], v)
		}
	}
}
