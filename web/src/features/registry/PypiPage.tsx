import type { ReactNode } from 'react';
import { useParams } from 'react-router-dom';
import { Breadcrumb, Card, CopyButton, EmptyState, PageHeader } from '../../components/ui';
import { LayersIcon, RegistryIcon } from '../../components/icons';
import { cx } from '../../lib/cx';
import { formatBytes } from '../../lib/format';
import type { PyPIFile } from '../../lib/types';
import { splitRepoAndRest } from './packageRoute';
import { usePypiDetail } from './useRegistry';

// PyPI package detail: the distribution files (wheels and sdists) of a package
// plus the index config + install command a consumer needs. A package lives in a
// typed repository, identified by (project, repo, name); the route splat carries
// "<repo>/<name>".
export function PypiPage() {
  const params = useParams();
  const project = params.project ?? '';
  const { repo, rest: name } = splitRepoAndRest(params['*'] ?? '');

  const { detail, state, error } = usePypiDetail(project, repo, name);

  if (!project || !repo || !name) {
    return <EmptyState message="No package selected." />;
  }

  return (
    <div className="animate-rise">
      <Breadcrumb
        items={[
          { label: 'Registry', to: '/registry' },
          { label: `${project}/${repo}`, to: '/registry' },
          { label: name },
        ]}
      />
      <PageHeader title={name} subtitle={`PyPI package in ${project}/${repo}.`} />

      {state === 'loading' ? <Card className="h-40 animate-pulse bg-slate-50" /> : null}
      {state === 'error' ? (
        <Card className="p-6">
          <p className="text-sm text-red-700">{error ?? 'Failed to load package.'}</p>
        </Card>
      ) : null}

      {state === 'ready' && detail ? (
        detail.files.length === 0 ? (
          <EmptyState icon={<RegistryIcon className="h-8 w-8" />} message="This package has no distributions yet." />
        ) : (
          <div className="space-y-5">
            <InstallSnippet project={project} repo={repo} name={name} />
            <FilesTable files={detail.files} />
          </div>
        )
      ) : null}
    </div>
  );
}

// InstallSnippet gives the command a consumer needs: point pip at this
// repository's simple index, then install.
function InstallSnippet({ project, repo, name }: { project: string; repo: string; name: string }) {
  const index = `${window.location.origin}/pypi/${project}/${repo}/simple/`;
  const command = `pip install --index-url ${index} ${name}`;

  return (
    <Card className="p-6">
      <div className="flex items-center gap-2">
        <LayersIcon className="h-5 w-5 text-slate-400" />
        <h2 className="font-semibold text-slate-900">Install</h2>
      </div>
      <p className="mt-1 text-sm text-slate-500">
        Point pip at this repository&apos;s simple index (credentials go in the URL or ~/.netrc).
      </p>
      <div className="mt-4">
        <CommandLine command={command} />
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

function FilesTable({ files }: { files: PyPIFile[] }) {
  return (
    <Card className="overflow-hidden">
      <div className="border-b border-slate-200/80 px-4 py-2.5 text-xs font-medium uppercase tracking-wide text-slate-400">
        {files.length} {files.length === 1 ? 'distribution' : 'distributions'}
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-slate-200/80 text-left text-xs uppercase tracking-wide text-slate-400">
              <Th>File</Th>
              <Th>Version</Th>
              <Th>Requires-Python</Th>
              <Th className="text-right">Size</Th>
            </tr>
          </thead>
          <tbody>
            {files.map((f) => (
              <tr key={f.filename} className="border-b border-slate-100 last:border-0">
                <Td>
                  <span className="font-mono text-slate-900">{f.filename}</span>
                </Td>
                <Td className="font-mono text-slate-600">{f.version || '—'}</Td>
                <Td className="text-slate-500">{f.requiresPython || '—'}</Td>
                <Td className="text-right tabular-nums text-slate-600">{formatBytes(f.sizeBytes)}</Td>
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
