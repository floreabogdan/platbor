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
