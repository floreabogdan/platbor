import { EmptyState, PageHeader } from '../../components/ui';

// Generic "coming in a later phase" page for nav destinations whose feature has
// not been built yet. Keeps the shell fully navigable during early phases.
export function PlaceholderPage({ title, subtitle }: { title: string; subtitle: string }) {
  return (
    <div className="animate-rise">
      <PageHeader title={title} />
      <EmptyState message={subtitle} />
    </div>
  );
}
