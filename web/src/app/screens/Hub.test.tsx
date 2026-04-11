import { describe, it, expect } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { server } from '../../test/server';
import { renderWithRouter } from '../../test/render';
import { Hub } from './Hub';

const BASE = 'http://localhost:8200/api/v1';

describe('Hub', () => {
  it('renders installed components', async () => {
    server.use(
      http.get(`${BASE}/hub/instances`, () =>
        HttpResponse.json([
          { name: 'ops-pack', kind: 'pack', source: 'official', version: '1.0.0' },
        ]),
      ),
      http.get(`${BASE}/hub/search`, () => HttpResponse.json([])),
    );
    renderWithRouter(<Hub />);
    await userEvent.click(screen.getByRole('tab', { name: /installed/i }));
    await waitFor(() => {
      expect(screen.getByText('ops-pack')).toBeInTheDocument();
      expect(screen.getAllByText('Official Hub').length).toBeGreaterThan(0);
    });
  });

  it('searches hub components', async () => {
    server.use(
      http.get(`${BASE}/hub/instances`, () => HttpResponse.json([])),
      http.get(`${BASE}/hub/search`, ({ request }) => {
        const url = new URL(request.url);
        if (url.searchParams.get('q') === 'redis') {
          return HttpResponse.json([
            { name: 'redis-connector', kind: 'connector', description: 'Redis integration' },
          ]);
        }
        return HttpResponse.json([]);
      }),
    );
    renderWithRouter(<Hub />);
    const input = screen.getByPlaceholderText(/search components/i);
    await userEvent.type(input, 'redis{Enter}');
    await waitFor(() => {
      expect(screen.getByText('redis-connector')).toBeInTheDocument();
    });
  });

  it('filters every backend hub kind exposed by OCI catalog search', async () => {
    server.use(
      http.get(`${BASE}/hub/instances`, () => HttpResponse.json([])),
      http.get(`${BASE}/hub/search`, () =>
        HttpResponse.json([
          { name: 'security-ops', kind: 'pack', description: 'Pack' },
          { name: 'security-triage', kind: 'preset', description: 'Preset' },
          { name: 'limacharlie', kind: 'connector', description: 'Connector' },
          { name: 'github', kind: 'service', description: 'Service' },
          { name: 'alert-triage', kind: 'mission', description: 'Mission' },
          { name: 'code-review', kind: 'skill', description: 'Skill' },
          { name: 'default-workspace', kind: 'workspace', description: 'Workspace' },
          { name: 'approval-policy', kind: 'policy', description: 'Policy' },
          { name: 'base-ontology', kind: 'ontology', description: 'Ontology' },
          { name: 'openai', kind: 'provider', description: 'Provider' },
          { name: 'default-wizard', kind: 'setup', description: 'Setup' },
        ]),
      ),
    );

    renderWithRouter(<Hub />);

    await waitFor(() => {
      expect(screen.getByText('code-review')).toBeInTheDocument();
    });

    for (const kind of ['service', 'mission', 'ontology', 'provider', 'setup']) {
      expect(screen.getByRole('button', { name: new RegExp(`${kind}\\(1\\)`, 'i') })).toBeInTheDocument();
    }

    await userEvent.click(screen.getByRole('button', { name: /provider\(1\)/i }));
    expect(screen.getByText('openai')).toBeInTheDocument();
    expect(screen.queryByText('github')).not.toBeInTheDocument();
  });

  it('does not offer install actions for hub-managed kinds', async () => {
    server.use(
      http.get(`${BASE}/hub/instances`, () => HttpResponse.json([])),
      http.get(`${BASE}/hub/search`, () =>
        HttpResponse.json([
          { name: 'base-ontology', kind: 'ontology', description: 'Managed ontology' },
          { name: 'default-wizard', kind: 'setup', description: 'Setup config' },
          { name: 'openai', kind: 'provider', description: 'Installable provider' },
        ]),
      ),
      http.get(`${BASE}/hub/:name/info`, ({ params }) =>
        HttpResponse.json({ name: params.name, kind: params.name === 'openai' ? 'provider' : 'ontology' }),
      ),
    );

    renderWithRouter(<Hub />);

    await waitFor(() => {
      expect(screen.getByText('base-ontology')).toBeInTheDocument();
    });

    expect(screen.getByRole('button', { name: /install/i })).toBeInTheDocument();
    expect(screen.getAllByRole('button', { name: /view hub-managed info/i })).toHaveLength(2);
  });

  it('shows readable hub component provenance from the backend info route', async () => {
    server.use(
      http.get(`${BASE}/hub/instances`, () => HttpResponse.json([])),
      http.get(`${BASE}/hub/search`, () =>
        HttpResponse.json([
          { name: 'default-wizard', kind: 'setup', description: 'Setup config', source: 'default' },
        ]),
      ),
      http.get(`${BASE}/hub/:name/info`, ({ params }) =>
        HttpResponse.json({
          name: params.name,
          kind: 'setup',
          version: '1.0.0',
          description: 'Setup config',
          _source: 'default',
          _kind: 'setup',
          _path: '/tmp/agency/hub-cache/default/setup/default-wizard/setup.yaml',
        }),
      ),
    );

    renderWithRouter(<Hub />);

    await waitFor(() => {
      expect(screen.getByText('default-wizard')).toBeInTheDocument();
    });
    await userEvent.click(screen.getByRole('button', { name: /view hub-managed info/i }));

    await waitFor(() => {
      const dialog = screen.getByRole('dialog');
      expect(screen.getByText('Trust & Provenance')).toBeInTheDocument();
      expect(within(dialog).getByText('Official Hub')).toBeInTheDocument();
      expect(within(dialog).getByText('OCI content from the official Agency Hub source.')).toBeInTheDocument();
      expect(within(dialog).getByText('Hub-managed content updated through source refresh and upgrade.')).toBeInTheDocument();
      expect(within(dialog).getByText('This kind is curated through hub source sync, not direct install.')).toBeInTheDocument();
      expect(within(dialog).getByText('/tmp/agency/hub-cache/default/setup/default-wizard/setup.yaml')).toBeInTheDocument();
    });
  });

  it('shows local operator content clearly in the installed view', async () => {
    server.use(
      http.get(`${BASE}/hub/instances`, () =>
        HttpResponse.json([
          { name: 'base-pack', kind: 'pack', source: 'local', installed_at: '2026-03-10', version: '1.0.0' },
        ]),
      ),
      http.get(`${BASE}/hub/search`, () => HttpResponse.json([])),
      http.get(`${BASE}/hub/outdated`, () => HttpResponse.json([])),
    );

    renderWithRouter(<Hub />);
    await userEvent.click(screen.getByRole('tab', { name: /installed/i }));

    await waitFor(() => {
      expect(screen.getAllByText('Local Operator Content').length).toBeGreaterThan(0);
      expect(screen.getByText('Local content under direct operator control on this machine.')).toBeInTheDocument();
    });
  });

  it('summarizes installed components by source trust and management mode', async () => {
    server.use(
      http.get(`${BASE}/hub/instances`, () =>
        HttpResponse.json([
          { name: 'official-pack', kind: 'pack', source: 'official', installed_at: '2026-03-10', version: '1.0.0' },
          { name: 'local-pack', kind: 'pack', source: 'local', installed_at: '2026-03-10', version: '1.0.0' },
          { name: 'custom-policy', kind: 'policy', source: 'partner', installed_at: '2026-03-10', version: '1.0.0' },
          { name: 'base-ontology', kind: 'ontology', source: 'official', installed_at: '2026-03-10', version: '1.0.0' },
        ]),
      ),
      http.get(`${BASE}/hub/search`, () => HttpResponse.json([])),
      http.get(`${BASE}/hub/outdated`, () => HttpResponse.json([])),
    );

    renderWithRouter(<Hub />);
    await userEvent.click(screen.getByRole('tab', { name: /installed/i }));

    await waitFor(() => {
      expect(screen.getAllByText('Official Hub').length).toBeGreaterThan(0);
      expect(screen.getAllByText('Local Operator Content').length).toBeGreaterThan(0);
      expect(screen.getByText('Custom Sources')).toBeInTheDocument();
      expect(screen.getByText('Operator-Installable')).toBeInTheDocument();
      expect(screen.getByText('Hub-Managed')).toBeInTheDocument();
      expect(screen.getByText('official-pack')).toBeInTheDocument();
    });

    expect(screen.getByText('Upgradeable from the configured OCI catalog.')).toBeInTheDocument();
    expect(screen.getByText('Machine-scoped content you manage directly.')).toBeInTheDocument();
    expect(screen.getByText('Review ownership and trust before upgrading.')).toBeInTheDocument();
    expect(screen.getByText('Updated through source refresh and upgrade.')).toBeInTheDocument();
  });

  it('installs a component', async () => {
    server.use(
      http.get(`${BASE}/hub/instances`, () => HttpResponse.json([])),
      http.get(`${BASE}/hub/search`, () =>
        HttpResponse.json([
          { name: 'test-pack', kind: 'pack', description: 'Test', source: 'hub' },
        ]),
      ),
      http.post(`${BASE}/hub/install`, () => HttpResponse.json({ ok: true })),
    );
    renderWithRouter(<Hub />);
    await waitFor(() => {
      expect(screen.getByText('test-pack')).toBeInTheDocument();
    });
    const installButton = screen.getByRole('button', { name: /install/i });
    await userEvent.click(installButton);
    // No error message should appear
    await waitFor(() => {
      expect(screen.queryByText(/failed to install/i)).not.toBeInTheDocument();
    });
  });

  it('removes a component from installed tab', async () => {
    server.use(
      http.get(`${BASE}/hub/instances`, () =>
        HttpResponse.json([
          { name: 'base-pack', kind: 'pack', source: 'local', installed_at: '2026-03-10' },
        ]),
      ),
      http.get(`${BASE}/hub/search`, () => HttpResponse.json([])),
      http.post(`${BASE}/hub/remove`, () => HttpResponse.json({ ok: true })),
    );
    renderWithRouter(<Hub />);
    await userEvent.click(screen.getByRole('tab', { name: /installed/i }));
    await waitFor(() => {
      expect(screen.getByText('base-pack')).toBeInTheDocument();
    });
    const removeButton = screen.getByRole('button', { name: /remove/i });
    await userEvent.click(removeButton);
    await waitFor(() => {
      expect(screen.queryByText(/failed to remove/i)).not.toBeInTheDocument();
    });
  });

  it('deploys a pack', async () => {
    server.use(
      http.get(`${BASE}/hub/instances`, () =>
        HttpResponse.json([
          { name: 'base-pack', kind: 'pack', source: 'local', installed_at: '2026-03-10' },
        ]),
      ),
      http.get(`${BASE}/hub/search`, () => HttpResponse.json([])),
      http.post(`${BASE}/hub/deploy`, () =>
        HttpResponse.json({ agents_created: ['agent-1'] }),
      ),
    );
    renderWithRouter(<Hub />);
    await userEvent.click(screen.getByRole('tab', { name: /deploy/i }));
    await waitFor(() => {
      expect(screen.getByText(/select installed pack/i)).toBeInTheDocument();
    });
    // Select the pack from the dropdown
    const select = screen.getByRole('combobox');
    await userEvent.selectOptions(select, 'base-pack');
    const deployButton = screen.getByRole('button', { name: /^deploy$/i });
    await userEvent.click(deployButton);
    await waitFor(() => {
      expect(screen.queryByText(/failed to deploy/i)).not.toBeInTheDocument();
    });
  });

  it('shows teardown confirmation and confirms', async () => {
    server.use(
      http.get(`${BASE}/hub/instances`, () =>
        HttpResponse.json([
          { name: 'base-pack', kind: 'pack', source: 'local', installed_at: '2026-03-10' },
        ]),
      ),
      http.get(`${BASE}/hub/search`, () => HttpResponse.json([])),
      http.post(`${BASE}/hub/teardown/:pack`, () => HttpResponse.json({ ok: true })),
    );
    renderWithRouter(<Hub />);
    await userEvent.click(screen.getByRole('tab', { name: /deploy/i }));
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /teardown/i })).toBeInTheDocument();
    });
    await userEvent.click(screen.getByRole('button', { name: /teardown/i }));
    await waitFor(() => {
      expect(screen.getByText(/are you sure you want to tear down/i)).toBeInTheDocument();
    });
    // Click the confirm button in the dialog
    const confirmButton = screen.getByRole('button', { name: /^teardown$/i });
    await userEvent.click(confirmButton);
    await waitFor(() => {
      expect(screen.queryByText(/failed to teardown/i)).not.toBeInTheDocument();
    });
  });

  it('updates sources', async () => {
    server.use(
      http.get(`${BASE}/hub/instances`, () => HttpResponse.json([])),
      http.get(`${BASE}/hub/search`, () => HttpResponse.json([])),
      http.post(`${BASE}/hub/update`, () => HttpResponse.json({ ok: true })),
    );
    renderWithRouter(<Hub />);
    const updateButton = screen.getByRole('button', { name: /update sources/i });
    await userEvent.click(updateButton);
    await waitFor(() => {
      expect(screen.queryByText(/failed to update/i)).not.toBeInTheDocument();
    });
  });

  it('shows installed component upgrades and upgrades a single component', async () => {
    let upgradeBody: unknown = null;
    server.use(
      http.get(`${BASE}/hub/instances`, () =>
        HttpResponse.json([
          { name: 'base-pack', kind: 'pack', source: 'local', installed_at: '2026-03-10', version: '1.0.0' },
        ]),
      ),
      http.get(`${BASE}/hub/search`, () => HttpResponse.json([])),
      http.get(`${BASE}/hub/outdated`, () =>
        HttpResponse.json([
          {
            name: 'base-pack',
            kind: 'pack',
            installed_version: '1.0.0',
            available_version: '1.1.0',
          },
        ]),
      ),
      http.post(`${BASE}/hub/upgrade`, async ({ request }) => {
        upgradeBody = await request.json();
        return HttpResponse.json({
          components: [{ name: 'base-pack', kind: 'pack', old_version: '1.0.0', new_version: '1.1.0', status: 'upgraded' }],
        });
      }),
    );

    renderWithRouter(<Hub />);
    await userEvent.click(screen.getByRole('tab', { name: /installed/i }));

    await waitFor(() => {
      expect(screen.getByText(/1 Hub upgrade available/i)).toBeInTheDocument();
      expect(screen.getByText(/1.0.0 → 1.1.0/i)).toBeInTheDocument();
    });

    await userEvent.click(screen.getByRole('button', { name: /^upgrade$/i }));

    await waitFor(() => {
      expect(upgradeBody).toEqual({ components: ['base-pack'] });
    });
  });
});
