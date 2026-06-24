package jwtutil_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/jedi-knights/go-platform/jwtutil"
)

// generateTestKey returns a fresh 2048-bit RSA keypair. Used per-test to avoid
// any cross-test key reuse — slow but unambiguous.
func generateTestKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	return k
}

// staticKeySource returns a KeySource that resolves a single kid to the matching
// public key, and returns an error for any other kid.
func staticKeySource(kid string, pub *rsa.PublicKey) jwtutil.KeySource {
	return func(_ context.Context, requestedKID string) (*rsa.PublicKey, error) {
		if requestedKID != kid {
			return nil, errors.New("unknown kid")
		}
		return pub, nil
	}
}

func makeRS256Claims(now time.Time) *jwtutil.Claims {
	return jwtutil.NewClaims(jwtutil.ClaimsConfig{
		Issuer:    "identity-platform",
		Subject:   "user-1234",
		TokenID:   "tok-1",
		ClientID:  "client-abc",
		Scope:     "openid read",
		IssuedAt:  now,
		ExpiresAt: now.Add(time.Hour),
	})
}

func TestSignRS256_RoundTrip(t *testing.T) {
	t.Parallel()

	priv := generateTestKey(t)
	now := time.Now().Truncate(time.Second)
	claims := makeRS256Claims(now)

	raw, err := jwtutil.SignRS256(claims, priv, "test-kid-1")
	if err != nil {
		t.Fatalf("SignRS256: %v", err)
	}

	got, err := jwtutil.ParseRS256(context.Background(), raw, staticKeySource("test-kid-1", &priv.PublicKey))
	if err != nil {
		t.Fatalf("ParseRS256: %v", err)
	}

	assertField(t, "Subject", got.Subject, "user-1234")
	assertField(t, "ClientID", got.ClientID, "client-abc")
	assertField(t, "Scope", got.Scope, "openid read")
	assertField(t, "Issuer", got.Issuer, "identity-platform")
}

func TestSignRS256_NilClaims(t *testing.T) {
	t.Parallel()

	priv := generateTestKey(t)
	_, err := jwtutil.SignRS256(nil, priv, "test-kid-1")
	if err == nil {
		t.Fatal("expected error for nil claims, got nil")
	}
}

func TestSignRS256_NilKey(t *testing.T) {
	t.Parallel()

	now := time.Now().Truncate(time.Second)
	claims := makeRS256Claims(now)
	_, err := jwtutil.SignRS256(claims, nil, "test-kid-1")
	if err == nil {
		t.Fatal("expected error for nil private key, got nil")
	}
}

func TestSignRS256_EmptyKID(t *testing.T) {
	t.Parallel()

	priv := generateTestKey(t)
	now := time.Now().Truncate(time.Second)
	claims := makeRS256Claims(now)
	_, err := jwtutil.SignRS256(claims, priv, "")
	if err == nil {
		t.Fatal("expected error for empty kid, got nil")
	}
}

// TestSignRS256_KIDInHeader verifies the issued token carries the kid in its
// JOSE header so resource servers can look up the verification key.
func TestSignRS256_KIDInHeader(t *testing.T) {
	t.Parallel()

	priv := generateTestKey(t)
	now := time.Now().Truncate(time.Second)
	claims := makeRS256Claims(now)

	raw, err := jwtutil.SignRS256(claims, priv, "2026-06-23a")
	if err != nil {
		t.Fatalf("SignRS256: %v", err)
	}

	parsed, _, err := new(jwt.Parser).ParseUnverified(raw, &jwtutil.Claims{})
	if err != nil {
		t.Fatalf("ParseUnverified: %v", err)
	}
	kid, _ := parsed.Header["kid"].(string)
	if kid != "2026-06-23a" {
		t.Errorf("kid header = %q, want %q", kid, "2026-06-23a")
	}
}

// TestSignRS256_AlgInHeader verifies the issued token carries alg=RS256 in its
// JOSE header. This is what tells a verifier which signing scheme to expect.
func TestSignRS256_AlgInHeader(t *testing.T) {
	t.Parallel()

	priv := generateTestKey(t)
	now := time.Now().Truncate(time.Second)
	claims := makeRS256Claims(now)

	raw, err := jwtutil.SignRS256(claims, priv, "test-kid-1")
	if err != nil {
		t.Fatalf("SignRS256: %v", err)
	}

	parsed, _, err := new(jwt.Parser).ParseUnverified(raw, &jwtutil.Claims{})
	if err != nil {
		t.Fatalf("ParseUnverified: %v", err)
	}
	alg, _ := parsed.Header["alg"].(string)
	if alg != "RS256" {
		t.Errorf("alg header = %q, want %q", alg, "RS256")
	}
}

