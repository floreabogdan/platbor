import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ProfilePage } from './ProfilePage';
import { api } from '../../lib/api';
import type { Token, User } from '../../lib/types';

vi.mock('../../lib/api', () => ({
  ApiError: class ApiError extends Error {},
  api: { listTokens: vi.fn(), createToken: vi.fn(), deleteToken: vi.fn() },
}));

// AuthProvider is not mounted in these tests, so stub useAuth with an
// authenticated admin.
const adminUser: User = {
  id: 'usr_1',
  username: 'admin',
  email: '',
  isAdmin: true,
  createdAt: '2026-07-02T10:00:00Z',
};
vi.mock('../../lib/auth', () => ({
  useAuth: () => ({ state: { status: 'authenticated', user: adminUser } }),
}));

const listTokens = vi.mocked(api.listTokens);
const deleteToken = vi.mocked(api.deleteToken);

const token: Token = {
  id: 'tok_1',
  name: 'CI pipeline',
  prefix: 'pbt_abc123',
  createdAt: '2026-07-02T10:00:00Z',
};

afterEach(() => {
  vi.clearAllMocks();
});

describe('ProfilePage', () => {
  it('shows account details on the Account tab', () => {
    listTokens.mockResolvedValue({ tokens: [] });
    render(<ProfilePage />);
    expect(screen.getByText('admin')).toBeInTheDocument();
    expect(screen.getByText('Instance admin')).toBeInTheDocument();
  });

  it('lists tokens on the Access tokens tab', async () => {
    listTokens.mockResolvedValue({ tokens: [token] });
    render(<ProfilePage />);
    fireEvent.click(screen.getByRole('button', { name: 'Access tokens' }));
    await waitFor(() => {
      expect(screen.getByText('CI pipeline')).toBeInTheDocument();
    });
    expect(screen.getByText(/pbt_abc123/)).toBeInTheDocument();
  });

  it('revokes a token only after confirmation', async () => {
    listTokens.mockResolvedValue({ tokens: [token] });
    deleteToken.mockResolvedValue(undefined);
    render(<ProfilePage />);
    fireEvent.click(screen.getByRole('button', { name: 'Access tokens' }));
    await waitFor(() => {
      expect(screen.getByText('CI pipeline')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole('button', { name: 'Revoke' }));
    expect(deleteToken).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
    await waitFor(() => {
      expect(deleteToken).toHaveBeenCalledWith('tok_1');
    });
  });
});
