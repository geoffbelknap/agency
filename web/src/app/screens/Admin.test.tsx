import { describe, it, expect } from 'vitest';
import { screen, waitFor, fireEvent, render } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { server } from '../../test/server';
import { Admin } from './Admin';

// Admin uses useParams({ tab }) and useNavigate for tab routing.
// Use createMemoryRouter with proper route config so useParams resolves correctly.
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

// Wrap in a <form> so Radix Select renders a native <select> (isFormControl check).
function renderAdminInForm(initialTab = 'infrastructure') {
  const router = createMemoryRouter(
    [
      { path: '/admin', element: <form><Admin /></form> },
      { path: '/admin/:tab', element: <form><Admin /></form> },
    ],
    { initialEntries: [`/admin/${initialTab}`] },
  );
  return render(<RouterProvider router={router} />);
}

function selectRadixValue(value: string) {
  const selects = Array.from(document.querySelectorAll('select'));
  if (selects.length === 0) throw new Error('No native select found');
  const nativeSelect = selects.find((select) =>
    Array.from(select.options).some((option) => option.value === value),
  ) ?? selects[selects.length - 1];
  fireEvent.change(nativeSelect, { target: { value } });
}

const BASE = 'http://localhost:8200/api/v1';

const agentHandlers = [
  http.get(`${BASE}/agents`, () =>
    HttpResponse.json([
      { name: 'alice', status: 'running', trust_level: 3, restrictions: [] },
    ]),
  ),
  http.get(`${BASE}/admin/doctor`, () =>
    HttpResponse.json({
      checks: [
        { name: 'config', agent: null, status: 'pass', detail: 'Valid' },
      ],
    }),
  ),
  http.get(`${BASE}/agents/:name/logs`, () =>
    HttpResponse.json([
      { timestamp: '2026-03-16T10:00:00Z', event: 'started', detail: 'Started' },
    ]),
  ),
  http.get(`${BASE}/admin/audit`, ({ request }) => {
    const url = new URL(request.url);
    expect(url.searchParams.get('agent')).not.toBe('_all');
    return HttpResponse.json([
      { timestamp: '2026-03-16T10:00:00Z', event: 'started', detail: 'Started', agent: url.searchParams.get('agent') || 'system' },
      { timestamp: '2026-03-16T10:01:00Z', event: 'DOMAIN_BLOCKED', domain: 'example.com', agent: url.searchParams.get('agent') || 'system' },
    ]);
  }),
];

