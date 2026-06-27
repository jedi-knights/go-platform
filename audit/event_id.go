package audit

import (
	"crypto/rand"
	"encoding/binary"
	"sync"
	"time"
)

// NewEventID returns a fresh ULID-formatted event identifier.
//
// The ULID format (https://github.com/ulid/spec) packs a 48-bit big-endian
// millisecond timestamp followed by 80 bits of cryptographic randomness into
// 16 bytes, encoded as a 26-character Crockford base32 string. The format
// gives globally unique, lexicographically time-ordered identifiers — exactly
// what the metering shim needs for Lago transaction_id (ADR-0019).
//
// NewEventID is safe to call concurrently. It panics if the system entropy
// source is unavailable, which is treated as a programming environment
// invariant rather than a recoverable error.
func NewEventID() string {
	return newEventIDAt(time.Now())
}

// newEventIDAt is the testable kernel; tests inject a deterministic time.
// It is private so callers depend only on [NewEventID].
func newEventIDAt(now time.Time) string {
	var b [16]byte

	// First 48 bits: milliseconds since the Unix epoch, big-endian.
	// ULID spec caps the timestamp at 2^48 - 1 (year 10889); panicking on
	// overflow is the spec-recommended behavior.
	ms := uint64(now.UnixMilli())
	if ms >= 1<<48 {
		panic("audit: ULID timestamp overflow")
	}
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)

	// Last 80 bits: cryptographic randomness. crypto/rand.Read never returns
	// a partial read or recoverable error per its current contract; if it
	// ever fails we treat it as a fatal environmental issue.
	if _, err := rand.Read(b[6:]); err != nil {
		panic("audit: entropy source unavailable: " + err.Error())
	}

	return crockfordEncode(b)
}

// crockfordAlphabet is the Crockford base32 alphabet used by ULID — Douglas
// Crockford's omission of I, L, O, U keeps decoded values unambiguous.
const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// crockfordEncode encodes 16 bytes as 26 Crockford base32 characters using
// the ULID encoding (timestamp + randomness packed without padding).
// The encoding draws 5-bit groups from the 128-bit big-endian value, which is
// equivalent to the per-spec lookup tables but more compact in source.
func crockfordEncode(b [16]byte) string {
	// View the 16 bytes as a big.Int-like 128-bit value addressed in 5-bit
	// chunks from the most-significant bit. 26 output chars * 5 bits = 130
	// bits; the first 2 bits are the high-order bits of the timestamp byte,
	// matching the ULID specification.
	hi := binary.BigEndian.Uint64(b[0:8])
	lo := binary.BigEndian.Uint64(b[8:16])

	var out [26]byte
	for i := 25; i >= 0; i-- {
		// Pop the low 5 bits of the 128-bit value, encode, then shift right.
		out[i] = crockfordAlphabet[lo&0x1f]
		// Shift the 128-bit value right by 5.
		lo = (lo >> 5) | ((hi & 0x1f) << 59)
		hi >>= 5
	}
	return string(out[:])
}

// monoMu and monoLastMS guard the rare clock-go-backwards case in tests and
// CI runners. They are unused by NewEventID today (the randomness component
// makes collisions astronomically unlikely even within a millisecond) but
// reserved here so a monotonic guarantee can be added without an API change.
//
//nolint:unused // reserved for future monotonic-ULID variant
var (
	monoMu     sync.Mutex
	monoLastMS uint64
)
