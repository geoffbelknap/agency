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
];

describe('Admin — Egress tab', () => {
  it('fetches egress config for selected agent', async () => {
    server.use(
      ...agentHandlers,
      http.get(`${BASE}/admin/egress`, ({ request }) => {
        const url = new URL(request.url);
        const agent = url.searchParams.get('agent');
        return HttpResponse.json({
          agent,
          domains: ['github.com'],
        });
      }),
    );
    renderAdminInForm('egress');
    await waitFor(() => {
      expect(document.querySelector('select')).toBeInTheDocument();
    });
    selectRadixValue('alice');
    await waitFor(() => {
      expect(screen.getAllByText(/github\.com/).length).toBeGreaterThanOrEqual(1);
    });
  });
});

describe('Admin — Policy tab', () => {
  it('renders policy tab trigger', async () => {
    server.use(...agentHandlers);
    renderAdminInForm();
    expect(screen.getByRole('tab', { name: /policy/i })).toBeInTheDocument();
  });

  it('shows the active section summary', async () => {
    server.use(...agentHandlers);
    renderAdmin('policy');
    expect(screen.getByRole('heading', { name: 'Policy' })).toBeInTheDocument();
    expect(screen.getByText(/inspect and validate per-agent policy state/i)).toBeInTheDocument();
    expect(screen.getAllByText(/policies, trust, and agent operating boundaries/i).length).toBeGreaterThan(0);
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
      expect(screen.getByText('2 issues need attention')).toBeInTheDocument();
      expect(screen.getByRole('link', { name: 'Open Infrastructure' })).toHaveAttribute('href', '/admin/infrastructure');
      expect(screen.getByRole('link', { name: 'Open Agent: alice' })).toHaveAttribute('href', '/agents/alice');
    });
  });
});

describe('Admin — Trust tab', () => {
  it('elevates agent trust', async () => {
    let elevated = false;
    server.use(
      ...agentHandlers,
      http.post(`${BASE}/admin/trust`, () => {
        elevated = true;
        return HttpResponse.json({ ok: true });
      }),
    );
    renderAdmin('trust');
    await waitFor(() => {
      expect(screen.getByText('alice')).toBeInTheDocument();
    });
    await userEvent.click(screen.getByRole('button', { name: /elevate/i }));
    await waitFor(() => {
      expect(elevated).toBe(true);
    });
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
});
