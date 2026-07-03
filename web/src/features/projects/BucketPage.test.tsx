import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { BucketPage } from './BucketPage';
import { api } from '../../lib/api';
import { useAuth } from '../../lib/auth';
import type { GenericFile } from '../../lib/types';

vi.mock('../../lib/api', () => ({
  ApiError: class ApiError extends Error {},
  api: { listGenericFiles: vi.fn() },
}));
vi.mock('../../lib/auth', () => ({ useAuth: vi.fn() }));

const listGenericFiles = vi.mocked(api.listGenericFiles);
const mockUseAuth = vi.mocked(useAuth);

function authed() {
  mockUseAuth.mockReturnValue({
    state: { status: 'authenticated', user: { id: 'u1', username: 'admin', email: '', isAdmin: true, createdAt: '' } },
    login: vi.fn(),
    logout: vi.fn(),
  });
}

const file = (over: Partial<GenericFile>): GenericFile => ({
  projectKey: 'acme',
  projectName: 'Acme',
  repoKey: 'drop',
  path: 'tools/setup.exe',
  kind: 'local',
  sizeBytes: 2048,
  updatedAt: '2026-07-02T10:00:00Z',
  ...over,
});

function renderBucket() {
  return render(
    <MemoryRouter initialEntries={['/projects/acme/buckets/drop']}>
      <Routes>
        <Route path="projects/:key/buckets/:repo" element={<BucketPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

afterEach(() => vi.clearAllMocks());

describe('BucketPage', () => {
  it('lists this bucket’s files with download links and an upload control', async () => {
    authed();
    listGenericFiles.mockResolvedValue({
      files: [
        file({ path: 'tools/setup.exe', sizeBytes: 2048 }),
        file({ repoKey: 'other', path: 'elsewhere.txt' }), // different repo, filtered out
      ],
    });
    renderBucket();

    // The matching file renders with a download link into /generic/<project>/<repo>/<path>.
    const link = await screen.findByRole('link', { name: 'tools/setup.exe' });
    expect(link).toHaveAttribute('href', '/generic/acme/drop/tools/setup.exe');
    // The file from another repo is filtered out.
    expect(screen.queryByText('elsewhere.txt')).not.toBeInTheDocument();
    // The upload control is available to an authenticated user.
    expect(screen.getByLabelText('File to upload')).toBeInTheDocument();
  });

  it('shows an empty state when the bucket has no files', async () => {
    authed();
    listGenericFiles.mockResolvedValue({ files: [] });
    renderBucket();
    await waitFor(() => {
      expect(screen.getByText(/This bucket is empty/i)).toBeInTheDocument();
    });
  });
});
