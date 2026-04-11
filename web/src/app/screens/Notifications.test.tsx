import { describe, it, expect, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { server } from '../../test/server';
import { renderWithRouter } from '../../test/render';
import { Notifications } from './Notifications';

vi.mock('../lib/ws', () => ({
  socket: { on: () => () => {}, connect: () => {}, disconnect: () => {}, connected: false, onConnectionChange: () => () => {}, gaveUp: false },
}));

const BASE = 'http://localhost:8200/api/v1';

describe('Notifications', () => {
  it('renders notification list', async () => {
    server.use(
      http.get(`${BASE}/events/notifications`, () =>
        HttpResponse.json([
          { name: 'alerts', type: 'ntfy', url: 'https://ntfy.sh/agency-alerts', events: ['operator_alert', 'enforcer_exited'] },
        ]),
      ),
    );
    renderWithRouter(<Notifications />);
    await waitFor(() => {
      expect(screen.getByText('alerts')).toBeInTheDocument();
      expect(screen.getByText('ntfy')).toBeInTheDocument();
    });
  });

  it('shows empty state when no notifications', async () => {
    server.use(
      http.get(`${BASE}/events/notifications`, () => HttpResponse.json([])),
    );
    renderWithRouter(<Notifications />, { route: '/admin/notifications' });
    await waitFor(() => {
      expect(screen.getByText(/no notification destinations/i)).toBeInTheDocument();
      expect(screen.getByRole('link', { name: 'Open Webhooks' })).toHaveAttribute('href', '/admin/webhooks');
      expect(screen.getByRole('link', { name: 'Review Events' })).toHaveAttribute('href', '/admin/events');
      expect(screen.getByText(/use notifications for operator alerts/i)).toBeInTheDocument();
    });
  });

  it('creates a notification destination', async () => {
    let created = false;
    server.use(
      http.get(`${BASE}/events/notifications`, () => HttpResponse.json([])),
      http.post(`${BASE}/events/notifications`, () => {
        created = true;
        return HttpResponse.json({ name: 'my-ntfy', type: 'ntfy', url: 'https://ntfy.sh/my-topic', events: ['operator_alert'] });
      }),
    );
    renderWithRouter(<Notifications />);
    await waitFor(() => {
      expect(screen.getByText(/no notification destinations/i)).toBeInTheDocument();
    });

    await userEvent.click(screen.getByRole('button', { name: /add/i }));
    await userEvent.type(screen.getByPlaceholderText(/name/i), 'my-ntfy');
    await userEvent.type(screen.getByPlaceholderText(/url/i), 'https://ntfy.sh/my-topic');
    await userEvent.click(screen.getByRole('button', { name: /^add$/i }));

    await waitFor(() => {
      expect(created).toBe(true);
    });
  });
});
