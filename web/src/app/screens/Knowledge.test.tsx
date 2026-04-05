import { describe, it, expect } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { server } from '../../test/server';
import { renderWithRouter } from '../../test/render';
import { Knowledge } from './Knowledge';

const BASE = 'http://localhost:8200/api/v1';

describe('Knowledge', () => {
  it('renders stats on mount', async () => {
    server.use(
      http.get(`${BASE}/knowledge/stats`, () =>
        HttpResponse.json({ node_count: 42, edge_count: 100 }),
      ),
    );
    renderWithRouter(<Knowledge />);
    await waitFor(() => {
      expect(screen.getByText('42')).toBeInTheDocument();
      expect(screen.getByText('100')).toBeInTheDocument();
    });
  });

  it('queries who-knows and renders results', async () => {
    server.use(
      http.get(`${BASE}/knowledge/stats`, () =>
        HttpResponse.json({ node_count: 0, edge_count: 0 }),
      ),
      http.get(`${BASE}/knowledge/who-knows`, () =>
        HttpResponse.json({
          agents: [
            { name: 'alice', confidence: 0.95, topics: ['deployment', 'docker'] },
          ],
        }),
      ),
    );
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
      http.get(`${BASE}/knowledge/stats`, () =>
        HttpResponse.json({ node_count: 0, edge_count: 0 }),
      ),
      http.post(`${BASE}/knowledge/query`, async ({ request }) => {
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
    renderWithRouter(<Knowledge />);
    const input = screen.getByPlaceholderText(/search topics and content/i);
    await userEvent.type(input, 'deployment');
    await userEvent.click(screen.getByRole('button', { name: /^search$/i }));
    await waitFor(() => {
      expect(screen.getByText('CI/CD')).toBeInTheDocument();
    });
  });
});
