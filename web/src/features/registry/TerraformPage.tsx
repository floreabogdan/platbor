import type { ReactNode } from 'react';
import { useParams } from 'react-router-dom';
import { Breadcrumb, Card, CopyButton, EmptyState, PageHeader } from '../../components/ui';
import { LayersIcon, RegistryIcon } from '../../components/icons';
import { cx } from '../../lib/cx';
import { formatBytes } from '../../lib/format';
import type { TerraformVersion } from '../../lib/types';
import { splitRepoAndRest } from './packageRoute';
import { useTerraformModuleDetail } from './useRegistry';

// Terraform module detail: the uploaded versions of one module plus the module
// block a consumer needs. A module is identified by (project, repo, name,
// provider); the route splat carries "<repo>/<name>/<provider>". The namespace
// terraform addresses the module by is the project key.
export function TerraformPage() {
  const params = useParams();
  const project = params.project ?? '';
  const { repo, rest } = splitRepoAndRest(params['*'] ?? '');
  const [name, provider] = splitNameProvider(rest);

  const { detail, state, error } = useTerraformModuleDetail(project, repo, name, provider);

  if (!project || !repo || !name || !provider) {
    return <EmptyState message="No module selected." />;
  }

  const latest = detail?.versions[0]?.version ?? '';

  return (
    <div className="animate-rise">
      <Breadcrumb
        items={[
          { label: 'Registry', to: '/registry' },
          { label: `${project}/${repo}`, to: '/registry' },
          { label: `${name}/${provider}` },
        ]}
      />
      <PageHeader title={`${name}/${provider}`} subtitle={`Terraform module in ${project}/${repo}.`} />

      {state === 'loading' ? <Card className="h-40 animate-pulse bg-slate-50" /> : null}
      {state === 'error' ? (
        <Card className="p-6">
          <p className="text-sm text-red-700">{error ?? 'Failed to load module.'}</p>
        </Card>
      ) : null}

      {state === 'ready' && detail ? (
        detail.versions.length === 0 ? (
          <EmptyState icon={<RegistryIcon className="h-8 w-8" />} message="This module has no versions yet." />
        ) : (
          <div className="space-y-5">
            <ModuleSnippet project={project} name={name} provider={provider} version={latest} />
            <VersionsTable versions={detail.versions} />
          </div>
        )
      ) : null}
    </div>
  );
}

// splitNameProvider splits "<name>/<provider>".
function splitNameProvider(rest: string): [string, string] {
  const slash = rest.indexOf('/');
  if (slash < 0) {
    return [rest, ''];
  }
  return [rest.slice(0, slash), rest.slice(slash + 1)];
}

// ModuleSnippet gives the Terraform module block a consumer pastes into their
// config. The namespace is the project key; the registry host is this instance.
function ModuleSnippet({
  project,
  name,
  provider,
  version,
}: {
  project: string;
  name: string;
  provider: string;
  version: string;
}) {
  const source = `${window.location.host}/${project}/${name}/${provider}`;
  const snippet = ['module "' + name + '" {', `  source  = "${source}"`, `  version = "${version || '1.0.0'}"`, '}'].join(
    '\n',
  );

  return (
    <Card className="p-6">
      <div className="flex items-center gap-2">
        <LayersIcon className="h-5 w-5 text-slate-400" />
        <h2 className="font-semibold text-slate-900">Use this module</h2>
      </div>
      <p className="mt-1 text-sm text-slate-500">
        Reference the module by its registry address (the namespace is the project); credentials go in a{' '}
        <code className="font-mono text-xs">TF_TOKEN_*</code> variable or the CLI config.
      </p>
      <div className="mt-4 flex items-start justify-between gap-2 rounded-lg bg-ink-900 px-3 py-2.5">
        <pre className="overflow-x-auto whitespace-pre font-mono text-xs leading-relaxed text-slate-200">{snippet}</pre>
        <CopyButton value={snippet} label="Copy" className="shrink-0 text-slate-400 hover:bg-white/10 hover:text-white" />
      </div>
    </Card>
  );
}

function VersionsTable({ versions }: { versions: TerraformVersion[] }) {
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
            </tr>
          </thead>
          <tbody>
            {versions.map((v) => (
              <tr key={v.version} className="border-b border-slate-100 last:border-0">
                <Td>
                  <span className="font-mono text-slate-900">{v.version}</span>
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
