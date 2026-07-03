// API types mirror the Go /api/v1 shapes (camelCase, RFC 3339 timestamps).
// One source of truth per shape — keep in sync with internal/httpapi.

export interface User {
  id: string;
  username: string;
  email: string;
  isAdmin: boolean;
  createdAt: string;
}

export interface ProjectUpstream {
  url: string;
  username?: string;
}

export interface Project {
  id: string;
  key: string;
  name: string;
  description: string;
  kind: 'local' | 'proxy';
  upstream?: ProjectUpstream;
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
  upstream?: {
    url: string;
    username?: string;
    password?: string;
  };
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
  repository: string;
  kind: 'local' | 'proxy'; // proxy = the project mirrors an upstream registry
  tagCount: number;
  manifestCount: number;
  sizeBytes: number;
  updatedAt: string;
}

export interface ListRepositoriesResponse {
  repositories: Repository[];
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
