package jwtutil

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
)

// idTokenJWTType is the JOSE "typ" header value SignIDToken sets and
// ParseIDToken requires. RFC 9068 introduces "at+jwt" for OAuth 2.0 access
// tokens; OIDC ID tokens have no analogous mandate, but emitting an explicit
// "JWT" lets the verifier reject "at+jwt" outright as the type-confusion
// defense (Mandatory Behaviors → "When validating tokens" in CLAUDE.md).
const idTokenJWTType = "JWT"

// IDClaims is the canonical OIDC ID-token claim set (OIDC Core §2). It is a
// sibling type to Claims, not a subset — the two share registered claims via
// jwt.RegisteredClaims but carry distinct optional fields:
//   - Claims (access tokens) carries Roles + Permissions for RBAC.
//   - IDClaims carries Nonce, AtHash, AuthTime, AMR + profile/email claims.
//
// Keeping them separate stops access-token-only fields from leaking into ID
// tokens via JSON serialization and lets each parse function enforce its own
// `typ` invariant without runtime branching.
//
// EmailVerified is a pointer so the absence of the claim is distinguishable
// from an explicit false. Nil omits the field; non-nil emits the value. OIDC
// recommends emitting email_verified whenever email is present.
type IDClaims struct {
	jwt.RegisteredClaims

	Nonce    string   `json:"nonce,omitempty"`
	AtHash   string   `json:"at_hash,omitempty"`
	AuthTime int64    `json:"auth_time,omitempty"`
	AMR      []string `json:"amr,omitempty"`

	Email         string `json:"email,omitempty"`
	EmailVerified *bool  `json:"email_verified,omitempty"`
	Name          string `json:"name,omitempty"`
	UpdatedAt     int64  `json:"updated_at,omitempty"`

	// ActorType identifies the principal kind ("user" | "service" | "agent")
	// per identity-platform-go ADR-0015. Empty omits the claim. ID tokens for
	// an OIDC end-user typically carry "user"; the field exists on IDClaims
	// for symmetry with [Claims] when an agent surfaces as the end-user
	// (uncommon today, but the wire shape stays stable).
	ActorType string `json:"actor_type,omitempty"`
	// AgentID is the stable agent identifier per ADR-0015. Set when
	// ActorType is "agent"; empty otherwise.
	AgentID string `json:"agent_id,omitempty"`
}

// SignIDToken signs OIDC ID-token claims with RSASSA-PKCS1-v1_5 + SHA-256
// (RFC 7518 §3.3). The JOSE header carries alg:"RS256", typ:"JWT" (NOT
// "at+jwt" — that distinguishes ID tokens from access tokens and lets the
// type-confusion defense in ParseIDToken / ParseRS256 fire), and kid for
// JWKS lookup (RFC 7517 §4.5).
//
// Returns an error when claims is nil, privateKey is nil, or kid is empty;
// each would produce either a panic or a verifier-unfriendly token.
func SignIDToken(claims *IDClaims, privateKey *rsa.PrivateKey, kid string) (string, error) {
	if claims == nil {
		return "", fmt.Errorf("signing ID token: claims must not be nil")
	}
	return signRSA256JWT(claims, privateKey, idTokenJWTType, kid)
}

// signRSA256JWT is the shared body of SignRS256 and SignIDToken. The typed-
// nil check belongs in the caller (Go's interface nil-trap means we cannot
// safely accept *Claims / *IDClaims via the jwt.Claims interface and detect
// nil here without a typeswitch).
func signRSA256JWT(claims jwt.Claims, privateKey *rsa.PrivateKey, typ, kid string) (string, error) {
	if privateKey == nil {
		return "", fmt.Errorf("signing token: private key must not be nil")
	}
	if kid == "" {
		return "", fmt.Errorf("signing token: kid must not be empty")
	}
	t := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	t.Header["typ"] = typ
	t.Header["kid"] = kid
	raw, err := t.SignedString(privateKey)
	if err != nil {
		return "", fmt.Errorf("signing token: %w", err)
	}
	return raw, nil
}

