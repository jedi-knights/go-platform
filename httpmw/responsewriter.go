package httpmw

import "net/http"

// responseWriter wraps [http.ResponseWriter] to capture the status code and
// track whether the response header has been committed. Logging
// uses the captured status; Recovery uses wroteHeader to avoid
// emitting a second response after a panic.
type responseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.wroteHeader = true
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		// Explicitly call WriteHeader(200) through the wrapper so that both
		// the wrapper state (status, wroteHeader) and the underlying
		// ResponseWriter are committed via a single canonical path.
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}
