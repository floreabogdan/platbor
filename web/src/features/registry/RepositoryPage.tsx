import { useState } from 'react';
import type { ReactNode } from 'react';
import { useParams, useSearchParams } from 'react-router-dom';
import { Breadcrumb, Card, CopyButton, EmptyState, PageHeader } from '../../components/ui';
import { LayersIcon, RegistryIcon, TrashIcon } from '../../components/icons';
import { api } from '../../lib/api';
import { cx } from '../../lib/cx';
import { formatBytes, formatDate, shortDigest } from '../../lib/format';
import type { IndexEntry, Layer, ManifestDetail, ManifestKind, Referrer, SbomResponse, TagSummary } from '../../lib/types';
import { DeleteDialog, type DeleteTarget } from './DeleteDialog';
import { splitRepoAndRest } from './packageRoute';
import { useManifest, useReferrers, useRepoTags } from './useRegistry';

// Image detail: the tags of an OCI image inside a repository and, for the
// selected tag, the manifest it points at — media type, layers, sizes, and a
// copy-paste pull command. The image lives in a typed repository, so it is
// identified by (project, repo, image); the route splat carries "<repo>/<image>".
// The selected tag lives in the URL (?ref=) so views are shareable.
export function RepositoryPage() {
  const params = useParams();
  const project = params.project ?? '';
  const { repo, rest: image } = splitRepoAndRest(params['*'] ?? '');

  const [searchParams, setSearchParams] = useSearchParams();
  const { tags, state, error, reload } = useRepoTags(project, repo, image);

  const selected = searchParams.get('ref') ?? tags[0]?.tag;
  const { manifest, state: manifestState } = useManifest(project, repo, image, selected);

  const [pendingDelete, setPendingDelete] = useState<DeleteTarget | null>(null);

  function select(ref: string) {
    setSearchParams({ ref }, { replace: true });
  }

  function requestDeleteTag(tag: TagSummary) {
    setPendingDelete({ mode: 'tag', label: tag.tag, reference: tag.tag });
  }

  function requestDeleteManifest() {
    if (!manifest) {
      return;
    }
    const affectedTags = tags.filter((t) => t.digest === manifest.digest).length;
    setPendingDelete({
      mode: 'manifest',
      label: shortDigest(manifest.digest),
      reference: manifest.digest,
      affectedTags,
    });
  }

  function afterDeleted() {
    setPendingDelete(null);
    setSearchParams({}, { replace: true }); // reset selection to the first remaining tag
    void reload();
  }

  if (!project || !repo || !image) {
    return <EmptyState message="No image selected." />;
  }

  return (
    <div className="animate-rise">
      <Breadcrumb
        items={[
          { label: 'Registry', to: '/registry' },
          { label: `${project}/${repo}`, to: '/registry' },
          { label: image },
        ]}
      />
      <PageHeader title={`${project}/${repo}/${image}`} subtitle="Tags and manifests for this image." />

      {state === 'loading' ? <Card className="h-40 animate-pulse bg-slate-50" /> : null}
      {state === 'error' ? (
        <Card className="p-6">
          <p className="text-sm text-red-700">{error ?? 'Failed to load tags.'}</p>
        </Card>
      ) : null}

      {state === 'ready' && tags.length === 0 ? (
        <EmptyState
          icon={<RegistryIcon className="h-8 w-8" />}
          message="This repository has no tags yet."
        />
      ) : null}

      {state === 'ready' && tags.length > 0 ? (
        <div className="space-y-5">
          <TagsTable tags={tags} selected={selected} onSelect={select} onDelete={requestDeleteTag} />
          <ManifestPanel
            project={project}
            repo={repo}
            image={image}
            reference={selected}
            manifest={manifest}
            loading={manifestState === 'loading'}
            onDelete={requestDeleteManifest}
          />
        </div>
      ) : null}

      {pendingDelete ? (
        <DeleteDialog
          project={project}
          repo={repo}
          image={image}
          target={pendingDelete}
          onClose={() => setPendingDelete(null)}
          onDeleted={afterDeleted}
        />
      ) : null}
    </div>
  );
}

