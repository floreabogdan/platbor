import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { Card, EmptyState, PageHeader } from '../../components/ui';
import { ShieldIcon } from '../../components/icons';
import { api } from '../../lib/api';
import { cx } from '../../lib/cx';
import { formatDate, shortDigest } from '../../lib/format';
import type { AffectedArtifact, VulnerabilitySummary } from '../../lib/types';
import { SeverityBadge } from './SeverityBadge';

// The instance-wide vulnerability index: every CVE (or advisory) any scanned
// artifact carries, and — the headline query — the artifacts each one affects.
// Populated by scanning image SBOMs from their manifest pages.
export function VulnerabilitiesPage() {
  const [vulns, setVulns] = useState<VulnerabilitySummary[]>([]);
  const [state, setState] = useState<'loading' | 'ready' | 'error'>('loading');
  const [error, setError] = useState<string>();
  const [openId, setOpenId] = useState<string>();

  useEffect(() => {
    let live = true;
    api
      .listVulnerabilities()
      .then((res) => {
        if (live) {
          setVulns(res.vulnerabilities);
          setState('ready');
        }
      })
      .catch((err: unknown) => {
        if (live) {
          setError(err instanceof Error ? err.message : 'Failed to load vulnerabilities');
          setState('error');
        }
      });
    return () => {
      live = false;
    };
  }, []);

  return (
    <div className="animate-rise">
      <PageHeader
        title="Vulnerabilities"
        subtitle="Known vulnerabilities across scanned artifacts, worst first. Expand one to see every image it affects."
      />

      {state === 'loading' ? <Card className="h-40 animate-pulse bg-slate-50" /> : null}
      {state === 'error' ? (
        <Card className="p-6">
          <p className="text-sm text-red-700">{error}</p>
        </Card>
      ) : null}

      {state === 'ready' && vulns.length === 0 ? (
        <EmptyState
          icon={<ShieldIcon className="h-8 w-8" />}
          message="No vulnerabilities recorded. Scan an image's SBOM from its manifest page to populate this view."
        />
      ) : null}

      {state === 'ready' && vulns.length > 0 ? (
        <Card className="overflow-hidden">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-slate-200/80 text-left text-xs uppercase tracking-wide text-slate-400">
                <Th>Severity</Th>
                <Th>Vulnerability</Th>
                <Th>Summary</Th>
                <Th className="text-right">Artifacts</Th>
              </tr>
            </thead>
            <tbody>
              {vulns.map((v) => (
                <VulnerabilityRow
                  key={v.vulnId}
                  vuln={v}
                  open={openId === v.vulnId}
                  onToggle={() => setOpenId(openId === v.vulnId ? undefined : v.vulnId)}
                />
              ))}
            </tbody>
          </table>
        </Card>
      ) : null}
    </div>
  );
}

function VulnerabilityRow({
  vuln,
  open,
  onToggle,
}: {
  vuln: VulnerabilitySummary;
  open: boolean;
  onToggle: () => void;
}) {
  const [affected, setAffected] = useState<AffectedArtifact[]>();
  const [state, setState] = useState<'idle' | 'loading' | 'error'>('idle');
  const [error, setError] = useState<string>();

  useEffect(() => {
    if (!open || affected || state === 'loading') {
      return;
    }
    let live = true;
    setState('loading');
    api
      .getVulnerability(vuln.vulnId)
      .then((res) => {
        if (live) {
          setAffected(res.affected);
          setState('idle');
        }
      })
      .catch((err: unknown) => {
        if (live) {
          setError(err instanceof Error ? err.message : 'Failed to load affected artifacts');
          setState('error');
        }
      });
    return () => {
      live = false;
    };
  }, [open, affected, state, vuln.vulnId]);

  return (
    <>
      <tr
        onClick={onToggle}
        aria-expanded={open}
        className={cx(
          'cursor-pointer border-b border-slate-100 transition-colors last:border-0',
          open ? 'bg-teal-50/60' : 'hover:bg-slate-50',
        )}
      >
        <Td>
          <SeverityBadge severity={vuln.severity} />
        </Td>
        <Td>
          <span className="font-mono text-xs font-medium text-slate-800">{vuln.vulnId}</span>
        </Td>
        <Td>
          <span className="line-clamp-1 text-slate-600">{vuln.summary || '—'}</span>
        </Td>
        <Td className="text-right tabular-nums text-slate-600">{vuln.artifactCount}</Td>
      </tr>
      {open ? (
        <tr className="bg-slate-50/50">
          <td colSpan={4} className="px-4 py-3">
            {state === 'loading' ? <p className="text-xs text-slate-400">Loading affected artifacts…</p> : null}
            {state === 'error' ? <p className="text-xs text-red-600">{error}</p> : null}
            {affected ? <AffectedTable affected={affected} /> : null}
          </td>
        </tr>
      ) : null}
    </>
  );
}

function AffectedTable({ affected }: { affected: AffectedArtifact[] }) {
  if (affected.length === 0) {
    return <p className="text-xs text-slate-400">No affected artifacts.</p>;
  }
  return (
    <div className="overflow-hidden rounded-md border border-slate-200/80 bg-white">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-slate-200/80 text-left text-xs uppercase tracking-wide text-slate-400">
            <Th>Image</Th>
            <Th>Package</Th>
            <Th>Version</Th>
            <Th>Fixed in</Th>
            <Th>Scanned</Th>
          </tr>
        </thead>
        <tbody>
          {affected.map((a, i) => (
            <tr key={`${a.digest}-${a.package}-${String(i)}`} className="border-b border-slate-100 last:border-0">
              <Td>
                <Link
                  to={`/registry/${encodeURIComponent(a.projectKey)}/${a.repoKey}/${a.image}?ref=${encodeURIComponent(a.digest)}`}
                  className="text-teal-700 hover:text-teal-900"
                >
                  <span className="text-slate-500">{a.projectKey}</span>
                  <span className="text-slate-300"> / </span>
                  {a.repoKey}
                  <span className="text-slate-300"> / </span>
                  {a.image}
                </Link>
                <span className="ml-2 font-mono text-xs text-slate-400">{shortDigest(a.digest)}</span>
              </Td>
              <Td>
                <span className="font-mono text-xs text-slate-700">{a.package}</span>
              </Td>
              <Td>
                <span className="font-mono text-xs text-slate-500">{a.version || '—'}</span>
              </Td>
              <Td>
                <span className="font-mono text-xs text-emerald-700">{a.fixedVersion || '—'}</span>
              </Td>
              <Td className="text-slate-500">{formatDate(a.scannedAt)}</Td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function Th({ children, className }: { children: React.ReactNode; className?: string }) {
  return <th className={cx('px-4 py-2.5 font-medium', className)}>{children}</th>;
}

function Td({ children, className }: { children: React.ReactNode; className?: string }) {
  return <td className={cx('px-4 py-2.5', className)}>{children}</td>;
}
