import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { NugetPage } from './NugetPage';
import { api } from '../../lib/api';
import type { NugetPackageDetail } from '../../lib/types';

vi.mock('../../lib/api', () => ({
  ApiError: class ApiError extends Error {},
  api: { getNugetPackage: vi.fn() },
}));

const getNugetPackage = vi.mocked(api.getNugetPackage);

afterEach(() => {
  vi.clearAllMocks();
});

// renderAt mounts the NuGet route so useParams resolves the project and the id
// splat exactly as the app router does.
function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="registry/:project/-nuget-/*" element={<NugetPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

const detail = (over: Partial<NugetPackageDetail>): NugetPackageDetail => ({
  id: 'Newtonsoft.Json',
  versions: [
    { version: '13.0.3', sizeBytes: 5_242_880, publishedAt: '2026-07-02T10:00:00Z' },
    { version: '13.0.1', sizeBytes: 1024, publishedAt: '2026-07-01T10:00:00Z' },
  ],
  ...over,
});

describe('NugetPage', () => {
  it('renders versions and the dotnet install snippet', async () => {
    getNugetPackage.mockResolvedValue(detail({}));
    renderAt('/registry/library/-nuget-/Newtonsoft.Json');

    // Requests the right (project, id) pair.
    await waitFor(() => {
      expect(getNugetPackage).toHaveBeenCalledWith('library', 'Newtonsoft.Json');
    });

    // Install snippet: add the feed source, then add the package.
    expect(await screen.findByText('dotnet add package Newtonsoft.Json')).toBeInTheDocument();
    expect(
      screen.getByText(/dotnet nuget add source .*\/nuget\/library\/v3\/index\.json --name library/),
    ).toBeInTheDocument();

    // Versions with their sizes, newest first.
    expect(screen.getByText('13.0.3')).toBeInTheDocument();
    expect(screen.getByText('13.0.1')).toBeInTheDocument();
    expect(screen.getByText('5 MB')).toBeInTheDocument();
  });

  it('shows an error state when loading fails', async () => {
    getNugetPackage.mockRejectedValue(new Error('nuget boom'));
    renderAt('/registry/library/-nuget-/Newtonsoft.Json');
    await waitFor(() => {
      expect(screen.getByText('nuget boom')).toBeInTheDocument();
    });
  });
});
