let BASE = import.meta.env.VITE_API_BASE_URL || '/api/v1';
let TOKEN = import.meta.env.VITE_API_TOKEN || '';

/** Connection mode: "relay" when accessed through the relay gateway, "local" otherwise. */
let VIA: 'relay' | 'local' = 'local';
/** Whether the relay session is authenticated. */
let AUTHENTICATED = false;

// Auto-detect gateway config from local Vite server (dev mode only).
// The agencyAutoConfig plugin in vite.config.ts serves the token and
// gateway address read from ~/.agency/config.yaml.
let _configPromise: Promise<void> | null = null;
function ensureConfig(): Promise<void> {
  if (!_configPromise) {
    _configPromise = (async () => {
      try {
        const res = await fetch('/__agency/config');
        if (res.ok) {
          const cfg = await res.json();
          if (cfg.token && !import.meta.env.VITE_API_TOKEN) TOKEN = cfg.token;
          if (cfg.gateway && !import.meta.env.VITE_API_BASE_URL) BASE = `${cfg.gateway}/api/v1`;
          // Empty gateway means same-origin (Vite proxy in dev mode)
          if (cfg.gateway === '' && !import.meta.env.VITE_API_BASE_URL) BASE = '/api/v1';
          // Relay connection metadata
          if (cfg.via === 'relay') VIA = 'relay';
          if (cfg.authenticated === true) AUTHENTICATED = true;
        }
      } catch {
        // Not in dev mode or plugin not available — use defaults/env vars
      }
    })();
  }
  return _configPromise;
}

// Kick off config fetch immediately
ensureConfig();

/** Expose token + config for other modules (e.g. WebSocket auth). */
export { ensureConfig };
export function getToken(): string { return TOKEN; }
/** Returns "relay" when accessed through the relay gateway, "local" otherwise. */
export function getVia(): 'relay' | 'local' { return VIA; }
/** Returns true when the relay session is authenticated. */
export function getAuthenticated(): boolean { return AUTHENTICATED; }

function extractErrorDetail(text: string, status: number, path: string): string {
  const trimmed = text.trim();
  if (!trimmed) return `API ${path} returned ${status}`;

  try {
    const parsed = JSON.parse(trimmed);
    if (typeof parsed?.error === 'string' && parsed.error.trim()) return parsed.error.trim();
    if (typeof parsed?.message === 'string' && parsed.message.trim()) return parsed.message.trim();
  } catch {
    // Fall through to text/html cleanup.
  }

  const title = trimmed.match(/<title>(.*?)<\/title>/is)?.[1] || trimmed.match(/<h1>(.*?)<\/h1>/is)?.[1];
  const withoutTags = (title || trimmed)
    .replace(/<script[\s\S]*?<\/script>/gi, ' ')
    .replace(/<style[\s\S]*?<\/style>/gi, ' ')
    .replace(/<!--[\s\S]*?-->/g, ' ')
    .replace(/<[^>]+>/g, ' ')
    .replace(/\s+/g, ' ')
    .trim();

  if (status === 504 || /gateway time-out|gateway timeout/i.test(withoutTags)) {
    if (/\/agents\/[^/]+\/start$/.test(path)) {
      return 'Gateway timed out while waiting for the agent to start. Startup may still be running; refresh the agent status or retry.';
    }
    return 'Gateway timed out while waiting for the request to complete. The operation may still be running; refresh status or retry.';
  }
  return withoutTags || `API ${path} returned ${status}`;
}

/** Authenticated fetch — attaches Bearer token to any request. */
export async function authenticatedFetch(url: string, options?: RequestInit): Promise<Response> {
  await ensureConfig();
  const headers: Record<string, string> = {
    ...(TOKEN ? { Authorization: `Bearer ${TOKEN}` } : {}),
    ...options?.headers as Record<string, string>,
  };
  return fetch(url, { ...options, headers });
}

async function req<T>(path: string, options?: RequestInit): Promise<T> {
  await ensureConfig();
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(TOKEN ? { Authorization: `Bearer ${TOKEN}` } : {}),
    ...options?.headers as Record<string, string>,
  };
  const res = await fetch(`${BASE}${path}`, {
    ...options,
    headers,
  });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(extractErrorDetail(text, res.status, path));
  }
  return res.json();
}

// ── Raw API response shapes (snake_case, matching gateway JSON) ──

export interface RawAgent {
  name: string;
  status: string;
  mode?: string;
  type?: string;
  preset?: string;
  team?: string;
  enforcer?: string;
  model?: string;
  role?: string;
  uptime?: string;
  last_active?: string;
  trust_level?: number;
  restrictions?: string[];
  granted_capabilities?: string[];
  current_task?: { task_id: string; content: string; timestamp: string; source?: string };
  build_id?: string;
  mission?: string;
  mission_status?: string;
}

export interface RawAgentStartProgress {
  type: string;
  phase?: number;
  name?: string;
  description?: string;
  agent?: string;
  model?: string;
  phases?: number;
  error?: string;
  elapsed_ms?: number;
  phase_elapsed_ms?: number;
}

export interface RawAgentRuntimeStatus {
  runtimeId?: string;
  agentId?: string;
  phase?: string;
  healthy?: boolean;
  backend?: string;
  backendEndpoint?: string;
  backendMode?: string;
  transport?: {
    type?: string;
    endpoint?: string;
    enforcerConnected?: boolean;
    lastError?: string;
  };
}

export interface RawChannel {
  name: string;
  topic?: string;
  unread?: number;
  mentions?: number;
  members?: string[];
  state?: string;
  type?: string;
  availability?: string;
}

export interface RawMessage {
  id?: string;
  timestamp?: string;
  author: string;
  content: string;
  reply_to?: string;
  flags?: Record<string, boolean>;
  metadata?: Record<string, unknown>;
  reactions?: Array<{ emoji: string; author: string }>;
}

export interface RawRoutingSuggestion {
  id: string;
  task_type: string;
  current_model: string;
  suggested_model: string;
  reason: string;
  savings_percent: number;
  savings_usd_per_1k: number;
  status: 'pending' | 'approved' | 'rejected' | string;
  created_at?: string;
  current_stats?: Record<string, unknown>;
  suggested_stats?: Record<string, unknown>;
}

