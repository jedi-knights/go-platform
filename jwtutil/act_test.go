package jwtutil_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jedi-knights/go-platform/jwtutil"
)

func TestActor_Depth_NilReceiverIsZero(t *testing.T) {
	var a *jwtutil.Actor
	if got := a.Depth(); got != 0 {
		t.Errorf("nil Actor depth = %d, want 0", got)
	}
}

func TestActor_Depth_SingleLevel(t *testing.T) {
	a := &jwtutil.Actor{Sub: "agent-planner"}
	if got := a.Depth(); got != 1 {
		t.Errorf("single-level Actor depth = %d, want 1", got)
	}
}

func TestActor_Depth_MultiLevelChain(t *testing.T) {
	a := &jwtutil.Actor{
		Sub: "agent-planner",
		Act: &jwtutil.Actor{
			Sub: "agent-claude",
			Act: &jwtutil.Actor{Sub: "user-omar"},
		},
	}
	if got := a.Depth(); got != 3 {
		t.Errorf("three-level chain depth = %d, want 3", got)
	}
}

func TestClaims_Act_OmittedWhenNil(t *testing.T) {
	c := jwtutil.NewClaims(jwtutil.ClaimsConfig{
		Subject:   "user-1",
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(time.Minute),
	})
	body, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(body), `"act"`) {
		t.Errorf("act claim must be omitted when nil; body=%s", body)
	}
}

func TestClaims_Act_PreservesChainOnRoundTrip(t *testing.T) {
	c := jwtutil.NewClaims(jwtutil.ClaimsConfig{
		Subject:   "user-omar",
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(time.Minute),
		Act: &jwtutil.Actor{
			Sub:       "agent-planner",
			ActorType: "agent",
			AgentID:   "agent-planner",
			Act: &jwtutil.Actor{
				Sub:       "agent-claude",
				ActorType: "agent",
				AgentID:   "agent-claude",
			},
		},
	})
	body, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded jwtutil.Claims
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Act == nil {
		t.Fatal("decoded act is nil")
	}
	if decoded.Act.Sub != "agent-planner" {
		t.Errorf("outer act sub = %q", decoded.Act.Sub)
	}
	if decoded.Act.Depth() != 2 {
		t.Errorf("decoded depth = %d, want 2", decoded.Act.Depth())
	}
	if decoded.Act.Act == nil || decoded.Act.Act.AgentID != "agent-claude" {
		t.Errorf("inner act lost; got %+v", decoded.Act.Act)
	}
}

func TestClaims_Act_EmitsRFC8693ShapedJSON(t *testing.T) {
	// RFC 8693 §4.1 example shape:
	// {"sub": "...", "act": {"sub": "..."}}
	c := jwtutil.NewClaims(jwtutil.ClaimsConfig{
		Subject:   "user-omar",
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(time.Minute),
		Act: &jwtutil.Actor{
			Sub: "agent-planner",
		},
	})
	body, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var generic map[string]any
	if err := json.Unmarshal(body, &generic); err != nil {
		t.Fatalf("unmarshal generic: %v", err)
	}
	act, ok := generic["act"].(map[string]any)
	if !ok {
		t.Fatalf("act claim is not an object; got %T", generic["act"])
	}
	if act["sub"] != "agent-planner" {
		t.Errorf("act.sub = %v, want agent-planner", act["sub"])
	}
}

func TestActor_OmitsEmptyFields(t *testing.T) {
	a := &jwtutil.Actor{Sub: "only-sub"}
	body, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, field := range []string{"actor_type", "agent_id", "client_id", "act"} {
		if strings.Contains(string(body), `"`+field+`"`) {
			t.Errorf("expected %q to be omitted; body=%s", field, body)
		}
	}
}
