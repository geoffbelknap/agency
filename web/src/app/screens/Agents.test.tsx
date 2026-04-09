import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Routes, Route } from 'react-router';
import { http, HttpResponse } from 'msw';
import { server } from '../../test/server';
import { Agents } from './Agents';

vi.mock('../../lib/ws', () => ({ socket: { on: () => () => {}, connect: () => {}, disconnect: () => {}, connected: false } }));

const toastSuccess = vi.fn();
const toastError = vi.fn();
vi.mock('sonner', () => ({ toast: { success: (...args: any[]) => toastSuccess(...args), error: (...args: any[]) => toastError(...args) } }));

const BASE = 'http://localhost:8200/api/v1';

const defaultAgents = [
  { name: 'alice', status: 'running', mode: 'autonomous', type: 'agent', preset: 'default', team: 'alpha', enforcer: 'active' },
  { name: 'bob', status: 'stopped', mode: 'assisted', type: 'agent', preset: 'researcher', team: 'alpha', enforcer: 'paused' },
];

function renderAgents(route = '/agents') {
  return render(
    <MemoryRouter initialEntries={[route]}>
      <Routes>
        <Route path="/agents" element={<Agents />} />
        <Route path="/agents/:name" element={<Agents />} />
        <Route path="/channels/:name" element={<div>channel view</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('Agents', () => {
  beforeEach(() => {
    toastSuccess.mockClear();
    toastError.mockClear();
  });

  it('renders agents from API', async () => {
    server.use(
      http.get(`${BASE}/agents`, () =>
        HttpResponse.json([
          { name: 'steve', status: 'running', mode: 'autonomous', preset: 'default', team: 'alpha' },
        ]),
      ),
    );
    renderAgents();
    await waitFor(() => {
      expect(screen.getByText('steve')).toBeInTheDocument();
      expect(screen.getByText('1 total agents')).toBeInTheDocument();
    });
  });

  it('shows Resume button for halted agents', async () => {
    server.use(
      http.get(`${BASE}/agents`, () =>
        HttpResponse.json([
          { name: 'halted-agent', status: 'halted', mode: 'assisted' },
        ]),
      ),
      http.get(`${BASE}/agents/halted-agent/logs`, () => HttpResponse.json([])),
      http.get(`${BASE}/agents/halted-agent/budget`, () => HttpResponse.json({ daily_limit: 0, monthly_limit: 0, daily_used: 0, monthly_used: 0, today_llm_calls: 0, today_input_tokens: 0, today_output_tokens: 0 })),
    );
    renderAgents();
    await waitFor(() => {
      expect(screen.getByText('halted-agent')).toBeInTheDocument();
    });
    await userEvent.click(screen.getByText('halted-agent'));
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /resume/i })).toBeInTheDocument();
    });
  });

  it('shows Start button for stopped agents', async () => {
    server.use(
      http.get(`${BASE}/agents`, () =>
        HttpResponse.json([
          { name: 'stopped-agent', status: 'stopped', mode: 'assisted' },
        ]),
      ),
      http.get(`${BASE}/agents/stopped-agent/logs`, () => HttpResponse.json([])),
      http.get(`${BASE}/agents/stopped-agent/budget`, () => HttpResponse.json({ daily_limit: 0, monthly_limit: 0, daily_used: 0, monthly_used: 0, today_llm_calls: 0, today_input_tokens: 0, today_output_tokens: 0 })),
    );
    renderAgents();
    await waitFor(() => {
      expect(screen.getByText('stopped-agent')).toBeInTheDocument();
    });
    await userEvent.click(screen.getByText('stopped-agent'));
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /start/i })).toBeInTheDocument();
    });
  });

  it('starts a stopped agent', async () => {
    server.use(
      http.get(`${BASE}/agents`, () =>
        HttpResponse.json(defaultAgents)
      ),
      http.get(`${BASE}/agents/bob/logs`, () => HttpResponse.json([])),
      http.get(`${BASE}/agents/bob/budget`, () => HttpResponse.json({ daily_limit: 0, monthly_limit: 0, daily_used: 0, monthly_used: 0, today_llm_calls: 0, today_input_tokens: 0, today_output_tokens: 0 })),
    );
    renderAgents();
    await waitFor(() => {
      expect(screen.getByText('bob')).toBeInTheDocument();
    });
    await userEvent.click(screen.getByText('bob'));
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /start/i })).toBeInTheDocument();
    });
    await userEvent.click(screen.getByRole('button', { name: /start/i }));
    await waitFor(() => {
      expect(toastError).not.toHaveBeenCalled();
    });
  });

  it('pauses a running agent', async () => {
    server.use(
      http.get(`${BASE}/agents`, () =>
        HttpResponse.json(defaultAgents)
      ),
      http.get(`${BASE}/agents/alice/logs`, () => HttpResponse.json([])),
      http.get(`${BASE}/agents/alice/budget`, () => HttpResponse.json({ daily_limit: 0, monthly_limit: 0, daily_used: 0, monthly_used: 0, today_llm_calls: 0, today_input_tokens: 0, today_output_tokens: 0 })),
    );
    renderAgents();
    await waitFor(() => {
      expect(screen.getByText('alice')).toBeInTheDocument();
    });
    await userEvent.click(screen.getByText('alice'));
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /pause/i })).toBeInTheDocument();
    });
    await userEvent.click(screen.getByRole('button', { name: /pause/i }));
    await waitFor(() => {
      expect(toastError).not.toHaveBeenCalled();
    });
  });

  it('sends a DM task', async () => {
    server.use(
      http.get(`${BASE}/agents`, () =>
        HttpResponse.json(defaultAgents)
      ),
      http.get(`${BASE}/agents/alice/logs`, () => HttpResponse.json([])),
      http.get(`${BASE}/agents/alice/budget`, () => HttpResponse.json({ daily_limit: 0, monthly_limit: 0, daily_used: 0, monthly_used: 0, today_llm_calls: 0, today_input_tokens: 0, today_output_tokens: 0 })),
    );
    renderAgents();
    await waitFor(() => {
      expect(screen.getByText('alice')).toBeInTheDocument();
    });
    await userEvent.click(screen.getByText('alice'));
    // Click the Activity primary tab
    await waitFor(() => {
      expect(screen.getByRole('tab', { name: /activity/i })).toBeInTheDocument();
    });
    await userEvent.click(screen.getByRole('tab', { name: /activity/i }));
    await waitFor(() => {
      expect(screen.getByPlaceholderText(/describe the task/i)).toBeInTheDocument();
    });
    await userEvent.type(screen.getByPlaceholderText(/describe the task/i), 'do the thing');
    await userEvent.click(screen.getByRole('button', { name: /send to dm/i }));
    await waitFor(() => {
      expect(toastSuccess).toHaveBeenCalledWith('Message sent to DM');
    });
  });

  it('shows economics and clears semantic cache from operations', async () => {
    let cacheCleared = false;
    server.use(
      http.get(`${BASE}/agents`, () => HttpResponse.json(defaultAgents)),
      http.get(`${BASE}/agents/alice/logs`, () => HttpResponse.json([])),
      http.get(`${BASE}/agents/alice/budget`, () =>
        HttpResponse.json({ daily_limit: 0, monthly_limit: 0, daily_used: 0, monthly_used: 0, today_llm_calls: 0, today_input_tokens: 0, today_output_tokens: 0 }),
      ),
      http.get(`${BASE}/agents/alice/economics`, () =>
        HttpResponse.json({
          agent: 'alice',
          period: '2026-04-09',
          total_cost_usd: 0.125,
          requests: 7,
          input_tokens: 1200,
          output_tokens: 300,
          cache_hits: 2,
          cache_hit_rate: 0.25,
          retry_waste_usd: 0.01,
          tool_hallucination_rate: 0,
          by_model: {},
        }),
      ),
      http.delete(`${BASE}/agents/alice/cache`, () => {
        cacheCleared = true;
        return HttpResponse.json({ deleted: 3 });
      }),
    );

    renderAgents();
    await waitFor(() => {
      expect(screen.getByText('alice')).toBeInTheDocument();
    });
    await userEvent.click(screen.getByText('alice'));
    await userEvent.click(screen.getByRole('tab', { name: /operations/i }));
    await userEvent.click(screen.getByRole('tab', { name: /economics/i }));

    await waitFor(() => {
      expect(screen.getByText('$0.1250')).toBeInTheDocument();
      expect(screen.getByText('7')).toBeInTheDocument();
    });

    await userEvent.click(screen.getByRole('tab', { name: /knowledge/i }));
    await userEvent.click(screen.getByRole('button', { name: /clear cache/i }));

    await waitFor(() => {
      expect(cacheCleared).toBe(true);
      expect(toastSuccess).toHaveBeenCalledWith('Cache cleared for alice (3 deleted)');
    });
  });

  it('refreshes and kills all meeseeks for an agent', async () => {
    let listCalls = 0;
    let killedAll = false;
    server.use(
      http.get(`${BASE}/agents`, () => HttpResponse.json(defaultAgents)),
      http.get(`${BASE}/agents/alice/logs`, () => HttpResponse.json([])),
      http.get(`${BASE}/agents/alice/budget`, () =>
        HttpResponse.json({ daily_limit: 0, monthly_limit: 0, daily_used: 0, monthly_used: 0, today_llm_calls: 0, today_input_tokens: 0, today_output_tokens: 0 }),
      ),
      http.get(`${BASE}/agents/alice/channels`, () => HttpResponse.json([])),
      http.get(`${BASE}/agents/meeseeks`, ({ request }) => {
        const parent = new URL(request.url).searchParams.get('parent');
        if (parent !== 'alice') return HttpResponse.json([]);
        listCalls += 1;
        if (killedAll) return HttpResponse.json([]);
        return HttpResponse.json([
          { id: 'mks-1', parent_agent: 'alice', task: 'summarize incident', status: 'working', model: 'haiku', budget: 1, budget_used: 0.1 },
        ]);
      }),
      http.delete(`${BASE}/agents/meeseeks`, ({ request }) => {
        const parent = new URL(request.url).searchParams.get('parent');
        if (parent === 'alice') {
          killedAll = true;
          return HttpResponse.json({ status: 'terminated', killed: ['mks-1'] });
        }
        return HttpResponse.json({ status: 'terminated', killed: [] });
      }),
    );

    renderAgents();
    await waitFor(() => {
      expect(screen.getByText('alice')).toBeInTheDocument();
    });
    await userEvent.click(screen.getByText('alice'));
    await userEvent.click(screen.getByRole('tab', { name: /operations/i }));
    await userEvent.click(screen.getByRole('tab', { name: /meeseeks/i }));

    await waitFor(() => {
      expect(screen.getByText('mks-1')).toBeInTheDocument();
    });

    await userEvent.click(screen.getByRole('button', { name: /refresh meeseeks/i }));
    await waitFor(() => {
      expect(listCalls).toBeGreaterThanOrEqual(2);
    });

    await userEvent.click(screen.getByRole('button', { name: /kill all/i }));
    await waitFor(() => {
      expect(killedAll).toBe(true);
      expect(toastSuccess).toHaveBeenCalledWith('Killed 1 meeseeks for alice');
      expect(screen.getByText('No active meeseeks.')).toBeInTheDocument();
    });
  });

  it('shows Resume for halted agent', async () => {
    server.use(
      http.get(`${BASE}/agents`, () =>
        HttpResponse.json([
          { name: 'carol', status: 'halted', mode: 'autonomous', type: 'agent', preset: 'default', team: 'alpha', enforcer: 'active' },
        ])
      ),
      http.get(`${BASE}/agents/carol/logs`, () => HttpResponse.json([])),
      http.get(`${BASE}/agents/carol/budget`, () => HttpResponse.json({ daily_limit: 0, monthly_limit: 0, daily_used: 0, monthly_used: 0, today_llm_calls: 0, today_input_tokens: 0, today_output_tokens: 0 })),
    );
    renderAgents();
    await waitFor(() => {
      expect(screen.getByText('carol')).toBeInTheDocument();
    });
    await userEvent.click(screen.getByText('carol'));
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /resume/i })).toBeInTheDocument();
    });
  });

  it('shows spinning feedback while refreshing agents', async () => {
    let releaseRefresh: (() => void) | null = null;
    let requestCount = 0;

    server.use(
      http.get(`${BASE}/agents`, async () => {
        requestCount += 1;
        if (requestCount === 1) {
          return HttpResponse.json(defaultAgents);
        }

        await new Promise<void>((resolve) => {
          releaseRefresh = resolve;
        });
        return HttpResponse.json(defaultAgents);
      }),
    );

    renderAgents();

    await waitFor(() => {
      expect(screen.getByText('alice')).toBeInTheDocument();
    });

    const refreshButton = screen.getByRole('button', { name: /refresh agents/i });
    await userEvent.click(refreshButton);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /refreshing agents/i })).toBeDisabled();
    });

    releaseRefresh!();

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /refresh agents/i })).not.toBeDisabled();
    });
  });
});
