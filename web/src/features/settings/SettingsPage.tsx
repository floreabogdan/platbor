import { useState } from 'react';
import { Button, Card, PageHeader } from '../../components/ui';
import { api } from '../../lib/api';
import { useAuth } from '../../lib/auth';
import { formatBytes } from '../../lib/format';
import type { GCResult } from '../../lib/types';

// Instance settings. Today this is the admin maintenance surface (garbage
// collection); user management and instance config land in later phases.
export function SettingsPage() {
  const { state } = useAuth();
  const isAdmin = state.status === 'authenticated' && state.user.isAdmin;

  return (
    <div className="animate-rise">
      <PageHeader title="Settings" subtitle="Instance administration and maintenance." />
      {isAdmin ? (
        <GarbageCollectionPanel />
      ) : (
        <Card className="p-6">
          <p className="text-sm text-slate-500">
            Instance administration is available to admins only.
          </p>
        </Card>
      )}
    </div>
  );
}

function GarbageCollectionPanel() {
  const [busy, setBusy] = useState<'preview' | 'run' | null>(null);
  const [result, setResult] = useState<GCResult>();
  const [error, setError] = useState<string>();

  async function run(dryRun: boolean) {
    setBusy(dryRun ? 'preview' : 'run');
    setError(undefined);
    try {
      setResult(await api.runGarbageCollection(dryRun));
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Garbage collection failed');
    } finally {
      setBusy(null);
    }
  }

  return (
    <Card className="p-6">
      <h2 className="text-sm font-semibold text-slate-900">Garbage collection</h2>
      <p className="mt-1 max-w-2xl text-sm text-slate-500">
        Reclaims disk space by deleting blobs (image layers and configs) that no manifest references —
        for example, layers left behind after a manifest was deleted. Blobs written in the last hour are
        always kept, so in-flight pushes are safe. Preview first to see what would be removed.
      </p>

      <div className="mt-4 flex items-center gap-2">
        <Button variant="secondary" disabled={busy !== null} onClick={() => void run(true)}>
          {busy === 'preview' ? 'Scanning…' : 'Preview'}
        </Button>
        <Button variant="danger" disabled={busy !== null} onClick={() => void run(false)}>
          {busy === 'run' ? 'Collecting…' : 'Run garbage collection'}
        </Button>
      </div>

      {error ? <p className="mt-4 text-sm text-red-700">{error}</p> : null}
      {result ? <GCResultLine result={result} /> : null}
    </Card>
  );
}

function GCResultLine({ result }: { result: GCResult }) {
  return (
    <div className="mt-4 rounded-lg bg-slate-50 p-4 text-sm text-slate-700">
      {result.dryRun ? (
        <p>
          Would remove <strong>{result.deleted}</strong> of {result.scanned} blobs, reclaiming{' '}
          <strong>{formatBytes(result.reclaimedBytes)}</strong>. Nothing was deleted.
        </p>
      ) : (
        <p>
          Removed <strong>{result.deleted}</strong> of {result.scanned} blobs, reclaiming{' '}
          <strong>{formatBytes(result.reclaimedBytes)}</strong>.
        </p>
      )}
    </div>
  );
}
