import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { AgentList } from '../app/screens/agents/AgentList';

const agents = [
  { name: 'alice', status: 'running' as const, team: 'alpha', mission: 'research', lastActive: '2026-03-27T10:00:00Z', budget: { daily_used: 80, daily_limit: 100 } },
  { name: 'bob', status: 'stopped' as const, team: 'beta', mission: undefined, lastActive: '2026-03-26T15:00:00Z', budget: { daily_used: 98, daily_limit: 100 } },
];

describe('AgentList', () => {
  it('renders the redesigned agent roster table', () => {
    render(<AgentList agents={agents} selectedAgent={null} onSelect={vi.fn()} />);

    expect(screen.getByRole('table')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /alice running/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /bob stopped/i })).toBeInTheDocument();
    expect(screen.queryByText('Team')).not.toBeInTheDocument();
    expect(screen.queryByText('Mission')).not.toBeInTheDocument();
  });

  it('calls onSelect when row is clicked', () => {
    const onSelect = vi.fn();
    render(<AgentList agents={agents} selectedAgent={null} onSelect={onSelect} />);

    fireEvent.click(screen.getByRole('button', { name: /bob stopped/i }));

    expect(onSelect).toHaveBeenCalledWith('bob');
  });
});
