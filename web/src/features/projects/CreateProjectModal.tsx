import { useState } from 'react';
import { api, ApiError } from '../../lib/api';
import { Button, Modal } from '../../components/ui';
import type { CreateProjectRequest } from '../../lib/types';

// Derives a URL-safe key suggestion from a display name.
function slugify(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 40);
}

// Upstream presets keep the common registries one click away; "custom" lets the
// user point at anything that speaks the OCI Distribution Spec.
const UPSTREAM_PRESETS = [
  { id: 'dockerhub', label: 'Docker Hub', url: 'https://registry-1.docker.io' },
  { id: 'ghcr', label: 'GitHub Container Registry', url: 'https://ghcr.io' },
  { id: 'custom', label: 'Custom…', url: '' },
] as const;

type ProjectKind = 'local' | 'proxy';

export function CreateProjectModal({
  onClose,
  onCreated,
}: {
  onClose: () => void;
  onCreated: () => void;
}) {
  const [name, setName] = useState('');
  const [key, setKey] = useState('');
  const [keyEdited, setKeyEdited] = useState(false);
  const [description, setDescription] = useState('');
  const [kind, setKind] = useState<ProjectKind>('local');
  const [preset, setPreset] = useState<(typeof UPSTREAM_PRESETS)[number]['id']>('dockerhub');
  const [upstreamUrl, setUpstreamUrl] = useState<string>(UPSTREAM_PRESETS[0].url);
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string>();
  const [submitting, setSubmitting] = useState(false);

  // Key follows the name until the user edits it directly.
  const effectiveKey = keyEdited ? key : slugify(name);

  function choosePreset(id: (typeof UPSTREAM_PRESETS)[number]['id']) {
    setPreset(id);
    const match = UPSTREAM_PRESETS.find((p) => p.id === id);
    if (match && id !== 'custom') {
      setUpstreamUrl(match.url);
    } else {
      setUpstreamUrl('');
    }
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setSubmitting(true);
    setError(undefined);
    try {
      const req: CreateProjectRequest = {
        key: effectiveKey,
        name,
        description: description || undefined,
      };
      if (kind === 'proxy') {
        req.upstream = {
          url: upstreamUrl.trim(),
          username: username || undefined,
          password: password || undefined,
        };
      }
      await api.createProject(req);
      onCreated();
      onClose();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Something went wrong');
      setSubmitting(false);
    }
  }

  return (
    <Modal title="New project" onClose={onClose}>
      <form
        onSubmit={(e) => {
          void submit(e);
        }}
        className="space-y-4"
      >
        <Field label="Name" htmlFor="project-name">
          <input
            id="project-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Acme Corp"
            required
            className={inputClass}
          />
        </Field>

        <Field label="Key" htmlFor="project-key" hint="Lowercase, digits, and hyphens. Used in URLs.">
          <input
            id="project-key"
            value={effectiveKey}
            onChange={(e) => {
              setKeyEdited(true);
              setKey(e.target.value);
            }}
            placeholder="acme"
            required
            className={`${inputClass} font-mono`}
          />
        </Field>

        <Field label="Description" htmlFor="project-description" hint="Optional.">
          <input
            id="project-description"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            className={inputClass}
          />
        </Field>

        <fieldset>
          <legend className="mb-1.5 block text-sm font-medium text-slate-700">Type</legend>
          <div className="grid grid-cols-2 gap-2">
            <KindOption
              active={kind === 'local'}
              onClick={() => setKind('local')}
              title="Local"
              subtitle="Push and pull your own artifacts."
            />
            <KindOption
              active={kind === 'proxy'}
              onClick={() => setKind('proxy')}
              title="Pull-through proxy"
              subtitle="Mirror & cache an upstream registry."
            />
          </div>
        </fieldset>

        {kind === 'proxy' ? (
          <div className="space-y-4 rounded-xl border border-slate-200 bg-slate-50/60 p-4">
            <Field label="Upstream" htmlFor="upstream-preset">
              <select
                id="upstream-preset"
                value={preset}
                onChange={(e) => choosePreset(e.target.value as typeof preset)}
                className={inputClass}
              >
                {UPSTREAM_PRESETS.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.label}
                  </option>
                ))}
              </select>
            </Field>

            <Field
              label="Upstream URL"
              htmlFor="upstream-url"
              hint="The registry's API root, e.g. https://registry-1.docker.io"
            >
              <input
                id="upstream-url"
                value={upstreamUrl}
                onChange={(e) => setUpstreamUrl(e.target.value)}
                placeholder="https://registry-1.docker.io"
                required
                readOnly={preset !== 'custom'}
                className={`${inputClass} font-mono ${preset !== 'custom' ? 'text-slate-500' : ''}`}
              />
            </Field>

            <div className="grid grid-cols-2 gap-3">
              <Field label="Username" htmlFor="upstream-username" hint="Optional.">
                <input
                  id="upstream-username"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  autoComplete="off"
                  className={inputClass}
                />
              </Field>
              <Field label="Password / token" htmlFor="upstream-password" hint="Optional.">
                <input
                  id="upstream-password"
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  autoComplete="new-password"
                  className={inputClass}
                />
              </Field>
            </div>

            {effectiveKey ? (
              <p className="text-xs text-slate-500">
                Pull through it with{' '}
                <code className="rounded bg-slate-200/70 px-1 py-0.5 font-mono text-[11px] text-slate-700">
                  docker pull &lt;host&gt;/{effectiveKey}/library/alpine
                </code>
              </p>
            ) : null}
          </div>
        ) : null}

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
            {submitting ? 'Creating…' : 'Create project'}
          </Button>
        </div>
      </form>
    </Modal>
  );
}

const inputClass =
  'w-full rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm text-slate-800 shadow-sm outline-none placeholder:text-slate-400 focus:border-teal-500 focus:ring-2 focus:ring-teal-500/20';

function KindOption({
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
  children: React.ReactNode;
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
