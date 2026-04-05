// Caches the operator's display name so message mappings can resolve _operator → real name.
import { api } from './api';

let operatorDisplayName = 'operator';
let fetched = false;

export function getOperatorDisplayName(): string {
  return operatorDisplayName;
}

export async function fetchOperatorDisplayName(): Promise<string> {
  if (fetched) return operatorDisplayName;
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
