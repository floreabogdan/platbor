import { useState } from 'react';
import type { ReactNode } from 'react';
import { Link, useParams } from 'react-router-dom';
import { api, ApiError } from '../../lib/api';
import { Breadcrumb, Button, Card, EmptyState, Modal, PageHeader } from '../../components/ui';
import { FileIcon, GoIcon, MavenIcon, NugetIcon, PackageIcon, ProjectsIcon, PypiIcon, RegistryIcon, TrashIcon } from '../../components/icons';
import { useAuth } from '../../lib/auth';
import { cx } from '../../lib/cx';
import { formatDate } from '../../lib/format';
import type { Repo, RepoFormat } from '../../lib/types';
import { MembersPanel } from './MembersPanel';
import { RepositoryModal } from './RepositoryModal';
import { useProjects } from './useProjects';
import { useRepositories } from './useRepositories';

const FORMAT_GLYPH: Record<RepoFormat, { label: string; icon: ReactNode }> = {
  oci: { label: 'Container images', icon: <RegistryIcon className="h-4 w-4" /> },
  npm: { label: 'npm', icon: <PackageIcon className="h-4 w-4" /> },
  nuget: { label: 'NuGet', icon: <NugetIcon className="h-4 w-4" /> },
  pypi: { label: 'PyPI', icon: <PypiIcon className="h-4 w-4" /> },
  maven: { label: 'Maven', icon: <MavenIcon className="h-4 w-4" /> },
  go: { label: 'Go', icon: <GoIcon className="h-4 w-4" /> },
  generic: { label: 'Generic files', icon: <FileIcon className="h-4 w-4" /> },
};

// Project detail: the tenant's repositories, where artifacts of one format live.
// Repositories can be created and configured (mode, upstream, retention) here
// before anything is pushed — or auto-created on first push when the project
// allows it.
export function ProjectPage() {
  const params = useParams();
  const key = params.key ?? '';
  const { projects } = useProjects();
  const project = projects.find((p) => p.key === key);
  const { repos, state, error, reload } = useRepositories(key);

  const { state: authState } = useAuth();
  const isAdmin = authState.status === 'authenticated' && authState.user.isAdmin;

  const [creating, setCreating] = useState(false);
  const [editing, setEditing] = useState<Repo | null>(null);
  const [deleting, setDeleting] = useState<Repo | null>(null);

  return (
    <div className="animate-rise">
      <Breadcrumb items={[{ label: 'Projects', to: '/projects' }, { label: key }]} />
      <PageHeader
        title={project?.name ?? key}
        subtitle={
          project?.allowAutoCreate === false
            ? 'Repositories must be created before pushing.'
            : 'Pushing to a new repo path auto-creates a local repository of that format.'
        }
        actions={isAdmin ? <Button onClick={() => setCreating(true)}>New repository</Button> : undefined}
      />

      {state === 'loading' ? <Card className="h-32 animate-pulse bg-slate-50" /> : null}

      {state === 'error' ? (
        <Card className="p-6">
          <p className="text-sm text-red-700">{error ?? 'Failed to load repositories.'}</p>
          <Button variant="secondary" className="mt-3" onClick={() => void reload()}>
            Retry
          </Button>
        </Card>
      ) : null}

      {state === 'ready' && repos.length === 0 ? (
        <EmptyState
          icon={<ProjectsIcon className="h-8 w-8" />}
          message={
            isAdmin
              ? 'No repositories yet. Create one to configure its format, mode, and retention — or just push and it auto-creates.'
              : 'No repositories yet. Push an artifact and one is created automatically.'
          }
          action={isAdmin ? <Button onClick={() => setCreating(true)}>New repository</Button> : undefined}
        />
      ) : null}

      {state === 'ready' && repos.length > 0 ? (
        <Card className="overflow-hidden">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-slate-200/80 text-left text-xs uppercase tracking-wide text-slate-400">
                <Th>Repository</Th>
                <Th>Format</Th>
                <Th>Mode</Th>
                <Th>Retention</Th>
                <Th>Created</Th>
                {isAdmin ? (
                  <Th className="text-right">
                    <span className="sr-only">Actions</span>
                  </Th>
                ) : null}
              </tr>
            </thead>
            <tbody>
              {repos.map((repo) => (
                <RepoRow
                  key={repo.key}
                  projectKey={key}
                  repo={repo}
                  isAdmin={isAdmin}
                  onEdit={() => setEditing(repo)}
                  onDelete={() => setDeleting(repo)}
                />
              ))}
            </tbody>
          </table>
        </Card>
      ) : null}

      <MembersPanel projectKey={key} />

      {creating ? (
        <RepositoryModal projectKey={key} onClose={() => setCreating(false)} onSaved={() => void reload()} />
      ) : null}
      {editing ? (
        <RepositoryModal
          projectKey={key}
          repo={editing}
          onClose={() => setEditing(null)}
          onSaved={() => void reload()}
        />
      ) : null}
      {deleting ? (
        <DeleteRepoDialog
          projectKey={key}
          repo={deleting}
          onClose={() => setDeleting(null)}
          onDeleted={() => {
            setDeleting(null);
            void reload();
          }}
        />
      ) : null}
    </div>
  );
}

