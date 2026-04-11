import { describe, it, expect, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { server } from '../../test/server';
import { renderWithRouter } from '../../test/render';
import { MissionList } from './MissionList';

vi.mock('../lib/ws', () => ({ socket: { on: () => () => {} } }));

const BASE = 'http://localhost:8200/api/v1';

describe('MissionList', () => {
  it('shows an attention banner when missions are degraded or unhealthy', async () => {
    server.use(
      http.get(`${BASE}/missions`, () =>
        HttpResponse.json([
          { name: 'nightly-sync', status: 'active', description: 'Nightly sync', has_canvas: false, triggers: [] },
          { name: 'daily-triage', status: 'active', description: 'Triage queue', has_canvas: false, triggers: [] },
        ]),
      ),
      http.get(`${BASE}/missions/health`, () =>
        HttpResponse.json({
          missions: [
            { mission: 'nightly-sync', status: 'unhealthy', summary: 'Gateway is unavailable', checks: [] },
            { mission: 'daily-triage', status: 'degraded', summary: 'Paused awaiting review', checks: [] },
          ],
        }),
      ),
    );

    renderWithRouter(<MissionList />, { route: '/missions' });

    await waitFor(() => {
      expect(screen.getByText('2 missions need attention')).toBeInTheDocument();
      expect(screen.getByRole('link', { name: 'Open Doctor' })).toHaveAttribute('href', '/admin/doctor');
      expect(screen.getByRole('link', { name: 'Open Infrastructure' })).toHaveAttribute('href', '/admin/infrastructure');
      expect(screen.getByText('unhealthy: Gateway is unavailable')).toBeInTheDocument();
      expect(screen.getByText('degraded: Paused awaiting review')).toBeInTheDocument();
    });
  });
});
