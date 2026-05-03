import { describe, it, expect } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { server } from '../../test/server';
import { renderWithRouter } from '../../test/render';
import { Presets } from './Presets';

const BASE = 'http://localhost:8200/api/v1';

describe('Presets', () => {
  it('renders the redesign preset catalog from API data', async () => {
    server.use(
      http.get(`${BASE}/hub/presets`, () =>
        HttpResponse.json([
          { name: 'generalist', description: 'Broad assistant', type: 'standard', source: 'built-in' },
          { name: 'researcher', description: 'Read-heavy work', type: 'standard', source: 'user' },
        ]),
      ),
      http.get(`${BASE}/agents`, () =>
        HttpResponse.json([
          { name: 'henry', status: 'running', preset: 'generalist' },
          { name: 'jules', status: 'running', preset: 'researcher' },
        ]),
      ),
      http.get(`${BASE}/hub/presets/generalist`, () =>
        HttpResponse.json({
          name: 'generalist',
          description: 'Broad assistant',
          type: 'standard',
          source: 'built-in',
          identity: { purpose: 'Broad work', body: 'Assist broadly.' },
          tools: ['python3'],
        }),
      ),
    );

    renderWithRouter(<Presets />, { route: '/admin/presets' });

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /new preset/i })).toBeInTheDocument();
      expect(screen.getAllByText('generalist').length).toBeGreaterThan(0);
      expect(screen.getByText('researcher')).toBeInTheDocument();
      expect(screen.getByText('Total')).toBeInTheDocument();
      expect(screen.getByText('Built-in')).toBeInTheDocument();
      expect(screen.getByText('Custom')).toBeInTheDocument();
      expect(screen.getByText('Assigned')).toBeInTheDocument();
      expect(screen.getAllByText('1 agent')).toHaveLength(2);
      expect(screen.queryByText(/active assignments/i)).not.toBeInTheDocument();
    });

    await userEvent.click(screen.getAllByRole('button', { name: /edit copy/i })[0]);

    expect(await screen.findByDisplayValue('generalist-custom')).toBeInTheDocument();
    expect(screen.getByDisplayValue('Broad assistant')).toBeInTheDocument();
    expect(screen.getByDisplayValue('Assist broadly.')).toBeInTheDocument();
    expect(screen.queryByText(/hard limits/i)).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /add limit/i })).not.toBeInTheDocument();
  });

  it('opens the compact editor for a new preset', async () => {
    server.use(http.get(`${BASE}/hub/presets`, () => HttpResponse.json([])));

    renderWithRouter(<Presets />, { route: '/admin/presets' });

    await userEvent.click(await screen.findByRole('button', { name: /new preset/i }));

    expect(screen.getAllByText('New preset').length).toBeGreaterThan(0);
    expect(screen.getByLabelText('Name')).toBeInTheDocument();
    expect(screen.getByLabelText('Type')).toBeInTheDocument();
    expect(screen.getByLabelText('Description')).toBeInTheDocument();
    expect(screen.getByLabelText('Identity prompt')).toBeInTheDocument();
  });
});
