import { createBrowserRouter, Navigate } from 'react-router';
import { Layout } from './components/Layout';
import { RouteErrorBoundary } from './components/ErrorBoundary';
import { featureEnabled } from './lib/features';

const overviewRoute = () => import('./screens/Overview').then((m) => ({ Component: m.Overview }));
const channelsRoute = () => import('./screens/Channels').then((m) => ({ Component: m.Channels }));
const agentsRoute = () => import('./screens/Agents').then((m) => ({ Component: m.Agents }));
const knowledgeRoute = () => import('./screens/KnowledgeExplorer').then((m) => ({ Component: m.KnowledgeExplorer }));
const adminRoute = () => import('./screens/Admin').then((m) => ({ Component: m.Admin }));
const setupRoute = () => import('./screens/Setup').then((m) => ({ Component: m.Setup }));
const missionsRoute = () => import('./screens/MissionList').then((m) => ({ Component: m.MissionList }));
const missionDetailRoute = () => import('./screens/MissionDetail').then((m) => ({ Component: m.MissionDetail }));
const profilesRoute = () => import('./screens/Profiles').then((m) => ({ Component: m.Profiles }));
const teamsRoute = () => import('./screens/Teams').then((m) => ({ Component: m.Teams }));

export const router = createBrowserRouter([
  { path: '/setup', lazy: setupRoute, ErrorBoundary: RouteErrorBoundary },
  {
    path: '/',
    Component: Layout,
    ErrorBoundary: RouteErrorBoundary,
    children: [
      { index: true, element: <Navigate to="/overview" replace /> },
      { path: 'overview', lazy: overviewRoute, ErrorBoundary: RouteErrorBoundary },
      { path: 'channels', lazy: channelsRoute, ErrorBoundary: RouteErrorBoundary },
      { path: 'channels/:name', lazy: channelsRoute, ErrorBoundary: RouteErrorBoundary },
      { path: 'agents', lazy: agentsRoute, ErrorBoundary: RouteErrorBoundary },
      { path: 'agents/:name', lazy: agentsRoute, ErrorBoundary: RouteErrorBoundary },
      { path: 'knowledge', lazy: knowledgeRoute, ErrorBoundary: RouteErrorBoundary },
      { path: 'knowledge/:view', lazy: knowledgeRoute, ErrorBoundary: RouteErrorBoundary },
      { path: 'admin', lazy: adminRoute, ErrorBoundary: RouteErrorBoundary },
      { path: 'admin/:tab', lazy: adminRoute, ErrorBoundary: RouteErrorBoundary },
      ...(featureEnabled('missions')
        ? [
            { path: 'missions', lazy: missionsRoute, ErrorBoundary: RouteErrorBoundary },
            { path: 'missions/:name', lazy: missionDetailRoute, ErrorBoundary: RouteErrorBoundary },
            { path: 'missions/:name/composer', lazy: () => import('./screens/missions/composer/MissionComposer').then(m => ({ Component: m.MissionComposer })), ErrorBoundary: RouteErrorBoundary },
          ]
        : [
            { path: 'missions', element: <Navigate to="/overview" replace /> },
            { path: 'missions/:name', element: <Navigate to="/overview" replace /> },
            { path: 'missions/:name/composer', element: <Navigate to="/overview" replace /> },
          ]),
      ...(featureEnabled('profiles')
        ? [
            { path: 'profiles', lazy: profilesRoute, ErrorBoundary: RouteErrorBoundary },
            { path: 'profiles/:id', lazy: profilesRoute, ErrorBoundary: RouteErrorBoundary },
          ]
        : [
            { path: 'profiles', element: <Navigate to="/overview" replace /> },
            { path: 'profiles/:id', element: <Navigate to="/overview" replace /> },
          ]),
      ...(featureEnabled('teams')
        ? [
            { path: 'teams', lazy: teamsRoute, ErrorBoundary: RouteErrorBoundary },
          ]
        : [
            { path: 'teams', element: <Navigate to="/overview" replace /> },
          ]),
    ],
  },
]);
