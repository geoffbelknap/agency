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
      http.get(`${BASE}/hub/intake/poll-health`, () =>
        HttpResponse.json({ connectors: { 'github-webhook': { status: 'healthy' } } }),
      ),
    );
    renderWithRouter(<Intake />);
    await waitFor(() => {
      expect(screen.getByText('github-webhook')).toBeInTheDocument();
      expect(screen.getByText('Healthy Polling')).toBeInTheDocument();
      expect(screen.getByText('Needs Review')).toBeInTheDocument();
    });
  });

  it('shows next-step guidance when no connectors are configured', async () => {
    server.use(
      http.get(`${BASE}/hub/instances`, () => HttpResponse.json([])),
      http.get(`${BASE}/events/intake/items`, () => HttpResponse.json([])),
      http.get(`${BASE}/hub/intake/poll-health`, () => HttpResponse.json({})),
    );

    renderWithRouter(<Intake />);

    await waitFor(() => {
      expect(screen.getByText('Start by adding a connector')).toBeInTheDocument();
      expect(screen.getByRole('link', { name: 'Open Hub' })).toHaveAttribute('href', '/admin/hub');
      expect(screen.getByRole('link', { name: 'Open Missions' })).toHaveAttribute('href', '/missions');
    });
  });

  it('shows connector readiness guidance and source labeling', async () => {
    server.use(
      http.get(`${BASE}/hub/instances`, () =>
        HttpResponse.json([
          { id: 'c1', name: 'github-webhook', kind: 'connector', source: 'hub:github-webhook', state: 'active' },
          { id: 'c2', name: 'custom-sync', kind: 'connector', source: 'partner-sync', state: 'inactive' },
        ]),
      ),
      http.get(`${BASE}/hub/intake/poll-health`, () =>
        HttpResponse.json({
          connectors: {
            'github-webhook': { status: 'healthy', last_poll: '2026-04-11T08:00:00Z' },
            'custom-sync': { status: 'error', last_error: 'auth failed' },
          },
        }),
      ),
    );

    renderWithRouter(<Intake />);

    await waitFor(() => {
      expect(screen.getByText('github-webhook')).toBeInTheDocument();
      expect(screen.getByText('custom-sync')).toBeInTheDocument();
      expect(screen.getByText('Active')).toBeInTheDocument();
      expect(screen.getByText('Inactive')).toBeInTheDocument();
      expect(screen.getByText('Healthy Polling')).toBeInTheDocument();
      expect(screen.getByText('Needs Review')).toBeInTheDocument();
    });

    await userEvent.click(screen.getByRole('button', { name: /github-webhook/i }));
    await waitFor(() => {
      expect(screen.getByText('Hub catalog')).toBeInTheDocument();
      expect(screen.getByText('Ready to ingest')).toBeInTheDocument();
      expect(screen.getByText(/connector state and intake health both look good/i)).toBeInTheDocument();
    });

    await userEvent.click(screen.getByRole('button', { name: /custom-sync/i }));
    await waitFor(() => {
      expect(screen.getByText('Custom source: partner-sync')).toBeInTheDocument();
      expect(screen.getByText('Inactive connector')).toBeInTheDocument();
      expect(screen.getByText(/installed but not currently ingesting work/i)).toBeInTheDocument();
    });
  });

  it('renders work items from API with stats', async () => {
    server.use(
      http.get(`${BASE}/events/intake/items`, () =>
        HttpResponse.json([
          { id: 'wi-1', status: 'unrouted', connector: 'github', source: 'github', summary: 'PR #42', created_at: '2026-03-16' },
          { id: 'wi-2', status: 'routed', connector: 'slack', source: 'slack', summary: 'Thread reply', created_at: '2026-03-16' },
        ]),
      ),
      http.get(`${BASE}/events/intake/stats`, () =>
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

  it('links routed work items to their current target', async () => {
    server.use(
      http.get(`${BASE}/events/intake/items`, () =>
        HttpResponse.json([
          {
            id: 'wi-1',
            status: 'routed',
            connector: 'github',
            source: 'github',
            summary: 'PR #42 needs triage',
            target_type: 'mission',
            target_name: 'triage-inbound',
            created_at: '2026-03-16',
            payload: { number: 42 },
          },
        ]),
      ),
    );

    renderWithRouter(<Intake />);
    await userEvent.click(screen.getByRole('tab', { name: /work items/i }));
    await waitFor(() => {
      expect(screen.getByText('PR #42 needs triage')).toBeInTheDocument();
    });

    await userEvent.click(screen.getByRole('button', { name: /github/i }));
    await waitFor(() => {
      expect(screen.getByRole('link', { name: 'mission: triage-inbound' })).toHaveAttribute('href', '/missions/triage-inbound');
      expect(screen.getByRole('link', { name: 'Open route target' })).toHaveAttribute('href', '/missions/triage-inbound');
    });
  });

  it('switches to the connectors tab when opening a connector from a work item', async () => {
    server.use(
      http.get(`${BASE}/hub/instances`, () =>
        HttpResponse.json([
          { id: 'c1', name: 'github', kind: 'connector', source: 'hub:github', state: 'active' },
        ]),
      ),
      http.get(`${BASE}/hub/intake/poll-health`, () =>
        HttpResponse.json({ connectors: { github: { status: 'healthy' } } }),
      ),
      http.get(`${BASE}/events/intake/items`, () =>
        HttpResponse.json([
          {
            id: 'wi-1',
            status: 'routed',
            connector: 'github',
            source: 'github',
            summary: 'PR #42 needs triage',
            target_type: 'mission',
            target_name: 'triage-inbound',
            created_at: '2026-03-16',
            payload: { number: 42 },
            route_index: 0,
          },
        ]),
      ),
    );

    renderWithRouter(<Intake />);
    await userEvent.click(screen.getByRole('tab', { name: /work items/i }));
    await waitFor(() => {
      expect(screen.getByText('PR #42 needs triage')).toBeInTheDocument();
    });

    await userEvent.click(screen.getByRole('button', { name: /github/i }));
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Open connector' })).toBeInTheDocument();
    });
    await userEvent.click(screen.getByRole('button', { name: 'Open connector' }));

    await waitFor(() => {
      expect(screen.getByRole('tab', { name: /connectors/i })).toHaveAttribute('aria-selected', 'true');
      expect(screen.getByText('Ready to ingest')).toBeInTheDocument();
      expect(screen.getByRole('button', { name: /poll now/i })).toBeInTheDocument();
    });
  });

  it('shows routing guidance for unrouted work items', async () => {
    server.use(
      http.get(`${BASE}/hub/instances`, () =>
        HttpResponse.json([
          { id: 'c9', name: 'linear', kind: 'connector', source: 'hub:linear', state: 'active' },
        ]),
      ),
      http.get(`${BASE}/hub/intake/poll-health`, () =>
        HttpResponse.json({ connectors: { linear: { status: 'healthy' } } }),
      ),
      http.get(`${BASE}/events/intake/items`, () =>
        HttpResponse.json([
          {
            id: 'wi-9',
            status: 'unrouted',
            connector: 'linear',
            source: 'linear',
            summary: 'New bug report arrived',
            created_at: '2026-03-16',
            payload: { issue: 'BUG-9' },
          },
        ]),
      ),
    );

    renderWithRouter(<Intake />);
    await userEvent.click(screen.getByRole('tab', { name: /work items/i }));

    await waitFor(() => {
      expect(screen.getByText('1 item needs routing')).toBeInTheDocument();
      expect(screen.getByText('New bug report arrived')).toBeInTheDocument();
      expect(screen.getByText('Work is arriving but needs routing')).toBeInTheDocument();
      expect(screen.getByRole('link', { name: 'Open Missions' })).toHaveAttribute('href', '/missions');
      expect(screen.getByRole('link', { name: 'Open Events' })).toHaveAttribute('href', '/admin/events');
    });

    await userEvent.click(screen.getByRole('button', { name: /linear/i }));
    await waitFor(() => {
      expect(screen.getByText('Needs routing')).toBeInTheDocument();
      expect(screen.getByText(/no route target has been assigned yet/i)).toBeInTheDocument();
      expect(screen.getByText('Route rule likely missing')).toBeInTheDocument();
      expect(screen.getByText(/no mission, agent, or channel claimed it/i)).toBeInTheDocument();
    });
  });

  it('shows attribution guidance when the producing connector is inactive', async () => {
    server.use(
      http.get(`${BASE}/hub/instances`, () =>
        HttpResponse.json([
          { id: 'c1', name: 'partner-sync', kind: 'connector', source: 'partner-sync', state: 'inactive' },
        ]),
      ),
      http.get(`${BASE}/hub/intake/poll-health`, () =>
        HttpResponse.json({ connectors: { 'partner-sync': { status: 'error' } } }),
      ),
      http.get(`${BASE}/events/intake/items`, () =>
        HttpResponse.json([
          {
            id: 'wi-2',
            status: 'pending',
            connector: 'partner-sync',
            summary: 'Queued event',
            created_at: '2026-03-16',
          },
        ]),
      ),
    );

    renderWithRouter(<Intake />);
    await userEvent.click(screen.getByRole('tab', { name: /work items/i }));

    await waitFor(() => {
      expect(screen.getByText('Queued event')).toBeInTheDocument();
    });

    await userEvent.click(screen.getByRole('button', { name: /partner-sync/i }));
    await waitFor(() => {
      expect(screen.getByText('Connector is inactive')).toBeInTheDocument();
      expect(screen.getByText(/re-check setup, activation, and poll health/i)).toBeInTheDocument();
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

  it('manually triggers connector polling', async () => {
    let triggered = false;
    server.use(
      http.get(`${BASE}/hub/instances`, () =>
        HttpResponse.json([
          { id: 'c3', name: 'github-poll', kind: 'connector', source: 'hub:github-poll', state: 'active' },
        ]),
      ),
      http.get(`${BASE}/hub/intake/poll-health`, () =>
        HttpResponse.json({ connectors: { 'github-poll': { status: 'healthy', last_poll: '2026-04-09T20:00:00Z' } } }),
      ),
      http.post(`${BASE}/hub/intake/poll/github-poll`, () => {
        triggered = true;
        return HttpResponse.json({ ok: true, connector: 'github-poll' });
      }),
    );

    renderWithRouter(<Intake />);
    await waitFor(() => {
      expect(screen.getByText('github-poll')).toBeInTheDocument();
    });

    await userEvent.click(screen.getByRole('button', { name: /github-poll/i }));
    await userEvent.click(screen.getByRole('button', { name: /poll now/i }));

    await waitFor(() => {
      expect(triggered).toBe(true);
    });
  });

  it('configures and activates a connector from setup', async () => {
    let configured = false;
    let activated = false;
    server.use(
      http.get(`${BASE}/hub/instances`, () =>
        HttpResponse.json([
          { id: 'c4', name: 'linear-poll', kind: 'connector', source: 'hub:linear-poll', state: activated ? 'active' : 'inactive' },
        ]),
      ),
      http.get(`${BASE}/hub/connectors/linear-poll/requirements`, () =>
        HttpResponse.json({
          connector: 'linear-poll',
          ready: false,
          credentials: [
            {
              name: 'LINEAR_API_KEY',
              description: 'Linear API key',
              required: true,
              configured: false,
              setup_url: 'https://linear.app/settings/api',
            },
          ],
          egress_domains: [{ domain: 'api.linear.app', allowed: false }],
        }),
      ),
      http.post(`${BASE}/hub/connectors/linear-poll/configure`, async ({ request }) => {
        const body = await request.json() as { credentials?: Record<string, string> };
        configured = body.credentials?.LINEAR_API_KEY === 'lin_test';
        return HttpResponse.json({
          configured: ['LINEAR_API_KEY'],
          auth_configured: false,
          egress_domains_added: ['api.linear.app'],
          ready: true,
        });
      }),
      http.post(`${BASE}/hub/linear-poll/activate`, () => {
        activated = true;
        return HttpResponse.json({ ok: true });
      }),
    );

    renderWithRouter(<Intake />);
    await waitFor(() => {
      expect(screen.getByText('linear-poll')).toBeInTheDocument();
    });

    await userEvent.click(screen.getByRole('button', { name: /linear-poll/i }));
    await userEvent.click(screen.getByRole('button', { name: /setup/i }));

    await waitFor(() => {
      expect(screen.getByText(/get key/i)).toBeInTheDocument();
    });
    expect(screen.getByRole('button', { name: /configure and activate/i })).toBeDisabled();

    await userEvent.type(screen.getByPlaceholderText('LINEAR_API_KEY'), 'lin_test');
    await userEvent.click(screen.getByRole('button', { name: /configure and activate/i }));

    await waitFor(() => {
      expect(configured).toBe(true);
      expect(activated).toBe(true);
    });
  });
});
