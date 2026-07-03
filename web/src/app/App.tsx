import { Navigate, Route, Routes } from 'react-router-dom';
import { Layout } from '../components/Layout';
import { AuthProvider, useAuth } from '../lib/auth';
import { LoginPage } from '../features/auth/LoginPage';
import { DashboardPage } from '../features/dashboard/DashboardPage';
import { ProjectsPage } from '../features/projects/ProjectsPage';
import { ProjectPage } from '../features/projects/ProjectPage';
import { BucketPage } from '../features/projects/BucketPage';
import { ProfilePage } from '../features/profile/ProfilePage';
import { RegistryPage } from '../features/registry/RegistryPage';
import { RepositoryPage } from '../features/registry/RepositoryPage';
import { PackagePage } from '../features/registry/PackagePage';
import { NugetPage } from '../features/registry/NugetPage';
import { PypiPage } from '../features/registry/PypiPage';
import { MavenPage } from '../features/registry/MavenPage';
import { SettingsPage } from '../features/settings/SettingsPage';
import { PlaceholderPage } from '../features/placeholder/PlaceholderPage';

// Route table. Feature pages live under src/features/<name>/ and never import
// one another (docs/CODING-STANDARDS.md).
export function App() {
  return (
    <AuthProvider>
      <Gate />
    </AuthProvider>
  );
}

// Gate renders the login screen until there is a session, then the app shell.
function Gate() {
  const { state } = useAuth();

  if (state.status === 'loading') {
    return (
      <div className="app-canvas grid min-h-screen place-items-center">
        <div className="h-6 w-6 animate-spin rounded-full border-2 border-slate-300 border-t-teal-600" />
      </div>
    );
  }

  if (state.status === 'anonymous') {
    return <LoginPage />;
  }

  return (
    <Routes>
      <Route element={<Layout />}>
        <Route index element={<DashboardPage />} />
        <Route path="projects" element={<ProjectsPage />} />
        <Route path="projects/:key" element={<ProjectPage />} />
        <Route path="projects/:key/buckets/:repo" element={<BucketPage />} />
        <Route path="registry" element={<RegistryPage />} />
        {/* npm and NuGet package detail — reserved sentinel segments ("-" and
            "-nuget-", which an OCI repo name can never start with) keep these
            distinct from the OCI repo route below. */}
        <Route path="registry/:project/-/*" element={<PackagePage />} />
        <Route path="registry/:project/-nuget-/*" element={<NugetPage />} />
        <Route path="registry/:project/-pypi-/*" element={<PypiPage />} />
        <Route path="registry/:project/-maven-/*" element={<MavenPage />} />
        <Route path="registry/:project/*" element={<RepositoryPage />} />
        <Route
          path="catalog"
          element={<PlaceholderPage title="Catalog" subtitle="Components, owners, and dependencies land in Phase 3." />}
        />
        <Route path="profile" element={<ProfilePage />} />
        <Route path="settings" element={<SettingsPage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  );
}
