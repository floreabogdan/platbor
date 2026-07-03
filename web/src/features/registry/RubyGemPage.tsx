import type { ReactNode } from 'react';
import { useParams } from 'react-router-dom';
import { Breadcrumb, Card, CopyButton, EmptyState, PageHeader } from '../../components/ui';
import { LayersIcon, RegistryIcon } from '../../components/icons';
import { cx } from '../../lib/cx';
import { formatBytes } from '../../lib/format';
import type { RubyGemVersion } from '../../lib/types';
import { splitRepoAndRest } from './packageRoute';
import { useRubyGemDetail } from './useRegistry';

// RubyGems gem detail: the published versions of one gem plus the install snippet
// a consumer needs. A gem lives in a typed repository, identified by (project,
// repo, name); the route splat carries "<repo>/<name>".
export function RubyGemPage() {
  const params = useParams();
  const project = params.project ?? '';
  const { repo, rest: name } = splitRepoAndRest(params['*'] ?? '');

  const { detail, state, error } = useRubyGemDetail(project, repo, name);

  if (!project || !repo || !name) {
    return <EmptyState message="No gem selected." />;
  }

  const latest = detail?.versions.find((v) => !v.yanked)?.number ?? detail?.versions[0]?.number ?? '';

  return (
    <div className="animate-rise">
      <Breadcrumb
        items={[
          { label: 'Registry', to: '/registry' },
          { label: `${project}/${repo}`, to: '/registry' },
          { label: name },
        ]}
      />
      <PageHeader title={name} subtitle={`Ruby gem in ${project}/${repo}.`} />

      {state === 'loading' ? <Card className="h-40 animate-pulse bg-slate-50" /> : null}
      {state === 'error' ? (
        <Card className="p-6">
          <p className="text-sm text-red-700">{error ?? 'Failed to load gem.'}</p>
        </Card>
      ) : null}

      {state === 'ready' && detail ? (
        detail.versions.length === 0 ? (
          <EmptyState icon={<RegistryIcon className="h-8 w-8" />} message="This gem has no versions yet." />
        ) : (
          <div className="space-y-5">
            <InstallSnippet project={project} repo={repo} name={name} version={latest} />
            <VersionsTable versions={detail.versions} />
          </div>
        )
      ) : null}
    </div>
  );
}

// InstallSnippet gives the Bundler source + gem line a consumer pastes into a
// Gemfile (Bundler speaks the compact index this repository serves).
function InstallSnippet({
  project,
  repo,
  name,
  version,
}: {
  project: string;
  repo: string;
  name: string;
  version: string;
}) {
  const source = `${window.location.origin}/rubygems/${project}/${repo}`;
  const snippet = ['# Gemfile', `source "${source}"`, `gem "${name}", "${version || '>= 0'}"`].join('\n');

  return (
    <Card className="p-6">
      <div className="flex items-center gap-2">
        <LayersIcon className="h-5 w-5 text-slate-400" />
        <h2 className="font-semibold text-slate-900">Install</h2>
      </div>
      <p className="mt-1 text-sm text-slate-500">
        Add this repository as a Bundler source (credentials go in the URL or ~/.gem/credentials).
      </p>
      <div className="mt-4 flex items-start justify-between gap-2 rounded-lg bg-ink-900 px-3 py-2.5">
        <pre className="overflow-x-auto whitespace-pre font-mono text-xs leading-relaxed text-slate-200">{snippet}</pre>
        <CopyButton value={snippet} label="Copy" className="shrink-0 text-slate-400 hover:bg-white/10 hover:text-white" />
      </div>
    </Card>
  );
}

function VersionsTable({ versions }: { versions: RubyGemVersion[] }) {
  return (
    <Card className="overflow-hidden">
      <div className="border-b border-slate-200/80 px-4 py-2.5 text-xs font-medium uppercase tracking-wide text-slate-400">
        {versions.length} {versions.length === 1 ? 'version' : 'versions'}
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-slate-200/80 text-left text-xs uppercase tracking-wide text-slate-400">
              <Th>Version</Th>
              <Th>Platform</Th>
              <Th>Status</Th>
              <Th className="text-right">Size</Th>
            </tr>
          </thead>
          <tbody>
            {versions.map((v) => (
              <tr key={v.number} className="border-b border-slate-100 last:border-0">
                <Td>
                  <span className="font-mono text-slate-900">{v.version}</span>
                </Td>
                <Td className="text-slate-500">{v.platform}</Td>
                <Td>
                  {v.yanked ? (
                    <span className="inline-flex items-center rounded-full bg-amber-100 px-2 py-0.5 text-xs font-medium text-amber-700 ring-1 ring-inset ring-amber-600/20">
                      Yanked
                    </span>
                  ) : (
                    <span className="text-slate-500">Published</span>
                  )}
                </Td>
                <Td className="text-right tabular-nums text-slate-600">{formatBytes(v.sizeBytes)}</Td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Card>
  );
}

function Th({ children, className }: { children: ReactNode; className?: string }) {
  return <th className={cx('px-4 py-2.5 font-medium', className)}>{children}</th>;
}

function Td({ children, className }: { children: ReactNode; className?: string }) {
  return <td className={cx('px-4 py-2.5', className)}>{children}</td>;
}