export interface RawRoutingStat {
  model: string;
  task_type: string;
  total_calls: number;
  retries: number;
  success_rate: number;
  avg_latency_ms: number;
  avg_input_tokens: number;
  avg_output_tokens: number;
  total_cost_usd: number;
  cost_per_1k: number;
}

export interface RawTeam {
  name: string;
  members?: string[];
  member_count?: number;
  created?: string;
}

export interface RawInfraService {
  name: string;
  state: string;
  health: string;
  container_id?: string;
  uptime?: string;
  build_id?: string;
}

export interface RawInfraStatus {
  version?: string;
  build_id?: string;
  components: RawInfraService[];
}

export interface RawInfraCapacity {
  host_memory_mb: number;
  host_cpu_cores: number;
  system_reserve_mb: number;
  infra_overhead_mb: number;
  max_agents: number;
  max_concurrent_meesks: number;
  agent_slot_mb: number;
  meeseeks_slot_mb: number;
  network_pool_configured: boolean;
  running_agents: number;
  running_meeseeks: number;
  available_slots: number;
}

export interface RawHubComponent {
  name?: string;
  component?: string;
  kind: string;
  version?: string;
  description?: string;
  source?: string;
  installed_at?: string;
}

export interface RawPackageRef {
  kind: string;
  name: string;
  version?: string;
}

export interface RawInstalledPackage {
  kind: string;
  name: string;
  version?: string;
  trust?: string;
  installed?: string;
  path?: string;
  spec?: Record<string, unknown>;
  assurance?: string[];
  assurance_issuer?: string;
  assurance_statements?: Array<{
    artifact_kind?: string;
    artifact_name?: string;
    artifact_version?: string;
    issuer_hub_id?: string;
    statement_type: string;
    result: string;
    review_scope?: string;
    reviewer_type?: string;
    policy_version?: string;
  }>;
  publisher?: string;
  review_scope?: string;
}

export interface RawPackageListResponse {
  packages: RawInstalledPackage[];
}

export interface RawInstanceSource {
  template?: RawPackageRef;
  package?: RawPackageRef;
}

export interface RawInstanceNode {
  id: string;
  kind: string;
  package?: RawPackageRef;
  config?: Record<string, unknown>;
}

export interface RawInstanceGrant {
  principal: string;
  action: string;
  resource?: string;
  config?: Record<string, unknown>;
}

export interface RawInstanceBinding {
  type: string;
  target?: string;
  config?: Record<string, unknown>;
}

export interface RawInstanceClaim {
  owner: string;
  claimed_at: string;
}

export interface RawInstance {
  id: string;
  name: string;
  source: RawInstanceSource;
  nodes?: RawInstanceNode[];
  grants?: RawInstanceGrant[];
  credentials?: Record<string, RawInstanceBinding>;
  config?: Record<string, unknown>;
  relationships?: Array<{ from: string; to: string; type: string }>;
  claim?: RawInstanceClaim;
  created_at?: string;
  updated_at?: string;
}

export interface RawInstanceListResponse {
  instances: RawInstance[];
}

export interface RawRuntimeNodeStatus {
  node_id: string;
  state: string;
  updated_at?: string;
  started_at?: string;
  stopped_at?: string;
  pid?: number;
  port?: number;
  url?: string;
  last_error?: string;
  runtime_path?: string;
}

export interface RawInstanceApplyResult {
  status: string;
  instance: RawInstance;
  manifest?: Record<string, unknown>;
  nodes?: RawRuntimeNodeStatus[];
}

export interface RawConnector {
  id: string;
  name: string;
  kind: string;
  source: string;
  state: string;
  created?: string;
}

export interface RawWorkItem {
  id: string;
  state: string;
  source: string;
  summary: string;
  created: string;
  payload?: unknown;
}

export interface RawCapability {
  name: string;
  kind: string;
  state: string;
  agents?: string[];
  description?: string;
  spec?: Record<string, unknown>;
}

export interface RawProviderToolProvider {
  status: string;
  request_tools?: string[];
  generic_tools?: string[];
  request_fields?: string[];
  required_headers?: string[];
  endpoints?: string[];
  capability_alias?: string;
  pricing?: Record<string, unknown>;
  tests?: string[];
  notes?: string;
}

export interface RawProviderToolCapability {
  title: string;
  risk: string;
  default_grant: boolean;
  execution: string;
  description: string;
  providers?: Record<string, RawProviderToolProvider>;
}

export interface RawProviderToolInventory {
  version: string;
  capabilities?: Record<string, RawProviderToolCapability>;
}

export interface RawRoutingConfig {
  configured: boolean;
  version?: string;
  providers?: Record<string, {
    api_base?: string;
    auth_env?: string;
    caching?: boolean;
  }>;
  models?: Record<string, {
    provider?: string;
    provider_model?: string;
    provider_tool_capabilities?: string[];
    cost_per_mtok_in?: number;
    cost_per_mtok_out?: number;
    cost_per_mtok_cached?: number;
  }>;
  tiers?: Record<string, Array<{ model: string; preference?: number }>>;
  settings?: {
    default_tier?: string;
    tier_strategy?: string;
    default_timeout?: number;
  };
  error?: string;
}

export interface RawAuditEntry {
  [key: string]: unknown;
  // Core fields
  timestamp?: string;
  ts?: string;
  event?: string;
  type?: string;
  detail?: string;
  source?: string;
  // Task fields
  task_content?: string;
  task_id?: string;
  delivered_by?: string;
  initiator?: string;
  // Agent/execution fields
  agent?: string;
  agent_name?: string;
  mode?: string;
  phase?: string | number;
  phase_name?: string;
  // Security/capability fields
  capability?: string;
  provider_tool_capability?: string;
  provider_tool_capabilities?: string;
  provider_tool_type?: string;
  provider_tool_types?: string;
  provider_source_count?: string | number;
  provider_citation_count?: string | number;
  provider_search_query_count?: string | number;
  provider_source_urls?: string;
  reason?: string;
  error?: string;
  domain?: string;
  host?: string;
  scan_type?: string;
  scan_surface?: string;
  scan_action?: string;
  scan_mode?: string;
  finding_count?: number;
  findings?: string[];
  content_sha256?: string;
  content_bytes?: number;
  content_count?: number;
  // LLM/HTTP fields
  model?: string;
  duration_ms?: number;
  input_tokens?: number;
  output_tokens?: number;
  cost?: number;
  status?: number;
  method?: string;
  url?: string;
  path?: string;
  // Tool call fields
  tool?: string;
  name?: string;
  args?: Record<string, unknown>;
  // Signal data
  data?: Record<string, unknown>;
}

