// Package jwtutil provides JWT signing and parsing for the identity platform.
//
// Three token shapes are supported. HS256 (Sign / Parse / ParseWithAudience /
// ParseWithIssuer) is the legacy symmetric-key path. RS256 (SignRS256 /
// ParseRS256) is the asymmetric-key path used with JWKS-based verification —
// resource servers resolve the verification key via a KeySource keyed on the
// token's kid header, and verifiers reject HS256 tokens outright to defeat
// algorithm-confusion attacks (RFC 8725 §3.1). ID tokens (SignIDToken /
// ParseIDToken / IDClaims / AtHash) are the OIDC Core §2 path used to assert
// end-user identity to a relying party; they share RS256 + KeySource with
// access tokens but carry typ:"JWT" instead of typ:"at+jwt", and each parser
// rejects the other's typ to defeat token-type confusion.
//
// Access-token claims live in Claims; ID-token claims live in IDClaims. They
// are sibling types, not subtypes — keeping them separate stops access-token-
// only fields (Roles, Permissions) from leaking into ID tokens via JSON.
//
// All paths return sentinel errors (ErrTokenExpired, ErrTokenInvalid,
// ErrTokenMalformed); how those map to HTTP responses (e.g. RFC 7662 §2.2
// "active:false") is the caller's choice.
package jwtutil

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Sentinel errors returned by Parse. Callers use errors.Is to distinguish failure
// modes without importing the jwt library directly, keeping the jwt dependency
// contained to this package.
var (
	// ErrTokenExpired is returned when the token's expiry time has passed.
	ErrTokenExpired = errors.New("token expired")
	// ErrTokenInvalid is returned for signature failures, algorithm mismatches,
	// or any other validity failure not covered by a more specific sentinel.
	ErrTokenInvalid = errors.New("token invalid")
	// ErrTokenMalformed is returned when the raw string is not a well-formed JWT.
	ErrTokenMalformed = errors.New("token malformed")
)

// accessTokenJWTType is the JOSE "typ" header value required for OAuth 2.0
// access tokens by RFC 9068 §2.1. Sign sets this value; Parse and its variants
// reject tokens that do not carry it (defense against token-type confusion
// attacks, per RFC 8725 §3.11).
const accessTokenJWTType = "at+jwt"

// Claims is the canonical JWT claims type for identity-platform access tokens.
// Scope is a space-delimited string per RFC 9068 §2.2.3.1.
// Roles lists the RBAC roles assigned to the subject at token issuance.
// Permissions lists the resolved permissions (format: "resource:action") granted
// by the subject's roles at token issuance. Resource services use this claim for
// local authorization evaluation without an outbound policy service call.
// Both fields are omitempty — tokens issued without RBAC context omit them.
//
// ActorType and AgentID are introduced by identity-platform-go ADR-0015.
// ActorType identifies the principal kind ("user" | "service" | "agent") for
// audit and policy attribution; AgentID is the stable agent identifier and is
// populated only when ActorType is "agent". Both are omitempty so resource
// servers that do not yet consume them are unaffected.
type Claims struct {
	jwt.RegisteredClaims
	ClientID    string   `json:"client_id"`
	Scope       string   `json:"scope"`
	Roles       []string `json:"roles,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
	ActorType   string   `json:"actor_type,omitempty"`
	AgentID     string   `json:"agent_id,omitempty"`

	// Act carries the RFC 8693 §4.1 actor (delegation) chain. Each level
	// records the principal acting on behalf of the level below; the
	// outermost Actor is the most recent actor. Empty when the token was
	// issued without delegation (the default for client_credentials,
	// authorization_code, and refresh_token grants).
	//
	// A pointer to a value type lets us emit `null` semantics via
	// omitempty (a nil pointer is dropped, a populated pointer is
	// serialised). Storing Actor by value inside Actor (rather than by
	// pointer-recursion) keeps deserialisation simple — the encoding/json
	// recursion limit applies, but realistic chains stay shallow
	// (AUTH_MAX_DELEGATION_DEPTH defaults to 3 in identity-platform-go).
	Act *Actor `json:"act,omitempty"`
}

// Actor is one level of the RFC 8693 §4.1 actor chain.
//
// Sub identifies the principal at this level; ActorType / AgentID
// classify it per ADR-0015. Act recursively points to the next level —
// when nil, this is the deepest actor in the chain (i.e., the original
// caller that initiated the delegation).
//
// All fields are omitempty so the wire form stays compact for the
// common case (single-hop delegation: outer Act with no inner Act).
type Actor struct {
	Sub       string `json:"sub,omitempty"`
	ActorType string `json:"actor_type,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
	ClientID  string `json:"client_id,omitempty"`
	Act       *Actor `json:"act,omitempty"`
}

