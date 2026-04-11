import { describe, it, expect } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { server } from '../../test/server';
import { renderWithRouter } from '../../test/render';
import { Presets } from './Presets';

const BASE = 'http://localhost:8200/api/v1';

describe('Presets', () => {
  it('shows operator guidance and cross-links before a preset is selected', async () => {
    server.use(
      http.get(`${BASE}/hub/presets`, () =>
        HttpResponse.json([
          { name: 'generalist', description: 'Broad assistant', type: 'standard', source: 'built-in' },
        ]),
      ),
    );

    renderWithRouter(<Presets />, { route: '/admin/presets' });

    await waitFor(() => {
      expect(screen.getByText(/use presets to standardize agent behavior before enabling tools/i)).toBeInTheDocument();
      expect(screen.getByRole('link', { name: 'Open Capabilities' })).toHaveAttribute('href', '/admin/capabilities');
      expect(screen.getByRole('link', { name: 'Open Agents' })).toHaveAttribute('href', '/agents');
      expect(screen.getByText(/select a preset to review before creating a custom one/i)).toBeInTheDocument();
      expect(screen.getByText(/built-in presets are the fastest path for alpha users/i)).toBeInTheDocument();
    });
  });
});