// ParseIDToken parses and validates an RS256-signed OIDC ID token, resolving
// the verification key via keySource keyed on the JOSE kid header. When
// expectedAudience is non-empty, the aud claim must contain it (OIDC §3.1.3.7
// #3). Empty expectedAudience skips audience binding for callers that need
// to inspect the claim themselves.
//
// The keyfunc enforces four invariants:
//
//   - alg header is exactly RS256 — no fallback to HS256 (RFC 8725 §3.1
//     algorithm-confusion defense).
//   - typ header is "JWT" or absent (NOT "at+jwt"). A typ of "at+jwt"
//     indicates an OAuth 2.0 access token; presenting one where an ID
//     token is expected is the token-type-confusion vector OIDC clients
//     defend against here.
//   - kid header is present and non-empty.
//   - KeySource resolution succeeds.
//
// All failures surface as sentinel errors (ErrTokenExpired / ErrTokenInvalid
// / ErrTokenMalformed) so callers can distinguish via errors.Is without
// importing the jwt library.
func ParseIDToken(ctx context.Context, raw string, keySource KeySource, expectedAudience string) (*IDClaims, error) {
	if keySource == nil {
		return nil, fmt.Errorf("parsing ID token: %w", ErrTokenInvalid)
	}
	opts := []jwt.ParserOption{}
	if expectedAudience != "" {
		opts = append(opts, jwt.WithAudience(expectedAudience))
	}
	token, err := jwt.ParseWithClaims(raw, &IDClaims{}, idTokenKeyfunc(ctx, keySource), opts...)
	if err != nil {
		return nil, mapJWTError(err)
	}
	claims, ok := token.Claims.(*IDClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("parsing ID token: %w", ErrTokenInvalid)
	}
	return claims, nil
}

// idTokenKeyfunc is the jwt.Keyfunc ParseIDToken uses. Factored out so its
// branches sit in their own complexity budget away from ParseIDToken's
// result-handling code.
func idTokenKeyfunc(ctx context.Context, keySource KeySource) jwt.Keyfunc {
	return func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		if alg, _ := t.Header["alg"].(string); alg != "RS256" {
			return nil, fmt.Errorf("unexpected signing alg: %v", alg)
		}
		typ, _ := t.Header["typ"].(string)
		if err := validateIDTokenTyp(typ); err != nil {
			return nil, err
		}
		kid, _ := t.Header["kid"].(string)
		if kid == "" {
			return nil, fmt.Errorf("missing kid header")
		}
		pub, err := keySource(ctx, kid)
		if err != nil {
			return nil, fmt.Errorf("resolving key for kid %q: %w", kid, err)
		}
		if pub == nil {
			return nil, fmt.Errorf("no public key for kid %q", kid)
		}
		return pub, nil
	}
}

// validateIDTokenTyp enforces the ID-token typ-header invariant. Extracted so
// idTokenKeyfunc stays under the cyclomatic-complexity cap. Two failure modes
// are distinguished by error message so a triage reader can tell whether they
// saw a token-type-confusion attempt (at+jwt) or just an unexpected value.
func validateIDTokenTyp(typ string) error {
	if typ == accessTokenJWTType {
		return fmt.Errorf("ID-token parser rejected typ:%q (access-token type)", typ)
	}
	if typ != "" && typ != idTokenJWTType {
		return fmt.Errorf("unexpected ID-token typ: %v", typ)
	}
	return nil
}

// AtHash returns the OIDC at_hash claim value for the given access token:
// the leftmost 128 bits (16 bytes) of SHA-256(access_token), base64url-encoded
// without padding. Per OIDC §3.1.3.6, the hash algorithm matches the ID
// token's signing alg — for RS256 that is SHA-256.
//
// Use this at issuance time over the FINAL signed access token (after every
// non-standard claim has been added) so the binding holds against the token
// the relying party will see. ADR-0010 §"ID token shape" calls this out
// explicitly.
func AtHash(accessToken string) string {
	sum := sha256.Sum256([]byte(accessToken))
	return base64.RawURLEncoding.EncodeToString(sum[:16])
}
