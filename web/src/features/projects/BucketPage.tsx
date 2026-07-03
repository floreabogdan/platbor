import { useEffect, useState } from 'react';
import type { ChangeEvent, ReactNode } from 'react';
import { useParams } from 'react-router-dom';
import { api } from '../../lib/api';
import { Breadcrumb, Button, Card, EmptyState, PageHeader } from '../../components/ui';
import { FileIcon, TrashIcon } from '../../components/icons';
import { useAuth } from '../../lib/auth';
import { cx } from '../../lib/cx';
import { formatBytes, formatDate } from '../../lib/format';
import type { GenericFile } from '../../lib/types';

// encodePath percent-encodes each segment but keeps the slashes that structure a
// bucket into folders, so /generic/<project>/<repo>/<path> resolves correctly.
function encodePath(path: string): string {
  return path.split('/').map(encodeURIComponent).join('/');
}

// BucketPage is a generic repository viewed as a file bucket: browse, download,
// upload, and delete files. Uploads and downloads hit /generic/<project>/<repo>/
// directly, carrying the session cookie (the generic adapter accepts it).
export function BucketPage() {
  const params = useParams();
  const project = params.key ?? '';
  const repo = params.repo ?? '';
  const { state: authState } = useAuth();
  const authed = authState.status === 'authenticated';

  const [files, setFiles] = useState<GenericFile[]>([]);
  const [state, setState] = useState<'loading' | 'ready' | 'error'>('loading');
  const [error, setError] = useState<string>();

  async function load() {
    try {
      const res = await api.listGenericFiles();
      setFiles(res.files.filter((f) => f.projectKey === project && f.repoKey === repo));
      setState('ready');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load files');
      setState('error');
    }
  }

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [project, repo]);

  return (
    <div className="animate-rise">
      <Breadcrumb
        items={[
          { label: 'Projects', to: '/projects' },
          { label: project, to: `/projects/${project}` },
          { label: repo },
        ]}
      />
      <PageHeader title={repo} subtitle={`Generic bucket in ${project} — browse, upload, and download files.`} />

      {authed ? <UploadCard project={project} repo={repo} onUploaded={() => void load()} /> : null}

      {state === 'loading' ? <Card className="mt-5 h-32 animate-pulse bg-slate-50" /> : null}
      {state === 'error' ? (
        <Card className="mt-5 p-6">
          <p className="text-sm text-red-700">{error}</p>
        </Card>
      ) : null}
      {state === 'ready' && files.length === 0 ? (
        <div className="mt-5">
          <EmptyState
            icon={<FileIcon className="h-8 w-8" />}
            message="This bucket is empty. Upload a file above, or PUT one from a script."
          />
        </div>
      ) : null}
      {state === 'ready' && files.length > 0 ? (
        <Card className="mt-5 overflow-hidden">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-slate-200/80 text-left text-xs uppercase tracking-wide text-slate-400">
                <Th>File</Th>
                <Th className="text-right">Size</Th>
                <Th>Updated</Th>
                <Th className="text-right">
                  <span className="sr-only">Actions</span>
                </Th>
              </tr>
            </thead>
            <tbody>
              {files.map((f) => (
                <FileRow
                  key={f.path}
                  project={project}
                  repo={repo}
                  file={f}
                  canDelete={authed}
                  onDeleted={() => void load()}
                />
              ))}
            </tbody>
          </table>
        </Card>
      ) : null}
    </div>
  );
}

function UploadCard({ project, repo, onUploaded }: { project: string; repo: string; onUploaded: () => void }) {
  const [file, setFile] = useState<File | null>(null);
  const [prefix, setPrefix] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();

  async function upload() {
    if (!file) return;
    setBusy(true);
    setError(undefined);
    const folder = prefix.replace(/^\/+|\/+$/g, '');
    const path = folder ? `${folder}/${file.name}` : file.name;
    try {
      const res = await fetch(`/generic/${project}/${repo}/${encodePath(path)}`, {
        method: 'PUT',
        body: file,
        credentials: 'include',
      });
      if (!res.ok) {
        throw new Error(`Upload failed (${String(res.status)})`);
      }
      setFile(null);
      setPrefix('');
      onUploaded();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Upload failed');
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card className="p-4">
      <div className="flex flex-wrap items-center gap-3">
        <input
          type="file"
          aria-label="File to upload"
          onChange={(e: ChangeEvent<HTMLInputElement>) => setFile(e.target.files?.[0] ?? null)}
          className="text-sm text-slate-600 file:mr-3 file:rounded-md file:border-0 file:bg-slate-100 file:px-3 file:py-1.5 file:text-sm file:font-medium file:text-slate-700 hover:file:bg-slate-200"
        />
        <input
          type="text"
          value={prefix}
          placeholder="optional/folder"
          aria-label="Optional folder prefix"
          onChange={(e) => setPrefix(e.target.value)}
          className="rounded-md border border-slate-200 px-3 py-1.5 text-sm text-slate-700 placeholder:text-slate-400 focus:border-teal-500 focus:outline-none"
        />
        <Button onClick={() => void upload()} disabled={!file || busy}>
          {busy ? 'Uploading…' : 'Upload'}
        </Button>
      </div>
      {error ? <p className="mt-2 text-sm text-red-700">{error}</p> : null}
    </Card>
  );
}

function FileRow({
  project,
  repo,
  file,
  canDelete,
  onDeleted,
}: {
  project: string;
  repo: string;
  file: GenericFile;
  canDelete: boolean;
  onDeleted: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const href = `/generic/${project}/${repo}/${encodePath(file.path)}`;

  async function del() {
    setBusy(true);
    try {
      const res = await fetch(href, { method: 'DELETE', credentials: 'include' });
      if (res.ok || res.status === 404) {
        onDeleted();
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <tr className="border-b border-slate-100 last:border-0">
      <Td>
        <a href={href} download className="font-mono text-teal-700 hover:underline">
          {file.path}
        </a>
      </Td>
      <Td className="text-right tabular-nums text-slate-600">{formatBytes(file.sizeBytes)}</Td>
      <Td className="text-slate-500">{formatDate(file.updatedAt)}</Td>
      <Td className="text-right">
        {canDelete ? (
          <button
            type="button"
            onClick={() => void del()}
            disabled={busy}
            aria-label={`Delete ${file.path}`}
            className="rounded-md p-1.5 text-slate-400 transition-colors hover:bg-red-50 hover:text-red-600"
          >
            <TrashIcon className="h-4 w-4" />
          </button>
        ) : null}
      </Td>
    </tr>
  );
}

function Th({ children, className }: { children: ReactNode; className?: string }) {
  return <th className={cx('px-4 py-2.5 font-medium', className)}>{children}</th>;
}

function Td({ children, className }: { children: ReactNode; className?: string }) {
  return <td className={cx('px-4 py-2.5', className)}>{children}</td>;
}