// --- Tags ---

function TagsTable({
  tags,
  selected,
  onSelect,
  onDelete,
}: {
  tags: TagSummary[];
  selected?: string;
  onSelect: (ref: string) => void;
  onDelete: (tag: TagSummary) => void;
}) {
  return (
    <Card className="overflow-hidden">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-slate-200/80 text-left text-xs uppercase tracking-wide text-slate-400">
            <Th>Tag</Th>
            <Th>Kind</Th>
            <Th className="text-right">Size</Th>
            <Th className="text-right">Layers</Th>
            <Th>Pushed</Th>
            <Th>Digest</Th>
            <Th>
              <span className="sr-only">Actions</span>
            </Th>
          </tr>
        </thead>
        <tbody>
          {tags.map((tag) => {
            const active = tag.tag === selected;
            return (
              <tr
                key={tag.tag}
                onClick={() => onSelect(tag.tag)}
                aria-selected={active}
                className={cx(
                  'cursor-pointer border-b border-slate-100 transition-colors last:border-0',
                  active ? 'bg-teal-50/70' : 'hover:bg-slate-50',
                )}
              >
                <Td>
                  <span className="font-mono font-medium text-slate-900">{tag.tag}</span>
                </Td>
                <Td>
                  <KindBadge kind={tag.kind} />
                </Td>
                <Td className="text-right tabular-nums text-slate-600">
                  {tag.kind === 'index' ? '—' : formatBytes(tag.size)}
                </Td>
                <Td className="text-right tabular-nums text-slate-600">
                  {tag.kind === 'index' ? `${String(tag.count)} platforms` : tag.count}
                </Td>
                <Td className="text-slate-500">{formatDate(tag.pushedAt)}</Td>
                <Td>
                  <span className="font-mono text-xs text-slate-400">{shortDigest(tag.digest)}</span>
                </Td>
                <Td className="text-right">
                  <button
                    type="button"
                    onClick={(e) => {
                      e.stopPropagation();
                      onDelete(tag);
                    }}
                    aria-label={`Delete tag ${tag.tag}`}
                    className="rounded-md p-1.5 text-slate-400 transition-colors hover:bg-red-50 hover:text-red-600"
                  >
                    <TrashIcon className="h-4 w-4" />
                  </button>
                </Td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </Card>
  );
}

// --- Manifest detail ---

function ManifestPanel({
  project,
  repo,
  image,
  reference,
  manifest,
  loading,
  onDelete,
}: {
  project: string;
  repo: string;
  image: string;
  reference?: string;
  manifest?: ManifestDetail;
  loading: boolean;
  onDelete: () => void;
}) {
  // Hooks run before any early return (rules of hooks); an absent digest keeps
  // this idle.
  const referrers = useReferrers(project, repo, image, manifest?.digest);

  if (loading) {
    return <Card className="h-48 animate-pulse bg-slate-50" />;
  }
  if (!manifest || !reference) {
    return (
      <Card className="p-6">
        <p className="text-sm text-slate-500">Select a tag to inspect its manifest.</p>
      </Card>
    );
  }

  return (
    <Card className="p-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <LayersIcon className="h-5 w-5 text-slate-400" />
          <h2 className="font-semibold text-slate-900">Manifest</h2>
          <KindBadge kind={manifest.kind} />
          <TrustBadges referrers={referrers} />
        </div>
        <div className="flex items-center gap-3">
          {manifest.kind !== 'index' ? (
            <span className="text-sm text-slate-500">
              Total size <span className="font-medium text-slate-700">{formatBytes(manifest.totalSize)}</span>
            </span>
          ) : null}
          <button
            type="button"
            onClick={onDelete}
            className="inline-flex items-center gap-1.5 rounded-lg px-3 py-2 text-sm font-medium text-red-600 ring-1 ring-inset ring-red-200 transition-colors hover:bg-red-50"
          >
            <TrashIcon className="h-4 w-4" />
            Delete manifest
          </button>
        </div>
      </div>

      <dl className="mt-4 grid gap-3 sm:grid-cols-2">
        <Field label="Digest">
          <span className="font-mono text-xs text-slate-700">{shortDigest(manifest.digest)}</span>
          <CopyButton value={manifest.digest} label="" className="ml-1 align-middle" />
        </Field>
        <Field label="Media type">
          <span className="font-mono text-xs text-slate-600">{manifest.mediaType}</span>
        </Field>
      </dl>

      <PullCommand project={project} repo={repo} image={image} reference={reference} kind={manifest.kind} />

      {manifest.kind === 'index' ? (
        <PlatformsTable manifests={manifest.manifests} />
      ) : (
        <LayersTable config={manifest.config} layers={manifest.layers} />
      )}

      {referrers.length > 0 ? (
        <ReferrersSection referrers={referrers} project={project} repo={repo} image={image} />
      ) : null}
    </Card>
  );
}

// TrustBadges surfaces the provenance attached to an image at a glance: whether
// it is signed and whether it ships an SBOM (derived from its referrers).
function TrustBadges({ referrers }: { referrers: Referrer[] }) {
  const signed = referrers.some((r) => referrerLabel(r.artifactType) === 'Signature');
  const sbom = referrers.some((r) => referrerLabel(r.artifactType) === 'SBOM');
  if (!signed && !sbom) {
    return null;
  }
  return (
    <div className="flex items-center gap-1.5">
      {signed ? (
        <span className="inline-flex items-center gap-1 rounded-full bg-emerald-100 px-2 py-0.5 text-xs font-medium text-emerald-700 ring-1 ring-inset ring-emerald-600/20">
          ✓ Signed
        </span>
      ) : null}
      {sbom ? (
        <span className="inline-flex items-center rounded-full bg-sky-100 px-2 py-0.5 text-xs font-medium text-sky-700 ring-1 ring-inset ring-sky-600/20">
          SBOM
        </span>
      ) : null}
    </div>
  );
}

// referrerLabel maps a known artifact type to a friendly noun, falling back to
// the raw type so unknown attestations are still legible.
function referrerLabel(artifactType?: string): string {
  if (!artifactType) {
    return 'Artifact';
  }
  const t = artifactType.toLowerCase();
  if (t.includes('cosign') && t.includes('sig')) {
    return 'Signature';
  }
  if (t.includes('sbom') || t.includes('spdx') || t.includes('cyclonedx')) {
    return 'SBOM';
  }
  if (t.includes('cosign') || t.includes('sig')) {
    return 'Signature';
  }
  if (t.includes('attestation') || t.includes('in-toto')) {
    return 'Attestation';
  }
  return artifactType;
}

function ReferrersSection({
  referrers,
  project,
  repo,
  image,
}: {
  referrers: Referrer[];
  project: string;
  repo: string;
  image: string;
}) {
  return (
    <div className="mt-6">
      <div className="mb-2 text-xs font-medium uppercase tracking-wide text-slate-400">
        {referrers.length} {referrers.length === 1 ? 'referrer' : 'referrers'}
        <span className="ml-1 normal-case text-slate-300">· signatures, SBOMs & attestations</span>
      </div>
      <div className="overflow-hidden rounded-lg border border-slate-200/80">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-slate-200/80 text-left text-xs uppercase tracking-wide text-slate-400">
              <Th>Type</Th>
              <Th>Artifact type</Th>
              <Th>Digest</Th>
              <Th className="text-right">Size</Th>
            </tr>
          </thead>
          <tbody>
            {referrers.map((ref, i) => (
              <ReferrerRow key={`${ref.digest}-${String(i)}`} referrer={ref} project={project} repo={repo} image={image} />
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function ReferrerRow({
  referrer,
  project,
  repo,
  image,
}: {
  referrer: Referrer;
  project: string;
  repo: string;
  image: string;
}) {
  const isSbom = referrerLabel(referrer.artifactType) === 'SBOM';
  const [open, setOpen] = useState(false);
  const [sbom, setSbom] = useState<SbomResponse>();
  const [state, setState] = useState<'idle' | 'loading' | 'error'>('idle');
  const [error, setError] = useState<string>();

  async function toggle() {
    if (open) {
      setOpen(false);
      return;
    }
    setOpen(true);
    if (sbom || state === 'loading') {
      return;
    }
    setState('loading');
    try {
      setSbom(await api.getSBOM(project, repo, image, referrer.digest));
      setState('idle');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load SBOM');
      setState('error');
    }
  }

  return (
    <>
      <tr className="border-b border-slate-100 last:border-0">
        <Td>
          <span className="inline-flex items-center rounded-full bg-slate-100 px-2 py-0.5 text-xs font-medium text-slate-700">
            {referrerLabel(referrer.artifactType)}
          </span>
        </Td>
        <Td>
          <span className="font-mono text-xs text-slate-400">{referrer.artifactType || '—'}</span>
        </Td>
        <Td>
          <span className="font-mono text-xs text-slate-600">{shortDigest(referrer.digest)}</span>
          {isSbom ? (
            <button
              type="button"
              onClick={() => void toggle()}
              className="ml-3 text-xs font-medium text-teal-700 hover:text-teal-900"
            >
              {open ? 'Hide components' : 'View components'}
            </button>
          ) : null}
        </Td>
        <Td className="text-right tabular-nums text-slate-600">{formatBytes(referrer.size)}</Td>
      </tr>
      {isSbom && open ? (
        <tr className="bg-slate-50/60">
          <td colSpan={4} className="px-4 py-3">
            {state === 'loading' ? <p className="text-xs text-slate-400">Loading SBOM…</p> : null}
            {state === 'error' ? <p className="text-xs text-red-600">{error}</p> : null}
            {sbom ? <SbomComponents sbom={sbom} /> : null}
          </td>
        </tr>
      ) : null}
    </>
  );
}

function SbomComponents({ sbom }: { sbom: SbomResponse }) {
  if (sbom.components.length === 0) {
    return <p className="text-xs text-slate-400">No components listed in this {sbom.format} SBOM.</p>;
  }
  return (
    <div>
      <div className="mb-1.5 text-xs text-slate-400">
        {sbom.components.length} components · <span className="uppercase">{sbom.format}</span>
      </div>
      <div className="max-h-72 overflow-y-auto rounded-md border border-slate-200/80 bg-white">
        <table className="w-full text-sm">
          <thead className="sticky top-0 bg-white">
            <tr className="border-b border-slate-200/80 text-left text-xs uppercase tracking-wide text-slate-400">
              <Th>Component</Th>
              <Th>Version</Th>
              <Th>License</Th>
            </tr>
          </thead>
          <tbody>
            {sbom.components.map((c, i) => (
              <tr key={`${c.name}-${c.version ?? ''}-${String(i)}`} className="border-b border-slate-100 last:border-0">
                <Td>
                  <span className="font-mono text-xs text-slate-800">{c.name}</span>
                </Td>
                <Td>
                  <span className="font-mono text-xs text-slate-500">{c.version || '—'}</span>
                </Td>
                <Td>
                  <span className="text-xs text-slate-500">{c.license || '—'}</span>
                </Td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function PullCommand({
  project,
  repo,
  image,
  reference,
  kind,
}: {
  project: string;
  repo: string;
  image: string;
  reference: string;
  kind: ManifestKind;
}) {
  const host = window.location.host;
  const path = `${host}/${project}/${repo}/${image}`;
  // A Helm chart is pulled by version with the helm client; images/indexes with
  // docker. A digest reference (contains ':') is addressed with @ for docker.
  const isDigest = reference.includes(':');
  // A Helm chart is pulled by version with the helm client (charts are selected
  // by tag); images and indexes use docker, with @ for a digest reference.
  const command =
    kind === 'chart'
      ? `helm pull oci://${path} --version ${reference}`
      : `docker pull ${path}${isDigest ? '@' : ':'}${reference}`;

  return (
    <div className="mt-5">
      <div className="mb-1.5 text-xs font-medium uppercase tracking-wide text-slate-400">Pull</div>
      <div className="flex items-center justify-between gap-2 rounded-lg bg-ink-900 px-3 py-2.5">
        <code className="truncate font-mono text-xs text-slate-200">{command}</code>
        <CopyButton value={command} label="Copy" className="shrink-0 text-slate-400 hover:bg-white/10 hover:text-white" />
      </div>
    </div>
  );
}

function LayersTable({ config, layers }: { config?: Layer; layers: Layer[] }) {
  return (
    <div className="mt-6">
      <div className="mb-2 text-xs font-medium uppercase tracking-wide text-slate-400">
        {layers.length} {layers.length === 1 ? 'layer' : 'layers'}
      </div>
      <div className="overflow-hidden rounded-lg border border-slate-200/80">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-slate-200/80 text-left text-xs uppercase tracking-wide text-slate-400">
              <Th>#</Th>
              <Th>Digest</Th>
              <Th className="text-right">Size</Th>
              <Th>Media type</Th>
            </tr>
          </thead>
          <tbody>
            {config ? <BlobRow index="config" blob={config} /> : null}
            {layers.map((layer, i) => (
              <BlobRow key={`${layer.digest}-${String(i)}`} index={String(i + 1)} blob={layer} />
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function BlobRow({ index, blob }: { index: string; blob: Layer }) {
  return (
    <tr className="border-b border-slate-100 last:border-0">
      <Td className="text-slate-400">{index}</Td>
      <Td>
        <span className="font-mono text-xs text-slate-600">{shortDigest(blob.digest)}</span>
      </Td>
      <Td className="text-right tabular-nums text-slate-600">{formatBytes(blob.size)}</Td>
      <Td>
        <span className="font-mono text-xs text-slate-400">{blob.mediaType}</span>
      </Td>
    </tr>
  );
}

function PlatformsTable({ manifests }: { manifests: IndexEntry[] }) {
  return (
    <div className="mt-6">
      <div className="mb-2 text-xs font-medium uppercase tracking-wide text-slate-400">
        {manifests.length} {manifests.length === 1 ? 'platform' : 'platforms'}
      </div>
      <div className="overflow-hidden rounded-lg border border-slate-200/80">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-slate-200/80 text-left text-xs uppercase tracking-wide text-slate-400">
              <Th>Platform</Th>
              <Th>Digest</Th>
              <Th className="text-right">Size</Th>
            </tr>
          </thead>
          <tbody>
            {manifests.map((entry, i) => (
              <tr key={`${entry.digest}-${String(i)}`} className="border-b border-slate-100 last:border-0">
                <Td>
                  <span className="font-mono text-xs text-slate-700">{entry.platform || 'unknown'}</span>
                </Td>
                <Td>
                  <span className="font-mono text-xs text-slate-600">{shortDigest(entry.digest)}</span>
                </Td>
                <Td className="text-right tabular-nums text-slate-600">{formatBytes(entry.size)}</Td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

// --- small building blocks ---

const KIND_BADGE: Record<ManifestKind, { label: string; styles: string }> = {
  image: { label: 'Image', styles: 'bg-teal-50 text-teal-700 ring-teal-600/20' },
  index: { label: 'Index', styles: 'bg-violet-50 text-violet-700 ring-violet-600/20' },
  chart: { label: 'Helm chart', styles: 'bg-sky-50 text-sky-700 ring-sky-600/20' },
};

function KindBadge({ kind }: { kind: ManifestKind }) {
  const badge = KIND_BADGE[kind];
  return (
    <span
      className={cx(
        'inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ring-1 ring-inset',
        badge.styles,
      )}
    >
      {badge.label}
    </span>
  );
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div>
      <dt className="text-xs font-medium uppercase tracking-wide text-slate-400">{label}</dt>
      <dd className="mt-0.5 text-slate-700">{children}</dd>
    </div>
  );
}

function Th({ children, className }: { children: ReactNode; className?: string }) {
  return <th className={cx('px-4 py-2.5 font-medium', className)}>{children}</th>;
}

function Td({ children, className }: { children: ReactNode; className?: string }) {
  return <td className={cx('px-4 py-2.5', className)}>{children}</td>;
}
