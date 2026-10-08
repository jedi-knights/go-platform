package httpmw_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/jedi-knights/go-logging/pkg/logging"
)

const validUUID = "f47ac10b-58cc-4372-a567-0e02b2c3d479"

// uuidV4 is the contract for IDs this package emits and accepts, restated
// here so tests do not depend on the package's own pattern.
var uuidV4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

var ok = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

func newBufLogger(t *testing.T) (logging.Logger, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	return logging.New(logging.Config{Level: "debug", Format: "json", Output: &buf}), &buf
}

// logLines decodes every JSON log record written to buf.
func logLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	dec := json.NewDecoder(bytes.NewReader(buf.Bytes()))
	for dec.More() {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			t.Fatalf("log output is not JSON: %v\n%s", err, buf.String())
		}
		out = append(out, m)
	}
	return out
}

// findLog returns the first record whose msg equals msg, or nil.
func findLog(lines []map[string]any, msg string) map[string]any {
	for _, l := range lines {
		if l["msg"] == msg {
			return l
		}
	}
	return nil
}

func serve(h http.Handler, path string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func mustPanic(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s: expected panic", name)
		}
	}()
	fn()
}
