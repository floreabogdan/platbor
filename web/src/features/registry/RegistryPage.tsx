import { Fragment, useMemo, useState } from 'react';
import type { ReactNode } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { Button, Card, EmptyState, PageHeader } from '../../components/ui';
import { FileIcon, GoIcon, MavenIcon, NugetIcon, PackageIcon, PypiIcon, RegistryIcon, SearchIcon } from '../../components/icons';
import { cx } from '../../lib/cx';
import { formatBytes, formatRelativeTime } from '../../lib/format';
import type { GenericFile, GoModule, MavenArtifact, NpmPackage, NugetPackage, PyPIPackage, Repository } from '../../lib/types';
import { goHref, mavenHref, nugetHref, ociHref, packageHref, pypiHref } from './packageRoute';
import { useGenericFiles, useGoModules, useMavens, useNugets, usePackages, usePypis, useRepositories } from './useRegistry';

// The registry index. It lists every artifact — OCI container images, npm and
// NuGet packages, and generic files — in one browser rather than separate tabs:
// they are the same kind of thing (named, versioned content in a project), so a
// per-row format icon plus an optional format filter beats a hard split.
// Registries accumulate thousands of artifacts across a smaller number of
// projects, so the default is a "grouped" view — collapsible per-project
// sections with rollups, which answers "what's where" at a glance. A "flat"
// toggle swaps in one sortable table for cross-project questions ("the biggest
// artifact anywhere"). Both are one aligned table — never a literal table nested
// inside a cell.

export function RegistryPage() {
  const repos = useRepositories();
  const pkgs = usePackages();
  const nugets = useNugets();
  const pypis = usePypis();
  const mavens = useMavens();
  const gomods = useGoModules();
  const generics = useGenericFiles();

  // One combined list once every source is in. Each artifact carries a format
  // discriminator, its detail-route href, and a globally unique key.
  const artifacts = useMemo<Artifact[]>(() => {
    if (
      repos.state !== 'ready' ||
      pkgs.state !== 'ready' ||
      nugets.state !== 'ready' ||
      pypis.state !== 'ready' ||
      mavens.state !== 'ready' ||
      gomods.state !== 'ready' ||
      generics.state !== 'ready'
    ) {
      return [];
    }
    return [
      ...repos.repositories.map(fromRepository),
      ...pkgs.packages.map(fromPackage),
      ...nugets.packages.map(fromNuget),
      ...pypis.packages.map(fromPypi),
      ...mavens.artifacts.map(fromMaven),
      ...gomods.modules.map(fromGoModule),
      ...generics.files.map(fromGeneric),
    ];
  }, [
    repos.state,
    repos.repositories,
    pkgs.state,
    pkgs.packages,
    nugets.state,
    nugets.packages,
    pypis.state,
    pypis.packages,
    mavens.state,
    mavens.artifacts,
    gomods.state,
    gomods.modules,
    generics.state,
    generics.files,
  ]);

  const loading =
    repos.state === 'loading' ||
    pkgs.state === 'loading' ||
    nugets.state === 'loading' ||
    pypis.state === 'loading' ||
    mavens.state === 'loading' ||
    gomods.state === 'loading' ||
    generics.state === 'loading';
  const failed =
    repos.state === 'error' ||
    pkgs.state === 'error' ||
    nugets.state === 'error' ||
    pypis.state === 'error' ||
    mavens.state === 'error' ||
    gomods.state === 'error' ||
    generics.state === 'error';
  const errorMessage =
    repos.error ?? pkgs.error ?? nugets.error ?? pypis.error ?? mavens.error ?? gomods.error ?? generics.error;

  function reloadAll() {
    void repos.reload();
    void pkgs.reload();
    void nugets.reload();
    void pypis.reload();
    void mavens.reload();
    void gomods.reload();
    void generics.reload();
  }

  return (
    <div className="animate-rise">
      <PageHeader
        title="Registry"
        subtitle="Container images, npm, NuGet, PyPI and Maven packages, and generic files across every project."
      />

      {loading ? <TableSkeleton /> : null}

      {failed ? (
        <Card className="p-6">
          <p className="text-sm text-red-700">{errorMessage ?? 'Failed to load the registry.'}</p>
          <Button variant="secondary" className="mt-3" onClick={reloadAll}>
            Retry
          </Button>
        </Card>
      ) : null}

      {!loading && !failed && artifacts.length === 0 ? (
        <EmptyState
          icon={<RegistryIcon className="h-8 w-8" />}
          message="No artifacts yet. Create a project, then push to it — docker push, npm publish, dotnet nuget push, or a generic file upload."
        />
      ) : null}

      {!loading && !failed && artifacts.length > 0 ? <ArtifactBrowser artifacts={artifacts} /> : null}
    </div>
  );
}

