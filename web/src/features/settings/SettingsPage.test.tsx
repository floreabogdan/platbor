import { render, screen, waitFor } from '@testing-library/react';
import { fireEvent } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { SettingsPage } from './SettingsPage';
import { api } from '../../lib/api';
import { useAuth } from '../../lib/auth';
import type { User } from '../../lib/types';

vi.mock('../../lib/api', () => ({
  ApiError: class ApiError extends Error {},
  api: { runGarbageCollection: vi.fn() },
}));

vi.mock('../../lib/auth', () => ({
  useAuth: vi.fn(),
}));

const runGarbageCollection = vi.mocked(api.runGarbageCollection);
const mockedUseAuth = vi.mocked(useAuth);

const adminUser: User = {
  id: 'u1',
  username: 'admin',
  email: 'a@e',
  isAdmin: true,
  createdAt: '2026-07-02T10:00:00Z',
};

function asAdmin(isAdmin: boolean) {
  mockedUseAuth.mockReturnValue({
    state: { status: 'authenticated', user: { ...adminUser, isAdmin } },
  } as unknown as ReturnType<typeof useAuth>);
}

afterEach(() => {
  vi.clearAllMocks();
});

describe('SettingsPage', () => {
  it('hides maintenance from non-admins', () => {
    asAdmin(false);
    render(<SettingsPage />);
    expect(screen.getByText(/available to admins only/i)).toBeInTheDocument();
    expect(screen.queryByText('Garbage collection')).not.toBeInTheDocument();
  });

  it('previews garbage collection for an admin', async () => {
    asAdmin(true);
    runGarbageCollection.mockResolvedValue({
      dryRun: true,
      scanned: 5,
      deleted: 2,
      reclaimedBytes: 2048,
      kept: 3,
    });
    render(<SettingsPage />);

    fireEvent.click(screen.getByRole('button', { name: 'Preview' }));

    await waitFor(() => {
      expect(screen.getByText(/Would remove/i)).toBeInTheDocument();
    });
    expect(runGarbageCollection).toHaveBeenCalledWith(true);
    expect(screen.getByText(/Nothing was deleted/i)).toBeInTheDocument();
  });
});
