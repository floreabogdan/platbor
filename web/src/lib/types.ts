// API types mirror the Go /api/v1 shapes (camelCase, RFC 3339 timestamps).
// One source of truth per shape — keep in sync with internal/httpapi.

export interface User {
  id: string;
  username: string;
  email: string;
  isAdmin: boolean;
  createdAt: string;
}

export interface Project {
  id: string;
  key: string;
  name: string;
  description: string;
  // When true (default), a push to an unknown repo path auto-creates a local
  // repository of that format; when false, repos must be created before pushing.
  allowAutoCreate: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface ListProjectsResponse {
  projects: Project[];
  nextCursor?: string;
}

export interface CreateProjectRequest {
  key: string;
  name: string;
  description?: string;
  allowAutoCreate?: boolean;
}

// --- Repositories (the typed, configured containers inside a project) ---

export type RepoFormat = 'oci' | 'npm' | 'nuget' | 'generic' | 'pypi' | 'maven' | 'go' | 'cargo' | 'rubygems';
export type RepoMode = 'local' | 'proxy';

export interface RepoUpstream {
  url: string;
  username?: string;
}

export interface RepoRetention {
  keepLast: number;
  deleteUntagged: boolean;
}

/** Repo — a typed artifact repository inside a project. */
export interface Repo {
  key: string;
  name: string;
  format: RepoFormat;
  mode: RepoMode;
  upstream?: RepoUpstream; // set for proxy repos; password is never returned
  retention: RepoRetention;
  createdAt: string;
  updatedAt: string;
}

export interface ListReposResponse {
  repositories: Repo[];
}

export interface CreateRepoRequest {
  key: string;
  name: string;
  format: RepoFormat;
  mode: RepoMode;
  upstream?: { url: string; username?: string; password?: string };
  retention: RepoRetention;
}

export interface UpdateRepoRequest {
  name: string;
  upstream?: { url: string; username?: string; password?: string };
  retention: RepoRetention;
}

export interface Token {
  id: string;
  name: string;
  prefix: string;
  createdAt: string;
  expiresAt?: string;
}

export interface CreateTokenRequest {
  name: string;
  expiresInDays?: number;
}

/** The create response includes the raw secret, shown exactly once. */
export interface CreateTokenResponse extends Token {
  token: string;
}

/** RFC 7807 problem+json — the single error shape from the API. */
export interface Problem {
  type: string;
  title: string;
  status: number;
  detail?: string;
}

// --- Registry browser ---

/** A manifest is a single-platform image, a multi-platform index, or a Helm
 *  chart (an image-shaped OCI artifact with the Helm config media type). */
export type ManifestKind = 'image' | 'index' | 'chart';

/** Repository — one artifact repository in a project (registry index). */
export interface Repository {
  projectKey: string;
  projectName: string;
  repoKey: string; // the typed repository the image lives in
  repository: string; // the OCI image name within the repo
  kind: 'local' | 'proxy'; // proxy = a pull-through mirror of an upstream registry
  tagCount: number;
  manifestCount: number;
  sizeBytes: number;
  updatedAt: string;
}

export interface ListRepositoriesResponse {
  repositories: Repository[];
}

// --- npm packages ---

/** NpmPackage — one npm package in a project (the project is the npm registry). */
export interface NpmPackage {
  projectKey: string;
  projectName: string;
  repoKey: string;
  name: string; // package name, incl. @scope/ prefix
  kind: 'local' | 'proxy';
  versionCount: number;
  sizeBytes: number;
  updatedAt: string;
}

export interface ListPackagesResponse {
  packages: NpmPackage[];
}

/** NpmPackageVersion — one published version of a package. */
export interface NpmPackageVersion {
  version: string;
  sizeBytes: number;
  shasum: string;
  integrity: string;
  publishedAt: string;
}

/** MemberRole — a user's role within a project, in increasing privilege. */
export type MemberRole = 'reader' | 'maintainer' | 'admin';

/** Member — a user with a role in a project. */
export interface Member {
  username: string;
  email: string;
  role: MemberRole;
  createdAt?: string;
  updatedAt?: string;
}

/** ListMembersResponse — a project's members. */
export interface ListMembersResponse {
  members: Member[];
}

/** NpmPackageDetail — a package with its versions (newest first) and dist-tags. */
export interface NpmPackageDetail {
  name: string;
  distTags: Record<string, string>;
  versions: NpmPackageVersion[];
  /** The latest version's README markdown, when published with one. */
  readme?: string;
}

// --- NuGet packages ---

/** NugetPackage — one NuGet package in a project (the project is the feed). */
export interface NugetPackage {
  projectKey: string;
  projectName: string;
  repoKey: string;
  id: string;
  kind: 'local' | 'proxy';
  versionCount: number;
  sizeBytes: number;
  updatedAt: string;
}

export interface ListNugetsResponse {
  packages: NugetPackage[];
}

/** NugetPackageVersion — one pushed version of a NuGet package. */
export interface NugetPackageVersion {
  version: string;
  sizeBytes: number;
  publishedAt: string;
}

/** NugetPackageDetail — a package with its versions (newest first). */
export interface NugetPackageDetail {
  id: string;
  versions: NugetPackageVersion[];
  /** The nuspec <description> of the latest version, shown as the README. */
  readme?: string;
}

// --- PyPI packages ---

/** PyPIPackage — one Python package (project) in a repository. */
export interface PyPIPackage {
  projectKey: string;
  projectName: string;
  repoKey: string;
  name: string;
  kind: 'local' | 'proxy';
  fileCount: number;
  sizeBytes: number;
  updatedAt: string;
}

export interface ListPypisResponse {
  packages: PyPIPackage[];
}

/** PyPIFile — one distribution file (an sdist or wheel) of a package. */
export interface PyPIFile {
  filename: string;
  version: string;
  sizeBytes: number;
  sha256: string;
  requiresPython?: string;
}

/** PyPIPackageDetail — a package with its distribution files. */
export interface PyPIPackageDetail {
  name: string;
  files: PyPIFile[];
}

// --- Maven artifacts ---

/** MavenArtifact — one Maven artifact (groupId:artifactId) in a repository. */
export interface MavenArtifact {
  projectKey: string;
  projectName: string;
  repoKey: string;
  groupId: string;
  artifactId: string;
  kind: 'local' | 'proxy';
  versionCount: number;
  sizeBytes: number;
  updatedAt: string;
}

export interface ListMavensResponse {
  artifacts: MavenArtifact[];
}

/** MavenFile — one file of an artifact (a pom, jar, checksum, or metadata). */
export interface MavenFile {
  path: string;
  version: string;
  filename: string;
  isMetadata: boolean;
  sizeBytes: number;
  sha1?: string;
}

/** MavenArtifactDetail — an artifact with its files. */
export interface MavenArtifactDetail {
  groupId: string;
  artifactId: string;
  files: MavenFile[];
}

// --- Go modules ---

/** GoModule — one Go module cached in a repository (the project proxies a GOPROXY). */
export interface GoModule {
  projectKey: string;
  projectName: string;
  repoKey: string;
  module: string;
  kind: 'local' | 'proxy';
  versionCount: number;
  sizeBytes: number;
  updatedAt: string;
}

export interface ListGoModulesResponse {
  modules: GoModule[];
}

/** GoVersion — one cached version of a module (info + mod + optional zip). */
export interface GoVersion {
  version: string;
  sizeBytes: number;
  hasZip: boolean;
}

/** GoModuleDetail — a module with its cached versions. */
export interface GoModuleDetail {
  module: string;
  versions: GoVersion[];
}

// --- Cargo crates ---

/** CargoCrate — one Rust crate in a repository (the project is the registry). */
export interface CargoCrate {
  projectKey: string;
  projectName: string;
  repoKey: string;
  name: string;
  kind: 'local' | 'proxy';
  versionCount: number;
  sizeBytes: number;
  updatedAt: string;
}

export interface ListCargoCratesResponse {
  crates: CargoCrate[];
}

/** CargoVersion — one published version of a crate. */
export interface CargoVersion {
  version: string;
  sizeBytes: number;
  yanked: boolean;
  cksum?: string;
}

/** CargoCrateDetail — a crate with its versions (newest first). */
export interface CargoCrateDetail {
  name: string;
  versions: CargoVersion[];
}

// --- RubyGems ---

/** RubyGem — one gem in a repository (the project is the gem source). */
export interface RubyGem {
  projectKey: string;
  projectName: string;
  repoKey: string;
  name: string;
  kind: 'local' | 'proxy';
  versionCount: number;
  sizeBytes: number;
  updatedAt: string;
}

export interface ListRubyGemsResponse {
  gems: RubyGem[];
}

/** RubyGemVersion — one pushed version of a gem. */
export interface RubyGemVersion {
  number: string; // version, or version-platform for non-ruby platforms
  version: string;
  platform: string;
  sizeBytes: number;
  yanked: boolean;
  sha256?: string;
}

/** RubyGemDetail — a gem with its versions (newest first). */
export interface RubyGemDetail {
  name: string;
  versions: RubyGemVersion[];
}

// --- Generic files ---

/** GenericFile — one arbitrary versioned file at a path under a project. */
export interface GenericFile {
  projectKey: string;
  projectName: string;
  repoKey: string;
  path: string;
  kind: 'local' | 'proxy';
  sizeBytes: number;
  updatedAt: string;
}

export interface ListGenericFilesResponse {
  files: GenericFile[];
}

/** TagSummary — a tag with the media type and size of the manifest it points at.
 *  `count` is the layer count for an image, or the platform count for an index. */
export interface TagSummary {
  tag: string;
  digest: string;
  mediaType: string;
  kind: ManifestKind;
  size: number;
  count: number;
  pushedAt: string;
}

export interface ListTagsResponse {
  repository: string;
  tags: TagSummary[];
}

/** Layer — a config or layer blob referenced by an image manifest. */
export interface Layer {
  mediaType: string;
  digest: string;
  size: number;
}

/** IndexEntry — a child manifest referenced by a multi-platform index. */
export interface IndexEntry {
  mediaType: string;
  digest: string;
  size: number;
  platform?: string;
}

/** ManifestDetail — the full detail of one manifest (image or index). */
export interface ManifestDetail {
  digest: string;
  mediaType: string;
  kind: ManifestKind;
  totalSize: number;
  config?: Layer;
  layers: Layer[];
  manifests: IndexEntry[];
}

/** Referrer — an artifact attached to a manifest via its subject field
 *  (signature, SBOM, attestation). */
export interface Referrer {
  digest: string;
  mediaType: string;
  size: number;
  artifactType?: string;
  annotations?: Record<string, string>;
}

export interface ListReferrersResponse {
  referrers: Referrer[];
}

// --- Dashboard ---

/** DashboardSummary — coarse instance counts. */
export interface DashboardSummary {
  projects: number;
  repositories: number;
  tags: number;
}

/** ActivityEntry — one audited mutation, for the recent-activity feed. */
export interface ActivityEntry {
  actor: string;
  action: string;
  targetType: string;
  targetId: string;
  metadata?: Record<string, string>;
  projectKey?: string;
  projectName?: string;
  at: string;
}

export interface DashboardResponse {
  summary: DashboardSummary;
  activity: ActivityEntry[];
}

// --- Maintenance ---

/** GCResult — the outcome of a garbage-collection sweep. */
export interface GCResult {
  dryRun: boolean;
  scanned: number;
  deleted: number;
  reclaimedBytes: number;
  kept: number;
}
