import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { MembersPanel } from './MembersPanel';
import { api, ApiError } from '../../lib/api';
import type { Member } from '../../lib/types';

vi.mock('../../lib/api', () => ({
  ApiError: class ApiError extends Error {
    constructor(
      public status: number,
      message: string,
    ) {
      super(message);
    }
  },
  api: { listMembers: vi.fn(), setMember: vi.fn(), removeMember: vi.fn() },
}));

const listMembers = vi.mocked(api.listMembers);
const setMember = vi.mocked(api.setMember);

const member = (over: Partial<Member> = {}): Member => ({
  username: 'alice',
  email: 'alice@example.com',
  role: 'reader',
  ...over,
});

afterEach(() => vi.clearAllMocks());

describe('MembersPanel', () => {
  it('lists members and lets an admin add one', async () => {
    listMembers.mockResolvedValue({ members: [member()] });
    setMember.mockResolvedValue(member({ username: 'bob', role: 'maintainer' }));
    render(<MembersPanel projectKey="acme" />);

    expect(await screen.findByText('alice')).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText('Username to add'), { target: { value: 'bob' } });
    fireEvent.click(screen.getByRole('button', { name: /add member/i }));

    await waitFor(() => {
      expect(setMember).toHaveBeenCalledWith('acme', 'bob', 'reader');
    });
  });

  it('renders nothing for a user who cannot manage members (403)', async () => {
    listMembers.mockRejectedValue(new ApiError(403, 'Forbidden'));
    const { container } = render(<MembersPanel projectKey="acme" />);
    await waitFor(() => {
      expect(listMembers).toHaveBeenCalled();
    });
    expect(container).toBeEmptyDOMElement();
  });
});
