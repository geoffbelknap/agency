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

export const coreSidebarFlags = {
  missions: experimentalSurfacesEnabled,
  teams: experimentalSurfacesEnabled,
  profiles: experimentalSurfacesEnabled,
  hub: experimentalSurfacesEnabled,
  intake: experimentalSurfacesEnabled,
} as const;

export const adminFeatureFlags = {
  hub: experimentalSurfacesEnabled,
  intake: experimentalSurfacesEnabled,
  events: experimentalSurfacesEnabled,
  webhooks: experimentalSurfacesEnabled,
  notifications: experimentalSurfacesEnabled,
} as const;
