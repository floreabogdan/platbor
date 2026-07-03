import type { ReactNode } from 'react';
import { useParams } from 'react-router-dom';
import { Breadcrumb, Card, CopyButton, EmptyState, PageHeader } from '../../components/ui';
import { Markdown } from '../../components/Markdown';
import { LayersIcon, RegistryIcon } from '../../components/icons';
import { cx } from '../../lib/cx';
import { formatBytes, formatDate } from '../../lib/format';
import type { NpmPackageVersion } from '../../lib/types';
import { splitRepoAndRest } from './packageRoute';
import { usePackageDetail } from './useRegistry';

// npm package detail: the versions of a package plus its dist-tags and the
// registry config + install command a consumer needs. A package lives in a
// typed repository, so it is identified by (project, repo, name); the route
// splat carries "<repo>/<name>" (the scoped name keeps its slash).
export function PackagePage() {
  const params = useParams();
  const project = params.project ?? '';
  const { repo, rest: name } = splitRepoAndRest(params['*'] ?? '');

  const { detail, state, error } = usePackageDetail(project, repo, name);

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
      <PageHeader title={name} subtitle={`npm package in ${project}/${repo}.`} />

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
            <InstallSnippet project={project} repo={repo} name={name} />
            {Object.keys(detail.distTags).length > 0 ? <DistTags distTags={detail.distTags} /> : null}
            {detail.readme ? <ReadmeCard source={detail.readme} /> : null}
            <VersionsTable versions={detail.versions} distTags={detail.distTags} />
          </div>
        )
      ) : null}
    </div>
  );
}

// InstallSnippet gives the two commands a consumer needs: point npm at this
// project's registry, then install. Scoped packages configure a per-scope
// registry so only that scope resolves here; unscoped packages set the default.
function InstallSnippet({ project, repo, name }: { project: string; repo: string; name: string }) {
  const registry = `${window.location.origin}/npm/${project}/${repo}/`;
  const scope = name.startsWith('@') ? name.slice(0, name.indexOf('/')) : '';
  const configCommand = scope
    ? `npm config set ${scope}:registry ${registry}`
    : `npm config set registry ${registry}`;
  const installCommand = `npm install ${name}`;

  return (
    <Card className="p-6">
      <div className="flex items-center gap-2">
        <LayersIcon className="h-5 w-5 text-slate-400" />
        <h2 className="font-semibold text-slate-900">Install</h2>
      </div>
      <p className="mt-1 text-sm text-slate-500">
        {scope
          ? `Point the ${scope} scope at this project, then install.`
          : 'Point npm at this project, then install.'}
      </p>
      <div className="mt-4 space-y-2">
        <CommandLine command={configCommand} />
        <CommandLine command={installCommand} />
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

// ReadmeCard renders the package's README beneath the install snippet.
function ReadmeCard({ source }: { source: string }) {
  return (
    <Card className="p-6">
      <div className="mb-3 text-xs font-medium uppercase tracking-wide text-slate-400">Readme</div>
      <Markdown source={source} className="space-y-3 text-sm text-slate-700" />
    </Card>
  );
}

function DistTags({ distTags }: { distTags: Record<string, string> }) {
  const entries = Object.entries(distTags).sort(([a], [b]) => a.localeCompare(b));
  return (
    <Card className="p-6">
      <div className="mb-3 text-xs font-medium uppercase tracking-wide text-slate-400">Dist-tags</div>
      <div className="flex flex-wrap gap-2">
        {entries.map(([tag, version]) => (
          <span
            key={tag}
            className="inline-flex items-center gap-1.5 rounded-full bg-teal-50 px-2.5 py-0.5 text-xs font-medium text-teal-700 ring-1 ring-inset ring-teal-600/20"
          >
            {tag}
            <span className="font-mono text-teal-600/70">{version}</span>
          </span>
        ))}
      </div>
    </Card>
  );
}

function VersionsTable({
  versions,
  distTags,
}: {
  versions: NpmPackageVersion[];
  distTags: Record<string, string>;
}) {
  // Invert dist-tags to version -> [tags] so each row can show its labels.
  const tagsByVersion = new Map<string, string[]>();
  for (const [tag, version] of Object.entries(distTags)) {
    const list = tagsByVersion.get(version) ?? [];
    list.push(tag);
    tagsByVersion.set(version, list);
  }

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
                  {(tagsByVersion.get(v.version) ?? []).map((tag) => (
                    <span
                      key={tag}
                      className="ml-2 inline-flex items-center rounded-full bg-teal-50 px-2 py-0.5 text-[11px] font-medium text-teal-700 ring-1 ring-inset ring-teal-600/20"
                    >
                      {tag}
                    </span>
                  ))}
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
