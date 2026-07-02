package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

// maxBodyBytes caps request bodies to keep JSON decoding bounded.
const maxBodyBytes = 1 << 20 // 1 MiB

// Problem is an RFC 7807 problem+json document — the single error shape across
// /api/v1 (docs/CODING-STANDARDS.md).
type Problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// writeProblem renders an RFC 7807 error response.
func writeProblem(w http.ResponseWriter, status int, title, detail string) {
	p := Problem{Type: "about:blank", Title: title, Status: status, Detail: detail}
	body, err := json.Marshal(p)
	if err != nil {
		// Marshaling a fixed-shape struct cannot realistically fail; degrade safely.
		http.Error(w, title, status)
		return
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// writeJSON renders v as a JSON response with the given status.
func writeJSON(w http.ResponseWriter, log *slog.Logger, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		log.Error("encoding response", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// decodeJSON strictly decodes a JSON request body into dst, rejecting unknown
// fields and oversized or malformed payloads with a client-facing message.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("decoding request body: %w", err)
	}
	// Reject trailing data after the first JSON value.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain a single JSON object")
	}
	return nil
}
