package audit_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/jedi-knights/go-platform/audit"
)

func TestNewEventID_Format(t *testing.T) {
	id := audit.NewEventID()
	if len(id) != 26 {
		t.Fatalf("expected 26-character ULID, got %d: %q", len(id), id)
	}
	// Crockford base32 alphabet.
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	for _, c := range id {
		if !strings.ContainsRune(alphabet, c) {
			t.Errorf("character %q not in Crockford base32 alphabet", c)
		}
	}
}

func TestNewEventID_UniqueAcrossGoroutines(t *testing.T) {
	const n = 10_000
	const goroutines = 8
	ids := make([]string, 0, n*goroutines)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := make([]string, 0, n)
			for i := 0; i < n; i++ {
				local = append(local, audit.NewEventID())
			}
			mu.Lock()
			ids = append(ids, local...)
			mu.Unlock()
		}()
	}
	wg.Wait()

	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate ULID generated: %q", id)
		}
		seen[id] = struct{}{}
	}
}

func TestNewEventID_TimeOrdered(t *testing.T) {
	// Two IDs taken at increasing wall-clock times should compare in order
	// because the leading bits encode milliseconds since epoch.
	a := audit.NewEventID()
	// Spin until at least one millisecond has passed.
	for {
		b := audit.NewEventID()
		if b[:10] != a[:10] {
			if !(a < b) {
				t.Fatalf("expected a < b, got a=%q b=%q", a, b)
			}
			return
		}
	}
}
