import { describe, it, expect } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { server } from '../../test/server';
import { renderWithRouter } from '../../test/render';
import { Usage } from './Usage';

const BASE = 'http://localhost:8200/api/v1';

const metrics = {
  period: { since: '2026-04-08T00:00:00Z', until: '2026-04-09T00:00:00Z' },
  totals: {
    requests: 12,
    input_tokens: 12000,
    output_tokens: 4000,
    total_tokens: 16000,
    est_cost_usd: 0.25,
    errors: 0,
    avg_latency_ms: 900,
  },
  by_agent: {},
  by_model: {},
  by_provider: {},
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
  it('renders pending routing suggestions', async () => {
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

    expect(await screen.findByText('Routing Suggestions')).toBeInTheDocument();
    expect(screen.getByText('summarization')).toBeInTheDocument();
    expect(screen.getByText('claude-sonnet')).toBeInTheDocument();
    expect(screen.getByText('claude-haiku')).toBeInTheDocument();
    expect(screen.getByText('42% savings')).toBeInTheDocument();
  });

  it('approves a routing suggestion and refreshes the pending list', async () => {
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

    await screen.findByText('summarization');
    await userEvent.click(screen.getByRole('button', { name: /approve/i }));

    await waitFor(() => {
      expect(screen.getByText('No pending routing suggestions')).toBeInTheDocument();
    });
  });

  it('renders routing optimizer stats', async () => {
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

    expect(await screen.findByText('Routing Model Stats')).toBeInTheDocument();
    expect(screen.getByText('summarization')).toBeInTheDocument();
    expect(screen.getByText('claude-haiku')).toBeInTheDocument();
    expect(screen.getByText('96%')).toBeInTheDocument();
    expect(screen.getByText('$0.0120')).toBeInTheDocument();
  });

  it('shows recovery guidance when recent routing errors exist', async () => {
    server.use(
      http.get('*/__agency/config', () => HttpResponse.json({ gateway: 'http://localhost:8200' })),
      http.get(`${BASE}/infra/routing/metrics`, () =>
        HttpResponse.json({
          ...metrics,
          totals: { ...metrics.totals, errors: 2 },
          recent_errors: [
            {
              ts: '2026-04-09T12:00:00Z',
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

    await waitFor(() => {
      expect(screen.getByText(/recent routing error/)).toBeInTheDocument();
      expect(screen.getAllByRole('link', { name: 'Open Agent: alice' }).length).toBeGreaterThan(0);
      expect(screen.getAllByRole('link', { name: 'Open Doctor' }).length).toBeGreaterThan(0);
      expect(screen.getByText('Rate limit exceeded')).toBeInTheDocument();
      expect(screen.getByText(/looks like a provider or rate-limit issue/i)).toBeInTheDocument();
    });
  });
});
