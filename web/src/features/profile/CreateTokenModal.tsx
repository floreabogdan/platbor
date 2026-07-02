import { useState } from 'react';
import { api, ApiError } from '../../lib/api';
import { Button, Modal } from '../../components/ui';
import type { CreateTokenResponse } from '../../lib/types';

const EXPIRY_OPTIONS = [
  { label: 'No expiry', days: 0 },
  { label: '30 days', days: 30 },
  { label: '60 days', days: 60 },
  { label: '90 days', days: 90 },
];

export function CreateTokenModal({
  onClose,
  onCreated,
}: {
  onClose: () => void;
  onCreated: () => void;
}) {
  const [name, setName] = useState('');
  const [days, setDays] = useState(0);
  const [error, setError] = useState<string>();
  const [submitting, setSubmitting] = useState(false);
  const [created, setCreated] = useState<CreateTokenResponse>();

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setSubmitting(true);
    setError(undefined);
    try {
      const res = await api.createToken({ name, expiresInDays: days });
      setCreated(res);
      onCreated(); // refresh the list behind the modal
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Something went wrong');
      setSubmitting(false);
    }
  }

  return (
    <Modal title={created ? 'Token created' : 'New token'} onClose={onClose}>
      {created ? (
        <CreatedView token={created.token} onClose={onClose} />
      ) : (
        <form
          onSubmit={(e) => {
            void submit(e);
          }}
          className="space-y-4"
        >
          <div>
            <label htmlFor="token-name" className="mb-1 block text-sm font-medium text-slate-700">
              Name
            </label>
            <input
              id="token-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="CI pipeline"
              required
              autoFocus
              className={inputClass}
            />
            <p className="mt-1 text-xs text-slate-400">A label to recognize this token later.</p>
          </div>

          <div>
            <label htmlFor="token-expiry" className="mb-1 block text-sm font-medium text-slate-700">
              Expiration
            </label>
            <select
              id="token-expiry"
              value={days}
              onChange={(e) => setDays(Number(e.target.value))}
              className={inputClass}
            >
              {EXPIRY_OPTIONS.map((o) => (
                <option key={o.days} value={o.days}>
                  {o.label}
                </option>
              ))}
            </select>
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
              {submitting ? 'Creating…' : 'Create token'}
            </Button>
          </div>
        </form>
      )}
    </Modal>
  );
}

function CreatedView({ token, onClose }: { token: string; onClose: () => void }) {
  const [copied, setCopied] = useState(false);

  async function copy() {
    try {
      await navigator.clipboard.writeText(token);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  }

  return (
    <div className="space-y-4">
      <p className="text-sm text-slate-600">
        Copy this token now. For your security, it won&apos;t be shown again.
      </p>
      <div className="flex items-center gap-2 rounded-lg bg-ink-900 px-3 py-2.5">
        <code className="flex-1 break-all font-mono text-xs text-slate-200">{token}</code>
        <button
          type="button"
          onClick={() => void copy()}
          className="shrink-0 rounded-md px-2 py-1 text-xs font-medium text-slate-300 ring-1 ring-inset ring-white/15 transition-colors hover:bg-white/10 hover:text-white"
        >
          {copied ? 'Copied' : 'Copy'}
        </button>
      </div>
      <div className="flex justify-end">
        <Button onClick={onClose}>Done</Button>
      </div>
    </div>
  );
}

const inputClass =
  'w-full rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm text-slate-800 shadow-sm outline-none placeholder:text-slate-400 focus:border-teal-500 focus:ring-2 focus:ring-teal-500/20';
