// A package lives in a typed repository inside a project, so it is identified by
// (project, repo, name). The npm route reserves a "-" segment (which an OCI
// repository name can never start with) to keep the registry routes unambiguous;
// the trailing splat carries <repo>/<name>, and the name's internal slash (for a
// scoped @scope/name) survives round-tripping:
//
//   /registry/<project>/-/<repo>/<name...>

/** packageHref builds the link to an npm package's detail page. */
export function packageHref(projectKey: string, repoKey: string, name: string): string {
  return `/registry/${encodeURIComponent(projectKey)}/-/${encodeURIComponent(repoKey)}/${name}`;
}

// The NuGet package detail route uses a distinct "-nuget-" sentinel segment so it
// never collides with the npm route ("-") or the OCI image route (a bare splat).
// The splat carries <repo>/<id>; a NuGet id is dot-separated with no slashes.
//
//   /registry/<project>/-nuget-/<repo>/<id>

/** nugetHref builds the link to a NuGet package's detail page. */
export function nugetHref(projectKey: string, repoKey: string, id: string): string {
  return `/registry/${encodeURIComponent(projectKey)}/-nuget-/${encodeURIComponent(repoKey)}/${encodeURIComponent(id)}`;
}

// The PyPI package detail route uses a distinct "-pypi-" sentinel segment. The
// splat carries <repo>/<name>; a PyPI project name has no slashes.
//
//   /registry/<project>/-pypi-/<repo>/<name>

/** pypiHref builds the link to a PyPI package's detail page. */
export function pypiHref(projectKey: string, repoKey: string, name: string): string {
  return `/registry/${encodeURIComponent(projectKey)}/-pypi-/${encodeURIComponent(repoKey)}/${encodeURIComponent(name)}`;
}

// The Maven artifact detail route uses a distinct "-maven-" sentinel segment. The
// splat carries <repo>/<groupId>:<artifactId>; a Maven groupId is dot-separated
// and neither coordinate contains a slash.
//
//   /registry/<project>/-maven-/<repo>/<groupId>:<artifactId>

/** mavenHref builds the link to a Maven artifact's detail page. */
export function mavenHref(projectKey: string, repoKey: string, groupId: string, artifactId: string): string {
  const coord = `${groupId}:${artifactId}`;
  return `/registry/${encodeURIComponent(projectKey)}/-maven-/${encodeURIComponent(repoKey)}/${encodeURIComponent(coord)}`;
}

// The Go module detail route uses a distinct "-go-" sentinel segment. The splat
// carries <repo>/<module>; a module path contains slashes (github.com/user/repo),
// which survive as the rest of the splat.
//
//   /registry/<project>/-go-/<repo>/<module...>

/** goHref builds the link to a Go module's detail page. */
export function goHref(projectKey: string, repoKey: string, module: string): string {
  return `/registry/${encodeURIComponent(projectKey)}/-go-/${encodeURIComponent(repoKey)}/${module}`;
}

// The Cargo crate detail route uses a distinct "-cargo-" sentinel segment. The
// splat carries <repo>/<crate>; a crate name has no slashes.
//
//   /registry/<project>/-cargo-/<repo>/<crate>

/** cargoHref builds the link to a Cargo crate's detail page. */
export function cargoHref(projectKey: string, repoKey: string, name: string): string {
  return `/registry/${encodeURIComponent(projectKey)}/-cargo-/${encodeURIComponent(repoKey)}/${encodeURIComponent(name)}`;
}

// The RubyGems gem detail route uses a distinct "-gem-" sentinel segment. The
// splat carries <repo>/<gem>; a gem name has no slashes.
//
//   /registry/<project>/-gem-/<repo>/<gem>

/** rubygemsHref builds the link to a RubyGems gem's detail page. */
export function rubygemsHref(projectKey: string, repoKey: string, name: string): string {
  return `/registry/${encodeURIComponent(projectKey)}/-gem-/${encodeURIComponent(repoKey)}/${encodeURIComponent(name)}`;
}

/** ociHref builds the link to an OCI image's detail page: the bare splat carries
 *  <repo>/<image>. */
export function ociHref(projectKey: string, repoKey: string, image: string): string {
  return `/registry/${encodeURIComponent(projectKey)}/${encodeURIComponent(repoKey)}/${image}`;
}

/** splitRepoAndRest splits a detail-route splat ("<repo>/<rest...>") into the
 *  repository key and the remaining artifact identifier (which may contain
 *  slashes — a scoped npm name or a multi-segment image path). */
export function splitRepoAndRest(splat: string): { repo: string; rest: string } {
  const slash = splat.indexOf('/');
  if (slash < 0) {
    return { repo: splat, rest: '' };
  }
  return { repo: splat.slice(0, slash), rest: splat.slice(slash + 1) };
}
