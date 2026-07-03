// Inline 18px stroke icons — no icon-font dependency (docs/DESIGN-SYSTEM.md).
// strokeWidth ≈ 2.2, currentColor so callers control hue.
import type { SVGProps } from 'react';

type IconProps = SVGProps<SVGSVGElement>;

function Icon({ children, ...props }: IconProps & { children: React.ReactNode }) {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width={18}
      height={18}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2.2}
      strokeLinecap="round"
      strokeLinejoin="round"
      className="h-[18px] w-[18px]"
      aria-hidden="true"
      {...props}
    >
      {children}
    </svg>
  );
}

export function DashboardIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <rect x="3" y="3" width="7" height="9" rx="1" />
      <rect x="14" y="3" width="7" height="5" rx="1" />
      <rect x="14" y="12" width="7" height="9" rx="1" />
      <rect x="3" y="16" width="7" height="5" rx="1" />
    </Icon>
  );
}

export function RegistryIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M21 8V6a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 6v12a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 18v-2" />
      <path d="m3.3 7 8.7 5 8.7-5" />
      <path d="M12 22V12" />
    </Icon>
  );
}

export function PackageIcon(props: IconProps) {
  // A flat parcel (box + lid seam + tape) — deliberately distinct from the 3D
  // cube RegistryIcon, so npm packages read differently from container images.
  return (
    <Icon {...props}>
      <rect x="3.5" y="5" width="17" height="15" rx="1.5" />
      <path d="M3.5 10h17" />
      <path d="M12 5v5" />
    </Icon>
  );
}

export function NugetIcon(props: IconProps) {
  // A hexagon — evokes the NuGet package mark, distinct from the OCI cube and the
  // flat npm parcel.
  return (
    <Icon {...props}>
      <path d="M12 2.5 4 7v10l8 4.5 8-4.5V7z" />
    </Icon>
  );
}

export function GoIcon(props: IconProps) {
  // A speed-line gopher-esque mark: two motion lines and a rounded module box,
  // evoking Go's "fetch fast" module proxy identity.
  return (
    <Icon {...props}>
      <path d="M2 9h5" />
      <path d="M2 13h4" />
      <rect x="9" y="6" width="12" height="12" rx="3" />
      <circle cx="13" cy="12" r="1" fill="currentColor" stroke="none" />
      <circle cx="17" cy="12" r="1" fill="currentColor" stroke="none" />
    </Icon>
  );
}

export function CargoIcon(props: IconProps) {
  // Stacked crates — Cargo's shipping-crate identity, a simple mark distinct
  // from the other format glyphs.
  return (
    <Icon {...props}>
      <rect x="3" y="13" width="8" height="8" rx="1" />
      <rect x="13" y="13" width="8" height="8" rx="1" />
      <rect x="8" y="4" width="8" height="8" rx="1" />
      <path d="M8 8h8M7 17h4M13 17h4" />
    </Icon>
  );
}

export function MavenIcon(props: IconProps) {
  // A feather over a stack — Maven's build-and-package identity, rendered as a
  // simple mark distinct from the other format glyphs.
  return (
    <Icon {...props}>
      <path d="M4 17h16" />
      <path d="M6 17V9l6-4 6 4v8" />
      <path d="M12 5v12" />
      <path d="M9 11l3-2 3 2" />
    </Icon>
  );
}

export function PypiIcon(props: IconProps) {
  // Two interlocking snake-tiles evoking Python's twin serpents, distinct from
  // the other format glyphs.
  return (
    <Icon {...props}>
      <path d="M8 4h5a2 2 0 0 1 2 2v5H10a2 2 0 0 0-2 2v-5a2 2 0 0 1 2-2h4" />
      <path d="M16 20h-5a2 2 0 0 1-2-2v-5h5a2 2 0 0 0 2-2v5a2 2 0 0 1-2 2h-4" />
      <circle cx="10" cy="6.5" r="0.6" fill="currentColor" stroke="none" />
      <circle cx="14" cy="17.5" r="0.6" fill="currentColor" stroke="none" />
    </Icon>
  );
}

export function FileIcon(props: IconProps) {
  // A document with a folded corner, for generic files.
  return (
    <Icon {...props}>
      <path d="M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z" />
      <path d="M14 3v6h6" />
    </Icon>
  );
}

export function SearchIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="11" cy="11" r="7" />
      <path d="m21 21-4.3-4.3" />
    </Icon>
  );
}

export function CatalogIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="6" cy="6" r="2.5" />
      <circle cx="18" cy="6" r="2.5" />
      <circle cx="12" cy="18" r="2.5" />
      <path d="M8 7.5 10.5 16M16 7.5 13.5 16" />
    </Icon>
  );
}

export function ProjectsIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />
    </Icon>
  );
}

export function LogoutIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" />
      <path d="m16 17 5-5-5-5" />
      <path d="M21 12H9" />
    </Icon>
  );
}

export function SettingsIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="12" cy="12" r="3" />
      <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-2.82 1.17V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 8 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.6 15H4.5a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 6 9.4l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 12 4.6a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 2.82 1.17l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9v.09A1.65 1.65 0 0 0 21 10.6a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
    </Icon>
  );
}

export function ChevronRightIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="m9 18 6-6-6-6" />
    </Icon>
  );
}

export function TagIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M12.586 2.586A2 2 0 0 0 11.172 2H4a2 2 0 0 0-2 2v7.172a2 2 0 0 0 .586 1.414l8.704 8.704a2.426 2.426 0 0 0 3.42 0l6.58-6.58a2.426 2.426 0 0 0 0-3.42z" />
      <circle cx="7.5" cy="7.5" r=".5" fill="currentColor" />
    </Icon>
  );
}

export function LayersIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="m12 2 9 5-9 5-9-5 9-5Z" />
      <path d="m3 12 9 5 9-5" />
      <path d="m3 17 9 5 9-5" />
    </Icon>
  );
}

export function CopyIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <rect x="9" y="9" width="12" height="12" rx="2" />
      <path d="M5 15a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h8a2 2 0 0 1 2 2" />
    </Icon>
  );
}

export function CheckIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M20 6 9 17l-5-5" />
    </Icon>
  );
}

export function TrashIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M3 6h18" />
      <path d="M8 6V4a1 1 0 0 1 1-1h6a1 1 0 0 1 1 1v2" />
      <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6" />
      <path d="M10 11v6M14 11v6" />
    </Icon>
  );
}
