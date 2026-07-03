// The npm package detail route. A package is identified by (project, repository,
// name), and the name may be scoped (@scope/name) — so it carries a slash. The
// route reserves a "-" segment (which an OCI repository name can never start
// with) to keep the two registry routes unambiguous:
//
//   /registry/<project>/-/<repository>/<name...>
//
// The name is the trailing splat, so its internal slash survives round-tripping.

/** packageHref builds the link to a package's detail page. */
export function packageHref(projectKey: string, repository: string, name: string): string {
  // project and repository are single, safe path segments; the scoped name keeps
  // its literal slash so the splat captures the whole "@scope/name".
  return `/registry/${encodeURIComponent(projectKey)}/-/${encodeURIComponent(repository)}/${name}`;
}