// TestSignRS256_TypInHeader verifies access tokens still carry typ:"at+jwt"
// (RFC 9068 §2.1) under RS256, matching the HS256 behaviour.
func TestSignRS256_TypInHeader(t *testing.T) {
	t.Parallel()

	priv := generateTestKey(t)
	now := time.Now().Truncate(time.Second)
	claims := makeRS256Claims(now)

	raw, err := jwtutil.SignRS256(claims, priv, "test-kid-1")
	if err != nil {
		t.Fatalf("SignRS256: %v", err)
	}

	parsed, _, err := new(jwt.Parser).ParseUnverified(raw, &jwtutil.Claims{})
	if err != nil {
		t.Fatalf("ParseUnverified: %v", err)
	}
	typ, _ := parsed.Header["typ"].(string)
	if typ != "at+jwt" {
		t.Errorf("typ header = %q, want %q", typ, "at+jwt")
	}
}

func TestParseRS256_ExpiredToken(t *testing.T) {
	t.Parallel()

	priv := generateTestKey(t)
	past := time.Now().Add(-2 * time.Hour)
	claims := jwtutil.NewClaims(jwtutil.ClaimsConfig{
		Issuer:    "identity-platform",
		Subject:   "user-1234",
		TokenID:   "tok-exp",
		ClientID:  "client-abc",
		Scope:     "read",
		IssuedAt:  past,
		ExpiresAt: past.Add(time.Hour),
	})
	raw, err := jwtutil.SignRS256(claims, priv, "test-kid-1")
	if err != nil {
		t.Fatalf("SignRS256: %v", err)
	}

	_, err = jwtutil.ParseRS256(context.Background(), raw, staticKeySource("test-kid-1", &priv.PublicKey))
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
	if !errors.Is(err, jwtutil.ErrTokenExpired) {
		t.Errorf("expected ErrTokenExpired, got %v", err)
	}
}

func TestParseRS256_WrongKey(t *testing.T) {
	t.Parallel()

	signingKey := generateTestKey(t)
	differentKey := generateTestKey(t)
	now := time.Now().Truncate(time.Second)
	claims := makeRS256Claims(now)

	raw, err := jwtutil.SignRS256(claims, signingKey, "test-kid-1")
	if err != nil {
		t.Fatalf("SignRS256: %v", err)
	}

	// Wrong public key returned by the KeySource — verification must fail.
	_, err = jwtutil.ParseRS256(context.Background(), raw, staticKeySource("test-kid-1", &differentKey.PublicKey))
	if err == nil {
		t.Fatal("expected error for wrong signing key, got nil")
	}
	if !errors.Is(err, jwtutil.ErrTokenInvalid) {
		t.Errorf("expected ErrTokenInvalid, got %v", err)
	}
}

func TestParseRS256_MalformedToken(t *testing.T) {
	t.Parallel()

	priv := generateTestKey(t)
	_, err := jwtutil.ParseRS256(context.Background(), "not.a.jwt", staticKeySource("test-kid-1", &priv.PublicKey))
	if err == nil {
		t.Fatal("expected error for malformed token, got nil")
	}
	if !errors.Is(err, jwtutil.ErrTokenMalformed) {
		t.Errorf("expected ErrTokenMalformed, got %v", err)
	}
}

// TestParseRS256_RejectsHS256 is the algorithm-confusion defence (RFC 8725 §3.1).
// If ParseRS256 accepted HS256 tokens, an attacker holding the public key could
// forge tokens by signing HS256 with the RSA public key as the HMAC secret —
// a documented attack against RS256 verifiers that allow algorithm flexibility.
func TestParseRS256_RejectsHS256(t *testing.T) {
	t.Parallel()

	priv := generateTestKey(t)
	hsKey := []byte("a-test-signing-key-that-is-32-chars-long!!")

	// Build an HS256 token; ParseRS256 must refuse to validate it.
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, &jwtutil.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "sub",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})
	token.Header["typ"] = "at+jwt"
	token.Header["kid"] = "test-kid-1"
	raw, err := token.SignedString(hsKey)
	if err != nil {
		t.Fatalf("signing HS256 token: %v", err)
	}

	_, err = jwtutil.ParseRS256(context.Background(), raw, staticKeySource("test-kid-1", &priv.PublicKey))
	if err == nil {
		t.Fatal("expected ParseRS256 to reject HS256-signed token, got nil")
	}
	if !errors.Is(err, jwtutil.ErrTokenInvalid) {
		t.Errorf("expected ErrTokenInvalid for HS256 token, got %v", err)
	}
}

// TestParseRS256_RejectsNoneAlgorithm verifies the alg=none confusion defence.
func TestParseRS256_RejectsNoneAlgorithm(t *testing.T) {
	t.Parallel()

	priv := generateTestKey(t)
	token := jwt.NewWithClaims(jwt.SigningMethodNone, &jwtutil.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "sub",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})
	token.Header["typ"] = "at+jwt"
	token.Header["kid"] = "test-kid-1"
	raw, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("signing none token: %v", err)
	}

	_, err = jwtutil.ParseRS256(context.Background(), raw, staticKeySource("test-kid-1", &priv.PublicKey))
	if err == nil {
		t.Fatal("expected ParseRS256 to reject none-algorithm token, got nil")
	}
}

