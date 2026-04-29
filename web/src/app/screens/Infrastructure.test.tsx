import { describe, it, expect, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { server } from '../../test/server';
import { renderWithRouter } from '../../test/render';
import { Infrastructure } from './Infrastructure';

vi.mock('../../lib/ws', () => ({ socket: { on: () => () => {}, connect: () => {}, disconnect: () => {}, connected: false } }));

const BASE = 'http://localhost:8200/api/v1';

function wrapInfra(components: any[]) {
  return { version: '0.1.0', build_id: 'test', components };
}

const infraServices = [
  { name: 'gateway', state: 'running', health: 'healthy', component_id: 'abc', uptime: '2h' },
  { name: 'redis', state: 'running', health: 'healthy', component_id: 'def', uptime: '2h' },
];

const stoppedInfraServices = [
  { name: 'egress', state: 'missing', health: 'none', component_id: '', uptime: '' },
  { name: 'comms', state: 'missing', health: 'none', component_id: '', uptime: '' },
];

describe('Infrastructure', () => {
  it('renders services from API', async () => {
    server.use(
      http.get(`${BASE}/infra/status`, () =>
        HttpResponse.json(wrapInfra([
          { name: 'gateway', state: 'running', health: 'healthy', component_id: 'abc123', uptime: '2h' },
          { name: 'redis', state: 'running', health: 'healthy', component_id: 'def456', uptime: '2h' },
        ])),
      ),
    );
    renderWithRouter(<Infrastructure />);
    await waitFor(() => {
      expect(screen.getByText('gateway')).toBeInTheDocument();
      expect(screen.getByText('redis')).toBeInTheDocument();
      expect(screen.getByText('2 / 2 services healthy')).toBeInTheDocument();
    });
  });

  it('renders host capacity from API', async () => {
    server.use(
      http.get(`${BASE}/infra/status`, () => HttpResponse.json(wrapInfra(infraServices))),
      http.get(`${BASE}/infra/capacity`, () =>
        HttpResponse.json({
          host_memory_mb: 32768,
          host_cpu_cores: 10,
          system_reserve_mb: 4096,
          infra_overhead_mb: 2048,
          runtime_backend: 'firecracker',
          enforcement_mode: 'microvm',
          max_agents: 8,
          max_concurrent_meesks: 3,
          agent_slot_mb: 4096,
          meeseeks_slot_mb: 2048,
          network_pool_configured: true,
          running_agents: 2,
          running_meeseeks: 1,
          available_slots: 5,
        }),
      ),
    );

    renderWithRouter(<Infrastructure />);

    await waitFor(() => {
      expect(screen.getByText('Host capacity')).toBeInTheDocument();
      expect(screen.getByText('3 / 8')).toBeInTheDocument();
      expect(screen.getByText('3 slots used / 5 available / 4.0 GB per agent / firecracker/microvm')).toBeInTheDocument();
      expect(screen.queryByText(/network pool/i)).not.toBeInTheDocument();
      expect(screen.getByText('10 cores')).toBeInTheDocument();
    });
  });

  it('restarts a service', async () => {
    let rebuilt = false;
    server.use(
      http.get(`${BASE}/infra/status`, () =>
        HttpResponse.json(wrapInfra([
          { name: 'gateway', state: 'running', health: 'healthy' },
        ])),
      ),
      http.post(`${BASE}/infra/rebuild/gateway`, () => {
        rebuilt = true;
        return HttpResponse.json({ ok: true });
      }),
    );
    renderWithRouter(<Infrastructure />);
    await waitFor(() => {
      expect(screen.getByText('gateway')).toBeInTheDocument();
    });
    await userEvent.click(screen.getByRole('button', { name: /^restart$/i }));
    await waitFor(() => {
      expect(rebuilt).toBe(true);
    });
  });

  it('starts all services', async () => {
    let started = false;
    server.use(
      http.get(`${BASE}/infra/status`, () => HttpResponse.json(wrapInfra(stoppedInfraServices))),
      http.post(`${BASE}/infra/up`, () => {
        started = true;
        return HttpResponse.json({ ok: true });
      }),
    );
    renderWithRouter(<Infrastructure />);
    await waitFor(() => {
      expect(screen.getByText('egress')).toBeInTheDocument();
      expect(screen.getAllByText('not running').length).toBeGreaterThan(0);
    });
    await userEvent.click(screen.getByRole('button', { name: /start all/i }));
    await waitFor(() => {
      expect(started).toBe(true);
    });
  });

  it('shows restart all when services are already running', async () => {
    server.use(
      http.get(`${BASE}/infra/status`, () => HttpResponse.json(wrapInfra(infraServices))),
    );

    renderWithRouter(<Infrastructure />);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /restart all/i })).toBeInTheDocument();
    });
  });

  it('shows recovery guidance when services are unhealthy', async () => {
    server.use(
      http.get(`${BASE}/infra/status`, () =>
        HttpResponse.json(wrapInfra([
          { name: 'gateway', state: 'running', health: 'unhealthy', component_id: 'abc123', uptime: '2h' },
        ])),
      ),
    );

    renderWithRouter(<Infrastructure />);

    await waitFor(() => {
      expect(screen.getByText(/1 service is unhealthy/i)).toBeInTheDocument();
      expect(screen.getByRole('button', { name: /restart all/i })).toBeInTheDocument();
    });
  });

  it('stops all services', async () => {
    let stopped = false;
    server.use(
      http.get(`${BASE}/infra/status`, () => HttpResponse.json(wrapInfra(infraServices))),
      http.post(`${BASE}/infra/down`, () => {
        stopped = true;
        return HttpResponse.json({ ok: true });
      }),
    );
    renderWithRouter(<Infrastructure />);
    await waitFor(() => {
      expect(screen.getByText('gateway')).toBeInTheDocument();
    });
    await userEvent.click(screen.getByRole('button', { name: /stop all/i }));
    await waitFor(() => {
      expect(stopped).toBe(true);
    });
  });

  it('restarts all services when the primary action is clicked while running', async () => {
    let restarted = false;
    server.use(
      http.get(`${BASE}/infra/status`, () => HttpResponse.json(wrapInfra(infraServices))),
      http.post(`${BASE}/infra/reload`, () => {
        restarted = true;
        return HttpResponse.json({ ok: true });
      }),
    );

    renderWithRouter(<Infrastructure />);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /restart all/i })).toBeInTheDocument();
    });

    await userEvent.click(screen.getByRole('button', { name: /restart all/i }));
    await waitFor(() => {
      expect(restarted).toBe(true);
    });
  });

  it('refreshes service list', async () => {
    let fetchCount = 0;
    const releaseRefreshes: Array<() => void> = [];
    server.use(
      http.get(`${BASE}/infra/status`, async () => {
        fetchCount++;
        if (fetchCount > 1) {
          await new Promise<void>((resolve) => {
            releaseRefreshes.push(resolve);
          });
        }
        return HttpResponse.json(wrapInfra(infraServices));
      }),
    );
    renderWithRouter(<Infrastructure />);
    await waitFor(() => {
      expect(screen.getByText('gateway')).toBeInTheDocument();
    });
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /reload config/i })).not.toBeDisabled();
    });
    const countBefore = fetchCount;
    await userEvent.click(screen.getByRole('button', { name: /reload config/i }));
    await waitFor(() => {
      expect(fetchCount).toBeGreaterThan(countBefore);
      expect(screen.getByRole('button', { name: /reload config/i })).toBeDisabled();
    });
    releaseRefreshes.forEach((release) => release());
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /reload config/i })).not.toBeDisabled();
    });
  });
});
