import { Card, PageHeader, StatusPill } from '../../components/ui';

// Placeholder flagship "everything at a glance" screen. Real metrics wire in as
// the registry and catalog land; the layout establishes the visual language now.
export function DashboardPage() {
  return (
    <div className="animate-rise">
      <PageHeader title="Dashboard" subtitle="Everything at a glance." />

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard label="Projects" value="0" />
        <StatCard label="Artifacts" value="0" />
        <StatCard label="Components" value="0" />
        <StatCard label="Vulnerabilities" value="0" />
      </div>

      <Card className="mt-6 p-6">
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-sm font-semibold text-slate-900">System</h2>
          <StatusPill status="success" label="Healthy" />
        </div>
        <dl className="grid grid-cols-2 gap-x-8 gap-y-3 text-sm sm:grid-cols-3">
          <Detail term="Version" value="0.0.0-dev" />
          <Detail term="Storage" value="filesystem" />
          <Detail term="Database" value="sqlite" />
        </dl>
      </Card>
    </div>
  );
}

function StatCard({ label, value }: { label: string; value: string }) {
  return (
    <Card className="p-5">
      <div className="font-mono text-[10px] uppercase tracking-[0.18em] text-slate-500">{label}</div>
      <div className="mt-2 text-3xl font-bold tracking-tight text-slate-900">{value}</div>
    </Card>
  );
}

function Detail({ term, value }: { term: string; value: string }) {
  return (
    <div>
      <dt className="text-xs text-slate-500">{term}</dt>
      <dd className="mt-0.5 font-mono text-sm text-slate-800">{value}</dd>
    </div>
  );
}
