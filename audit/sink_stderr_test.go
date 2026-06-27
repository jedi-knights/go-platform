package audit_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/jedi-knights/go-platform/audit"
)

func TestJSONSink_WritesOneLinePerEvent(t *testing.T) {
	var buf bytes.Buffer
	sink := audit.NewJSONSink(&buf)
	e := audit.New(sink)

	if err := e.Emit(context.Background(), audit.Event{
		EventType: "tool_invoked",
		Service:   "jk-mcp-nwsl",
		ActorType: audit.ActorTypeAgent,
		ActorID:   "agent-claude",
		Resource:  "tool:get_standings",
		Action:    "invoke",
		Decision:  audit.DecisionAllow,
	}); err != nil {
		t.Fatalf("emit failed: %v", err)
	}
	if err := e.Emit(context.Background(), audit.Event{
		EventType: "token_issued",
		Service:   "auth-server",
		ActorType: audit.ActorTypeService,
		ActorID:   "client-abc",
		Resource:  "token:access",
		Action:    "issue",
		Decision:  audit.DecisionAllow,
	}); err != nil {
		t.Fatalf("emit failed: %v", err)
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 JSON lines, got %d (%q)", len(lines), buf.String())
	}
	for i, line := range lines {
		var got audit.Event
		if err := json.Unmarshal([]byte(line), &got); err != nil {
			t.Errorf("line %d not valid JSON: %v", i, err)
		}
	}
}

func TestJSONSink_ConcurrentEmitsDoNotInterleave(t *testing.T) {
	var buf bytes.Buffer
	sink := audit.NewJSONSink(&buf)
	e := audit.New(sink)

	const goroutines = 16
	const perGoroutine = 100
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				_ = e.Emit(context.Background(), audit.Event{
					EventType: "tool_invoked",
					Service:   "jk-mcp-nwsl",
					ActorType: audit.ActorTypeAgent,
					ActorID:   "agent-claude",
					Resource:  "tool:get_standings",
					Action:    "invoke",
				})
			}
		}()
	}
	wg.Wait()

	// Each line must independently parse as JSON — proves no interleaving.
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	want := goroutines * perGoroutine
	if len(lines) != want {
		t.Fatalf("expected %d lines, got %d", want, len(lines))
	}
	for i, line := range lines {
		var got audit.Event
		if err := json.Unmarshal([]byte(line), &got); err != nil {
			t.Errorf("line %d not valid JSON: %v\nline=%q", i, err, line)
		}
	}
}

func TestNewJSONSink_NilWriterPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic")
		}
	}()
	_ = audit.NewJSONSink(nil)
}

func TestStderrJSONSink_Constructable(t *testing.T) {
	// Smoke test: constructor returns a non-nil sink. We don't write to
	// real stderr in the test suite.
	s := audit.NewStderrJSONSink()
	if s == nil {
		t.Fatal("expected non-nil sink")
	}
}