// --- unified artifact model ---

type Format = 'oci' | 'npm' | 'nuget' | 'pypi' | 'maven' | 'go' | 'generic';

// Artifact is the row model both formats normalize to. `contents` is the
// format-specific rollup phrase ("3 tags" / "2 versions"); it is display-only
// (deliberately not sortable — the two counts are not comparable across formats).
interface Artifact {
  format: Format;
  projectKey: string;
  projectName: string;
  repoKey: string; // the typed repository the artifact lives in
  name: string; // oci: image; npm/nuget: package name; generic: file path
  kind: 'local' | 'proxy';
  contents: string;
  sizeBytes: number;
  updatedAt: string;
  href: string; // detail-route link; empty when the format has no detail page (generic)
  key: string; // unique across every format
}

function fromRepository(r: Repository): Artifact {
  return {
    format: 'oci',
    projectKey: r.projectKey,
    projectName: r.projectName,
    repoKey: r.repoKey,
    name: r.repository,
    kind: r.kind,
    contents: `${String(r.tagCount)} ${r.tagCount === 1 ? 'tag' : 'tags'}`,
    sizeBytes: r.sizeBytes,
    updatedAt: r.updatedAt,
    href: ociHref(r.projectKey, r.repoKey, r.repository),
    key: `oci:${r.projectKey}/${r.repoKey}/${r.repository}`,
  };
}

function fromPackage(p: NpmPackage): Artifact {
  return {
    format: 'npm',
    projectKey: p.projectKey,
    projectName: p.projectName,
    repoKey: p.repoKey,
    name: p.name,
    kind: p.kind,
    contents: `${String(p.versionCount)} ${p.versionCount === 1 ? 'version' : 'versions'}`,
    sizeBytes: p.sizeBytes,
    updatedAt: p.updatedAt,
    href: packageHref(p.projectKey, p.repoKey, p.name),
    key: `npm:${p.projectKey}/${p.repoKey}/${p.name}`,
  };
}

function fromNuget(p: NugetPackage): Artifact {
  return {
    format: 'nuget',
    projectKey: p.projectKey,
    projectName: p.projectName,
    repoKey: p.repoKey,
    name: p.id,
    kind: p.kind,
    contents: `${String(p.versionCount)} ${p.versionCount === 1 ? 'version' : 'versions'}`,
    sizeBytes: p.sizeBytes,
    updatedAt: p.updatedAt,
    href: nugetHref(p.projectKey, p.repoKey, p.id),
    key: `nuget:${p.projectKey}/${p.repoKey}/${p.id}`,
  };
}

function fromPypi(p: PyPIPackage): Artifact {
  return {
    format: 'pypi',
    projectKey: p.projectKey,
    projectName: p.projectName,
    repoKey: p.repoKey,
    name: p.name,
    kind: p.kind,
    contents: `${String(p.fileCount)} ${p.fileCount === 1 ? 'file' : 'files'}`,
    sizeBytes: p.sizeBytes,
    updatedAt: p.updatedAt,
    href: pypiHref(p.projectKey, p.repoKey, p.name),
    key: `pypi:${p.projectKey}/${p.repoKey}/${p.name}`,
  };
}

function fromMaven(a: MavenArtifact): Artifact {
  return {
    format: 'maven',
    projectKey: a.projectKey,
    projectName: a.projectName,
    repoKey: a.repoKey,
    name: `${a.groupId}:${a.artifactId}`,
    kind: a.kind,
    contents: `${String(a.versionCount)} ${a.versionCount === 1 ? 'version' : 'versions'}`,
    sizeBytes: a.sizeBytes,
    updatedAt: a.updatedAt,
    href: mavenHref(a.projectKey, a.repoKey, a.groupId, a.artifactId),
    key: `maven:${a.projectKey}/${a.repoKey}/${a.groupId}:${a.artifactId}`,
  };
}

