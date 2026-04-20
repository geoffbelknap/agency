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
    expect(screen.queryByText('Team')).not.toBeInTheDocument();
    expect(screen.queryByText('Mission')).not.toBeInTheDocument();
  });

  it('shows status for each agent', () => {
    const { container } = render(<AgentList agents={agents} selectedAgent={null} onSelect={vi.fn()} />);
    const statusDots = container.querySelectorAll('[aria-hidden="true"][style*="border-radius: 50%"]');
    expect(statusDots).toHaveLength(2);
  });

  it('highlights selected agent', () => {
    render(<AgentList agents={agents} selectedAgent="alice" onSelect={vi.fn()} />);
    const aliceRow = screen.getByRole('button', { name: /alice/i });
    expect(aliceRow.getAttribute('style')).toContain('border-left: 2px solid var(--teal)');
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
    const bobBar = Array.from(bars).find((b) => b.closest('button')?.textContent?.includes('bob'));
    expect(bobBar).toHaveStyle({ background: 'var(--red)' });
  });
});
