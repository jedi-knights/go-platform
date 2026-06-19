// Package jwtutil provides HS256 JWT signing and parsing.
//
// It encapsulates the HMAC key-function boilerplate and exposes the canonical
// Claims type so callers share one definition. Parse returns sentinel errors
// (ErrTokenExpired, ErrTokenInvalid, ErrTokenMalformed); how those map to
// HTTP responses (e.g. RFC 7662 §2.2 "active:false") is the caller's choice.
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
type Claims struct {
	jwt.RegisteredClaims
	ClientID    string   `json:"client_id"`
	Scope       string   `json:"scope"`
	Roles       []string `json:"roles,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
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
