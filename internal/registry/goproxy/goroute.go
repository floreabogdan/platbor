package goproxy

import (
	"errors"
	"strings"
)

// op is the kind of Go proxy request.
type op int

const (
	opFile   op = iota // <module>/@v/<version>.<ext>  (immutable, cached)
	opList             // <module>/@v/list             (mutable)
	opLatest           // <module>/@latest             (mutable)
)

// goRequest is a parsed Go proxy request. module and version are decoded to
// canonical (mixed-case) form; ext is one of info|mod|zip for opFile.
type goRequest struct {
	op      op
	module  string // decoded, e.g. github.com/Azure/go-autorest
	version string // decoded, e.g. v1.2.3
	ext     string // info | mod | zip (opFile only)
}

var errBadPath = errors.New("unrecognized go proxy path")

// parseGoPath splits an escaped Go proxy request path into its parts. The path is
// <module>/@v/<file> or <module>/@latest, where <module> and the version segment
// use the "!"-lowercase escaping the Go proxy protocol defines.
func parseGoPath(splat string) (goRequest, error) {
	if mod, ok := strings.CutSuffix(splat, "/@latest"); ok && mod != "" {
		return goRequest{op: opLatest, module: unescape(mod)}, nil
	}
	i := strings.Index(splat, "/@v/")
	if i <= 0 {
		return goRequest{}, errBadPath
	}
	module := unescape(splat[:i])
	rest := splat[i+len("/@v/"):]
	if rest == "list" {
		return goRequest{op: opList, module: module}, nil
	}
	for _, ext := range []string{".info", ".mod", ".zip"} {
		if ver, ok := strings.CutSuffix(rest, ext); ok && ver != "" && !strings.Contains(ver, "/") {
			return goRequest{op: opFile, module: module, version: unescape(ver), ext: ext[1:]}, nil
		}
	}
	return goRequest{}, errBadPath
}

// unescape decodes the Go module proxy path encoding: "!" before a lowercase
// letter denotes the uppercase letter (so a case-insensitive filesystem cannot
// collide "Azure" with "azure"). Used only for display/storage; the escaped path
// is forwarded to the upstream verbatim.
func unescape(s string) string {
	if !strings.Contains(s, "!") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '!' && i+1 < len(s) {
			i++
			n := s[i]
			if n >= 'a' && n <= 'z' {
				b.WriteByte(n - 'a' + 'A')
			} else {
				b.WriteByte(n)
			}
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// contentType picks the Content-Type for a Go proxy response.
func contentType(req goRequest) string {
	switch req.op {
	case opList:
		return "text/plain; charset=utf-8"
	case opLatest:
		return "application/json"
	default:
		switch req.ext {
		case "info":
			return "application/json"
		case "mod":
			return "text/plain; charset=utf-8"
		case "zip":
			return "application/zip"
		}
	}
	return "application/octet-stream"
}
