// Package httputil provides HTTP response helpers shared across
// jedi-knights services.
//
// # Surface
//
//   - [WriteJSON] / [WriteError] — buffer-before-headers JSON response helpers.
//     Encoding failures result in 500 Internal Server Error, never a 200 with
//     a truncated body.
//   - [HTTPStatus] — maps an [apperrors.AppError] code to its HTTP status.
//
// HTTP middleware (trace IDs, request IDs, logging, panic recovery) lives in
// [github.com/jedi-knights/go-platform/httpmw], not here.
//
// # WriteJSON invariant
//
// [WriteJSON] always encodes into a [bytes.Buffer] before touching the
// [http.ResponseWriter]. This is intentional and must be preserved: a stream
// encode that fails mid-write commits a 200 OK header with a truncated body,
// which is worse than a 500 because clients cannot distinguish it from a
// successful response.
package httputil
