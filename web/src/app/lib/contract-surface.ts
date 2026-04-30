import { featureEnabled, type FeatureId, type FeatureTier } from './features';

export type ContractSurface = {
  id: string;
  label: string;
  route: string;
  summary: string;
  tag: string;
  tier: FeatureTier;
  feature?: FeatureId;
  endpoints: string[];
};

export type ContractModule = {
  id: string;
  label: string;
  eyebrow: string;
  summary: string;
  surfaces: ContractSurface[];
};

export const contractModules: ContractModule[] = [
  {
    id: 'operate',
    label: 'Operate',
    eyebrow: 'Core runtime',
    summary: 'Daily operator work: fleet, direct messages, infrastructure state, and knowledge retrieval.',
    surfaces: [
      {
        id: 'overview',
        label: 'Overview',
        route: '/overview',
        summary: 'Runtime readiness across gateway, providers, agents, and mediation surfaces.',
        tag: 'Health / Infra',
        tier: 'core',
        endpoints: ['/health', '/api/v1/infra/status', '/api/v1/infra/routing/config'],
      },
      {
        id: 'agents',
        label: 'Agents',
        route: '/agents',
        summary: 'Create, inspect, start, stop, halt, resume, and audit gateway-managed agents.',
        tag: 'Agents',
        tier: 'core',
        endpoints: ['/api/v1/agents', '/api/v1/agents/{name}', '/api/v1/agents/{name}/logs'],
      },
      {
        id: 'channels',
        label: 'Channels',
        route: '/channels',
        summary: 'Operator conversations and first-class agent direct message establishment.',
        tag: 'Comms',
        tier: 'core',
        endpoints: ['/api/v1/comms/channels', '/api/v1/agents/{name}/dm'],
      },
      {
        id: 'knowledge',
        label: 'Knowledge',
        route: '/knowledge',
        summary: 'Graph-backed context lookup, neighborhood review, and curation queues.',
        tag: 'Graph',
        tier: 'core',
        endpoints: ['/api/v1/graph/query', '/api/v1/graph/stats', '/api/v1/graph/context'],
      },
    ],
  },
  {
    id: 'govern',
    label: 'Govern',
    eyebrow: 'Trust boundary',
    summary: 'Controls that make enforcement, auditability, credentials, and mediation explicit.',
    surfaces: [
      {
        id: 'admin',
        label: 'Admin',
        route: '/admin',
        summary: 'Doctor checks, capability grants, policy validation, egress, and teardown controls.',
        tag: 'Admin',
        tier: 'core',
        endpoints: ['/api/v1/admin/doctor', '/api/v1/admin/capabilities', '/api/v1/admin/egress'],
      },
      {
        id: 'setup',
        label: 'Setup',
        route: '/setup',
        summary: 'Provider credentials, routing defaults, and initial gateway configuration.',
        tag: 'Infra',
        tier: 'core',
        endpoints: ['/api/v1/infra/setup/config', '/api/v1/infra/providers', '/api/v1/creds'],
      },
      {
        id: 'audit',
        label: 'Audit',
        route: '/admin/audit',
        summary: 'Agent logs and audit summaries surfaced through backend contracts.',
        tag: 'Admin / Agents',
        tier: 'core',
        endpoints: ['/api/v1/agents/{name}/logs', '/api/v1/admin/audit/summarize'],
      },
      {
        id: 'mcp',
        label: 'MCP',
        route: '/admin/mcp',
        summary: 'Native MCP server registration and mediation-plane visibility.',
        tag: 'MCP',
        tier: 'core',
        endpoints: ['/api/v1/mcp/*'],
      },
    ],
  },
  {
    id: 'extend',
    label: 'Extend',
    eyebrow: 'Gated surfaces',
    summary: 'Feature-registry surfaces that should remain visibly gated until enabled.',
    surfaces: [
      {
        id: 'missions',
        label: 'Missions',
        route: '/missions',
        summary: 'Experimental workflow orchestration, assignment, health, history, and canvas state.',
        tag: 'Missions',
        tier: 'experimental',
        feature: 'missions',
        endpoints: ['/api/v1/missions', '/api/v1/missions/{name}/assign', '/api/v1/missions/{name}/canvas'],
      },
      {
        id: 'teams',
        label: 'Teams',
        route: '/teams',
        summary: 'Experimental team ownership, roster state, and team activity.',
        tag: 'Admin',
        tier: 'experimental',
        feature: 'teams',
        endpoints: ['/api/v1/admin/teams', '/api/v1/admin/teams/{name}/activity'],
      },
      {
        id: 'profiles',
        label: 'Profiles',
        route: '/profiles',
        summary: 'Experimental reusable profile scaffolds and defaults for agent creation.',
        tag: 'Admin',
        tier: 'experimental',
        feature: 'profiles',
        endpoints: ['/api/v1/admin/profiles'],
      },
      {
        id: 'events',
        label: 'Events',
        route: '/admin/events',
        summary: 'Experimental event stream, subscriptions, notifications, webhooks, and intake work items.',
        tag: 'Events',
        tier: 'experimental',
        feature: 'events',
        endpoints: ['/api/v1/events', '/api/v1/events/notifications', '/api/v1/events/webhooks'],
      },
      {
        id: 'hub',
        label: 'Hub',
        route: '/admin/hub',
        summary: 'Experimental package distribution and registry administration.',
        tag: 'Platform',
        tier: 'experimental',
        feature: 'hub',
        endpoints: ['/api/v1/admin/hub', '/api/v1/admin/packages'],
      },
      {
        id: 'intake',
        label: 'Intake',
        route: '/admin/intake',
        summary: 'Experimental inbound work intake and triage controls.',
        tag: 'Events',
        tier: 'experimental',
        feature: 'intake',
        endpoints: ['/api/v1/events/intake/items', '/api/v1/events/intake/stats'],
      },
    ],
  },
];

export const contractSurfaces = contractModules.flatMap((module) => module.surfaces);

export function surfaceIsVisible(surface: ContractSurface) {
  return !surface.feature || featureEnabled(surface.feature);
}

export function moduleVisibleSurfaces(module: ContractModule) {
  return module.surfaces.filter(surfaceIsVisible);
}

export function findSurfaceForPath(pathname: string) {
  const exact = contractSurfaces.find((surface) => pathname === surface.route);
  if (exact) return exact;
  return contractSurfaces
    .filter((surface) => surface.route !== '/overview' && pathname.startsWith(`${surface.route}/`))
    .sort((a, b) => b.route.length - a.route.length)[0] ?? contractSurfaces[0];
}

export function findModuleForSurface(surfaceId: string) {
  return contractModules.find((module) => module.surfaces.some((surface) => surface.id === surfaceId)) ?? contractModules[0];
}
