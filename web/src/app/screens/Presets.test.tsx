import { describe, it, expect } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { server } from '../../test/server';
import { renderWithRouter } from '../../test/render';
import { Presets } from './Presets';

const BASE = 'http://localhost:8200/api/v1';

describe('Presets', () => {
  it('renders the redesigned preset catalog from API data', async () => {
    server.use(
      http.get(`${BASE}/hub/presets`, () =>
        HttpResponse.json([
          { name: 'generalist', description: 'Broad assistant', type: 'standard', source: 'built-in' },
          { name: 'researcher', description: 'Read-heavy work', type: 'standard', source: 'user' },
        ]),
      ),
      http.get(`${BASE}/agents`, () => HttpResponse.json([{ name: 'henry', status: 'running', preset: 'generalist' }])),
    );

    renderWithRouter(<Presets />, { route: '/admin/presets' });

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /new preset/i })).toBeInTheDocument();
      expect(screen.getAllByText('generalist').length).toBeGreaterThan(0);
      expect(screen.getByText('researcher')).toBeInTheDocument();
    });
  });
});
