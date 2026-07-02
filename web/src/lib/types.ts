// API types mirror the Go /api/v1 shapes (camelCase, RFC 3339 timestamps).
// One source of truth per shape — keep in sync with internal/httpapi.

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

/** RFC 7807 problem+json — the single error shape from the API. */
export interface Problem {
  type: string;
  title: string;
  status: number;
  detail?: string;
}
