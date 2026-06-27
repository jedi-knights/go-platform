package audit_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jedi-knights/go-platform/audit"
)

func validEvent() audit.Event {
	return audit.Event{
		SchemaVersion: audit.SchemaVersion,
		EventID:       "01JXYZTEST0000000000000000",
		EventType:     "tool_invoked",
		Timestamp:     time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC),
		Service:       "jk-mcp-nwsl",
		ActorType:     audit.ActorTypeAgent,
		ActorID:       "agent-claude-test",
		Resource:      "tool:get_standings",
		Action:        "invoke",
		Decision:      audit.DecisionAllow,
	}
}

func TestEventValidate_Valid(t *testing.T) {
	e := validEvent()
	if err := e.Validate(); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestEventValidate_RequiredFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(e *audit.Event)
		want   string
	}{
		{"nil receiver", nil, "nil event"},
		{"missing event_id", func(e *audit.Event) { e.EventID = "" }, "event_id"},
		{"missing event_type", func(e *audit.Event) { e.EventType = "" }, "event_type"},
		{"missing timestamp", func(e *audit.Event) { e.Timestamp = time.Time{} }, "timestamp"},
		{"missing service", func(e *audit.Event) { e.Service = "" }, "service"},
		{"missing actor_type", func(e *audit.Event) { e.ActorType = "" }, "actor_type"},
		{"missing actor_id", func(e *audit.Event) { e.ActorID = "" }, "actor_id"},
		{"missing resource", func(e *audit.Event) { e.Resource = "" }, "resource"},
		{"missing action", func(e *audit.Event) { e.Action = "" }, "action"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.mutate == nil {
				var e *audit.Event
				err = e.Validate()
			} else {
				e := validEvent()
				tt.mutate(&e)
				err = e.Validate()
			}
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, audit.ErrInvalidEvent) {
				t.Errorf("expected error to wrap ErrInvalidEvent, got %v", err)
			}
			if !contains(err.Error(), tt.want) {
				t.Errorf("expected error %q to mention %q", err, tt.want)
			}
		})
	}
}

func TestEventValidate_BadEnumValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(e *audit.Event)
	}{
		{"unknown actor_type", func(e *audit.Event) { e.ActorType = "alien" }},
		{"unknown decision", func(e *audit.Event) { e.Decision = "maybe" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := validEvent()
			tt.mutate(&e)
			if err := e.Validate(); err == nil {
				t.Fatal("expected validation error, got nil")
			}
		})
	}
}

func TestEventValidate_DecisionOptional(t *testing.T) {
	// Empty decision is permitted (Allow is implicit), but a non-empty
	// value must be one of the recognised constants.
	e := validEvent()
	e.Decision = ""
	if err := e.Validate(); err != nil {
		t.Fatalf("empty decision should be valid, got %v", err)
	}
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
