// The npm package detail route. The project is the npm registry, so a package
// is identified by (project, name); the name may be scoped (@scope/name) and so
// carries a slash. The route reserves a "-" segment (which an OCI repository
// name can never start with) to keep the two registry routes unambiguous:
//
//   /registry/<project>/-/<name...>
//
// The name is the trailing splat, so its internal slash survives round-tripping.

/** packageHref builds the link to a package's detail page. */
export function packageHref(projectKey: string, name: string): string {
  // The project is a single safe path segment; the scoped name keeps its literal
  // slash so the splat captures the whole "@scope/name".
  return `/registry/${encodeURIComponent(projectKey)}/-/${name}`;
}

// The NuGet package detail route uses a distinct "-nuget-" sentinel segment so it
// never collides with the npm route ("-") or the OCI repo route (a bare splat):
//
//   /registry/<project>/-nuget-/<id>
//
// A NuGet id is dot-separated with no slashes, so it is a single safe segment.

/** nugetHref builds the link to a NuGet package's detail page. */
export function nugetHref(projectKey: string, id: string): string {
  return `/registry/${encodeURIComponent(projectKey)}/-nuget-/${encodeURIComponent(id)}`;
}
