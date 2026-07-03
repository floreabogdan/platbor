import type {
  CreateProjectRequest,
  CreateRepoRequest,
  CreateTokenRequest,
  CreateTokenResponse,
  DashboardResponse,
  GCResult,
  ListCargoCratesResponse,
  ListGenericFilesResponse,
  ListGoModulesResponse,
  ListMavensResponse,
  ListRubyGemsResponse,
  ListMembersResponse,
  ListNugetsResponse,
  ListPackagesResponse,
  ListProjectsResponse,
  ListReferrersResponse,
  ListReposResponse,
  ListRepositoriesResponse,
  ListTagsResponse,
  ListPypisResponse,
  ManifestDetail,
  Member,
  MemberRole,
  CargoCrateDetail,
  GoModuleDetail,
  MavenArtifactDetail,
  NpmPackageDetail,
  RubyGemDetail,
  NugetPackageDetail,
  Problem,
  Project,
  PyPIPackageDetail,
  Repo,
  Token,
  UpdateRepoRequest,
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

  // Repositories (typed containers inside a project)
  listRepos: (project: string): Promise<ListReposResponse> =>
    request<ListReposResponse>(`/projects/${encodeURIComponent(project)}/repositories`),

  createRepo: (project: string, body: CreateRepoRequest): Promise<Repo> =>
    request<Repo>(`/projects/${encodeURIComponent(project)}/repositories`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  getRepo: (project: string, repo: string): Promise<Repo> =>
    request<Repo>(`/projects/${encodeURIComponent(project)}/repositories/${encodeURIComponent(repo)}`),

  updateRepo: (project: string, repo: string, body: UpdateRepoRequest): Promise<Repo> =>
    request<Repo>(`/projects/${encodeURIComponent(project)}/repositories/${encodeURIComponent(repo)}`, {
      method: 'PUT',
      body: JSON.stringify(body),
    }),

  deleteRepo: (project: string, repo: string): Promise<void> =>
    request<undefined>(`/projects/${encodeURIComponent(project)}/repositories/${encodeURIComponent(repo)}`, {
      method: 'DELETE',
    }),

  // Members (project RBAC: reader | maintainer | admin)
  listMembers: (project: string): Promise<ListMembersResponse> =>
    request<ListMembersResponse>(`/projects/${encodeURIComponent(project)}/members`),

  setMember: (project: string, username: string, role: MemberRole): Promise<Member> =>
    request<Member>(`/projects/${encodeURIComponent(project)}/members/${encodeURIComponent(username)}`, {
      method: 'PUT',
      body: JSON.stringify({ role }),
    }),

  removeMember: (project: string, username: string): Promise<void> =>
    request<undefined>(`/projects/${encodeURIComponent(project)}/members/${encodeURIComponent(username)}`, {
      method: 'DELETE',
    }),

  // Tokens
  listTokens: (): Promise<{ tokens: Token[] }> => request<{ tokens: Token[] }>(`/tokens`),

  createToken: (body: CreateTokenRequest): Promise<CreateTokenResponse> =>
    request<CreateTokenResponse>(`/tokens`, { method: 'POST', body: JSON.stringify(body) }),

  deleteToken: (id: string): Promise<void> =>
    request<undefined>(`/tokens/${id}`, { method: 'DELETE' }),

  // Registry browser (read-only view of what was pushed over /v2)
  listRepositories: (): Promise<ListRepositoriesResponse> =>
    request<ListRepositoriesResponse>(`/registry/repositories`),

  // Repositories are keyed by (project, repo, image); the OCI image name is the
  // artifact within the typed repository.
  listRepoTags: (project: string, repo: string, image: string): Promise<ListTagsResponse> =>
    request<ListTagsResponse>(`/registry/${encodeURIComponent(project)}/tags${query({ repo, image })}`),

  getManifest: (project: string, repo: string, image: string, reference: string): Promise<ManifestDetail> =>
    request<ManifestDetail>(
      `/registry/${encodeURIComponent(project)}/manifests${query({ repo, image, reference })}`,
    ),

  // Delete a tag (reference is a tag) or a whole manifest and its tags
  // (reference is a digest).
  deleteManifest: (project: string, repo: string, image: string, reference: string): Promise<void> =>
    request<undefined>(
      `/registry/${encodeURIComponent(project)}/manifests${query({ repo, image, reference })}`,
      { method: 'DELETE' },
    ),

  listReferrers: (project: string, repo: string, image: string, subject: string): Promise<ListReferrersResponse> =>
    request<ListReferrersResponse>(
      `/registry/${encodeURIComponent(project)}/referrers${query({ repo, image, subject })}`,
    ),

  // npm package browser
  listPackages: (): Promise<ListPackagesResponse> =>
    request<ListPackagesResponse>(`/registry/packages`),

  getPackage: (project: string, repo: string, name: string): Promise<NpmPackageDetail> =>
    request<NpmPackageDetail>(
      `/registry/${encodeURIComponent(project)}/package${query({ repo, name })}`,
    ),

  // NuGet package browser
  listNugets: (): Promise<ListNugetsResponse> =>
    request<ListNugetsResponse>(`/registry/nuget-packages`),

  getNugetPackage: (project: string, repo: string, id: string): Promise<NugetPackageDetail> =>
    request<NugetPackageDetail>(
      `/registry/${encodeURIComponent(project)}/nuget-package${query({ repo, id })}`,
    ),

  // PyPI package browser
  listPypis: (): Promise<ListPypisResponse> => request<ListPypisResponse>(`/registry/pypi-packages`),

  getPypiPackage: (project: string, repo: string, name: string): Promise<PyPIPackageDetail> =>
    request<PyPIPackageDetail>(
      `/registry/${encodeURIComponent(project)}/pypi-package${query({ repo, name })}`,
    ),

  // Maven artifact browser
  listMavens: (): Promise<ListMavensResponse> => request<ListMavensResponse>(`/registry/maven-artifacts`),

  getMavenArtifact: (project: string, repo: string, group: string, artifact: string): Promise<MavenArtifactDetail> =>
    request<MavenArtifactDetail>(
      `/registry/${encodeURIComponent(project)}/maven-artifact${query({ repo, group, artifact })}`,
    ),

  // Go module browser
  listGoModules: (): Promise<ListGoModulesResponse> => request<ListGoModulesResponse>(`/registry/go-modules`),

  getGoModule: (project: string, repo: string, module: string): Promise<GoModuleDetail> =>
    request<GoModuleDetail>(
      `/registry/${encodeURIComponent(project)}/go-module${query({ repo, module })}`,
    ),

  // Cargo crate browser
  listCargoCrates: (): Promise<ListCargoCratesResponse> => request<ListCargoCratesResponse>(`/registry/cargo-crates`),

  getCargoCrate: (project: string, repo: string, name: string): Promise<CargoCrateDetail> =>
    request<CargoCrateDetail>(
      `/registry/${encodeURIComponent(project)}/cargo-crate${query({ repo, name })}`,
    ),

  // RubyGems gem browser
  listRubyGems: (): Promise<ListRubyGemsResponse> => request<ListRubyGemsResponse>(`/registry/rubygems`),

  getRubyGem: (project: string, repo: string, name: string): Promise<RubyGemDetail> =>
    request<RubyGemDetail>(
      `/registry/${encodeURIComponent(project)}/rubygem${query({ repo, name })}`,
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
