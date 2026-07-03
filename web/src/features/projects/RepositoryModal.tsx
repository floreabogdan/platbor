import { useState } from 'react';
import type { ReactNode } from 'react';
import { api, ApiError } from '../../lib/api';
import { Button, Modal } from '../../components/ui';
import type { CreateRepoRequest, Repo, RepoFormat, RepoMode, UpdateRepoRequest } from '../../lib/types';

const FORMATS: { value: RepoFormat; label: string }[] = [
  { value: 'oci', label: 'Container images (OCI)' },
  { value: 'npm', label: 'npm' },
  { value: 'nuget', label: 'NuGet' },
  { value: 'generic', label: 'Generic files' },
];

function slugify(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 40);
}

// RepositoryModal creates a new repository, or edits an existing one's name,
// upstream, and retention (key/format/mode are immutable once created).
export function RepositoryModal({
  projectKey,
  repo,
  onClose,
  onSaved,
}: {
  projectKey: string;
  repo?: Repo; // present in edit mode
  onClose: () => void;
  onSaved: () => void;
}) {
  const editing = repo !== undefined;

  const [name, setName] = useState(repo?.name ?? '');
  const [key, setKey] = useState(repo?.key ?? '');
  const [keyEdited, setKeyEdited] = useState(false);
  const [format, setFormat] = useState<RepoFormat>(repo?.format ?? 'oci');
  const [mode, setMode] = useState<RepoMode>(repo?.mode ?? 'local');
  const [upstreamUrl, setUpstreamUrl] = useState(repo?.upstream?.url ?? '');
  const [username, setUsername] = useState(repo?.upstream?.username ?? '');
  const [password, setPassword] = useState('');
  const [keepLast, setKeepLast] = useState<number>(repo?.retention.keepLast ?? 0);
  const [deleteUntagged, setDeleteUntagged] = useState<boolean>(repo?.retention.deleteUntagged ?? false);
  const [error, setError] = useState<string>();
  const [submitting, setSubmitting] = useState(false);

  const effectiveKey = editing ? key : keyEdited ? key : slugify(name);
  const isProxy = mode === 'proxy';
  const isOci = format === 'oci';

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setSubmitting(true);
    setError(undefined);
    const retention = { keepLast: keepLast || 0, deleteUntagged: isOci ? deleteUntagged : false };
    const upstream = isProxy
      ? { url: upstreamUrl.trim(), username: username || undefined, password: password || undefined }
      : undefined;
    try {
      if (editing) {
        const body: UpdateRepoRequest = { name, upstream, retention };
        await api.updateRepo(projectKey, repo.key, body);
      } else {
        const body: CreateRepoRequest = { key: effectiveKey, name, format, mode, upstream, retention };
        await api.createRepo(projectKey, body);
      }
      onSaved();
      onClose();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Something went wrong');
      setSubmitting(false);
    }
  }

  return (
    <Modal title={editing ? `Edit ${repo.key}` : 'New repository'} onClose={onClose}>
      <form
        onSubmit={(e) => {
          void submit(e);
        }}
        className="space-y-4"
      >
        <Field label="Name" htmlFor="repo-name">
          <input
            id="repo-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Docker Production"
            required
            className={inputClass}
          />
        </Field>

        {editing ? null : (
          <Field label="Key" htmlFor="repo-key" hint="Lowercase, digits, and hyphens. Used in the push/pull URL.">
            <input
              id="repo-key"
              value={effectiveKey}
              onChange={(e) => {
                setKeyEdited(true);
                setKey(e.target.value);
              }}
              placeholder="docker-prod"
              required
              className={`${inputClass} font-mono`}
            />
          </Field>
        )}

        {editing ? (
          <p className="text-xs text-slate-400">
            Format <span className="font-mono text-slate-600">{repo.format}</span> and mode{' '}
            <span className="font-mono text-slate-600">{repo.mode}</span> are fixed once a repository is created.
          </p>
        ) : (
          <>
            <Field label="Format" htmlFor="repo-format">
              <select
                id="repo-format"
                value={format}
                onChange={(e) => setFormat(e.target.value as RepoFormat)}
                className={inputClass}
              >
                {FORMATS.map((f) => (
                  <option key={f.value} value={f.value}>
                    {f.label}
                  </option>
                ))}
              </select>
            </Field>

            <fieldset>
              <legend className="mb-1.5 block text-sm font-medium text-slate-700">Mode</legend>
              <div className="grid grid-cols-2 gap-2">
                <ModeOption
                  active={mode === 'local'}
                  onClick={() => setMode('local')}
                  title="Local"
                  subtitle="Store your own artifacts here."
                />
                <ModeOption
                  active={mode === 'proxy'}
                  onClick={() => setMode('proxy')}
                  title="Pull-through proxy"
                  subtitle="Mirror & cache an upstream registry."
                />
              </div>
            </fieldset>
          </>
        )}

        {isProxy ? (
          <div className="space-y-4 rounded-xl border border-slate-200 bg-slate-50/60 p-4">
            <Field label="Upstream URL" htmlFor="repo-upstream" hint="The registry this repository mirrors.">
              <input
                id="repo-upstream"
                value={upstreamUrl}
                onChange={(e) => setUpstreamUrl(e.target.value)}
                placeholder="https://registry-1.docker.io"
                required
                className={`${inputClass} font-mono`}
              />
            </Field>
            <div className="grid grid-cols-2 gap-3">
              <Field label="Username" htmlFor="repo-upstream-user" hint="Optional.">
                <input
                  id="repo-upstream-user"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  autoComplete="off"
                  className={inputClass}
                />
              </Field>
              <Field
                label="Password / token"
                htmlFor="repo-upstream-pass"
                hint={editing ? 'Leave blank to keep current.' : 'Optional.'}
              >
                <input
                  id="repo-upstream-pass"
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  autoComplete="new-password"
                  className={inputClass}
                />
              </Field>
            </div>
          </div>
        ) : null}

        <div className="space-y-3 rounded-xl border border-slate-200 bg-slate-50/60 p-4">
          <div className="text-sm font-medium text-slate-700">Retention</div>
          <Field label="Keep last N versions" htmlFor="repo-keeplast" hint="0 keeps everything.">
            <input
              id="repo-keeplast"
              type="number"
              min={0}
              value={keepLast}
              onChange={(e) => setKeepLast(Number(e.target.value))}
              className={inputClass}
            />
          </Field>
          {isOci ? (
            <label className="flex items-center gap-2 text-sm text-slate-700">
              <input
                type="checkbox"
                checked={deleteUntagged}
                onChange={(e) => setDeleteUntagged(e.target.checked)}
                className="h-4 w-4 rounded border-slate-300 text-teal-600 focus:ring-teal-500/30"
              />
              Sweep untagged manifests
            </label>
          ) : null}
        </div>

        {error ? (
          <p className="rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700 ring-1 ring-inset ring-red-600/20">
            {error}
          </p>
        ) : null}

        <div className="flex justify-end gap-2 pt-2">
          <Button variant="secondary" onClick={onClose} disabled={submitting}>
            Cancel
          </Button>
          <Button type="submit" disabled={submitting}>
            {submitting ? 'Saving…' : editing ? 'Save changes' : 'Create repository'}
          </Button>
        </div>
      </form>
    </Modal>
  );
}

const inputClass =
  'w-full rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm text-slate-800 shadow-sm outline-none placeholder:text-slate-400 focus:border-teal-500 focus:ring-2 focus:ring-teal-500/20';

function ModeOption({
  active,
  onClick,
  title,
  subtitle,
}: {
  active: boolean;
  onClick: () => void;
  title: string;
  subtitle: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={`rounded-xl border p-3 text-left transition ${
        active
          ? 'border-teal-500 bg-teal-50/60 ring-2 ring-teal-500/20'
          : 'border-slate-200 bg-white hover:border-slate-300'
      }`}
    >
      <div className="text-sm font-semibold text-slate-900">{title}</div>
      <div className="mt-0.5 text-xs text-slate-500">{subtitle}</div>
    </button>
  );
}

function Field({
  label,
  htmlFor,
  hint,
  children,
}: {
  label: string;
  htmlFor: string;
  hint?: string;
  children: ReactNode;
}) {
  return (
    <div>
      <label htmlFor={htmlFor} className="mb-1 block text-sm font-medium text-slate-700">
        {label}
      </label>
      {children}
      {hint ? <p className="mt-1 text-xs text-slate-400">{hint}</p> : null}
    </div>
  );
}
