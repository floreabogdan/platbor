import type { ReactNode } from 'react';
import { useParams } from 'react-router-dom';
import { Breadcrumb, Card, CopyButton, EmptyState, PageHeader } from '../../components/ui';
import { LayersIcon, RegistryIcon } from '../../components/icons';
import { cx } from '../../lib/cx';
import { formatBytes } from '../../lib/format';
import type { GoVersion } from '../../lib/types';
import { splitRepoAndRest } from './packageRoute';
import { useGoDetail } from './useRegistry';

// Go module detail: the cached versions of one module plus the GOPROXY snippet a
// consumer needs. A module lives in a proxy repository, identified by (project,
// repo, module); the route splat carries "<repo>/<module...>" (a module path
// contains slashes).
export function GoModulePage() {
  const params = useParams();
  const project = params.project ?? '';
  const { repo, rest: module } = splitRepoAndRest(params['*'] ?? '');

  const { detail, state, error } = useGoDetail(project, repo, module);

  if (!project || !repo || !module) {
    return <EmptyState message="No module selected." />;
  }

  const latest = detail?.versions[0]?.version ?? '';

  return (
    <div className="animate-rise">
      <Breadcrumb
        items={[
          { label: 'Registry', to: '/registry' },
          { label: `${project}/${repo}`, to: '/registry' },
          { label: module },
        ]}
      />
      <PageHeader title={module} subtitle={`Go module cached in ${project}/${repo}.`} />

      {state === 'loading' ? <Card className="h-40 animate-pulse bg-slate-50" /> : null}
      {state === 'error' ? (
        <Card className="p-6">
          <p className="text-sm text-red-700">{error ?? 'Failed to load module.'}</p>
        </Card>
      ) : null}

      {state === 'ready' && detail ? (
        detail.versions.length === 0 ? (
          <EmptyState icon={<RegistryIcon className="h-8 w-8" />} message="This module has no cached versions yet." />
        ) : (
          <div className="space-y-5">
            <GetSnippet project={project} repo={repo} module={module} version={latest} />
            <VersionsTable versions={detail.versions} />
          </div>
        )
      ) : null}
    </div>
  );
}

// GetSnippet gives the command a consumer needs: point GOPROXY at this repository,
// then go get the module.
function GetSnippet({
  project,
  repo,
  module,
  version,
}: {
  project: string;
  repo: string;
  module: string;
  version: string;
}) {
  const proxy = `${window.location.origin}/go/${project}/${repo}`;
  const command = `GOPROXY=${proxy} go get ${module}@${version || 'latest'}`;

  return (
    <Card className="p-6">
      <div className="flex items-center gap-2">
        <LayersIcon className="h-5 w-5 text-slate-400" />
        <h2 className="font-semibold text-slate-900">Fetch</h2>
      </div>
      <p className="mt-1 text-sm text-slate-500">
        Point GOPROXY at this repository (credentials go in ~/.netrc), then go get the module.
      </p>
      <div className="mt-4 flex items-center justify-between gap-2 rounded-lg bg-ink-900 px-3 py-2.5">
        <code className="truncate font-mono text-xs text-slate-200">{command}</code>
        <CopyButton value={command} label="Copy" className="shrink-0 text-slate-400 hover:bg-white/10 hover:text-white" />
      </div>
    </Card>
  );
}

function VersionsTable({ versions }: { versions: GoVersion[] }) {
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
              <Th>Archive</Th>
              <Th className="text-right">Cached size</Th>
            </tr>
          </thead>
          <tbody>
            {versions.map((v) => (
              <tr key={v.version} className="border-b border-slate-100 last:border-0">
                <Td>
                  <span className="font-mono text-slate-900">{v.version}</span>
                </Td>
                <Td className="text-slate-500">{v.hasZip ? 'zip cached' : 'metadata only'}</Td>
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
