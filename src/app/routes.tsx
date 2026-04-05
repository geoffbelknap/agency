import { createBrowserRouter, Navigate } from 'react-router';
import { Layout } from './components/Layout';
import { RouteErrorBoundary } from './components/ErrorBoundary';
import { Overview } from './screens/Overview';
import { Agents } from './screens/Agents';
import { Teams } from './screens/Teams';
import { Channels } from './screens/Channels';
import { Admin } from './screens/Admin';
import { KnowledgeExplorer } from './screens/KnowledgeExplorer';
import { MissionList } from './screens/MissionList';
import { MissionDetail } from './screens/MissionDetail';
import { Setup } from './screens/Setup';
import { Profiles } from './screens/Profiles';

export const router = createBrowserRouter([
  { path: '/setup', Component: Setup, ErrorBoundary: RouteErrorBoundary },
  {
    path: '/',
    Component: Layout,
    ErrorBoundary: RouteErrorBoundary,
    children: [
      { index: true, element: <Navigate to="/channels" replace /> },
      { path: 'overview', Component: Overview, ErrorBoundary: RouteErrorBoundary },
      { path: 'channels', Component: Channels, ErrorBoundary: RouteErrorBoundary },
      { path: 'channels/:name', Component: Channels, ErrorBoundary: RouteErrorBoundary },
      { path: 'agents', Component: Agents, ErrorBoundary: RouteErrorBoundary },
      { path: 'agents/:name', Component: Agents, ErrorBoundary: RouteErrorBoundary },
      { path: 'missions', Component: MissionList, ErrorBoundary: RouteErrorBoundary },
      { path: 'missions/:name', Component: MissionDetail, ErrorBoundary: RouteErrorBoundary },
      { path: 'missions/:name/composer', lazy: () => import('./screens/missions/composer/MissionComposer').then(m => ({ Component: m.MissionComposer })), ErrorBoundary: RouteErrorBoundary },
      { path: 'knowledge', Component: KnowledgeExplorer, ErrorBoundary: RouteErrorBoundary },
      { path: 'knowledge/:view', Component: KnowledgeExplorer, ErrorBoundary: RouteErrorBoundary },
      { path: 'profiles', Component: Profiles, ErrorBoundary: RouteErrorBoundary },
      { path: 'profiles/:id', Component: Profiles, ErrorBoundary: RouteErrorBoundary },
      { path: 'teams', Component: Teams, ErrorBoundary: RouteErrorBoundary },
      { path: 'admin', Component: Admin, ErrorBoundary: RouteErrorBoundary },
      { path: 'admin/:tab', Component: Admin, ErrorBoundary: RouteErrorBoundary },
    ],
  },
]);
