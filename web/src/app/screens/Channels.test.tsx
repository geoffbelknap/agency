// src/app/screens/Channels.test.tsx
import { describe, it, expect, beforeAll, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { server } from '../../test/server';
import { renderWithRouter } from '../../test/render';
import { Channels } from './Channels';

vi.mock('../lib/ws', () => ({
  socket: { on: () => () => {}, connect: () => {}, disconnect: () => {}, connected: false, onConnectionChange: () => () => {}, gaveUp: false },
}));


beforeAll(() => {
  window.HTMLElement.prototype.scrollIntoView = () => {};
});

const BASE = 'http://localhost:8200/api/v1';
const operatorProfilesHandler = http.get(`${BASE}/admin/profiles`, ({ request }) => {
  const url = new URL(request.url);
  if (url.searchParams.get('type') === 'operator') {
    return HttpResponse.json([]);
  }
  return HttpResponse.json([]);
});

describe('Channels', () => {
  it('renders channel list and auto-selects first', async () => {
    server.use(
      operatorProfilesHandler,
      http.get(`${BASE}/comms/channels`, () =>
        HttpResponse.json([
          { name: 'general', topic: 'General chat' },
          { name: 'ops', topic: 'Operations' },
        ]),
      ),
      http.get(`${BASE}/comms/channels/general/messages`, () =>
        HttpResponse.json([
          { id: 'm1', author: 'steve', content: 'Hello world', timestamp: '2026-03-16T10:00:00Z' },
        ]),
      ),
      http.get(`${BASE}/agents`, () => HttpResponse.json([])),
    );
    renderWithRouter(<Channels />);
    await waitFor(() => {
      expect(screen.getAllByText('general').length).toBeGreaterThanOrEqual(1);
      expect(screen.getByText('ops')).toBeInTheDocument();
      expect(screen.getByText('Hello world')).toBeInTheDocument();
    });
  });

  it('hides archived channels from the default sidebar', async () => {
    server.use(
      operatorProfilesHandler,
      http.get(`${BASE}/comms/channels`, () =>
        HttpResponse.json([
          { name: 'general', topic: 'General chat', state: 'active' },
          { name: 'playwright-old', topic: 'Archived test channel', state: 'archived' },
        ]),
      ),
      http.get(`${BASE}/comms/channels/general/messages`, () =>
        HttpResponse.json([
          { id: 'm1', author: 'steve', content: 'Hello world', timestamp: '2026-03-16T10:00:00Z' },
        ]),
      ),
      http.get(`${BASE}/agents`, () => HttpResponse.json([])),
    );
    renderWithRouter(<Channels />);
    await waitFor(() => {
      expect(screen.getAllByText('general').length).toBeGreaterThanOrEqual(1);
      expect(screen.queryByText('playwright-old')).not.toBeInTheDocument();
    });
  });

  it('marks DM targets without a live backing agent as unavailable', async () => {
    let channelListCalls = 0;
    server.use(
      operatorProfilesHandler,
      http.get(`${BASE}/comms/channels`, () => {
        channelListCalls += 1;
        if (channelListCalls > 1) {
          return HttpResponse.json([
            { name: 'general', topic: 'General chat', state: 'active' },
            {
              name: 'dm-retired-agent',
              topic: 'Legacy DM',
              type: 'dm',
              state: 'active',
              availability: 'unavailable',
            },
          ]);
        }
        return HttpResponse.json([
          { name: 'general', topic: 'General chat', state: 'active' },
        ]);
      }),
      http.get(`${BASE}/comms/channels/general/messages`, () =>
        HttpResponse.json([
          { id: 'm1', author: 'steve', content: 'Hello world', timestamp: '2026-03-16T10:00:00Z' },
        ]),
      ),
      http.get(`${BASE}/agents`, () =>
        HttpResponse.json([
          { name: 'alice', status: 'running' },
        ]),
      ),
    );

    renderWithRouter(<Channels />);
    await waitFor(() => {
      expect(screen.getAllByText('general').length).toBeGreaterThanOrEqual(1);
      expect(screen.queryByText('UNAVAILABLE')).not.toBeInTheDocument();
    });

    await userEvent.click(screen.getByRole('button', { name: /show inactive/i }));

    await waitFor(() => {
      expect(screen.getByText('retired-agent')).toBeInTheDocument();
      expect(screen.getByLabelText('Unavailable')).toBeInTheDocument();
    });
  });

  it('sends a message', async () => {
    let sent = false;
    server.use(
      operatorProfilesHandler,
      http.get(`${BASE}/comms/channels`, () =>
        HttpResponse.json([{ name: 'general' }]),
      ),
      http.get(`${BASE}/comms/channels/general/messages`, () => HttpResponse.json([])),
      http.post(`${BASE}/comms/channels/general/messages`, () => {
        sent = true;
        return HttpResponse.json({ ok: true });
      }),
      http.get(`${BASE}/agents`, () => HttpResponse.json([])),
    );
    renderWithRouter(<Channels />);
    await waitFor(() => {
      expect(screen.getAllByText('general').length).toBeGreaterThanOrEqual(1);
    });
    const input = screen.getByPlaceholderText(/message #general/i);
    await userEvent.type(input, 'Test message{Enter}');
    await waitFor(() => {
      expect(sent).toBe(true);
    });
  });

  it('sends message via send button click', async () => {
    let sent = false;
    server.use(
      operatorProfilesHandler,
      http.get(`${BASE}/comms/channels`, () =>
        HttpResponse.json([{ name: 'general' }]),
      ),
      http.get(`${BASE}/comms/channels/general/messages`, () => HttpResponse.json([])),
      http.post(`${BASE}/comms/channels/general/messages`, () => {
        sent = true;
        return HttpResponse.json({ ok: true });
      }),
      http.get(`${BASE}/agents`, () => HttpResponse.json([])),
    );
    renderWithRouter(<Channels />);
    await waitFor(() => {
      expect(screen.getAllByText('general').length).toBeGreaterThanOrEqual(1);
    });
    const input = screen.getByPlaceholderText(/message #general/i);
    await userEvent.type(input, 'Hello from button');
    const buttons = screen.getAllByRole('button');
    await userEvent.click(buttons[buttons.length - 1]);
    await waitFor(() => {
      expect(sent).toBe(true);
    });
    expect(input).toHaveValue('');
  });

  it('shows a core-first empty state when no channels exist', async () => {
    server.use(
      operatorProfilesHandler,
      http.get(`${BASE}/comms/channels`, () => HttpResponse.json([])),
      http.get(`${BASE}/agents`, () => HttpResponse.json([])),
    );

    renderWithRouter(<Channels />);

    await waitFor(() => {
      expect(screen.getByText('No channels yet')).toBeInTheDocument();
      expect(screen.getByRole('button', { name: 'Create channel' })).toBeInTheDocument();
      expect(screen.getByRole('link', { name: 'Open Agents' })).toHaveAttribute('href', '/agents');
    });
  });
});
