import { useState } from 'react';
import { Card, PageHeader } from '../../components/ui';
import { cx } from '../../lib/cx';
import { useAuth } from '../../lib/auth';
import { formatDate } from '../../lib/format';
import { TokensPanel } from './TokensPanel';

type Tab = 'account' | 'tokens';

const TABS: { id: Tab; label: string }[] = [
  { id: 'account', label: 'Account' },
  { id: 'tokens', label: 'Access tokens' },
];

export function ProfilePage() {
  const [tab, setTab] = useState<Tab>('account');

  return (
    <div className="animate-rise">
      <PageHeader title="Profile" subtitle="Your account and personal access tokens." />

      <div className="mb-6 flex gap-6 border-b border-slate-200">
        {TABS.map((t) => (
          <button
            key={t.id}
            type="button"
            onClick={() => setTab(t.id)}
            className={cx(
              '-mb-px border-b-2 pb-3 text-sm font-medium transition-colors',
              tab === t.id
                ? 'border-teal-600 text-slate-900'
                : 'border-transparent text-slate-500 hover:text-slate-800',
            )}
          >
            {t.label}
          </button>
        ))}
      </div>

      {tab === 'account' ? <AccountPanel /> : <TokensPanel />}
    </div>
  );
}

function AccountPanel() {
  const { state } = useAuth();
  if (state.status !== 'authenticated') {
    return null;
  }
  const user = state.user;

  return (
    <Card className="max-w-xl p-6">
      <dl className="grid grid-cols-1 gap-x-8 gap-y-4 sm:grid-cols-2">
        <Field term="Username" value={user.username} mono />
        <Field term="Email" value={user.email || '—'} />
        <Field term="Role" value={user.isAdmin ? 'Instance admin' : 'Member'} />
        <Field term="Member since" value={formatDate(user.createdAt)} />
      </dl>
    </Card>
  );
}

function Field({ term, value, mono }: { term: string; value: string; mono?: boolean }) {
  return (
    <div>
      <dt className="text-xs text-slate-500">{term}</dt>
      <dd className={cx('mt-0.5 text-sm text-slate-800', mono && 'font-mono')}>{value}</dd>
    </div>
  );
}
