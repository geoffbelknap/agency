import { describe, it, expect, vi, beforeAll } from 'vitest';
import { render as baseRender, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import userEvent from '@testing-library/user-event';
import { ThreadPanel } from '../app/components/chat/ThreadPanel';
import type { Message } from '../app/types';

function render(ui: React.ReactElement, opts?: any) { return baseRender(<MemoryRouter>{ui}</MemoryRouter>, opts); }

vi.mock('../app/lib/ws', () => ({ socket: { on: () => () => {} } }));

beforeAll(() => {
  window.HTMLElement.prototype.scrollIntoView = () => {};
});

const makeMessage = (overrides: Partial<Message> = {}): Message => ({
  id: 'msg-1',
  channelId: 'ch-1',
  author: 'alice',
  displayAuthor: 'alice',
  isAgent: false,
  isSystem: false,
  timestamp: '12:00',
  content: 'Hello world',
  flag: null,
  ...overrides,
});

describe('ThreadPanel', () => {
  const parentMessage = makeMessage({ id: 'parent-1', author: 'alice', content: 'Parent message' });
  const replies: Message[] = [
    makeMessage({ id: 'reply-1', author: 'bob', content: 'First reply', parentId: 'parent-1' }),
    makeMessage({ id: 'reply-2', author: 'carol', content: 'Second reply', parentId: 'parent-1' }),
  ];
  const defaultProps = {
    parentMessage,
    replies,
    onClose: vi.fn(),
    onSend: vi.fn(),
  };

  it('renders parent message', () => {
    render(<ThreadPanel {...defaultProps} />);
    expect(screen.getByText('Parent message', { selector: 'div' })).toBeInTheDocument();
    expect(screen.getAllByText('alice').length).toBeGreaterThan(0);
  });

  it('renders reply messages', () => {
    render(<ThreadPanel {...defaultProps} />);
    expect(screen.getByText('First reply')).toBeInTheDocument();
    expect(screen.getByText('Second reply')).toBeInTheDocument();
  });

  it('shows "Thread" header with close button', () => {
    render(<ThreadPanel {...defaultProps} />);
    expect(screen.getByText('Thread')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /close thread/i })).toBeInTheDocument();
  });

  it('close button calls onClose', async () => {
    const user = userEvent.setup();
    render(<ThreadPanel {...defaultProps} />);
    await user.click(screen.getByRole('button', { name: /close thread/i }));
    expect(defaultProps.onClose).toHaveBeenCalledOnce();
  });

  it('ComposeBar sends message through onSend', async () => {
    const user = userEvent.setup();
    render(<ThreadPanel {...defaultProps} />);
    const input = screen.getByPlaceholderText('Reply in thread');
    await user.type(input, 'my reply{Enter}');
    await waitFor(() => {
      expect(defaultProps.onSend).toHaveBeenCalledWith('my reply');
    });
  });

  it('shows empty state when no replies', () => {
    render(<ThreadPanel {...defaultProps} replies={[]} />);
    expect(screen.getByText('No replies yet')).toBeInTheDocument();
  });
});
