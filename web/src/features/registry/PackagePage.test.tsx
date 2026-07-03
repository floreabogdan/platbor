import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { PackagePage } from './PackagePage';
import { api } from '../../lib/api';
import type { NpmPackageDetail } from '../../lib/types';

vi.mock('../../lib/api', () => ({
  ApiError: class ApiError extends Error {},
  api: { getPackage: vi.fn() },
}));

const getPackage = vi.mocked(api.getPackage);

afterEach(() => {
  vi.clearAllMocks();
});

// renderAt mounts the package route so useParams resolves the project and the
// scoped-or-plain name splat exactly as the app router does.
function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="registry/:project/-/*" element={<PackagePage />} />
      </Routes>
    </MemoryRouter>,
  );
}

const detail = (over: Partial<NpmPackageDetail>): NpmPackageDetail => ({
  name: 'left-pad',
  distTags: { latest: '1.1.0', beta: '1.0.0' },
  versions: [
    { version: '1.1.0', sizeBytes: 5_242_880, shasum: 'aaa', integrity: 'sha512-x', publishedAt: '2026-07-02T10:00:00Z' },
    { version: '1.0.0', sizeBytes: 1024, shasum: 'bbb', integrity: 'sha512-y', publishedAt: '2026-07-01T10:00:00Z' },
  ],
  ...over,
});

describe('PackagePage', () => {
  it('renders versions, dist-tags, and the install snippet', async () => {
    getPackage.mockResolvedValue(detail({}));
    renderAt('/registry/library/-/npm/left-pad');

    // Requests the right (project, repo, name) triple.
    await waitFor(() => {
      expect(getPackage).toHaveBeenCalledWith('library', 'npm', 'left-pad');
    });

    // Install snippet: repository-registry config + install command.
    expect(await screen.findByText('npm install left-pad')).toBeInTheDocument();
    expect(screen.getByText(/npm config set registry .*\/npm\/library\/npm\//)).toBeInTheDocument();

    // Versions with their sizes. Version numbers can appear twice (a version row
    // plus a dist-tag value), so tolerate duplicates.
    expect(screen.getAllByText('1.1.0').length).toBeGreaterThan(0);
    expect(screen.getAllByText('1.0.0').length).toBeGreaterThan(0);
    expect(screen.getByText('5 MB')).toBeInTheDocument();

    // Dist-tags are shown (latest appears both as a chip and on its version row).
    expect(screen.getAllByText('latest').length).toBeGreaterThan(0);
    expect(screen.getAllByText('beta').length).toBeGreaterThan(0);
  });

  it('configures a scoped registry for scoped packages', async () => {
    getPackage.mockResolvedValue(
      detail({ name: '@acme/widgets', versions: [{ version: '2.0.0', sizeBytes: 2048, shasum: 'c', integrity: 'sha512-z', publishedAt: '2026-07-02T10:00:00Z' }], distTags: { latest: '2.0.0' } }),
    );
    renderAt('/registry/library/-/npm/@acme/widgets');

    await waitFor(() => {
      expect(getPackage).toHaveBeenCalledWith('library', 'npm', '@acme/widgets');
    });
    expect(screen.getByText(/npm config set @acme:registry .*\/npm\/library\/npm\//)).toBeInTheDocument();
    expect(screen.getByText('npm install @acme/widgets')).toBeInTheDocument();
  });

  it('shows an error state when loading fails', async () => {
    getPackage.mockRejectedValue(new Error('package boom'));
    renderAt('/registry/library/-/npm/left-pad');
    await waitFor(() => {
      expect(screen.getByText('package boom')).toBeInTheDocument();
    });
  });
});
