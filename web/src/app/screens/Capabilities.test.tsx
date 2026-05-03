import { describe, it, expect } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { server } from '../../test/server';
import { renderWithRouter } from '../../test/render';
import { Capabilities } from './Capabilities';

const BASE = 'http://localhost:8200/api/v1';

describe('Capabilities', () => {
  it('renders the redesign capability surface from API data', async () => {
    server.use(
      http.get(`${BASE}/admin/capabilities`, () =>
        HttpResponse.json([
          { name: 'fs.read', kind: 'tool', state: 'available', agents: [], spec: { risk: 'low', scope: 'any path' } },
          { name: 'shell.exec', kind: 'tool', state: 'restricted', agents: ['henry'], spec: { risk: 'high', scope: 'whitelist (8 cmds)' } },
        ]),
      ),
    );

    renderWithRouter(<Capabilities />);

    await waitFor(() => {
      expect(screen.getByText('Total')).toBeInTheDocument();
      expect(screen.getByText('Enabled')).toBeInTheDocument();
      expect(screen.getByRole('button', { name: /explain selected/i })).toBeInTheDocument();
      expect(screen.getByText('Action')).toBeInTheDocument();
      expect(screen.getByText('From')).toBeInTheDocument();
      expect(screen.getByText('Risk')).toBeInTheDocument();
      expect(screen.getByText('Scope')).toBeInTheDocument();
      expect(screen.getByText('Used by')).toBeInTheDocument();
      expect(screen.getByText('fs.read')).toBeInTheDocument();
      expect(screen.getByText('shell.exec')).toBeInTheDocument();
      expect(screen.getByText('whitelist (8 cmds)')).toBeInTheDocument();
      expect(screen.getByText('1 agents')).toBeInTheDocument();
    });
  });

  it('renders provider tools as grantable capability rows', async () => {
    server.use(
      http.get(`${BASE}/admin/capabilities`, () =>
        HttpResponse.json([
          {
            name: 'provider-web-search',
            kind: 'provider-tool',
            state: 'disabled',
            agents: [],
            description: 'Provider-side web search or search grounding.',
            spec: { risk: 'medium', execution: 'provider_hosted' },
          },
        ]),
      ),
    );

    renderWithRouter(<Capabilities />, { route: '/admin/capabilities' });

    await waitFor(() => {
      expect(screen.getByText('provider-web-search')).toBeInTheDocument();
      expect(screen.getByText('provider')).toBeInTheDocument();
      expect(screen.getByText('medium')).toBeInTheDocument();
      expect(screen.getByText('provider hosted')).toBeInTheDocument();
      expect(screen.getByRole('button', { name: /enable provider-web-search/i })).toBeInTheDocument();
    });
  });

  it('keeps provider tools visible when the registry has not exposed them yet', async () => {
    server.use(
      http.get(`${BASE}/admin/capabilities`, () => HttpResponse.json([])),
      http.get(`${BASE}/infra/provider-tools`, () =>
        HttpResponse.json({
          version: '0.1',
          capabilities: {
            'provider-web-search': {
              title: 'Web search',
              risk: 'medium',
              default_grant: false,
              execution: 'provider_hosted',
              description: 'Provider-side web search or search grounding.',
            },
          },
        }),
      ),
    );

    renderWithRouter(<Capabilities />, { route: '/admin/capabilities' });

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /provider tools 1/i })).toBeInTheDocument();
      expect(screen.getByText('provider-web-search')).toBeInTheDocument();
      expect(screen.getByText('provider')).toBeInTheDocument();
      expect(screen.getByText('medium')).toBeInTheDocument();
      expect(screen.getByText('provider hosted')).toBeInTheDocument();
    });
  });

  it('reflects enable state for provider tools loaded from fallback inventory', async () => {
    server.use(
      http.get(`${BASE}/admin/capabilities`, () => HttpResponse.json([])),
      http.get(`${BASE}/infra/provider-tools`, () =>
        HttpResponse.json({
          version: '0.1',
          capabilities: {
            'provider-web-search': {
              title: 'Web search',
              risk: 'medium',
              default_grant: false,
              execution: 'provider_hosted',
              description: 'Provider-side web search or search grounding.',
            },
          },
        }),
      ),
      http.get(`${BASE}/agents`, () => HttpResponse.json([])),
      http.post(`${BASE}/admin/capabilities/:name/enable`, () => HttpResponse.json({ ok: true })),
    );

    renderWithRouter(<Capabilities />, { route: '/admin/capabilities' });

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /enable provider-web-search/i })).toBeInTheDocument();
    });
    await userEvent.click(screen.getByRole('button', { name: /enable provider-web-search/i }));
    await waitFor(() => {
      expect(screen.getByText('Enable provider-web-search')).toBeInTheDocument();
    });
    await userEvent.click(screen.getByRole('button', { name: /^enable$/i }));
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /disable provider-web-search/i })).toBeInTheDocument();
    });
  });

  it('opens configure flow from the compact enable toggle', async () => {
    server.use(
      http.get(`${BASE}/admin/capabilities`, () =>
        HttpResponse.json([
          { name: 'web-search', kind: 'service', state: 'disabled', agents: [] },
        ]),
      ),
      http.get(`${BASE}/agents`, () => HttpResponse.json([{ name: 'henry', status: 'running' }])),
      http.post(`${BASE}/admin/capabilities/:name/enable`, () => HttpResponse.json({ ok: true })),
    );

    renderWithRouter(<Capabilities />);

    await waitFor(() => {
      expect(screen.getByText('web-search')).toBeInTheDocument();
    });
    await userEvent.click(screen.getByRole('button', { name: /enable web-search/i }));
    await waitFor(() => {
      expect(screen.getByText('Enable web-search')).toBeInTheDocument();
      expect(screen.getByText('henry')).toBeInTheDocument();
    });
    await userEvent.click(screen.getByRole('button', { name: /^enable$/i }));
    await waitFor(() => {
      expect(screen.queryByText(/failed to enable/i)).not.toBeInTheDocument();
    });
  });

  it('disables an active capability from the compact toggle', async () => {
    server.use(
      http.get(`${BASE}/admin/capabilities`, () =>
        HttpResponse.json([
          { name: 'http.get', kind: 'tool', state: 'available', agents: [] },
        ]),
      ),
      http.post(`${BASE}/admin/capabilities/:name/disable`, () => HttpResponse.json({ ok: true })),
    );

    renderWithRouter(<Capabilities />);

    await waitFor(() => {
      expect(screen.getByText('http.get')).toBeInTheDocument();
    });
    await userEvent.click(screen.getByRole('button', { name: /disable http.get/i }));
    await waitFor(() => {
      expect(screen.queryByText(/failed to disable/i)).not.toBeInTheDocument();
    });
  });

  it('shows the compact empty state', async () => {
    server.use(http.get(`${BASE}/admin/capabilities`, () => HttpResponse.json([])));

    renderWithRouter(<Capabilities />, { route: '/admin/capabilities' });

    await waitFor(() => {
      expect(screen.getByText('No entries found.')).toBeInTheDocument();
      expect(screen.getByText('0%')).toBeInTheDocument();
    });
  });
});
