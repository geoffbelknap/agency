import { describe, it, expect } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { server } from '../../test/server';
import { renderWithRouter } from '../../test/render';
import { Knowledge } from './Knowledge';

const BASE = 'http://localhost:8200/api/v1';

function mockOntologyReviewData({
  candidates = [],
  curationEntries = [],
  pending = [],
  quarantined = [],
  classification = { tiers: [] },
  principals = [],
  communities = { communities: [] },
  hubs = { hubs: [] },
}: {
  candidates?: unknown[];
  curationEntries?: unknown[];
  pending?: unknown[];
  quarantined?: unknown[];
  classification?: unknown;
  principals?: unknown;
  communities?: unknown;
  hubs?: unknown;
} = {}) {
  server.use(
    http.get(`${BASE}/graph/ontology/candidates`, () =>
      HttpResponse.json({ candidates }),
    ),
    http.get(`${BASE}/graph/curation-log`, () =>
      HttpResponse.json({ entries: curationEntries }),
    ),
    http.get(`${BASE}/graph/pending`, () =>
      HttpResponse.json({ pending }),
    ),
    http.get(`${BASE}/graph/quarantine`, () =>
      HttpResponse.json({ nodes: quarantined }),
    ),
    http.get(`${BASE}/graph/classification`, () =>
      HttpResponse.json(classification),
    ),
    http.get(`${BASE}/graph/principals`, () =>
      HttpResponse.json(principals),
    ),
    http.get(`${BASE}/graph/communities`, () =>
      HttpResponse.json(communities),
    ),
    http.get(`${BASE}/graph/hubs`, () =>
      HttpResponse.json(hubs),
    ),
  );
}

describe('Knowledge', () => {
  it('renders stats on mount', async () => {
    server.use(
      http.get(`${BASE}/graph/stats`, () =>
        HttpResponse.json({ node_count: 42, edge_count: 100 }),
      ),
    );
    mockOntologyReviewData();
    renderWithRouter(<Knowledge />);
    await waitFor(() => {
      expect(screen.getByText('42')).toBeInTheDocument();
      expect(screen.getByText('100')).toBeInTheDocument();
    });
  });

  it('queries who-knows and renders results', async () => {
    server.use(
      http.get(`${BASE}/graph/stats`, () =>
        HttpResponse.json({ node_count: 0, edge_count: 0 }),
      ),
      http.get(`${BASE}/graph/who-knows`, () =>
        HttpResponse.json({
          agents: [
            { name: 'alice', confidence: 0.95, topics: ['deployment', 'docker'] },
          ],
        }),
      ),
    );
    mockOntologyReviewData();
    renderWithRouter(<Knowledge />);
    const input = screen.getByPlaceholderText(/enter a topic/i);
    await userEvent.type(input, 'deployment');
    await userEvent.click(screen.getByRole('button', { name: /^find$/i }));
    await waitFor(() => {
      expect(screen.getByText('alice')).toBeInTheDocument();
    });
    expect(screen.getByText('deployment')).toBeInTheDocument();
  });

  it('queries knowledge graph', async () => {
    server.use(
      http.get(`${BASE}/graph/stats`, () =>
        HttpResponse.json({ node_count: 0, edge_count: 0 }),
      ),
      http.post(`${BASE}/graph/query`, async ({ request }) => {
        const body = await request.json() as { query?: string };
        if (body.query === 'deployment') {
          return HttpResponse.json({
            results: [
              { label: 'CI/CD', kind: 'topic', summary: 'Deployment pipeline info', source_type: 'agent', updated_at: '2026-03-16', connections: 3 },
            ],
          });
        }
        return HttpResponse.json({ results: [] });
      }),
    );
    mockOntologyReviewData();
    renderWithRouter(<Knowledge />);
    const input = screen.getByPlaceholderText(/search topics and content/i);
    await userEvent.type(input, 'deployment');
    await userEvent.click(screen.getByRole('button', { name: /^search$/i }));
    await waitFor(() => {
      expect(screen.getByText('CI/CD')).toBeInTheDocument();
    });
  });

  it('hides experimental graph governance sections in the default core workspace', async () => {
    server.use(
      http.get(`${BASE}/graph/stats`, () =>
        HttpResponse.json({ node_count: 0, edge_count: 0 }),
      ),
    );
    mockOntologyReviewData({
      curationEntries: [
        {
          id: 'entry-detail-1',
          action: 'ontology_promote',
          node_id: 'cand-detail-1',
          detail: JSON.stringify({ value: 'mystery_kind', occurrence_count: 12 }),
          timestamp: '2026-04-09T10:10:00Z',
        },
      ],
    });

    renderWithRouter(<Knowledge />);

    await waitFor(() => {
      expect(screen.getByText('Core Knowledge Surface')).toBeInTheDocument();
      expect(screen.getByText(/advanced graph governance, ontology review, quarantine, and topology inspection are experimental/i)).toBeInTheDocument();
      expect(screen.queryByText('Structural Review')).not.toBeInTheDocument();
      expect(screen.queryByText('Graph Topology')).not.toBeInTheDocument();
      expect(screen.queryByText('Quarantine')).not.toBeInTheDocument();
      expect(screen.queryByText('Ontology Review')).not.toBeInTheDocument();
    });
  });

  it('keeps experimental contribution-review data from surfacing in core mode', async () => {
    server.use(
      http.get(`${BASE}/graph/stats`, () =>
        HttpResponse.json({ node_count: 0, edge_count: 0 }),
      ),
    );

    renderWithRouter(<Knowledge />);

    await waitFor(() => {
      expect(screen.getByText('Core Knowledge Surface')).toBeInTheDocument();
      expect(screen.queryByText('Structural Review')).not.toBeInTheDocument();
    });
  });

  it('keeps experimental quarantine data hidden in core mode', async () => {
    server.use(
      http.get(`${BASE}/graph/stats`, () =>
        HttpResponse.json({ node_count: 0, edge_count: 0 }),
      ),
    );

    renderWithRouter(<Knowledge />);

    await waitFor(() => {
      expect(screen.getByText('Core Knowledge Surface')).toBeInTheDocument();
      expect(screen.queryByText('Quarantine')).not.toBeInTheDocument();
    });
  });

  it('keeps experimental topology summaries hidden in core mode', async () => {
    server.use(
      http.get(`${BASE}/graph/stats`, () =>
        HttpResponse.json({ node_count: 0, edge_count: 0 }),
      ),
    );
    mockOntologyReviewData({
      classification: { tiers: [{ tier: 'restricted', description: 'Operator-only facts' }] },
      principals: [{ uuid: 'agent:alice', type: 'agent', name: 'alice' }],
      communities: { communities: [{ id: 'community-1', label: 'Platform Ops', node_count: 4 }] },
      hubs: { hubs: [{ id: 'hub-1', label: 'release process', degree: 9 }] },
    });

    renderWithRouter(<Knowledge />);

    await waitFor(() => {
      expect(screen.getByText('Core Knowledge Surface')).toBeInTheDocument();
      expect(screen.queryByText('Graph Topology')).not.toBeInTheDocument();
    });
  });
});
