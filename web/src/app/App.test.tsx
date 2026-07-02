import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { App } from './App';
import { api } from '../lib/api';
import type { User } from '../lib/types';

vi.mock('../lib/api', () => ({
  ApiError: class ApiError extends Error {},
  api: {
    me: vi.fn(),
    login: vi.fn(),
    logout: vi.fn(),
    listProjects: vi.fn(),
    createProject: vi.fn(),
  },
}));

const me = vi.mocked(api.me);

const adminUser: User = {
  id: 'usr_1',
  username: 'admin',
  email: '',
  isAdmin: true,
  createdAt: '2026-07-02T10:00:00Z',
};

function renderApp() {
  return render(
    <MemoryRouter>
      <App />
    </MemoryRouter>,
  );
}

afterEach(() => {
  vi.clearAllMocks();
});

describe('App auth gating', () => {
  it('shows the login screen when there is no session', async () => {
    me.mockRejectedValue(new Error('unauthorized'));
    renderApp();
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /sign in/i })).toBeInTheDocument();
    });
  });

  it('shows the app shell with the signed-in user when authenticated', async () => {
    me.mockResolvedValue(adminUser);
    renderApp();
    await waitFor(() => {
      expect(screen.getByText('Everything at a glance.')).toBeInTheDocument();
    });
    // Sidebar reflects the real identity.
    expect(screen.getByText('admin')).toBeInTheDocument();
    expect(screen.getByText('instance admin')).toBeInTheDocument();
  });
});
