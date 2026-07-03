package maven

import "strings"

// coordinates parses a Maven repository path into its (groupId, artifactId,
// version) coordinates and marks metadata files. The layout is:
//
//	<group with / separators>/<artifactId>/<version>/<artifactId>-<version>...
//	<group>/<artifactId>/maven-metadata.xml                (artifact level)
//	<group>/<artifactId>/<version>/maven-metadata.xml      (SNAPSHOT, version level)
//
// Coordinates drive the browser only, so an unusual path parses best-effort (an
// empty groupId/artifactId just means it will not group into an artifact).
func coordinates(path string) (groupID, artifactID, version, filename string, isMetadata bool) {
	segs := strings.Split(path, "/")
	filename = segs[len(segs)-1]
	dirs := segs[:len(segs)-1]
	isMetadata = filename == "maven-metadata.xml" || strings.HasPrefix(filename, "maven-metadata.xml.")

	if isMetadata {
		// Version-level metadata has a version directory (starts with a digit);
		// artifact-level metadata does not.
		if len(dirs) >= 2 && looksLikeVersion(dirs[len(dirs)-1]) {
			version = dirs[len(dirs)-1]
			artifactID = dirs[len(dirs)-2]
			groupID = strings.Join(dirs[:len(dirs)-2], ".")
		} else if len(dirs) >= 1 {
			artifactID = dirs[len(dirs)-1]
			groupID = strings.Join(dirs[:len(dirs)-1], ".")
		}
		return groupID, artifactID, version, filename, true
	}

	// A regular artifact file: <group>/<artifact>/<version>/<file>.
	if len(dirs) < 3 {
		return "", "", "", filename, false
	}
	version = dirs[len(dirs)-1]
	artifactID = dirs[len(dirs)-2]
	groupID = strings.Join(dirs[:len(dirs)-2], ".")
	return groupID, artifactID, version, filename, false
}

// looksLikeVersion reports whether a path segment is a Maven version directory:
// versions start with a digit (1.0.0, 2.1-SNAPSHOT), artifactIds effectively
// never do. Good enough to tell an artifact-level from a version-level metadata
// path.
func looksLikeVersion(seg string) bool {
	return seg != "" && seg[0] >= '0' && seg[0] <= '9'
}

// isMetadataPath reports whether a path names a maven-metadata.xml file or one of
// its checksums, which are mutable and never permanently cached for a proxy.
func isMetadataPath(path string) bool {
	i := strings.LastIndexByte(path, '/')
	name := path[i+1:]
	return name == "maven-metadata.xml" || strings.HasPrefix(name, "maven-metadata.xml.")
}
