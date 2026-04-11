import { describe, it, expect } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { server } from '../../test/server';
import { renderWithRouter } from '../../test/render';
import { Profiles } from './Profiles';

const BASE = 'http://localhost:8200/api/v1';

describe('Profiles', () => {
  it('shows profile guidance and empty-detail help', async () => {
    server.use(
      http.get(`${BASE}/admin/profiles`, () =>
        HttpResponse.json([
          { id: 'operator', type: 'operator', display_name: 'Operator', created_at: '2026-04-08T00:00:00Z' },
        ]),
      ),
    );

    renderWithRouter(<Profiles />, { route: '/profiles' });

    await waitFor(() => {
      expect(screen.getByText(/use profiles to separate operator identity from agent identity/i)).toBeInTheDocument();
      expect(screen.getByRole('link', { name: 'Open Channels' })).toHaveAttribute('href', '/channels');
      expect(screen.getByRole('link', { name: 'Open Agents' })).toHaveAttribute('href', '/agents');
      expect(screen.getByText(/select a profile to view details/i)).toBeInTheDocument();
      expect(screen.getByText(/start with the operator profile you use most often/i)).toBeInTheDocument();
    });
  });
});
