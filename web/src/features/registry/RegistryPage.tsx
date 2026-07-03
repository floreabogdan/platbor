import { useMemo, useState } from 'react';
import type { ReactNode } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { Button, Card, EmptyState, PageHeader } from '../../components/ui';
import { RegistryIcon, SearchIcon } from '../../components/icons';
import { cx } from '../../lib/cx';
import { formatBytes, formatRelativeTime } from '../../lib/format';
import type { Repository } from '../../lib/types';
import { useRepositories } from './useRegistry';

// The registry index. Registries accumulate thousands of repositories across a
// smaller number of projects, so the default is a "grouped" view — collapsible
// per-project sections with rollups, which answers "what's where" at a glance
// and matches the project-as-container model. A "flat" toggle swaps in one
// sortable table for cross-project questions ("the biggest repo anywhere").
// Both are one aligned table — never a literal table nested inside a cell.
export function RegistryPage() {
  const { repositories, state, error, reload } = useRepositories();

  return (
    <div className="animate-rise">
      <PageHeader
        title="Registry"
        subtitle="Container images and other artifacts across every project."
      />

      {state === 'loading' ? <TableSkeleton /> : null}

      {state === 'error' ? (
        <Card className="p-6">
          <p className="text-sm text-red-700">{error ?? 'Failed to load repositories.'}</p>
          <Button variant="secondary" className="mt-3" onClick={() => void reload()}>
            Retry
          </Button>
        </Card>
      ) : null}

      {state === 'ready' && repositories.length === 0 ? (
        <EmptyState
          icon={<RegistryIcon className="h-8 w-8" />}
          message="No artifacts yet. Create a project, then push an image to it with docker push <host>/<project>/<repo>:<tag>."
        />
      ) : null}

      {state === 'ready' && repositories.length > 0 ? (
        <RepositoryBrowser repositories={repositories} />
      ) : null}
    </div>
  );
}

// --- sorting ---

type SortKey = 'repository' | 'project' | 'kind' | 'tags' | 'manifests' | 'size' | 'updated';
type SortDir = 'asc' | 'desc';
type Sort = { key: SortKey; dir: SortDir };

// Text columns read best ascending; counts, size, and recency read best with the
// largest/newest first, so that is each column's default direction on first click.
const NUMERIC_KEYS: ReadonlySet<SortKey> = new Set<SortKey>(['tags', 'manifests', 'size', 'updated']);

function compareBy(a: Repository, b: Repository, key: SortKey): number {
  switch (key) {
    case 'repository':
      return a.repository.localeCompare(b.repository);
    case 'kind':
      return a.kind.localeCompare(b.kind);
    case 'tags':
      return a.tagCount - b.tagCount;
    case 'manifests':
      return a.manifestCount - b.manifestCount;
    case 'size':
      return a.sizeBytes - b.sizeBytes;
    case 'updated':
      return new Date(a.updatedAt).getTime() - new Date(b.updatedAt).getTime();
    case 'project':
      return a.projectKey.localeCompare(b.projectKey);
  }
}

// stableKey keeps sorting deterministic within ties: project, then repository.
function stableKey(r: Repository): string {
  return `${r.projectKey}/${r.repository}`;
}

function sortRepos(list: Repository[], sort: Sort): Repository[] {
  const dir = sort.dir === 'asc' ? 1 : -1;
  return [...list].sort((a, b) => {
    const primary = compareBy(a, b, sort.key) * dir;
    return primary !== 0 ? primary : stableKey(a).localeCompare(stableKey(b));
  });
}

// --- browser ---

type View = 'grouped' | 'flat';

interface ProjectGroup {
  key: string;
  name: string;
  repos: Repository[];
  sizeBytes: number;
}

