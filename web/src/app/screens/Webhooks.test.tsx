import { describe, it, expect, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { server } from '../../test/server';
import { renderWithRouter } from '../../test/render';
import { Webhooks } from './Webhooks';

vi.mock('../lib/ws', () => ({
  socket: { on: () => () => {}, connect: () => {}, disconnect: () => {}, connected: false, onConnectionChange: () => () => {}, gaveUp: false },
}));

const BASE = 'http://localhost:8200/api/v1';

describe('Webhooks', () => {
  it('renders webhook list', async () => {
    server.use(
      http.get(`${BASE}/events/webhooks`, () =>
        HttpResponse.json([
          { name: 'alerts', event_type: 'operator_alert', url: 'https://ntfy.sh/my-alerts', created_at: '2026-03-26T10:00:00Z' },
        ]),
      ),
    );
    renderWithRouter(<Webhooks />);
    await waitFor(() => {
      expect(screen.getByText('alerts')).toBeInTheDocument();
      expect(screen.getByText('operator_alert')).toBeInTheDocument();
    });
  });

  it('shows empty state when no webhooks', async () => {
    server.use(
      http.get(`${BASE}/events/webhooks`, () => HttpResponse.json([])),
    );
    renderWithRouter(<Webhooks />);
    await waitFor(() => {
      expect(screen.getByText(/no webhooks/i)).toBeInTheDocument();
    });
  });

  it('creates a webhook', async () => {
    let created = false;
    server.use(
      http.get(`${BASE}/events/webhooks`, () => HttpResponse.json([])),
      http.post(`${BASE}/events/webhooks`, () => {
        created = true;
        return HttpResponse.json({ name: 'new-hook', event_type: 'operator_alert', url: '/events/webhook/new-hook', secret: 'sec_abc' });
      }),
    );
    renderWithRouter(<Webhooks />);
    await waitFor(() => {
      expect(screen.getByText(/no webhooks/i)).toBeInTheDocument();
    });

    await userEvent.click(screen.getByRole('button', { name: /create/i }));
    await userEvent.type(screen.getByPlaceholderText(/name/i), 'new-hook');
    await userEvent.type(screen.getByPlaceholderText(/event type/i), 'operator_alert');
    await userEvent.click(screen.getByRole('button', { name: /^create$/i }));

    await waitFor(() => {
      expect(created).toBe(true);
    });
  });
});
