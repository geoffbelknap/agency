// Caches the operator's display name so message mappings can resolve _operator → real name.
import { api } from './api';
import { featureEnabled } from './features';

let operatorDisplayName = 'operator';
let fetched = false;

export function getOperatorDisplayName(): string {
  return operatorDisplayName;
}

export async function fetchOperatorDisplayName(): Promise<string> {
  if (fetched) return operatorDisplayName;
  if (!featureEnabled('profiles')) {
    fetched = true;
    return operatorDisplayName;
  }
  try {
    const profiles = await api.profiles.list('operator');
    if (profiles && profiles.length > 0) {
      operatorDisplayName = profiles[0].display_name || profiles[0].id || 'operator';
    }
    fetched = true;
  } catch {
    // Fall back to 'operator' — non-critical
  }
  return operatorDisplayName;
}
