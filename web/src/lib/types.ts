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

export type RepoFormat = 'oci' | 'npm' | 'nuget' | 'generic';
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

/** A manifest is either a single-platform image or a multi-platform index. */
export type ManifestKind = 'image' | 'index';

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
