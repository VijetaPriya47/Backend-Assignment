// Package httputil provides shared HTTP helpers used by all handler packages.
package httputil

import (
	"encoding/json"
	"net/http"
)

// ErrResp is the standard error envelope returned on all non-2xx responses.
type ErrResp struct {
	Error string `json:"error"`
}

// WriteJSON marshals v to JSON and writes it with the given status code.
// Using json.Marshal + w.Write avoids allocating a new json.Encoder per call.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

// BadRequest writes a 400 with a plain error message.
func BadRequest(w http.ResponseWriter, msg string) {
	WriteJSON(w, http.StatusBadRequest, ErrResp{Error: msg})
}

// NotFound writes a 404 with a plain error message.
func NotFound(w http.ResponseWriter, msg string) {
	WriteJSON(w, http.StatusNotFound, ErrResp{Error: msg})
}

// InternalError writes a 500 with a generic message.
// The real error is intentionally not forwarded to the caller.
func InternalError(w http.ResponseWriter) {
	WriteJSON(w, http.StatusInternalServerError, ErrResp{Error: "internal server error"})
}
