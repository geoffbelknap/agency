import { http, HttpResponse } from 'msw';

// Use wildcard prefix so handlers match regardless of the resolved gateway
// origin (localhost:8200 in prod, jsdom origin in tests, etc.)
const BASE = '*/api/v1';

export const handlers = [
  // Agents
  http.get(`${BASE}/agents`, () => HttpResponse.json([])),
  http.get(`${BASE}/agents/:name`, () => HttpResponse.json({ name: 'test', status: 'stopped' })),
  http.post(`${BASE}/agents`, () => HttpResponse.json({ ok: true })),
  http.delete(`${BASE}/agents/:name`, () => HttpResponse.json({ ok: true })),
  http.post(`${BASE}/agents/:name/start`, () => HttpResponse.json({ ok: true })),
  http.post(`${BASE}/agents/:name/stop`, () => HttpResponse.json({ ok: true })),
  http.post(`${BASE}/agents/:name/halt`, () => HttpResponse.json({ ok: true })),
  http.post(`${BASE}/agents/:name/resume`, () => HttpResponse.json({ ok: true })),
  http.post(`${BASE}/agents/:name/grant`, () => HttpResponse.json({ ok: true })),
  http.post(`${BASE}/agents/:name/revoke`, () => HttpResponse.json({ ok: true })),
  http.get(`${BASE}/agents/:name/logs`, () => HttpResponse.json([])),
  http.get(`${BASE}/agents/:name/knowledge`, () => HttpResponse.json({ nodes: [] })),
  http.get(`${BASE}/agents/:name/budget`, () => HttpResponse.json({
    agent_name: 'test', daily_used: 0, daily_limit: 10, daily_remaining: 10,
    monthly_used: 0, monthly_limit: 100, monthly_remaining: 100,
    per_task_limit: 5, today_llm_calls: 0, today_input_tokens: 0, today_output_tokens: 0,
    task_usage: [],
  })),

  // Teams
  http.get(`${BASE}/teams`, () => HttpResponse.json([])),
  http.get(`${BASE}/teams/:name`, () => HttpResponse.json({ name: 'test', members: [] })),
  http.post(`${BASE}/teams`, () => HttpResponse.json({ ok: true })),
  http.get(`${BASE}/teams/:name/activity`, () => HttpResponse.json([])),

  // Channels
  http.get(`${BASE}/channels`, () => HttpResponse.json([])),
  http.get(`${BASE}/channels/:name/messages`, () => HttpResponse.json([])),
  http.post(`${BASE}/channels/:name/messages`, () => HttpResponse.json({ ok: true })),
  http.post(`${BASE}/channels`, () => HttpResponse.json({ ok: true })),
  http.post(`${BASE}/channels/:name/archive`, () => HttpResponse.json({ ok: true })),
  http.get(`${BASE}/channels/search`, () => HttpResponse.json([])),
  http.put(`${BASE}/channels/:name/messages/:id`, () => HttpResponse.json({ ok: true })),
  http.delete(`${BASE}/channels/:name/messages/:id`, () => HttpResponse.json({ ok: true })),
  http.post(`${BASE}/channels/:name/messages/:id/reactions`, () => HttpResponse.json({ ok: true })),
  http.delete(`${BASE}/channels/:name/messages/:id/reactions/:emoji`, () => HttpResponse.json({ ok: true })),
  http.get(`${BASE}/unreads`, () => HttpResponse.json({})),
  http.post(`${BASE}/channels/:name/mark-read`, () => HttpResponse.json({ ok: true })),

  // Infrastructure
  http.get(`${BASE}/infra/status`, () => HttpResponse.json({ version: '0.1.0', build_id: 'test', components: [] })),
  http.post(`${BASE}/infra/up`, () => HttpResponse.json({ ok: true })),
  http.post(`${BASE}/infra/down`, () => HttpResponse.json({ ok: true })),
  http.post(`${BASE}/infra/rebuild/:component`, () => HttpResponse.json({ ok: true })),
  http.post(`${BASE}/infra/reload`, () => HttpResponse.json({ ok: true })),

  // Hub
  http.get(`${BASE}/hub/search`, () => HttpResponse.json([])),
  http.get(`${BASE}/hub/installed`, () => HttpResponse.json([])),
  http.post(`${BASE}/hub/install`, () => HttpResponse.json({ ok: true })),
  http.delete(`${BASE}/hub/:name`, () => HttpResponse.json({ ok: true })),
  http.post(`${BASE}/hub/update`, () => HttpResponse.json({ ok: true })),
  http.get(`${BASE}/hub/info/:name`, () => HttpResponse.json({})),

  // Deploy
  http.post(`${BASE}/deploy`, () => HttpResponse.json({ agents_created: [] })),
  http.post(`${BASE}/teardown/:pack`, () => HttpResponse.json({ ok: true })),

  // Missions
  http.get(`${BASE}/missions`, () => HttpResponse.json([])),
  http.get(`${BASE}/missions/:name`, () => HttpResponse.json({ name: 'test', status: 'unassigned' })),
  http.post(`${BASE}/missions`, () => HttpResponse.json({ name: 'test', status: 'unassigned' })),
  http.put(`${BASE}/missions/:name`, () => HttpResponse.json({ name: 'test', status: 'active' })),
  http.delete(`${BASE}/missions/:name`, () => HttpResponse.json({ ok: true })),
  http.post(`${BASE}/missions/:name/assign`, () => HttpResponse.json({ ok: true })),
  http.post(`${BASE}/missions/:name/pause`, () => HttpResponse.json({ ok: true })),
  http.post(`${BASE}/missions/:name/resume`, () => HttpResponse.json({ ok: true })),
  http.post(`${BASE}/missions/:name/complete`, () => HttpResponse.json({ ok: true })),
  http.get(`${BASE}/missions/:name/history`, () => HttpResponse.json([])),

  // Connectors
  http.get(`${BASE}/connectors`, () => HttpResponse.json([])),
  http.get(`${BASE}/hub/instances`, () => HttpResponse.json([])),
  http.post(`${BASE}/hub/:name/activate`, () => HttpResponse.json({ ok: true })),
  http.post(`${BASE}/hub/:name/deactivate`, () => HttpResponse.json({ ok: true })),
  http.get(`${BASE}/hub/:name`, () => HttpResponse.json({ state: 'active' })),

  // Intake
  http.get(`${BASE}/intake/items`, () => HttpResponse.json([])),
  http.get(`${BASE}/intake/stats`, () => HttpResponse.json({ pending: 0, processing: 0, done: 0, failed: 0 })),

  // Events
  http.get(`${BASE}/events`, () => HttpResponse.json([])),
  http.get(`${BASE}/events/:id`, () => HttpResponse.json({ id: 'evt-1', source_type: 'platform', source_name: 'system', event_type: 'test', timestamp: new Date().toISOString() })),
  http.get(`${BASE}/subscriptions`, () => HttpResponse.json([])),

  // Meeseeks
  http.get(`${BASE}/meeseeks`, () => HttpResponse.json([])),
  http.get(`${BASE}/meeseeks/:id`, () => HttpResponse.json({ id: 'mks-test', status: 'spawned', task: 'test', parent_agent: 'test' })),
  http.delete(`${BASE}/meeseeks/:id`, () => HttpResponse.json({ ok: true })),
  http.delete(`${BASE}/meeseeks`, () => HttpResponse.json({ status: 'ok', killed: [] })),

  // Knowledge
  http.post(`${BASE}/knowledge/query`, () => HttpResponse.json({ results: [] })),
  http.get(`${BASE}/knowledge/who-knows`, () => HttpResponse.json({ agents: [] })),
  http.get(`${BASE}/knowledge/stats`, () => HttpResponse.json({ node_count: 0, edge_count: 0 })),
  http.get(`${BASE}/knowledge/export`, () => HttpResponse.json([])),
  http.get(`${BASE}/knowledge/neighbors`, () => HttpResponse.json({ neighbors: [], edges: [] })),
  http.get(`${BASE}/knowledge/context`, () => HttpResponse.json({})),

  // Capabilities
  http.get(`${BASE}/capabilities`, () => HttpResponse.json([])),
  http.get(`${BASE}/capabilities/:name`, () => HttpResponse.json({})),
  http.post(`${BASE}/capabilities/:name/enable`, () => HttpResponse.json({ ok: true })),
  http.post(`${BASE}/capabilities/:name/disable`, () => HttpResponse.json({ ok: true })),
  http.post(`${BASE}/capabilities`, () => HttpResponse.json({ ok: true })),
  http.delete(`${BASE}/capabilities/:name`, () => HttpResponse.json({ ok: true })),

  // Policy
  http.get(`${BASE}/policy/:agent`, () => HttpResponse.json({ rules: [] })),
  http.post(`${BASE}/policy/:agent/validate`, () => HttpResponse.json({ valid: true })),

  // Presets
  http.get(`${BASE}/presets`, () =>
    HttpResponse.json([
      { name: 'generalist', description: 'Proactive generalist assistant with broad tool access', type: 'standard' },
      { name: 'engineer', description: 'Code development and debugging', type: 'standard' },
      { name: 'researcher', description: 'Information gathering and research', type: 'standard' },
      { name: 'coordinator', description: 'Team lead — task decomposition and delegation', type: 'coordinator' },
    ]),
  ),

  // Webhooks
  http.get(`${BASE}/webhooks`, () => HttpResponse.json([])),
  http.get(`${BASE}/webhooks/:name`, () => HttpResponse.json({ name: 'test-hook', event_type: 'operator_alert', url: 'https://ntfy.sh/test', created_at: new Date().toISOString() })),
  http.post(`${BASE}/webhooks`, () => HttpResponse.json({ name: 'test-hook', event_type: 'operator_alert', url: 'https://example.com/hook', secret: 'sec_abc123' })),
  http.delete(`${BASE}/webhooks/:name`, () => HttpResponse.json({ ok: true })),
  http.post(`${BASE}/webhooks/:name/rotate-secret`, () => HttpResponse.json({ name: 'test-hook', event_type: 'operator_alert', url: 'https://example.com/hook', secret: 'sec_new456' })),

  // Notifications
  http.get(`${BASE}/notifications`, () => HttpResponse.json([])),
  http.get(`${BASE}/notifications/:name`, () => HttpResponse.json({ name: 'alerts', type: 'ntfy', url: 'https://ntfy.sh/agency-alerts', events: ['operator_alert'] })),
  http.post(`${BASE}/notifications`, () => HttpResponse.json({ name: 'alerts', type: 'ntfy', url: 'https://ntfy.sh/agency-alerts', events: ['operator_alert'] })),
  http.delete(`${BASE}/notifications/:name`, () => HttpResponse.json({ status: 'ok', name: 'alerts' })),
  http.post(`${BASE}/notifications/:name/test`, () => HttpResponse.json({ event_id: 'evt-test-1', status: 'sent' })),

  // Agent memory endpoints
  http.get('*/api/v1/agents/:name/procedures', () =>
    HttpResponse.json({
      procedures: [
        {
          task_id: 'task-1',
          agent: 'alice',
          mission_id: 'mission-1',
          mission_name: 'test-mission',
          task_type: 'research',
          timestamp: '2026-03-27T10:00:00Z',
          approach: 'Used web search to gather data',
          tools_used: ['web_search', 'read_file'],
          outcome: 'success',
          duration_minutes: 12,
          lessons: ['Search with specific queries', 'Verify sources'],
        },
      ],
    }),
  ),
  http.get('*/api/v1/agents/:name/episodes', () =>
    HttpResponse.json({
      episodes: [
        {
          task_id: 'task-1',
          agent: 'alice',
          mission_name: 'test-mission',
          timestamp: '2026-03-27T10:00:00Z',
          duration_minutes: 12,
          outcome: 'success',
          summary: 'Completed research task successfully',
          notable_events: ['Found critical bug in API response', 'Escalated to team lead'],
          entities_mentioned: [{ type: 'service', name: 'gateway' }],
          operational_tone: 'notable',
          tags: ['research', 'api'],
        },
      ],
      total: 1,
    }),
  ),
  http.get('*/api/v1/agents/:name/trajectory', () =>
    HttpResponse.json({
      agent: 'alice',
      enabled: true,
      window_size: 50,
      current_entries: 23,
      active_anomalies: [
        {
          detector: 'loop_detector',
          detail: 'Repeated action 5 times in a loop',
          severity: 'warning',
          first_detected: '2026-03-27T09:45:00Z',
        },
      ],
      detectors: {
        loop_detector: { status: 'active', last_fired: '2026-03-27T09:45:00Z' },
        drift_detector: { status: 'idle', last_fired: null },
      },
    }),
  ),
  // Mission memory/evaluation endpoints
  http.get('*/api/v1/missions/:name/procedures', () =>
    HttpResponse.json({ procedures: [] }),
  ),
  http.get('*/api/v1/missions/:name/episodes', () =>
    HttpResponse.json({ episodes: [], total: 0 }),
  ),
  http.get('*/api/v1/missions/:name/evaluations', () =>
    HttpResponse.json({
      mission: 'test-mission',
      evaluations: [
        {
          task_id: 'task-1',
          passed: true,
          evaluation_mode: 'checklist_only',
          model_used: 'default',
          criteria_results: [
            { id: 'c1', passed: true, required: true, reasoning: 'All checks passed' },
          ],
          evaluated_at: '2026-03-27T10:15:00Z',
        },
        {
          task_id: 'task-2',
          passed: false,
          evaluation_mode: 'llm',
          model_used: 'claude-sonnet',
          criteria_results: [
            { id: 'c1', passed: true, required: true, reasoning: 'OK' },
            { id: 'c2', passed: false, required: true, reasoning: 'Missing validation' },
          ],
          evaluated_at: '2026-03-27T11:00:00Z',
        },
      ],
      summary: { total: 2, passed: 1, failed: 1, pass_rate: 0.5 },
    }),
  ),

  // Connector setup
  http.get(`${BASE}/connectors/:name/requirements`, () =>
    HttpResponse.json({
      connector: 'github',
      version: '1.0.0',
      ready: false,
      credentials: [
        { name: 'GITHUB_TOKEN', required: true, description: 'Personal access token' },
      ],
      auth: {},
      egress_domains: ['api.github.com'],
    }),
  ),
  http.post(`${BASE}/connectors/:name/configure`, () =>
    HttpResponse.json({
      configured: ['GITHUB_TOKEN'],
      auth_configured: true,
      egress_domains_added: ['api.github.com'],
      ready: true,
    }),
  ),

  // Ontology
  http.get(`${BASE}/ontology/candidates`, () =>
    HttpResponse.json({
      candidates: [
        { value: 'deployment_pipeline', count: 12, source: 'graph_ingest' },
        { value: 'api_endpoint', count: 8, source: 'graph_ingest' },
      ],
    }),
  ),
  http.post(`${BASE}/ontology/promote`, () =>
    HttpResponse.json({ promoted: true, value: 'deployment_pipeline' }),
  ),
  http.post(`${BASE}/ontology/reject`, () =>
    HttpResponse.json({ rejected: true, value: 'api_endpoint' }),
  ),

  // Egress domains with provenance
  http.get(`${BASE}/egress/domains`, () =>
    HttpResponse.json([
      {
        domain: 'api.github.com',
        sources: [{ type: 'connector', name: 'github', added_at: '2026-03-28T10:00:00Z' }],
        auto_managed: true,
      },
      {
        domain: 'api.openai.com',
        sources: [{ type: 'manual', name: 'operator', added_at: '2026-03-25T08:00:00Z' }],
        auto_managed: false,
      },
    ]),
  ),
  http.get(`${BASE}/egress/domains/:domain/provenance`, () =>
    HttpResponse.json({
      domain: 'api.github.com',
      sources: [{ type: 'connector', name: 'github', added_at: '2026-03-28T10:00:00Z' }],
      auto_managed: true,
      active_dependents: ['github-connector'],
    }),
  ),

  // Audit summarize
  http.post(`${BASE}/audit/summarize`, () =>
    HttpResponse.json({
      metrics: [
        { agent: 'alice', total_tasks: 15, success_rate: 0.87, avg_duration_minutes: 8 },
      ],
      count: 1,
    }),
  ),

  // Admin
  http.get(`${BASE}/admin/doctor`, () => HttpResponse.json({ checks: [] })),
  http.post(`${BASE}/admin/trust`, () => HttpResponse.json({ ok: true })),
  http.get(`${BASE}/admin/egress`, () => HttpResponse.json({ agent: 'test', domains: [] })),
  http.post(`${BASE}/admin/destroy`, () => HttpResponse.json({ ok: true })),
  http.post(`${BASE}/admin/department`, () => HttpResponse.json({ ok: true })),
  http.post(`${BASE}/admin/knowledge/:action`, () => HttpResponse.json({ ok: true })),
  http.post(`${BASE}/admin/model`, () => HttpResponse.json({ ok: true })),
];
