import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { AgentList } from '../app/screens/agents/AgentList';

const agents = [
  { name: 'alice', status: 'running' as const, team: 'alpha', mission: 'research', lastActive: '2026-03-27T10:00:00Z', budget: { daily_used: 80, daily_limit: 100 } },
  { name: 'bob', status: 'stopped' as const, team: 'beta', mission: undefined, lastActive: '2026-03-26T15:00:00Z', budget: { daily_used: 98, daily_limit: 100 } },
];

describe('AgentList', () => {
  it('renders agent names in table rows', () => {
    render(<AgentList agents={agents} selectedAgent={null} onSelect={vi.fn()} />);
    expect(screen.getByText('alice')).toBeInTheDocument();
    expect(screen.getByText('bob')).toBeInTheDocument();
  });

  it('shows status for each agent', () => {
    render(<AgentList agents={agents} selectedAgent={null} onSelect={vi.fn()} />);
    expect(screen.getByText('running')).toBeInTheDocument();
    expect(screen.getByText('stopped')).toBeInTheDocument();
  });

  it('highlights selected agent', () => {
    const { container } = render(<AgentList agents={agents} selectedAgent="alice" onSelect={vi.fn()} />);
    const rows = container.querySelectorAll('tr');
    const aliceRow = Array.from(rows).find((r) => r.textContent?.includes('alice'));
    expect(aliceRow?.className).toMatch(/primary/);
  });

  it('calls onSelect when row is clicked', () => {
    const onSelect = vi.fn();
    render(<AgentList agents={agents} selectedAgent={null} onSelect={onSelect} />);
    fireEvent.click(screen.getByText('bob'));
    expect(onSelect).toHaveBeenCalledWith('bob');
  });

  it('shows budget bar with warning color when usage > 95%', () => {
    const { container } = render(<AgentList agents={agents} selectedAgent={null} onSelect={vi.fn()} />);
    const bars = container.querySelectorAll('[data-budget-bar]');
    const bobBar = Array.from(bars).find((b) => b.closest('tr')?.textContent?.includes('bob'));
    expect(bobBar?.className).toMatch(/red/);
  });
});
