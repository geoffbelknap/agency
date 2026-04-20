import { afterEach, describe, it, expect, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { server } from '../../test/server';
import { renderWithRouter } from '../../test/render';
import { Usage } from './Usage';

const BASE = 'http://localhost:8200/api/v1';
const featureState = vi.hoisted(() => ({ routingOptimizer: false }));

vi.mock('../lib/features', () => ({
  featureEnabled: (id: string) => id === 'routing_optimizer' ? featureState.routingOptimizer : true,
}));

const metrics = {
  period: { since: '2026-04-08T00:00:00Z', until: '2026-04-09T00:00:00Z' },
  totals: {
    requests: 12,
    input_tokens: 12000,
    output_tokens: 4000,
    total_tokens: 16000,
    est_cost_usd: 0.25,
    provider_tool_calls: 2,
    provider_tool_cost_usd: 0.02,
    provider_tool_unpriced_calls: 1,
    errors: 0,
    avg_latency_ms: 900,
  },
  by_agent: {},
  by_model: {},
  by_provider: {},
  by_provider_tool: {
    'provider-web-search': {
      requests: 1,
      input_tokens: 0,
      output_tokens: 0,
      total_tokens: 0,
      est_cost_usd: 0.02,
      provider_tool_calls: 2,
      provider_tool_cost_usd: 0.02,
      provider_tool_unpriced_calls: 1,
      provider_tool_cost_confidence: 'exact,unknown',
      provider_tool_cost_source: 'provider_catalog',
      errors: 0,
      avg_latency_ms: 0,
    },
  },
  by_source: {},
  recent_errors: [],
};

function mockUsageData(suggestions: unknown[] = [], stats: unknown[] = []) {
  server.use(
    http.get('*/__agency/config', () => HttpResponse.json({ gateway: 'http://localhost:8200' })),
    http.get(`${BASE}/infra/routing/metrics`, () => HttpResponse.json(metrics)),
    http.get(`${BASE}/infra/routing/suggestions`, () => HttpResponse.json(suggestions)),
    http.get(`${BASE}/infra/routing/stats`, () => HttpResponse.json(stats)),
  );
}

describe('Usage', () => {
  afterEach(() => {
    featureState.routingOptimizer = false;
  });

  it('renders pending routing suggestions', async () => {
    featureState.routingOptimizer = true;
    mockUsageData([
      {
        id: 'suggestion-1',
        task_type: 'summarization',
        current_model: 'claude-sonnet',
        suggested_model: 'claude-haiku',
        reason: 'claude-haiku costs less for summarization tasks',
        savings_percent: 0.42,
        savings_usd_per_1k: 0.018,
        status: 'pending',
      },
    ]);

    renderWithRouter(<Usage />);

    await screen.findByText('Cost components');
    await userEvent.click(screen.getByRole('button', { name: /optimizer/i }));
    expect(await screen.findByText('Routing Suggestions')).toBeInTheDocument();
    expect(screen.getByText('summarization')).toBeInTheDocument();
    expect(screen.getByText('claude-sonnet')).toBeInTheDocument();
    expect(screen.getByText('claude-haiku')).toBeInTheDocument();
    expect(screen.getByText('42% savings')).toBeInTheDocument();
  });

  it('approves a routing suggestion and refreshes the pending list', async () => {
    featureState.routingOptimizer = true;
    let suggestions: unknown[] = [
      {
        id: 'suggestion-1',
        task_type: 'summarization',
        current_model: 'claude-sonnet',
        suggested_model: 'claude-haiku',
        reason: 'claude-haiku costs less for summarization tasks',
        savings_percent: 0.42,
        savings_usd_per_1k: 0.018,
        status: 'pending',
      },
    ];

    mockUsageData(suggestions);
    server.use(
      http.post(`${BASE}/infra/routing/suggestions/:id/approve`, () => {
        suggestions = [];
        return HttpResponse.json({ id: 'suggestion-1', status: 'approved' });
      }),
      http.get(`${BASE}/infra/routing/suggestions`, () => HttpResponse.json(suggestions)),
    );

    renderWithRouter(<Usage />);

    await userEvent.click(await screen.findByRole('button', { name: /optimizer/i }));
    await screen.findByText('summarization');
    await userEvent.click(screen.getByRole('button', { name: /approve/i }));

    await waitFor(() => {
      expect(screen.getByText('No pending routing suggestions')).toBeInTheDocument();
    });
  });

  it('renders routing optimizer stats', async () => {
    featureState.routingOptimizer = true;
    mockUsageData([], [
      {
        model: 'claude-haiku',
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
    ]);

    renderWithRouter(<Usage />);

    await screen.findByRole('button', { name: /optimizer/i });
    await userEvent.click(screen.getByRole('button', { name: /optimizer/i }));
    expect(await screen.findByText('Routing Model Stats')).toBeInTheDocument();
    expect(screen.getByText('summarization')).toBeInTheDocument();
    expect(screen.getByText('claude-haiku')).toBeInTheDocument();
    expect(screen.getByText('96%')).toBeInTheDocument();
    expect(screen.getByText('$0.0120')).toBeInTheDocument();
  });

  it('renders provider tool costs as cost components', async () => {
    mockUsageData();

    renderWithRouter(<Usage />);

    expect(await screen.findByText('Cost components')).toBeInTheDocument();
    expect(screen.getByText('Provider tools')).toBeInTheDocument();
    expect(screen.getByText('Unpriced tools')).toBeInTheDocument();
    expect(screen.getAllByText('provider-web-search').length).toBeGreaterThan(0);
    expect(screen.getByText(/1 calls missing pricing metadata/i)).toBeInTheDocument();
    expect(screen.getAllByText('exact, unknown').length).toBeGreaterThan(0);
    expect(screen.getAllByText('$0.0200').length).toBeGreaterThan(0);
  });

  it('does not call optimizer-only endpoints when the feature is disabled', async () => {
    let suggestionCalls = 0;
    let statsCalls = 0;
    server.use(
      http.get('*/__agency/config', () => HttpResponse.json({ gateway: 'http://localhost:8200' })),
      http.get(`${BASE}/infra/routing/metrics`, () => HttpResponse.json(metrics)),
      http.get(`${BASE}/infra/routing/suggestions`, () => {
        suggestionCalls += 1;
        return HttpResponse.json([]);
      }),
      http.get(`${BASE}/infra/routing/stats`, () => {
        statsCalls += 1;
        return HttpResponse.json([]);
      }),
    );

    renderWithRouter(<Usage />);

    await screen.findByText('Cost components');
    expect(screen.queryByRole('button', { name: /optimizer/i })).not.toBeInTheDocument();
    expect(suggestionCalls).toBe(0);
    expect(statsCalls).toBe(0);
  });

  it('shows recent routing errors in the recent calls ledger', async () => {
    server.use(
      http.get('*/__agency/config', () => HttpResponse.json({ gateway: 'http://localhost:8200' })),
      http.get(`${BASE}/infra/routing/metrics`, () =>
        HttpResponse.json({
          ...metrics,
          totals: { ...metrics.totals, errors: 2 },
          recent_errors: [
            {
              ts: new Date().toISOString(),
              agent: 'alice',
              model: 'claude-sonnet',
              status: 429,
              error: 'Rate limit exceeded',
            },
          ],
        }),
      ),
      http.get(`${BASE}/infra/routing/suggestions`, () => HttpResponse.json([])),
      http.get(`${BASE}/infra/routing/stats`, () => HttpResponse.json([])),
    );

    renderWithRouter(<Usage />, { route: '/admin/usage' });

    expect(await screen.findByText('Recent calls')).toBeInTheDocument();
    expect(screen.getByText('alice')).toBeInTheDocument();
    expect(screen.getByText('claude-sonnet')).toBeInTheDocument();
    expect(screen.getByText('429')).toBeInTheDocument();
  });

});
