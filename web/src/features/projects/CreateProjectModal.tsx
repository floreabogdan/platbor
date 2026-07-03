import { useState } from 'react';
import type { ReactNode } from 'react';
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

// A project is a tenant that holds typed repositories. Proxy and retention are
// configured per repository (created inside the project), so the project form
// only sets identity and the auto-create governance toggle.
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
  const [allowAutoCreate, setAllowAutoCreate] = useState(true);
  const [error, setError] = useState<string>();
  const [submitting, setSubmitting] = useState(false);

  // Key follows the name until the user edits it directly.
  const effectiveKey = keyEdited ? key : slugify(name);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setSubmitting(true);
    setError(undefined);
    try {
      const req: CreateProjectRequest = {
        key: effectiveKey,
        name,
        description: description || undefined,
        allowAutoCreate,
      };
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

        <label className="flex items-start gap-2.5 rounded-xl border border-slate-200 bg-slate-50/60 p-3">
          <input
            type="checkbox"
            checked={allowAutoCreate}
            onChange={(e) => setAllowAutoCreate(e.target.checked)}
            className="mt-0.5 h-4 w-4 rounded border-slate-300 text-teal-600 focus:ring-teal-500/30"
          />
          <span>
            <span className="block text-sm font-medium text-slate-800">Auto-create repositories on push</span>
            <span className="mt-0.5 block text-xs text-slate-500">
              On (default), pushing to a new repo path creates a local repository of that format. Off, repositories
              must be created before pushing.
            </span>
          </span>
        </label>

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
