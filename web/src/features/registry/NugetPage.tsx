import type { ReactNode } from 'react';
import { useParams } from 'react-router-dom';
import { Breadcrumb, Card, CopyButton, EmptyState, PageHeader } from '../../components/ui';
import { LayersIcon, RegistryIcon } from '../../components/icons';
import { cx } from '../../lib/cx';
import { formatBytes, formatDate } from '../../lib/format';
import type { NugetPackageVersion } from '../../lib/types';
import { splitRepoAndRest } from './packageRoute';
import { useNugetDetail } from './useRegistry';

// NuGet package detail: the versions of a package plus the feed-config and
// install commands a consumer needs. A package lives in a typed repository, so
// it is identified by (project, repo, id); the route splat carries "<repo>/<id>".
export function NugetPage() {
  const params = useParams();
  const project = params.project ?? '';
  const { repo, rest: id } = splitRepoAndRest(params['*'] ?? '');

  const { detail, state, error } = useNugetDetail(project, repo, id);

  if (!project || !repo || !id) {
    return <EmptyState message="No package selected." />;
  }

  return (
    <div className="animate-rise">
      <Breadcrumb
        items={[
          { label: 'Registry', to: '/registry' },
          { label: `${project}/${repo}`, to: '/registry' },
          { label: id },
        ]}
      />
      <PageHeader title={id} subtitle={`NuGet package in ${project}/${repo}.`} />

      {state === 'loading' ? <Card className="h-40 animate-pulse bg-slate-50" /> : null}
      {state === 'error' ? (
        <Card className="p-6">
          <p className="text-sm text-red-700">{error ?? 'Failed to load package.'}</p>
        </Card>
      ) : null}

      {state === 'ready' && detail ? (
        detail.versions.length === 0 ? (
          <EmptyState icon={<RegistryIcon className="h-8 w-8" />} message="This package has no versions yet." />
        ) : (
          <div className="space-y-5">
            <InstallSnippet project={project} repo={repo} id={id} />
            <VersionsTable versions={detail.versions} />
          </div>
        )
      ) : null}
    </div>
  );
}

// InstallSnippet gives the two commands a consumer needs: register this
// repository's feed as a NuGet source, then add the package.
function InstallSnippet({ project, repo, id }: { project: string; repo: string; id: string }) {
  const index = `${window.location.origin}/nuget/${project}/${repo}/v3/index.json`;
  const sourceCommand = `dotnet nuget add source ${index} --name ${project}-${repo}`;
  const addCommand = `dotnet add package ${id}`;

  return (
    <Card className="p-6">
      <div className="flex items-center gap-2">
        <LayersIcon className="h-5 w-5 text-slate-400" />
        <h2 className="font-semibold text-slate-900">Install</h2>
      </div>
      <p className="mt-1 text-sm text-slate-500">Register this project&apos;s feed, then add the package.</p>
      <div className="mt-4 space-y-2">
        <CommandLine command={sourceCommand} />
        <CommandLine command={addCommand} />
      </div>
    </Card>
  );
}

function CommandLine({ command }: { command: string }) {
  return (
    <div className="flex items-center justify-between gap-2 rounded-lg bg-ink-900 px-3 py-2.5">
      <code className="truncate font-mono text-xs text-slate-200">{command}</code>
      <CopyButton value={command} label="Copy" className="shrink-0 text-slate-400 hover:bg-white/10 hover:text-white" />
    </div>
  );
}

function VersionsTable({ versions }: { versions: NugetPackageVersion[] }) {
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
              <Th className="text-right">Size</Th>
              <Th>Published</Th>
            </tr>
          </thead>
          <tbody>
            {versions.map((v) => (
              <tr key={v.version} className="border-b border-slate-100 last:border-0">
                <Td>
                  <span className="font-mono font-medium text-slate-900">{v.version}</span>
                </Td>
                <Td className="text-right tabular-nums text-slate-600">{formatBytes(v.sizeBytes)}</Td>
                <Td className="text-slate-500">{formatDate(v.publishedAt)}</Td>
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
