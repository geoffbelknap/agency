import { render, screen, fireEvent } from '@testing-library/react';
import { ToolCallCard } from '../app/components/chat/ToolCallCard';

describe('ToolCallCard', () => {
  const toolCall = {
    tool: 'execute_command',
    input: { command: 'ls -la /workspace' },
    output: 'total 42\ndrwxr-xr-x ...',
    duration_ms: 1234,
  };

  it('renders tool name and duration', () => {
    render(<ToolCallCard call={toolCall} agent="engineer" />);
    expect(screen.getByText(/execute_command/)).toBeInTheDocument();
    expect(screen.getByText(/1.2s/)).toBeInTheDocument();
  });

  it('is collapsed by default', () => {
    render(<ToolCallCard call={toolCall} agent="engineer" />);
    expect(screen.queryByText('ls -la /workspace')).not.toBeInTheDocument();
  });

  it('expands to show input and output on click', () => {
    render(<ToolCallCard call={toolCall} agent="engineer" />);
    fireEvent.click(screen.getByText(/execute_command/));
    expect(screen.getByText(/ls -la \/workspace/)).toBeInTheDocument();
    expect(screen.getByText(/total 42/)).toBeInTheDocument();
  });

  it('keeps the compact header focused on the tool call', () => {
    render(<ToolCallCard call={toolCall} agent="engineer" />);
    expect(screen.getByText(/execute_command/)).toBeInTheDocument();
    expect(screen.queryByText(/engineer/)).not.toBeInTheDocument();
  });

  it('handles missing duration gracefully', () => {
    const noDuration = { ...toolCall, duration_ms: undefined };
    render(<ToolCallCard call={noDuration} agent="engineer" />);
    expect(screen.getByText(/execute_command/)).toBeInTheDocument();
    expect(screen.queryByText(/s$/)).not.toBeInTheDocument();
  });

  it('handles missing output gracefully', () => {
    const noOutput = { ...toolCall, output: undefined };
    render(<ToolCallCard call={noOutput} agent="engineer" />);
    fireEvent.click(screen.getByText(/execute_command/));
    expect(screen.getByText(/ls -la \/workspace/)).toBeInTheDocument();
  });
});