function fromGoModule(m: GoModule): Artifact {
  return {
    format: 'go',
    projectKey: m.projectKey,
    projectName: m.projectName,
    repoKey: m.repoKey,
    name: m.module,
    kind: m.kind,
    contents: `${String(m.versionCount)} ${m.versionCount === 1 ? 'version' : 'versions'}`,
    sizeBytes: m.sizeBytes,
    updatedAt: m.updatedAt,
    href: goHref(m.projectKey, m.repoKey, m.module),
    key: `go:${m.projectKey}/${m.repoKey}/${m.module}`,
  };
}

function fromGeneric(f: GenericFile): Artifact {
  return {
    format: 'generic',
    projectKey: f.projectKey,
    projectName: f.projectName,
    repoKey: f.repoKey,
    name: f.path,
    kind: f.kind,
    // A generic file is a single file, not a container of versions/tags.
    contents: 'file',
    sizeBytes: f.sizeBytes,
    updatedAt: f.updatedAt,
    href: '', // generic files have no detail page — display only
    key: `generic:${f.projectKey}/${f.repoKey}/${f.path}`,
  };
}

// --- sorting ---

type SortKey = 'name' | 'project' | 'kind' | 'size' | 'updated';
type SortDir = 'asc' | 'desc';
type Sort = { key: SortKey; dir: SortDir };

// Text columns read best ascending; size and recency read best largest/newest
// first, so that is each column's default direction on first click.
const NUMERIC_KEYS: ReadonlySet<SortKey> = new Set<SortKey>(['size', 'updated']);

function compareBy(a: Artifact, b: Artifact, key: SortKey): number {
  switch (key) {
    case 'name':
      return a.name.localeCompare(b.name);
    case 'project':
      return a.projectKey.localeCompare(b.projectKey);
    case 'kind':
      return a.kind.localeCompare(b.kind);
    case 'size':
      return a.sizeBytes - b.sizeBytes;
    case 'updated':
      return new Date(a.updatedAt).getTime() - new Date(b.updatedAt).getTime();
  }
}

function sortArtifacts(list: Artifact[], sort: Sort): Artifact[] {
  const dir = sort.dir === 'asc' ? 1 : -1;
  return [...list].sort((a, b) => {
    const primary = compareBy(a, b, sort.key) * dir;
    return primary !== 0 ? primary : a.key.localeCompare(b.key);
  });
}

// --- browser ---

type View = 'grouped' | 'flat';
type FormatFilter = '' | Format;

interface RepoSubGroup {
  key: string; // repository key
  artifacts: Artifact[];
  sizeBytes: number;
}

interface ProjectGroup {
  key: string;
  name: string;
  repos: RepoSubGroup[];
  count: number; // total artifacts across the project's repos
  sizeBytes: number;
}

