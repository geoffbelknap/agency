import { expect, type Page } from '@playwright/test';

type RouteController = {
  assertNoUnhandledRequests: () => void;
};

const agents = [
  {
    name: 'alice',
    status: 'running',
    mode: 'autonomous',
    type: 'agent',
    preset: 'platform-expert',
    team: 'alpha',
    enforcer: 'active',
    role: 'release lead',
    last_active: '2026-04-08T20:30:00Z',
    trust_level: 4,
    restrictions: ['filesystem.write'],
    mission: 'prepare weekly release notes',
    mission_status: 'active',
    build_id: 'build-test-1',
  },
  {
    name: 'bob',
    status: 'halted',
    mode: 'assisted',
    type: 'agent',
    preset: 'researcher',
    team: 'alpha',
    enforcer: 'paused',
    role: 'research support',
    last_active: '2026-04-08T18:00:00Z',
    trust_level: 2,
    restrictions: ['network.egress'],
    mission: 'triage escalations',
    mission_status: 'paused',
    build_id: 'build-test-1',
  },
];

const channels = [
  { name: 'general', topic: 'Company-wide coordination', unread: 1, mentions: 0, members: ['alice', 'bob', 'operator'] },
  { name: 'dm-alice', topic: 'Direct messages with alice', unread: 0, mentions: 0, members: ['alice', 'operator'] },
];

const channelMessages: Record<string, unknown[]> = {
  general: [
    { id: 'm1', author: 'alice', content: 'Hello from Alice', timestamp: '2026-04-08T19:55:00Z' },
    { id: 'm2', author: 'operator', content: 'Status looks good.', timestamp: '2026-04-08T20:00:00Z' },
  ],
  'dm-alice': [
    { id: 'dm1', author: 'alice', content: 'Release draft is ready for review.', timestamp: '2026-04-08T20:05:00Z' },
  ],
};

const infraStatus = {
  version: '0.1.0',
  build_id: 'build-test-1',
  components: [
    { name: 'gateway', state: 'running', health: 'healthy', uptime: '2h' },
    { name: 'redis', state: 'running', health: 'healthy', uptime: '2h' },
    { name: 'knowledge', state: 'running', health: 'healthy', uptime: '90m' },
  ],
};

const infraCapacity = {
  host_memory_mb: 32768,
  host_cpu_cores: 8,
  system_reserve_mb: 4096,
  infra_overhead_mb: 2048,
  runtime_backend: 'docker',
  enforcement_mode: '',
  max_agents: 6,
  max_concurrent_meesks: 2,
  agent_slot_mb: 4096,
  meeseeks_slot_mb: 2048,
  network_pool_configured: true,
  running_agents: 2,
  running_meeseeks: 1,
  available_slots: 3,
};

const agentLogs: Record<string, unknown[]> = {
  _all: [
    {
      timestamp: '2026-04-08T19:40:00Z',
      event: 'LLM_DIRECT',
      detail: 'Produced release summary',
      model: 'provider-a-standard',
      duration_ms: 1800,
      input_tokens: 800,
      output_tokens: 220,
      cost: 0.0034,
    },
  ],
  alice: [
    {
      timestamp: '2026-04-08T19:40:00Z',
      event: 'LLM_DIRECT',
      detail: 'Produced release summary',
      model: 'provider-a-standard',
      duration_ms: 1800,
      input_tokens: 800,
      output_tokens: 220,
      cost: 0.0034,
    },
    {
      timestamp: '2026-04-08T19:42:00Z',
      event: 'TOOL_CALL',
      detail: 'Opened changelog',
      tool: 'browser.open',
      args: { path: 'CHANGELOG.md' },
    },
  ],
  bob: [
    {
      timestamp: '2026-04-08T18:10:00Z',
      event: 'DOMAIN_BLOCKED',
      detail: 'Blocked outbound request',
      domain: 'example.com',
      reason: 'egress restricted',
    },
  ],
};

const missions = [
  {
    name: 'release-train',
    description: 'Prepare weekly release notes and rollout summary.',
    status: 'active',
    assigned_to: 'alice',
    assigned_type: 'agent',
    cost_mode: 'balanced',
    version: 3,
    has_canvas: true,
    triggers: [{ source: 'channel', channel: 'general', event_type: 'message' }],
    budget: { daily: 12, monthly: 120, per_task: 3 },
    health: { indicators: ['freshness', 'review'], business_hours: '09:00-17:00' },
    requires: { capabilities: ['browser.open'], channels: ['general'] },
    instructions: 'Monitor release work and publish summaries.',
  },
  {
    name: 'nightly-sync',
    description: 'Collect overnight signals and summarize exceptions.',
    status: 'paused',
    assigned_to: 'bob',
    assigned_type: 'agent',
    cost_mode: 'frugal',
    version: 1,
    triggers: [{ source: 'schedule', cron: '0 7 * * *' }],
    budget: { daily: 3, monthly: 20, per_task: 1 },
    instructions: 'Summarize exceptions after nightly runs.',
  },
];

const missionHealth = {
  missions: [
    { mission: 'release-train', status: 'healthy', checks: [], summary: 'All checks passing' },
    { mission: 'nightly-sync', status: 'degraded', checks: [], summary: 'Paused awaiting review' },
  ],
};

