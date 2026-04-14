import { render, screen, fireEvent } from '@testing-library/react';
import { ChannelItem } from '../app/components/chat/ChannelItem';

describe('ChannelItem', () => {
  const channel = {
    id: 'general',
    name: 'general',
    topic: 'General discussion',
    unreadCount: 3,
    mentionCount: 1,
    lastActivity: '',
    members: ['researcher', 'engineer'],
  };

  it('renders channel name and topic', () => {
    render(<ChannelItem channel={channel} active={false} onClick={() => {}} />);
    expect(screen.getByText('general')).toBeInTheDocument();
    expect(screen.getByText('General discussion')).toBeInTheDocument();
  });

  it('shows unread badge when unreadCount > 0', () => {
    render(<ChannelItem channel={channel} active={false} onClick={() => {}} />);
    expect(screen.getByText('3')).toBeInTheDocument();
  });

  it('shows mention badge when mentionCount > 0', () => {
    render(<ChannelItem channel={channel} active={false} onClick={() => {}} />);
    expect(screen.getByText('@1')).toBeInTheDocument();
  });

  it('hides badges when counts are 0', () => {
    const noUnreads = { ...channel, unreadCount: 0, mentionCount: 0 };
    render(<ChannelItem channel={noUnreads} active={false} onClick={() => {}} />);
    expect(screen.queryByText('0')).not.toBeInTheDocument();
  });

  it('applies active styling when active', () => {
    render(<ChannelItem channel={channel} active={true} onClick={() => {}} />);
    expect(screen.getByRole('button')).toHaveClass('bg-accent/80');
  });

  it('calls onClick when clicked', () => {
    const onClick = vi.fn();
    render(<ChannelItem channel={channel} active={false} onClick={onClick} />);
    fireEvent.click(screen.getByText('general'));
    expect(onClick).toHaveBeenCalled();
  });
});
