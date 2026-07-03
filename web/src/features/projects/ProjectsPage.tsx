import { useState } from 'react';
import { Button, Card, EmptyState, PageHeader } from '../../components/ui';
import { ProjectsIcon } from '../../components/icons';
import { formatDate } from '../../lib/format';
import type { Project } from '../../lib/types';
import { useProjects } from './useProjects';
import { CreateProjectModal } from './CreateProjectModal';

export function ProjectsPage() {
  const { projects, state, error, reload } = useProjects();
  const [creating, setCreating] = useState(false);

  return (
    <div className="animate-rise">
      <PageHeader
        title="Projects"
        subtitle="The tenant boundary that scopes every repository and catalog entity. Create and configure them here."
        actions={<Button onClick={() => setCreating(true)}>New project</Button>}
      />

      {state === 'loading' ? <SkeletonList /> : null}

      {state === 'error' ? (
        <Card className="p-6">
          <p className="text-sm text-red-700">{error ?? 'Failed to load projects.'}</p>
          <Button variant="secondary" className="mt-3" onClick={() => void reload()}>
            Retry
          </Button>
        </Card>
      ) : null}

      {state === 'ready' && projects.length === 0 ? (
        <EmptyState
          icon={<ProjectsIcon className="h-8 w-8" />}
          message="No projects yet. Create one to start pushing artifacts and cataloging components."
          action={<Button onClick={() => setCreating(true)}>New project</Button>}
        />
      ) : null}

      {state === 'ready' && projects.length > 0 ? (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {projects.map((p) => (
            <ProjectCard key={p.id} project={p} />
          ))}
        </div>
      ) : null}

      {creating ? (
        <CreateProjectModal onClose={() => setCreating(false)} onCreated={() => void reload()} />
      ) : null}
    </div>
  );
}

function ProjectCard({ project }: { project: Project }) {
  const isProxy = project.kind === 'proxy';
  return (
    <Card className="p-5">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <h3 className="truncate font-semibold text-slate-900">{project.name}</h3>
          <span className="mt-1 inline-block rounded-md bg-slate-100 px-1.5 py-0.5 font-mono text-xs text-slate-600">
            {project.key}
          </span>
        </div>
        {isProxy ? (
          <span className="shrink-0 rounded-full bg-amber-100 px-2 py-0.5 text-[11px] font-medium text-amber-700 ring-1 ring-inset ring-amber-600/20">
            Proxy
          </span>
        ) : null}
      </div>
      {project.description ? (
        <p className="mt-3 line-clamp-2 text-sm text-slate-500">{project.description}</p>
      ) : null}
      {isProxy && project.upstream ? (
        <p className="mt-3 truncate font-mono text-xs text-slate-500" title={project.upstream.url}>
          ↳ {project.upstream.url}
        </p>
      ) : null}
      <p className="mt-4 text-xs text-slate-400">Created {formatDate(project.createdAt)}</p>
    </Card>
  );
}

function SkeletonList() {
  return (
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
      {[0, 1, 2].map((i) => (
        <Card key={i} className="h-28 animate-pulse bg-slate-50" />
      ))}
    </div>
  );
}
