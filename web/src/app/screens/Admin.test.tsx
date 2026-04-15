import { describe, it, expect } from 'vitest';
import { screen, waitFor, render } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { server } from '../../test/server';
import { Admin } from './Admin';

const BASE = 'http://localhost:8200/api/v1';

function renderAdmin(initialTab = 'infrastructure') {
  const router = createMemoryRouter(
    [
      { path: '/admin', element: <Admin /> },
      { path: '/admin/:tab', element: <Admin /> },
    ],
    { initialEntries: [`/admin/${initialTab}`] },
  );
  return render(<RouterProvider router={router} />);
}

const baseHandlers = [
  http.get(`${BASE}/agents`, () => HttpResponse.json([{ name: 'alice', status: 'running' }])),
  http.get(`${BASE}/admin/doctor`, () => HttpResponse.json({ checks: [{ name: 'config', status: 'pass', detail: 'Valid' }] })),
  http.get(`${BASE}/admin/audit`, () => HttpResponse.json([{ timestamp: '2026-03-16T10:00:00Z', event: 'started', detail: 'Started', agent: 'alice' }])),
  http.get(`${BASE}/admin/egress`, () => HttpResponse.json({ agent: 'alice', mode: 'allowlist', domains: ['github.com'] })),
  http.get(`${BASE}/hub/egress/domains`, () => HttpResponse.json({ domains: [{ domain: 'provider-a.example.com', auto_managed: true }] })),
  http.get(`${BASE}/infra/providers`, () => HttpResponse.json([])),
  http.get(`${BASE}/infra/routing/config`, () => HttpResponse.json({ configured: false, providers: {}, models: {}, tiers: {} })),
  http.get(`${BASE}/admin/policy/alice`, () => HttpResponse.json({ valid: true, rules: [] })),
];

describe('Admin', () => {
  it('renders the redesigned egress governance surface', async () => {
    server.use(...baseHandlers);
    renderAdmin('egress');

    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'Egress' })).toBeInTheDocument();
      expect(screen.getByText('Define allowed outbound network destinations.')).toBeInTheDocument();
      expect(screen.getByText('github.com')).toBeInTheDocument();
    });
  });

  it('renders the capability governance tab', async () => {
    server.use(
      ...baseHandlers,
      http.get(`${BASE}/admin/capabilities`, () =>
        HttpResponse.json([
          { name: 'shell.exec', kind: 'tool', state: 'restricted', agents: ['bob'] },
        ]),
      ),
    );

    renderAdmin('capabilities');

    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'Capabilities' })).toBeInTheDocument();
      expect(screen.getByText('shell.exec')).toBeInTheDocument();
    });
  });

  it('renders audit entries', async () => {
    server.use(...baseHandlers);
    renderAdmin('audit');

    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'Audit' })).toBeInTheDocument();
      expect(screen.getByText('started')).toBeInTheDocument();
      expect(screen.getByText('Started')).toBeInTheDocument();
    });
  });
});