function RepoRow({
  projectKey,
  repo,
  isAdmin,
  onEdit,
  onDelete,
}: {
  projectKey: string;
  repo: Repo;
  isAdmin: boolean;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const glyph = FORMAT_GLYPH[repo.format];
  // A generic repository is a browsable bucket: link into it. Other formats are
  // read/written by their protocol client and browsed in the Registry.
  const isBucket = repo.format === 'generic';
  return (
    <tr className="border-b border-slate-100 last:border-0">
      <Td>
        {isBucket ? (
          <Link
            to={`/projects/${projectKey}/buckets/${repo.key}`}
            className="font-mono font-medium text-teal-700 hover:underline"
          >
            {repo.key}
          </Link>
        ) : (
          <span className="font-mono font-medium text-slate-900">{repo.key}</span>
        )}
        {repo.name && repo.name !== repo.key ? (
          <span className="ml-2 text-xs text-slate-400">{repo.name}</span>
        ) : null}
      </Td>
      <Td>
        <span className="inline-flex items-center gap-1.5 text-slate-600" title={glyph.label}>
          <span className="text-slate-400">{glyph.icon}</span>
          {glyph.label}
        </span>
      </Td>
      <Td>
        <ModeBadge mode={repo.mode} upstream={repo.upstream?.url} />
      </Td>
      <Td className="text-slate-500">
        {repo.retention.keepLast > 0 ? `keep last ${String(repo.retention.keepLast)}` : '—'}
        {repo.retention.deleteUntagged ? ' · untagged swept' : ''}
      </Td>
      <Td className="text-slate-500">{formatDate(repo.createdAt)}</Td>
      {isAdmin ? (
        <Td className="text-right">
          <div className="inline-flex items-center gap-1">
            <button
              type="button"
              onClick={onEdit}
              className="rounded-md px-2 py-1 text-xs font-medium text-slate-500 transition-colors hover:bg-slate-100 hover:text-slate-700"
            >
              Edit
            </button>
            <button
              type="button"
              onClick={onDelete}
              aria-label={`Delete repository ${repo.key}`}
              className="rounded-md p-1.5 text-slate-400 transition-colors hover:bg-red-50 hover:text-red-600"
            >
              <TrashIcon className="h-4 w-4" />
            </button>
          </div>
        </Td>
      ) : null}
    </tr>
  );
}

function ModeBadge({ mode, upstream }: { mode: string; upstream?: string }) {
  const proxy = mode === 'proxy';
  return (
    <span
      className={cx(
        'inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ring-1 ring-inset',
        proxy ? 'bg-amber-100 text-amber-700 ring-amber-600/20' : 'bg-slate-100 text-slate-600 ring-slate-500/20',
      )}
      title={proxy ? upstream : 'Stores its own artifacts'}
    >
      {proxy ? 'Proxy' : 'Local'}
    </span>
  );
}

function DeleteRepoDialog({
  projectKey,
  repo,
  onClose,
  onDeleted,
}: {
  projectKey: string;
  repo: Repo;
  onClose: () => void;
  onDeleted: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();

  async function confirm() {
    setBusy(true);
    setError(undefined);
    try {
      await api.deleteRepo(projectKey, repo.key);
      onDeleted();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Delete failed');
      setBusy(false);
    }
  }

  return (
    <Modal title="Delete repository" onClose={onClose}>
      <p className="text-sm text-slate-600">
        Delete <span className="font-mono text-slate-800">{repo.key}</span> and everything in it? The artifacts are
        removed immediately; their storage is reclaimed by the next garbage-collection sweep. This cannot be undone.
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

function Th({ children, className }: { children: ReactNode; className?: string }) {
  return <th className={cx('px-4 py-2.5 font-medium', className)}>{children}</th>;
}

function Td({ children, className }: { children: ReactNode; className?: string }) {
  return <td className={cx('px-4 py-2.5', className)}>{children}</td>;
}
