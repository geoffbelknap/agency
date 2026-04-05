import { describe, it, expect, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { server } from '../../test/server';
import { renderWithRouter } from '../../test/render';
import { Intake } from './Intake';

const BASE = 'http://localhost:8200/api/v1';

describe('Intake', () => {
  it('renders connectors from API', async () => {
    server.use(
      http.get(`${BASE}/hub/instances`, () =>
        HttpResponse.json([
          { id: 'c1', name: 'github-webhook', kind: 'connector', source: 'hub:github-webhook', state: 'active' },
        ]),
      ),
    );
    renderWithRouter(<Intake />);
    await waitFor(() => {
      expect(screen.getByText('github-webhook')).toBeInTheDocument();
    });
  });

  it('renders work items from API with stats', async () => {
    server.use(
      http.get(`${BASE}/intake/items`, () =>
        HttpResponse.json([
          { id: 'wi-1', status: 'unrouted', connector: 'github', source: 'github', summary: 'PR #42', created_at: '2026-03-16' },
          { id: 'wi-2', status: 'routed', connector: 'slack', source: 'slack', summary: 'Thread reply', created_at: '2026-03-16' },
        ]),
      ),
      http.get(`${BASE}/intake/stats`, () =>
        HttpResponse.json({ pending: 1, processing: 0, done: 1, failed: 0 }),
      ),
    );
    renderWithRouter(<Intake />);
    await userEvent.click(screen.getByRole('tab', { name: /work items/i }));
    await waitFor(() => {
      // The work items list shows connector names as primary identifier
      expect(screen.getByText('github')).toBeInTheDocument();
    });
  });

  it('checks connector status', async () => {
    server.use(
      http.get(`${BASE}/hub/instances`, () =>
        HttpResponse.json([
          { id: 'c1', name: 'github-webhook', kind: 'connector', source: 'hub:github-webhook', state: 'active' },
        ]),
      ),
      http.get(`${BASE}/hub/github-webhook`, () =>
        HttpResponse.json({ state: 'active' }),
      ),
    );
    renderWithRouter(<Intake />);
    await waitFor(() => {
      expect(screen.getByText('github-webhook')).toBeInTheDocument();
    });
    // The connector row is a button — click it to expand and reveal action buttons
    await userEvent.click(screen.getByRole('button', { name: /github-webhook/i }));
    // After expanding, the Deactivate button should be visible (active connector)
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /deactivate/i })).toBeInTheDocument();
    });
  });

  it('activates connector on button click', async () => {
    let activated = false;
    server.use(
      http.get(`${BASE}/hub/instances`, () =>
        HttpResponse.json([
          { id: 'c2', name: 'slack-poll', kind: 'connector', source: 'hub:slack-poll', state: 'inactive' },
        ]),
      ),
      http.post(`${BASE}/hub/slack-poll/activate`, () => {
        activated = true;
        return HttpResponse.json({ ok: true });
      }),
    );
    renderWithRouter(<Intake />);
    await waitFor(() => {
      expect(screen.getByText('slack-poll')).toBeInTheDocument();
    });
    // Expand the connector row to reveal action buttons
    await userEvent.click(screen.getByRole('button', { name: /slack-poll/i }));
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /activate/i })).toBeInTheDocument();
    });
    await userEvent.click(screen.getByRole('button', { name: /activate/i }));
    await waitFor(() => {
      expect(activated).toBe(true);
    });
  });
});
