import { describe, it, expect, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { server } from '../../test/server';
import { renderWithRouter } from '../../test/render';
import { Overview } from './Overview';

vi.mock('../../lib/ws', () => ({ socket: { on: () => () => {}, connect: () => {}, disconnect: () => {}, connected: false } }));

const BASE = 'http://localhost:8200/api/v1';

function wrapInfra(components: any[]) {
  return { version: '0.1.0', build_id: 'test', components };
}

describe('Overview', () => {
  beforeEach(() => {
    server.use(
      http.get(`${BASE}/infra/providers`, () =>
        HttpResponse.json([
          { name: 'anthropic', display_name: 'Anthropic', description: '', category: 'cloud', installed: true, credential_configured: true },
          { name: 'openai', display_name: 'OpenAI', description: '', category: 'cloud', installed: true, credential_configured: true },
        ]),
      ),
      http.get(`${BASE}/infra/routing/config`, () =>
        HttpResponse.json({ configured: true, version: 'test' }),
      ),
    );
  });

  it('renders agent summary table', async () => {
    server.use(
      http.get(`${BASE}/agents`, () =>
        HttpResponse.json([
          { name: 'steve', status: 'running', mode: 'autonomous', team: 'alpha', preset: 'ops', enforcer: 'active' },
        ]),
      ),
      http.get(`${BASE}/infra/status`, () => HttpResponse.json(wrapInfra([]))),
      http.get(`${BASE}/agents/steve/logs`, () => HttpResponse.json([])),
    );
    renderWithRouter(<Overview />);
    await waitFor(() => {
      expect(screen.getByText('steve')).toBeInTheDocument();
      expect(screen.getByText('autonomous')).toBeInTheDocument();
    });
  });

  it('renders infrastructure status strip', async () => {
    server.use(
      http.get(`${BASE}/agents`, () => HttpResponse.json([])),
      http.get(`${BASE}/infra/status`, () =>
        HttpResponse.json(wrapInfra([
          { name: 'gateway', state: 'running', health: 'healthy' },
          { name: 'redis', state: 'running', health: 'healthy' },
        ])),
      ),
    );
    renderWithRouter(<Overview />);
    await waitFor(() => {
      expect(screen.getByText('gateway')).toBeInTheDocument();
      expect(screen.getByText('redis')).toBeInTheDocument();
    });
  });

  it('shows loading state initially', () => {
    renderWithRouter(<Overview />);
    expect(screen.getByText('Loading...')).toBeInTheDocument();
  });

  it('starts infrastructure via button', async () => {
    let started = false;
    server.use(
      http.get(`${BASE}/agents`, () =>
        HttpResponse.json([
          { name: 'steve', status: 'running', mode: 'autonomous', team: 'alpha', preset: 'ops', enforcer: 'active' },
        ]),
      ),
      http.get(`${BASE}/infra/status`, () =>
        HttpResponse.json(wrapInfra([
          { name: 'egress', state: 'missing', health: 'none', container_id: '', uptime: '' },
          { name: 'comms', state: 'missing', health: 'none', container_id: '', uptime: '' },
        ])),
      ),
      http.get(`${BASE}/agents/steve/logs`, () => HttpResponse.json([])),
      http.post(`${BASE}/infra/up`, () => {
        started = true;
        return HttpResponse.json({ ok: true });
      }),
    );
    renderWithRouter(<Overview />);
    await waitFor(() => {
      expect(screen.getByText('steve')).toBeInTheDocument();
      expect(screen.getByRole('button', { name: /^start infra$/i })).toBeInTheDocument();
    });
    await userEvent.click(screen.getByRole('button', { name: /^start infra$/i }));
    await waitFor(() => {
      expect(started).toBe(true);
    });
  });

  it('shows restart infra when services are already running', async () => {
    server.use(
      http.get(`${BASE}/agents`, () => HttpResponse.json([])),
      http.get(`${BASE}/infra/status`, () =>
        HttpResponse.json(wrapInfra([
          { name: 'gateway', state: 'running', health: 'healthy' },
          { name: 'redis', state: 'running', health: 'healthy' },
        ])),
      ),
    );

    renderWithRouter(<Overview />);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /restart infra/i })).toBeInTheDocument();
    });
  });

  it('shows start guidance when infrastructure is down', async () => {
    server.use(
      http.get(`${BASE}/agents`, () => HttpResponse.json([])),
      http.get(`${BASE}/infra/status`, () =>
        HttpResponse.json(wrapInfra([
          { name: 'gateway', state: 'missing', health: 'none' },
        ])),
      ),
    );

    renderWithRouter(<Overview />, { route: '/' });

    await waitFor(() => {
      expect(screen.getByText('Suggested next steps')).toBeInTheDocument();
      expect(screen.getByText(/start infrastructure first/i)).toBeInTheDocument();
      expect(screen.getByRole('button', { name: /start infrastructure/i })).toBeInTheDocument();
    });
  });

  it('shows agent creation guidance when infrastructure is up but no agents exist', async () => {
    server.use(
      http.get(`${BASE}/agents`, () => HttpResponse.json([])),
      http.get(`${BASE}/infra/status`, () =>
        HttpResponse.json(wrapInfra([
          { name: 'gateway', state: 'running', health: 'healthy' },
        ])),
      ),
    );

    renderWithRouter(<Overview />, { route: '/' });

    await waitFor(() => {
      expect(screen.getByText(/create your first agent, then open its dm/i)).toBeInTheDocument();
      expect(screen.getByRole('link', { name: 'Create first agent' })).toHaveAttribute('href', '/agents');
      expect(screen.getByRole('link', { name: 'Review providers' })).toHaveAttribute('href', '/setup');
    });
  });

  it('shows provider coverage for tester orientation', async () => {
    server.use(
      http.get(`${BASE}/agents`, () => HttpResponse.json([])),
      http.get(`${BASE}/infra/status`, () =>
        HttpResponse.json(wrapInfra([
          { name: 'gateway', state: 'running', health: 'healthy' },
        ])),
      ),
    );

    renderWithRouter(<Overview />, { route: '/' });

    await waitFor(() => {
      expect(screen.getByText('Provider Coverage')).toBeInTheDocument();
      expect(screen.getByText('Anthropic')).toBeInTheDocument();
      expect(screen.getByText('OpenAI')).toBeInTheDocument();
      expect(screen.getByText('Routing ready')).toBeInTheDocument();
      expect(screen.getByRole('link', { name: 'Open provider setup' })).toHaveAttribute('href', '/setup');
    });
  });

  it('shows operator flow shortcuts when infrastructure and agents are available', async () => {
    server.use(
      http.get(`${BASE}/agents`, () =>
        HttpResponse.json([
          { name: 'steve', status: 'running', mode: 'autonomous', team: 'alpha', preset: 'ops', enforcer: 'active' },
        ]),
      ),
      http.get(`${BASE}/infra/status`, () =>
        HttpResponse.json(wrapInfra([
          { name: 'gateway', state: 'running', health: 'healthy' },
        ])),
      ),
      http.get(`${BASE}/agents/steve/logs`, () => HttpResponse.json([])),
    );

    renderWithRouter(<Overview />, { route: '/' });

    await waitFor(() => {
      expect(screen.getByText(/open a dm, inspect recent activity, or review graph context/i)).toBeInTheDocument();
      expect(screen.getAllByRole('link', { name: 'Open channels' }).length).toBeGreaterThan(0);
      expect(screen.getByRole('link', { name: 'Open knowledge' })).toHaveAttribute('href', '/knowledge');
    });
  });

  it('stops infrastructure via button', async () => {
    let stopped = false;
    server.use(
      http.get(`${BASE}/agents`, () =>
        HttpResponse.json([
          { name: 'steve', status: 'running', mode: 'autonomous', team: 'alpha', preset: 'ops', enforcer: 'active' },
        ]),
      ),
      http.get(`${BASE}/infra/status`, () =>
        HttpResponse.json(wrapInfra([
          { name: 'gateway', state: 'running', health: 'healthy', container_id: 'abc', uptime: '2h' },
          { name: 'redis', state: 'running', health: 'healthy', container_id: 'def', uptime: '2h' },
        ])),
      ),
      http.get(`${BASE}/agents/steve/logs`, () => HttpResponse.json([])),
      http.post(`${BASE}/infra/down`, () => {
        stopped = true;
        return HttpResponse.json({ ok: true });
      }),
    );
    renderWithRouter(<Overview />);
    await waitFor(() => {
      expect(screen.getByText('steve')).toBeInTheDocument();
    });
    await userEvent.click(screen.getByRole('button', { name: /stop infra/i }));
    await waitFor(() => {
      expect(stopped).toBe(true);
    });
  });

  it('restarts infrastructure via the primary button when services are running', async () => {
    let restarted = false;
    server.use(
      http.get(`${BASE}/agents`, () => HttpResponse.json([])),
      http.get(`${BASE}/infra/status`, () =>
        HttpResponse.json(wrapInfra([
          { name: 'gateway', state: 'running', health: 'healthy' },
          { name: 'redis', state: 'running', health: 'healthy' },
        ])),
      ),
      http.post(`${BASE}/infra/reload`, () => {
        restarted = true;
        return HttpResponse.json({ ok: true });
      }),
    );

    renderWithRouter(<Overview />);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /restart infra/i })).toBeInTheDocument();
    });

    await userEvent.click(screen.getByRole('button', { name: /restart infra/i }));
    await waitFor(() => {
      expect(restarted).toBe(true);
    });
  });
});
