import type { ReactNode } from 'react';
import { useParams } from 'react-router-dom';
import { Breadcrumb, Card, CopyButton, EmptyState, PageHeader } from '../../components/ui';
import { LayersIcon, RegistryIcon } from '../../components/icons';
import { cx } from '../../lib/cx';
import { formatBytes } from '../../lib/format';
import type { MavenFile } from '../../lib/types';
import { splitRepoAndRest } from './packageRoute';
import { useMavenDetail } from './useRegistry';

// Maven artifact detail: the files (poms, jars, checksums, metadata) of one
// groupId:artifactId, plus the <dependency> snippet a consumer needs. An artifact
// lives in a typed repository, identified by (project, repo, groupId:artifactId);
// the route splat carries "<repo>/<groupId>:<artifactId>".
export function MavenPage() {
  const params = useParams();
  const project = params.project ?? '';
  const { repo, rest } = splitRepoAndRest(params['*'] ?? '');
  const [group, artifact] = splitCoordinate(rest);

  const { detail, state, error } = useMavenDetail(project, repo, group, artifact);

  if (!project || !repo || !group || !artifact) {
    return <EmptyState message="No artifact selected." />;
  }

  const latest = detail ? latestVersion(detail.files) : '';

  return (
    <div className="animate-rise">
      <Breadcrumb
        items={[
          { label: 'Registry', to: '/registry' },
          { label: `${project}/${repo}`, to: '/registry' },
          { label: `${group}:${artifact}` },
        ]}
      />
      <PageHeader title={`${group}:${artifact}`} subtitle={`Maven artifact in ${project}/${repo}.`} />

      {state === 'loading' ? <Card className="h-40 animate-pulse bg-slate-50" /> : null}
      {state === 'error' ? (
        <Card className="p-6">
          <p className="text-sm text-red-700">{error ?? 'Failed to load artifact.'}</p>
        </Card>
      ) : null}

      {state === 'ready' && detail ? (
        detail.files.length === 0 ? (
          <EmptyState icon={<RegistryIcon className="h-8 w-8" />} message="This artifact has no files yet." />
        ) : (
          <div className="space-y-5">
            <DependencySnippet group={group} artifact={artifact} version={latest} />
            <FilesTable files={detail.files} />
          </div>
        )
      ) : null}
    </div>
  );
}

// splitCoordinate splits "<groupId>:<artifactId>" into its two parts.
function splitCoordinate(coord: string): [string, string] {
  const i = coord.lastIndexOf(':');
  if (i < 0) {
    return [coord, ''];
  }
  return [coord.slice(0, i), coord.slice(i + 1)];
}

// latestVersion returns the newest non-empty version among the files (they are
// listed newest-first by the API).
function latestVersion(files: MavenFile[]): string {
  for (const f of files) {
    if (!f.isMetadata && f.version !== '') {
      return f.version;
    }
  }
  return '';
}

// DependencySnippet gives the Maven <dependency> block a consumer pastes into a
// pom, using the artifact's latest version.
function DependencySnippet({ group, artifact, version }: { group: string; artifact: string; version: string }) {
  const snippet = [
    '<dependency>',
    `  <groupId>${group}</groupId>`,
    `  <artifactId>${artifact}</artifactId>`,
    `  <version>${version || 'VERSION'}</version>`,
    '</dependency>',
  ].join('\n');

  return (
    <Card className="p-6">
      <div className="flex items-center gap-2">
        <LayersIcon className="h-5 w-5 text-slate-400" />
        <h2 className="font-semibold text-slate-900">Dependency</h2>
      </div>
      <p className="mt-1 text-sm text-slate-500">
        Add this repository to your pom (or settings.xml), then declare the dependency.
      </p>
      <div className="mt-4 flex items-start justify-between gap-2 rounded-lg bg-ink-900 px-3 py-2.5">
        <pre className="overflow-x-auto whitespace-pre font-mono text-xs leading-relaxed text-slate-200">{snippet}</pre>
        <CopyButton value={snippet} label="Copy" className="shrink-0 text-slate-400 hover:bg-white/10 hover:text-white" />
      </div>
    </Card>
  );
}

function FilesTable({ files }: { files: MavenFile[] }) {
  return (
    <Card className="overflow-hidden">
      <div className="border-b border-slate-200/80 px-4 py-2.5 text-xs font-medium uppercase tracking-wide text-slate-400">
        {files.length} {files.length === 1 ? 'file' : 'files'}
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-slate-200/80 text-left text-xs uppercase tracking-wide text-slate-400">
              <Th>File</Th>
              <Th>Version</Th>
              <Th>Type</Th>
              <Th className="text-right">Size</Th>
            </tr>
          </thead>
          <tbody>
            {files.map((f) => (
              <tr key={f.path} className="border-b border-slate-100 last:border-0">
                <Td>
                  <span className="font-mono text-slate-900">{f.filename}</span>
                </Td>
                <Td className="font-mono text-slate-600">{f.version || '—'}</Td>
                <Td className="text-slate-500">{f.isMetadata ? 'metadata' : 'artifact'}</Td>
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
