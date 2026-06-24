package jwtutil_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/jedi-knights/go-platform/jwtutil"
)

func newIDTokenKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	return k
}

func idKeySource(t *testing.T, kid string, pub *rsa.PublicKey) jwtutil.KeySource {
	t.Helper()
	return func(_ context.Context, requested string) (*rsa.PublicKey, error) {
		if requested != kid {
			return nil, errors.New("unknown kid")
		}
		return pub, nil
	}
}

func makeIDClaims(now time.Time, audience string) *jwtutil.IDClaims {
	tru := true
	return &jwtutil.IDClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "https://identity.example.com",
			Subject:   "user-1234",
			Audience:  jwt.ClaimStrings{audience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute)),
		},
		Nonce:         "nonce-abc",
		AtHash:        "ahash",
		AuthTime:      now.Unix(),
		AMR:           []string{"pwd"},
		Email:         "alice@example.com",
		EmailVerified: &tru,
		Name:          "Alice Liddell",
		UpdatedAt:     now.Unix(),
	}
}

func TestSignIDToken_RoundTrip(t *testing.T) {
	t.Parallel()
	priv := newIDTokenKey(t)
	now := time.Now().Truncate(time.Second)
	claims := makeIDClaims(now, "client-abc")

	raw, err := jwtutil.SignIDToken(claims, priv, "kid-1")
	if err != nil {
		t.Fatalf("SignIDToken: %v", err)
	}
	got, err := jwtutil.ParseIDToken(context.Background(), raw, idKeySource(t, "kid-1", &priv.PublicKey), "client-abc")
	if err != nil {
		t.Fatalf("ParseIDToken: %v", err)
	}
	assertField(t, "Issuer", got.Issuer, "https://identity.example.com")
	assertField(t, "Subject", got.Subject, "user-1234")
	assertField(t, "Nonce", got.Nonce, "nonce-abc")
	assertField(t, "AtHash", got.AtHash, "ahash")
	assertField(t, "Name", got.Name, "Alice Liddell")
	assertField(t, "Email", got.Email, "alice@example.com")
}

func TestSignIDToken_TypIsJWT(t *testing.T) {
	t.Parallel()
	priv := newIDTokenKey(t)
	raw, err := jwtutil.SignIDToken(makeIDClaims(time.Now(), "client-abc"), priv, "kid-1")
	if err != nil {
		t.Fatalf("SignIDToken: %v", err)
	}
	parsed, _, err := new(jwt.Parser).ParseUnverified(raw, &jwtutil.IDClaims{})
	if err != nil {
		t.Fatalf("ParseUnverified: %v", err)
	}
	typ, _ := parsed.Header["typ"].(string)
	if typ != "JWT" {
		t.Errorf("typ header = %q, want %q (ID tokens must not carry at+jwt)", typ, "JWT")
	}
}

func TestSignIDToken_AlgIsRS256(t *testing.T) {
	t.Parallel()
	priv := newIDTokenKey(t)
	raw, err := jwtutil.SignIDToken(makeIDClaims(time.Now(), "client-abc"), priv, "kid-1")
	if err != nil {
		t.Fatalf("SignIDToken: %v", err)
	}
	parsed, _, _ := new(jwt.Parser).ParseUnverified(raw, &jwtutil.IDClaims{})
	if alg, _ := parsed.Header["alg"].(string); alg != "RS256" {
		t.Errorf("alg header = %q, want %q", alg, "RS256")
	}
}

func TestSignIDToken_NilClaims(t *testing.T) {
	t.Parallel()
	priv := newIDTokenKey(t)
	if _, err := jwtutil.SignIDToken(nil, priv, "kid-1"); err == nil {
		t.Fatal("expected error for nil claims, got nil")
	}
}

func TestSignIDToken_NilKey(t *testing.T) {
	t.Parallel()
	if _, err := jwtutil.SignIDToken(makeIDClaims(time.Now(), "client-abc"), nil, "kid-1"); err == nil {
		t.Fatal("expected error for nil private key, got nil")
	}
}