const knowledgeExport = [
  {
    type: 'node',
    label: 'Release notes',
    kind: 'document',
    source_type: 'agent',
    updated_at: '2026-04-08T18:00:00Z',
    contributed_by: 'alice',
  },
  {
    type: 'node',
    label: 'Escalation playbook',
    kind: 'procedure',
    source_type: 'rule',
    updated_at: '2026-04-08T17:00:00Z',
    contributed_by: 'operator',
  },
  {
    type: 'edge',
    source: 'Release notes',
    target: 'Escalation playbook',
    relation: 'references',
  },
];

const profiles = [
  {
    id: 'operator',
    type: 'operator',
    display_name: 'Trent',
    email: 'trent@example.com',
    status: 'active',
    bio: 'Primary operator profile',
  },
  {
    id: 'alice',
    type: 'agent',
    display_name: 'Alice',
    email: 'alice@agency.local',
    status: 'available',
    bio: 'Release automation specialist',
  },
];

const teams = [
  { name: 'alpha', member_count: 2, created: '2026-04-01T10:00:00Z' },
];

const hubInstalled = [
  { name: 'agency-slack', component: 'agency-slack', kind: 'connector', source: 'hub://agency/slack', installed_at: '2026-04-08T10:00:00Z' },
  { name: 'platform-expert', component: 'platform-expert', kind: 'preset', source: 'hub://agency/presets', installed_at: '2026-04-08T10:00:00Z' },
];

const hubSearch = [
  { name: 'agency-slack', kind: 'connector', description: 'Slack intake connector', source: 'hub://agency/slack' },
  { name: 'platform-expert', kind: 'preset', description: 'Broad platform operations preset', source: 'hub://agency/presets' },
];

const v2Packages = [
  {
    kind: 'connector',
    name: 'slack-interactivity',
    version: '1.0.0',
    trust: 'official',
    path: '/tmp/.agency/packages/connector/slack-interactivity.json',
    assurance: ['publisher_verified', 'ask_partial'],
    assurance_issuer: 'hub:official:agency',
    assurance_statements: [
      {
        statement_type: 'ask_reviewed',
        result: 'ASK-Partial',
        reviewer_type: 'automated',
        issuer_hub_id: 'hub:official:agency',
      },
    ],
  },
  {
    kind: 'connector',
    name: 'google-drive-admin',
    version: '1.0.0',
    trust: 'official',
    path: '/tmp/.agency/packages/connector/google-drive-admin.json',
    assurance: ['publisher_verified', 'ask_partial'],
    assurance_issuer: 'hub:official:agency',
    assurance_statements: [
      {
        statement_type: 'ask_reviewed',
        result: 'ASK-Partial',
        reviewer_type: 'automated',
        issuer_hub_id: 'hub:official:agency',
      },
    ],
  },
];

const v2InstanceSeed = [
  {
    id: 'inst_slack',
    name: 'slack-community-admin',
    source: { package: { kind: 'connector', name: 'slack-interactivity', version: '1.0.0' } },
    nodes: [
      { id: 'slack_ingress', kind: 'connector.ingress' },
      { id: 'slack_authority', kind: 'connector.authority' },
    ],
    grants: [
      { principal: 'agent:alice', action: 'consent_open_approval_card' },
    ],
    created_at: '2026-04-08T10:00:00Z',
    updated_at: '2026-04-08T10:10:00Z',
  },
  {
    id: 'inst_drive',
    name: 'drive-admin',
    source: { package: { kind: 'connector', name: 'google-drive-admin', version: '1.0.0' } },
    nodes: [{ id: 'drive_admin', kind: 'connector.authority' }],
    grants: [
      { principal: 'agent:alice', action: 'drive_list_file_permissions' },
      { principal: 'agent:alice', action: 'drive_share_file' },
    ],
    created_at: '2026-04-08T10:20:00Z',
    updated_at: '2026-04-08T10:25:00Z',
  },
];

const connectors = [
  { id: 'slack-intake', name: 'slack-intake', kind: 'connector', source: 'hub://agency/slack', state: 'active', version: '1.0.0' },
];

const workItems = [
  {
    id: 'wi-1',
    connector: 'slack-intake',
    status: 'routed',
    summary: 'Customer escalation from #ops',
    created_at: '2026-04-08T19:20:00Z',
    payload: { channel: '#ops', severity: 'high' },
  },
];

const capabilities = [
  { name: 'browser.open', kind: 'tool', state: 'enabled', agents: ['alice'], description: 'Open local or remote resources' },
  { name: 'shell.exec', kind: 'service', state: 'restricted', agents: ['alice'], description: 'Run approved shell commands' },
];

const notifications = [
  { name: 'agency-trent', type: 'ntfy', url: 'https://ntfy.sh/agency-trent', events: ['operator_alert'] },
];

const events = [
  {
    id: 'evt-1',
    source_type: 'channel',
    source_name: 'general',
    event_type: 'message.created',
    timestamp: '2026-04-08T20:00:00Z',
    data: { message: 'Release draft posted' },
  },
];

const webhooks = [
  {
    name: 'release-events',
    event_type: 'mission.updated',
    url: 'https://hooks.example.com/release-events',
    secret: 'whsec_test',
    created_at: '2026-04-08T10:00:00Z',
  },
];

