import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { RegistryPage } from './RegistryPage';
import { api } from '../../lib/api';
import type { NpmPackage, Repository } from '../../lib/types';

vi.mock('../../lib/api', () => ({
  ApiError: class ApiError extends Error {},
  api: { listRepositories: vi.fn(), listPackages: vi.fn() },
}));

const listRepositories = vi.mocked(api.listRepositories);
const listPackages = vi.mocked(api.listPackages);

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

const pkg = (over: Partial<NpmPackage>): NpmPackage => ({
  projectKey: 'library',
  projectName: 'Library',
  name: 'left-pad',
  kind: 'local',
  versionCount: 1,
  sizeBytes: 0,
  updatedAt: '2026-07-02T10:00:00Z',
  ...over,
});

describe('RegistryPage', () => {
  it('shows the empty state when nothing has been pushed', async () => {
    listRepositories.mockResolvedValue({ repositories: [] });
    listPackages.mockResolvedValue({ packages: [] });
    renderPage();
    await waitFor(() => {
      expect(screen.getByText(/No artifacts yet/i)).toBeInTheDocument();
    });
  });

  it('lists container images and npm packages together, grouped by project', async () => {
    listRepositories.mockResolvedValue({
      repositories: [repo({ repository: 'alpine', tagCount: 3, sizeBytes: 5_242_880 })],
    });
    listPackages.mockResolvedValue({
      packages: [
        pkg({ name: 'left-pad', versionCount: 2 }),
        pkg({ name: 'is-odd', projectKey: 'platform', projectName: 'Platform', kind: 'proxy' }),
      ],
    });
    renderPage();

    // Each format links into its own detail route.
    const alpine = await screen.findByRole('link', { name: 'alpine' });
    expect(alpine).toHaveAttribute('href', '/registry/library/alpine');
    const leftPad = screen.getByRole('link', { name: 'left-pad' });
    expect(leftPad).toHaveAttribute('href', '/registry/library/-/left-pad');

    // A per-row format icon distinguishes the two formats.
    expect(screen.getByLabelText('Container image')).toBeInTheDocument();
    expect(screen.getAllByLabelText('npm package').length).toBe(2);

    // Grouped by project (project is a group header, not a per-row column).
    const table = within(screen.getByRole('table'));
    expect(table.getByText('Library')).toBeInTheDocument();
    expect(table.getByText('Platform')).toBeInTheDocument();

    // Rows carry format-appropriate "contents" and a size.
    expect(within(alpine.closest('tr') as HTMLElement).getByText('3 tags')).toBeInTheDocument();
    expect(within(alpine.closest('tr') as HTMLElement).getByText('5 MB')).toBeInTheDocument();
    expect(within(leftPad.closest('tr') as HTMLElement).getByText('2 versions')).toBeInTheDocument();

    // Local and proxy are labelled, and the count spans both formats.
    expect(screen.getAllByText('Local').length).toBeGreaterThan(0);
    expect(screen.getByText('Proxy')).toBeInTheDocument();
    expect(screen.getByText(/3 artifacts · 2 projects/)).toBeInTheDocument();
  });

  it('narrows to one format via the format dropdown', async () => {
    listRepositories.mockResolvedValue({ repositories: [repo({ repository: 'alpine' })] });
    listPackages.mockResolvedValue({ packages: [pkg({ name: 'left-pad' })] });
    renderPage();

    await screen.findByRole('link', { name: 'alpine' });
    expect(screen.getByRole('link', { name: 'left-pad' })).toBeInTheDocument();

    // Filter to container images only: the npm package drops out.
    fireEvent.change(screen.getByLabelText('Filter by format'), { target: { value: 'oci' } });
    expect(screen.getByRole('link', { name: 'alpine' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'left-pad' })).not.toBeInTheDocument();
    expect(screen.getByText(/1 of 2/)).toBeInTheDocument();

    // Filter to npm only: the image drops out.
    fireEvent.change(screen.getByLabelText('Filter by format'), { target: { value: 'npm' } });
    expect(screen.queryByRole('link', { name: 'alpine' })).not.toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'left-pad' })).toBeInTheDocument();
  });

  it('switches to a flat table that carries the project on each row', async () => {
    listRepositories.mockResolvedValue({ repositories: [repo({ repository: 'alpine' })] });
    listPackages.mockResolvedValue({ packages: [] });
    renderPage();

    await screen.findByRole('link', { name: 'alpine' });
    fireEvent.click(screen.getByRole('button', { name: 'flat' }));

    const cells = within(screen.getByRole('link', { name: 'alpine' }).closest('tr') as HTMLElement);
    expect(cells.getByText('library')).toBeInTheDocument();
    expect(screen.getByText('1 artifact')).toBeInTheDocument();
  });

  it('filters by the search box', async () => {
    listRepositories.mockResolvedValue({ repositories: [repo({ repository: 'alpine' }), repo({ repository: 'nginx' })] });
    listPackages.mockResolvedValue({ packages: [] });
    renderPage();

    await screen.findByRole('link', { name: 'alpine' });
    fireEvent.change(screen.getByLabelText('Filter artifacts'), { target: { value: 'nginx' } });

    expect(screen.queryByRole('link', { name: 'alpine' })).not.toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'nginx' })).toBeInTheDocument();
    expect(screen.getByText(/1 of 2/)).toBeInTheDocument();
  });

  it('shows an error state when loading fails', async () => {
    listRepositories.mockRejectedValue(new Error('network down'));
    listPackages.mockResolvedValue({ packages: [] });
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('network down')).toBeInTheDocument();
    });
    expect(screen.getByRole('button', { name: /retry/i })).toBeInTheDocument();
  });
});
