import { useCallback, useEffect, useState } from 'react';
import { api } from '../../lib/api';
import type { Project } from '../../lib/types';

type LoadState = 'loading' | 'ready' | 'error';

interface UseProjects {
  projects: Project[];
  state: LoadState;
  error?: string;
  reload: () => Promise<void>;
}

// Server state lives in this hook, not in a global store (KISS —
// docs/CODING-STANDARDS.md). It fetches on mount and exposes a manual reload
// for after mutations.
export function useProjects(): UseProjects {
  const [projects, setProjects] = useState<Project[]>([]);
  const [state, setState] = useState<LoadState>('loading');
  const [error, setError] = useState<string>();

  const reload = useCallback(async () => {
    setState('loading');
    try {
      const res = await api.listProjects({ limit: 100 });
      setProjects(res.projects);
      setState('ready');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load projects');
      setState('error');
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  return { projects, state, error, reload };
}
