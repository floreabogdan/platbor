import { useState } from 'react';
import { Button, Card } from '../../components/ui';
import { ApiError } from '../../lib/api';
import { useAuth } from '../../lib/auth';

// Full-screen sign-in. Rendered by the app shell whenever there is no session.
export function LoginPage() {
  const { login } = useAuth();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string>();
  const [submitting, setSubmitting] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setSubmitting(true);
    setError(undefined);
    try {
      await login(username, password);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Could not sign in. Please try again.');
      setSubmitting(false);
    }
  }

  return (
    <div className="app-canvas flex min-h-screen items-center justify-center p-6">
      <div className="w-full max-w-sm animate-rise">
        <div className="mb-6 flex items-center gap-3">
          <div className="grid h-10 w-10 place-items-center rounded-lg bg-gradient-to-br from-teal-400 to-teal-600 shadow-lg shadow-teal-500/20">
            <span className="text-base font-bold text-white">P</span>
          </div>
          <div>
            <div className="text-lg font-semibold tracking-tight text-slate-900">Platbor</div>
            <div className="font-mono text-[10px] uppercase tracking-[0.18em] text-slate-500">
              registry · catalog
            </div>
          </div>
        </div>

        <Card className="p-6">
          <h1 className="mb-1 text-lg font-semibold tracking-tight text-slate-900">Sign in</h1>
          <p className="mb-5 text-sm text-slate-500">Access your registry and catalog.</p>

          <form
            onSubmit={(e) => {
              void submit(e);
            }}
            className="space-y-4"
          >
            <div>
              <label htmlFor="username" className="mb-1 block text-sm font-medium text-slate-700">
                Username
              </label>
              <input
                id="username"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                autoComplete="username"
                required
                autoFocus
                className={inputClass}
              />
            </div>

            <div>
              <label htmlFor="password" className="mb-1 block text-sm font-medium text-slate-700">
                Password
              </label>
              <input
                id="password"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete="current-password"
                required
                className={inputClass}
              />
            </div>

            {error ? (
              <p className="rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700 ring-1 ring-inset ring-red-600/20">
                {error}
              </p>
            ) : null}

            <Button type="submit" disabled={submitting} className="w-full">
              {submitting ? 'Signing in…' : 'Sign in'}
            </Button>
          </form>
        </Card>
      </div>
    </div>
  );
}

const inputClass =
  'w-full rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm text-slate-800 shadow-sm outline-none placeholder:text-slate-400 focus:border-teal-500 focus:ring-2 focus:ring-teal-500/20';
