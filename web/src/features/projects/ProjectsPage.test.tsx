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

  it('renders projects returned by the API, linking to their detail page', async () => {
    listProjects.mockResolvedValue({
      projects: [
        {
          id: 'proj_1',
          key: 'acme',
          name: 'Acme Corp',
          description: '',
          allowAutoCreate: true,
          quotaBytes: 0,
          createdAt: '2026-07-02T10:00:00Z',
          updatedAt: '2026-07-02T10:00:00Z',
        },
      ],
    });
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Acme Corp')).toBeInTheDocument();
    });
    expect(screen.getByText('acme')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /Acme Corp/ })).toHaveAttribute('href', '/projects/acme');
  });

  it('notes when a project requires repositories to be pre-created', async () => {
    listProjects.mockResolvedValue({
      projects: [
        {
          id: 'proj_2',
          key: 'governed',
          name: 'Governed',
          description: '',
          allowAutoCreate: false,
          quotaBytes: 0,
          createdAt: '2026-07-02T10:00:00Z',
          updatedAt: '2026-07-02T10:00:00Z',
        },
      ],
    });
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Governed')).toBeInTheDocument();
    });
    expect(screen.getByText(/must be created before pushing/i)).toBeInTheDocument();
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
