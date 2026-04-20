import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ChannelSidebar } from './ChannelSidebar';
import type { Channel } from '../../types';

const channels: Channel[] = [
  { id: 'general', name: 'general', topic: 'General chat', unreadCount: 3, mentionCount: 0, lastActivity: '', members: ['scout'] },
  { id: 'ops', name: 'ops', topic: 'Operations', unreadCount: 0, mentionCount: 0, lastActivity: '', members: [] },
  { id: 'dm-alice', name: 'dm-alice', topic: 'Direct messages with alice', unreadCount: 0, mentionCount: 0, lastActivity: '', members: ['alice', 'operator'] },
  { id: 'dm-retired-agent', name: 'dm-retired-agent', topic: 'Legacy DM', availability: 'unavailable', unreadCount: 0, mentionCount: 0, lastActivity: '', members: ['operator'] },
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

  it('shows active state for selected channel', () => {
    render(<ChannelSidebar channels={channels} selectedChannel={channels[0]} onSelect={() => {}} />);
    const buttons = screen.getAllByRole('button');
    const generalButton = buttons.find((btn) => btn.textContent?.includes('general'));
    expect(generalButton).toHaveClass('is-active');
  });

  it('renders DMs as compact rows without legacy agent pills', () => {
    render(
      <ChannelSidebar
        channels={channels}
        selectedChannel={null}
        onSelect={() => {}}
      />,
    );

    expect(screen.getByText('alice')).toBeInTheDocument();
    expect(screen.getByText('retired-agent')).toBeInTheDocument();
    expect(screen.queryByText('AGENT')).not.toBeInTheDocument();
    expect(screen.queryByText('LEGACY')).not.toBeInTheDocument();
    expect(screen.queryByText('UNAVAILABLE')).not.toBeInTheDocument();
  });

  it('does not render the inactive toggle as visible chrome', () => {
    const onToggleInactive = vi.fn();
    render(
      <ChannelSidebar
        channels={channels}
        selectedChannel={null}
        onSelect={() => {}}
        onCreateChannel={() => {}}
        onToggleInactive={onToggleInactive}
        showInactive={false}
      />,
    );

    expect(screen.queryByRole('button', { name: /show inactive/i })).not.toBeInTheDocument();
    expect(onToggleInactive).not.toHaveBeenCalled();
  });
});
