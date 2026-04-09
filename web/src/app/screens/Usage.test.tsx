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
};

function mockUsageData(suggestions: unknown[] = []) {
  server.use(
    http.get('*/__agency/config', () => HttpResponse.json({ gateway: 'http://localhost:8200' })),
    http.get(`${BASE}/infra/routing/metrics`, () => HttpResponse.json(metrics)),
    http.get(`${BASE}/infra/routing/suggestions`, () => HttpResponse.json(suggestions)),
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
});
