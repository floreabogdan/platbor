// formatDate renders an RFC 3339 timestamp as a short, locale-aware date,
// falling back to the raw string if it cannot be parsed.
export function formatDate(iso: string): string {
  const date = new Date(iso);
  return Number.isNaN(date.getTime())
    ? iso
    : date.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
}
