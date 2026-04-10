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
    let startCalled = false;

    server.use(
      http.post(`${BASE}/agents`, () =>
        HttpResponse.json({ error: 'agent "henry" already exists' }, { status: 409 }),
      ),
      http.post(`${BASE}/agents/henry/start`, () => {
        startCalled = true;
        return HttpResponse.json({ ok: true });
      }),
    );

    render(
      <AgentStep
        agentName="henry"
        agentPreset="platform-expert"
        platformExpert
        onUpdate={onUpdate}
        onPlatformExpertToggle={() => {}}
        onNext={onNext}
        onBack={() => {}}
      />,
    );

    await userEvent.click(screen.getByRole('button', { name: /create or start/i }));

    await waitFor(() => {
      expect(startCalled).toBe(true);
      expect(onUpdate).toHaveBeenCalledWith('henry', 'platform-expert');
      expect(onNext).toHaveBeenCalled();
    });
  });

  it('still shows actionable errors for non-idempotent create failures', async () => {
    server.use(
      http.post(`${BASE}/agents`, () =>
        HttpResponse.json({ error: 'Docker is not running' }, { status: 500 }),
      ),
    );

    render(
      <AgentStep
        agentName="henry"
        agentPreset="platform-expert"
        platformExpert
        onUpdate={() => {}}
        onPlatformExpertToggle={() => {}}
        onNext={() => {}}
        onBack={() => {}}
      />,
    );

    await userEvent.click(screen.getByRole('button', { name: /create or start/i }));

    expect(await screen.findByText('Docker is required to run agents. Please start Docker and try again.')).toBeInTheDocument();
  });
});
