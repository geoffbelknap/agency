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
}: {
  candidates?: unknown[];
  curationEntries?: unknown[];
} = {}) {
  server.use(
    http.get(`${BASE}/graph/ontology/candidates`, () =>
      HttpResponse.json({ candidates }),
    ),
    http.get(`${BASE}/graph/curation-log`, () =>
      HttpResponse.json({ entries: curationEntries }),
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

  it('keeps ontology decisions reversible via restore', async () => {
    let candidates = [] as Array<{ id: string; value: string; count?: number; status?: string }>;
    let curationEntries = [
      {
        id: 'entry-1',
        action: 'ontology_reject',
        node_id: 'cand-1',
        value: 'device',
        timestamp: '2026-04-09T10:00:00Z',
      },
    ];

    server.use(
      http.get(`${BASE}/graph/stats`, () =>
        HttpResponse.json({ node_count: 0, edge_count: 0 }),
      ),
      http.get(`${BASE}/graph/ontology/candidates`, () =>
        HttpResponse.json({ candidates }),
      ),
      http.get(`${BASE}/graph/curation-log`, () =>
        HttpResponse.json({ entries: curationEntries }),
      ),
      http.post(`${BASE}/graph/ontology/restore`, async ({ request }) => {
        const body = await request.json() as { node_id?: string };
        expect(body.node_id).toBe('cand-1');
        candidates = [{ id: 'cand-1', value: 'device', count: 3, status: 'candidate' }];
        curationEntries = [
          {
            id: 'entry-restore',
            action: 'ontology_restore',
            node_id: 'cand-1',
            value: 'device',
            timestamp: '2026-04-09T10:05:00Z',
          },
        ];
        return HttpResponse.json({ restored: true, node_id: 'cand-1' });
      }),
    );

    renderWithRouter(<Knowledge />);

    await waitFor(() => {
      expect(screen.getByText('Recent Decisions')).toBeInTheDocument();
      expect(screen.getByRole('button', { name: /restore/i })).toBeInTheDocument();
    });

    await userEvent.click(screen.getByRole('button', { name: /restore/i }));

    await waitFor(() => {
      expect(screen.getByText('Pending Candidates')).toBeInTheDocument();
      expect(screen.getAllByText('device')[0]).toBeInTheDocument();
      expect(screen.getByText('candidate')).toBeInTheDocument();
    });
  });
});