export interface RawDoctorResult {
  checks: Array<{ name: string; agent?: string; scope?: string; backend?: string; status: string; detail?: string; fix?: string }>;
}

export interface RawPruneImagesResult {
  status: string;
  pruned: number;
  skipped: number;
}

export interface RawKnowledgeStats {
  node_count: number;
  edge_count: number;
  source_count: number;
  avg_confidence: number;
}

export interface RawPolicyValidation {
  valid?: boolean;
  violations?: string[];
  effective?: Record<string, unknown>;
}

export interface RawEgress {
  allowed_domains?: Array<string | { domain?: string; approved_by?: string; reason?: string; approved_at?: string }>;
  domains?: Array<string | { domain?: string; approved_by?: string; reason?: string; approved_at?: string }>;
  mode?: string;
  agent?: string;
  [key: string]: unknown;
}

export interface RawMissionTrigger {
  source: string;
  connector?: string;
  channel?: string;
  event_type?: string;
  match?: string;
  name?: string;
  cron?: string;
}

export interface RawMission {
  id?: string;
  name: string;
  description?: string;
  version?: number;
  status: string;
  assigned_to?: string;
  assigned_type?: string;
  instructions?: string;
  triggers?: RawMissionTrigger[];
  requires?: { capabilities?: string[]; channels?: string[] };
  health?: { indicators?: string[]; business_hours?: string };
  budget?: { daily?: number; monthly?: number; per_task?: number };
  meeseeks?: boolean;
  meeseeks_limit?: number;
  meeseeks_model?: string;
  meeseeks_budget?: number;
  cost_mode?: string;
  min_task_tier?: string;
  reflection?: { enabled: boolean; max_rounds: number; criteria: string[] };
  success_criteria?: { checklist: { id: string; description: string; required: boolean }[]; evaluation: { enabled: boolean; mode: string; model?: string; on_failure: string; max_retries?: number } };
  fallback?: { policies: unknown[]; default_policy?: { strategy: unknown[] } };
  procedural_memory?: { capture: boolean; retrieve: boolean; max_retrieved: number };
  episodic_memory?: { capture: boolean; retrieve: boolean; max_retrieved: number; tool_enabled: boolean };
  has_canvas?: boolean;
}

export interface MissionHealthCheck {
  name: string;
  status: 'pass' | 'warn' | 'fail';
  detail: string;
  fix?: string;
}

export interface MissionHealthResponse {
  mission: string;
  status: 'healthy' | 'degraded' | 'unhealthy' | 'unknown';
  checks: MissionHealthCheck[];
  summary: string;
}

export interface RawEvent {
  id: string;
  source_type: string;
  source_name: string;
  event_type: string;
  timestamp: string;
  data?: Record<string, unknown>;
  metadata?: Record<string, unknown>;
}

export interface RawMeeseeks {
  id: string;
  parent_agent: string;
  parent_mission_id?: string;
  task: string;
  tools?: string[];
  model?: string;
  budget?: number;
  budget_used?: number;
  channel?: string;
  status: string;
  orphaned?: boolean;
  spawned_at?: string;
  completed_at?: string;
  container_name?: string;
  enforcer_name?: string;
  network_name?: string;
}

export interface RawWebhook {
  name: string;
  event_type: string;
  secret?: string;
  url: string;
  created_at?: string;
}

export interface RawTaskUsage {
  task_id: string;
  mission_id?: string;
  cost_usd: number;
  input_tokens: number;
  output_tokens: number;
  cached_tokens?: number;
  llm_calls: number;
  model?: string;
  started_at?: string;
  ended_at?: string;
}

export interface RawBudgetResponse {
  agent_name: string;
  daily_used: number;
  daily_limit: number;
  daily_remaining: number;
  monthly_used: number;
  monthly_limit: number;
  monthly_remaining: number;
  per_task_limit?: number;
  today_llm_calls: number;
  today_input_tokens: number;
  today_output_tokens: number;
  task_usage?: RawTaskUsage[];
}

export interface RawEconomicsResponse {
  agent?: string;
  period?: string;
  total_cost_usd?: number;
  requests?: number;
  input_tokens?: number;
  output_tokens?: number;
  ttft_p50_ms?: number;
  ttft_p95_ms?: number;
  tpot_p50_ms?: number;
  tpot_p95_ms?: number;
  avg_latency_ms?: number;
  p95_latency_ms?: number;
  tool_calls?: number;
  tool_hallucinations?: number;
  tool_hallucination_rate?: number;
  retry_waste_usd?: number;
  cache_hits?: number;
  cache_hit_rate?: number;
  cache_saved_usd?: number;
  by_model?: Record<string, unknown>;
}

export interface RawNotification {
  name: string;
  type: string;
  url: string;
  events: string[];
}

export interface RawProfile {
  id: string;
  type: string;
  display_name?: string;
  email?: string;
  avatar_url?: string;
  bio?: string;
  status?: string;
  settings?: Record<string, unknown>;
  created_at?: string;
  updated_at?: string;
}

interface OkResponse {
  ok?: boolean;
  [key: string]: unknown;
}

// ── API client ──

