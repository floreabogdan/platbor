// Shared UI primitives — the only place these visual patterns live (DRY).
// Every class here traces to a recipe in docs/DESIGN-SYSTEM.md.
import { forwardRef, useEffect, useRef } from 'react';
import type { ButtonHTMLAttributes, HTMLAttributes, ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { cx } from '../lib/cx';

/** Card — the standard raised surface. */
export const Card = forwardRef<HTMLDivElement, HTMLAttributes<HTMLDivElement>>(
  ({ className, ...props }, ref) => (
    <div
      ref={ref}
      className={cx('rounded-2xl border border-slate-200/80 bg-white shadow-card', className)}
      {...props}
    />
  ),
);
Card.displayName = 'Card';

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

type ButtonVariant = 'primary' | 'secondary';

const BUTTON_VARIANTS: Record<ButtonVariant, string> = {
  primary: 'bg-teal-600 text-white shadow-sm hover:bg-teal-700 disabled:hover:bg-teal-600',
  secondary: 'bg-white text-slate-700 ring-1 ring-inset ring-slate-200 hover:bg-slate-50',
};

/** Button — the standard action control. */
export function Button({
  variant = 'primary',
  className,
  type = 'button',
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: ButtonVariant }) {
  return (
    <button
      type={type}
      className={cx(
        'rounded-lg px-3.5 py-2 text-sm font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-60',
        BUTTON_VARIANTS[variant],
        className,
      )}
      {...props}
    />
  );
}

/** Modal — rendered via portal to document.body so it escapes transformed
 *  ancestors. Closes on Escape and backdrop click; focuses its panel on open. */
export function Modal({
  title,
  onClose,
  children,
}: {
  title: string;
  onClose: () => void;
  children: ReactNode;
}) {
  const panelRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    panelRef.current?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        onClose();
      }
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [onClose]);

  return createPortal(
    <div
      className="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-ink-900/50 p-6 backdrop-blur-sm"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) {
          onClose();
        }
      }}
    >
      <Card
        ref={panelRef}
        tabIndex={-1}
        role="dialog"
        aria-modal="true"
        aria-label={title}
        className="mt-16 w-full max-w-md p-6 outline-none animate-rise"
      >
        <h2 className="mb-4 text-lg font-semibold tracking-tight text-slate-900">{title}</h2>
        {children}
      </Card>
    </div>,
    document.body,
  );
}
