import { describe, it, expect, beforeAll, beforeEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { server } from '../../../test/server';
import { ChatStep } from './ChatStep';

const BASE = 'http://localhost:8200/api/v1';
const wsHandlers: Record<string, ((event: any) => void)[]> = {};

vi.mock('../../lib/ws', () => ({
  socket: {
    on: (type: string, handler: (event: any) => void) => {
      wsHandlers[type] ??= [];
      wsHandlers[type].push(handler);
      return () => {
        wsHandlers[type] = (wsHandlers[type] || []).filter((h) => h !== handler);
      };
    },
    connect: () => {},
    disconnect: () => {},
    connected: true,
  },
}));

beforeAll(() => {
  window.HTMLElement.prototype.scrollIntoView = () => {};
});

beforeEach(() => {
  Object.keys(wsHandlers).forEach((key) => delete wsHandlers[key]);
});

function emitSocket(type: string, event: any) {
  for (const handler of wsHandlers[type] || []) {
    handler(event);
  }
}

describe('ChatStep', () => {
  it('offers guided first-task prompts and finishes into the agent DM', async () => {
    const onFinish = vi.fn();

    server.use(
      http.get(`${BASE}/agents/bob`, () => HttpResponse.json({ name: 'bob', status: 'running' })),
      http.get(`${BASE}/comms/channels`, () => HttpResponse.json([{ name: 'dm-bob', type: 'dm' }])),
      http.get(`${BASE}/comms/channels/dm-bob/messages`, () => HttpResponse.json([])),
      http.post(`${BASE}/agents/bob/dm`, () => HttpResponse.json({ status: 'ready', channel: 'dm-bob' })),
      http.post(`${BASE}/comms/channels/dm-bob/messages`, () => HttpResponse.json({ ok: true })),
    );

    render(
      <ChatStep
        agentName="bob"
        operatorName="Alice"
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
    expect(onFinish).toHaveBeenCalledWith('dm-bob');
  });

  it('retries the initial setup prompt after a transient send failure', async () => {
    const messages: Array<{ id: string; author: string; content: string; timestamp: string; flags: Record<string, boolean> }> = [];
    let postAttempts = 0;

    server.use(
      http.get(`${BASE}/agents/bob`, () => HttpResponse.json({ name: 'bob', status: 'running' })),
      http.get(`${BASE}/comms/channels`, () => HttpResponse.json([{ name: 'dm-bob', type: 'dm' }])),
      http.get(`${BASE}/comms/channels/dm-bob/messages`, () => HttpResponse.json(messages)),
      http.post(`${BASE}/agents/bob/dm`, () => HttpResponse.json({ status: 'ready', channel: 'dm-bob' })),
      http.post(`${BASE}/comms/channels/dm-bob/messages`, async ({ request }) => {
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
        agentName="bob"
        operatorName="Alice"
        onFinish={() => {}}
        onBack={() => {}}
      />,
    );

    await waitFor(() => {
      expect(postAttempts).toBe(2);
    }, { timeout: 5_000 });
    expect(await screen.findByText(/Hey bob, I just set up Agency/)).toBeInTheDocument();
  });

  it('does not mark chat ready when agent startup polling times out', async () => {
    let initialPromptSent = false;

    server.use(
      http.get(`${BASE}/agents/bob`, () => HttpResponse.json({ name: 'bob', status: 'stopped' })),
      http.get(`${BASE}/comms/channels`, () => HttpResponse.json([{ name: 'dm-bob', type: 'dm' }])),
      http.get(`${BASE}/comms/channels/dm-bob/messages`, () => HttpResponse.json([])),
      http.post(`${BASE}/agents/bob/dm`, () => HttpResponse.json({ status: 'ready', channel: 'dm-bob' })),
      http.post(`${BASE}/comms/channels/dm-bob/messages`, () => {
        initialPromptSent = true;
        return HttpResponse.json({ ok: true });
      }),
    );

    render(
      <ChatStep
        agentName="bob"
        operatorName="Alice"
        onFinish={() => {}}
        onBack={() => {}}
        agentReadyPolls={2}
        agentReadyPollDelayMs={1}
      />,
    );

    expect(await screen.findByText(/Starting bob/)).toBeInTheDocument();
    expect(await screen.findByText('Agent is not ready yet')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Check Again' })).toBeInTheDocument();
    expect(initialPromptSent).toBe(false);
  });

  it('sends the initial prompt when the agent is already running', async () => {
    let initialPromptSent = false;

    server.use(
      http.get(`${BASE}/agents/bob`, () => HttpResponse.json({ name: 'bob', status: 'running' })),
      http.get(`${BASE}/comms/channels`, () => HttpResponse.json([{ name: 'dm-bob', type: 'dm' }])),
      http.get(`${BASE}/comms/channels/dm-bob/messages`, () => HttpResponse.json([])),
      http.post(`${BASE}/agents/bob/dm`, () => HttpResponse.json({ status: 'ready', channel: 'dm-bob' })),
      http.post(`${BASE}/comms/channels/dm-bob/messages`, () => {
        initialPromptSent = true;
        return HttpResponse.json({ ok: true });
      }),
    );

    render(
      <ChatStep
        agentName="bob"
        operatorName="Alice"
        onFinish={() => {}}
        onBack={() => {}}
      />,
    );

    await waitFor(() => {
      expect(initialPromptSent).toBe(true);
      expect(screen.getByPlaceholderText(/what can you help me with/i)).toBeEnabled();
    });
  });

  it('uses the production chat avatar treatment for setup messages', async () => {
    server.use(
      http.get(`${BASE}/comms/channels`, () => HttpResponse.json([{ name: 'dm-bob', type: 'dm' }])),
      http.get(`${BASE}/comms/channels/dm-bob/messages`, () => HttpResponse.json([
        {
          id: 'm1',
          author: 'operator',
          content: 'test',
          timestamp: new Date().toISOString(),
          flags: {},
        },
        {
          id: 'm2',
          author: 'bob',
          content: 'Ready.',
          timestamp: new Date().toISOString(),
          flags: {},
        },
      ])),
      http.post(`${BASE}/comms/channels/dm-bob/messages`, () => HttpResponse.json({ ok: true })),
    );

    render(
      <ChatStep
        agentName="bob"
        operatorName="Alice"
        onFinish={() => {}}
        onBack={() => {}}
        initialAgentReady
      />,
    );

    expect(await screen.findByText('Ready.')).toBeInTheDocument();
    expect(screen.getByText('Alice')).toBeInTheDocument();
    expect(screen.getByText('bob')).toBeInTheDocument();
    expect(screen.getByText('AGENT')).toBeInTheDocument();
  });

  it('marks the setup chat ready from an agent_status event', async () => {
    server.use(
      http.get(`${BASE}/agents/bob`, () => HttpResponse.json({ name: 'bob', status: 'stopped' })),
      http.get(`${BASE}/comms/channels`, () => HttpResponse.json([])),
      http.post(`${BASE}/agents/bob/dm`, () => HttpResponse.json({ status: 'ready', channel: 'dm-bob' })),
      http.get(`${BASE}/comms/channels/dm-bob/messages`, () => HttpResponse.json([])),
      http.post(`${BASE}/comms/channels/dm-bob/messages`, () => HttpResponse.json({ ok: true })),
    );

    render(
      <ChatStep
        agentName="bob"
        operatorName="Alice"
        onFinish={() => {}}
        onBack={() => {}}
        agentReadyPolls={100}
        agentReadyPollDelayMs={1000}
      />,
    );

    expect(await screen.findByText(/Starting bob/)).toBeInTheDocument();
    emitSocket('agent_status', { agent: 'bob', status: 'running' });

    await waitFor(() => {
      expect(screen.getByPlaceholderText(/what can you help me with/i)).toBeEnabled();
    });
  });

  it('ensures the DM channel through the agent endpoint when no DM exists yet', async () => {
    let ensured = false;

    server.use(
      http.get(`${BASE}/agents/bob`, () => HttpResponse.json({ name: 'bob', status: 'running' })),
      http.get(`${BASE}/comms/channels`, () => HttpResponse.json([])),
      http.post(`${BASE}/agents/bob/dm`, () => {
        ensured = true;
        return HttpResponse.json({ status: 'ready', channel: 'dm-bob' });
      }),
      http.get(`${BASE}/comms/channels/dm-bob/messages`, () => HttpResponse.json([])),
      http.post(`${BASE}/comms/channels/dm-bob/messages`, () => HttpResponse.json({ ok: true })),
    );

    render(
      <ChatStep
        agentName="bob"
        operatorName="Alice"
        onFinish={() => {}}
        onBack={() => {}}
      />,
    );

    await waitFor(() => {
      expect(ensured).toBe(true);
      expect(screen.getByPlaceholderText(/what can you help me with/i)).toBeInTheDocument();
    });
  });
});
