import { describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { server } from '../../../test/server';
import { render } from '../../../test/render';
import { StartingAgentStep } from './StartingAgentStep';

const BASE = 'http://localhost:8200/api/v1';
const ndjson = (lines: unknown[]) => lines.map((line) => JSON.stringify(line)).join('\n') + '\n';

describe('StartingAgentStep', () => {
  it('advances after the startup stream completes', async () => {
    const onReady = vi.fn();

    server.use(
      http.get(`${BASE}/agents/henry`, () => HttpResponse.json({ name: 'henry', status: 'starting' })),
      http.post(`${BASE}/agents/henry/start`, () =>
        new HttpResponse(ndjson([
          { type: 'phase', phase: 1, name: 'verify', description: 'Verifying agent configuration' },
          { type: 'complete', agent: 'henry', model: 'gemini-2.5-pro', phases: 7 },
        ]), { headers: { 'Content-Type': 'application/x-ndjson' } }),
      ),
    );

    render(
      <StartingAgentStep
        agentName="henry"
        onReady={onReady}
        onBack={() => {}}
        handoffDelayMs={1}
      />,
    );

    expect(screen.getByText('Starting agent')).toBeInTheDocument();
    expect(screen.getByText('@henry')).toBeInTheDocument();
    expect(await screen.findByText('Runtime startup complete')).toBeInTheDocument();

    await waitFor(() => expect(onReady).toHaveBeenCalledTimes(1));
  });

  it('advances directly when the agent is already running', async () => {
    const onReady = vi.fn();
    let startRequests = 0;
    server.use(
      http.get(`${BASE}/agents/henry`, () => HttpResponse.json({ name: 'henry', status: 'running' })),
      http.post(`${BASE}/agents/henry/start`, () => {
        startRequests += 1;
        return new HttpResponse(ndjson([{ type: 'complete', agent: 'henry' }]), { headers: { 'Content-Type': 'application/x-ndjson' } });
      }),
    );

    render(
      <StartingAgentStep
        agentName="henry"
        onReady={onReady}
        onBack={() => {}}
        handoffDelayMs={1}
      />,
    );

    expect(await screen.findByText('Agent is already running')).toBeInTheDocument();
    await waitFor(() => expect(onReady).toHaveBeenCalledTimes(1));
    expect(startRequests).toBe(0);
  });

  it('shows a retry action when the startup stream fails', async () => {
    let startRequests = 0;
    server.use(
      http.get(`${BASE}/agents/henry`, () => HttpResponse.json({ name: 'henry', status: 'starting' })),
      http.post(`${BASE}/agents/henry/start`, () => {
        startRequests += 1;
        return new HttpResponse(ndjson([{ type: 'error', error: 'runtime failed' }]), { headers: { 'Content-Type': 'application/x-ndjson' } });
      }),
    );

    render(
      <StartingAgentStep
        agentName="henry"
        onReady={() => {}}
        onBack={() => {}}
        handoffDelayMs={1}
      />,
    );

    expect((await screen.findAllByText('runtime failed')).length).toBeGreaterThan(0);
    await userEvent.click(screen.getByRole('button', { name: /try again/i }));

    await waitFor(() => expect(startRequests).toBeGreaterThan(1));
  });
});
