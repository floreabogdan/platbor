import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ProjectPage } from './ProjectPage';
import { api } from '../../lib/api';
import { useAuth } from '../../lib/auth';
import type { Repo } from '../../lib/types';

vi.mock('../../lib/api', () => ({
  ApiError: class ApiError extends Error {},
  api: { listProjects: vi.fn(), listRepos: vi.fn(), createRepo: vi.fn(), updateRepo: vi.fn(), deleteRepo: vi.fn() },
}));
vi.mock('../../lib/auth', () => ({ useAuth: vi.fn() }));

const listProjects = vi.mocked(api.listProjects);
const listRepos = vi.mocked(api.listRepos);
const mockUseAuth = vi.mocked(useAuth);

function asAdmin(isAdmin: boolean) {
  mockUseAuth.mockReturnValue({
    state: {
      status: 'authenticated',
      user: { id: 'u1', username: 'admin', email: '', isAdmin, createdAt: '' },
    },
    login: vi.fn(),
    logout: vi.fn(),
  });
}

const repo = (over: Partial<Repo>): Repo => ({
  key: 'docker-prod',
  name: 'Docker Prod',
  format: 'oci',
  mode: 'local',
  retention: { keepLast: 10, deleteUntagged: true },
  createdAt: '2026-07-02T10:00:00Z',
  updatedAt: '2026-07-02T10:00:00Z',
  ...over,
});

function renderAt() {
  return render(
    <MemoryRouter initialEntries={['/projects/acme']}>
      <Routes>
        <Route path="projects/:key" element={<ProjectPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  listProjects.mockResolvedValue({
    projects: [
      { id: 'p1', key: 'acme', name: 'Acme', description: '', allowAutoCreate: true, quotaBytes: 0, verificationKeyConfigured: false, createdAt: '', updatedAt: '' },
    ],
  });
});

afterEach(() => {
  vi.clearAllMocks();
});

describe('ProjectPage', () => {
  it('lists a project\'s repositories with their format, mode, and retention', async () => {
    asAdmin(true);
    listRepos.mockResolvedValue({
      repositories: [
        repo({ key: 'docker-prod', format: 'oci', mode: 'local', retention: { keepLast: 10, deleteUntagged: false } }),
        repo({ key: 'npmjs', format: 'npm', mode: 'proxy', upstream: { url: 'https://registry.npmjs.org' }, retention: { keepLast: 0, deleteUntagged: false } }),
      ],
    });
    renderAt();

    expect(await screen.findByText('docker-prod')).toBeInTheDocument();
    expect(screen.getByText('npmjs')).toBeInTheDocument();
    expect(screen.getByText('keep last 10')).toBeInTheDocument();
    expect(screen.getByText('Proxy')).toBeInTheDocument();
    expect(screen.getByText('Local')).toBeInTheDocument();
  });

  it('lets an admin create a repository', async () => {
    asAdmin(true);
    listRepos.mockResolvedValue({ repositories: [] });
    renderAt();

    await waitFor(() => {
      expect(screen.getAllByRole('button', { name: 'New repository' }).length).toBeGreaterThan(0);
    });
  });

  it('hides create/edit controls from non-admins', async () => {
    asAdmin(false);
    listRepos.mockResolvedValue({ repositories: [repo({})] });
    renderAt();

    expect(await screen.findByText('docker-prod')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'New repository' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Edit' })).not.toBeInTheDocument();
  });
});
