import { describe, it, expect } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { server } from '../../test/server';
import { renderWithRouter } from '../../test/render';
import { Teams } from './Teams';

const BASE = 'http://localhost:8200/api/v1';

describe('Teams', () => {
  it('renders teams from API', async () => {
    server.use(
      http.get(`${BASE}/admin/teams`, () =>
        HttpResponse.json([
          { name: 'alpha', member_count: 3, created: '2026-03-15' },
        ]),
      ),
    );
    renderWithRouter(<Teams />);
    await waitFor(() => {
      expect(screen.getByText('alpha')).toBeInTheDocument();
      expect(screen.getByText('3 members')).toBeInTheDocument();
    });
  });

  it('creates a new team', async () => {
    let created = false;
    server.use(
      http.get(`${BASE}/admin/teams`, () => HttpResponse.json([])),
      http.post(`${BASE}/admin/teams`, () => {
        created = true;
        return HttpResponse.json({ ok: true });
      }),
    );
    renderWithRouter(<Teams />);
    await userEvent.click(screen.getByRole('button', { name: /create team/i }));
    const input = screen.getByPlaceholderText('Team name...');
    await userEvent.type(input, 'beta');
    await userEvent.click(screen.getByRole('button', { name: /^create$/i }));
    await waitFor(() => {
      expect(created).toBe(true);
    });
  });

  it('shows team detail and members on click', async () => {
    server.use(
      http.get(`${BASE}/admin/teams`, () =>
        HttpResponse.json([{ name: 'alpha', member_count: 2, created: '2026-03-15' }]),
      ),
      http.get(`${BASE}/admin/teams/alpha`, () =>
        HttpResponse.json({ name: 'alpha', members: ['agent-a', 'agent-b'] }),
      ),
      http.get(`${BASE}/teams/alpha/activity`, () => HttpResponse.json([])),
    );
    renderWithRouter(<Teams />);
    await waitFor(() => {
      expect(screen.getByText('alpha')).toBeInTheDocument();
    });
    await userEvent.click(screen.getByText('alpha'));
    await waitFor(() => {
      expect(screen.getByText('agent-a')).toBeInTheDocument();
      expect(screen.getByText('agent-b')).toBeInTheDocument();
    });
  });
});
