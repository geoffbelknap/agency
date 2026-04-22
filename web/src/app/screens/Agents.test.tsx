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
const toastInfo = vi.fn();
vi.mock('sonner', () => ({ toast: { success: (...args: any[]) => toastSuccess(...args), error: (...args: any[]) => toastError(...args), info: (...args: any[]) => toastInfo(...args) } }));

const BASE = 'http://localhost:8200/api/v1';
const ndjson = (lines: unknown[]) => lines.map((line) => JSON.stringify(line)).join('\n') + '\n';

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
    toastInfo.mockClear();
    window.localStorage.clear();
    window.sessionStorage.clear();
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
      http.get(`${BASE}/agents/bob`, () => HttpResponse.json({ name: 'bob', status: 'running' })),
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

  it('opens roster actions and routes settings to the system tab', async () => {
    window.localStorage.setItem('agency.agents.variant', 'roster');
    server.use(
      http.get(`${BASE}/agents`, () =>
        HttpResponse.json(defaultAgents)
      ),
      http.get(`${BASE}/agents/alice/logs`, () => HttpResponse.json([])),
      http.get(`${BASE}/agents/alice/budget`, () => HttpResponse.json({ daily_limit: 10, monthly_limit: 100, daily_used: 1, monthly_used: 1, today_llm_calls: 0, today_input_tokens: 0, today_output_tokens: 0 })),
      http.get(`${BASE}/agents/alice/config`, () => HttpResponse.json({ identity: 'Alice identity', constraints: {} })),
      http.get(`${BASE}/admin/policy/alice`, () => HttpResponse.json({ valid: true })),
      http.get(`${BASE}/capabilities`, () => HttpResponse.json([])),
      http.get(`${BASE}/infra/provider-tools`, () => HttpResponse.json({ capabilities: {} })),
    );
    renderAgents();
    await waitFor(() => {
      expect(screen.getByText('alice')).toBeInTheDocument();
    });

    await userEvent.click(screen.getByRole('button', { name: /actions for alice/i }));
    expect(screen.getByRole('menuitem', { name: /restart/i })).toBeInTheDocument();

    await userEvent.click(screen.getByRole('menuitem', { name: /settings/i }));
    await waitFor(() => {
      expect(screen.getByRole('tab', { name: /system/i })).toHaveAttribute('aria-selected', 'true');
    });
    expect(screen.getByRole('tab', { name: /config/i })).toHaveAttribute('aria-selected', 'true');
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
      http.post(`${BASE}/agents/alice/dm`, () => HttpResponse.json({ status: 'ready', channel: 'dm-alice' })),
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

  it('shows a specific error when the DM channel is not ready yet', async () => {
    server.use(
      http.get(`${BASE}/agents`, () =>
        HttpResponse.json(defaultAgents)
      ),
      http.get(`${BASE}/agents/alice/logs`, () => HttpResponse.json([])),
      http.get(`${BASE}/agents/alice/budget`, () => HttpResponse.json({ daily_limit: 0, monthly_limit: 0, daily_used: 0, monthly_used: 0, today_llm_calls: 0, today_input_tokens: 0, today_output_tokens: 0 })),
      http.post(`${BASE}/agents/alice/dm`, () =>
        HttpResponse.json({ error: 'channel create failed' }, { status: 502 })
      ),
      http.post(`${BASE}/comms/channels/dm-alice/messages`, () =>
        HttpResponse.json({ error: 'channel not found' }, { status: 404 })
      ),
      http.get(`${BASE}/agents/alice/channels`, () => HttpResponse.json([])),
    );
    renderAgents();
    await waitFor(() => {
      expect(screen.getByText('alice')).toBeInTheDocument();
    });
    await userEvent.click(screen.getByText('alice'));
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
      expect(toastError).toHaveBeenCalledWith('DM for "alice" is not ready yet. Start the agent and wait for its conversation to appear, then try again.');
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

  it('shows saved result artifacts with PACT verdicts', async () => {
    server.use(
      http.get(`${BASE}/agents`, () => HttpResponse.json(defaultAgents)),
      http.get(`${BASE}/agents/alice/logs`, () => HttpResponse.json([])),
      http.get(`${BASE}/agents/alice/budget`, () =>
        HttpResponse.json({ daily_limit: 0, monthly_limit: 0, daily_used: 0, monthly_used: 0, today_llm_calls: 0, today_input_tokens: 0, today_output_tokens: 0 }),
      ),
      http.get(`${BASE}/agents/alice/results`, () =>
        HttpResponse.json([
          {
            task_id: 'task-20260422-node',
            has_metadata: true,
            metadata: { timestamp: '2026-04-22T08:00:00Z' },
            pact: {
              kind: 'current_info',
              verdict: 'completed',
              source_urls: ['https://nodejs.org/en/blog/release/v24.15.0'],
            },
          },
        ]),
      ),
    );

    renderAgents('/agents/alice');

    await waitFor(() => {
      expect(screen.getByText('alice')).toBeInTheDocument();
    });
    await userEvent.click(screen.getByRole('tab', { name: /results/i }));

    await waitFor(() => {
      expect(screen.getByText('task-20260422-node')).toBeInTheDocument();
      expect(screen.getByText('completed')).toBeInTheDocument();
      expect(screen.getByText('1 source')).toBeInTheDocument();
    });
  });

  it('confirms and deletes an agent from the system tab', async () => {
    let deleted = false;
    server.use(
      http.get(`${BASE}/agents`, () => HttpResponse.json(deleted ? [defaultAgents[1]] : defaultAgents)),
      http.get(`${BASE}/agents/alice/logs`, () => HttpResponse.json([])),
      http.get(`${BASE}/agents/alice/budget`, () =>
        HttpResponse.json({ daily_limit: 0, monthly_limit: 0, daily_used: 0, monthly_used: 0, today_llm_calls: 0, today_input_tokens: 0, today_output_tokens: 0 }),
      ),
      http.get(`${BASE}/capabilities`, () => HttpResponse.json([])),
      http.get(`${BASE}/admin/policy/alice`, () => HttpResponse.json({ valid: true })),
      http.get(`${BASE}/agents/alice/config`, () => HttpResponse.json({ identity: 'Alice identity' })),
      http.delete(`${BASE}/agents/alice`, () => {
        deleted = true;
        return HttpResponse.json({ status: 'deleted', name: 'alice' });
      }),
    );

    renderAgents('/agents/alice');

    await waitFor(() => {
      expect(screen.getByText('alice')).toBeInTheDocument();
    });
    await userEvent.click(screen.getByRole('tab', { name: /system/i }));

    await waitFor(() => {
      expect(screen.getByText('Danger zone')).toBeInTheDocument();
    });
    await userEvent.click(screen.getByRole('button', { name: /delete agent/i }));

    expect(screen.getByText('Delete agent "alice"?')).toBeInTheDocument();
    await userEvent.click(screen.getAllByRole('button', { name: /delete agent/i }).at(-1)!);

    await waitFor(() => {
      expect(deleted).toBe(true);
      expect(toastSuccess).toHaveBeenCalledWith('Agent "alice" deleted');
      expect(screen.queryByText('Delete agent "alice"?')).not.toBeInTheDocument();
      expect(screen.queryByText('Alice identity')).not.toBeInTheDocument();
    });
  });

  it('hides meeseeks operations in the default core UI', async () => {
    server.use(
      http.get(`${BASE}/agents`, () => HttpResponse.json(defaultAgents)),
      http.get(`${BASE}/agents/alice/logs`, () => HttpResponse.json([])),
      http.get(`${BASE}/agents/alice/budget`, () =>
        HttpResponse.json({ daily_limit: 0, monthly_limit: 0, daily_used: 0, monthly_used: 0, today_llm_calls: 0, today_input_tokens: 0, today_output_tokens: 0 }),
      ),
      http.get(`${BASE}/agents/alice/channels`, () => HttpResponse.json([])),
    );

    renderAgents();
    await waitFor(() => {
      expect(screen.getByText('alice')).toBeInTheDocument();
    });
    await userEvent.click(screen.getByText('alice'));
    await userEvent.click(screen.getByRole('tab', { name: /operations/i }));

    await waitFor(() => {
      expect(screen.queryByRole('tab', { name: /meeseeks/i })).not.toBeInTheDocument();
    });
  });

  it('opens the new agent DM after a successful create-and-start flow', async () => {
    server.use(
      http.get(`${BASE}/agents`, () => HttpResponse.json([])),
      http.get(`${BASE}/capabilities`, () => HttpResponse.json([])),
      http.get(`${BASE}/infra/status`, () => HttpResponse.json({ components: [], build_id: 'test' })),
      http.get(`${BASE}/hub/presets`, () =>
        HttpResponse.json([
          { name: 'generalist', description: 'General purpose', type: 'agent', source: 'default' },
          { name: 'researcher', description: 'Research focused', type: 'agent', source: 'default' },
        ]),
      ),
      http.post(`${BASE}/agents`, async ({ request }) => {
        const body = await request.json() as any;
        return HttpResponse.json({ status: 'created', name: body.name }, { status: 201 });
      }),
      http.post(`${BASE}/agents/research-pal/start`, () =>
        new HttpResponse(ndjson([{ type: 'complete', agent: 'research-pal' }]), { headers: { 'Content-Type': 'application/x-ndjson' } }),
      ),
      http.get(`${BASE}/agents/research-pal`, () => HttpResponse.json({ name: 'research-pal', status: 'running' })),
      http.post(`${BASE}/agents/research-pal/dm`, () => HttpResponse.json({ status: 'ready', channel: 'dm-research-pal' })),
    );

    renderAgents();

    await userEvent.click(await screen.findByRole('button', { name: /create/i }));
    await userEvent.type(await screen.findByLabelText(/name/i), 'research-pal');
    await userEvent.click(screen.getByRole('button', { name: /^create$/i }));

    await waitFor(() => {
      expect(screen.getByText('channel view')).toBeInTheDocument();
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
