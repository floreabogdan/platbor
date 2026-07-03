import { useCallback, useEffect, useState } from 'react';
import { api } from '../../lib/api';
import type {
  GenericFile,
  ManifestDetail,
  MavenArtifact,
  MavenArtifactDetail,
  NpmPackage,
  NpmPackageDetail,
  NugetPackage,
  NugetPackageDetail,
  PyPIPackage,
  PyPIPackageDetail,
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

/** useNugets loads the global, project-grouped NuGet package index. */
export function useNugets() {
  const [packages, setPackages] = useState<NugetPackage[]>([]);
  const [state, setState] = useState<LoadState>('loading');
  const [error, setError] = useState<string>();

  const reload = useCallback(async () => {
    setState('loading');
    try {
      const res = await api.listNugets();
      setPackages(res.packages);
      setState('ready');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load NuGet packages');
      setState('error');
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  return { packages, state, error, reload };
}

/** useNugetDetail loads one NuGet package's versions. */
export function useNugetDetail(project: string, repo: string, id: string) {
  const [detail, setDetail] = useState<NugetPackageDetail>();
  const [state, setState] = useState<LoadState>('loading');
  const [error, setError] = useState<string>();

  const reload = useCallback(async () => {
    setState('loading');
    try {
      const res = await api.getNugetPackage(project, repo, id);
      setDetail(res);
      setState('ready');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load package');
      setState('error');
    }
  }, [project, repo, id]);

  useEffect(() => {
    void reload();
  }, [reload]);

  return { detail, state, error, reload };
}

/** usePypis loads the global, project-grouped PyPI package index. */
export function usePypis() {
  const [packages, setPackages] = useState<PyPIPackage[]>([]);
  const [state, setState] = useState<LoadState>('loading');
  const [error, setError] = useState<string>();

  const reload = useCallback(async () => {
    setState('loading');
    try {
      const res = await api.listPypis();
      setPackages(res.packages);
      setState('ready');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load PyPI packages');
      setState('error');
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  return { packages, state, error, reload };
}

/** usePypiDetail loads one PyPI package's distribution files. */
export function usePypiDetail(project: string, repo: string, name: string) {
  const [detail, setDetail] = useState<PyPIPackageDetail>();
  const [state, setState] = useState<LoadState>('loading');
  const [error, setError] = useState<string>();

  const reload = useCallback(async () => {
    setState('loading');
    try {
      const res = await api.getPypiPackage(project, repo, name);
      setDetail(res);
      setState('ready');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load package');
      setState('error');
    }
  }, [project, repo, name]);

  useEffect(() => {
    void reload();
  }, [reload]);

  return { detail, state, error, reload };
}

/** useMavens loads the global, project-grouped Maven artifact index. */
export function useMavens() {
  const [artifacts, setArtifacts] = useState<MavenArtifact[]>([]);
  const [state, setState] = useState<LoadState>('loading');
  const [error, setError] = useState<string>();

  const reload = useCallback(async () => {
    setState('loading');
    try {
      const res = await api.listMavens();
      setArtifacts(res.artifacts);
      setState('ready');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load Maven artifacts');
      setState('error');
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  return { artifacts, state, error, reload };
}

/** useMavenDetail loads one Maven artifact's files. */
export function useMavenDetail(project: string, repo: string, group: string, artifact: string) {
  const [detail, setDetail] = useState<MavenArtifactDetail>();
  const [state, setState] = useState<LoadState>('loading');
  const [error, setError] = useState<string>();

  const reload = useCallback(async () => {
    setState('loading');
    try {
      const res = await api.getMavenArtifact(project, repo, group, artifact);
      setDetail(res);
      setState('ready');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load artifact');
      setState('error');
    }
  }, [project, repo, group, artifact]);

  useEffect(() => {
    void reload();
  }, [reload]);

  return { detail, state, error, reload };
}

/** useGenericFiles loads the global, project-grouped generic file index. */
export function useGenericFiles() {
  const [files, setFiles] = useState<GenericFile[]>([]);
  const [state, setState] = useState<LoadState>('loading');
  const [error, setError] = useState<string>();

  const reload = useCallback(async () => {
    setState('loading');
    try {
      const res = await api.listGenericFiles();
      setFiles(res.files);
      setState('ready');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load generic files');
      setState('error');
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  return { files, state, error, reload };
}

/** usePackageDetail loads one npm package's versions and dist-tags. */
export function usePackageDetail(project: string, repo: string, name: string) {
  const [detail, setDetail] = useState<NpmPackageDetail>();
  const [state, setState] = useState<LoadState>('loading');
  const [error, setError] = useState<string>();

  const reload = useCallback(async () => {
    setState('loading');
    try {
      const res = await api.getPackage(project, repo, name);
      setDetail(res);
      setState('ready');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load package');
      setState('error');
    }
  }, [project, repo, name]);

  useEffect(() => {
    void reload();
  }, [reload]);

  return { detail, state, error, reload };
}

/** useRepoTags loads one image's tags (with per-tag manifest summary). */
export function useRepoTags(project: string, repo: string, image: string) {
  const [tags, setTags] = useState<TagSummary[]>([]);
  const [state, setState] = useState<LoadState>('loading');
  const [error, setError] = useState<string>();

  const reload = useCallback(async () => {
    setState('loading');
    try {
      const res = await api.listRepoTags(project, repo, image);
      setTags(res.tags);
      setState('ready');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load tags');
      setState('error');
    }
  }, [project, repo, image]);

  useEffect(() => {
    void reload();
  }, [reload]);

  return { tags, state, error, reload };
}

/** useManifest loads the detail for the selected reference. With no reference it
 *  stays idle (ready, empty) so the panel can prompt for a selection. */
export function useManifest(
  project: string,
  repo: string,
  image: string,
  reference: string | undefined,
) {
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
      .getManifest(project, repo, image, reference)
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
  }, [project, repo, image, reference]);

  return { manifest, state, error };
}

/** useReferrers loads the artifacts (signatures, SBOMs) attached to a manifest.
 *  With no subject it stays idle so the panel simply shows nothing. */
export function useReferrers(
  project: string,
  repo: string,
  image: string,
  subject: string | undefined,
) {
  const [referrers, setReferrers] = useState<Referrer[]>([]);

  useEffect(() => {
    if (!subject) {
      setReferrers([]);
      return;
    }
    let active = true;
    api
      .listReferrers(project, repo, image, subject)
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
  }, [project, repo, image, subject]);

  return referrers;
}
