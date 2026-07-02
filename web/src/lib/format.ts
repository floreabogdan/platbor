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

// formatRelativeTime renders how long ago an RFC 3339 timestamp was, coarsely.
// Beyond a month it falls back to an absolute date.
export function formatRelativeTime(iso: string): string {
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) {
    return iso;
  }
  const seconds = Math.round((Date.now() - then) / 1000);
  if (seconds < 45) {
    return 'just now';
  }
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) {
    return `${String(minutes)}m ago`;
  }
  const hours = Math.round(minutes / 60);
  if (hours < 24) {
    return `${String(hours)}h ago`;
  }
  const days = Math.round(hours / 24);
  if (days < 30) {
    return `${String(days)}d ago`;
  }
  return formatDate(iso);
}