function ArtifactBrowser({ artifacts }: { artifacts: Artifact[] }) {
  const [view, setView] = useState<View>('grouped');
  const [query, setQuery] = useState<string>('');
  const [project, setProject] = useState<string>('');
  const [format, setFormat] = useState<FormatFilter>('');
  const [sort, setSort] = useState<Sort>({ key: 'name', dir: 'asc' });
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(new Set());

  // The project filter offers each distinct project once, ordered as the server
  // returned them (already project-sorted).
  const projects = useMemo(() => {
    const seen = new Map<string, string>();
    for (const a of artifacts) {
      if (!seen.has(a.projectKey)) {
        seen.set(a.projectKey, a.projectName);
      }
    }
    return [...seen.entries()].map(([key, name]) => ({ key, name }));
  }, [artifacts]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return artifacts.filter((a) => {
      if (project !== '' && a.projectKey !== project) {
        return false;
      }
      if (format !== '' && a.format !== format) {
        return false;
      }
      if (q === '') {
        return true;
      }
      return (
        a.name.toLowerCase().includes(q) ||
        a.projectKey.toLowerCase().includes(q) ||
        a.projectName.toLowerCase().includes(q)
      );
    });
  }, [artifacts, query, project, format]);

  const sorted = useMemo(() => sortArtifacts(filtered, sort), [filtered, sort]);

  // Grouped view: bucket the sorted rows by project, then by the repository they
  // live in (so within-group order still follows the active sort). Project and
  // repository sections are in stable key order.
  const groups = useMemo<ProjectGroup[]>(() => {
    const byProject = new Map<string, { name: string; repos: Map<string, RepoSubGroup> }>();
    for (const a of sorted) {
      let p = byProject.get(a.projectKey);
      if (!p) {
        p = { name: a.projectName, repos: new Map() };
        byProject.set(a.projectKey, p);
      }
      let r = p.repos.get(a.repoKey);
      if (!r) {
        r = { key: a.repoKey, artifacts: [], sizeBytes: 0 };
        p.repos.set(a.repoKey, r);
      }
      r.artifacts.push(a);
      r.sizeBytes += a.sizeBytes;
    }
    return [...byProject.entries()]
      .map(([key, p]) => {
        const repos = [...p.repos.values()].sort((a, b) => a.key.localeCompare(b.key));
        return {
          key,
          name: p.name,
          repos,
          count: repos.reduce((n, r) => n + r.artifacts.length, 0),
          sizeBytes: repos.reduce((n, r) => n + r.sizeBytes, 0),
        };
      })
      .sort((a, b) => a.key.localeCompare(b.key));
  }, [sorted]);

  function toggleSort(key: SortKey) {
    setSort((prev) =>
      prev.key === key
        ? { key, dir: prev.dir === 'asc' ? 'desc' : 'asc' }
        : { key, dir: NUMERIC_KEYS.has(key) ? 'desc' : 'asc' },
    );
  }

  function toggleGroup(key: string) {
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(key)) {
        next.delete(key);
      } else {
        next.add(key);
      }
      return next;
    });
  }

  const allCollapsed = collapsed.size >= groups.length && groups.length > 0;

  function toggleAll() {
    setCollapsed(allCollapsed ? new Set() : new Set(groups.map((g) => g.key)));
  }

  return (
    <div className="space-y-4">
      <Toolbar
        view={view}
        onView={setView}
        query={query}
        onQuery={setQuery}
        project={project}
        onProject={setProject}
        projects={projects}
        format={format}
        onFormat={setFormat}
        shown={filtered.length}
        total={artifacts.length}
        projectCount={groups.length}
        allCollapsed={allCollapsed}
        onToggleAll={toggleAll}
      />

      <Card className="overflow-hidden">
        <div className="overflow-x-auto">
          {view === 'flat' ? (
            <FlatTable rows={sorted} sort={sort} onSort={toggleSort} />
          ) : (
            <GroupedTable groups={groups} sort={sort} onSort={toggleSort} collapsed={collapsed} onToggle={toggleGroup} />
          )}
        </div>
      </Card>
    </div>
  );
}

// --- flat table ---

function FlatTable({ rows, sort, onSort }: { rows: Artifact[]; sort: Sort; onSort: (k: SortKey) => void }) {
  const navigate = useNavigate();
  return (
    <table className="w-full text-sm">
      <thead className="sticky top-0 z-10 bg-white">
        <tr className="border-b border-slate-200/80 text-left text-xs uppercase tracking-wide text-slate-400">
          <SortableTh label="Name" col="name" sort={sort} onSort={onSort} />
          <SortableTh label="Project" col="project" sort={sort} onSort={onSort} />
          <PlainTh label="Contents" />
          <SortableTh label="Type" col="kind" sort={sort} onSort={onSort} />
          <SortableTh label="Size" col="size" sort={sort} onSort={onSort} align="right" />
          <SortableTh label="Updated" col="updated" sort={sort} onSort={onSort} align="right" />
        </tr>
      </thead>
      <tbody>
        {rows.length === 0 ? (
          <EmptyRow colSpan={6} />
        ) : (
          rows.map((a) => <ArtifactRow key={a.key} artifact={a} showProject navigate={navigate} />)
        )}
      </tbody>
    </table>
  );
}

// --- grouped table ---

