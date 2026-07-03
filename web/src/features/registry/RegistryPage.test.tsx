import { render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { RegistryPage } from './RegistryPage';
import { api } from '../../lib/api';
import type { Repository } from '../../lib/types';

vi.mock('../../lib/api', () => ({
  ApiError: class ApiError extends Error {},
  api: { listRepositories: vi.fn() },
}));

const listRepositories = vi.mocked(api.listRepositories);

function renderPage() {
  return render(
    <MemoryRouter>
      <RegistryPage />
    </MemoryRouter>,
  );
}

afterEach(() => {
  vi.clearAllMocks();
});

const repo = (over: Partial<Repository>): Repository => ({
  projectKey: 'library',
  projectName: 'Library',
  repository: 'alpine',
  tagCount: 1,
  manifestCount: 1,
  sizeBytes: 0,
  updatedAt: '2026-07-02T10:00:00Z',
  ...over,
});

describe('RegistryPage', () => {
  it('shows the empty state when nothing has been pushed', async () => {
    listRepositories.mockResolvedValue({ repositories: [] });
    renderPage();
    await waitFor(() => {
      expect(screen.getByText(/No artifacts yet/i)).toBeInTheDocument();
    });
  });

  it('groups repositories under their project', async () => {
    listRepositories.mockResolvedValue({
      repositories: [
        repo({ projectKey: 'library', projectName: 'Library', repository: 'alpine', tagCount: 3, sizeBytes: 5_242_880 }),
        repo({ projectKey: 'library', projectName: 'Library', repository: 'nginx', tagCount: 5 }),
        repo({ projectKey: 'platform', projectName: 'Platform', repository: 'api-gw', tagCount: 2 }),
      ],
    });
    renderPage();

    // Two project group headers.
    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'Library' })).toBeInTheDocument();
    });
    expect(screen.getByRole('heading', { name: 'Platform' })).toBeInTheDocument();

    // Repositories link into their detail route.
    const alpine = screen.getByRole('link', { name: /alpine/i });
    expect(alpine).toHaveAttribute('href', '/registry/library/alpine');
    expect(within(alpine).getByText(/3 tags/i)).toBeInTheDocument();
    expect(within(alpine).getByText('5 MB')).toBeInTheDocument();
  });

  it('shows an error state when loading fails', async () => {
    listRepositories.mockRejectedValue(new Error('network down'));
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('network down')).toBeInTheDocument();
    });
    expect(screen.getByRole('button', { name: /retry/i })).toBeInTheDocument();
  });
});