const presets = [
  { name: 'platform-expert', description: 'Default platform operator preset', type: 'standard', source: 'built-in' },
  { name: 'release-assistant', description: 'Helps compile release notes', type: 'standard', source: 'user' },
];

const presetDetail = {
  name: 'release-assistant',
  description: 'Helps compile release notes',
  type: 'standard',
  source: 'user',
  tools: ['browser.open'],
  capabilities: ['browser.open'],
  identity: { purpose: 'Release support', body: 'Draft and refine weekly release notes.' },
};

const usageMetrics = {
  period: { since: '2026-04-07T00:00:00Z', until: '2026-04-08T23:59:59Z' },
  totals: {
    requests: 24,
    input_tokens: 42000,
    output_tokens: 11000,
    total_tokens: 53000,
    est_cost_usd: 0.42,
    errors: 1,
    avg_latency_ms: 980,
    p95_latency_ms: 1600,
  },
  by_agent: {
    alice: { requests: 18, input_tokens: 32000, output_tokens: 8000, total_tokens: 40000, est_cost_usd: 0.31, errors: 0, avg_latency_ms: 910 },
    bob: { requests: 6, input_tokens: 10000, output_tokens: 3000, total_tokens: 13000, est_cost_usd: 0.11, errors: 1, avg_latency_ms: 1200 },
  },
  by_model: {
    'provider-a-standard': { requests: 24, input_tokens: 42000, output_tokens: 11000, total_tokens: 53000, est_cost_usd: 0.42, errors: 1, avg_latency_ms: 980 },
  },
  by_provider: {
    'provider-a': { requests: 24, input_tokens: 42000, output_tokens: 11000, total_tokens: 53000, est_cost_usd: 0.42, errors: 1, avg_latency_ms: 980 },
  },
  by_source: {
    missions: { requests: 10, input_tokens: 21000, output_tokens: 6000, total_tokens: 27000, est_cost_usd: 0.23, errors: 0, avg_latency_ms: 900 },
    channels: { requests: 14, input_tokens: 21000, output_tokens: 5000, total_tokens: 26000, est_cost_usd: 0.19, errors: 1, avg_latency_ms: 1030 },
  },
  recent_errors: [
    { ts: '2026-04-08T18:15:00Z', agent: 'bob', model: 'provider-a-standard', status: 429, error: 'Rate limited' },
  ],
};

const doctorChecks = {
  checks: [
    { name: 'gateway', agent: 'alice', status: 'pass', detail: 'Gateway healthy' },
    { name: 'policy', agent: 'bob', status: 'warn', detail: 'Restricted due to egress policy' },
  ],
};

const egressDomains = {
  domains: [
    {
      domain: 'provider-a.example.com',
      sources: [{ type: 'provider', name: 'provider-a', added_at: '2026-04-08T09:00:00Z' }],
      auto_managed: true,
    },
  ],
};

const egressDomainDetail = {
  domain: 'provider-a.example.com',
  sources: [{ type: 'provider', name: 'provider-a', added_at: '2026-04-08T09:00:00Z' }],
  auto_managed: true,
  active_dependents: ['alice'],
};

const initialOntologyCandidates = [
  { id: 'candidate-rollout-readiness', value: 'rollout-readiness', count: 4, source: 'agent', status: 'candidate' },
  { id: 'candidate-policy-drift', value: 'policy-drift', count: 2, source: 'operator', status: 'candidate' },
];

const initialOntologyDecisions: Array<{
  id: string;
  action: string;
  node_id: string;
  value: string;
  timestamp: string;
}> = [];

const policyData = {
  valid: true,
  violations: [],
  effective: {
    egress: ['provider-a.example.com'],
    filesystem: ['workspace-read'],
  },
};

const routingSuggestions = [
  {
    id: 'route-suggestion-1',
    task_type: 'summarization',
    current_model: 'provider-a-standard',
    suggested_model: 'provider-b-fast',
    reason: 'provider-b-fast costs 42.0% less than provider-a-standard for summarization tasks with 96% success rate',
    savings_percent: 0.42,
    savings_usd_per_1k: 0.018,
    status: 'pending',
    created_at: '2026-04-08T20:00:00Z',
  },
];

const routingStats = [
  {
    model: 'provider-a-standard',
    task_type: 'summarization',
    total_calls: 32,
    retries: 1,
    success_rate: 0.97,
    avg_latency_ms: 1100,
    avg_input_tokens: 900,
    avg_output_tokens: 220,
    total_cost_usd: 0.44,
    cost_per_1k: 0.021,
  },
  {
    model: 'provider-b-fast',
    task_type: 'summarization',
    total_calls: 28,
    retries: 0,
    success_rate: 0.96,
    avg_latency_ms: 620,
    avg_input_tokens: 880,
    avg_output_tokens: 210,
    total_cost_usd: 0.18,
    cost_per_1k: 0.012,
  },
];

function json(body: unknown, status = 200) {
  return {
    status,
    contentType: 'application/json',
    body: JSON.stringify(body),
  };
}