// Depth reports the length of the actor chain rooted at a. A nil
// receiver returns 0 — convenient for clamping against a configured
// max-delegation-depth without a nil guard at the call site. The
// receiver itself counts as 1; each nested Act adds 1.
func (a *Actor) Depth() int {
	depth := 0
	for cursor := a; cursor != nil; cursor = cursor.Act {
		depth++
	}
	return depth
}

// ClaimsConfig holds all inputs for NewClaims. Using a config struct instead of
// positional parameters prevents argument transposition at call sites — a risk
// with nine string/time parameters that format identically in error messages.
type ClaimsConfig struct {
	Issuer      string
	Subject     string
	TokenID     string
	ClientID    string
	Scope       string
	Audience    []string // RFC 9068 §2.2: resource server identifiers this token is intended for
	Roles       []string
	Permissions []string
	IssuedAt    time.Time
	ExpiresAt   time.Time

	// ActorType identifies the principal kind ("user" | "service" | "agent")
	// per identity-platform-go ADR-0015. Empty omits the claim, preserving the
	// pre-ADR-0015 wire shape for callers that do not yet classify principals.
	ActorType string
	// AgentID is the stable agent identifier per ADR-0015. Set when
	// ActorType is "agent"; empty otherwise.
	AgentID string

	// Act is the RFC 8693 §4.1 delegation chain rooted at the most
	// recent actor. Nil omits the claim — every grant other than
	// token-exchange leaves this nil. The token-exchange grant
	// constructs the chain by prepending the current actor to the
	// subject_token's Act chain.
	Act *Actor
}

// NewClaims constructs a Claims value from cfg, avoiding direct dependency on
// the jwt package in callers that only sign tokens.
// Roles and Permissions are defensively copied — callers may safely mutate
// their slices after calling NewClaims without affecting the returned Claims.
// Nil slices in cfg produce nil fields, which are omitted from the JWT (omitempty).
func NewClaims(cfg ClaimsConfig) *Claims {
	var audience jwt.ClaimStrings
	if len(cfg.Audience) > 0 {
		audience = append(jwt.ClaimStrings(nil), cfg.Audience...)
	}
	return &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    cfg.Issuer,
			Subject:   cfg.Subject,
			Audience:  audience,
			ID:        cfg.TokenID,
			IssuedAt:  jwt.NewNumericDate(cfg.IssuedAt),
			ExpiresAt: jwt.NewNumericDate(cfg.ExpiresAt),
		},
		ClientID:    cfg.ClientID,
		Scope:       cfg.Scope,
		Roles:       append([]string(nil), cfg.Roles...),
		Permissions: append([]string(nil), cfg.Permissions...),
		ActorType:   cfg.ActorType,
		AgentID:     cfg.AgentID,
		Act:         cfg.Act,
	}
}

