import { describe, it, expect } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { server } from '../../../test/server';
import { renderWithRouter } from '../../../test/render';
import { MissionHealthTab } from './MissionHealthTab';

const BASE = 'http://localhost:8200/api/v1';

describe('MissionHealthTab', () => {
  it('shows recommended next steps when mission infrastructure is degraded', async () => {
    server.use(
      http.get(`${BASE}/missions/alpha/health`, () =>
        HttpResponse.json({
          mission: 'alpha',
          status: 'degraded',
          summary: 'Gateway proxy needs attention',
          checks: [
            { name: 'gateway', status: 'fail', detail: 'Gateway proxy is unavailable', fix: 'Restart infrastructure from the Admin screen.' },
            { name: 'doctor', status: 'warn', detail: 'Doctor found follow-up issues', fix: 'Run Doctor and review affected agents.' },
          ],
        }),
      ),
      http.get(`${BASE}/missions/alpha/evaluations`, () => HttpResponse.json({ evaluations: [] })),
      http.get(`${BASE}/missions/alpha/procedures`, () => HttpResponse.json({ procedures: [] })),
      http.get(`${BASE}/missions/alpha/episodes`, () => HttpResponse.json({ episodes: [] })),
    );

    renderWithRouter(<MissionHealthTab missionName="alpha" />, { route: '/missions/alpha' });

    await waitFor(() => {
      expect(screen.getByText('Infrastructure: degraded')).toBeInTheDocument();
      expect(screen.getByText('Recommended Next Steps')).toBeInTheDocument();
      expect(screen.getAllByText('Restart infrastructure from the Admin screen.').length).toBeGreaterThan(1);
      expect(screen.getByText('Run Doctor and review affected agents.')).toBeInTheDocument();
      expect(screen.getByRole('link', { name: 'Open Infrastructure' })).toHaveAttribute('href', '/admin/infrastructure');
      expect(screen.getByRole('link', { name: 'Open Doctor' })).toHaveAttribute('href', '/admin/doctor');
      expect(screen.getByRole('link', { name: 'Open Mission Overview' })).toHaveAttribute('href', '/missions/alpha');
    });
  });
});
