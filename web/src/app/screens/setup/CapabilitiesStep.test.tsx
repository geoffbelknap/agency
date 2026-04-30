import { describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { server } from '../../../test/server';
import { render } from '../../../test/render';
import { CapabilitiesStep } from './CapabilitiesStep';

const BASE = 'http://localhost:8200/api/v1';

describe('CapabilitiesStep', () => {
  it('defaults first-run setup to the standard tier instead of minimal', async () => {
    server.use(
      http.get(`${BASE}/admin/capabilities`, () =>
        HttpResponse.json([
          { name: 'filesystem', kind: 'tool', state: 'disabled', description: 'Read and write files' },
        ]),
      ),
      http.get(`${BASE}/infra/setup/config`, () =>
        HttpResponse.json({
          capability_tiers: {
            minimal: { display_name: 'Minimal', description: 'Minimal tier', capabilities: [] },
            standard: { display_name: 'Standard', description: 'Recommended defaults', capabilities: ['filesystem'] },
          },
        }),
      ),
    );

    render(
      <CapabilitiesStep
        capabilities={[]}
        onUpdate={() => {}}
        onNext={() => {}}
        onBack={() => {}}
      />,
    );

    await waitFor(() => {
      expect(screen.getByText('filesystem')).toBeInTheDocument();
      expect(screen.getByText('Recommended defaults')).toBeInTheDocument();
    });
  });
});
