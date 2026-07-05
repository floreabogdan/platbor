import { useEffect, useState } from 'react';
import { api, ApiError } from '../../lib/api';
import { Button, Card, CopyButton } from '../../components/ui';
import { TrashIcon } from '../../components/icons';
import type { Webhook } from '../../lib/types';

// WebhooksPanel manages a project's event subscriptions. Like the members and
// storage panels it is manage-only: the webhooks API returns 403 to non-managers,
// so they see nothing here. A new webhook's signing secret is shown once.
export function WebhooksPanel({ projectKey }: { projectKey: string }) {
  const [webhooks, setWebhooks] = useState<Webhook[]>([]);
  const [state, setState] = useState<'loading' | 'ready' | 'forbidden' | 'error'>('loading');
  const [error, setError] = useState<string>();
  const [newSecret, setNewSecret] = useState<{ id: string; secret: string }>();

  async function load() {
    try {
      setWebhooks((await api.listWebhooks(projectKey)).webhooks);
      setState('ready');
    } catch (err) {
      if (err instanceof ApiError && err.status === 403) {
        setState('forbidden');
        return;
      }
      setError(err instanceof Error ? err.message : 'Failed to load webhooks');
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
      <h2 className="mb-1 font-semibold text-slate-900">Webhooks</h2>
      <p className="mb-4 text-sm text-slate-500">
        POST a signed JSON event to a URL when artifacts in this project change. Each delivery carries an{' '}
        <code className="font-mono text-xs">X-Platbor-Signature</code> HMAC-SHA256 header.
      </p>

      {state === 'error' ? <p className="mb-3 text-sm text-red-700">{error}</p> : null}

      {newSecret ? (
        <div className="mb-4 rounded-lg border border-teal-200 bg-teal-50/60 p-3">
          <p className="text-sm text-slate-700">
            Signing secret (shown once — store it now):
          </p>
          <div className="mt-2 flex items-center justify-between gap-2 rounded-md bg-ink-900 px-3 py-2">
            <code className="truncate font-mono text-xs text-slate-200">{newSecret.secret}</code>
            <CopyButton value={newSecret.secret} label="Copy" className="shrink-0 text-slate-400 hover:text-white" />
          </div>
        </div>
      ) : null}

      {webhooks.length > 0 ? (
        <div className="overflow-hidden rounded-lg border border-slate-200/80">
          <table className="w-full text-sm">
            <tbody>
              {webhooks.map((wh) => (
                <WebhookRow key={wh.id} projectKey={projectKey} webhook={wh} onChanged={() => void load()} />
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <p className="text-sm text-slate-400">No webhooks yet.</p>
      )}

      <AddWebhookForm
        projectKey={projectKey}
        onAdded={(created) => {
          if (created.secret) setNewSecret({ id: created.id, secret: created.secret });
          void load();
        }}
      />
    </Card>
  );
}

function WebhookRow({
  projectKey,
  webhook,
  onChanged,
}: {
  projectKey: string;
  webhook: Webhook;
  onChanged: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();

  async function remove() {
    setBusy(true);
    try {
      await api.deleteWebhook(projectKey, webhook.id);
      onChanged();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to delete webhook');
      setBusy(false);
    }
  }

  return (
    <tr className="border-b border-slate-100 last:border-0">
      <td className="px-4 py-2.5">
        <span className="font-mono text-slate-900">{webhook.url}</span>
        <span className="ml-2 rounded bg-slate-100 px-1.5 py-0.5 font-mono text-xs text-slate-500">
          {webhook.events}
        </span>
        {error ? <span className="ml-2 text-xs text-red-600">{error}</span> : null}
      </td>
      <td className="px-4 py-2.5 text-right">
        <button
          type="button"
          onClick={() => void remove()}
          disabled={busy}
          aria-label={`Delete webhook ${webhook.url}`}
          className="rounded-md p-1.5 text-slate-400 transition-colors hover:bg-red-50 hover:text-red-600"
        >
          <TrashIcon className="h-4 w-4" />
        </button>
      </td>
    </tr>
  );
}

function AddWebhookForm({ projectKey, onAdded }: { projectKey: string; onAdded: (created: Webhook) => void }) {
  const [url, setUrl] = useState('');
  const [events, setEvents] = useState('*');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();

  async function add() {
    if (!url.trim()) return;
    setBusy(true);
    setError(undefined);
    try {
      const created = await api.createWebhook(projectKey, { url: url.trim(), events: events.trim() || '*' });
      setUrl('');
      setEvents('*');
      onAdded(created);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to add webhook');
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="mt-4">
      <div className="flex flex-wrap items-center gap-2">
        <input
          type="url"
          value={url}
          placeholder="https://example.com/hook"
          aria-label="Webhook URL"
          onChange={(e) => setUrl(e.target.value)}
          className="min-w-[16rem] flex-1 rounded-md border border-slate-200 px-3 py-1.5 text-sm text-slate-700 placeholder:text-slate-400 focus:border-teal-500 focus:outline-none"
        />
        <input
          type="text"
          value={events}
          placeholder="* or oci.,generic."
          aria-label="Event filter"
          onChange={(e) => setEvents(e.target.value)}
          className="w-40 rounded-md border border-slate-200 px-3 py-1.5 font-mono text-sm text-slate-700 placeholder:text-slate-400 focus:border-teal-500 focus:outline-none"
        />
        <Button onClick={() => void add()} disabled={busy || !url.trim()}>
          {busy ? 'Adding…' : 'Add webhook'}
        </Button>
      </div>
      <p className="mt-2 text-xs text-slate-400">
        Events: <code className="font-mono">*</code> for all, or comma-separated action prefixes (e.g.{' '}
        <code className="font-mono">oci.,generic.push</code>).
      </p>
      {error ? <p className="mt-2 text-sm text-red-700">{error}</p> : null}
    </div>
  );
}
