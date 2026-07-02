import { useCallback, useEffect, useState } from 'react';
import { api } from '../../lib/api';
import type { Token } from '../../lib/types';

type LoadState = 'loading' | 'ready' | 'error';

interface UseTokens {
  tokens: Token[];
  state: LoadState;
  error?: string;
  reload: () => Promise<void>;
}

// Server state for the current user's personal access tokens.
export function useTokens(): UseTokens {
  const [tokens, setTokens] = useState<Token[]>([]);
  const [state, setState] = useState<LoadState>('loading');
  const [error, setError] = useState<string>();

  const reload = useCallback(async () => {
    setState('loading');
    try {
      const res = await api.listTokens();
      setTokens(res.tokens);
      setState('ready');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load tokens');
      setState('error');
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  return { tokens, state, error, reload };
}
