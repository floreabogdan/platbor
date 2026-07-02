import { EmptyState, PageHeader } from '../../components/ui';
import { ProjectsIcon } from '../../components/icons';

// Projects list. Data fetching moves into a useProjects() hook once the
// /api/v1/projects endpoint exists (Phase 0 auth slice); for now it renders the
// empty state so the shell is navigable.
export function ProjectsPage() {
  return (
    <div className="animate-rise">
      <PageHeader
        title="Projects"
        subtitle="Every artifact and catalog entity is scoped to a project."
        actions={
          <button
            type="button"
            className="rounded-lg bg-teal-600 px-3.5 py-2 text-sm font-medium text-white shadow-sm transition-colors hover:bg-teal-700"
          >
            New project
          </button>
        }
      />
      <EmptyState
        icon={<ProjectsIcon className="h-8 w-8" />}
        message="No projects yet. Create one to start pushing artifacts and cataloging components."
        action={
          <button
            type="button"
            className="rounded-lg bg-teal-600 px-3.5 py-2 text-sm font-medium text-white shadow-sm transition-colors hover:bg-teal-700"
          >
            New project
          </button>
        }
      />
    </div>
  );
}
