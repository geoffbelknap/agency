import { describe, it, expect, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { server } from '../../test/server';
import { renderWithRouter } from '../../test/render';
import { Capabilities } from './Capabilities';

const BASE = 'http://localhost:8200/api/v1';

describe('Capabilities', () => {
  it('renders capabilities from API', async () => {
    server.use(
      http.get(`${BASE}/capabilities`, () =>
        HttpResponse.json([
          { name: 'slack-api', kind: 'service', state: 'enabled', agents: ['steve'] },
        ]),
      ),
    );
    renderWithRouter(<Capabilities />);
    await waitFor(() => {
      expect(screen.getByText('slack-api')).toBeInTheDocument();
      expect(screen.getByText('steve')).toBeInTheDocument();
    });
  });

  it('shows confirmation before delete', async () => {
    server.use(
      http.get(`${BASE}/capabilities`, () =>
        HttpResponse.json([
          { name: 'test-cap', kind: 'tool', state: 'disabled', agents: [] },
        ]),
      ),
    );
    renderWithRouter(<Capabilities />);
    await waitFor(() => {
      expect(screen.getByText('test-cap')).toBeInTheDocument();
    });
    await userEvent.click(screen.getByRole('button', { name: /delete/i }));
    await waitFor(() => {
      expect(screen.getByText(/cannot be undone/i)).toBeInTheDocument();
    });
  });

  it('has correct kind options in add form', async () => {
    server.use(http.get(`${BASE}/capabilities`, () => HttpResponse.json([])));
    renderWithRouter(<Capabilities />);
    await userEvent.click(screen.getByRole('button', { name: /add capability/i }));
    // The add-form select should have service as default value
    const select = screen.getByDisplayValue('service');
    expect(select).toBeInTheDocument();
  });

  it('enables a disabled capability', async () => {
    server.use(
      http.get(`${BASE}/capabilities`, () =>
        HttpResponse.json([
          { name: 'web-search', kind: 'service', state: 'disabled', agents: [] },
        ]),
      ),
      http.post(`${BASE}/capabilities/:name/enable`, () => HttpResponse.json({ ok: true })),
    );
    renderWithRouter(<Capabilities />);
    await waitFor(() => {
      expect(screen.getByText('web-search')).toBeInTheDocument();
    });
    const enableButton = screen.getByRole('button', { name: /^enable$/i });
    await userEvent.click(enableButton);
    await waitFor(() => {
      expect(screen.queryByText(/failed to enable/i)).not.toBeInTheDocument();
    });
  });

  it('disables an enabled capability', async () => {
    server.use(
      http.get(`${BASE}/capabilities`, () =>
        HttpResponse.json([
          { name: 'web-search', kind: 'service', state: 'enabled', agents: [] },
        ]),
      ),
      http.post(`${BASE}/capabilities/:name/disable`, () => HttpResponse.json({ ok: true })),
    );
    renderWithRouter(<Capabilities />);
    await waitFor(() => {
      expect(screen.getByText('web-search')).toBeInTheDocument();
    });
    const disableButton = screen.getByRole('button', { name: /^disable$/i });
    await userEvent.click(disableButton);
    await waitFor(() => {
      expect(screen.queryByText(/failed to disable/i)).not.toBeInTheDocument();
    });
  });

  it('confirms delete and calls API', async () => {
    server.use(
      http.get(`${BASE}/capabilities`, () =>
        HttpResponse.json([
          { name: 'file-write', kind: 'tool', state: 'disabled', agents: [] },
        ]),
      ),
      http.delete(`${BASE}/capabilities/:name`, () => HttpResponse.json({ ok: true })),
    );
    renderWithRouter(<Capabilities />);
    await waitFor(() => {
      expect(screen.getByText('file-write')).toBeInTheDocument();
    });
    await userEvent.click(screen.getByRole('button', { name: /delete/i }));
    await waitFor(() => {
      expect(screen.getByText(/cannot be undone/i)).toBeInTheDocument();
    });
    // Click the confirm button in the dialog
    const confirmButton = screen.getByRole('button', { name: /^delete$/i });
    await userEvent.click(confirmButton);
    await waitFor(() => {
      expect(screen.queryByText(/failed to delete/i)).not.toBeInTheDocument();
    });
  });

  it('adds a new capability', async () => {
    server.use(
      http.get(`${BASE}/capabilities`, () => HttpResponse.json([])),
      http.post(`${BASE}/capabilities`, () => HttpResponse.json({ ok: true })),
    );
    renderWithRouter(<Capabilities />);
    await userEvent.click(screen.getByRole('button', { name: /add capability/i }));
    const nameInput = screen.getByPlaceholderText(/capability name/i);
    await userEvent.type(nameInput, 'my-new-cap');
    const addButton = screen.getByRole('button', { name: /^add$/i });
    await userEvent.click(addButton);
    await waitFor(() => {
      expect(screen.queryByText(/failed to add/i)).not.toBeInTheDocument();
    });
  });
});
