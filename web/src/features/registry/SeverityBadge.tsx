import { cx } from '../../lib/cx';
import type { Severity } from '../../lib/types';
import { SEVERITY_LABEL, SEVERITY_STYLES } from './severity';

// SeverityBadge renders a coloured pill for a vulnerability severity.
export function SeverityBadge({ severity }: { severity: Severity }) {
  return (
    <span
      className={cx(
        'inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ring-1 ring-inset',
        SEVERITY_STYLES[severity],
      )}
    >
      {SEVERITY_LABEL[severity]}
    </span>
  );
}

// SeverityCount is a compact "n Critical" chip for a scan summary.
export function SeverityCount({ severity, count }: { severity: Severity; count: number }) {
  return (
    <span
      className={cx(
        'inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium ring-1 ring-inset',
        SEVERITY_STYLES[severity],
      )}
    >
      <span className="tabular-nums">{count}</span>
      {SEVERITY_LABEL[severity]}
    </span>
  );
}