// Sign creates and signs a JWT using HMAC-SHA256.
// Returns an error if claims is nil or signingKey is empty — both produce
// either a panic or a cryptographically unsafe token in the underlying library.
func Sign(claims *Claims, signingKey []byte) (string, error) {
	if claims == nil {
		return "", fmt.Errorf("signing token: claims must not be nil")
	}
	if len(signingKey) == 0 {
		return "", fmt.Errorf("signing token: signing key must not be empty")
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	// RFC 9068 §2.1: access tokens MUST carry typ:"at+jwt" in the JOSE header to
	// distinguish them from ID tokens and prevent token-type confusion attacks.
	t.Header["typ"] = accessTokenJWTType
	raw, err := t.SignedString(signingKey)
	if err != nil {
		return "", fmt.Errorf("signing token: %w", err)
	}
	return raw, nil
}

// Parse parses and validates a raw JWT string signed with HMAC-SHA256.
// Rejects tokens that do not carry typ:"at+jwt" in the JOSE header (RFC 9068 §2.1 /
// RFC 8725 §3.11) to prevent token-type confusion attacks.
// Returns a sentinel error (ErrTokenExpired, ErrTokenInvalid, ErrTokenMalformed)
// for specific failure modes so callers can distinguish them via errors.Is without
// importing the jwt library. Any error means the token is not valid for use.
// Callers that need RFC 7662 {active:false} semantics should treat any error as inactive.
func Parse(raw string, signingKey []byte) (*Claims, error) {
	return parseWith(raw, signingKey)
}

// ParseWithAudience parses and validates a raw JWT, additionally verifying that
// the aud claim contains the expected audience value. Returns ErrTokenInvalid when
// the audience is absent or does not match. Enforces typ:"at+jwt" (RFC 9068 §2.1).
// Use this in resource servers that need to enforce RFC 9068 §2.2 audience binding.
func ParseWithAudience(raw string, signingKey []byte, audience string) (*Claims, error) {
	return parseWith(raw, signingKey, jwt.WithAudience(audience))
}

// ParseWithIssuer parses and validates a raw JWT, additionally verifying that
// the iss claim matches the expected issuer value. Returns ErrTokenInvalid when
// the issuer is absent or does not match. Enforces typ:"at+jwt" (RFC 9068 §2.1).
// Use this in services that need RFC 8725 §3.8 issuer binding to prevent
// tokens from one issuer being accepted by services expecting another.
func ParseWithIssuer(raw string, signingKey []byte, issuer string) (*Claims, error) {
	return parseWith(raw, signingKey, jwt.WithIssuer(issuer))
}

// parseWith is the shared implementation for Parse and its claim-checking
// variants. It enforces HMAC signing, the at+jwt typ header, and then applies
// any extra parser options (audience binding, issuer binding, etc.).
func parseWith(raw string, signingKey []byte, opts ...jwt.ParserOption) (*Claims, error) {
	if len(signingKey) == 0 {
		return nil, fmt.Errorf("parsing token: %w", ErrTokenInvalid)
	}
	token, err := jwt.ParseWithClaims(raw, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		if typ, _ := t.Header["typ"].(string); typ != accessTokenJWTType {
			return nil, fmt.Errorf("unexpected token type: %v", t.Header["typ"])
		}
		return signingKey, nil
	}, opts...)
	if err != nil {
		return nil, mapJWTError(err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("parsing token: %w", ErrTokenInvalid)
	}

	return claims, nil
}

// mapJWTError converts jwt library errors to package sentinel errors so callers
// do not need to import the jwt library to inspect failure reasons.
func mapJWTError(err error) error {
	switch {
	case errors.Is(err, jwt.ErrTokenExpired):
		return fmt.Errorf("parsing token: %w", ErrTokenExpired)
	case errors.Is(err, jwt.ErrTokenSignatureInvalid):
		return fmt.Errorf("parsing token: %w", ErrTokenInvalid)
	case errors.Is(err, jwt.ErrTokenMalformed):
		return fmt.Errorf("parsing token: %w", ErrTokenMalformed)
	default:
		return fmt.Errorf("parsing token: %w", ErrTokenInvalid)
	}
}
