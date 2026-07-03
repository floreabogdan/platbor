import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { DeleteDialog } from './DeleteDialog';
import { api } from '../../lib/api';

vi.mock('../../lib/api', () => ({
  ApiError: class ApiError extends Error {},
  api: { deleteManifest: vi.fn() },
}));

const deleteManifest = vi.mocked(api.deleteManifest);

afterEach(() => {
  vi.clearAllMocks();
});

describe('DeleteDialog', () => {
  it('deletes a tag and reports completion', async () => {
    deleteManifest.mockResolvedValue(undefined);
    const onDeleted = vi.fn();
    render(
      <DeleteDialog
        project="library"
        repo="images"
        image="alpine"
        target={{ mode: 'tag', label: 'v1.0', reference: 'v1.0' }}
        onClose={() => {}}
        onDeleted={onDeleted}
      />,
    );

    expect(screen.getByText(/Delete the tag/i)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }));

    await waitFor(() => {
      expect(onDeleted).toHaveBeenCalled();
    });
    expect(deleteManifest).toHaveBeenCalledWith('library', 'images', 'alpine', 'v1.0');
  });

  it('warns that a manifest delete removes its tags', () => {
    render(
      <DeleteDialog
        project="library"
        repo="images"
        image="alpine"
        target={{ mode: 'manifest', label: 'abc123def456', reference: 'sha256:abc', affectedTags: 2 }}
        onClose={() => {}}
        onDeleted={() => {}}
      />,
    );

    expect(screen.getByRole('heading', { name: 'Delete manifest' })).toBeInTheDocument();
    expect(screen.getByText(/cannot be undone/i)).toBeInTheDocument();
  });

  it('surfaces an API error and stays open', async () => {
    deleteManifest.mockRejectedValue(new Error('permission denied'));
    const onDeleted = vi.fn();
    render(
      <DeleteDialog
        project="library"
        repo="images"
        image="alpine"
        target={{ mode: 'tag', label: 'v1.0', reference: 'v1.0' }}
        onClose={() => {}}
        onDeleted={onDeleted}
      />,
    );

    fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
    await waitFor(() => {
      expect(screen.getByText('permission denied')).toBeInTheDocument();
    });
    expect(onDeleted).not.toHaveBeenCalled();
  });
});