function GroupedTable({
  groups,
  sort,
  onSort,
  collapsed,
  onToggle,
}: {
  groups: ProjectGroup[];
  sort: Sort;
  onSort: (k: SortKey) => void;
  collapsed: ReadonlySet<string>;
  onToggle: (key: string) => void;
}) {
  const navigate = useNavigate();
  return (
    <table className="w-full text-sm">
      <thead className="sticky top-0 z-10 bg-white">
        <tr className="border-b border-slate-200/80 text-left text-xs uppercase tracking-wide text-slate-400">
          <SortableTh label="Name" col="name" sort={sort} onSort={onSort} />
          <PlainTh label="Contents" />
          <SortableTh label="Type" col="kind" sort={sort} onSort={onSort} />
          <SortableTh label="Size" col="size" sort={sort} onSort={onSort} align="right" />
          <SortableTh label="Updated" col="updated" sort={sort} onSort={onSort} align="right" />
        </tr>
      </thead>
      {groups.length === 0 ? (
        <tbody>
          <EmptyRow colSpan={5} />
        </tbody>
      ) : (
        groups.map((g) => {
          const open = !collapsed.has(g.key);
          return (
            <tbody key={g.key}>
              {/* Project header: name + rollup on the left; the project's total
                  size aligns under the Size column, above the per-artifact sizes. */}
              <tr
                onClick={() => {
                  onToggle(g.key);
                }}
                className="cursor-pointer border-b border-slate-200/70 bg-slate-50/60 transition-colors hover:bg-slate-100/70"
              >
                <td colSpan={3} className="py-2.5 pl-4 pr-3">
                  <div className="flex items-center">
                    <span className="w-5 shrink-0 text-slate-400">{open ? '▾' : '▸'}</span>
                    <span className="mr-2 font-semibold text-slate-900">{g.name}</span>
                    <span className="rounded-md bg-slate-200/70 px-1.5 py-0.5 font-mono text-xs text-slate-600">
                      {g.key}
                    </span>
                    <span className="ml-2 font-mono text-xs text-slate-400">
                      {g.repos.length} {g.repos.length === 1 ? 'repo' : 'repos'} · {g.count}{' '}
                      {g.count === 1 ? 'artifact' : 'artifacts'}
                    </span>
                  </div>
                </td>
                <td className="px-4 py-2.5 text-right font-mono text-xs tabular-nums text-slate-500">
                  {formatBytes(g.sizeBytes)}
                </td>
                <td className="px-4 py-2.5" />
              </tr>
              {open
                ? g.repos.map((repo) => (
                    <Fragment key={repo.key}>
                      {/* Repository sub-header: the typed repo the rows below live
                          in, indented under its project. */}
                      <tr className="border-b border-slate-100 bg-white">
                        <td colSpan={3} className="py-1.5 pl-9 pr-3">
                          <div className="flex items-center">
                            <span className="font-mono text-xs font-medium text-slate-700">{repo.key}</span>
                            <span className="ml-2 font-mono text-[11px] text-slate-400">
                              {repo.artifacts.length}
                            </span>
                          </div>
                        </td>
                        <td className="px-4 py-1.5 text-right font-mono text-[11px] tabular-nums text-slate-400">
                          {formatBytes(repo.sizeBytes)}
                        </td>
                        <td className="px-4 py-1.5" />
                      </tr>
                      {repo.artifacts.map((a) => (
                        <ArtifactRow key={a.key} artifact={a} showProject={false} indent navigate={navigate} />
                      ))}
                    </Fragment>
                  ))
                : null}
            </tbody>
          );
        })
      )}
    </table>
  );
}

// --- shared row ---

type NavigateFn = ReturnType<typeof useNavigate>;

function ArtifactRow({
  artifact,
  showProject,
  indent = false,
  navigate,
}: {
  artifact: Artifact;
  showProject: boolean;
  indent?: boolean;
  navigate: NavigateFn;
}) {
  const { href } = artifact;
  // Most formats have a detail page; a generic file does not (href is empty), so
  // its row is display-only — no navigation, name shown as plain text.
  const clickable = href !== '';
  return (
    <tr
      onClick={
        clickable
          ? () => {
              navigate(href);
            }
          : undefined
      }
      className={cx(
        'border-b border-slate-100 transition-colors last:border-0',
        clickable ? 'cursor-pointer hover:bg-slate-50' : '',
      )}
    >
      {/* The format glyph sits in a fixed gutter so the icon makes the format
          scannable; in the grouped view rows indent to sit under their repo
          sub-header. */}
      <td className={cx('py-2.5 pr-3', indent ? 'pl-9' : 'pl-4')}>
        <div className="flex items-center">
          <FormatGlyph format={artifact.format} />
          {clickable ? (
            <Link
              to={href}
              onClick={(e) => e.stopPropagation()}
              className="font-mono font-medium text-slate-900 hover:text-teal-700"
            >
              {artifact.name}
            </Link>
          ) : (
            <span className="font-mono font-medium text-slate-700">{artifact.name}</span>
          )}
        </div>
      </td>
      {showProject ? (
        <Td>
          <span
            className="rounded-md bg-slate-100 px-1.5 py-0.5 font-mono text-xs text-slate-600"
            title={artifact.projectName}
          >
            {artifact.projectKey}/{artifact.repoKey}
          </span>
        </Td>
      ) : null}
      <Td className="text-slate-500">{artifact.contents}</Td>
      <Td>
        <TypeBadge kind={artifact.kind} />
      </Td>
      <Td className="text-right tabular-nums text-slate-600">{formatBytes(artifact.sizeBytes)}</Td>
      <Td className="text-right text-slate-500">
        <span title={artifact.updatedAt}>{formatRelativeTime(artifact.updatedAt)}</span>
      </Td>
    </tr>
  );
}

