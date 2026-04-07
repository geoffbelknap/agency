import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { server } from './server';
import { ChannelBrowser } from '../app/components/chat/ChannelBrowser';

vi.mock('../app/lib/ws', () => ({ socket: { on: () => () => {} } }));

const BASE = 'http://localhost:8200/api/v1';

const defaultChannels = [
  { name: 'general', topic: 'General discussion', members: ['alice', 'bob'] },
  { name: 'engineering', topic: 'Engineering topics', members: ['alice'] },
  { name: 'design', topic: '', members: [] },
];

function renderBrowser(
  props: Partial<{
    open: boolean;
    onOpenChange: (v: boolean) => void;
    onJoinChannel: (name: string) => void;
  }> = {},
) {
  const defaultProps = {
    open: true,
    onOpenChange: vi.fn(),
    onJoinChannel: vi.fn(),
    ...props,
  };
  return { ...render(<ChannelBrowser {...defaultProps} />), ...defaultProps };
}

describe('ChannelBrowser', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    server.use(
      http.get(`${BASE}/comms/channels`, () => HttpResponse.json(defaultChannels)),
    );
  });

  it('lists channels when open', async () => {
    renderBrowser();
    await waitFor(() => {
      expect(screen.getByText('general')).toBeInTheDocument();
      expect(screen.getByText('engineering')).toBeInTheDocument();
      expect(screen.getByText('design')).toBeInTheDocument();
    });
  });

  it('shows channel topics', async () => {
    renderBrowser();
    await waitFor(() => {
      expect(screen.getByText('General discussion')).toBeInTheDocument();
      expect(screen.getByText('Engineering topics')).toBeInTheDocument();
    });
  });

  it('shows member counts', async () => {
    renderBrowser();
    await waitFor(() => {
      // general has 2 members, engineering has 1
      expect(screen.getByText('2 members')).toBeInTheDocument();
      expect(screen.getByText('1 member')).toBeInTheDocument();
    });
  });

  it('filters channels on search input', async () => {
    const user = userEvent.setup();
    renderBrowser();
    await waitFor(() => {
      expect(screen.getByText('general')).toBeInTheDocument();
    });
    await user.type(screen.getByPlaceholderText(/search/i), 'engin');
    expect(screen.getByText('engineering')).toBeInTheDocument();
    expect(screen.queryByText('general')).not.toBeInTheDocument();
    expect(screen.queryByText('design')).not.toBeInTheDocument();
  });

  it('calls onJoinChannel when a channel row is clicked', async () => {
    const user = userEvent.setup();
    const onJoinChannel = vi.fn();
    renderBrowser({ onJoinChannel });
    await waitFor(() => {
      expect(screen.getByText('general')).toBeInTheDocument();
    });
    await user.click(screen.getByRole('button', { name: /open general/i }));
    expect(onJoinChannel).toHaveBeenCalledWith('general');
  });

  it('shows loading skeleton while fetching', async () => {
    server.use(
      http.get(`${BASE}/comms/channels`, async () => {
        await new Promise((r) => setTimeout(r, 500));
        return HttpResponse.json(defaultChannels);
      }),
    );
    renderBrowser();
    // Skeleton elements should be visible before data arrives
    expect(screen.getAllByTestId('channel-skeleton').length).toBeGreaterThan(0);
  });

  it('does not render content when closed', () => {
    renderBrowser({ open: false });
    expect(screen.queryByPlaceholderText(/search/i)).not.toBeInTheDocument();
  });
});
