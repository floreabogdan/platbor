import { useEffect, useState } from 'react';
import { api, ApiError } from '../../lib/api';
import { Button, Card } from '../../components/ui';
import type { Project } from '../../lib/types';

// TrustPanel lets a project admin set the cosign signature-verification public
// key used to check key-based signatures. Keyless signatures verify against their
// embedded certificate and need no key here. Manage-only: it probes the usage API
// (403 for non-managers) so members see nothing, matching StoragePanel.
export function TrustPanel({ project }: { project: Project }) {
  const [state, setState] = useState<'loading' | 'ready' | 'hidden'>('loading');
  const [configured, setConfigured] = useState(project.verificationKeyConfigured);
  const [keyText, setKeyText] = useState(project.verificationKey ?? '');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    let live = true;
    // Reuse the usage endpoint purely as a manager gate: a non-manager gets 403,
    // so the panel stays hidden. Any other failure also hides it (fail closed).
    async function gate() {
      try {
        await api.getProjectUsage(project.key);
        if (live) {
          setState('ready');
        }
      } catch {
        if (live) {
          setState('hidden');
        }
      }
    }
    void gate();
    return () => {
      live = false;
    };
  }, [project.key]);

  async function save(nextKey: string) {
    setBusy(true);
    setError(undefined);
    setSaved(false);
    try {
      const updated = await api.setVerificationKey(project.key, nextKey);
      setConfigured(updated.verificationKeyConfigured);
      setKeyText(updated.verificationKey ?? '');
      setSaved(true);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to save the key');
    } finally {
      setBusy(false);
    }
  }

  if (state !== 'ready') {
    return null;
  }

  return (
    <Card className="mt-6 p-6">
      <div className="mb-1 flex items-center gap-2">
        <h2 className="font-semibold text-slate-900">Signature verification</h2>
        {configured ? (
          <span className="inline-flex items-center rounded-full bg-emerald-100 px-2 py-0.5 text-xs font-medium text-emerald-700 ring-1 ring-inset ring-emerald-600/20">
            key configured
          </span>
        ) : null}
      </div>
      <p className="mb-4 text-sm text-slate-500">
        A public key (PEM) used to verify key-based cosign signatures on this project&apos;s images. Keyless
        signatures verify against their own certificate and need no key here.
      </p>

      <textarea
        value={keyText}
        onChange={(e) => setKeyText(e.target.value)}
        rows={5}
        spellCheck={false}
        placeholder={'-----BEGIN PUBLIC KEY-----\n...\n-----END PUBLIC KEY-----'}
        aria-label="Verification public key (PEM)"
        className="w-full rounded-md border border-slate-200 px-3 py-2 font-mono text-xs text-slate-700 focus:border-teal-500 focus:outline-none"
      />

      <div className="mt-3 flex items-center gap-2">
        <Button onClick={() => void save(keyText)} disabled={busy || keyText.trim() === ''}>
          {busy ? 'Saving…' : 'Save key'}
        </Button>
        {configured ? (
          <button
            type="button"
            onClick={() => void save('')}
            disabled={busy}
            className="rounded-lg px-3 py-2 text-sm font-medium text-slate-500 ring-1 ring-inset ring-slate-200 transition-colors hover:bg-slate-50"
          >
            Clear
          </button>
        ) : null}
        {saved ? <span className="text-sm text-emerald-600">Saved.</span> : null}
      </div>
      {error ? <p className="mt-2 text-sm text-red-700">{error}</p> : null}
    </Card>
  );
}