export async function installAgencyMocks(page: Page): Promise<RouteController> {
  const unhandled: string[] = [];
  let ontologyCandidates = initialOntologyCandidates.map((candidate) => ({ ...candidate }));
  let ontologyDecisions = initialOntologyDecisions.map((entry) => ({ ...entry }));
  let localInstances = v2InstanceSeed.map((instance) => JSON.parse(JSON.stringify(instance)));

  await page.route('**/__agency/config', async (route) => {
    await route.fulfill(json({ token: '', gateway: '', via: 'local', authenticated: true }));
  });

  await page.route('**/auth/signout', async (route) => {
    await route.fulfill(json({ ok: true }));
  });

  await page.route('**/api/v1/**', async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const { pathname, searchParams } = url;
    const method = request.method();

    if (method === 'GET' && pathname === '/api/v1/infra/routing/config') {
      await route.fulfill(json({ configured: true, version: 'test-build' }));
      return;
    }
    if (method === 'GET' && pathname === '/api/v1/infra/status') {
      await route.fulfill(json(infraStatus));
      return;
    }
    if (method === 'GET' && pathname === '/api/v1/infra/capacity') {
      await route.fulfill(json(infraCapacity));
      return;
    }
    if (method === 'GET' && pathname === '/api/v1/packages') {
      await route.fulfill(json({ packages: v2Packages }));
      return;
    }
    if (method === 'GET' && pathname === '/api/v1/instances') {
      await route.fulfill(json({ instances: localInstances }));
      return;
    }
    if (method === 'POST' && pathname === '/api/v1/instances/from-package') {
      const body = JSON.parse(request.postData() || '{}') as Record<string, unknown>;
      const sourceKind = String(body.kind || 'connector');
      const sourceName = String(body.name || 'package');
      const instanceName = String(body.instance_name || `${sourceName}-instance`);
      const created = {
        id: `inst-${instanceName}`,
        name: instanceName,
        source: { package: { kind: sourceKind, name: sourceName, version: '1.0.0' } },
        nodes: [{ id: sourceName.replaceAll('-', '_'), kind: 'connector.authority' }],
        grants: [],
        created_at: '2026-04-08T20:30:00Z',
        updated_at: '2026-04-08T20:30:00Z',
      };
      localInstances = [created, ...localInstances];
      await route.fulfill(json(created, 201));
      return;
    }
    if (method === 'GET' && pathname.startsWith('/api/v1/instances/')) {
      const parts = pathname.split('/');
      if (parts.length === 5) {
        const id = decodeURIComponent(parts[4] || '');
        const instance = localInstances.find((item) => item.id === id);
        if (instance) {
          await route.fulfill(json(instance));
          return;
        }
      }
    }
    if (method === 'POST' && pathname.endsWith('/validate') && pathname.startsWith('/api/v1/instances/')) {
      await route.fulfill(json({ status: 'valid' }));
      return;
    }
    if (method === 'POST' && pathname.endsWith('/apply') && pathname.startsWith('/api/v1/instances/')) {
      const id = decodeURIComponent(pathname.split('/')[4] || '');
      const instance = localInstances.find((item) => item.id === id);
      await route.fulfill(json({
        status: 'applied',
        instance,
        nodes: (instance?.nodes || []).map((node: any) => ({ node_id: node.id, state: 'running' })),
      }));
      return;
    }
    if (method === 'POST' && ['/api/v1/infra/up', '/api/v1/infra/down', '/api/v1/infra/reload'].includes(pathname)) {
      await route.fulfill(json({ ok: true }));
      return;
    }
    if (method === 'POST' && pathname.startsWith('/api/v1/infra/rebuild/')) {
      await route.fulfill(json({ ok: true }));
      return;
    }

    if (method === 'GET' && pathname === '/api/v1/agents') {
      await route.fulfill(json(agents));
      return;
    }
    if (method === 'GET' && pathname === '/api/v1/agents/economics/summary') {
      await route.fulfill(json({ period: '2026-04-09', total_cost_usd: 0.13, requests: 3, by_agent: {}, by_model: {} }));
      return;
    }
    if (method === 'GET' && pathname.startsWith('/api/v1/agents/') && !pathname.endsWith('/logs') && !pathname.endsWith('/budget') && !pathname.endsWith('/channels') && !pathname.endsWith('/knowledge') && !pathname.endsWith('/economics') && !pathname.endsWith('/results') && !pathname.endsWith('/procedures') && !pathname.endsWith('/episodes') && !pathname.endsWith('/trajectory') && !pathname.endsWith('/config')) {
      const name = decodeURIComponent(pathname.split('/')[4] || '');
      await route.fulfill(json(agents.find((agent) => agent.name === name) ?? agents[0]));
      return;
    }
    if (method === 'GET' && pathname.endsWith('/logs')) {
      const name = decodeURIComponent(pathname.split('/')[4] || '');
      await route.fulfill(json(agentLogs[name] ?? []));
      return;
    }
    if (method === 'GET' && pathname.endsWith('/budget')) {
      const name = decodeURIComponent(pathname.split('/')[4] || '');
      await route.fulfill(json({
        agent_name: name,
        daily_used: 2.4,
        daily_limit: 10,
        daily_remaining: 7.6,
        monthly_used: 18,
        monthly_limit: 200,
        monthly_remaining: 182,
        per_task_limit: 2,
        today_llm_calls: 12,
        today_input_tokens: 14000,
        today_output_tokens: 3200,
        task_usage: [
          { task_id: 'task-1', cost_usd: 0.13, input_tokens: 4000, output_tokens: 800, llm_calls: 3 },
        ],
      }));
      return;
    }
    if (method === 'GET' && pathname.endsWith('/economics') && pathname.startsWith('/api/v1/agents/')) {
      const name = decodeURIComponent(pathname.split('/')[4] || '');
      await route.fulfill(json({
        agent: name,
        period: '2026-04-09',
        total_cost_usd: 0.13,
        requests: 3,
        input_tokens: 4000,
        output_tokens: 800,
        retry_waste_usd: 0,
        tool_hallucination_rate: 0,
        cache_hits: 1,
        cache_hit_rate: 0.25,
        by_model: {},
      }));
      return;
    }
    if (method === 'DELETE' && pathname.endsWith('/cache') && pathname.startsWith('/api/v1/agents/')) {
      await route.fulfill(json({ deleted: 1 }));
      return;
    }
    if (method === 'GET' && pathname.endsWith('/channels') && pathname.startsWith('/api/v1/agents/')) {
      await route.fulfill(json(channels));
      return;
    }
    if (method === 'GET' && pathname.endsWith('/knowledge') && pathname.startsWith('/api/v1/agents/')) {
      await route.fulfill(json({ nodes: knowledgeExport.filter((entry) => (entry as { type?: string }).type === 'node') }));
      return;
    }
    if (method === 'GET' && pathname.endsWith('/results')) {
      await route.fulfill(json([]));
      return;
    }
    if (method === 'GET' && pathname.endsWith('/procedures')) {
      await route.fulfill(json({ procedures: [{ id: 'proc-1', title: 'Review release notes', outcome: 'success' }] }));
      return;
    }
    if (method === 'GET' && pathname.endsWith('/episodes')) {
      await route.fulfill(json({ episodes: [{ id: 'ep-1', summary: 'Resolved rollout issue', outcome: 'success' }], total: 1 }));
      return;
    }
    if (method === 'GET' && pathname.endsWith('/trajectory')) {
      await route.fulfill(json({ current_phase: 'analysis', checkpoints: [] }));
      return;
    }
    if (method === 'GET' && pathname.endsWith('/config')) {
      await route.fulfill(json({ identity: 'Release coordination specialist' }));
      return;
    }
    if (method === 'PUT' && pathname.endsWith('/config')) {
      await route.fulfill(json({ identity: 'Release coordination specialist' }));
      return;
    }
    if (method === 'POST' && /\/api\/v1\/agents\/[^/]+\/(start|stop|halt|resume|restart|grant|revoke)$/.test(pathname)) {
      await route.fulfill(json({ ok: true }));
      return;
    }

    if (method === 'GET' && pathname === '/api/v1/comms/channels') {
      await route.fulfill(json(channels));
      return;
    }
    if (method === 'GET' && pathname.startsWith('/api/v1/comms/channels/') && pathname.endsWith('/messages')) {
      const channelName = decodeURIComponent(pathname.split('/')[5] || '');
      await route.fulfill(json(channelMessages[channelName] ?? []));
      return;
    }
    if (method === 'POST' && pathname.startsWith('/api/v1/comms/channels/') && pathname.endsWith('/messages')) {
      await route.fulfill(json({ ok: true }));
      return;
    }
    if (method === 'GET' && pathname === '/api/v1/comms/unreads') {
      await route.fulfill(json({ general: { unread: 1, mentions: 0 } }));
      return;
    }
    if (method === 'POST' && pathname.endsWith('/mark-read')) {
      await route.fulfill(json({ ok: true }));
      return;
    }

    if (method === 'GET' && pathname === '/api/v1/missions') {
      await route.fulfill(json(missions));
      return;
    }
    if (method === 'GET' && pathname === '/api/v1/missions/health') {
      await route.fulfill(json(missionHealth));
      return;
    }
    if (method === 'GET' && pathname.startsWith('/api/v1/missions/') && pathname.split('/').length === 5) {
      const name = decodeURIComponent(pathname.split('/')[4] || '');
      await route.fulfill(json(missions.find((mission) => mission.name === name) ?? missions[0]));
      return;
    }
    if (method === 'GET' && pathname.endsWith('/history')) {
      await route.fulfill(json([{ changed_at: '2026-04-08T19:00:00Z', version: 3, change: 'Updated instructions' }]));
      return;
    }
    if (method === 'GET' && pathname.endsWith('/health') && pathname.startsWith('/api/v1/missions/')) {
      await route.fulfill(json({ mission: 'release-train', status: 'healthy', checks: [], summary: 'All checks passing' }));
      return;
    }
    if (method === 'GET' && (pathname.endsWith('/procedures') || pathname.endsWith('/episodes') || pathname.endsWith('/evaluations'))) {
      await route.fulfill(json({ procedures: [], episodes: [], evaluations: [], summary: {}, total: 0 }));
      return;
    }
    if (method === 'GET' && pathname.endsWith('/canvas')) {
      await route.fulfill(json({ nodes: [], edges: [] }));
      return;
    }
    if (['POST', 'PUT', 'DELETE'].includes(method) && pathname.startsWith('/api/v1/missions/')) {
      await route.fulfill(json({ ok: true }));
      return;
    }

    if (method === 'GET' && pathname === '/api/v1/graph/export') {
      await route.fulfill(json(knowledgeExport));
      return;
    }
    if (method === 'GET' && pathname === '/api/v1/graph/stats') {
      await route.fulfill(json({ node_count: 2, edge_count: 1, source_count: 2, avg_confidence: 0.92 }));
      return;
    }
    if (method === 'GET' && pathname === '/api/v1/graph/neighbors') {
      await route.fulfill(json({
        neighbors: [
          { label: 'Deployment plan', kind: 'document', relation: 'references' },
        ],
      }));
      return;
    }
    if (method === 'GET' && pathname === '/api/v1/graph/context') {
      await route.fulfill(json({ summary: 'Release notes connect deployment planning and weekly rollout context.' }));
      return;
    }
    if (method === 'POST' && pathname === '/api/v1/graph/query') {
      await route.fulfill(json({
        results: [
          {
            label: 'Release notes',
            kind: 'document',
            summary: 'Weekly rollout notes and highlights',
            source_type: 'agent',
            updated_at: '2026-04-08T18:00:00Z',
            connections: 1,
          },
        ],
      }));
      return;
    }
    if (method === 'GET' && pathname === '/api/v1/graph/who-knows') {
      await route.fulfill(json({ agents: [{ name: 'alice', confidence: 0.94, topics: ['release notes', 'rollouts'] }] }));
      return;
    }
    if (method === 'GET' && pathname === '/api/v1/graph/ontology/candidates') {
      await route.fulfill(json({ candidates: ontologyCandidates }));
      return;
    }
    if (method === 'GET' && pathname === '/api/v1/graph/curation-log') {
      await route.fulfill(json({ entries: ontologyDecisions }));
      return;
    }
    if (method === 'GET' && pathname === '/api/v1/graph/pending') {
      await route.fulfill(json({ pending: [] }));
      return;
    }
    if (method === 'POST' && pathname.startsWith('/api/v1/graph/review/')) {
      await route.fulfill(json({ ok: true }));
      return;
    }
    if (method === 'GET' && pathname === '/api/v1/graph/memory/proposals') {
      await route.fulfill(json({ items: [] }));
      return;
    }
    if (method === 'GET' && pathname === '/api/v1/graph/memory') {
      await route.fulfill(json({ items: [] }));
      return;
    }
    if (method === 'POST' && /^\/api\/v1\/graph\/memory\/[^/]+\/actions$/.test(pathname)) {
      await route.fulfill(json({ ok: true }));
      return;
    }
    if (method === 'POST' && /^\/api\/v1\/graph\/memory\/proposals\/[^/]+\/review$/.test(pathname)) {
      await route.fulfill(json({ ok: true }));
      return;
    }
    if (method === 'GET' && pathname === '/api/v1/graph/quarantine') {
      await route.fulfill(json({ nodes: [] }));
      return;
    }
    if (method === 'POST' && pathname === '/api/v1/graph/quarantine/release') {
      await route.fulfill(json({ ok: true }));
      return;
    }
    if (method === 'GET' && pathname === '/api/v1/graph/classification') {
      await route.fulfill(json({ tiers: [] }));
      return;
    }
    if (method === 'GET' && pathname === '/api/v1/graph/principals') {
      await route.fulfill(json([]));
      return;
    }
    if (method === 'GET' && pathname === '/api/v1/graph/communities') {
      await route.fulfill(json({ communities: [] }));
      return;
    }
    if (method === 'GET' && pathname === '/api/v1/graph/hubs') {
      await route.fulfill(json({ hubs: [] }));
      return;
    }
    if (method === 'POST' && /^\/api\/v1\/graph\/ontology\/(promote|reject|restore)$/.test(pathname)) {
      const bodyText = request.postData() || '{}';
      const body = JSON.parse(bodyText) as { node_id?: string; value?: string };
      const action = pathname.split('/').pop() || 'unknown';
      const nodeId = body.node_id || body.value || `candidate-${Date.now()}`;
      const value = body.value || nodeId;

      if (action === 'restore') {
        if (!ontologyCandidates.some((candidate) => candidate.id === nodeId)) {
          ontologyCandidates = [
            { id: nodeId, value, count: 1, source: 'curation', status: 'candidate' },
            ...ontologyCandidates,
          ];
        }
      } else {
        ontologyCandidates = ontologyCandidates.filter((candidate) => candidate.id !== nodeId);
      }

      ontologyDecisions = [
        {
          id: `curation-${action}-${nodeId}-${ontologyDecisions.length + 1}`,
          action: `ontology_${action}`,
          node_id: nodeId,
          value,
          timestamp: `2026-04-09T15:${String(10 + ontologyDecisions.length).padStart(2, '0')}:00Z`,
        },
        ...ontologyDecisions,
      ];

      await route.fulfill(json({ ok: true, node_id: nodeId, value }));
      return;
    }

    if (method === 'GET' && pathname === '/api/v1/admin/profiles') {
      const type = searchParams.get('type');
      await route.fulfill(json(type ? profiles.filter((profile) => profile.type === type) : profiles));
      return;
    }
    if (method === 'GET' && pathname.startsWith('/api/v1/admin/profiles/')) {
      const id = decodeURIComponent(pathname.split('/')[5] || '');
      await route.fulfill(json(profiles.find((profile) => profile.id === id) ?? profiles[0]));
      return;
    }
    if (['PUT', 'DELETE'].includes(method) && pathname.startsWith('/api/v1/admin/profiles/')) {
      await route.fulfill(json({ ok: true }));
      return;
    }

    if (method === 'GET' && pathname === '/api/v1/admin/teams') {
      await route.fulfill(json(teams));
      return;
    }
    if (method === 'GET' && pathname.startsWith('/api/v1/admin/teams/') && pathname.endsWith('/activity')) {
      await route.fulfill(json([{ timestamp: '2026-04-08T18:30:00Z', event: 'mission.assigned', detail: 'Assigned release-train to alice' }]));
      return;
    }
    if (method === 'GET' && pathname.startsWith('/api/v1/admin/teams/')) {
      await route.fulfill(json({ name: 'alpha', members: ['alice', 'bob'] }));
      return;
    }
    if (method === 'POST' && pathname === '/api/v1/admin/teams') {
      await route.fulfill(json({ ok: true }));
      return;
    }

    if (method === 'GET' && pathname === '/api/v1/hub/instances') {
      if (searchParams.get('kind') === 'connector') {
        await route.fulfill(json(connectors));
      } else {
        await route.fulfill(json(hubInstalled));
      }
      return;
    }
    if (method === 'GET' && pathname === '/api/v1/hub/search') {
      await route.fulfill(json(hubSearch));
      return;
    }
    if (method === 'GET' && pathname.startsWith('/api/v1/hub/info/')) {
      const name = decodeURIComponent(pathname.split('/')[5] || '');
      await route.fulfill(json({ name, kind: 'connector', description: 'Detailed hub info', source: 'hub://agency/test' }));
      return;
    }
    if (method === 'POST' && pathname === '/api/v1/hub/update') {
      await route.fulfill(json({ available: [{ name: 'agency-slack' }] }));
      return;
    }
    if (method === 'POST' && pathname === '/api/v1/hub/upgrade') {
      await route.fulfill(json({ components: [{ name: 'agency-slack', status: 'upgraded' }] }));
      return;
    }
    if (method === 'GET' && pathname === '/api/v1/hub/outdated') {
      await route.fulfill(json([]));
      return;
    }
    if (method === 'GET' && pathname === '/api/v1/hub/presets') {
      await route.fulfill(json(presets));
      return;
    }
    if (method === 'GET' && pathname.startsWith('/api/v1/hub/presets/')) {
      await route.fulfill(json(presetDetail));
      return;
    }
    if ((method === 'POST' && pathname === '/api/v1/hub/install') || (method === 'DELETE' && pathname.startsWith('/api/v1/hub/'))) {
      await route.fulfill(json({ ok: true }));
      return;
    }
    if (['POST', 'PUT', 'DELETE'].includes(method) && pathname.startsWith('/api/v1/hub/presets')) {
      await route.fulfill(json({ ok: true }));
      return;
    }

    if (method === 'POST' && pathname === '/api/v1/hub/deploy') {
      await route.fulfill(json({ ok: true, pack: 'agency-slack' }));
      return;
    }
    if (method === 'POST' && pathname.startsWith('/api/v1/hub/teardown/')) {
      await route.fulfill(json({ ok: true }));
      return;
    }

    if (method === 'GET' && pathname.startsWith('/api/v1/hub/connectors/') && pathname.endsWith('/requirements')) {
      await route.fulfill(json({
        connector: 'slack-intake',
        ready: true,
        credentials: [],
        egress_domains: ['slack.com'],
      }));
      return;
    }
    if (method === 'POST' && pathname.startsWith('/api/v1/hub/connectors/') && pathname.endsWith('/configure')) {
      await route.fulfill(json({ configured: [], auth_configured: true, egress_domains_added: [], ready: true }));
      return;
    }
    if (method === 'POST' && pathname.startsWith('/api/v1/hub/') && (pathname.endsWith('/activate') || pathname.endsWith('/deactivate'))) {
      await route.fulfill(json({ ok: true }));
      return;
    }
    if (method === 'GET' && pathname.startsWith('/api/v1/hub/') && pathname.split('/').length === 5) {
      await route.fulfill(json(connectors[0]));
      return;
    }

    if (method === 'GET' && pathname === '/api/v1/events/intake/items') {
      await route.fulfill(json(workItems));
      return;
    }
    if (method === 'GET' && pathname === '/api/v1/events/intake/stats') {
      await route.fulfill(json({ total: 1, routed: 1 }));
      return;
    }
    if (method === 'GET' && pathname === '/api/v1/hub/intake/poll-health') {
      await route.fulfill(json({ connectors: {} }));
      return;
    }
    if (method === 'POST' && pathname.startsWith('/api/v1/hub/intake/poll/')) {
      await route.fulfill(json({ ok: true }));
      return;
    }

    if (method === 'GET' && pathname === '/api/v1/admin/capabilities') {
      await route.fulfill(json(capabilities));
      return;
    }
    if (['POST', 'DELETE'].includes(method) && pathname.startsWith('/api/v1/admin/capabilities')) {
      await route.fulfill(json({ ok: true }));
      return;
    }

    if (method === 'GET' && pathname === '/api/v1/events/notifications') {
      await route.fulfill(json(notifications));
      return;
    }
    if (method === 'GET' && pathname.startsWith('/api/v1/events/notifications/')) {
      await route.fulfill(json(notifications[0]));
      return;
    }
    if (method === 'POST' && pathname.endsWith('/test')) {
      await route.fulfill(json({ event_id: 'evt-test', status: 'queued' }));
      return;
    }
    if ((method === 'POST' && pathname === '/api/v1/events/notifications') || (method === 'DELETE' && pathname.startsWith('/api/v1/events/notifications/'))) {
      await route.fulfill(json({ ok: true }));
      return;
    }

    if (method === 'GET' && pathname === '/api/v1/events') {
      await route.fulfill(json(events));
      return;
    }
    if (method === 'GET' && pathname === '/api/v1/events/subscriptions') {
      await route.fulfill(json([{ topic: 'mission.updated', destination: 'agency-trent' }]));
      return;
    }

    if (method === 'GET' && pathname === '/api/v1/events/webhooks') {
      await route.fulfill(json(webhooks));
      return;
    }
    if (method === 'POST' && pathname === '/api/v1/events/webhooks') {
      await route.fulfill(json(webhooks[0]));
      return;
    }
    if (method === 'POST' && pathname.endsWith('/rotate-secret')) {
      await route.fulfill(json({ ...webhooks[0], secret: 'whsec_rotated' }));
      return;
    }
    if (method === 'DELETE' && pathname.startsWith('/api/v1/events/webhooks/')) {
      await route.fulfill(json({ ok: true }));
      return;
    }

    if (method === 'GET' && pathname === '/api/v1/admin/doctor') {
      await route.fulfill(json(doctorChecks));
      return;
    }
    if (method === 'POST' && pathname === '/api/v1/admin/trust') {
      await route.fulfill(json({ ok: true }));
      return;
    }
    if (method === 'GET' && pathname.startsWith('/api/v1/admin/policy/')) {
      await route.fulfill(json(policyData));
      return;
    }
    if (method === 'POST' && pathname.endsWith('/validate') && pathname.startsWith('/api/v1/admin/policy/')) {
      await route.fulfill(json(policyData));
      return;
    }
    if (method === 'POST' && pathname === '/api/v1/admin/destroy') {
      await route.fulfill(json({ ok: true }));
      return;
    }

    if (method === 'GET' && pathname === '/api/v1/hub/egress/domains') {
      await route.fulfill(json(egressDomains));
      return;
    }
    if (method === 'GET' && pathname.startsWith('/api/v1/hub/egress/domains/')) {
      await route.fulfill(json(egressDomainDetail));
      return;
    }
    if (method === 'GET' && pathname === '/api/v1/admin/egress') {
      await route.fulfill(json({ allowed_domains: ['provider-a.example.com', 'slack.com'] }));
      return;
    }
    if (method === 'GET' && pathname === '/api/v1/admin/audit') {
      const agent = url.searchParams.get('agent') || '_all';
      await route.fulfill(json(agentLogs[agent] ?? []));
      return;
    }
    if (method === 'POST' && pathname === '/api/v1/admin/audit/summarize') {
      await route.fulfill(json({ metrics: [{ event: 'LLM_DIRECT', count: 1 }], count: 1 }));
      return;
    }

    if (method === 'GET' && pathname === '/api/v1/infra/providers') {
      await route.fulfill(json([
        { id: 'provider-a', name: 'Provider A', configured: true, validated: true },
        { id: 'provider-b', name: 'Provider B', configured: false, validated: false },
      ]));
      return;
    }
    if (method === 'GET' && pathname === '/api/v1/infra/setup/config') {
      await route.fulfill(json({
        providers: {
          'provider-a': { configured: true, validated: true },
          'provider-b': { configured: false, validated: false },
        },
      }));
      return;
    }
    if (method === 'POST' && pathname === '/api/v1/init') {
      await route.fulfill(json({ status: 'ok', home: '/tmp/agency-home' }));
      return;
    }

    if (method === 'GET' && pathname === '/api/v1/infra/routing/metrics') {
      await route.fulfill(json(usageMetrics));
      return;
    }
    if (method === 'GET' && pathname === '/api/v1/infra/routing/suggestions') {
      await route.fulfill(json(routingSuggestions));
      return;
    }
    if (method === 'GET' && pathname === '/api/v1/infra/routing/stats') {
      await route.fulfill(json(routingStats));
      return;
    }
    if (method === 'POST' && /^\/api\/v1\/infra\/routing\/suggestions\/[^/]+\/approve$/.test(pathname)) {
      await route.fulfill(json({ ...routingSuggestions[0], status: 'approved' }));
      return;
    }
    if (method === 'POST' && /^\/api\/v1\/infra\/routing\/suggestions\/[^/]+\/reject$/.test(pathname)) {
      await route.fulfill(json({ id: 'route-suggestion-1', status: 'rejected' }));
      return;
    }

    unhandled.push(`${method} ${pathname}${url.search}`);
    await route.fulfill(json({ error: `Unhandled mocked API request: ${method} ${pathname}` }, 500));
  });

  return {
    assertNoUnhandledRequests() {
      expect(unhandled, `Unhandled API requests:\n${unhandled.join('\n')}`).toEqual([]);
    },
  };
}
