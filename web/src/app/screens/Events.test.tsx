import { describe, it, expect } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { server } from '../../test/server';
import { renderWithRouter } from '../../test/render';
import { Events } from './Events';

const BASE = 'http://localhost:8200/api/v1';

describe('Events', () => {
  it('shows recovery guidance when recent warning or error events exist', async () => {
    server.use(
      http.get(`${BASE}/events`, () =>
        HttpResponse.json([
          {
            id: 'evt-1',
            source_type: 'platform',
            source_name: 'gateway',
            event_type: 'gateway_error',
            timestamp: new Date().toISOString(),
            data: { message: 'Gateway failed to start' },
            metadata: {},
          },
          {
            id: 'evt-2',
            source_type: 'connector',
            source_name: 'github',
            event_type: 'connector_warning',
            timestamp: new Date().toISOString(),
            data: { message: 'Connector credentials need review' },
            metadata: {},
          },
        ]),
      ),
      http.get(`${BASE}/events/subscriptions`, () => HttpResponse.json([])),
    );

    renderWithRouter(<Events />, { route: '/admin/events' });

    await waitFor(() => {
      expect(screen.getByText('2 recent events need attention')).toBeInTheDocument();
      expect(screen.getByRole('link', { name: 'Open Infrastructure' })).toHaveAttribute('href', '/admin/infrastructure');
      expect(screen.getByRole('link', { name: 'Open Doctor' })).toHaveAttribute('href', '/admin/doctor');
      expect(screen.getByText('gateway_error')).toBeInTheDocument();
      expect(screen.getByText('connector_warning')).toBeInTheDocument();
    });
  });

  it('shows per-event likely next steps when an event is expanded', async () => {
    server.use(
      http.get(`${BASE}/events`, () =>
        HttpResponse.json([
          {
            id: 'evt-1',
            source_type: 'connector',
            source_name: 'github',
            event_type: 'connector_warning',
            timestamp: new Date().toISOString(),
            data: { message: 'Connector credentials need review' },
            metadata: {},
          },
        ]),
      ),
      http.get(`${BASE}/events/subscriptions`, () => HttpResponse.json([])),
    );

    renderWithRouter(<Events />, { route: '/admin/events' });

    await waitFor(() => {
      expect(screen.getByText('connector_warning')).toBeInTheDocument();
    });

    await userEvent.click(screen.getByText('connector_warning'));

    await waitFor(() => {
      expect(screen.getByText('Likely next step')).toBeInTheDocument();
      expect(screen.getByText(/connector-sourced failures usually mean intake setup/i)).toBeInTheDocument();
      expect(screen.getAllByRole('link', { name: 'Open Intake' }).length).toBeGreaterThan(0);
      expect(screen.getAllByRole('link', { name: 'Open Doctor' }).length).toBeGreaterThan(0);
    });
  });
});
