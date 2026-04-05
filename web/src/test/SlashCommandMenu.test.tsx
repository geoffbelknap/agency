import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { SlashCommandMenu } from '../app/components/chat/SlashCommandMenu';

describe('SlashCommandMenu', () => {
  const defaultProps = {
    filter: '',
    onSelect: vi.fn(),
    onClose: vi.fn(),
  };

  it('renders all commands when filter is empty', () => {
    render(<SlashCommandMenu {...defaultProps} />);
    expect(screen.getByText('/summarize')).toBeInTheDocument();
    expect(screen.getByText('/task')).toBeInTheDocument();
    expect(screen.getByText('/status')).toBeInTheDocument();
    expect(screen.getByText('/help')).toBeInTheDocument();
  });

  it('shows command descriptions', () => {
    render(<SlashCommandMenu {...defaultProps} />);
    expect(screen.getByText('Summarize the conversation')).toBeInTheDocument();
    expect(screen.getByText('Create a task for an agent')).toBeInTheDocument();
  });

  it('filters commands based on filter text', () => {
    render(<SlashCommandMenu filter="su" onSelect={vi.fn()} onClose={vi.fn()} />);
    expect(screen.getByText('/summarize')).toBeInTheDocument();
    expect(screen.queryByText('/task')).not.toBeInTheDocument();
    expect(screen.queryByText('/status')).not.toBeInTheDocument();
    expect(screen.queryByText('/help')).not.toBeInTheDocument();
  });

  it('filter is case-insensitive', () => {
    render(<SlashCommandMenu filter="SU" onSelect={vi.fn()} onClose={vi.fn()} />);
    expect(screen.getByText('/summarize')).toBeInTheDocument();
  });

  it('shows multiple results when filter matches several commands', () => {
    render(<SlashCommandMenu filter="s" onSelect={vi.fn()} onClose={vi.fn()} />);
    expect(screen.getByText('/summarize')).toBeInTheDocument();
    expect(screen.getByText('/status')).toBeInTheDocument();
    expect(screen.queryByText('/task')).not.toBeInTheDocument();
    expect(screen.queryByText('/help')).not.toBeInTheDocument();
  });

  it('calls onSelect with command name when a command is clicked', async () => {
    const user = userEvent.setup();
    const onSelect = vi.fn();
    render(<SlashCommandMenu filter="" onSelect={onSelect} onClose={vi.fn()} />);
    await user.click(screen.getByText('/task'));
    expect(onSelect).toHaveBeenCalledWith('/task');
  });

  it('shows empty state when no commands match', () => {
    render(<SlashCommandMenu filter="zzz" onSelect={vi.fn()} onClose={vi.fn()} />);
    expect(screen.getByText('No matching commands')).toBeInTheDocument();
  });

  it('calls onClose when Escape is pressed', async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    render(<SlashCommandMenu filter="" onSelect={vi.fn()} onClose={onClose} />);
    // Focus the button containing the command name then press Escape
    screen.getByText('/summarize').closest('button')!.focus();
    await user.keyboard('{Escape}');
    expect(onClose).toHaveBeenCalled();
  });

  it('highlights first item by default', () => {
    const { container } = render(<SlashCommandMenu {...defaultProps} />);
    const items = container.querySelectorAll('[data-selected="true"]');
    expect(items.length).toBe(1);
  });

  it('calls onSelect with Enter key on selected item', async () => {
    const user = userEvent.setup();
    const onSelect = vi.fn();
    render(<SlashCommandMenu filter="" onSelect={onSelect} onClose={vi.fn()} />);
    // Focus the first item
    screen.getByText('/summarize').closest('button')!.focus();
    await user.keyboard('{Enter}');
    expect(onSelect).toHaveBeenCalledWith('/summarize');
  });
});
