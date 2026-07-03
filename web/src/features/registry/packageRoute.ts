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
