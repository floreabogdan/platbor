import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { DashboardPage } from './DashboardPage';
import { api } from '../../lib/api';

vi.mock('../../lib/api', () => ({
  ApiError: class ApiError extends Error {},
  api: { getDashboard: vi.fn() },
}));

const getDashboard = vi.mocked(api.getDashboard);

function renderPage() {
  return render(
    <MemoryRouter>
      <DashboardPage />
    </MemoryRouter>,
  );
}

afterEach(() => {
  vi.clearAllMocks();
});

describe('DashboardPage', () => {
  it('renders live counts and a linked activity entry', async () => {
    getDashboard.mockResolvedValue({
      summary: { projects: 2, repositories: 3, tags: 5 },
      activity: [
        {
          actor: 'admin',
          action: 'oci.manifest.push',
          targetType: 'manifest',
          targetId: 'sha256:abc',
          metadata: { repository: 'alpine', reference: 'v1.0' },
          projectKey: 'library',
          projectName: 'Library',
          at: '2026-07-02T10:00:00Z',
        },
      ],
    });

    renderPage();

    // Repositories count (unique among the three cards).
    await waitFor(() => {
      expect(screen.getByText('3')).toBeInTheDocument();
    });
    expect(screen.getByText('admin')).toBeInTheDocument();

    // The push links to the affected tag view.
    const link = screen.getByRole('link', { name: 'library/alpine:v1.0' });
    expect(link).toHaveAttribute('href', '/registry/library/alpine?ref=v1.0');
  });

  it('shows an empty activity state', async () => {
    getDashboard.mockResolvedValue({
      summary: { projects: 0, repositories: 0, tags: 0 },
      activity: [],
    });

    renderPage();

    await waitFor(() => {
      expect(screen.getByText(/No activity yet/i)).toBeInTheDocument();
    });
  });
});
