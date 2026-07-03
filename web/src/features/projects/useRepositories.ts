import { useCallback, useEffect, useState } from 'react';
import { api } from '../../lib/api';
import type { Repo } from '../../lib/types';

type LoadState = 'loading' | 'ready' | 'error';

/** useRepositories loads the typed repositories inside a project. */
export function useRepositories(project: string) {
  const [repos, setRepos] = useState<Repo[]>([]);
  const [state, setState] = useState<LoadState>('loading');
  const [error, setError] = useState<string>();

  const reload = useCallback(async () => {
    setState('loading');
    try {
      const res = await api.listRepos(project);
      setRepos(res.repositories);
      setState('ready');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load repositories');
      setState('error');
    }
  }, [project]);

  useEffect(() => {
    void reload();
  }, [reload]);

  return { repos, state, error, reload };
}
