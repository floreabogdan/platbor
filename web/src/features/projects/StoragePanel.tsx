import { useEffect, useState } from 'react';
import { api, ApiError } from '../../lib/api';
import { Button, Card } from '../../components/ui';
import { formatBytes } from '../../lib/format';
import type { ProjectUsage } from '../../lib/types';

const MB = 1024 * 1024;

// StoragePanel shows a project's storage usage against its quota and lets a
// project admin set the quota. Like MembersPanel it is manage-only: the usage
// API returns 403 to non-managers, so they see nothing here.
export function StoragePanel({ projectKey }: { projectKey: string }) {
  const [usage, setUsage] = useState<ProjectUsage>();
  const [state, setState] = useState<'loading' | 'ready' | 'forbidden' | 'error'>('loading');
  const [error, setError] = useState<string>();

  async function load() {
    try {
      setUsage(await api.getProjectUsage(projectKey));
      setState('ready');
    } catch (err) {
      if (err instanceof ApiError && err.status === 403) {
        setState('forbidden');
        return;
      }
      setError(err instanceof Error ? err.message : 'Failed to load storage');
      setState('error');
    }
  }

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [projectKey]);

  if (state === 'loading' || state === 'forbidden') {
    return null;
  }

  return (
    <Card className="mt-6 p-6">
      <h2 className="mb-1 font-semibold text-slate-900">Storage</h2>
      <p className="mb-4 text-sm text-slate-500">
        A quota caps the project&apos;s logical storage; once reached, further pushes are rejected until space is
        freed. Set 0 for unlimited.
      </p>

      {state === 'error' ? <p className="mb-3 text-sm text-red-700">{error}</p> : null}

      {usage ? (
        <>
          <UsageBar used={usage.usedBytes} quota={usage.quotaBytes} />
          <QuotaForm projectKey={projectKey} quotaBytes={usage.quotaBytes} onSaved={() => void load()} />
        </>
      ) : null}
    </Card>
  );
}

function UsageBar({ used, quota }: { used: number; quota: number }) {
  const unlimited = quota <= 0;
  const pct = unlimited ? 0 : Math.min(100, Math.round((used / quota) * 100));
  const over = !unlimited && used >= quota;
  return (
    <div className="mb-5">
      <div className="mb-1 flex items-baseline justify-between text-sm">
        <span className="font-mono text-slate-700">{formatBytes(used)} used</span>
        <span className="text-slate-400">
          {unlimited ? 'unlimited' : `of ${formatBytes(quota)} (${String(pct)}%)`}
        </span>
      </div>
      {!unlimited ? (
        <div className="h-2 w-full overflow-hidden rounded-full bg-slate-100">
          <div
            className={over ? 'h-full bg-red-500' : pct >= 80 ? 'h-full bg-amber-500' : 'h-full bg-teal-500'}
            style={{ width: `${String(Math.max(pct, 2))}%` }}
          />
        </div>
      ) : null}
    </div>
  );
}

function QuotaForm({
  projectKey,
  quotaBytes,
  onSaved,
}: {
  projectKey: string;
  quotaBytes: number;
  onSaved: () => void;
}) {
  const [mb, setMb] = useState<string>(quotaBytes > 0 ? String(Math.round(quotaBytes / MB)) : '0');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();

  async function save() {
    const n = Number(mb);
    if (!Number.isFinite(n) || n < 0) {
      setError('Enter a non-negative number of MB (0 = unlimited).');
      return;
    }
    setBusy(true);
    setError(undefined);
    try {
      await api.setProjectQuota(projectKey, Math.round(n) * MB);
      onSaved();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to set quota');
    } finally {
      setBusy(false);
    }
  }

  return (
    <div>
      <div className="flex flex-wrap items-center gap-2">
        <label htmlFor="quota-mb" className="text-sm text-slate-600">
          Quota
        </label>
        <input
          id="quota-mb"
          type="number"
          min={0}
          value={mb}
          aria-label="Quota in megabytes"
          onChange={(e) => setMb(e.target.value)}
          className="w-32 rounded-md border border-slate-200 px-3 py-1.5 text-sm text-slate-700 focus:border-teal-500 focus:outline-none"
        />
        <span className="text-sm text-slate-400">MB (0 = unlimited)</span>
        <Button onClick={() => void save()} disabled={busy}>
          {busy ? 'Saving…' : 'Save'}
        </Button>
      </div>
      {error ? <p className="mt-2 text-sm text-red-700">{error}</p> : null}
    </div>
  );
}