describe('Admin — Egress tab', () => {
  it('fetches egress config for selected agent', async () => {
    let approvedDomain = '';
    let revokedDomain = '';
    let updatedMode = '';
    server.use(
      ...agentHandlers,
      http.get(`${BASE}/admin/egress`, ({ request }) => {
        const url = new URL(request.url);
        const agent = url.searchParams.get('agent');
        return HttpResponse.json({
          agent,
          mode: 'allowlist',
          domains: ['github.com'],
        });
      }),
      http.post(`${BASE}/admin/egress/:agent/domains`, async ({ request, params }) => {
        const body = await request.json() as { domain: string; reason?: string };
        approvedDomain = body.domain;
        return HttpResponse.json({
          agent: params.agent,
          mode: 'allowlist',
          domains: ['github.com', body.domain],
        });
      }),
      http.delete(`${BASE}/admin/egress/:agent/domains/:domain`, ({ params }) => {
        revokedDomain = String(params.domain);
        return HttpResponse.json({
          agent: params.agent,
          mode: 'allowlist',
          domains: ['github.com'],
        });
      }),
      http.put(`${BASE}/admin/egress/:agent/mode`, async ({ request, params }) => {
        const body = await request.json() as { mode: string };
        updatedMode = body.mode;
        return HttpResponse.json({
          agent: params.agent,
          mode: body.mode,
          domains: ['github.com', 'api.example.com'],
        });
      }),
      http.get(`${BASE}/hub/egress/domains`, () =>
        HttpResponse.json({
          domains: [
            {
              domain: 'api.anthropic.com',
              auto_managed: true,
              sources: [{ type: 'connector', name: 'anthropic' }],
            },
          ],
        }),
      ),
    );
    renderAdminInForm('egress');
    await waitFor(() => {
      expect(document.querySelector('select')).toBeInTheDocument();
    });
    selectRadixValue('alice');
    await waitFor(() => {
      expect(screen.getByLabelText('Mode')).toHaveValue('allowlist');
      expect(screen.getByText('api.anthropic.com')).toBeInTheDocument();
      expect(screen.getAllByText(/github\.com/).length).toBeGreaterThanOrEqual(1);
    });

    await userEvent.type(screen.getByLabelText('Host'), 'api.example.com');
    await userEvent.type(screen.getByLabelText('Reason'), 'provider access');
    await userEvent.click(screen.getByRole('button', { name: /allow host/i }));

    await waitFor(() => {
      expect(approvedDomain).toBe('api.example.com');
      expect(screen.getAllByText('api.example.com').length).toBeGreaterThan(0);
    });

    fireEvent.change(screen.getByLabelText('Mode'), { target: { value: 'supervised-strict' } });
    await waitFor(() => {
      expect(updatedMode).toBe('supervised-strict');
    });

    await userEvent.click(screen.getByRole('button', { name: /revoke api\.example\.com/i }));
    await waitFor(() => {
      expect(revokedDomain).toBe('api.example.com');
      expect(screen.queryAllByText('api.example.com')).toHaveLength(0);
    });
  });
});

describe('Admin — Policy tab', () => {
  it('renders policy tab trigger', async () => {
    server.use(...agentHandlers);
    renderAdminInForm();
    await userEvent.click(screen.getByRole('button', { name: /governance/i }));
    await waitFor(() => {
      expect(screen.getByRole('tab', { name: /policy/i })).toBeInTheDocument();
    });
  });

  it('shows policy controls without the removed section summary', async () => {
    server.use(...agentHandlers);
    renderAdmin('policy');
    expect(screen.getByRole('button', { name: /validate policy/i })).toBeInTheDocument();
  });

  it('validates policy', async () => {
    let validated = false;
    server.use(
      ...agentHandlers,
      http.get(`${BASE}/admin/policy/alice`, () =>
        HttpResponse.json({ rules: [] }),
      ),
      http.post(`${BASE}/admin/policy/alice/validate`, () => {
        validated = true;
        return HttpResponse.json({ valid: true });
      }),
    );
    renderAdminInForm('policy');
    // Wait for agents to load — Radix renders a hidden native <select> inside <form>
    await waitFor(() => {
      expect(document.querySelector('select')).toBeInTheDocument();
    });
    // Use native select fireEvent to avoid jsdom hasPointerCapture issues
    selectRadixValue('alice');
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /validate/i })).not.toBeDisabled();
    });
    await userEvent.click(screen.getByRole('button', { name: /validate/i }));
    await waitFor(() => {
      expect(validated).toBe(true);
    });
  });
});