// TestParseRS256_RejectsMissingKID verifies tokens without a kid header are
// refused — the platform mandates kid for forward-compat with key rotation.
func TestParseRS256_RejectsMissingKID(t *testing.T) {
	t.Parallel()

	priv := generateTestKey(t)
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, &jwtutil.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "sub",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})
	token.Header["typ"] = "at+jwt"
	// Deliberately do not set "kid".
	raw, err := token.SignedString(priv)
	if err != nil {
		t.Fatalf("signing RS256 token: %v", err)
	}

	_, err = jwtutil.ParseRS256(context.Background(), raw, staticKeySource("test-kid-1", &priv.PublicKey))
	if err == nil {
		t.Fatal("expected ParseRS256 to reject token without kid, got nil")
	}
	if !errors.Is(err, jwtutil.ErrTokenInvalid) {
		t.Errorf("expected ErrTokenInvalid for missing kid, got %v", err)
	}
}

// TestParseRS256_RejectsTokenWithoutAtJWTTyp mirrors the HS256 defence:
// tokens missing typ:"at+jwt" are refused (RFC 9068 §2.1 / RFC 8725 §3.11).
func TestParseRS256_RejectsTokenWithoutAtJWTTyp(t *testing.T) {
	t.Parallel()

	priv := generateTestKey(t)
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, &jwtutil.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "sub",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})
	token.Header["kid"] = "test-kid-1"
	// Library default typ is "JWT" — not "at+jwt".
	raw, err := token.SignedString(priv)
	if err != nil {
		t.Fatalf("signing RS256 token: %v", err)
	}

	_, err = jwtutil.ParseRS256(context.Background(), raw, staticKeySource("test-kid-1", &priv.PublicKey))
	if err == nil {
		t.Fatal("expected ParseRS256 to reject token without typ:at+jwt, got nil")
	}
	if !errors.Is(err, jwtutil.ErrTokenInvalid) {
		t.Errorf("expected ErrTokenInvalid, got %v", err)
	}
}

// TestParseRS256_KeySourceError verifies that a KeySource that returns an error
// (e.g. unknown kid) propagates as ErrTokenInvalid — never as a missing-key
// silent acceptance.
func TestParseRS256_KeySourceError(t *testing.T) {
	t.Parallel()

	priv := generateTestKey(t)
	now := time.Now().Truncate(time.Second)
	claims := makeRS256Claims(now)

	raw, err := jwtutil.SignRS256(claims, priv, "good-kid")
	if err != nil {
		t.Fatalf("SignRS256: %v", err)
	}

	// KeySource is configured for a different kid → unknown-kid error.
	_, err = jwtutil.ParseRS256(context.Background(), raw, staticKeySource("other-kid", &priv.PublicKey))
	if err == nil {
		t.Fatal("expected ParseRS256 to fail when KeySource errors, got nil")
	}
	if !errors.Is(err, jwtutil.ErrTokenInvalid) {
		t.Errorf("expected ErrTokenInvalid for KeySource error, got %v", err)
	}
}

// TestParseRS256_NilKeySource verifies the function defends against nil
// KeySource at the boundary rather than crashing.
func TestParseRS256_NilKeySource(t *testing.T) {
	t.Parallel()

	priv := generateTestKey(t)
	now := time.Now().Truncate(time.Second)
	claims := makeRS256Claims(now)

	raw, err := jwtutil.SignRS256(claims, priv, "test-kid-1")
	if err != nil {
		t.Fatalf("SignRS256: %v", err)
	}

	_, err = jwtutil.ParseRS256(context.Background(), raw, nil)
	if err == nil {
		t.Fatal("expected ParseRS256 to fail with nil KeySource, got nil")
	}
}

// TestParseRS256_KeySourceReceivesKID verifies that ParseRS256 invokes the
// KeySource with the kid extracted from the token's JOSE header. This is the
// JWKS lookup hook — every resource server's KeySource implementation depends
// on receiving the correct kid.
func TestParseRS256_KeySourceReceivesKID(t *testing.T) {
	t.Parallel()

	priv := generateTestKey(t)
	now := time.Now().Truncate(time.Second)
	claims := makeRS256Claims(now)

	raw, err := jwtutil.SignRS256(claims, priv, "expected-kid")
	if err != nil {
		t.Fatalf("SignRS256: %v", err)
	}

	var receivedKID string
	ks := func(_ context.Context, kid string) (*rsa.PublicKey, error) {
		receivedKID = kid
		return &priv.PublicKey, nil
	}

	if _, err := jwtutil.ParseRS256(context.Background(), raw, ks); err != nil {
		t.Fatalf("ParseRS256: %v", err)
	}
	if receivedKID != "expected-kid" {
		t.Errorf("KeySource received kid = %q, want %q", receivedKID, "expected-kid")
	}
}
