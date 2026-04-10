import { describe, it, expect, beforeAll, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { server } from '../../../test/server';
import { ChatStep } from './ChatStep';

const BASE = 'http://localhost:8200/api/v1';

beforeAll(() => {
  window.HTMLElement.prototype.scrollIntoView = () => {};
});

describe('ChatStep', () => {
  it('offers guided first-task prompts and finishes into the agent DM', async () => {
    const onFinish = vi.fn();

    server.use(
      http.get(`${BASE}/agents/henry`, () => HttpResponse.json({ name: 'henry', status: 'running' })),
      http.get(`${BASE}/comms/channels`, () => HttpResponse.json([{ name: 'dm-henry', type: 'dm' }])),
      http.get(`${BASE}/comms/channels/dm-henry/messages`, () => HttpResponse.json([])),
      http.post(`${BASE}/comms/channels/dm-henry/messages`, () => HttpResponse.json({ ok: true })),
    );

    render(
      <ChatStep
        agentName="henry"
        operatorName="Geoff"
        onFinish={onFinish}
        onBack={() => {}}
      />,
    );

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Check My Setup' })).toBeInTheDocument();
    });

    await userEvent.click(screen.getByRole('button', { name: 'Check My Setup' }));
    expect(screen.getByPlaceholderText(/what can you help me with/i)).toHaveValue(
      'Check whether my local Agency setup looks healthy. Tell me what you can verify from inside Agency and what I should check manually.',
    );

    await userEvent.click(screen.getByRole('button', { name: 'Finish Setup' }));
    expect(onFinish).toHaveBeenCalledWith('dm-henry');
  });

  it('retries the initial setup prompt after a transient send failure', async () => {
    const messages: Array<{ id: string; author: string; content: string; timestamp: string; flags: Record<string, boolean> }> = [];
    let postAttempts = 0;

    server.use(
      http.get(`${BASE}/agents/henry`, () => HttpResponse.json({ name: 'henry', status: 'running' })),
      http.get(`${BASE}/comms/channels`, () => HttpResponse.json([{ name: 'dm-henry', type: 'dm' }])),
      http.get(`${BASE}/comms/channels/dm-henry/messages`, () => HttpResponse.json(messages)),
      http.post(`${BASE}/comms/channels/dm-henry/messages`, async ({ request }) => {
        postAttempts += 1;
        if (postAttempts === 1) {
          return HttpResponse.json({ error: 'comms warming up' }, { status: 502 });
        }
        const body = await request.json() as { author: string; content: string };
        messages.push({
          id: `msg-${postAttempts}`,
          author: body.author,
          content: body.content,
          timestamp: new Date().toISOString(),
          flags: {},
        });
        return HttpResponse.json({ ok: true });
      }),
    );

    render(
      <ChatStep
        agentName="henry"
        operatorName="Geoff"
        onFinish={() => {}}
        onBack={() => {}}
      />,
    );

    await waitFor(() => {
      expect(postAttempts).toBe(2);
    }, { timeout: 5_000 });
    expect(await screen.findByText(/Hey henry, I just set up Agency/)).toBeInTheDocument();
  });
});