// FormatGlyph is the leading per-row icon — a distinct glyph per format so the
// format is scannable at a glance. It occupies the caret-width gutter so names
// stay aligned under their project header.
const FORMAT_META: Record<Format, { label: string; icon: ReactNode }> = {
  oci: { label: 'Container image', icon: <RegistryIcon className="h-4 w-4" /> },
  npm: { label: 'npm package', icon: <PackageIcon className="h-4 w-4" /> },
  nuget: { label: 'NuGet package', icon: <NugetIcon className="h-4 w-4" /> },
  pypi: { label: 'PyPI package', icon: <PypiIcon className="h-4 w-4" /> },
  maven: { label: 'Maven artifact', icon: <MavenIcon className="h-4 w-4" /> },
  go: { label: 'Go module', icon: <GoIcon className="h-4 w-4" /> },
  generic: { label: 'Generic file', icon: <FileIcon className="h-4 w-4" /> },
};

function FormatGlyph({ format }: { format: Format }) {
  const { label, icon } = FORMAT_META[format];
  return (
    <span className="flex w-5 shrink-0 items-center text-slate-400" role="img" aria-label={label} title={label}>
      {icon}
    </span>
  );
}

// --- toolbar ---

function Toolbar({
  view,
  onView,
  query,
  onQuery,
  project,
  onProject,
  projects,
  format,
  onFormat,
  shown,
  total,
  projectCount,
  allCollapsed,
  onToggleAll,
}: {
  view: View;
  onView: (v: View) => void;
  query: string;
  onQuery: (v: string) => void;
  project: string;
  onProject: (v: string) => void;
  projects: { key: string; name: string }[];
  format: FormatFilter;
  onFormat: (v: FormatFilter) => void;
  shown: number;
  total: number;
  projectCount: number;
  allCollapsed: boolean;
  onToggleAll: () => void;
}) {
  return (
    <div className="flex flex-wrap items-center gap-3">
      <ViewToggle view={view} onView={onView} />

      <div className="relative min-w-[12rem] flex-1">
        <SearchIcon className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400" />
        <input
          type="search"
          value={query}
          onChange={(e) => onQuery(e.target.value)}
          placeholder="Filter by name or project…"
          aria-label="Filter artifacts"
          className="w-full rounded-lg border border-slate-200 bg-white py-2 pl-9 pr-3 text-sm text-slate-800 placeholder:text-slate-400 focus:border-teal-400 focus:outline-none focus:ring-2 focus:ring-teal-500/20"
        />
      </div>

      <select
        value={format}
        onChange={(e) => onFormat(e.target.value as FormatFilter)}
        aria-label="Filter by format"
        className="rounded-lg border border-slate-200 bg-white py-2 pl-3 pr-8 text-sm text-slate-700 focus:border-teal-400 focus:outline-none focus:ring-2 focus:ring-teal-500/20"
      >
        <option value="">All formats</option>
        <option value="oci">Container images</option>
        <option value="npm">npm packages</option>
        <option value="nuget">NuGet packages</option>
        <option value="pypi">PyPI packages</option>
        <option value="maven">Maven artifacts</option>
        <option value="go">Go modules</option>
        <option value="generic">Generic files</option>
      </select>

      <select
        value={project}
        onChange={(e) => onProject(e.target.value)}
        aria-label="Filter by project"
        className="rounded-lg border border-slate-200 bg-white py-2 pl-3 pr-8 text-sm text-slate-700 focus:border-teal-400 focus:outline-none focus:ring-2 focus:ring-teal-500/20"
      >
        <option value="">All projects</option>
        {projects.map((p) => (
          <option key={p.key} value={p.key}>
            {p.name}
          </option>
        ))}
      </select>

      {view === 'grouped' && projectCount > 1 ? (
        <button
          type="button"
          onClick={onToggleAll}
          className="rounded-lg border border-slate-200 bg-white px-3 py-2 text-xs font-medium text-slate-600 transition-colors hover:bg-slate-50"
        >
          {allCollapsed ? 'Expand all' : 'Collapse all'}
        </button>
      ) : null}

      <span className="shrink-0 font-mono text-xs text-slate-400">
        {shown === total
          ? `${String(total)} ${total === 1 ? 'artifact' : 'artifacts'}`
          : `${String(shown)} of ${String(total)}`}
        {view === 'grouped' ? ` · ${String(projectCount)} ${projectCount === 1 ? 'project' : 'projects'}` : ''}
      </span>
    </div>
  );
}

