import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { LogsSection } from './AgentActivityTab';
import type { RawAuditEntry } from '../../lib/api';

const logs: RawAuditEntry[] = [
  {
    timestamp: '2026-04-21T19:44:40Z',
    event: 'LLM_DIRECT',
    source: 'enforcer',
    agent: 'test-1',
    model: 'provider-a-standard',
    status: 200,
  },
  {
    timestamp: '2026-04-21T19:45:15Z',
    event: 'agent_signal_pact_verdict',
    source: 'body',
    agent: 'test-1',
    task_id: 'task-20260422-node',
    kind: 'current_info',
    verdict: 'completed',
    source_urls: ['https://nodejs.org/en/blog/release/v24.15.0'],
    tools: ['provider-web-search'],
  },
];

describe('LogsSection', () => {
  it('renders redesigned audit activity rows', () => {
    render(<LogsSection agentName="test-1" logs={logs} refreshingLogs={false} refreshLogs={vi.fn()} />);

    expect(screen.getByText('LLM_DIRECT')).toBeInTheDocument();
    expect(screen.getByText('agent_signal_pact_verdict')).toBeInTheDocument();
    expect(screen.getByText('enforcer')).toBeInTheDocument();
    expect(screen.getByText('body')).toBeInTheDocument();
  });
});