func TestSignIDToken_EmptyKID(t *testing.T) {
	t.Parallel()
	priv := newIDTokenKey(t)
	if _, err := jwtutil.SignIDToken(makeIDClaims(time.Now(), "client-abc"), priv, ""); err == nil {
		t.Fatal("expected error for empty kid, got nil")
	}
}

func TestParseIDToken_RejectsAtJWTTyp(t *testing.T) {
	// Type-confusion defense — an access-token-typ JWT must NOT validate as an
	// ID token. Mirror of the access-token side which rejects typ:"JWT".
	t.Parallel()
	priv := newIDTokenKey(t)
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, makeIDClaims(time.Now(), "client-abc"))
	token.Header["typ"] = "at+jwt"
	token.Header["kid"] = "kid-1"
	raw, err := token.SignedString(priv)
	if err != nil {
		t.Fatalf("constructing access-token-typ JWT: %v", err)
	}
	if _, err := jwtutil.ParseIDToken(context.Background(), raw, idKeySource(t, "kid-1", &priv.PublicKey), "client-abc"); err == nil {
		t.Fatal("expected ParseIDToken to reject typ:at+jwt, got nil")
	}
}

func TestParseRS256_RejectsIDTokenTyp(t *testing.T) {
	// The inverse defense — the access-token parser must reject ID tokens.
	t.Parallel()
	priv := newIDTokenKey(t)
	raw, err := jwtutil.SignIDToken(makeIDClaims(time.Now(), "client-abc"), priv, "kid-1")
	if err != nil {
		t.Fatalf("SignIDToken: %v", err)
	}
	if _, err := jwtutil.ParseRS256(context.Background(), raw, idKeySource(t, "kid-1", &priv.PublicKey)); err == nil {
		t.Fatal("expected ParseRS256 to reject ID token, got nil")
	}
}

func TestParseIDToken_RejectsHS256(t *testing.T) {
	t.Parallel()
	priv := newIDTokenKey(t)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, makeIDClaims(time.Now(), "client-abc"))
	token.Header["typ"] = "JWT"
	token.Header["kid"] = "kid-1"
	raw, err := token.SignedString([]byte("32-byte-hmac-secret-for-attack!!"))
	if err != nil {
		t.Fatalf("signing HS256 token: %v", err)
	}
	if _, err := jwtutil.ParseIDToken(context.Background(), raw, idKeySource(t, "kid-1", &priv.PublicKey), "client-abc"); err == nil {
		t.Fatal("expected ParseIDToken to reject HS256, got nil")
	}
}

func TestParseIDToken_RejectsNoneAlgorithm(t *testing.T) {
	t.Parallel()
	priv := newIDTokenKey(t)
	token := jwt.NewWithClaims(jwt.SigningMethodNone, makeIDClaims(time.Now(), "client-abc"))
	token.Header["typ"] = "JWT"
	token.Header["kid"] = "kid-1"
	raw, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("signing none token: %v", err)
	}
	if _, err := jwtutil.ParseIDToken(context.Background(), raw, idKeySource(t, "kid-1", &priv.PublicKey), "client-abc"); err == nil {
		t.Fatal("expected ParseIDToken to reject none alg, got nil")
	}
}

func TestParseIDToken_RejectsMissingKID(t *testing.T) {
	t.Parallel()
	priv := newIDTokenKey(t)
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, makeIDClaims(time.Now(), "client-abc"))
	token.Header["typ"] = "JWT"
	raw, err := token.SignedString(priv)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	_, err = jwtutil.ParseIDToken(context.Background(), raw, idKeySource(t, "kid-1", &priv.PublicKey), "client-abc")
	if err == nil {
		t.Fatal("expected ParseIDToken to reject token without kid, got nil")
	}
	if !errors.Is(err, jwtutil.ErrTokenInvalid) {
		t.Errorf("err = %v, want ErrTokenInvalid", err)
	}
}

