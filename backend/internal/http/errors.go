// Package http contains the HTTP transport: routing, middleware, request
// decoding, and response encoding.
//
// Handlers translate between HTTP and the service layer and do nothing else.
// No SQL, no business rules.
package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"atrium/internal/domain"
)

// ErrorBody is the single error shape the API ever returns. One shape means
// the frontend needs one error path, not one per endpoint.
type ErrorBody struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

type errorEnvelope struct {
	Error ErrorBody `json:"error"`
}

// Machine-readable codes. The frontend switches on these; the message is for
// humans and may be reworded without breaking a client.
const (
	CodeValidation   = "validation_failed"
	CodeUnauthorized = "unauthorized"
	CodeForbidden    = "forbidden"
	CodeNotFound     = "not_found"
	CodeConflict     = "conflict"
	CodeBadRequest   = "bad_request"
	CodeInternal     = "internal_error"
)

// writeJSON encodes v as the response body.
//
// The body is marshalled before the status is written: if encoding fails, the
// header has not been sent yet, so a 500 can still be returned instead of a
// 200 with a truncated body.
func writeJSON(w http.ResponseWriter, status int, v any) {
	buf, err := json.Marshal(v)
	if err != nil {
		slog.Error("encode response", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"internal_error","message":"Something went wrong."}}`))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(buf)
}

func writeNoContent(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }

// writeError maps a domain error to a status code and response body.
//
// This is the only place that mapping happens. Handlers return domain errors
// and call this; they never choose a status code themselves, so the mapping
// cannot drift between endpoints.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	status, code := statusForError(err)

	// 5xx means we broke something: log the full error with request context
	// for debugging, and tell the client nothing about it. Internal errors
	// routinely carry connection strings, SQL, and table names.
	if status >= 500 {
		slog.ErrorContext(r.Context(), "request failed",
			"error", err,
			"method", r.Method,
			"path", r.URL.Path,
		)
		writeJSON(w, status, errorEnvelope{ErrorBody{
			Code:    CodeInternal,
			Message: "Something went wrong on our end.",
		}})
		return
	}

	// 4xx messages are written for the person who will read them, so they are
	// safe and useful to pass through verbatim.
	writeJSON(w, status, errorEnvelope{ErrorBody{
		Code:    code,
		Message: userMessage(err),
	}})
}

// writeValidationError reports per-field problems, so a form can highlight the
// offending inputs rather than showing one message above everything.
func writeValidationError(w http.ResponseWriter, message string, fields map[string]string) {
	writeJSON(w, http.StatusUnprocessableEntity, errorEnvelope{ErrorBody{
		Code:    CodeValidation,
		Message: message,
		Fields:  fields,
	}})
}

func statusForError(err error) (int, string) {
	switch {
	case errors.Is(err, domain.ErrValidation):
		// 422, not 400: the request was syntactically valid JSON that the
		// server understood, but it asked for something the rules forbid.
		return http.StatusUnprocessableEntity, CodeValidation
	case errors.Is(err, domain.ErrUnauthorized):
		return http.StatusUnauthorized, CodeUnauthorized
	case errors.Is(err, domain.ErrForbidden):
		return http.StatusForbidden, CodeForbidden
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound, CodeNotFound
	case errors.Is(err, domain.ErrConflict):
		return http.StatusConflict, CodeConflict
	default:
		return http.StatusInternalServerError, CodeInternal
	}
}

// userMessage strips the sentinel prefix from a wrapped domain error, so
// "conflict: that slot is already booked" reads as "That slot is already
// booked." rather than leaking the sentinel's name into the UI.
func userMessage(err error) string {
	msg := err.Error()
	for _, sentinel := range []error{
		domain.ErrValidation,
		domain.ErrUnauthorized,
		domain.ErrForbidden,
		domain.ErrNotFound,
		domain.ErrConflict,
	} {
		prefix := sentinel.Error() + ": "
		if len(msg) > len(prefix) && msg[:len(prefix)] == prefix {
			return capitalize(msg[len(prefix):])
		}
		if msg == sentinel.Error() {
			return defaultMessages[sentinel.Error()]
		}
	}
	return capitalize(msg)
}

var defaultMessages = map[string]string{
	domain.ErrValidation.Error():   "That request could not be processed.",
	domain.ErrUnauthorized.Error(): "You need to sign in to do that.",
	domain.ErrForbidden.Error():    "You do not have permission to do that.",
	domain.ErrNotFound.Error():     "Not found.",
	domain.ErrConflict.Error():     "That conflicts with something that already exists.",
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	if c := s[0]; c >= 'a' && c <= 'z' {
		return string(c-32) + s[1:]
	}
	return s
}
