import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { MessageActions } from '../app/components/chat/MessageActions';
import type { Message } from '../app/types';

const baseMessage: Message = {
  id: 'msg-1',
  channelId: 'general',
  author: 'operator',
  displayAuthor: 'operator',
  isAgent: false,
  isSystem: false,
  timestamp: '12:00',
  content: 'Hello world',
  flag: null,
};

const agentMessage: Message = {
  ...baseMessage,
  id: 'msg-2',
  author: 'scout',
  isAgent: true,
};

describe('MessageActions', () => {
  const defaultProps = {
    onReply: vi.fn(),
    onReact: vi.fn(),
    onEdit: vi.fn(),
    onDelete: vi.fn(),
  };

  it('shows reply and react buttons for agent messages', () => {
    render(<MessageActions message={agentMessage} {...defaultProps} />);
    expect(screen.getByLabelText('Reply')).toBeInTheDocument();
    expect(screen.getByLabelText('React')).toBeInTheDocument();
  });

  it('shows all 4 buttons for operator messages', () => {
    render(<MessageActions message={baseMessage} {...defaultProps} />);
    expect(screen.getByLabelText('Reply')).toBeInTheDocument();
    expect(screen.getByLabelText('React')).toBeInTheDocument();
    expect(screen.getByLabelText('Edit')).toBeInTheDocument();
    expect(screen.getByLabelText('Delete')).toBeInTheDocument();
  });

  it('hides edit/delete for agent messages', () => {
    render(<MessageActions message={agentMessage} {...defaultProps} />);
    expect(screen.queryByLabelText('Edit')).not.toBeInTheDocument();
    expect(screen.queryByLabelText('Delete')).not.toBeInTheDocument();
  });

  it('calls onReply when reply button is clicked', () => {
    const onReply = vi.fn();
    render(<MessageActions message={baseMessage} {...defaultProps} onReply={onReply} />);
    fireEvent.click(screen.getByLabelText('Reply'));
    expect(onReply).toHaveBeenCalledTimes(1);
  });

  it('calls onReact when react button is clicked', () => {
    const onReact = vi.fn();
    render(<MessageActions message={baseMessage} {...defaultProps} onReact={onReact} />);
    fireEvent.click(screen.getByLabelText('React'));
    expect(onReact).toHaveBeenCalledTimes(1);
  });

  it('calls onEdit when edit button is clicked', () => {
    const onEdit = vi.fn();
    render(<MessageActions message={baseMessage} {...defaultProps} onEdit={onEdit} />);
    fireEvent.click(screen.getByLabelText('Edit'));
    expect(onEdit).toHaveBeenCalledTimes(1);
  });

  it('calls onDelete when delete button is clicked', () => {
    const onDelete = vi.fn();
    render(<MessageActions message={baseMessage} {...defaultProps} onDelete={onDelete} />);
    fireEvent.click(screen.getByLabelText('Delete'));
    expect(onDelete).toHaveBeenCalledTimes(1);
  });
});
