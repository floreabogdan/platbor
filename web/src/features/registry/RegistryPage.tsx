import { useMemo } from 'react';
import { Link } from 'react-router-dom';
import { Button, Card, EmptyState, PageHeader } from '../../components/ui';
import { RegistryIcon, TagIcon } from '../../components/icons';
import { formatBytes, formatDate } from '../../lib/format';
import type { Repository } from '../../lib/types';
import { useRepositories } from './useRegistry';

// The registry index: every repository, categorised under its project. This is
// the first place pushed artifacts become visible in the UI.
export function RegistryPage() {
  const { repositories, state, error, reload } = useRepositories();
  const groups = useGroupedByProject(repositories);

  return (
    <div className="animate-rise">
      <PageHeader
        title="Registry"
        subtitle="Container images and other artifacts, grouped by project."
      />

      {state === 'loading' ? <SkeletonGroups /> : null}

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

      {state === 'ready' && groups.length > 0 ? (
        <div className="space-y-8">
          {groups.map((group) => (
            <ProjectGroup key={group.projectKey} group={group} />
          ))}
        </div>
      ) : null}
    </div>
  );
}

interface Group {
  projectKey: string;
  projectName: string;
  repositories: Repository[];
}

// useGroupedByProject preserves the server's (project, repository) ordering while
// bucketing rows under their project.
function useGroupedByProject(repositories: Repository[]): Group[] {
  return useMemo(() => {
    const groups: Group[] = [];
    const byKey = new Map<string, Group>();
    for (const repo of repositories) {
      let group = byKey.get(repo.projectKey);
      if (!group) {
        group = { projectKey: repo.projectKey, projectName: repo.projectName, repositories: [] };
        byKey.set(repo.projectKey, group);
        groups.push(group);
      }
      group.repositories.push(repo);
    }
    return groups;
  }, [repositories]);
}

function ProjectGroup({ group }: { group: Group }) {
  return (
    <section>
      <header className="mb-3 flex items-baseline gap-2">
        <h2 className="text-sm font-semibold text-slate-900">{group.projectName}</h2>
        <span className="rounded-md bg-slate-100 px-1.5 py-0.5 font-mono text-xs text-slate-500">
          {group.projectKey}
        </span>
        <span className="text-xs text-slate-400">
          {group.repositories.length} {group.repositories.length === 1 ? 'repository' : 'repositories'}
        </span>
      </header>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {group.repositories.map((repo) => (
          <RepositoryCard key={`${repo.projectKey}/${repo.repository}`} repo={repo} />
        ))}
      </div>
    </section>
  );
}

function RepositoryCard({ repo }: { repo: Repository }) {
  return (
    <Link
      to={`/registry/${encodeURIComponent(repo.projectKey)}/${repo.repository}`}
      className="group block rounded-2xl border border-slate-200/80 bg-white p-5 shadow-card transition-all hover:border-teal-300 hover:shadow-md"
    >
      <h3 className="truncate font-semibold text-slate-900 group-hover:text-teal-700">
        {repo.repository}
      </h3>
      <div className="mt-3 flex items-center gap-4 text-xs text-slate-500">
        <span className="inline-flex items-center gap-1.5">
          <TagIcon className="h-4 w-4 text-slate-400" />
          {repo.tagCount} {repo.tagCount === 1 ? 'tag' : 'tags'}
        </span>
        <span>
          {repo.manifestCount} {repo.manifestCount === 1 ? 'manifest' : 'manifests'}
        </span>
      </div>
      <div className="mt-4 flex items-center justify-between text-xs text-slate-400">
        <span>Updated {formatDate(repo.updatedAt)}</span>
        <span className="font-mono text-slate-500">{formatBytes(repo.sizeBytes)}</span>
      </div>
    </Link>
  );
}

function SkeletonGroups() {
  return (
    <div className="space-y-8">
      {[0, 1].map((g) => (
        <div key={g}>
          <div className="mb-3 h-4 w-32 animate-pulse rounded bg-slate-100" />
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {[0, 1, 2].map((i) => (
              <Card key={i} className="h-28 animate-pulse bg-slate-50" />
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}
