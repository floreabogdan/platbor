import type { ReactNode } from 'react';
import { Link } from 'react-router-dom';
import { Card, EmptyState, PageHeader, StatusPill } from '../../components/ui';
import { ProjectsIcon, RegistryIcon, TrashIcon } from '../../components/icons';
import { formatRelativeTime } from '../../lib/format';
import type { ActivityEntry } from '../../lib/types';
import { useDashboard } from './useDashboard';

// The "everything at a glance" screen: live counts and a recent-activity feed
// drawn from the audit log. Catalog and vulnerability tiles arrive with Phases
// 3–4; the numbers shown here are real today.
export function DashboardPage() {
  const { data, state, error, reload } = useDashboard();

  return (
    <div className="animate-rise">
      <PageHeader title="Dashboard" subtitle="Everything at a glance." />

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
        <StatCard label="Projects" value={data?.summary.projects} to="/projects" loading={state === 'loading'} />
        <StatCard label="Repositories" value={data?.summary.repositories} to="/registry" loading={state === 'loading'} />
        <StatCard label="Tags" value={data?.summary.tags} to="/registry" loading={state === 'loading'} />
      </div>

      <div className="mt-6 grid grid-cols-1 gap-6 lg:grid-cols-3">
        <Card className="p-6 lg:col-span-2">
          <div className="mb-4 flex items-center justify-between">
            <h2 className="text-sm font-semibold text-slate-900">Recent activity</h2>
            {state === 'error' ? (
              <button
                type="button"
                onClick={() => void reload()}
                className="text-xs font-medium text-teal-700 hover:underline"
              >
                Retry
              </button>
            ) : null}
          </div>
          <ActivityFeed
            entries={data?.activity ?? []}
            loading={state === 'loading'}
            error={state === 'error' ? (error ?? 'Failed to load activity.') : undefined}
          />
        </Card>

        <Card className="h-fit p-6">
          <div className="mb-4 flex items-center justify-between">
            <h2 className="text-sm font-semibold text-slate-900">System</h2>
            <StatusPill status="success" label="Healthy" />
          </div>
          <dl className="grid grid-cols-2 gap-x-8 gap-y-3 text-sm">
            <Detail term="Version" value="0.0.0-dev" />
            <Detail term="Storage" value="filesystem" />
            <Detail term="Database" value="sqlite" />
          </dl>
        </Card>
      </div>
    </div>
  );
}

function StatCard({
  label,
  value,
  to,
  loading,
}: {
  label: string;
  value?: number;
  to: string;
  loading: boolean;
}) {
  return (
    <Link
      to={to}
      className="group block rounded-2xl border border-slate-200/80 bg-white p-5 shadow-card transition-all hover:border-teal-300 hover:shadow-md"
    >
      <div className="font-mono text-[10px] uppercase tracking-[0.18em] text-slate-500">{label}</div>
      {loading ? (
        <div className="mt-3 h-8 w-12 animate-pulse rounded bg-slate-100" />
      ) : (
        <div className="mt-2 text-3xl font-bold tracking-tight text-slate-900 group-hover:text-teal-700">
          {value ?? 0}
        </div>
      )}
    </Link>
  );
}

function ActivityFeed({
  entries,
  loading,
  error,
}: {
  entries: ActivityEntry[];
  loading: boolean;
  error?: string;
}) {
  if (loading) {
    return (
      <div className="space-y-3">
        {[0, 1, 2].map((i) => (
          <div key={i} className="h-6 animate-pulse rounded bg-slate-50" />
        ))}
      </div>
    );
  }
  if (error) {
    return <p className="text-sm text-red-700">{error}</p>;
  }
  if (entries.length === 0) {
    return <EmptyState message="No activity yet. Push an image or create a project to get started." />;
  }
  return (
    <ul className="divide-y divide-slate-100">
      {entries.map((entry, i) => (
        <ActivityRow key={`${entry.action}-${entry.targetId}-${String(i)}`} entry={entry} />
      ))}
    </ul>
  );
}

function ActivityRow({ entry }: { entry: ActivityEntry }) {
  const { verb, target, to } = describeActivity(entry);
  return (
    <li className="flex items-center gap-3 py-2.5 text-sm">
      <span className="text-slate-400">{actionIcon(entry.action)}</span>
      <span className="min-w-0 flex-1 truncate text-slate-600">
        <span className="font-medium text-slate-900">{entry.actor}</span> {verb}{' '}
        {target ? (
          to ? (
            <Link to={to} className="font-mono text-slate-700 hover:text-teal-700">
              {target}
            </Link>
          ) : (
            <span className="font-mono text-slate-700">{target}</span>
          )
        ) : null}
      </span>
      <time className="shrink-0 text-xs text-slate-400">{formatRelativeTime(entry.at)}</time>
    </li>
  );
}

// describeActivity turns an audit action into a human phrase, plus a link to the
// affected resource when there is one.
function describeActivity(e: ActivityEntry): { verb: string; target?: string; to?: string } {
  const repo = e.metadata?.repository;
  const ref = e.metadata?.reference;
  const project = e.projectKey;

  switch (e.action) {
    case 'oci.manifest.push':
      if (project && repo) {
        return {
          verb: 'pushed',
          target: `${project}/${repo}${ref ? `:${ref}` : ''}`,
          to: `/registry/${project}/${repo}${ref ? `?ref=${ref}` : ''}`,
        };
      }
      return { verb: 'pushed a manifest' };
    case 'oci.manifest.delete':
      if (project && repo) {
        return { verb: 'deleted a manifest from', target: `${project}/${repo}`, to: `/registry/${project}/${repo}` };
      }
      return { verb: 'deleted a manifest' };
    case 'oci.tag.delete':
      if (project && repo) {
        return { verb: 'deleted tag', target: `${project}/${repo}:${e.targetId}` };
      }
      return { verb: 'deleted a tag' };
    case 'project.create':
      return { verb: 'created project', target: e.projectName ?? project };
    case 'project.create.proxy':
      return { verb: 'created proxy project', target: e.projectName ?? project };
    case 'registry.gc':
      return { verb: 'ran garbage collection' };
    default:
      return { verb: e.action };
  }
}

function actionIcon(action: string): ReactNode {
  if (action.endsWith('.delete') || action === 'registry.gc') {
    return <TrashIcon className="h-4 w-4" />;
  }
  if (action === 'project.create') {
    return <ProjectsIcon className="h-4 w-4" />;
  }
  return <RegistryIcon className="h-4 w-4" />;
}

function Detail({ term, value }: { term: string; value: string }) {
  return (
    <div>
      <dt className="text-xs text-slate-500">{term}</dt>
      <dd className="mt-0.5 font-mono text-sm text-slate-800">{value}</dd>
    </div>
  );
}
