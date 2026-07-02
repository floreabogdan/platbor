import { Navigate, Route, Routes } from 'react-router-dom';
import { Layout } from '../components/Layout';
import { DashboardPage } from '../features/dashboard/DashboardPage';
import { ProjectsPage } from '../features/projects/ProjectsPage';
import { PlaceholderPage } from '../features/placeholder/PlaceholderPage';

// Route table. Feature pages live under src/features/<name>/ and never import
// one another (docs/CODING-STANDARDS.md).
export function App() {
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route index element={<DashboardPage />} />
        <Route path="projects" element={<ProjectsPage />} />
        <Route
          path="registry"
          element={<PlaceholderPage title="Registry" subtitle="Container images and packages land in Phase 1." />}
        />
        <Route
          path="catalog"
          element={<PlaceholderPage title="Catalog" subtitle="Components, owners, and dependencies land in Phase 3." />}
        />
        <Route
          path="settings"
          element={<PlaceholderPage title="Settings" subtitle="Users, tokens, and instance config." />}
        />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  );
}
