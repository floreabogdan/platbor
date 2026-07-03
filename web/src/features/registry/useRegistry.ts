import { useCallback, useEffect, useState } from 'react';
import { api } from '../../lib/api';
import type {
  ManifestDetail,
  NpmPackage,
  NpmPackageDetail,
  Referrer,
  Repository,
  TagSummary,
} from '../../lib/types';

// Server state lives in these hooks, one per browse level (KISS —
// docs/CODING-STANDARDS.md): repositories → tags → manifest.
type LoadState = 'loading' | 'ready' | 'error';

/** useRepositories loads the global, project-grouped repository index. */
export function useRepositories() {
  const [repositories, setRepositories] = useState<Repository[]>([]);
  const [state, setState] = useState<LoadState>('loading');
  const [error, setError] = useState<string>();

  const reload = useCallback(async () => {
    setState('loading');
    try {
      const res = await api.listRepositories();
      setRepositories(res.repositories);
      setState('ready');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load repositories');
      setState('error');
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  return { repositories, state, error, reload };
}

/** usePackages loads the global, project-grouped npm package index. */
export function usePackages() {
  const [packages, setPackages] = useState<NpmPackage[]>([]);
  const [state, setState] = useState<LoadState>('loading');
  const [error, setError] = useState<string>();

  const reload = useCallback(async () => {
    setState('loading');
    try {
      const res = await api.listPackages();
      setPackages(res.packages);
      setState('ready');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load packages');
      setState('error');
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  return { packages, state, error, reload };
}

/** usePackageDetail loads one npm package's versions and dist-tags. */
export function usePackageDetail(project: string, repository: string, name: string) {
  const [detail, setDetail] = useState<NpmPackageDetail>();
  const [state, setState] = useState<LoadState>('loading');
  const [error, setError] = useState<string>();

  const reload = useCallback(async () => {
    setState('loading');
    try {
      const res = await api.getPackage(project, repository, name);
      setDetail(res);
      setState('ready');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load package');
      setState('error');
    }
  }, [project, repository, name]);

  useEffect(() => {
    void reload();
  }, [reload]);

  return { detail, state, error, reload };
}

/** useRepoTags loads one repository's tags (with per-tag manifest summary). */
export function useRepoTags(project: string, repository: string) {
  const [tags, setTags] = useState<TagSummary[]>([]);
  const [state, setState] = useState<LoadState>('loading');
  const [error, setError] = useState<string>();

  const reload = useCallback(async () => {
    setState('loading');
    try {
      const res = await api.listRepoTags(project, repository);
      setTags(res.tags);
      setState('ready');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load tags');
      setState('error');
    }
  }, [project, repository]);

  useEffect(() => {
    void reload();
  }, [reload]);

  return { tags, state, error, reload };
}

/** useManifest loads the detail for the selected reference. With no reference it
 *  stays idle (ready, empty) so the panel can prompt for a selection. */
export function useManifest(project: string, repository: string, reference: string | undefined) {
  const [manifest, setManifest] = useState<ManifestDetail>();
  const [state, setState] = useState<LoadState>('ready');
  const [error, setError] = useState<string>();

  useEffect(() => {
    if (!reference) {
      setManifest(undefined);
      setState('ready');
      return;
    }
    let active = true;
    setState('loading');
    setError(undefined);
    api
      .getManifest(project, repository, reference)
      .then((m) => {
        if (active) {
          setManifest(m);
          setState('ready');
        }
      })
      .catch((err: unknown) => {
        if (active) {
          setError(err instanceof Error ? err.message : 'Failed to load manifest');
          setState('error');
        }
      });
    return () => {
      active = false;
    };
  }, [project, repository, reference]);

  return { manifest, state, error };
}

/** useReferrers loads the artifacts (signatures, SBOMs) attached to a manifest.
 *  With no subject it stays idle so the panel simply shows nothing. */
export function useReferrers(project: string, repository: string, subject: string | undefined) {
  const [referrers, setReferrers] = useState<Referrer[]>([]);

  useEffect(() => {
    if (!subject) {
      setReferrers([]);
      return;
    }
    let active = true;
    api
      .listReferrers(project, repository, subject)
      .then((res) => {
        if (active) {
          setReferrers(res.referrers);
        }
      })
      .catch(() => {
        if (active) {
          setReferrers([]);
        }
      });
    return () => {
      active = false;
    };
  }, [project, repository, subject]);

  return referrers;
}
