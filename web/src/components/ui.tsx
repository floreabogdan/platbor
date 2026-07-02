// Shared UI primitives — the only place these visual patterns live (DRY).
// Every class here traces to a recipe in docs/DESIGN-SYSTEM.md.
import type { HTMLAttributes, ReactNode } from 'react';
import { cx } from '../lib/cx';

/** Card — the standard raised surface. */
export function Card({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cx('rounded-2xl border border-slate-200/80 bg-white shadow-card', className)}
      {...props}
    />
  );
}

/** PageHeader — title + optional subtitle on the left, actions on the right. */
export function PageHeader({
  title,
  subtitle,
  actions,
}: {
  title: string;
  subtitle?: string;
  actions?: ReactNode;
}) {
  return (
    <div className="mb-6 flex items-end justify-between gap-4">
      <div>
        <h1 className="text-2xl font-bold tracking-tight text-slate-900">{title}</h1>
        {subtitle ? <p className="mt-1 text-sm text-slate-500">{subtitle}</p> : null}
      </div>
      {actions ? <div className="flex items-center gap-2">{actions}</div> : null}
    </div>
  );
}

export type Status = 'success' | 'warning' | 'running' | 'failed' | 'neutral';

// Fixed mapping of semantic status to the canonical palette. Tailwind needs the
// full class strings present at build time, so these are spelled out, not
// interpolated.
const STATUS_STYLES: Record<Status, { pill: string; dot: string; pulse: boolean }> = {
  success: { pill: 'bg-emerald-50 text-emerald-700 ring-emerald-600/20', dot: 'bg-emerald-500', pulse: false },
  warning: { pill: 'bg-amber-50 text-amber-700 ring-amber-600/20', dot: 'bg-amber-500', pulse: true },
  running: { pill: 'bg-sky-50 text-sky-700 ring-sky-600/20', dot: 'bg-sky-500', pulse: true },
  failed: { pill: 'bg-red-50 text-red-700 ring-red-600/20', dot: 'bg-red-500', pulse: false },
  neutral: { pill: 'bg-slate-100 text-slate-600 ring-slate-600/20', dot: 'bg-slate-400', pulse: false },
};

/** StatusPill — semantic state as a dotted pill. */
export function StatusPill({ status, label }: { status: Status; label: string }) {
  const s = STATUS_STYLES[status];
  return (
    <span
      className={cx(
        'inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium ring-1 ring-inset',
        s.pill,
      )}
    >
      <span className={cx('h-1.5 w-1.5 rounded-full', s.dot, s.pulse && 'animate-pulse')} />
      {label}
    </span>
  );
}

/** EmptyState — never a bare "no data" string (docs/DESIGN-SYSTEM.md). */
export function EmptyState({
  icon,
  message,
  action,
}: {
  icon?: ReactNode;
  message: string;
  action?: ReactNode;
}) {
  return (
    <Card className="flex flex-col items-center gap-3 px-6 py-14 text-center">
      {icon ? <div className="text-slate-400">{icon}</div> : null}
      <p className="max-w-sm text-sm text-slate-500">{message}</p>
      {action}
    </Card>
  );
}
