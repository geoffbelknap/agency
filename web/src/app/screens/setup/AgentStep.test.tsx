import { describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { server } from '../../../test/server';
import { render } from '../../../test/render';
import { AgentStep } from './AgentStep';

const BASE = 'http://localhost:8200/api/v1';

describe('AgentStep', () => {
  it('reuses and starts an existing agent during repeat setup', async () => {
    const onUpdate = vi.fn();
    const onNext = vi.fn();
    const granted: string[] = [];

    server.use(
      http.get(`${BASE}/agents`, () => HttpResponse.json([{ name: 'henry', status: 'running' }])),
      http.post(`${BASE}/agents`, () =>
        HttpResponse.json({ error: 'agent "henry" already exists' }, { status: 409 }),
      ),
      http.get(`${BASE}/admin/capabilities`, () => HttpResponse.json([
        { name: 'standard-tool', state: 'available' },
        { name: 'advanced-tool', state: 'available' },
        { name: 'provider-web-fetch', state: 'available' },
        { name: 'provider-web-search', state: 'available' },
      ])),
      http.get(`${BASE}/infra/setup/config`, () => HttpResponse.json({
        capability_tiers: {
          standard: { capabilities: ['standard-tool'] },
          advanced: { capabilities: ['advanced-tool'] },
        },
      })),
      http.post(`${BASE}/agents/henry/grant`, async ({ request }) => {
        const body = await request.json() as { capability?: string };
        if (body.capability) granted.push(body.capability);
        return HttpResponse.json({ ok: true });
      }),
    );

    render(
      <AgentStep
        agentName="henry"
        onUpdate={onUpdate}
        onNext={onNext}
        onBack={() => {}}
      />,
    );

    expect(await screen.findByText(/@henry already exists/i)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: /continue with henry/i }));

    await waitFor(() => {
      expect(onUpdate).toHaveBeenCalledWith('henry', 'platform-expert');
      expect(onNext).toHaveBeenCalled();
    });
    expect(granted).toEqual(['standard-tool', 'provider-web-fetch', 'provider-web-search']);
  });

  it('still shows actionable errors for non-idempotent create failures', async () => {
    server.use(
      http.get(`${BASE}/agents`, () => HttpResponse.json([])),
      http.post(`${BASE}/agents`, () =>
        HttpResponse.json({ error: 'Docker is not running' }, { status: 500 }),
      ),
    );

    render(
      <AgentStep
        agentName="henry"
        onUpdate={() => {}}
        onNext={() => {}}
        onBack={() => {}}
      />,
    );

    await userEvent.click(screen.getByRole('button', { name: /create agent/i }));

    expect(await screen.findByText('The selected runtime backend is not ready. Run agency admin doctor, fix the reported host checks, and try again.')).toBeInTheDocument();
  });
});
