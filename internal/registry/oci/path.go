package oci

import (
	"regexp"
	"strconv"
	"strings"
)

// opKind identifies which OCI operation a request path addresses.
type opKind int

const (
	opUnknown opKind = iota
	opBlobUpload
	opBlob
	opManifest
	opTags
)

// parsedPath is a decoded /v2 request path: the repository name plus the
// operation and its reference (upload id, digest, or tag).
type parsedPath struct {
	name string
	kind opKind
	ref  string
}

// namePattern is the distribution-spec repository-name grammar: one or more
// lowercase path components.
// https://github.com/opencontainers/distribution-spec/blob/main/spec.md#pulling-manifests
var namePattern = regexp.MustCompile(`^[a-z0-9]+((\.|_|__|-+)[a-z0-9]+)*(/[a-z0-9]+((\.|_|__|-+)[a-z0-9]+)*)*$`)

// parsePath splits the portion of the URL after "/v2/" into a repository name
// and an operation. The operation is always the suffix (…/blobs/uploads[/id],
// …/blobs/<digest>, …/manifests/<ref>, …/tags/list), so the name is whatever
// precedes it — which may itself contain slashes.
func parsePath(tail string) parsedPath {
	if i := strings.LastIndex(tail, "/blobs/uploads"); i >= 0 {
		return parsedPath{
			name: tail[:i],
			kind: opBlobUpload,
			ref:  strings.TrimPrefix(tail[i+len("/blobs/uploads"):], "/"),
		}
	}
	if i := strings.LastIndex(tail, "/blobs/"); i >= 0 {
		return parsedPath{name: tail[:i], kind: opBlob, ref: tail[i+len("/blobs/"):]}
	}
	if i := strings.LastIndex(tail, "/manifests/"); i >= 0 {
		return parsedPath{name: tail[:i], kind: opManifest, ref: tail[i+len("/manifests/"):]}
	}
	if name, ok := strings.CutSuffix(tail, "/tags/list"); ok {
		return parsedPath{name: name, kind: opTags}
	}
	return parsedPath{kind: opUnknown}
}

// validName reports whether name is a well-formed repository name.
func validName(name string) bool {
	return name != "" && namePattern.MatchString(name)
}

// parseContentRange parses a "start-end" Content-Range value used by chunked
// blob uploads, returning the start offset.
func parseContentRange(v string) (start int64, ok bool) {
	first, _, found := strings.Cut(v, "-")
	if !found {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimSpace(first), 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}