function RepositoryBrowser({ repositories }: { repositories: Repository[] }) {
  const [view, setView] = useState<View>('grouped');
  const [query, setQuery] = useState<string>('');
  const [project, setProject] = useState<string>('');
  const [sort, setSort] = useState<Sort>({ key: 'repository', dir: 'asc' });
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(new Set());

  // The project filter offers each distinct project once, ordered as the server
  // returned them (already project-sorted).
  const projects = useMemo(() => {
    const seen = new Map<string, string>();
    for (const r of repositories) {
      if (!seen.has(r.projectKey)) {
        seen.set(r.projectKey, r.projectName);
      }
    }
    return [...seen.entries()].map(([key, name]) => ({ key, name }));
  }, [repositories]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return repositories.filter((r) => {
      if (project !== '' && r.projectKey !== project) {
        return false;
      }
      if (q === '') {
        return true;
      }
      return (
        r.repository.toLowerCase().includes(q) ||
        r.projectKey.toLowerCase().includes(q) ||
        r.projectName.toLowerCase().includes(q)
      );
    });
  }, [repositories, query, project]);

  const sorted = useMemo(() => sortRepos(filtered, sort), [filtered, sort]);

  // Grouped view: bucket the sorted rows by project (so within-group order still
  // follows the active sort), with project sections in stable key order.
  const groups = useMemo<ProjectGroup[]>(() => {
    const byKey = new Map<string, ProjectGroup>();
    for (const r of sorted) {
      let g = byKey.get(r.projectKey);
      if (!g) {
        g = { key: r.projectKey, name: r.projectName, repos: [], sizeBytes: 0 };
        byKey.set(r.projectKey, g);
      }
      g.repos.push(r);
      g.sizeBytes += r.sizeBytes;
    }
    return [...byKey.values()].sort((a, b) => a.key.localeCompare(b.key));
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
        shown={filtered.length}
        total={repositories.length}
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

function FlatTable({ rows, sort, onSort }: { rows: Repository[]; sort: Sort; onSort: (k: SortKey) => void }) {
  const navigate = useNavigate();
  return (
    <table className="w-full text-sm">
      <thead className="sticky top-0 z-10 bg-white">
        <tr className="border-b border-slate-200/80 text-left text-xs uppercase tracking-wide text-slate-400">
          <SortableTh label="Repository" col="repository" sort={sort} onSort={onSort} />
          <SortableTh label="Project" col="project" sort={sort} onSort={onSort} />
          <SortableTh label="Type" col="kind" sort={sort} onSort={onSort} />
          <SortableTh label="Tags" col="tags" sort={sort} onSort={onSort} align="right" />
          <SortableTh label="Manifests" col="manifests" sort={sort} onSort={onSort} align="right" />
          <SortableTh label="Size" col="size" sort={sort} onSort={onSort} align="right" />
          <SortableTh label="Updated" col="updated" sort={sort} onSort={onSort} align="right" />
        </tr>
      </thead>
      <tbody>
        {rows.length === 0 ? (
          <EmptyRow colSpan={7} />
        ) : (
          rows.map((repo) => <RepoRow key={stableKey(repo)} repo={repo} showProject navigate={navigate} />)
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
          <SortableTh label="Repository" col="repository" sort={sort} onSort={onSort} />
          <SortableTh label="Type" col="kind" sort={sort} onSort={onSort} />
          <SortableTh label="Tags" col="tags" sort={sort} onSort={onSort} align="right" />
          <SortableTh label="Manifests" col="manifests" sort={sort} onSort={onSort} align="right" />
          <SortableTh label="Size" col="size" sort={sort} onSort={onSort} align="right" />
          <SortableTh label="Updated" col="updated" sort={sort} onSort={onSort} align="right" />
        </tr>
      </thead>
      {groups.length === 0 ? (
        <tbody>
          <EmptyRow colSpan={6} />
        </tbody>
      ) : (
        groups.map((g) => {
          const open = !collapsed.has(g.key);
          return (
            <tbody key={g.key}>
              <tr
                onClick={() => {
                  onToggle(g.key);
                }}
                className="cursor-pointer border-b border-slate-200/70 bg-slate-50/60 transition-colors hover:bg-slate-100/70"
              >
                <td colSpan={6} className="px-4 py-2.5">
                  <div className="flex items-center gap-2">
                    <span className="text-slate-400">{open ? '▾' : '▸'}</span>
                    <span className="font-semibold text-slate-900">{g.name}</span>
                    <span className="rounded-md bg-slate-200/70 px-1.5 py-0.5 font-mono text-xs text-slate-600">
                      {g.key}
                    </span>
                    <span className="ml-auto font-mono text-xs text-slate-400">
                      {g.repos.length} {g.repos.length === 1 ? 'repo' : 'repos'} · {formatBytes(g.sizeBytes)}
                    </span>
                  </div>
                </td>
              </tr>
              {open
                ? g.repos.map((repo) => (
                    <RepoRow key={stableKey(repo)} repo={repo} showProject={false} navigate={navigate} />
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

function RepoRow({
  repo,
  showProject,
  navigate,
}: {
  repo: Repository;
  showProject: boolean;
  navigate: NavigateFn;
}) {
  const href = `/registry/${encodeURIComponent(repo.projectKey)}/${repo.repository}`;
  return (
    <tr
      onClick={() => {
        navigate(href);
      }}
      className="cursor-pointer border-b border-slate-100 transition-colors last:border-0 hover:bg-slate-50"
    >
      <Td>
        <Link
          to={href}
          onClick={(e) => e.stopPropagation()}
          className="font-mono font-medium text-slate-900 hover:text-teal-700"
        >
          {repo.repository}
        </Link>
      </Td>
      {showProject ? (
        <Td>
          <span
            className="rounded-md bg-slate-100 px-1.5 py-0.5 font-mono text-xs text-slate-600"
            title={repo.projectName}
          >
            {repo.projectKey}
          </span>
        </Td>
      ) : null}
      <Td>
        <TypeBadge kind={repo.kind} />
      </Td>
      <Td className="text-right tabular-nums text-slate-600">{repo.tagCount}</Td>
      <Td className="text-right tabular-nums text-slate-600">{repo.manifestCount}</Td>
      <Td className="text-right tabular-nums text-slate-600">{formatBytes(repo.sizeBytes)}</Td>
      <Td className="text-right text-slate-500">
        <span title={repo.updatedAt}>{formatRelativeTime(repo.updatedAt)}</span>
      </Td>
    </tr>
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
          placeholder="Filter by repository or project…"
          aria-label="Filter repositories"
          className="w-full rounded-lg border border-slate-200 bg-white py-2 pl-9 pr-3 text-sm text-slate-800 placeholder:text-slate-400 focus:border-teal-400 focus:outline-none focus:ring-2 focus:ring-teal-500/20"
        />
      </div>

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
          ? `${String(total)} ${total === 1 ? 'repository' : 'repositories'}`
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

function TypeBadge({ kind }: { kind: Repository['kind'] }) {
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
        No repositories match your filters.
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
