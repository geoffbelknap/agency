import { describe, it, expect } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { server } from '../../test/server';
import { renderWithRouter } from '../../test/render';
import { Capabilities } from './Capabilities';

const BASE = 'http://localhost:8200/api/v1';

describe('Capabilities', () => {
  it('renders capability rows from API data', async () => {
    server.use(
      http.get(`${BASE}/admin/capabilities`, () =>
        HttpResponse.json([
          { name: 'fs.read', kind: 'tool', state: 'available', agents: [], spec: { risk: 'low', scope: 'any path' } },
          { name: 'shell.exec', kind: 'tool', state: 'restricted', agents: ['bob'], spec: { risk: 'high', scope: 'whitelist (8 cmds)' } },
        ]),
      ),
    );

    renderWithRouter(<Capabilities />);

    await waitFor(() => {
      expect(screen.getByText('Use capabilities to control what agents can touch')).toBeInTheDocument();
      expect(screen.getByText('fs.read')).toBeInTheDocument();
      expect(screen.getByText('shell.exec')).toBeInTheDocument();
      expect(screen.getByText('bob')).toBeInTheDocument();
    });
  });

  it('renders an empty capability directory state', async () => {
    server.use(
      http.get(`${BASE}/admin/capabilities`, () => HttpResponse.json([])),
    );

    renderWithRouter(<Capabilities />, { route: '/admin/capabilities' });

    await waitFor(() => {
      expect(screen.getByText('No capabilities found')).toBeInTheDocument();
      expect(screen.getByText(/Add a capability only when/)).toBeInTheDocument();
    });
  });
});
