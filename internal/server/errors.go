package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	prettylog "github.com/rickliujh/loom/internal/log"
)

// Error codes, as the API contract names them. Each maps to one HTTP status.
const (
	codeUnauthorized  = "unauthorized"
	codeForbidden     = "forbidden"
	codeMisdirected   = "misdirected"
	codeNotFound      = "not_found"
	codeConflict      = "conflict"
	codeInvalid       = "invalid_request"
	codeUnprocessable = "unprocessable"
	codeInternal      = "internal"
)

var codeStatus = map[string]int{
	codeUnauthorized:  http.StatusUnauthorized,
	codeForbidden:     http.StatusForbidden,
	codeMisdirected:   http.StatusMisdirectedRequest,
	codeNotFound:      http.StatusNotFound,
	codeConflict:      http.StatusConflict,
	codeInvalid:       http.StatusBadRequest,
	codeUnprocessable: http.StatusUnprocessableEntity,
	codeInternal:      http.StatusInternalServerError,
}

// apiError is an error with the code the client sees. Handlers return one
// instead of writing the response themselves, so every failure has the same
// envelope and status.
type apiError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func (e *apiError) Error() string { return e.Code + ": " + e.Message }

func (e *apiError) status() int {
	if s, ok := codeStatus[e.Code]; ok {
		return s
	}
	return http.StatusInternalServerError
}

func newError(code, format string, args ...any) *apiError {
	return &apiError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// withField names the request field the error is about.
func (e *apiError) withField(field string) *apiError {
	return e.with("field", field)
}

func (e *apiError) with(key string, value any) *apiError {
	if e.Details == nil {
		e.Details = map[string]any{}
	}
	e.Details[key] = value
	return e
}

func errInvalid(field, format string, args ...any) *apiError {
	e := newError(codeInvalid, format, args...)
	if field != "" {
		e.withField(field)
	}
	return e
}

func errNotFound(format string, args ...any) *apiError {
	return newError(codeNotFound, format, args...)
}

func errForbidden(format string, args ...any) *apiError {
	return newError(codeForbidden, format, args...)
}

func errConflict(format string, args ...any) *apiError {
	return newError(codeConflict, format, args...)
}

// redact removes URL credentials (https://user:token@host) from text bound
// for a client, a log or history.
func redact(s string) string { return prettylog.RedactURLUserinfo(s) }

// errUnprocessable reports a module that could not be loaded; details carries
// the messages, one per line of the underlying error.
func errUnprocessable(err error) *apiError {
	e := newError(codeUnprocessable, "%s", redact(err.Error()))
	return e.with("messages", []string{redact(err.Error())})
}

// asAPIError turns any error into the one the client sees: an apiError as it
// is, anything else as internal.
func asAPIError(err error) *apiError {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae
	}
	return newError(codeInternal, "%s", redact(err.Error()))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	ae := asAPIError(err)
	writeJSON(w, ae.status(), map[string]any{"error": ae})
}

// handlerFunc is an API handler that reports failure by returning an error.
type handlerFunc func(w http.ResponseWriter, r *http.Request) error

func (s *Server) api(h handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			writeError(w, err)
		}
	}
}

// decodeBody reads a JSON request body into v. An empty body leaves v as it
// is, so endpoints whose body is optional need no special case.
func decodeBody(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return errInvalid("", "request body is larger than %d bytes", tooBig.Limit)
		}
		return errInvalid("", "malformed JSON body: %v", err)
	}
	if dec.More() {
		return errInvalid("", "malformed JSON body: more than one value")
	}
	return nil
}
