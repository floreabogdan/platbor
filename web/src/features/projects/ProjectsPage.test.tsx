import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ProjectsPage } from './ProjectsPage';
import { api } from '../../lib/api';

vi.mock('../../lib/api', () => ({
  ApiError: class ApiError extends Error {},
  api: { listProjects: vi.fn(), createProject: vi.fn() },
}));

const listProjects = vi.mocked(api.listProjects);

function renderPage() {
  return render(
    <MemoryRouter>
      <ProjectsPage />
    </MemoryRouter>,
  );
}

afterEach(() => {
  vi.clearAllMocks();
});

describe('ProjectsPage', () => {
  it('shows the empty state when there are no projects', async () => {
    listProjects.mockResolvedValue({ projects: [] });
    renderPage();
    await waitFor(() => {
      expect(screen.getByText(/No projects yet/i)).toBeInTheDocument();
    });
  });

  it('renders projects returned by the API', async () => {
    listProjects.mockResolvedValue({
      projects: [
        { id: 'proj_1', key: 'acme', name: 'Acme Corp', description: '', kind: 'local', createdAt: '2026-07-02T10:00:00Z', updatedAt: '2026-07-02T10:00:00Z' },
      ],
    });
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Acme Corp')).toBeInTheDocument();
    });
    expect(screen.getByText('acme')).toBeInTheDocument();
  });

  it('badges a proxy project and shows its upstream', async () => {
    listProjects.mockResolvedValue({
      projects: [
        {
          id: 'proj_2',
          key: 'dockerhub',
          name: 'Docker Hub Mirror',
          description: '',
          kind: 'proxy',
          upstream: { url: 'https://registry-1.docker.io' },
          createdAt: '2026-07-02T10:00:00Z',
          updatedAt: '2026-07-02T10:00:00Z',
        },
      ],
    });
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Docker Hub Mirror')).toBeInTheDocument();
    });
    expect(screen.getByText('Proxy')).toBeInTheDocument();
    expect(screen.getByText(/registry-1\.docker\.io/)).toBeInTheDocument();
  });

  it('shows an error state and a retry when loading fails', async () => {
    listProjects.mockRejectedValue(new Error('network down'));
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('network down')).toBeInTheDocument();
    });
    expect(screen.getByRole('button', { name: /retry/i })).toBeInTheDocument();
  });
});
