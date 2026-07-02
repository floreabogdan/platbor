import { useCallback, useEffect, useState } from 'react';
import { api } from '../../lib/api';
import type { DashboardResponse } from '../../lib/types';

type LoadState = 'loading' | 'ready' | 'error';

// useDashboard loads the summary counts and recent-activity feed in one request.
export function useDashboard() {
  const [data, setData] = useState<DashboardResponse>();
  const [state, setState] = useState<LoadState>('loading');
  const [error, setError] = useState<string>();

  const reload = useCallback(async () => {
    setState('loading');
    try {
      const res = await api.getDashboard();
      setData(res);
      setState('ready');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load dashboard');
      setState('error');
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  return { data, state, error, reload };
}
