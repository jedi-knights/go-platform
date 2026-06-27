package audit_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jedi-knights/go-platform/audit"
)

type captureSink struct {
	events []audit.Event
	err    error
}

func (c *captureSink) Sink(_ context.Context, e audit.Event) error {
	c.events = append(c.events, e)
	return c.err
}

func TestNew_NilSinkPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic")
		}
	}()
	_ = audit.New(nil)
}

func TestEmit_DefaultsFilled(t *testing.T) {
	sink := &captureSink{}
	e := audit.New(sink)

	in := audit.Event{
		EventType: "tool_invoked",
		Service:   "jk-mcp-nwsl",
		ActorType: audit.ActorTypeAgent,
		ActorID:   "agent-claude",
		Resource:  "tool:get_standings",
		Action:    "invoke",
		Decision:  audit.DecisionAllow,
	}
	if err := e.Emit(context.Background(), in); err != nil {
		t.Fatalf("emit returned error: %v", err)
	}
	if len(sink.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(sink.events))
	}
	got := sink.events[0]
	if got.SchemaVersion != audit.SchemaVersion {
		t.Errorf("expected SchemaVersion filled, got %q", got.SchemaVersion)
	}
	if got.EventID == "" {
		t.Errorf("expected EventID filled")
	}
	if got.Timestamp.IsZero() {
		t.Errorf("expected Timestamp filled")
	}
}

func TestEmit_RespectsCallerSuppliedFields(t *testing.T) {
	sink := &captureSink{}
	e := audit.New(sink)

	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	in := audit.Event{
		SchemaVersion: "1.0",
		EventID:       "01JABC0000000000000000000A",
		EventType:     "tool_invoked",
		Timestamp:     ts,
		Service:       "jk-mcp-nwsl",
		ActorType:     audit.ActorTypeAgent,
		ActorID:       "agent-claude",
		Resource:      "tool:get_standings",
		Action:        "invoke",
	}
	if err := e.Emit(context.Background(), in); err != nil {
		t.Fatalf("emit returned error: %v", err)
	}
	got := sink.events[0]
	if got.EventID != in.EventID {
		t.Errorf("expected EventID preserved, got %q", got.EventID)
	}
	if !got.Timestamp.Equal(ts) {
		t.Errorf("expected Timestamp preserved, got %v", got.Timestamp)
	}
}

func TestEmit_ValidationFailure(t *testing.T) {
	sink := &captureSink{}
	e := audit.New(sink)

	// Missing actor_id
	in := audit.Event{
		EventType: "tool_invoked",
		Service:   "jk-mcp-nwsl",
		ActorType: audit.ActorTypeAgent,
		Resource:  "tool:get_standings",
		Action:    "invoke",
	}
	err := e.Emit(context.Background(), in)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !errors.Is(err, audit.ErrInvalidEvent) {
		t.Errorf("expected ErrInvalidEvent, got %v", err)
	}
	if len(sink.events) != 0 {
		t.Errorf("sink should not have received the invalid event")
	}
}

func TestEmit_SinkErrorPropagates(t *testing.T) {
	sinkErr := errors.New("transport failure")
	sink := &captureSink{err: sinkErr}
	e := audit.New(sink)

	in := audit.Event{
		EventType: "tool_invoked",
		Service:   "jk-mcp-nwsl",
		ActorType: audit.ActorTypeAgent,
		ActorID:   "agent-claude",
		Resource:  "tool:get_standings",
		Action:    "invoke",
	}
	err := e.Emit(context.Background(), in)
	if !errors.Is(err, sinkErr) {
		t.Errorf("expected sink error to propagate, got %v", err)
	}
}

func TestMultiSink_FansOut(t *testing.T) {
	a := &captureSink{}
	b := &captureSink{}
	multi := audit.NewMultiSink(a, b)
	e := audit.New(multi)

	in := audit.Event{
		EventType: "tool_invoked",
		Service:   "jk-mcp-nwsl",
		ActorType: audit.ActorTypeAgent,
		ActorID:   "agent-claude",
		Resource:  "tool:get_standings",
		Action:    "invoke",
	}
	if err := e.Emit(context.Background(), in); err != nil {
		t.Fatalf("emit returned error: %v", err)
	}
	if len(a.events) != 1 || len(b.events) != 1 {
		t.Errorf("expected both sinks to receive the event, got a=%d b=%d", len(a.events), len(b.events))
	}
}

func TestMultiSink_JoinsErrors(t *testing.T) {
	errA := errors.New("a failed")
	errB := errors.New("b failed")
	a := &captureSink{err: errA}
	b := &captureSink{err: errB}
	multi := audit.NewMultiSink(a, b)
	e := audit.New(multi)

	in := audit.Event{
		EventType: "tool_invoked",
		Service:   "jk-mcp-nwsl",
		ActorType: audit.ActorTypeAgent,
		ActorID:   "agent-claude",
		Resource:  "tool:get_standings",
		Action:    "invoke",
	}
	err := e.Emit(context.Background(), in)
	if !errors.Is(err, errA) || !errors.Is(err, errB) {
		t.Errorf("expected joined error to contain both, got %v", err)
	}
	// Both sinks should still have received the event despite the errors.
	if len(a.events) != 1 || len(b.events) != 1 {
		t.Errorf("expected fan-out even on error, got a=%d b=%d", len(a.events), len(b.events))
	}
}

func TestMultiSink_NilEntryPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic")
		}
	}()
	_ = audit.NewMultiSink(&captureSink{}, nil)
}

func TestNoopSink_DiscardsAndSucceeds(t *testing.T) {
	e := audit.New(audit.NoopSink{})
	in := audit.Event{
		EventType: "tool_invoked",
		Service:   "jk-mcp-nwsl",
		ActorType: audit.ActorTypeAgent,
		ActorID:   "agent-claude",
		Resource:  "tool:get_standings",
		Action:    "invoke",
	}
	if err := e.Emit(context.Background(), in); err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}