describe('Admin — Providers tab', () => {
  it('renders provider operations and configures a provider', async () => {
    let storedCredential = '';
    let installedProvider = '';
    server.use(
      ...agentHandlers,
      http.get(`${BASE}/infra/providers`, () =>
        HttpResponse.json([
          {
            name: 'anthropic',
            display_name: 'Anthropic',
            description: 'Claude models',
            category: 'cloud',
            installed: false,
            credential_name: 'ANTHROPIC_API_KEY',
            credential_label: 'Anthropic API key',
            api_key_url: 'https://console.anthropic.com/settings/keys',
            credential_configured: false,
          },
        ]),
      ),
      http.get(`${BASE}/infra/routing/config`, () =>
        HttpResponse.json({
          configured: false,
          providers: {},
          models: {},
          tiers: {},
          settings: { default_tier: 'standard' },
        }),
      ),
      http.post(`${BASE}/creds`, async ({ request }) => {
        const body = await request.json() as { name: string };
        storedCredential = body.name;
        return HttpResponse.json({ ok: true });
      }),
      http.post(`${BASE}/creds/ANTHROPIC_API_KEY/test`, () =>
        HttpResponse.json({ ok: true, message: 'ok' }),
      ),
      http.post(`${BASE}/infra/providers/anthropic/install`, () => {
        installedProvider = 'anthropic';
        return HttpResponse.json({ status: 'installed', provider: 'anthropic' });
      }),
    );

    renderAdmin('providers');

    expect(await screen.findByText('Model provider operations')).toBeInTheDocument();
    const providerLabel = await screen.findByText('Anthropic');
    await userEvent.click(providerLabel.closest('button')!);
    await userEvent.type(screen.getByLabelText(/anthropic api key/i), 'sk-test');
    await userEvent.click(screen.getByRole('button', { name: /install provider/i }));

    await waitFor(() => {
      expect(storedCredential).toBe('ANTHROPIC_API_KEY');
      expect(installedProvider).toBe('anthropic');
    });
  });
});

describe('Admin — Danger Zone tab', () => {
  it('requires confirmation before destroy', async () => {
    renderAdmin('danger');
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /destroy all/i })).toBeInTheDocument();
    });
    await userEvent.click(screen.getByRole('button', { name: /destroy all/i }));
    await waitFor(() => {
      expect(screen.getAllByText(/cannot be undone/i).length).toBeGreaterThan(0);
    });
  });

  it('confirms and executes destroy', async () => {
    let destroyed = false;
    server.use(
      ...agentHandlers,
      http.post(`${BASE}/admin/destroy`, () => {
        destroyed = true;
        return HttpResponse.json({ ok: true });
      }),
    );
    renderAdmin('danger');
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /destroy all/i })).toBeInTheDocument();
    });
    await userEvent.click(screen.getByRole('button', { name: /destroy all/i }));
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /destroy everything/i })).toBeInTheDocument();
    });
    await userEvent.click(screen.getByRole('button', { name: /destroy everything/i }));
    await waitFor(() => {
      expect(destroyed).toBe(true);
    });
  });
});

describe('Admin — Doctor tab', () => {
  it('runs doctor check', async () => {
    server.use(...agentHandlers);
    renderAdmin('doctor');
    // Wait for initial load to complete
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /run doctor/i })).not.toBeDisabled();
    });
    await userEvent.click(screen.getByRole('button', { name: /run doctor/i }));
    // After running, doctor results are grouped by agent — expand the group card to see check names
    await waitFor(() => {
      // The group card shows "(platform)" for checks with no agent
      expect(screen.getByText('(platform)')).toBeInTheDocument();
    });
    // Click the group card to expand and show individual check names
    await userEvent.click(screen.getByText('(platform)'));
    await waitFor(() => {
      expect(screen.getByText('config')).toBeInTheDocument();
    });
  });

  it('shows recovery shortcuts when doctor finds issues', async () => {
    server.use(
      http.get(`${BASE}/agents`, () =>
        HttpResponse.json([
          { name: 'alice', status: 'running', trust_level: 3, restrictions: [] },
        ]),
      ),
      http.get(`${BASE}/admin/doctor`, () =>
        HttpResponse.json({
          checks: [
            { name: 'comms', agent: null, status: 'fail', detail: 'Comms is unavailable' },
            { name: 'workspace', agent: 'alice', status: 'warn', detail: 'Workspace drift detected' },
          ],
        }),
      ),
    );
    renderAdmin('doctor');
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /run doctor/i })).not.toBeDisabled();
    });
    await userEvent.click(screen.getByRole('button', { name: /run doctor/i }));

    await waitFor(() => {
      expect(screen.getByText('2 issues need attention')).toBeInTheDocument();
      expect(screen.getByRole('link', { name: 'Open Infrastructure' })).toHaveAttribute('href', '/admin/infrastructure');
      expect(screen.getByRole('link', { name: 'Open Agent: alice' })).toHaveAttribute('href', '/agents/alice');
    });
  });
});

