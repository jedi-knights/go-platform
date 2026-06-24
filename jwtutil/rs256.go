package jwtutil

import (
	"context"
	"crypto/rsa"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
)

// KeySource resolves the verification public key for a given JWT kid header.
// JWKS-backed implementations look the kid up in a cached key set and refresh
// out-of-cycle on miss. Implementations returning an error cause the verifier
// to reject the token as ErrTokenInvalid — there is no silent acceptance path.
type KeySource func(ctx context.Context, kid string) (*rsa.PublicKey, error)

// SignRS256 creates and signs a JWT access token using RSASSA-PKCS1-v1_5 with
// SHA-256 (RFC 7518 §3.3). The JOSE header carries alg:"RS256", typ:"at+jwt"
// (RFC 9068 §2.1), and kid for verifier key lookup (RFC 7517 §4.5).
// Returns an error if claims is nil, privateKey is nil, or kid is empty —
// each would produce either a panic or a verifier-unfriendly token.
func SignRS256(claims *Claims, privateKey *rsa.PrivateKey, kid string) (string, error) {
	if claims == nil {
		return "", fmt.Errorf("signing token: claims must not be nil")
	}
	return signRSA256JWT(claims, privateKey, accessTokenJWTType, kid)
}

// ParseRS256 parses and validates a raw JWT signed with RS256, resolving the
// verification key via keySource based on the kid header. The keyfunc enforces:
//
//   - alg header is exactly RS256 — no fallback to HS256 with the public key
//     as the secret (RFC 8725 §3.1 algorithm-confusion defense)
//   - typ header is exactly "at+jwt" (RFC 9068 §2.1 / RFC 8725 §3.11)
//   - kid header is present and non-empty
//
// Returns a sentinel error (ErrTokenExpired, ErrTokenInvalid, ErrTokenMalformed)
// so callers distinguish failure modes via errors.Is without importing the jwt
// library. Any error means the token is not valid for use. KeySource errors —
// including unknown-kid responses — surface as ErrTokenInvalid, never as silent
// acceptance.
func ParseRS256(ctx context.Context, raw string, keySource KeySource) (*Claims, error) {
	if keySource == nil {
		return nil, fmt.Errorf("parsing token: %w", ErrTokenInvalid)
	}
	token, err := jwt.ParseWithClaims(raw, &Claims{}, rs256Keyfunc(ctx, keySource))
	if err != nil {
		return nil, mapJWTError(err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("parsing token: %w", ErrTokenInvalid)
	}

	return claims, nil
}

// rs256Keyfunc builds the jwt.Keyfunc that ParseRS256 hands to the underlying
// library. It is factored out so the validation branches it contains (method,
// alg, typ, kid, key resolution) sit in a single function with their own
// complexity budget — not stacked on top of ParseRS256's result-handling code.
func rs256Keyfunc(ctx context.Context, keySource KeySource) jwt.Keyfunc {
	return func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		if alg, _ := t.Header["alg"].(string); alg != "RS256" {
			return nil, fmt.Errorf("unexpected signing alg: %v", alg)
		}
		if typ, _ := t.Header["typ"].(string); typ != accessTokenJWTType {
			return nil, fmt.Errorf("unexpected token type: %v", t.Header["typ"])
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
