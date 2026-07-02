/** cx joins class names, dropping falsy values — the one place we compose
 *  Tailwind class lists (docs/CODING-STANDARDS.md: DRY applies to class lists). */
export function cx(...parts: Array<string | false | undefined | null>): string {
  return parts.filter(Boolean).join(' ');
}
