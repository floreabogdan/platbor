package pypi

import (
	"regexp"
	"strings"
)

// pep503Runs matches runs of the separators PEP 503 collapses.
var pep503Runs = regexp.MustCompile(`[-_.]+`)

// normalizeName applies PEP 503 name normalization: lowercase, and collapse any
// run of "-", "_" or "." to a single "-". `pip install Flask_Foo` and
// `flask-foo` must resolve to the same project.
func normalizeName(name string) string {
	return strings.ToLower(pep503Runs.ReplaceAllString(name, "-"))
}
