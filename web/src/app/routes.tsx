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
const missionComposerRoute = () => import('./screens/missions/composer/MissionComposer').then((m) => ({ Component: m.MissionComposer }));
const profilesRoute = () => import('./screens/Profiles').then((m) => ({ Component: m.Profiles }));
const teamsRoute = () => import('./screens/Teams').then((m) => ({ Component: m.Teams }));

function RouteFallback() {
  return <div className="min-h-0 flex-1 bg-background" />;
}

const routeMeta = {
  ErrorBoundary: RouteErrorBoundary,
  HydrateFallback: RouteFallback,
};

export const router = createBrowserRouter([
  { path: '/setup', lazy: setupRoute, ...routeMeta },
  {
    path: '/',
    Component: Layout,
    ...routeMeta,
    children: [
      { index: true, element: <Navigate to="/overview" replace /> },
      { path: 'overview', lazy: overviewRoute, ...routeMeta },
      { path: 'channels', lazy: channelsRoute, ...routeMeta },
      { path: 'channels/:name', lazy: channelsRoute, ...routeMeta },
      { path: 'agents', lazy: agentsRoute, ...routeMeta },
      { path: 'agents/:name', lazy: agentsRoute, ...routeMeta },
      { path: 'knowledge', lazy: knowledgeRoute, ...routeMeta },
      { path: 'knowledge/:view', lazy: knowledgeRoute, ...routeMeta },
      { path: 'admin', lazy: adminRoute, ...routeMeta },
      { path: 'admin/:tab', lazy: adminRoute, ...routeMeta },
      ...(featureEnabled('missions')
        ? [
            { path: 'missions', lazy: missionsRoute, ...routeMeta },
            { path: 'missions/:name', lazy: missionDetailRoute, ...routeMeta },
            { path: 'missions/:name/composer', lazy: missionComposerRoute, ...routeMeta },
          ]
        : [
            { path: 'missions', element: <Navigate to="/overview" replace /> },
            { path: 'missions/:name', element: <Navigate to="/overview" replace /> },
            { path: 'missions/:name/composer', element: <Navigate to="/overview" replace /> },
          ]),
      ...(featureEnabled('profiles')
        ? [
            { path: 'profiles', lazy: profilesRoute, ...routeMeta },
            { path: 'profiles/:id', lazy: profilesRoute, ...routeMeta },
          ]
        : [
            { path: 'profiles', element: <Navigate to="/overview" replace /> },
            { path: 'profiles/:id', element: <Navigate to="/overview" replace /> },
          ]),
      ...(featureEnabled('teams')
        ? [
            { path: 'teams', lazy: teamsRoute, ...routeMeta },
          ]
        : [
            { path: 'teams', element: <Navigate to="/overview" replace /> },
          ]),
    ],
  },
]);
