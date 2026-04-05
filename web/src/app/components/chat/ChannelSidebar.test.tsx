import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ChannelSidebar } from './ChannelSidebar';
import type { Channel } from '../../types';

const channels: Channel[] = [
  { id: 'general', name: 'general', topic: 'General chat', unreadCount: 3, mentionCount: 0, lastActivity: '', members: ['scout'] },
  { id: 'ops', name: 'ops', topic: 'Operations', unreadCount: 0, mentionCount: 0, lastActivity: '', members: [] },
];

describe('ChannelSidebar', () => {
  it('renders all channels', () => {
    render(<ChannelSidebar channels={channels} selectedChannel={null} onSelect={() => {}} />);
    expect(screen.getByText('general')).toBeInTheDocument();
    expect(screen.getByText('ops')).toBeInTheDocument();
  });

  it('shows unread count', () => {
    render(<ChannelSidebar channels={channels} selectedChannel={null} onSelect={() => {}} />);
    expect(screen.getByText('3')).toBeInTheDocument();
  });

  it('calls onSelect when channel clicked', async () => {
    const onSelect = vi.fn();
    render(<ChannelSidebar channels={channels} selectedChannel={null} onSelect={onSelect} />);
    await userEvent.click(screen.getByText('ops'));
    expect(onSelect).toHaveBeenCalledWith(channels[1]);
  });

  it('filters channels by search query', async () => {
    render(<ChannelSidebar channels={channels} selectedChannel={null} onSelect={() => {}} />);
    const searchInput = screen.getByRole('textbox');
    await userEvent.type(searchInput, 'gen');
    expect(screen.getByText('general')).toBeInTheDocument();
    expect(screen.queryByText('ops')).not.toBeInTheDocument();
  });

  it('shows active state for selected channel', () => {
    render(<ChannelSidebar channels={channels} selectedChannel={channels[0]} onSelect={() => {}} />);
    const buttons = screen.getAllByRole('button');
    const generalButton = buttons.find((btn) => btn.textContent?.includes('general'));
    expect(generalButton).toHaveClass('bg-accent');
  });
});
