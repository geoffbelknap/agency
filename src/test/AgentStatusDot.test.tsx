import { render } from '@testing-library/react';
import { AgentStatusDot } from '../app/components/chat/AgentStatusDot';

describe('AgentStatusDot', () => {
  it('shows green dot for running', () => {
    const { container } = render(<AgentStatusDot status="running" />);
    expect(container.querySelector('.bg-green-500')).toBeInTheDocument();
  });

  it('shows yellow dot for idle', () => {
    const { container } = render(<AgentStatusDot status="idle" />);
    expect(container.querySelector('.bg-yellow-500')).toBeInTheDocument();
  });

  it('shows red dot for halted', () => {
    const { container } = render(<AgentStatusDot status="halted" />);
    expect(container.querySelector('.bg-red-500')).toBeInTheDocument();
  });

  it('shows gray dot for unknown', () => {
    const { container } = render(<AgentStatusDot status="unknown" />);
    expect(container.querySelector('.bg-muted-foreground')).toBeInTheDocument();
  });
});