function ViewToggle({ view, onView }: { view: View; onView: (v: View) => void }) {
  return (
    <div className="inline-flex shrink-0 rounded-lg border border-slate-200 bg-white p-0.5">
      {(['grouped', 'flat'] as const).map((v) => (
        <button
          key={v}
          type="button"
          onClick={() => onView(v)}
          className={cx(
            'rounded-md px-3 py-1.5 text-xs font-medium capitalize transition-colors',
            view === v ? 'bg-slate-900 text-white' : 'text-slate-500 hover:text-slate-800',
          )}
        >
          {v}
        </button>
      ))}
    </div>
  );
}

// --- small building blocks ---

function TypeBadge({ kind }: { kind: Artifact['kind'] }) {
  const proxy = kind === 'proxy';
  return (
    <span
      className={cx(
        'inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ring-1 ring-inset',
        proxy ? 'bg-amber-100 text-amber-700 ring-amber-600/20' : 'bg-slate-100 text-slate-600 ring-slate-500/20',
      )}
      title={proxy ? 'Cached from an upstream registry' : 'Pushed directly to this registry'}
    >
      {proxy ? 'Proxy' : 'Local'}
    </span>
  );
}

// PlainTh is a non-sortable column header (Contents, whose meaning differs per
// format, so a cross-format ordering would be meaningless).
function PlainTh({ label }: { label: string }) {
  return <th className="px-4 py-2.5 text-left font-medium uppercase tracking-wide">{label}</th>;
}

// SortableTh is a column header that sorts on click and shows the active
// direction with a caret.
function SortableTh({
  label,
  col,
  sort,
  onSort,
  align = 'left',
}: {
  label: string;
  col: SortKey;
  sort: Sort;
  onSort: (key: SortKey) => void;
  align?: 'left' | 'right';
}) {
  const active = sort.key === col;
  return (
    <th className={cx('px-4 py-2.5 font-medium', align === 'right' ? 'text-right' : 'text-left')}>
      <button
        type="button"
        onClick={() => onSort(col)}
        className={cx(
          'inline-flex items-center gap-1 uppercase tracking-wide transition-colors hover:text-slate-600',
          align === 'right' ? 'flex-row-reverse' : '',
          active ? 'text-slate-600' : '',
        )}
      >
        {label}
        <span className={cx('text-[10px]', active ? 'text-teal-600' : 'text-transparent')}>
          {active && sort.dir === 'asc' ? '▲' : '▼'}
        </span>
      </button>
    </th>
  );
}

function Td({ children, className }: { children: ReactNode; className?: string }) {
  return <td className={cx('px-4 py-2.5', className)}>{children}</td>;
}

function EmptyRow({ colSpan }: { colSpan: number }) {
  return (
    <tr>
      <td colSpan={colSpan} className="px-4 py-10 text-center text-sm text-slate-400">
        No artifacts match your filters.
      </td>
    </tr>
  );
}

function TableSkeleton() {
  return (
    <div className="space-y-4">
      <div className="flex gap-3">
        <div className="h-9 w-36 animate-pulse rounded-lg bg-slate-100" />
        <div className="h-9 flex-1 animate-pulse rounded-lg bg-slate-100" />
        <div className="h-9 w-40 animate-pulse rounded-lg bg-slate-100" />
      </div>
      <Card className="p-4">
        <div className="space-y-2">
          {[0, 1, 2, 3, 4, 5].map((i) => (
            <div key={i} className="h-8 animate-pulse rounded bg-slate-50" />
          ))}
        </div>
      </Card>
    </div>
  );
}
