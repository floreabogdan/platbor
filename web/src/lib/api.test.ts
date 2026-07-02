import { afterEach, describe, expect, it, vi } from 'vitest';
import { api, ApiError } from './api';

function mockFetch(status: number, body: unknown, contentType = 'application/json') {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify(body), {
        status,
        headers: { 'Content-Type': contentType },
      }),
    ),
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('api client', () => {
  it('returns the parsed body on success', async () => {
    mockFetch(200, { projects: [{ id: 'proj_1', key: 'acme' }], nextCursor: '' });
    const res = await api.listProjects();
    expect(res.projects[0]?.key).toBe('acme');
  });

  it('throws ApiError carrying the problem+json title and detail', async () => {
    mockFetch(409, { type: 'about:blank', title: 'Project key already exists', status: 409, detail: 'choose a different key' });
    await expect(api.createProject({ key: 'dup', name: 'x' })).rejects.toMatchObject({
      name: 'ApiError',
      status: 409,
      title: 'Project key already exists',
      message: 'choose a different key',
    });
  });

  it('falls back to statusText when the error body is not JSON', async () => {
    mockFetch(500, 'oops', 'text/plain');
    const err = await api.listProjects().catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).status).toBe(500);
  });
});
