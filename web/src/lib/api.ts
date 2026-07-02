import type { CreateProjectRequest, ListProjectsResponse, Problem, Project, User } from './types';

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
};
