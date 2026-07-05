import type { Severity } from '../../lib/types';

// SEVERITY_ORDER lists severities worst-first, for stable ordering of summary chips.
export const SEVERITY_ORDER: Severity[] = ['critical', 'high', 'medium', 'low', 'unknown'];

export const SEVERITY_STYLES: Record<Severity, string> = {
  critical: 'bg-red-100 text-red-700 ring-red-600/20',
  high: 'bg-orange-100 text-orange-700 ring-orange-600/20',
  medium: 'bg-amber-100 text-amber-800 ring-amber-600/20',
  low: 'bg-slate-100 text-slate-600 ring-slate-500/20',
  unknown: 'bg-slate-100 text-slate-500 ring-slate-400/20',
};

export const SEVERITY_LABEL: Record<Severity, string> = {
  critical: 'Critical',
  high: 'High',
  medium: 'Medium',
  low: 'Low',
  unknown: 'Unknown',
};