func TestParseIDToken_ExpiredToken(t *testing.T) {
	t.Parallel()
	priv := newIDTokenKey(t)
	past := time.Now().Add(-2 * time.Hour)
	claims := makeIDClaims(past, "client-abc")
	claims.ExpiresAt = jwt.NewNumericDate(past.Add(time.Hour))
	raw, err := jwtutil.SignIDToken(claims, priv, "kid-1")
	if err != nil {
		t.Fatalf("SignIDToken: %v", err)
	}
	_, err = jwtutil.ParseIDToken(context.Background(), raw, idKeySource(t, "kid-1", &priv.PublicKey), "client-abc")
	if !errors.Is(err, jwtutil.ErrTokenExpired) {
		t.Errorf("err = %v, want ErrTokenExpired", err)
	}
}

func TestParseIDToken_AudienceMismatchRejected(t *testing.T) {
	t.Parallel()
	priv := newIDTokenKey(t)
	raw, err := jwtutil.SignIDToken(makeIDClaims(time.Now(), "client-actual"), priv, "kid-1")
	if err != nil {
		t.Fatalf("SignIDToken: %v", err)
	}
	_, err = jwtutil.ParseIDToken(context.Background(), raw, idKeySource(t, "kid-1", &priv.PublicKey), "client-other")
	if !errors.Is(err, jwtutil.ErrTokenInvalid) {
		t.Errorf("err = %v, want ErrTokenInvalid for audience mismatch", err)
	}
}

func TestParseIDToken_EmptyExpectedAudienceSkipsCheck(t *testing.T) {
	// Mirrors ParseRS256's behavior: when the audience parameter is empty,
	// the parser does not enforce audience binding (callers that want
	// binding pass a non-empty value).
	t.Parallel()
	priv := newIDTokenKey(t)
	raw, err := jwtutil.SignIDToken(makeIDClaims(time.Now(), "client-actual"), priv, "kid-1")
	if err != nil {
		t.Fatalf("SignIDToken: %v", err)
	}
	if _, err := jwtutil.ParseIDToken(context.Background(), raw, idKeySource(t, "kid-1", &priv.PublicKey), ""); err != nil {
		t.Fatalf("ParseIDToken with empty audience: %v", err)
	}
}

func TestParseIDToken_KeySourceErrorIsInvalidToken(t *testing.T) {
	t.Parallel()
	priv := newIDTokenKey(t)
	raw, err := jwtutil.SignIDToken(makeIDClaims(time.Now(), "client-abc"), priv, "kid-actual")
	if err != nil {
		t.Fatalf("SignIDToken: %v", err)
	}
	// Key source resolves a different kid; unknown-kid surfaces as ErrTokenInvalid.
	_, err = jwtutil.ParseIDToken(context.Background(), raw, idKeySource(t, "kid-other", &priv.PublicKey), "client-abc")
	if !errors.Is(err, jwtutil.ErrTokenInvalid) {
		t.Errorf("err = %v, want ErrTokenInvalid for KeySource miss", err)
	}
}

func TestParseIDToken_NilKeySource(t *testing.T) {
	t.Parallel()
	if _, err := jwtutil.ParseIDToken(context.Background(), "any.jwt.value", nil, "client-abc"); err == nil {
		t.Fatal("expected ParseIDToken to reject nil KeySource, got nil")
	}
}

func TestAtHash_Standard(t *testing.T) {
	// Reference: OIDC §3.1.3.6 — at_hash is the leftmost 128 bits of
	// SHA-256(access_token), base64url-encoded without padding. For a 32-byte
	// SHA-256 output, the leftmost 16 bytes encode to 22 base64url chars.
	t.Parallel()
	access := "eyJhbGciOiJSUzI1NiJ9.example.access_token"
	got := jwtutil.AtHash(access)
	if len(got) != 22 {
		t.Errorf("AtHash length = %d, want 22 (16 bytes base64url-unpadded)", len(got))
	}
	// Recompute manually to verify the value.
	sum := sha256.Sum256([]byte(access))
	want := base64.RawURLEncoding.EncodeToString(sum[:16])
	if got != want {
		t.Errorf("AtHash = %q, want %q", got, want)
	}
}
