import { useState } from 'react';
import { Button, Card, EmptyState } from '../../components/ui';
import { api } from '../../lib/api';
import { formatDate } from '../../lib/format';
import type { Token } from '../../lib/types';
import { useTokens } from './useTokens';
import { CreateTokenModal } from './CreateTokenModal';

// TokensPanel manages the current user's personal access tokens. It is one tab
// of the Profile page — PATs are personal, not instance settings.
export function TokensPanel() {
  const { tokens, state, error, reload } = useTokens();
  const [creating, setCreating] = useState(false);

  return (
    <div>
      <div className="mb-4 flex items-start justify-between gap-4">
        <p className="max-w-xl text-sm text-slate-500">
          Personal access tokens authenticate the CLI and scripts as you. Send one as{' '}
          <code className="rounded bg-slate-100 px-1 py-0.5 font-mono text-xs">
            Authorization: Bearer &lt;token&gt;
          </code>
          .
        </p>
        <Button onClick={() => setCreating(true)}>New token</Button>
      </div>

      {state === 'loading' ? <Card className="h-32 animate-pulse bg-slate-50" /> : null}

      {state === 'error' ? (
        <Card className="p-6">
          <p className="text-sm text-red-700">{error ?? 'Failed to load tokens.'}</p>
          <Button variant="secondary" className="mt-3" onClick={() => void reload()}>
            Retry
          </Button>
        </Card>
      ) : null}

      {state === 'ready' && tokens.length === 0 ? (
        <EmptyState
          message="No tokens yet. Create one to authenticate the CLI or scripts as you."
          action={<Button onClick={() => setCreating(true)}>New token</Button>}
        />
      ) : null}

      {state === 'ready' && tokens.length > 0 ? (
        <Card className="divide-y divide-slate-100">
          {tokens.map((token) => (
            <TokenRow key={token.id} token={token} onRevoked={() => void reload()} />
          ))}
        </Card>
      ) : null}

      {creating ? (
        <CreateTokenModal onClose={() => setCreating(false)} onCreated={() => void reload()} />
      ) : null}
    </div>
  );
}

function TokenRow({ token, onRevoked }: { token: Token; onRevoked: () => void }) {
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);

  async function revoke() {
    setBusy(true);
    try {
      await api.deleteToken(token.id);
      onRevoked();
    } catch {
      setBusy(false);
      setConfirming(false);
    }
  }

  return (
    <div className="flex items-center gap-4 px-5 py-4">
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate text-sm font-medium text-slate-900">{token.name}</span>
          <span className="rounded-md bg-slate-100 px-1.5 py-0.5 font-mono text-xs text-slate-600">
            {token.prefix}…
          </span>
        </div>
        <p className="mt-1 text-xs text-slate-400">
          Created {formatDate(token.createdAt)}
          {token.expiresAt ? ` · expires ${formatDate(token.expiresAt)}` : ' · no expiry'}
        </p>
      </div>

      {confirming ? (
        <div className="flex items-center gap-2">
          <span className="text-xs text-slate-500">Revoke?</span>
          <button
            type="button"
            onClick={() => void revoke()}
            disabled={busy}
            className="rounded-md bg-red-600 px-2.5 py-1 text-xs font-medium text-white transition-colors hover:bg-red-700 disabled:opacity-60"
          >
            {busy ? 'Revoking…' : 'Confirm'}
          </button>
          <button
            type="button"
            onClick={() => setConfirming(false)}
            disabled={busy}
            className="rounded-md px-2.5 py-1 text-xs font-medium text-slate-600 ring-1 ring-inset ring-slate-200 transition-colors hover:bg-slate-50"
          >
            Cancel
          </button>
        </div>
      ) : (
        <button
          type="button"
          onClick={() => setConfirming(true)}
          className="rounded-md px-2.5 py-1 text-xs font-medium text-red-600 ring-1 ring-inset ring-red-600/20 transition-colors hover:bg-red-50"
        >
          Revoke
        </button>
      )}
    </div>
  );
}
