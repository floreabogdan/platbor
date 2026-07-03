package npm

import (
	"regexp"
	"strings"
)

// opKind identifies which npm operation a request addresses. Publish vs
// packument (both bare "<pkg>") and dist-tag set vs delete are distinguished by
// HTTP method at dispatch, not here.
type opKind int

const (
	opUnknown  opKind = iota
	opLogin           // PUT /-/user/org.couchdb.user:<name>
	opWhoami          // GET /-/whoami
	opDistTags        // /-/package/<pkg>/dist-tags[/<tag>]
	opTarball         // GET /<pkg>/-/<filename>
	opPackage         // GET (packument) or PUT (publish) /<pkg>
)

// npmOp is a parsed npm request path.
type npmOp struct {
	kind opKind
	pkg  string // package name, may be scoped (@scope/name)
	ref  string // dist-tag name (opDistTags) or tarball filename (opTarball)
	user string // username (opLogin)
}

// couchUserPrefix is the CouchDB user-document prefix npm's legacy login uses:
// PUT /-/user/org.couchdb.user:<username>.
const couchUserPrefix = "-/user/org.couchdb.user:"

// parsePath splits the portion of the URL after /npm/<project>/<repo>/ into an
// npm operation. tail must already be percent-decoded, so scoped names carry a
// literal '/'. Operations are anchored on fixed markers ("-/user/", "-/whoami",
// "-/package/.../dist-tags", "/-/"), and the package name is whatever a marker
// leaves — which for a scoped package itself contains a slash.
func parsePath(tail string) npmOp {
	tail = strings.TrimPrefix(tail, "/")

	if user, ok := strings.CutPrefix(tail, couchUserPrefix); ok {
		return npmOp{kind: opLogin, user: user}
	}
	if tail == "-/whoami" {
		return npmOp{kind: opWhoami}
	}
	if rest, ok := strings.CutPrefix(tail, "-/package/"); ok {
		i := strings.Index(rest, "/dist-tags")
		if i < 0 {
			return npmOp{kind: opUnknown}
		}
		pkg := rest[:i]
		after := rest[i+len("/dist-tags"):]
		tag := strings.TrimPrefix(after, "/")
		return npmOp{kind: opDistTags, pkg: pkg, ref: tag}
	}
	if i := strings.Index(tail, "/-/"); i >= 0 {
		return npmOp{kind: opTarball, pkg: tail[:i], ref: tail[i+len("/-/"):]}
	}
	if tail == "" {
		return npmOp{kind: opUnknown}
	}
	return npmOp{kind: opPackage, pkg: tail}
}

// packageNamePattern is a deliberately permissive but safe npm package-name
// grammar: an optional @scope/ prefix then the name, each component a run of
// url-safe characters. It excludes path separators and dot-segments so a name
// can never escape its repository.
var packageNamePattern = regexp.MustCompile(`^(@[a-z0-9][a-z0-9._~-]*/)?[a-z0-9][a-z0-9._~-]*$`)

// validPackageName reports whether name is a well-formed npm package name.
func validPackageName(name string) bool {
	return name != "" && !strings.Contains(name, "..") && packageNamePattern.MatchString(name)
}

// versionFromFilename extracts the version from a tarball filename. npm names
// tarballs "<basename>-<version>.tgz", where basename is the package name minus
// any @scope/ prefix — so "@acme/widgets" at 1.2.3 is "widgets-1.2.3.tgz".
func versionFromFilename(pkg, filename string) (string, bool) {
	base := pkg
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	trimmed, ok := strings.CutSuffix(filename, ".tgz")
	if !ok {
		return "", false
	}
	version, ok := strings.CutPrefix(trimmed, base+"-")
	if !ok || version == "" {
		return "", false
	}
	return version, true
}