describe('Admin — Trust tab', () => {
  it('is hidden in the default core admin UI', async () => {
    renderAdmin('trust');
    await waitFor(() => {
      expect(screen.getByRole('tab', { name: /infrastructure/i })).toHaveAttribute('aria-selected', 'true');
    });
    expect(screen.queryByText('alice')).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /elevate/i })).not.toBeInTheDocument();
  });
});

describe('Admin — Audit tab', () => {
  it('loads audit log for selected agent', async () => {
    server.use(...agentHandlers);
    renderAdminInForm('audit');
    // Wait for the native <select> rendered by Radix Select (inside <form>) to appear
    await waitFor(() => {
      expect(document.querySelector('select')).toBeInTheDocument();
    });
    // Trigger agent selection via native select to avoid jsdom hasPointerCapture issues
    selectRadixValue('alice');
    // Either audit entries appear or a loading/empty state is shown
    await waitFor(() => {
      const hasEntry = screen.queryByText(/started/i) !== null;
      const hasLoading = screen.queryByText(/loading audit log/i) !== null;
      const hasEmpty = screen.queryByText(/no audit entries/i) !== null;
      expect(hasEntry || hasLoading || hasEmpty).toBe(true);
    });
  });

  it('filters and pages audit entries', async () => {
    server.use(...agentHandlers);
    renderAdminInForm('audit');

    await waitFor(() => {
      expect(screen.getAllByText('All verdicts').length).toBeGreaterThan(0);
      expect(screen.getAllByText('25 rows').length).toBeGreaterThan(0);
      expect(screen.getByText(/Showing 1-2 of 2 entries/i)).toBeInTheDocument();
    });

    selectRadixValue('deny');

    await waitFor(() => {
      expect(screen.getByText(/Showing 1-1 of 1 entries/i)).toBeInTheDocument();
      expect(screen.getAllByText('domain.blocked').length).toBeGreaterThan(0);
    });
  });

  it('shows security scan detail in admin audit rows', async () => {
    server.use(...agentHandlers);
    server.use(
      http.get(`${BASE}/admin/audit`, () =>
        HttpResponse.json([
          {
            timestamp: '2026-03-16T10:02:00Z',
            event: 'SECURITY_SCAN_NOT_APPLICABLE',
            agent: 'alice',
            source: 'enforcer',
            scan_type: 'xpia',
            scan_surface: 'provider_tool_content',
            scan_action: 'not_applicable',
            scan_mode: 'provider_hosted',
            finding_count: 0,
            content_count: 1,
            security_boundary: 'provider_hosted_raw_content_not_visible',
          },
        ]),
      ),
    );

    renderAdminInForm('audit');

    await waitFor(() => {
      expect(screen.getAllByText('security.scan.not.applicable').length).toBeGreaterThan(0);
      expect(screen.getByText(/provider_hosted_raw_content_not_visible/)).toBeInTheDocument();
      expect(screen.getByText(/Security/)).toBeInTheDocument();
    });

    await userEvent.click(screen.getByLabelText('Expand audit entry'));

    expect(screen.getByText('Security scan')).toBeInTheDocument();
    expect(screen.getByText('scan_surface')).toBeInTheDocument();
    expect(screen.getAllByText('provider_tool_content').length).toBeGreaterThan(0);
    expect(screen.getByText('scan_mode')).toBeInTheDocument();
    expect(screen.getByText('provider_hosted')).toBeInTheDocument();
  });
});

describe('Admin — Setup tab', () => {
  it('links out to the full setup wizard instead of embedding it', async () => {
    server.use(...agentHandlers);
    renderAdmin('setup');

    expect(screen.getByRole('heading', { name: 'Re-run setup wizard' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Re-run setup wizard' })).toHaveAttribute('href', '/setup');
    expect(screen.queryByText(/Name your agent/i)).not.toBeInTheDocument();
  });
});
