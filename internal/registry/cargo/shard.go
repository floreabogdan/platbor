package cargo

import "strings"

// normalizeName lowercases a crate name for index-path sharding and lookups.
// Cargo treats crate names case-insensitively for the index path (and '-'/'_' are
// distinct, unlike PyPI).
func normalizeName(name string) string { return strings.ToLower(name) }

// crateFromIndexPath extracts the crate name from a sparse index request path.
// The index path shards by name length: 1-char names live under "1/", 2-char
// under "2/", 3-char under "3/<first>/", and 4+ under "<first two>/<next two>/".
// Whatever the shard, the crate name is always the final path segment.
func crateFromIndexPath(path string) (string, bool) {
	path = strings.Trim(path, "/")
	if path == "" {
		return "", false
	}
	i := strings.LastIndexByte(path, '/')
	name := path
	if i >= 0 {
		name = path[i+1:]
	}
	if name == "" || strings.Contains(name, "..") {
		return "", false
	}
	return name, true
}

// indexPath builds the sharded index path for a lowercased crate name, matching
// the layout cargo expects (used when rewriting/serving is needed).
func indexPath(nameLower string) string {
	switch n := len(nameLower); n {
	case 0:
		return ""
	case 1:
		return "1/" + nameLower
	case 2:
		return "2/" + nameLower
	case 3:
		return "3/" + nameLower[:1] + "/" + nameLower
	default:
		return nameLower[:2] + "/" + nameLower[2:4] + "/" + nameLower
	}
}
