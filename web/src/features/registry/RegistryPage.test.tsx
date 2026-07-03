import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { RegistryPage } from './RegistryPage';
import { api } from '../../lib/api';
import type { GenericFile, NpmPackage, NugetPackage, Repository } from '../../lib/types';

vi.mock('../../lib/api', () => ({
  ApiError: class ApiError extends Error {},
  api: {
    listRepositories: vi.fn(),
    listPackages: vi.fn(),
    listNugets: vi.fn(),
    listPypis: vi.fn(),
    listGenericFiles: vi.fn(),
  },
}));

const listRepositories = vi.mocked(api.listRepositories);
const listPackages = vi.mocked(api.listPackages);
const listNugets = vi.mocked(api.listNugets);
const listPypis = vi.mocked(api.listPypis);
const listGenericFiles = vi.mocked(api.listGenericFiles);

function renderPage() {
  return render(
    <MemoryRouter>
      <RegistryPage />
    </MemoryRouter>,
  );
}

// Every format is fetched now; default each to empty and let a test override the
// ones it exercises.
beforeEach(() => {
  listRepositories.mockResolvedValue({ repositories: [] });
  listPackages.mockResolvedValue({ packages: [] });
  listNugets.mockResolvedValue({ packages: [] });
  listPypis.mockResolvedValue({ packages: [] });
  listGenericFiles.mockResolvedValue({ files: [] });
});

afterEach(() => {
  vi.clearAllMocks();
});

const repo = (over: Partial<Repository>): Repository => ({
  projectKey: 'library',
  projectName: 'Library',
  repoKey: 'images',
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
  repoKey: 'npm',
  name: 'left-pad',
  kind: 'local',
  versionCount: 1,
  sizeBytes: 0,
  updatedAt: '2026-07-02T10:00:00Z',
  ...over,
});

const nug = (over: Partial<NugetPackage>): NugetPackage => ({
  projectKey: 'library',
  projectName: 'Library',
  repoKey: 'nuget',
  id: 'Newtonsoft.Json',
  kind: 'local',
  versionCount: 1,
  sizeBytes: 0,
  updatedAt: '2026-07-02T10:00:00Z',
  ...over,
});

const gen = (over: Partial<GenericFile>): GenericFile => ({
  projectKey: 'library',
  projectName: 'Library',
  repoKey: 'files',
  path: 'installers/tool-1.0.0.bin',
  kind: 'local',
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
    expect(alpine).toHaveAttribute('href', '/registry/library/images/alpine');
    const leftPad = screen.getByRole('link', { name: 'left-pad' });
    expect(leftPad).toHaveAttribute('href', '/registry/library/-/npm/left-pad');

    // A per-row format icon distinguishes the two formats.
    expect(screen.getByLabelText('Container image')).toBeInTheDocument();
    expect(screen.getAllByLabelText('npm package').length).toBe(2);

    // Grouped by project (project is a group header, not a per-row column) and
    // sub-grouped by the repository each artifact lives in.
    const table = within(screen.getByRole('table'));
    expect(table.getByText('Library')).toBeInTheDocument();
    expect(table.getByText('Platform')).toBeInTheDocument();
    expect(table.getByText('images')).toBeInTheDocument(); // repo sub-header
    expect(table.getAllByText('npm').length).toBeGreaterThan(0); // repo sub-header

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

  it('lists NuGet packages and generic files with their own icons', async () => {
    listNugets.mockResolvedValue({ packages: [nug({ id: 'Newtonsoft.Json', versionCount: 3 })] });
    listGenericFiles.mockResolvedValue({ files: [gen({ path: 'installers/tool-1.0.0.bin', sizeBytes: 1024 })] });
    renderPage();

    // The NuGet package links into its detail route (the "-nuget-" sentinel).
    const nuget = await screen.findByRole('link', { name: 'Newtonsoft.Json' });
    expect(nuget).toHaveAttribute('href', '/registry/library/-nuget-/nuget/Newtonsoft.Json');
    expect(screen.getByLabelText('NuGet package')).toBeInTheDocument();
    expect(within(nuget.closest('tr') as HTMLElement).getByText('3 versions')).toBeInTheDocument();

    // The generic file shows its path but is display-only (no detail page).
    expect(screen.getByText('installers/tool-1.0.0.bin')).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'installers/tool-1.0.0.bin' })).not.toBeInTheDocument();
    expect(screen.getByLabelText('Generic file')).toBeInTheDocument();
  });

  it('narrows to NuGet and to generic via the format dropdown', async () => {
    listRepositories.mockResolvedValue({ repositories: [repo({ repository: 'alpine' })] });
    listNugets.mockResolvedValue({ packages: [nug({ id: 'Newtonsoft.Json' })] });
    listGenericFiles.mockResolvedValue({ files: [gen({ path: 'blob.bin' })] });
    renderPage();

    await screen.findByRole('link', { name: 'alpine' });

    fireEvent.change(screen.getByLabelText('Filter by format'), { target: { value: 'nuget' } });
    expect(screen.getByRole('link', { name: 'Newtonsoft.Json' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'alpine' })).not.toBeInTheDocument();
    expect(screen.queryByText('blob.bin')).not.toBeInTheDocument();

    fireEvent.change(screen.getByLabelText('Filter by format'), { target: { value: 'generic' } });
    expect(screen.getByText('blob.bin')).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Newtonsoft.Json' })).not.toBeInTheDocument();
  });

  it('switches to a flat table that carries the project on each row', async () => {
    listRepositories.mockResolvedValue({ repositories: [repo({ repository: 'alpine' })] });
    listPackages.mockResolvedValue({ packages: [] });
    renderPage();

    await screen.findByRole('link', { name: 'alpine' });
    fireEvent.click(screen.getByRole('button', { name: 'flat' }));

    const cells = within(screen.getByRole('link', { name: 'alpine' }).closest('tr') as HTMLElement);
    expect(cells.getByText('library/images')).toBeInTheDocument();
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
