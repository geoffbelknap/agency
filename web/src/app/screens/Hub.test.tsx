import { describe, it, expect } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { server } from '../../test/server';
import { renderWithRouter } from '../../test/render';
import { Hub } from './Hub';

const BASE = 'http://localhost:8200/api/v1';

describe('Hub', () => {
  it('renders installed packages from the V2 package registry', async () => {
    server.use(
      http.get(`${BASE}/packages`, () =>
        HttpResponse.json({
          packages: [
            {
              kind: 'connector',
              name: 'google-drive-admin',
              version: '1.0.0',
              trust: 'official',
              path: '/tmp/.agency/packages/connector/google-drive-admin.json',
              assurance: ['publisher_verified', 'ask_partial'],
            },
          ],
        }),
      ),
      http.get(`${BASE}/instances`, () => HttpResponse.json({ instances: [] })),
    );

    renderWithRouter(<Hub />);

    await waitFor(() => {
      expect(screen.getByText('google-drive-admin')).toBeInTheDocument();
      expect(screen.getByText('Installed packages')).toBeInTheDocument();
      expect(screen.getByText('Ready to instantiate')).toBeInTheDocument();
    });
  });

  it('creates an instance from a package and shows it in the instances tab', async () => {
    let instances = [
      {
        id: 'inst_drive',
        name: 'drive-admin',
        source: { package: { kind: 'connector', name: 'google-drive-admin', version: '1.0.0' } },
        nodes: [{ id: 'drive_admin', kind: 'connector.authority' }],
        grants: [],
        created_at: '2026-04-12T18:00:00Z',
        updated_at: '2026-04-12T18:00:00Z',
      },
    ];

    server.use(
      http.get(`${BASE}/packages`, () =>
        HttpResponse.json({
          packages: [
            {
              kind: 'connector',
              name: 'google-drive-admin',
              version: '1.0.0',
              trust: 'official',
              path: '/tmp/.agency/packages/connector/google-drive-admin.json',
              assurance: ['publisher_verified', 'ask_partial'],
            },
          ],
        }),
      ),
      http.get(`${BASE}/instances`, () => HttpResponse.json({ instances })),
      http.post(`${BASE}/instances/from-package`, async ({ request }) => {
        const body = await request.json() as Record<string, string>;
        const created = {
          id: 'inst_new',
          name: body.instance_name || 'google-drive-admin',
          source: { package: { kind: 'connector', name: 'google-drive-admin', version: '1.0.0' } },
          nodes: [{ id: 'google_drive_admin', kind: 'connector.authority' }],
          grants: [],
          created_at: '2026-04-12T18:15:00Z',
          updated_at: '2026-04-12T18:15:00Z',
        };
        instances = [created, ...instances];
        return HttpResponse.json(created, { status: 201 });
      }),
      http.get(`${BASE}/instances/inst_new`, () =>
        HttpResponse.json(instances.find((instance) => instance.id === 'inst_new')),
      ),
    );

    renderWithRouter(<Hub />);

    await waitFor(() => {
      expect(screen.getByText('google-drive-admin')).toBeInTheDocument();
    });

    await userEvent.type(screen.getByLabelText('Instance name for google-drive-admin'), 'drive-ops');
    await userEvent.click(screen.getByRole('button', { name: /create instance/i }));
    await userEvent.click(screen.getByRole('tab', { name: /instances/i }));

    await waitFor(() => {
      expect(screen.getAllByText('drive-ops').length).toBeGreaterThan(0);
      expect(screen.getAllByText(/google-drive-admin/).length).toBeGreaterThan(0);
    });
  });

  it('validates and applies an instance through the V2 control loop', async () => {
    server.use(
      http.get(`${BASE}/packages`, () => HttpResponse.json({ packages: [] })),
      http.get(`${BASE}/instances`, () =>
        HttpResponse.json({
          instances: [
            {
              id: 'inst_drive',
              name: 'drive-admin',
              source: { package: { kind: 'connector', name: 'google-drive-admin', version: '1.0.0' } },
              nodes: [{ id: 'drive_admin', kind: 'connector.authority' }],
              grants: [{ principal: 'agent:community-admin/coordinator', action: 'add_viewer' }],
              created_at: '2026-04-12T18:00:00Z',
              updated_at: '2026-04-12T18:00:00Z',
            },
          ],
        }),
      ),
      http.get(`${BASE}/instances/inst_drive`, () =>
        HttpResponse.json({
          id: 'inst_drive',
          name: 'drive-admin',
          source: { package: { kind: 'connector', name: 'google-drive-admin', version: '1.0.0' } },
          nodes: [{ id: 'drive_admin', kind: 'connector.authority' }],
          grants: [{ principal: 'agent:community-admin/coordinator', action: 'add_viewer' }],
          created_at: '2026-04-12T18:00:00Z',
          updated_at: '2026-04-12T18:00:00Z',
        }),
      ),
      http.post(`${BASE}/instances/inst_drive/validate`, () => HttpResponse.json({ status: 'valid' })),
      http.post(`${BASE}/instances/inst_drive/apply`, () =>
        HttpResponse.json({
          status: 'applied',
          instance: {
            id: 'inst_drive',
            name: 'drive-admin',
            source: { package: { kind: 'connector', name: 'google-drive-admin', version: '1.0.0' } },
            nodes: [{ id: 'drive_admin', kind: 'connector.authority' }],
            grants: [{ principal: 'agent:community-admin/coordinator', action: 'add_viewer' }],
            created_at: '2026-04-12T18:00:00Z',
            updated_at: '2026-04-12T18:05:00Z',
          },
          nodes: [{ node_id: 'drive_admin', state: 'running' }],
        }),
      ),
    );

    renderWithRouter(<Hub />);
    await userEvent.click(screen.getByRole('tab', { name: /instances/i }));

    await waitFor(() => {
      expect(screen.getAllByText('drive-admin').length).toBeGreaterThan(0);
    });

    await userEvent.click(screen.getByRole('button', { name: /^validate$/i }));
    await userEvent.click(screen.getByRole('button', { name: /^apply$/i }));

    await waitFor(() => {
      expect(screen.getByText('Last apply reconciled 1 runtime node(s).')).toBeInTheDocument();
      expect(screen.getByText('add_viewer')).toBeInTheDocument();
    });
  });

  it('shows assurance state and disables instance creation when policy is not met', async () => {
    server.use(
      http.get(`${BASE}/packages`, () =>
        HttpResponse.json({
          packages: [
            {
              kind: 'connector',
              name: 'google-drive-admin',
              version: '1.0.0',
              trust: 'verified',
              publisher: 'example-publisher',
              path: '/tmp/.agency/packages/connector/google-drive-admin.json',
              assurance: ['publisher_verified'],
            },
          ],
        }),
      ),
      http.get(`${BASE}/instances`, () => HttpResponse.json({ instances: [] })),
    );

    renderWithRouter(<Hub />);

    await waitFor(() => {
      expect(screen.getByText('More assurance required')).toBeInTheDocument();
      expect(screen.getByText(/This package cannot be instantiated yet because it does not meet the local assurance policy./i)).toBeInTheDocument();
      expect(screen.getByText(/Assurance:/i)).toBeInTheDocument();
      expect(screen.getByText(/publisher_verified/i)).toBeInTheDocument();
    });

    expect(screen.getByRole('button', { name: /create instance/i })).toBeDisabled();
  });
});
