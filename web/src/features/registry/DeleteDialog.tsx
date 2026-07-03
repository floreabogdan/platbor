import { useState } from 'react';
import { Button, Modal } from '../../components/ui';
import { api } from '../../lib/api';

// A delete request from the browser: either untag (mode 'tag') or remove a whole
// manifest and every tag pointing at it (mode 'manifest').
export interface DeleteTarget {
  mode: 'tag' | 'manifest';
  label: string; // tag name or short digest, for display
  reference: string; // the tag or digest sent to the API
  affectedTags?: number; // manifest mode: how many tags disappear with it
}

// DeleteDialog confirms and performs a registry deletion. It owns the request so
// callers only supply the target and react to completion.
export function DeleteDialog({
  project,
  repo,
  image,
  target,
  onClose,
  onDeleted,
}: {
  project: string;
  repo: string;
  image: string;
  target: DeleteTarget;
  onClose: () => void;
  onDeleted: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();

  async function confirm() {
    setBusy(true);
    setError(undefined);
    try {
      await api.deleteManifest(project, repo, image, target.reference);
      onDeleted();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Delete failed');
      setBusy(false);
    }
  }

  return (
    <Modal title={target.mode === 'tag' ? 'Delete tag' : 'Delete manifest'} onClose={onClose}>
      <p className="text-sm text-slate-600">
        {target.mode === 'tag' ? (
          <>
            Delete the tag{' '}
            <code className="rounded bg-slate-100 px-1 py-0.5 font-mono text-slate-800">{target.label}</code> from{' '}
            <span className="font-mono text-slate-700">
              {project}/{repo}/{image}
            </span>
            ? The image itself stays if other tags point to it.
          </>
        ) : (
          <>
            Delete this manifest{' '}
            <code className="rounded bg-slate-100 px-1 py-0.5 font-mono text-slate-800">{target.label}</code> and its{' '}
            {target.affectedTags ?? 0} {target.affectedTags === 1 ? 'tag' : 'tags'} from{' '}
            <span className="font-mono text-slate-700">
              {project}/{repo}/{image}
            </span>
            ? This cannot be undone.
          </>
        )}
      </p>

      {error ? <p className="mt-3 text-sm text-red-700">{error}</p> : null}

      <div className="mt-6 flex justify-end gap-2">
        <Button variant="secondary" onClick={onClose} disabled={busy}>
          Cancel
        </Button>
        <Button variant="danger" onClick={() => void confirm()} disabled={busy}>
          {busy ? 'Deleting…' : 'Delete'}
        </Button>
      </div>
    </Modal>
  );
}
