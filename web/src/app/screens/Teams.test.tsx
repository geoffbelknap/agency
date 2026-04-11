import { describe, it, expect } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { server } from '../../test/server';
import { renderWithRouter } from '../../test/render';
import { Teams } from './Teams';

const BASE = 'http://localhost:8200/api/v1';

describe('Teams', () => {
  it('shows operator guidance and next-step links', async () => {
    server.use(
      http.get(`${BASE}/admin/teams`, () => HttpResponse.json([])),
    );

    renderWithRouter(<Teams />, { route: '/teams' });

    await waitFor(() => {
      expect(screen.getByText(/use teams for shared ownership, not just naming/i)).toBeInTheDocument();
      expect(screen.getByRole('link', { name: 'Open Agents' })).toHaveAttribute('href', '/agents');
      expect(screen.getByRole('link', { name: 'Open Missions' })).toHaveAttribute('href', '/missions');
      expect(screen.getByText(/create a team only when multiple agents should share ownership or mission context/i)).toBeInTheDocument();
    });
  });

  it('renders teams from API', async () => {
    server.use(
      http.get(`${BASE}/admin/teams`, () =>
        HttpResponse.json([
          { name: 'alpha-team', member_count: 2, created: '2026-04-08T00:00:00Z' },
        ]),
      ),
    );

    renderWithRouter(<Teams />);

    await waitFor(() => {
      expect(screen.getByText('alpha-team')).toBeInTheDocument();
      expect(screen.getByText('2 members')).toBeInTheDocument();
    });
  });

  it('confirms delete and removes the team from the list', async () => {
    server.use(
      http.get(`${BASE}/admin/teams`, () =>
        HttpResponse.json([
          { name: 'alpha-team', member_count: 2, created: '2026-04-08T00:00:00Z' },
        ]),
      ),
      http.delete(`${BASE}/admin/teams/:name`, () => HttpResponse.json({ ok: true })),
    );

    renderWithRouter(<Teams />);

    await waitFor(() => {
      expect(screen.getByText('alpha-team')).toBeInTheDocument();
    });

    await userEvent.click(screen.getByRole('button', { name: /delete team alpha-team/i }));

    const dialog = await screen.findByRole('alertdialog');
    expect(within(dialog).getByText(/cannot be undone/i)).toBeInTheDocument();

    await userEvent.click(within(dialog).getByRole('button', { name: /^delete$/i }));

    await waitFor(() => {
      expect(screen.queryByText('alpha-team')).not.toBeInTheDocument();
      expect(screen.getByText(/no teams yet/i)).toBeInTheDocument();
    });
  });
});
