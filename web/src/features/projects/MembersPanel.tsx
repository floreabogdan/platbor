import { useEffect, useState } from 'react';
import { api, ApiError } from '../../lib/api';
import { Button, Card } from '../../components/ui';
import { TrashIcon } from '../../components/icons';
import type { Member, MemberRole } from '../../lib/types';

const ROLES: MemberRole[] = ['reader', 'maintainer', 'admin'];

const ROLE_HINT: Record<MemberRole, string> = {
  reader: 'Pull artifacts',
  maintainer: 'Pull and push artifacts',
  admin: 'Push and configure the project',
};

// MembersPanel manages a project's RBAC: who can access it and with what role.
// It is only usable by a project admin (or instance admin); a non-admin who
// opens the project page sees a muted notice instead of the controls, because
// the members API returns 403 for them.
export function MembersPanel({ projectKey }: { projectKey: string }) {
  const [members, setMembers] = useState<Member[]>([]);
  const [state, setState] = useState<'loading' | 'ready' | 'forbidden' | 'error'>('loading');
  const [error, setError] = useState<string>();

  async function load() {
    try {
      const res = await api.listMembers(projectKey);
      setMembers(res.members);
      setState('ready');
    } catch (err) {
      if (err instanceof ApiError && err.status === 403) {
        setState('forbidden');
        return;
      }
      setError(err instanceof Error ? err.message : 'Failed to load members');
      setState('error');
    }
  }

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [projectKey]);

  if (state === 'loading' || state === 'forbidden') {
    // Readers/maintainers simply don't see member management.
    return null;
  }

  return (
    <Card className="mt-6 p-6">
      <div className="mb-1 flex items-center justify-between">
        <h2 className="font-semibold text-slate-900">Members</h2>
      </div>
      <p className="mb-4 text-sm text-slate-500">
        Roles govern access: readers pull, maintainers also push, admins also configure the project.
      </p>

      {state === 'error' ? <p className="mb-3 text-sm text-red-700">{error}</p> : null}

      {members.length > 0 ? (
        <div className="overflow-hidden rounded-lg border border-slate-200/80">
          <table className="w-full text-sm">
            <tbody>
              {members.map((m) => (
                <MemberRow key={m.username} projectKey={projectKey} member={m} onChanged={() => void load()} />
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <p className="text-sm text-slate-400">No members yet.</p>
      )}

      <AddMemberForm projectKey={projectKey} onAdded={() => void load()} />
    </Card>
  );
}

function MemberRow({
  projectKey,
  member,
  onChanged,
}: {
  projectKey: string;
  member: Member;
  onChanged: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();

  async function changeRole(role: MemberRole) {
    setBusy(true);
    setError(undefined);
    try {
      await api.setMember(projectKey, member.username, role);
      onChanged();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to update role');
    } finally {
      setBusy(false);
    }
  }

  async function remove() {
    setBusy(true);
    try {
      await api.removeMember(projectKey, member.username);
      onChanged();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to remove member');
      setBusy(false);
    }
  }

  return (
    <tr className="border-b border-slate-100 last:border-0">
      <td className="px-4 py-2.5">
        <span className="font-medium text-slate-900">{member.username}</span>
        {member.email ? <span className="ml-2 text-xs text-slate-400">{member.email}</span> : null}
        {error ? <span className="ml-2 text-xs text-red-600">{error}</span> : null}
      </td>
      <td className="px-4 py-2.5 text-right">
        <select
          aria-label={`Role for ${member.username}`}
          value={member.role}
          disabled={busy}
          onChange={(e) => void changeRole(e.target.value as MemberRole)}
          className="rounded-md border border-slate-200 px-2 py-1 text-sm text-slate-700 focus:border-teal-500 focus:outline-none"
        >
          {ROLES.map((r) => (
            <option key={r} value={r}>
              {r}
            </option>
          ))}
        </select>
      </td>
      <td className="px-4 py-2.5 text-right">
        <button
          type="button"
          onClick={() => void remove()}
          disabled={busy}
          aria-label={`Remove ${member.username}`}
          className="rounded-md p-1.5 text-slate-400 transition-colors hover:bg-red-50 hover:text-red-600"
        >
          <TrashIcon className="h-4 w-4" />
        </button>
      </td>
    </tr>
  );
}

function AddMemberForm({ projectKey, onAdded }: { projectKey: string; onAdded: () => void }) {
  const [username, setUsername] = useState('');
  const [role, setRole] = useState<MemberRole>('reader');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();

  async function add() {
    if (!username.trim()) return;
    setBusy(true);
    setError(undefined);
    try {
      await api.setMember(projectKey, username.trim(), role);
      setUsername('');
      setRole('reader');
      onAdded();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to add member');
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="mt-4">
      <div className="flex flex-wrap items-center gap-2">
        <input
          type="text"
          value={username}
          placeholder="username"
          aria-label="Username to add"
          onChange={(e) => setUsername(e.target.value)}
          className="rounded-md border border-slate-200 px-3 py-1.5 text-sm text-slate-700 placeholder:text-slate-400 focus:border-teal-500 focus:outline-none"
        />
        <select
          aria-label="Role for new member"
          value={role}
          onChange={(e) => setRole(e.target.value as MemberRole)}
          className="rounded-md border border-slate-200 px-2 py-1.5 text-sm text-slate-700 focus:border-teal-500 focus:outline-none"
        >
          {ROLES.map((r) => (
            <option key={r} value={r}>
              {r} — {ROLE_HINT[r]}
            </option>
          ))}
        </select>
        <Button onClick={() => void add()} disabled={busy || !username.trim()}>
          {busy ? 'Adding…' : 'Add member'}
        </Button>
      </div>
      {error ? <p className="mt-2 text-sm text-red-700">{error}</p> : null}
    </div>
  );
}
