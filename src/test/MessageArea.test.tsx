import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MessageArea } from '../app/components/chat/MessageArea';
import type { Channel } from '../app/types';

vi.mock('../app/components/chat/MessageList', () => ({
  MessageList: () => <div data-testid="message-list" />,
}));

vi.mock('../app/components/chat/TypingIndicator', () => ({
  TypingIndicator: () => <div data-testid="typing-indicator" />,
}));

vi.mock('../app/components/chat/ComposeBar', () => ({
  ComposeBar: ({ channelName }: { channelName: string }) => (
    <div data-testid="compose-bar">compose:{channelName}</div>
  ),
}));

const channel: Channel = {
  id: 'general',
  name: 'general',
  topic: 'General discussion',
  unreadCount: 0,
  mentionCount: 0,
  lastActivity: '',
  members: ['alice', 'bob'],
};

describe('MessageArea layout constraints', () => {
  it('applies min-h-0 to keep scrolling inside message pane', () => {
    const { container } = render(
      <MessageArea
        channel={channel}
        messages={[]}
        loading={false}
        onSend={vi.fn()}
      />,
    );

    const root = container.firstElementChild;
    expect(root).toBeTruthy();
    expect(root).toHaveClass('flex-1');
    expect(root).toHaveClass('flex');
    expect(root).toHaveClass('min-h-0');
    expect(root).toHaveClass('min-w-0');
    expect(root).toHaveClass('flex-col');
  });

  it('renders key chat regions', () => {
    render(
      <MessageArea
        channel={channel}
        messages={[]}
        loading={false}
        onSend={vi.fn()}
      />,
    );

    expect(screen.getByText('general')).toBeInTheDocument();
    expect(screen.getByText('General discussion')).toBeInTheDocument();
    expect(screen.getByTestId('message-list')).toBeInTheDocument();
    expect(screen.getByTestId('typing-indicator')).toBeInTheDocument();
    expect(screen.getByTestId('compose-bar')).toHaveTextContent('compose:general');
  });
});