export const api = {
  profiles: {
    list: (type?: 'operator' | 'agent') => {
      const params = type ? `?type=${type}` : '';
      return req<RawProfile[]>(`/admin/profiles${params}`);
    },
    show: (id: string) => req<RawProfile>(`/admin/profiles/${encodeURIComponent(id)}`),
    update: (id: string, data: Partial<Omit<RawProfile, 'id'>>) =>
      req<RawProfile>(`/admin/profiles/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify(data) }),
    delete: (id: string) => req<OkResponse>(`/admin/profiles/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  },

  agents: {
    list: () => req<RawAgent[]>('/agents'),
    show: (name: string) => req<RawAgent>(`/agents/${name}`),
    create: (name: string, preset: string, mode: string) =>
      req<OkResponse>('/agents', { method: 'POST', body: JSON.stringify({ name, preset, mode }) }),
    delete: (name: string) => req<OkResponse>(`/agents/${name}`, { method: 'DELETE' }),
    start: (name: string) => req<OkResponse>(`/agents/${name}/start`, { method: 'POST', body: '{}' }),
    runtimeStatus: (name: string) => req<RawAgentRuntimeStatus>(`/agents/${encodeURIComponent(name)}/runtime/status`),
    startStream: async (name: string, onProgress: (event: RawAgentStartProgress) => void) => {
      await ensureConfig();
      const headers: Record<string, string> = {
        Accept: 'application/x-ndjson',
        'Content-Type': 'application/json',
        ...(TOKEN ? { Authorization: `Bearer ${TOKEN}` } : {}),
      };
      const res = await fetch(`${BASE}/agents/${encodeURIComponent(name)}/start`, {
        method: 'POST',
        headers,
        body: '{}',
      });
      if (!res.ok) {
        const text = await res.text();
        throw new Error(extractErrorDetail(text, res.status, `/agents/${name}/start`));
      }

      let complete = false;
      const parseLine = (line: string) => {
        const trimmed = line.trim();
        if (!trimmed) return;
        const event = JSON.parse(trimmed) as RawAgentStartProgress;
        if (!event.type) return;
        onProgress(event);
        if (event.type === 'error') throw new Error(event.error || 'Agent startup failed');
        if (event.type === 'complete') complete = true;
      };

      if (!res.body?.getReader) {
        const text = await res.text();
        for (const line of text.split('\n')) {
          parseLine(line);
          if (complete) return;
        }
        throw new Error('Agent startup stream ended before completion');
      }

      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buffer = '';
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split('\n');
        buffer = lines.pop() || '';
        for (const line of lines) {
          parseLine(line);
          if (complete) {
            reader.cancel().catch(() => {});
            return;
          }
        }
      }
      buffer += decoder.decode();
      parseLine(buffer);
      if (!complete) throw new Error('Agent startup stream ended before completion');
    },
    stop: (name: string) => req<OkResponse>(`/agents/${name}/stop`, { method: 'POST', body: '{}' }),
    halt: (name: string, tier = 'supervised', reason = '') =>
      req<OkResponse>(`/agents/${name}/halt`, { method: 'POST', body: JSON.stringify({ tier, reason }) }),
    resume: (name: string) => req<OkResponse>(`/agents/${name}/resume`, { method: 'POST', body: '{}' }),
    restart: (name: string) => req<OkResponse>(`/agents/${name}/restart`, { method: 'POST', body: '{}' }),
    grant: (name: string, capability: string) =>
      req<OkResponse>(`/agents/${name}/grant`, { method: 'POST', body: JSON.stringify({ capability }) }),
    revoke: (name: string, capability: string) =>
      req<OkResponse>(`/agents/${name}/revoke`, { method: 'POST', body: JSON.stringify({ capability }) }),
    logs: (name: string, since?: string, until?: string) => {
      const params = new URLSearchParams();
      if (since) params.set('since', since);
      if (until) params.set('until', until);
      return req<RawAuditEntry[]>(`/agents/${name}/logs?${params}`);
    },
    channels: (name: string) => req<RawChannel[]>(`/agents/${name}/channels`),
    ensureDM: (name: string) => req<{ status: string; channel: string }>(`/agents/${name}/dm`, { method: 'POST', body: '{}' }),
    knowledge: (name: string) => req<{ nodes?: unknown[] } | unknown[]>(`/agents/${name}/knowledge`),
    results: (name: string) => req<Array<Record<string, unknown>>>(`/agents/${name}/results`),
    resultUrl: (name: string, taskId: string) =>
      `${BASE}/agents/${name}/results/${taskId}`,
    resultMetadata: (name: string, taskId: string) =>
      req<Record<string, unknown>>(`/agents/${name}/results/${taskId}/metadata`),
    resultDownloadUrl: (name: string, taskId: string) =>
      `${BASE}/agents/${name}/results/${taskId}?download=true`,
    budget: (name: string) => req<RawBudgetResponse>(`/agents/${name}/budget`),
    economics: (name: string) => req<RawEconomicsResponse>(`/agents/${name}/economics`),
    economicsSummary: () => req<RawEconomicsResponse>('/agents/economics/summary'),
    clearCache: (name: string) => req<Record<string, unknown>>(`/agents/${name}/cache`, { method: 'DELETE' }),
    procedures: async (name: string, params?: { mission?: string; outcome?: string }) => {
      const qs = new URLSearchParams();
      if (params?.mission) qs.set('mission', params.mission);
      if (params?.outcome) qs.set('outcome', params.outcome);
      const q = qs.toString();
      const raw = await req<any>(`/agents/${name}/procedures${q ? `?${q}` : ''}`);
      return { procedures: raw.procedures ?? raw.results ?? [] };
    },
    episodes: async (name: string, params?: { mission?: string; from?: string; to?: string; outcome?: string; tag?: string }) => {
      const qs = new URLSearchParams();
      if (params?.mission) qs.set('mission', params.mission);
      if (params?.from) qs.set('from', params.from);
      if (params?.to) qs.set('to', params.to);
      if (params?.outcome) qs.set('outcome', params.outcome);
      if (params?.tag) qs.set('tag', params.tag);
      const q = qs.toString();
      const raw = await req<any>(`/agents/${name}/episodes${q ? `?${q}` : ''}`);
      return { episodes: raw.episodes ?? raw.results ?? [], total: raw.total ?? (raw.results?.length ?? 0) };
    },
    trajectory: (name: string) =>
      req<import('../types').TrajectoryState>(`/agents/${name}/trajectory`),
  },

  teams: {
    list: () => req<RawTeam[]>('/admin/teams'),
    show: (name: string) => req<RawTeam>(`/admin/teams/${name}`),
    create: (name: string, agents: string[]) =>
      req<OkResponse>('/admin/teams', { method: 'POST', body: JSON.stringify({ name, agents }) }),
    delete: (name: string) => req<OkResponse>(`/admin/teams/${name}`, { method: 'DELETE' }),
    activity: (name: string) => req<RawAuditEntry[]>(`/admin/teams/${name}/activity`),
  },

  channels: {
    list: (opts?: { includeArchived?: boolean; includeUnavailable?: boolean }) => {
      const params = new URLSearchParams();
      if (opts?.includeArchived) params.set('include_archived', 'true');
      if (opts?.includeUnavailable) params.set('include_unavailable', 'true');
      const query = params.toString();
      return req<RawChannel[]>(`/comms/channels${query ? `?${query}` : ''}`);
    },
    read: (name: string, limit = 50) => req<RawMessage[]>(`/comms/channels/${name}/messages?limit=${limit}&reader=operator`),
    send: (name: string, content: string, replyTo?: string, flags?: Record<string, boolean>) =>
      req<RawMessage>(`/comms/channels/${name}/messages`, {
        method: 'POST',
        body: JSON.stringify({ author: 'operator', content, reply_to: replyTo, flags }),
      }),
    create: (name: string, topic?: string) =>
      req<OkResponse>('/comms/channels', { method: 'POST', body: JSON.stringify({ name, topic }) }),
    archive: (name: string) => req<OkResponse>(`/comms/channels/${name}/archive`, { method: 'POST', body: '{}' }),
    search: (query: string, channel?: string) => {
      const params = new URLSearchParams({ q: query, participant: 'operator' });
      if (channel) params.set('channel', channel);
      return req<RawMessage[]>(`/comms/channels/search?${params}`);
    },
    edit: (channel: string, messageId: string, content: string) =>
      req<OkResponse>(`/comms/channels/${channel}/messages/${messageId}`, {
        method: 'PUT',
        body: JSON.stringify({ content, author: 'operator' }),
      }),
    delete: (channel: string, messageId: string) =>
      req<OkResponse>(`/comms/channels/${channel}/messages/${messageId}`, { method: 'DELETE' }),
    react: (channel: string, messageId: string, emoji: string) =>
      req<OkResponse>(`/comms/channels/${channel}/messages/${messageId}/reactions`, {
        method: 'POST',
        body: JSON.stringify({ emoji, author: 'operator' }),
      }),
    unreact: (channel: string, messageId: string, emoji: string) =>
      req<OkResponse>(`/comms/channels/${channel}/messages/${messageId}/reactions/${encodeURIComponent(emoji)}?author=operator`, {
        method: 'DELETE',
      }),
    unreads: () => req<Record<string, { unread: number; mentions: number }>>('/comms/unreads'),
    markRead: (channel: string) =>
      req<OkResponse>(`/comms/channels/${channel}/mark-read`, { method: 'POST', body: '{}' }),
  },

  infra: {
    status: async (): Promise<RawInfraStatus> => {
      const data = await req<RawInfraStatus | RawInfraService[]>('/infra/status');
      if (Array.isArray(data)) return { components: data };
      return data as RawInfraStatus;
    },
    up: () => req<OkResponse>('/infra/up', { method: 'POST', body: '{}' }),
    down: () => req<OkResponse>('/infra/down', { method: 'POST', body: '{}' }),
    rebuild: (component: string) =>
      req<OkResponse>(`/infra/rebuild/${component}`, { method: 'POST', body: '{}' }),
    reload: () => req<OkResponse>('/infra/reload', { method: 'POST', body: '{}' }),
    capacity: () => req<RawInfraCapacity>('/infra/capacity'),
    logs: (component: string, tail = 200) =>
      req<{ component: string; tail: number; logs: string }>(`/infra/services/${encodeURIComponent(component)}/logs?tail=${tail}`),
  },

  packages: {
    list: (kind?: string) => {
      const params = new URLSearchParams();
      if (kind) params.set('kind', kind);
      const suffix = params.size ? `?${params.toString()}` : '';
      return req<RawPackageListResponse>(`/packages${suffix}`);
    },
    show: (kind: string, name: string) =>
      req<RawInstalledPackage>(`/packages/${encodeURIComponent(kind)}/${encodeURIComponent(name)}`),
  },

  instances: {
    list: () => req<RawInstanceListResponse>('/instances'),
    show: (id: string) => req<RawInstance>(`/instances/${encodeURIComponent(id)}`),
    createFromPackage: (body: {
      kind: string;
      name: string;
      instance_name?: string;
      node_id?: string;
      config?: Record<string, unknown>;
      node_config?: Record<string, unknown>;
    }) =>
      req<RawInstance>('/instances/from-package', { method: 'POST', body: JSON.stringify(body) }),
    validate: (id: string) =>
      req<{ status: string }>(`/instances/${encodeURIComponent(id)}/validate`, {
        method: 'POST',
        body: '{}',
      }),
    apply: (id: string) =>
      req<RawInstanceApplyResult>(`/instances/${encodeURIComponent(id)}/apply`, {
        method: 'POST',
        body: '{}',
      }),
  },

  hub: {
    search: (query: string, kind?: string) => {
      const params = new URLSearchParams({ q: query });
      if (kind) params.set('kind', kind);
      return req<RawHubComponent[]>(`/hub/search?${params}`);
    },
    list: () => req<RawHubComponent[]>('/hub/instances'),
    install: (name: string, kind: string, source?: string) =>
      req<OkResponse>('/hub/install', { method: 'POST', body: JSON.stringify({ name, kind, source }) }),
    remove: (name: string, _kind?: string) =>
      req<OkResponse>(`/hub/${encodeURIComponent(name)}`, { method: 'DELETE' }),
    update: () => req<OkResponse>('/hub/update', { method: 'POST', body: '{}' }),
    upgrade: (components?: string[]) => {
      const body = components ? { components } : {};
      return req<{ files?: unknown[]; components?: unknown[]; warnings?: string[] }>('/hub/upgrade', {
        method: 'POST', body: JSON.stringify(body),
      });
    },
    outdated: () => req<unknown[]>('/hub/outdated'),
    info: (name: string, kind?: string) => {
      const params = kind ? `?kind=${kind}` : '';
      return req<RawHubComponent>(`/hub/${encodeURIComponent(name)}/info${params}`);
    },
  },

  deploy: {
    deploy: (pack: string) =>
      req<OkResponse>('/hub/deploy', { method: 'POST', body: JSON.stringify({ pack_name: pack }) }),
    teardown: (pack: string, del = false) =>
      req<OkResponse>(`/hub/teardown/${encodeURIComponent(pack)}`, { method: 'POST', body: JSON.stringify({ delete: del }) }),
  },

  missions: {
    list: () => req<RawMission[]>('/missions'),
    show: (name: string) => req<RawMission>(`/missions/${name}`),
    create: (yaml: string) =>
      req<RawMission>('/missions', {
        method: 'POST',
        body: yaml,
        headers: { 'Content-Type': 'application/x-yaml' },
      }),
    update: (name: string, yaml: string) =>
      req<RawMission>(`/missions/${name}`, {
        method: 'PUT',
        body: yaml,
        headers: { 'Content-Type': 'application/x-yaml' },
      }),
    delete: (name: string) => req<OkResponse>(`/missions/${name}`, { method: 'DELETE' }),
    assign: (name: string, target: string, type = 'agent') =>
      req<OkResponse>(`/missions/${name}/assign`, {
        method: 'POST',
        body: JSON.stringify({ target, type }),
      }),
    pause: (name: string, reason?: string) =>
      req<OkResponse>(`/missions/${name}/pause`, {
        method: 'POST',
        body: JSON.stringify({ reason }),
      }),
    resume: (name: string) =>
      req<OkResponse>(`/missions/${name}/resume`, { method: 'POST', body: '{}' }),
    complete: (name: string) =>
      req<OkResponse>(`/missions/${name}/complete`, { method: 'POST', body: '{}' }),
    history: (name: string) => req<Record<string, unknown>[]>(`/missions/${name}/history`),
    health: (name?: string) => {
      if (name) return req<MissionHealthResponse>(`/missions/${name}/health`);
      return req<{ missions: MissionHealthResponse[] }>('/missions/health');
    },
    procedures: async (name: string, params?: { agent?: string }) => {
      const q = params?.agent ? `?agent=${params.agent}` : '';
      const raw = await req<any>(`/missions/${name}/procedures${q}`);
      return { procedures: raw.procedures ?? raw.results ?? [] };
    },
    episodes: async (name: string) => {
      const raw = await req<any>(`/missions/${name}/episodes`);
      return { episodes: raw.episodes ?? raw.results ?? [], total: raw.total ?? (raw.results?.length ?? 0) };
    },
    evaluations: async (name: string, params?: { limit?: number }) => {
      const q = params?.limit ? `?limit=${params.limit}` : '';
      const raw = await req<any>(`/missions/${name}/evaluations${q}`);
      return { mission: raw.mission ?? name, evaluations: raw.evaluations ?? raw.results ?? [], summary: raw.summary ?? {} };
    },
    canvas: async (name: string) => {
      await ensureConfig();
      const resp = await authenticatedFetch(`${BASE}/missions/${encodeURIComponent(name)}/canvas`);
      if (resp.status === 404) return null;
      if (!resp.ok) throw new Error(`GET ${BASE}/missions/${name}/canvas: ${resp.status}`);
      return resp.json();
    },
    saveCanvas: async (name: string, doc: unknown) => {
      await ensureConfig();
      const resp = await authenticatedFetch(`${BASE}/missions/${encodeURIComponent(name)}/canvas`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(doc),
      });
      if (!resp.ok) throw new Error(`PUT ${BASE}/missions/${name}/canvas: ${resp.status}`);
      return resp.json();
    },
  },

  connectors: {
    list: () => req<RawConnector[]>('/hub/instances?kind=connector'),
    activate: (name: string) => req<OkResponse>(`/hub/${name}/activate`, { method: 'POST', body: '{}' }),
    deactivate: (name: string) => req<OkResponse>(`/hub/${name}/deactivate`, { method: 'POST', body: '{}' }),
    status: (name: string) => req<RawConnector>(`/hub/${name}`),
    requirements: (name: string) =>
      req<{
        connector: string;
        version?: string;
        ready: boolean;
        credentials: unknown[];
        auth?: unknown;
        egress_domains?: Array<string | { domain: string; allowed: boolean }>;
      }>(`/hub/connectors/${name}/requirements`),
    configure: (name: string, credentials: Record<string, string>) =>
      req<{ configured: string[]; auth_configured: boolean; egress_domains_added: string[]; ready: boolean }>(`/hub/connectors/${name}/configure`, {
        method: 'POST',
        body: JSON.stringify({ credentials }),
      }),
  },

  intake: {
    items: (connector?: string) => {
      const params = connector ? `?connector=${connector}` : '';
      return req<RawWorkItem[]>(`/events/intake/items${params}`);
    },
    stats: () => req<Record<string, unknown>>('/events/intake/stats'),
    pollHealth: () => req<Record<string, unknown>>('/hub/intake/poll-health'),
    triggerPoll: (connector: string) =>
      req<Record<string, unknown>>(`/hub/intake/poll/${encodeURIComponent(connector)}`, { method: 'POST', body: '{}' }),
  },

  notifications: {
    list: () => req<RawNotification[]>('/events/notifications'),
    show: (name: string) => req<RawNotification>(`/events/notifications/${name}`),
    add: (name: string, url: string, type?: string, events?: string[]) =>
      req<RawNotification>('/events/notifications', {
        method: 'POST',
        body: JSON.stringify({ name, url, ...(type ? { type } : {}), ...(events ? { events } : {}) }),
      }),
    remove: (name: string) => req<OkResponse>(`/events/notifications/${name}`, { method: 'DELETE' }),
    test: (name: string) => req<{ event_id: string; status: string }>(`/events/notifications/${name}/test`, { method: 'POST', body: '{}' }),
  },

  events: {
    list: (opts?: { limit?: number; source_type?: string; source_name?: string; event_type?: string }) => {
      const params = new URLSearchParams();
      if (opts?.limit) params.set('limit', String(opts.limit));
      if (opts?.source_type) params.set('source_type', opts.source_type);
      if (opts?.source_name) params.set('source_name', opts.source_name);
      if (opts?.event_type) params.set('event_type', opts.event_type);
      return req<RawEvent[]>(`/events?${params}`);
    },
    show: (id: string) => req<RawEvent>(`/events/${id}`),
    subscriptions: () => req<Record<string, unknown>[]>('/events/subscriptions'),
  },

  meeseeks: {
    list: (parent?: string) => {
      const params = parent ? `?parent=${encodeURIComponent(parent)}` : '';
      return req<RawMeeseeks[]>(`/agents/meeseeks${params}`);
    },
    show: (id: string) => req<RawMeeseeks>(`/agents/meeseeks/${id}`),
    kill: (id: string) => req<OkResponse>(`/agents/meeseeks/${id}`, { method: 'DELETE' }),
    killByParent: (parent: string) =>
      req<{ status: string; killed: string[] }>(`/agents/meeseeks?parent=${encodeURIComponent(parent)}`, { method: 'DELETE' }),
  },

  webhooks: {
    list: () => req<RawWebhook[]>('/events/webhooks'),
    show: (name: string) => req<RawWebhook>(`/events/webhooks/${name}`),
    create: (name: string, eventType: string) =>
      req<RawWebhook>('/events/webhooks', { method: 'POST', body: JSON.stringify({ name, event_type: eventType }) }),
    delete: (name: string) => req<OkResponse>(`/events/webhooks/${name}`, { method: 'DELETE' }),
    rotateSecret: (name: string) =>
      req<RawWebhook>(`/events/webhooks/${name}/rotate-secret`, { method: 'POST', body: '{}' }),
  },

  knowledge: {
    query: (text: string) => req<Record<string, unknown>>('/graph/query', { method: 'POST', body: JSON.stringify({ query: text }) }),
    whoKnows: (topic: string) => req<Record<string, unknown>>(`/graph/who-knows?topic=${encodeURIComponent(topic)}`),
    stats: () => req<RawKnowledgeStats>('/graph/stats'),
    export: (format = 'json') => req<Record<string, unknown>[]>(`/graph/export?format=${format}`),
    ingest: (input: { content?: string; filename?: string; content_type?: string; scope?: unknown }) =>
      req<Record<string, unknown>>('/graph/ingest', { method: 'POST', body: JSON.stringify(input) }),
    neighbors: (nodeId: string) => req<Record<string, unknown>>(`/graph/neighbors?node_id=${encodeURIComponent(nodeId)}`),
    context: (subject: string) => req<Record<string, unknown>>(`/graph/context?subject=${encodeURIComponent(subject)}`),
    ontologyCandidates: () =>
      req<{ candidates: Array<{ id: string; value: string; count?: number; source?: string; status?: string; candidate_type?: string }> }>('/graph/ontology/candidates'),
    ontologyPromote: (nodeId: string, value: string) =>
      req<{ promoted: string; value: string }>('/graph/ontology/promote', { method: 'POST', body: JSON.stringify({ node_id: nodeId, value }) }),
    ontologyReject: (nodeId: string, value: string) =>
      req<{ rejected: string; value: string }>('/graph/ontology/reject', { method: 'POST', body: JSON.stringify({ node_id: nodeId, value }) }),
    ontologyRestore: (nodeId: string, value = '') =>
      req<Record<string, unknown>>('/graph/ontology/restore', { method: 'POST', body: JSON.stringify({ node_id: nodeId, value }) }),
    curationLog: () => req<unknown>('/graph/curation-log'),
    pending: () => req<unknown>('/graph/pending'),
    review: (id: string, action: 'approve' | 'reject', reason = '') =>
      req<Record<string, unknown>>(`/graph/review/${encodeURIComponent(id)}`, { method: 'POST', body: JSON.stringify({ action, reason }) }),
    memoryProposals: (status = 'needs_review', limit = 100) =>
      req<unknown>(`/graph/memory/proposals?status=${encodeURIComponent(status)}&limit=${limit}`),
    reviewMemoryProposal: (id: string, action: 'approve' | 'reject', reason = '') =>
      req<Record<string, unknown>>(`/graph/memory/proposals/${encodeURIComponent(id)}/review`, { method: 'POST', body: JSON.stringify({ action, reason }) }),
    memories: (opts: { type?: string; agent?: string; limit?: number } = {}) => {
      const params = new URLSearchParams();
      if (opts.type) params.set('type', opts.type);
      if (opts.agent) params.set('agent', opts.agent);
      params.set('limit', String(opts.limit ?? 100));
      return req<unknown>(`/graph/memory?${params.toString()}`);
    },
    memoryAction: (id: string, action: 'revoke', reason = '') =>
      req<Record<string, unknown>>(`/graph/memory/${encodeURIComponent(id)}/actions`, { method: 'POST', body: JSON.stringify({ action, reason }) }),
    quarantineList: (agent?: string) =>
      req<unknown>(`/graph/quarantine${agent ? `?agent=${encodeURIComponent(agent)}` : ''}`),
    quarantineRelease: (opts: { node_id?: string; agent?: string }) =>
      req<Record<string, unknown>>('/graph/quarantine/release', { method: 'POST', body: JSON.stringify(opts) }),
    classification: () => req<unknown>('/graph/classification'),
    principals: (type?: string) =>
      req<unknown>(`/graph/principals${type ? `?type=${encodeURIComponent(type)}` : ''}`),
    communities: () => req<unknown>('/graph/communities'),
    hubs: (limit?: number) => req<unknown>(`/graph/hubs${limit ? `?limit=${limit}` : ''}`),
  },

  capabilities: {
    list: () => req<RawCapability[]>('/admin/capabilities'),
    show: (name: string) => req<RawCapability>(`/admin/capabilities/${encodeURIComponent(name)}`),
    enable: (name: string, key?: string, agents?: string[]) =>
      req<OkResponse>(`/admin/capabilities/${encodeURIComponent(name)}/enable`, { method: 'POST', body: JSON.stringify({ key, agents }) }),
    disable: (name: string) => req<OkResponse>(`/admin/capabilities/${encodeURIComponent(name)}/disable`, { method: 'POST', body: '{}' }),
    add: (name: string, kind: string) =>
      req<OkResponse>('/admin/capabilities', { method: 'POST', body: JSON.stringify({ name, kind }) }),
    delete: (name: string) => req<OkResponse>(`/admin/capabilities/${encodeURIComponent(name)}`, { method: 'DELETE' }),
  },

  credentials: {
    list: (filters?: Record<string, string>) => {
      const params = filters ? '?' + new URLSearchParams(filters).toString() : '';
      return req<{ name: string; value: string; metadata?: Record<string, unknown> }[]>(`/creds${params}`);
    },
    store: (name: string, value: string, opts?: { kind?: string; scope?: string; protocol?: string; service?: string }) =>
      req<OkResponse>('/creds', { method: 'POST', body: JSON.stringify({ name, value, ...opts }) }),
    test: (name: string) =>
      req<{ ok: boolean; status?: number; message?: string; latency_ms?: number }>(`/creds/${encodeURIComponent(name)}/test`, { method: 'POST', body: '{}' }),
    delete: (name: string) =>
      req<OkResponse>(`/creds/${encodeURIComponent(name)}`, { method: 'DELETE' }),
  },

  providers: {
    list: () => req<import('../types').Provider[]>('/infra/providers'),
    routingConfig: () => req<RawRoutingConfig>('/infra/routing/config'),
    tools: () => req<RawProviderToolInventory>('/infra/provider-tools'),
    install: (name: string) =>
      req<OkResponse>(`/infra/providers/${encodeURIComponent(name)}/install`, { method: 'POST', body: '{}' }),
  },

  setup: {
    config: () => req<import('../types').SetupConfig>('/infra/setup/config'),
  },

  init: (opts: { operator: string; force?: boolean; anthropic_api_key?: string; openai_api_key?: string }) =>
    req<{ status: string; home: string }>('/init', { method: 'POST', body: JSON.stringify(opts) }),

  routing: {
    config: () => req<{ configured: boolean; version: string; [key: string]: unknown }>('/infra/routing/config'),
    suggestions: (status?: string) =>
      req<RawRoutingSuggestion[]>(`/infra/routing/suggestions${status ? `?status=${encodeURIComponent(status)}` : ''}`),
    approveSuggestion: (id: string) =>
      req<RawRoutingSuggestion>(`/infra/routing/suggestions/${encodeURIComponent(id)}/approve`, { method: 'POST', body: '{}' }),
    rejectSuggestion: (id: string) =>
      req<OkResponse>(`/infra/routing/suggestions/${encodeURIComponent(id)}/reject`, { method: 'POST', body: '{}' }),
    stats: (taskType?: string) =>
      req<RawRoutingStat[]>(`/infra/routing/stats${taskType ? `?task_type=${encodeURIComponent(taskType)}` : ''}`),
  },

  policy: {
    show: (agent: string) => req<RawPolicyValidation>(`/admin/policy/${agent}`),
    validate: (agent: string) => req<RawPolicyValidation>(`/admin/policy/${agent}/validate`, { method: 'POST', body: '{}' }),
  },

  presets: {
    list: () => req<{ name: string; description: string; type: string; source: string }[]>('/hub/presets'),
    show: (name: string) => req<Record<string, unknown>>(`/hub/presets/${encodeURIComponent(name)}`),
    create: (data: Record<string, unknown>) =>
      req<OkResponse>('/hub/presets', { method: 'POST', body: JSON.stringify(data) }),
    update: (name: string, data: Record<string, unknown>) =>
      req<OkResponse>(`/hub/presets/${encodeURIComponent(name)}`, { method: 'PUT', body: JSON.stringify(data) }),
    delete: (name: string) => req<OkResponse>(`/hub/presets/${encodeURIComponent(name)}`, { method: 'DELETE' }),
  },

  agentConfig: {
    get: (name: string) => req<Record<string, unknown>>(`/agents/${name}/config`),
    update: (name: string, data: Record<string, unknown>) =>
      req<Record<string, unknown>>(`/agents/${name}/config`, { method: 'PUT', body: JSON.stringify(data) }),
  },

  admin: {
    doctor: (options?: RequestInit) => req<RawDoctorResult>('/admin/doctor', options),
    pruneImages: () => req<RawPruneImagesResult>('/admin/prune-images', { method: 'POST', body: '{}' }),
    trust: (action: string, agent?: string, level?: string) =>
      req<OkResponse>('/admin/trust', { method: 'POST', body: JSON.stringify({ action, args: { agent, level } }) }),
    audit: (agent: string, params?: { since?: string; until?: string }) => {
      const query = new URLSearchParams();
      if (agent && agent !== '_all') query.set('agent', agent);
      if (params?.since) query.set('since', params.since);
      if (params?.until) query.set('until', params.until);
      const suffix = query.toString();
      return req<RawAuditEntry[]>(`/admin/audit${suffix ? `?${suffix}` : ''}`);
    },
    egress: (agent?: string) => req<RawEgress>(`/admin/egress${agent ? `?agent=${encodeURIComponent(agent)}` : ''}`),
    approveEgressDomain: (agent: string, domain: string, reason?: string) =>
      req<RawEgress>(`/admin/egress/${encodeURIComponent(agent)}/domains`, { method: 'POST', body: JSON.stringify({ domain, reason }) }),
    revokeEgressDomain: (agent: string, domain: string) =>
      req<RawEgress>(`/admin/egress/${encodeURIComponent(agent)}/domains/${encodeURIComponent(domain)}`, { method: 'DELETE' }),
    updateEgressMode: (agent: string, mode: 'denylist' | 'allowlist' | 'supervised-strict' | 'supervised-permissive') =>
      req<RawEgress>(`/admin/egress/${encodeURIComponent(agent)}/mode`, { method: 'PUT', body: JSON.stringify({ mode }) }),
    destroy: () => req<OkResponse>('/admin/destroy', { method: 'POST', body: '{}' }),
    department: (action: string, name?: string) =>
      req<OkResponse>('/admin/department', { method: 'POST', body: JSON.stringify({ action, name }) }),
    knowledge: (action: string) =>
      req<OkResponse>('/admin/graph', { method: 'POST', body: JSON.stringify({ action }) }),
    model: (action: string, name?: string) =>
      req<OkResponse>('/admin/model', { method: 'POST', body: JSON.stringify({ action, name }) }),
    egressDomains: async () => {
      const data = await req<{ domains: Array<{ domain: string; sources: Array<{ type: string; name: string; added_at?: string }>; auto_managed: boolean }> } | Array<unknown>>('/hub/egress/domains');
      return Array.isArray(data) ? data : (data as any).domains || [];
    },
    egressDomainProvenance: (domain: string) =>
      req<{ domain: string; sources: Array<{ type: string; name: string; added_at?: string }>; auto_managed: boolean; active_dependents?: string[] }>(`/hub/egress/domains/${encodeURIComponent(domain)}/provenance`),
    auditSummarize: () =>
      req<{ metrics: unknown[]; count: number }>('/admin/audit/summarize', { method: 'POST', body: '{}' }),
  },
};
