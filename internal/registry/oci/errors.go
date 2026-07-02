package oci

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// OCI distribution-spec error codes used by the blob API.
// https://github.com/opencontainers/distribution-spec/blob/main/spec.md#error-codes
const (
	codeBlobUnknown       = "BLOB_UNKNOWN"
	codeBlobUploadUnknown = "BLOB_UPLOAD_UNKNOWN"
	codeBlobUploadInvalid = "BLOB_UPLOAD_INVALID"
	codeDigestInvalid     = "DIGEST_INVALID"
	codeNameInvalid       = "NAME_INVALID"
	codeUnsupported       = "UNSUPPORTED"
	codeUnauthorized      = "UNAUTHORIZED"
	codeDenied            = "DENIED"
	codeManifestUnknown   = "MANIFEST_UNKNOWN"
)

// ociError is one entry in the spec's error envelope.
type ociError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// writeError renders the distribution-spec error envelope
// ({"errors":[{code,message}]}) with the given status.
func writeError(w http.ResponseWriter, log *slog.Logger, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	body := map[string][]ociError{"errors": {{Code: code, Message: message}}}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Error("encoding oci error", slog.String("error", err.Error()))
	}
}
