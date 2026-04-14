import { describe, it, expect, vi, beforeAll, afterEach } from 'vitest';
import { render, screen, waitFor, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { server } from './server';
import { SearchPanel } from '../app/components/chat/SearchPanel';

vi.mock('../app/lib/ws', () => ({ socket: { on: () => () => {} } }));

beforeAll(() => {
  window.HTMLElement.prototype.scrollIntoView = () => {};
});

afterEach(() => {
  vi.useRealTimers();
});

const defaultProps = {
  onClose: vi.fn(),
  onJumpToMessage: vi.fn(),
};

describe('SearchPanel', () => {
  it('renders search input', () => {
    render(<SearchPanel {...defaultProps} />);
    expect(screen.getByPlaceholderText(/search messages/i)).toBeInTheDocument();
  });

  it('shows "Search messages..." empty state when no query', () => {
    render(<SearchPanel {...defaultProps} />);
    expect(screen.getByText('Search messages')).toBeInTheDocument();
    expect(screen.getByText(/use keywords, agent names, or decision text/i)).toBeInTheDocument();
  });

  it('shows "Search" header with close button', () => {
    render(<SearchPanel {...defaultProps} />);
    expect(screen.getByText('Search')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /close search/i })).toBeInTheDocument();
  });

  it('close button calls onClose', async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    render(<SearchPanel {...defaultProps} onClose={onClose} />);
    await user.click(screen.getByRole('button', { name: /close search/i }));
    expect(onClose).toHaveBeenCalledOnce();
  });

  it('shows loading state while searching', async () => {
    // Use a delayed response so we can observe loading state
    server.use(
      http.get('*/channels/search*', async () => {
        await new Promise((r) => setTimeout(r, 500));
        return HttpResponse.json([]);
      }),
    );
    const user = userEvent.setup({ delay: null });
    render(<SearchPanel {...defaultProps} />);
    const input = screen.getByPlaceholderText(/search messages/i);
    await user.type(input, 'hello');
    // Wait for debounce (300ms) to pass and loading to begin
    await waitFor(
      () => {
        expect(screen.getByText(/searching/i)).toBeInTheDocument();
      },
      { timeout: 2000 },
    );
  });

  it('shows results with channel, author, and content', async () => {
    server.use(
      http.get('*/channels/search*', () =>
        HttpResponse.json([
          {
            id: 'msg-1',
            channel: 'general',
            author: 'alice',
            timestamp: '2026-03-18T12:00:00Z',
            content: 'Hello from alice',
            flags: {},
            metadata: {},
          },
        ]),
      ),
    );
    const user = userEvent.setup({ delay: null });
    render(<SearchPanel {...defaultProps} />);
    await user.type(screen.getByPlaceholderText(/search messages/i), 'alice');
    await waitFor(
      () => {
        expect(screen.getByText('#general')).toBeInTheDocument();
        // Author name appears in result header
        expect(screen.getAllByText('alice').length).toBeGreaterThan(0);
        // Content snippet appears (may be split by highlight mark elements)
        expect(screen.getByRole('button', { name: (n) => /hello from alice/i.test(n) })).toBeInTheDocument();
      },
      { timeout: 2000 },
    );
  });

  it('clicking a result calls onJumpToMessage with channel and message id', async () => {
    server.use(
      http.get('*/channels/search*', () =>
        HttpResponse.json([
          {
            id: 'msg-42',
            channel: 'general',
            author: 'alice',
            timestamp: '2026-03-18T12:00:00Z',
            content: 'Jump to me',
            flags: {},
            metadata: {},
          },
        ]),
      ),
    );
    const user = userEvent.setup({ delay: null });
    const onJumpToMessage = vi.fn();
    render(<SearchPanel {...defaultProps} onJumpToMessage={onJumpToMessage} />);
    await user.type(screen.getByPlaceholderText(/search messages/i), 'jump');
    let resultButton: HTMLElement;
    await waitFor(
      () => {
        // The result is a button; find it by its accessible text (may be split by <mark>)
        resultButton = screen.getByRole('button', {
          name: (name) => /jump to me/i.test(name),
        });
        expect(resultButton).toBeInTheDocument();
      },
      { timeout: 2000 },
    );
    await user.click(resultButton!);
    expect(onJumpToMessage).toHaveBeenCalledWith('general', 'msg-42');
  });

  it('shows "No results" for empty response', async () => {
    server.use(
      http.get('*/channels/search*', () => HttpResponse.json([])),
    );
    const user = userEvent.setup({ delay: null });
    render(<SearchPanel {...defaultProps} />);
    await user.type(screen.getByPlaceholderText(/search messages/i), 'nope');
    await waitFor(
      () => {
        expect(screen.getByText(/no results/i)).toBeInTheDocument();
      },
      { timeout: 2000 },
    );
  });
});
