import type {
  CreateProjectRequest,
  CreateTokenRequest,
  CreateTokenResponse,
  DashboardResponse,
  GCResult,
  ListGenericFilesResponse,
  ListNugetsResponse,
  ListPackagesResponse,
  ListProjectsResponse,
  ListReferrersResponse,
  ListRepositoriesResponse,
  ListTagsResponse,
  ManifestDetail,
  NpmPackageDetail,
  NugetPackageDetail,
  Problem,
  Project,
  Token,
  User,
} from './types';

const BASE = '/api/v1';

/** ApiError carries the RFC 7807 fields so callers can show a useful message. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly title: string,
    readonly detail?: string,
  ) {
    super(detail && detail.length > 0 ? detail : title);
    this.name = 'ApiError';
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers);
  if (!headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json');
  }

  const res = await fetch(BASE + path, { ...init, headers });

  if (!res.ok) {
    throw await toApiError(res);
  }
  if (res.status === 204) {
    return undefined as T;
  }
  return (await res.json()) as T;
}

async function toApiError(res: Response): Promise<ApiError> {
  try {
    const problem = (await res.json()) as Partial<Problem>;
    return new ApiError(res.status, problem.title ?? res.statusText, problem.detail);
  } catch {
    return new ApiError(res.status, res.statusText);
  }
}

function query(params: Record<string, string | number | undefined>): string {
  const pairs = Object.entries(params).filter(([, v]) => v !== undefined && v !== '');
  if (pairs.length === 0) {
    return '';
  }
  const search = new URLSearchParams(pairs.map(([k, v]) => [k, String(v)]));
  return `?${search.toString()}`;
}

export const api = {
  // Auth
  me: (): Promise<User> => request<User>(`/auth/me`),

  login: (username: string, password: string): Promise<User> =>
    request<User>(`/auth/login`, { method: 'POST', body: JSON.stringify({ username, password }) }),

  logout: (): Promise<void> => request<undefined>(`/auth/logout`, { method: 'POST' }),

  // Projects
  listProjects: (params: { cursor?: string; limit?: number } = {}): Promise<ListProjectsResponse> =>
    request<ListProjectsResponse>(`/projects${query(params)}`),

  createProject: (body: CreateProjectRequest): Promise<Project> =>
    request<Project>(`/projects`, { method: 'POST', body: JSON.stringify(body) }),

  // Tokens
  listTokens: (): Promise<{ tokens: Token[] }> => request<{ tokens: Token[] }>(`/tokens`),

  createToken: (body: CreateTokenRequest): Promise<CreateTokenResponse> =>
    request<CreateTokenResponse>(`/tokens`, { method: 'POST', body: JSON.stringify(body) }),

  deleteToken: (id: string): Promise<void> =>
    request<undefined>(`/tokens/${id}`, { method: 'DELETE' }),

  // Registry browser (read-only view of what was pushed over /v2)
  listRepositories: (): Promise<ListRepositoriesResponse> =>
    request<ListRepositoriesResponse>(`/registry/repositories`),

  listRepoTags: (project: string, repository: string): Promise<ListTagsResponse> =>
    request<ListTagsResponse>(`/registry/${encodeURIComponent(project)}/tags${query({ repository })}`),

  getManifest: (project: string, repository: string, reference: string): Promise<ManifestDetail> =>
    request<ManifestDetail>(
      `/registry/${encodeURIComponent(project)}/manifests${query({ repository, reference })}`,
    ),

  // Delete a tag (reference is a tag) or a whole manifest and its tags
  // (reference is a digest).
  deleteManifest: (project: string, repository: string, reference: string): Promise<void> =>
    request<undefined>(
      `/registry/${encodeURIComponent(project)}/manifests${query({ repository, reference })}`,
      { method: 'DELETE' },
    ),

  listReferrers: (project: string, repository: string, subject: string): Promise<ListReferrersResponse> =>
    request<ListReferrersResponse>(
      `/registry/${encodeURIComponent(project)}/referrers${query({ repository, subject })}`,
    ),

  // npm package browser
  listPackages: (): Promise<ListPackagesResponse> =>
    request<ListPackagesResponse>(`/registry/packages`),

  getPackage: (project: string, name: string): Promise<NpmPackageDetail> =>
    request<NpmPackageDetail>(
      `/registry/${encodeURIComponent(project)}/package${query({ name })}`,
    ),

  // NuGet package browser
  listNugets: (): Promise<ListNugetsResponse> =>
    request<ListNugetsResponse>(`/registry/nuget-packages`),

  getNugetPackage: (project: string, id: string): Promise<NugetPackageDetail> =>
    request<NugetPackageDetail>(
      `/registry/${encodeURIComponent(project)}/nuget-package${query({ id })}`,
    ),

  // Generic file browser
  listGenericFiles: (): Promise<ListGenericFilesResponse> =>
    request<ListGenericFilesResponse>(`/registry/generic-files`),

  // Dashboard
  getDashboard: (): Promise<DashboardResponse> => request<DashboardResponse>(`/dashboard`),

  // Maintenance (instance admin)
  runGarbageCollection: (dryRun: boolean): Promise<GCResult> =>
    request<GCResult>(`/registry/gc?dryRun=${String(dryRun)}`, { method: 'POST' }),
};
