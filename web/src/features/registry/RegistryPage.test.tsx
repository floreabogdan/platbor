import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
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
  kind: 'local',
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

  it('groups repositories by project (the default view) with tags and size', async () => {
    listRepositories.mockResolvedValue({
      repositories: [
        repo({ projectKey: 'library', projectName: 'Library', repository: 'alpine', tagCount: 3, sizeBytes: 5_242_880 }),
        repo({ projectKey: 'library', projectName: 'Library', repository: 'nginx', tagCount: 5 }),
        repo({ projectKey: 'platform', projectName: 'Platform', repository: 'api-gw', tagCount: 2 }),
      ],
    });
    renderPage();

    // The repository name links into its detail route.
    const alpine = await screen.findByRole('link', { name: 'alpine' });
    expect(alpine).toHaveAttribute('href', '/registry/library/alpine');

    // The project is a group header, not a per-row column (the names in the
    // filter dropdown live outside the table, so scope the assertion to it).
    const table = within(screen.getByRole('table'));
    expect(table.getByText('Library')).toBeInTheDocument();
    expect(table.getByText('Platform')).toBeInTheDocument();

    // The repository row carries the tag count and size.
    const cells = within(alpine.closest('tr') as HTMLElement);
    expect(cells.getByText('3')).toBeInTheDocument();
    expect(cells.getByText('5 MB')).toBeInTheDocument();

    // The count reflects repositories and how many projects they span.
    expect(screen.getByText(/3 repositories · 2 projects/)).toBeInTheDocument();
  });

  it('switches to a flat table that carries the project on each row', async () => {
    listRepositories.mockResolvedValue({
      repositories: [repo({ repository: 'alpine', projectKey: 'library', projectName: 'Library' })],
    });
    renderPage();

    await screen.findByRole('link', { name: 'alpine' });
    fireEvent.click(screen.getByRole('button', { name: 'flat' }));

    // In the flat view the project moves onto the row as a chip.
    const cells = within(screen.getByRole('link', { name: 'alpine' }).closest('tr') as HTMLElement);
    expect(cells.getByText('library')).toBeInTheDocument();
    // The grouped-only project count disappears from the toolbar.
    expect(screen.getByText('1 repository')).toBeInTheDocument();
  });

  it('labels local and proxy repositories', async () => {
    listRepositories.mockResolvedValue({
      repositories: [
        repo({ repository: 'alpine', kind: 'local' }),
        repo({ projectKey: 'dockerhub', projectName: 'Docker Hub', repository: 'library/nginx', kind: 'proxy' }),
      ],
    });
    renderPage();

    await screen.findByRole('link', { name: 'alpine' });
    expect(screen.getByText('Local')).toBeInTheDocument();
    expect(screen.getByText('Proxy')).toBeInTheDocument();
  });

  it('filters the table by the search box', async () => {
    listRepositories.mockResolvedValue({
      repositories: [
        repo({ repository: 'alpine' }),
        repo({ repository: 'nginx' }),
      ],
    });
    renderPage();

    await screen.findByRole('link', { name: 'alpine' });
    fireEvent.change(screen.getByLabelText('Filter repositories'), { target: { value: 'nginx' } });

    expect(screen.queryByRole('link', { name: 'alpine' })).not.toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'nginx' })).toBeInTheDocument();
    expect(screen.getByText(/1 of 2/)).toBeInTheDocument();
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
