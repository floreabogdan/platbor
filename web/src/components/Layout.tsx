// App shell: dark ink sidebar + textured canvas main pane (docs/DESIGN-SYSTEM.md).
import { NavLink, Outlet } from 'react-router-dom';
import type { ReactNode } from 'react';
import { cx } from '../lib/cx';
import { useAuth } from '../lib/auth';
import { CatalogIcon, DashboardIcon, LogoutIcon, ProjectsIcon, RegistryIcon, SettingsIcon } from './icons';

interface NavItem {
  to: string;
  label: string;
  icon: ReactNode;
}

interface NavGroup {
  label: string;
  items: NavItem[];
}

// The sidebar separates two axes that used to sit as look-alike peers:
// "Browse" is the content axis (what kind of artifact — global, cross-project),
// while "Manage" is the container axis (projects are the tenant boundary that
// scopes everything, not another browse view). See docs/DESIGN-SYSTEM.md.
const NAV: NavGroup[] = [
  {
    label: 'Overview',
    items: [{ to: '/', label: 'Dashboard', icon: <DashboardIcon /> }],
  },
  {
    label: 'Browse',
    items: [
      { to: '/registry', label: 'Registry', icon: <RegistryIcon /> },
      { to: '/catalog', label: 'Catalog', icon: <CatalogIcon /> },
    ],
  },
  {
    label: 'Manage',
    items: [{ to: '/projects', label: 'Projects', icon: <ProjectsIcon /> }],
  },
  {
    label: 'Admin',
    items: [{ to: '/settings', label: 'Settings', icon: <SettingsIcon /> }],
  },
];

export function Layout() {
  return (
    <div className="flex h-screen overflow-hidden">
      <Sidebar />
      <main className="app-canvas flex-1 overflow-y-auto">
        <div className="px-8 py-7">
          <Outlet />
        </div>
      </main>
    </div>
  );
}

function Sidebar() {
  return (
    <aside className="flex w-64 flex-col bg-ink-900 text-slate-300">
      <div className="flex items-center gap-3 px-5 py-6">
        <div className="grid h-9 w-9 place-items-center rounded-lg bg-gradient-to-br from-teal-400 to-teal-600 shadow-lg shadow-teal-500/20">
          <span className="text-sm font-bold text-white">P</span>
        </div>
        <div>
          <div className="text-sm font-semibold text-white">Platbor</div>
          <div className="font-mono text-[10px] uppercase tracking-[0.18em] text-slate-500">
            registry · catalog
          </div>
        </div>
      </div>

      <nav className="flex-1 space-y-6 px-3 py-2">
        {NAV.map((group) => (
          <div key={group.label}>
            <div className="px-3 pb-2 font-mono text-[10px] uppercase tracking-[0.18em] text-slate-500">
              {group.label}
            </div>
            <div className="space-y-1">
              {group.items.map((item) => (
                <NavItemLink key={item.to} item={item} />
              ))}
            </div>
          </div>
        ))}
      </nav>

      <UserBlock />
    </aside>
  );
}

function UserBlock() {
  const { state, logout } = useAuth();
  const user = state.status === 'authenticated' ? state.user : undefined;

  return (
    <div className="border-t border-white/5 px-5 py-4">
      <div className="flex items-center gap-2">
        <NavLink
          to="/profile"
          title="Profile"
          className="flex min-w-0 flex-1 items-center gap-3 rounded-lg p-1 transition-colors hover:bg-white/5"
        >
          <div className="grid h-8 w-8 shrink-0 place-items-center rounded-full bg-white/10 text-xs font-semibold uppercase text-white">
            {user ? user.username.charAt(0) : '?'}
          </div>
          <div className="min-w-0 flex-1 text-left">
            <div className="truncate text-sm font-medium text-white">{user?.username ?? 'unknown'}</div>
            <div className="truncate text-xs text-slate-500">
              {user?.isAdmin ? 'instance admin' : 'member'}
            </div>
          </div>
        </NavLink>
        <button
          type="button"
          onClick={() => void logout()}
          title="Sign out"
          aria-label="Sign out"
          className="shrink-0 rounded-md p-1.5 text-slate-400 transition-colors hover:bg-white/5 hover:text-white"
        >
          <LogoutIcon />
        </button>
      </div>
    </div>
  );
}

function NavItemLink({ item }: { item: NavItem }) {
  return (
    <NavLink
      to={item.to}
      end={item.to === '/'}
      className={({ isActive }) =>
        cx(
          'flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium transition-all',
          isActive
            ? 'bg-white/10 text-white shadow-sm ring-1 ring-white/10'
            : 'text-slate-400 hover:bg-white/5 hover:text-white',
        )
      }
    >
      {item.icon}
      {item.label}
    </NavLink>
  );
}
