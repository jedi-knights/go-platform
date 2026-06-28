package jwtutil_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jedi-knights/go-platform/jwtutil"
)

func TestClaims_AuthorizationDetails_OmittedWhenNil(t *testing.T) {
	c := jwtutil.NewClaims(jwtutil.ClaimsConfig{
		Subject:   "u",
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(time.Minute),
	})
	body, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(body), `"authorization_details"`) {
		t.Errorf("claim must be omitted when nil; body=%s", body)
	}
}

func TestClaims_AuthorizationDetails_PreservesRawJSON(t *testing.T) {
	detail := json.RawMessage(`{"type":"mcp_tool","tool":"get_standings","constraints":{"team_id":"1234"}}`)
	c := jwtutil.NewClaims(jwtutil.ClaimsConfig{
		Subject:              "u",
		IssuedAt:             time.Now(),
		ExpiresAt:            time.Now().Add(time.Minute),
		AuthorizationDetails: []json.RawMessage{detail},
	})
	body, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(body), `"authorization_details"`) {
		t.Errorf("authorization_details missing; body=%s", body)
	}
	// Round-trip to confirm the raw payload survives unchanged.
	var decoded jwtutil.Claims
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded.AuthorizationDetails) != 1 {
		t.Fatalf("decoded length = %d, want 1", len(decoded.AuthorizationDetails))
	}
	var got map[string]any
	if err := json.Unmarshal(decoded.AuthorizationDetails[0], &got); err != nil {
		t.Fatalf("unmarshal detail: %v", err)
	}
	if got["type"] != "mcp_tool" {
		t.Errorf("type = %v, want mcp_tool", got["type"])
	}
}

func TestClaims_AuthorizationDetails_DefensiveCopy(t *testing.T) {
	// Mutating the caller's slice after NewClaims must not affect the
	// returned Claims — Roles/Permissions already follow this rule and
	// AuthorizationDetails was added to match.
	source := []json.RawMessage{json.RawMessage(`{"type":"resource"}`)}
	c := jwtutil.NewClaims(jwtutil.ClaimsConfig{
		Subject:              "u",
		IssuedAt:             time.Now(),
		ExpiresAt:            time.Now().Add(time.Minute),
		AuthorizationDetails: source,
	})
	source[0] = json.RawMessage(`{"type":"hijacked"}`)
	got, _ := json.Marshal(c.AuthorizationDetails[0])
	if strings.Contains(string(got), "hijacked") {
		t.Errorf("Claims must defensively copy; got %s", got)
	}
}

func TestClaims_AuthorizationDetails_EmitsArrayShape(t *testing.T) {
	c := jwtutil.NewClaims(jwtutil.ClaimsConfig{
		Subject:   "u",
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(time.Minute),
		AuthorizationDetails: []json.RawMessage{
			json.RawMessage(`{"type":"mcp_tool","tool":"a"}`),
			json.RawMessage(`{"type":"mcp_tool","tool":"b"}`),
		},
	})
	body, _ := json.Marshal(c)
	var generic map[string]any
	_ = json.Unmarshal(body, &generic)
	arr, ok := generic["authorization_details"].([]any)
	if !ok {
		t.Fatalf("authorization_details must marshal as an array; got %T", generic["authorization_details"])
	}
	if len(arr) != 2 {
		t.Errorf("len = %d, want 2", len(arr))
	}
}
