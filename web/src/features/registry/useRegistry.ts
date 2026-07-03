import { useCallback, useEffect, useState } from 'react';
import { api } from '../../lib/api';
import type {
  CargoCrate,
  CargoCrateDetail,
  GenericFile,
  GoModule,
  GoModuleDetail,
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
  RubyGem,
  RubyGemDetail,
  TagSummary,
  TerraformModule,
  TerraformModuleDetail,
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

/** useGoModules loads the global, project-grouped Go module index. */
export function useGoModules() {
  const [modules, setModules] = useState<GoModule[]>([]);
  const [state, setState] = useState<LoadState>('loading');
  const [error, setError] = useState<string>();

  const reload = useCallback(async () => {
    setState('loading');
    try {
      const res = await api.listGoModules();
      setModules(res.modules);
      setState('ready');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load Go modules');
      setState('error');
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  return { modules, state, error, reload };
}

/** useGoDetail loads one Go module's cached versions. */
export function useGoDetail(project: string, repo: string, module: string) {
  const [detail, setDetail] = useState<GoModuleDetail>();
  const [state, setState] = useState<LoadState>('loading');
  const [error, setError] = useState<string>();

  const reload = useCallback(async () => {
    setState('loading');
    try {
      const res = await api.getGoModule(project, repo, module);
      setDetail(res);
      setState('ready');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load module');
      setState('error');
    }
  }, [project, repo, module]);

  useEffect(() => {
    void reload();
  }, [reload]);

  return { detail, state, error, reload };
}

/** useCargoCrates loads the global, project-grouped Cargo crate index. */
export function useCargoCrates() {
  const [crates, setCrates] = useState<CargoCrate[]>([]);
  const [state, setState] = useState<LoadState>('loading');
  const [error, setError] = useState<string>();

  const reload = useCallback(async () => {
    setState('loading');
    try {
      const res = await api.listCargoCrates();
      setCrates(res.crates);
      setState('ready');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load Cargo crates');
      setState('error');
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  return { crates, state, error, reload };
}

/** useCargoDetail loads one Cargo crate's versions. */
export function useCargoDetail(project: string, repo: string, name: string) {
  const [detail, setDetail] = useState<CargoCrateDetail>();
  const [state, setState] = useState<LoadState>('loading');
  const [error, setError] = useState<string>();

  const reload = useCallback(async () => {
    setState('loading');
    try {
      const res = await api.getCargoCrate(project, repo, name);
      setDetail(res);
      setState('ready');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load crate');
      setState('error');
    }
  }, [project, repo, name]);

  useEffect(() => {
    void reload();
  }, [reload]);

  return { detail, state, error, reload };
}

/** useRubyGems loads the global, project-grouped RubyGems gem index. */
export function useRubyGems() {
  const [gems, setGems] = useState<RubyGem[]>([]);
  const [state, setState] = useState<LoadState>('loading');
  const [error, setError] = useState<string>();

  const reload = useCallback(async () => {
    setState('loading');
    try {
      const res = await api.listRubyGems();
      setGems(res.gems);
      setState('ready');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load gems');
      setState('error');
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  return { gems, state, error, reload };
}

/** useRubyGemDetail loads one gem's versions. */
export function useRubyGemDetail(project: string, repo: string, name: string) {
  const [detail, setDetail] = useState<RubyGemDetail>();
  const [state, setState] = useState<LoadState>('loading');
  const [error, setError] = useState<string>();

  const reload = useCallback(async () => {
    setState('loading');
    try {
      const res = await api.getRubyGem(project, repo, name);
      setDetail(res);
      setState('ready');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load gem');
      setState('error');
    }
  }, [project, repo, name]);

  useEffect(() => {
    void reload();
  }, [reload]);

  return { detail, state, error, reload };
}

/** useTerraformModules loads the global, project-grouped Terraform module index. */
export function useTerraformModules() {
  const [modules, setModules] = useState<TerraformModule[]>([]);
  const [state, setState] = useState<LoadState>('loading');
  const [error, setError] = useState<string>();

  const reload = useCallback(async () => {
    setState('loading');
    try {
      const res = await api.listTerraformModules();
      setModules(res.modules);
      setState('ready');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load Terraform modules');
      setState('error');
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  return { modules, state, error, reload };
}

/** useTerraformModuleDetail loads one module's versions. */
export function useTerraformModuleDetail(project: string, repo: string, name: string, provider: string) {
  const [detail, setDetail] = useState<TerraformModuleDetail>();
  const [state, setState] = useState<LoadState>('loading');
  const [error, setError] = useState<string>();

  const reload = useCallback(async () => {
    setState('loading');
    try {
      const res = await api.getTerraformModule(project, repo, name, provider);
      setDetail(res);
      setState('ready');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load module');
      setState('error');
    }
  }, [project, repo, name, provider]);

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
