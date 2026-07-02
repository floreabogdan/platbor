import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { SettingsPage } from './SettingsPage';
import { api } from '../../lib/api';
import type { Token } from '../../lib/types';

vi.mock('../../lib/api', () => ({
  ApiError: class ApiError extends Error {},
  api: { listTokens: vi.fn(), createToken: vi.fn(), deleteToken: vi.fn() },
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

describe('SettingsPage', () => {
  it('shows the empty state when there are no tokens', async () => {
    listTokens.mockResolvedValue({ tokens: [] });
    render(<SettingsPage />);
    await waitFor(() => {
      expect(screen.getByText(/No tokens yet/i)).toBeInTheDocument();
    });
  });

  it('lists tokens with their prefix', async () => {
    listTokens.mockResolvedValue({ tokens: [token] });
    render(<SettingsPage />);
    await waitFor(() => {
      expect(screen.getByText('CI pipeline')).toBeInTheDocument();
    });
    expect(screen.getByText(/pbt_abc123/)).toBeInTheDocument();
  });

  it('revokes a token only after confirmation', async () => {
    listTokens.mockResolvedValue({ tokens: [token] });
    deleteToken.mockResolvedValue(undefined);
    render(<SettingsPage />);
    await waitFor(() => {
      expect(screen.getByText('CI pipeline')).toBeInTheDocument();
    });

    // First click asks for confirmation; it must not delete yet.
    fireEvent.click(screen.getByRole('button', { name: 'Revoke' }));
    expect(deleteToken).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
    await waitFor(() => {
      expect(deleteToken).toHaveBeenCalledWith('tok_1');
    });
  });
});
