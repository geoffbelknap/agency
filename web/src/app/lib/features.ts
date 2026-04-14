import registry from './feature-registry.json';

function envFlag(value: string | boolean | undefined, fallback = false): boolean {
  if (typeof value === 'boolean') return value;
  if (typeof value !== 'string') return fallback;
  switch (value.trim().toLowerCase()) {
    case '1':
    case 'true':
    case 'yes':
    case 'on':
      return true;
    case '0':
    case 'false':
    case 'no':
    case 'off':
      return false;
    default:
      return fallback;
  }
}

export const experimentalSurfacesEnabled = envFlag(import.meta.env.VITE_ENABLE_EXPERIMENTAL_SURFACES, false);

type FeatureTier = 'core' | 'experimental' | 'internal';
type FeatureId = (typeof registry)[number]['id'];

const tierById = new Map<FeatureId, FeatureTier>(
  registry.map((feature) => [feature.id, feature.tier as FeatureTier]),
);

export function featureEnabled(id: FeatureId): boolean {
  const tier = tierById.get(id) ?? 'core';
  switch (tier) {
    case 'core':
      return true;
    case 'experimental':
      return experimentalSurfacesEnabled;
    default:
      return false;
  }
}

export const coreSidebarFlags = {
  missions: featureEnabled('missions'),
  teams: featureEnabled('teams'),
  profiles: featureEnabled('profiles'),
  hub: featureEnabled('hub'),
  intake: featureEnabled('intake'),
} as const;

export const adminFeatureFlags = {
  hub: featureEnabled('hub'),
  intake: featureEnabled('intake'),
  events: featureEnabled('events'),
  webhooks: featureEnabled('webhooks'),
  notifications: featureEnabled('notifications'),
} as const;
