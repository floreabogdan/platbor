// formatDate renders an RFC 3339 timestamp as a short, locale-aware date,
// falling back to the raw string if it cannot be parsed.
export function formatDate(iso: string): string {
  const date = new Date(iso);
  return Number.isNaN(date.getTime())
    ? iso
    : date.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
}

// formatBytes renders a byte count in binary units (KiB steps, shown as KB/MB…),
// with one decimal below 10 of a unit for readability.
export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) {
    return '0 B';
  }
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const value = bytes / 1024 ** i;
  const rounded = value >= 10 || i === 0 ? Math.round(value) : Number(value.toFixed(1));
  return `${String(rounded)} ${units[i] ?? 'B'}`;
}

// shortDigest returns the first 12 hex characters of a digest (after the
// algorithm prefix) — enough to recognize, short enough to scan.
export function shortDigest(digest: string): string {
  const hex = digest.includes(':') ? digest.slice(digest.indexOf(':') + 1) : digest;
  return hex.slice(0, 12);
}
