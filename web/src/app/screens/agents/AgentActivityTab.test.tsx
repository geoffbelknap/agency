import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { LogsSection } from './AgentActivityTab';
import type { RawAuditEntry } from '../../lib/api';

const logs: RawAuditEntry[] = [
  {
    timestamp: '2026-04-21T19:44:40Z',
    event: 'LLM_DIRECT',
    source: 'enforcer',
    agent: 'test-1',
    model: 'claude-sonnet',
    provider_model: 'claude-sonnet-4-5-20251001',
    status: 200,
    duration_ms: 753,
    input_tokens: 627,
    output_tokens: 17,
    request_id: 'req-llm-1',
  },
  {
    timestamp: '2026-04-21T19:44:54Z',
    event: 'MEDIATION_PROXY',
    source: 'enforcer',
    agent: 'test-1',
    method: 'POST',
    path: '/v1/messages',
    host: 'api.anthropic.com',
    status: 200,
    duration_ms: 1323,
    lifecycle_id: 'life-123',
  },
  {
    ts: '2026-04-21T19:45:01Z',
    type: 'SECURITY_SCAN_FLAGGED',
    source: 'enforcer',
    agent: 'test-1',
    scan_type: 'xpia',
    scan_surface: 'llm_tool_messages',
    scan_action: 'flagged',
    scan_mode: 'pattern',
    finding_count: 2,
    findings: ['instruction override', 'cross-tool: output references tool'],
    content_sha256: 'abc123',
    content_bytes: 88,
    content_count: 1,
  },
  {
    ts: '2026-04-21T19:45:08Z',
    type: 'SECURITY_SCAN_NOT_APPLICABLE',
    source: 'enforcer',
    agent: 'test-1',
    scan_type: 'xpia',
    scan_surface: 'provider_tool_content',
    scan_action: 'not_applicable',
    scan_mode: 'provider_boundary',
    finding_count: 0,
    content_count: 1,
  },
];

describe('LogsSection', () => {
  it('renders useful audit summaries instead of only event source', async () => {
    render(<LogsSection agentName="test-1" logs={logs} refreshingLogs={false} refreshLogs={vi.fn()} />);

    expect(screen.getByText('LLM request')).toBeInTheDocument();
    expect(screen.getByText(/claude-sonnet · 753ms · 627 in \/ 17 out · status 200 · enforcer/)).toBeInTheDocument();
    expect(screen.getByText('Mediation event')).toBeInTheDocument();
    expect(screen.getByText(/POST · \/v1\/messages · enforcer · status 200 · 1.3s/)).toBeInTheDocument();
    expect(screen.getByText('Security scan flagged')).toBeInTheDocument();
    expect(screen.getByText(/xpia · llm_tool_messages · flagged · 2 findings · 1 items · 88 bytes/)).toBeInTheDocument();
    expect(screen.getByText('Security scan not applicable')).toBeInTheDocument();
  });

  it('expands structured audit fields and preserves raw JSON', async () => {
    const user = userEvent.setup();
    render(<LogsSection agentName="test-1" logs={logs} refreshingLogs={false} refreshLogs={vi.fn()} />);

    await user.click(screen.getByText('LLM request'));

    expect(screen.getByText('Actor and identity')).toBeInTheDocument();
    expect(screen.getByText('request_id')).toBeInTheDocument();
    expect(screen.getByText('req-llm-1')).toBeInTheDocument();
    expect(screen.getByText('Raw JSON')).toBeInTheDocument();
  });

  it('filters audit events by category', async () => {
    const user = userEvent.setup();
    render(<LogsSection agentName="test-1" logs={logs} refreshingLogs={false} refreshLogs={vi.fn()} />);

    await user.click(screen.getByRole('tab', { name: 'LLM' }));

    expect(screen.getByText('LLM request')).toBeInTheDocument();
    expect(screen.queryByText('Mediation event')).not.toBeInTheDocument();
  });

  it('filters security audit events by category', async () => {
    const user = userEvent.setup();
    render(<LogsSection agentName="test-1" logs={logs} refreshingLogs={false} refreshLogs={vi.fn()} />);

    await user.click(screen.getByRole('tab', { name: 'Security' }));

    expect(screen.getByText('Security scan flagged')).toBeInTheDocument();
    expect(screen.getByText('Security scan not applicable')).toBeInTheDocument();
    expect(screen.queryByText('LLM request')).not.toBeInTheDocument();
  });
});
