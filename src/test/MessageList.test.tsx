import { describe, it, expect, beforeAll, vi } from 'vitest';
import { fireEvent, render as baseRender, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { MessageList } from '../app/components/chat/MessageList';
import type { Message } from '../app/types';

function render(ui: React.ReactElement, opts?: any) { return baseRender(<MemoryRouter>{ui}</MemoryRouter>, opts); }

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

describe('MessageList', () => {
  it('renders a list of messages via AgencyMessage', () => {
    const messages: Message[] = [
      makeMessage({ id: 'msg-1', author: 'alice', displayAuthor: 'alice', content: 'Hello' }),
      makeMessage({ id: 'msg-2', author: 'bob', displayAuthor: 'bob', content: 'World' }),
    ];
    render(<MessageList messages={messages} loading={false} />);
    expect(screen.getAllByText('alice').length).toBeGreaterThan(0);
    expect(screen.getAllByText('bob').length).toBeGreaterThan(0);
    expect(screen.getByText('Hello')).toBeInTheDocument();
    expect(screen.getByText('World')).toBeInTheDocument();
  });

  it('shows loading skeleton when loading is true', () => {
    render(<MessageList messages={[]} loading={true} />);
    // Skeleton elements should be present, no messages or empty state
    const skeletons = document.querySelectorAll('[data-slot="skeleton"]');
    expect(skeletons.length).toBeGreaterThan(0);
    expect(screen.queryByText('No messages yet')).not.toBeInTheDocument();
  });

  it('shows empty state when not loading and no messages', () => {
    render(<MessageList messages={[]} loading={false} />);
    expect(screen.getByText('No messages yet')).toBeInTheDocument();
  });

  it('does not show empty state when messages are present', () => {
    const messages: Message[] = [makeMessage()];
    render(<MessageList messages={messages} loading={false} />);
    expect(screen.queryByText('No messages yet')).not.toBeInTheDocument();
  });

  it('auto-scrolls to bottom when messages change', () => {
    const messages: Message[] = [makeMessage({ id: 'msg-1', content: 'First' })];
    const { rerender } = render(<MessageList messages={messages} loading={false} />);

    const newMessages: Message[] = [
      ...messages,
      makeMessage({ id: 'msg-2', content: 'Second' }),
    ];

    // scrollIntoView is mocked — just verify re-render with new messages doesn't throw
    // and the new message appears
    rerender(<MemoryRouter><MessageList messages={newMessages} loading={false} /></MemoryRouter>);
    expect(screen.getByText('Second')).toBeInTheDocument();
  });

  it('calls onLoadMore when wheeling up near the top', () => {
    const onLoadMore = vi.fn();
    const originalClientHeight = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'clientHeight');
    const originalScrollHeight = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'scrollHeight');
    const originalScrollTop = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'scrollTop');

    try {
      Object.defineProperty(HTMLElement.prototype, 'clientHeight', {
        configurable: true,
        get() {
          return 100;
        },
      });

      Object.defineProperty(HTMLElement.prototype, 'scrollHeight', {
        configurable: true,
        get() {
          return 300;
        },
      });

      let currentScrollTop = 200;
      Object.defineProperty(HTMLElement.prototype, 'scrollTop', {
        configurable: true,
        get() {
          return currentScrollTop;
        },
        set(value) {
          currentScrollTop = value;
        },
      });

      const messages: Message[] = [makeMessage({ id: 'msg-1', content: 'First' })];
      const { container } = render(
        <MessageList
          messages={messages}
          loading={false}
          hasMore={true}
          loadingMore={false}
          onLoadMore={onLoadMore}
        />,
      );

      const viewport = container.querySelector('[data-slot="scroll-area-viewport"]') as HTMLDivElement;
      // Establish a first scroll event so firstRenderRef is cleared.
      currentScrollTop = 100;
      fireEvent.scroll(viewport);
      fireEvent.wheel(viewport, { deltaY: -100 });
      expect(onLoadMore).toHaveBeenCalledTimes(1);
    } finally {
      if (originalClientHeight) {
        Object.defineProperty(HTMLElement.prototype, 'clientHeight', originalClientHeight);
      }
      if (originalScrollHeight) {
        Object.defineProperty(HTMLElement.prototype, 'scrollHeight', originalScrollHeight);
      }
      if (originalScrollTop) {
        Object.defineProperty(HTMLElement.prototype, 'scrollTop', originalScrollTop);
      }
    }
  });

  it('does not call onLoadMore when wheeling down near the top', () => {
    const onLoadMore = vi.fn();
    const originalClientHeight = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'clientHeight');
    const originalScrollHeight = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'scrollHeight');
    const originalScrollTop = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'scrollTop');

    try {
      Object.defineProperty(HTMLElement.prototype, 'clientHeight', {
        configurable: true,
        get() {
          return 100;
        },
      });

      Object.defineProperty(HTMLElement.prototype, 'scrollHeight', {
        configurable: true,
        get() {
          return 300;
        },
      });

      let currentScrollTop = 50;
      Object.defineProperty(HTMLElement.prototype, 'scrollTop', {
        configurable: true,
        get() {
          return currentScrollTop;
        },
        set(value) {
          currentScrollTop = value;
        },
      });

      const messages: Message[] = [makeMessage({ id: 'msg-1', content: 'First' })];
      const { container, rerender } = render(
        <MessageList
          messages={messages}
          loading={false}
          hasMore={false}
          loadingMore={false}
          onLoadMore={onLoadMore}
        />,
      );

      const viewport = container.querySelector('[data-slot="scroll-area-viewport"]') as HTMLDivElement;
      // Establish a near-top baseline scroll position without triggering load-more.
      currentScrollTop = 50;
      fireEvent.scroll(viewport);

      rerender(
        <MemoryRouter><MessageList
          messages={messages}
          loading={false}
          hasMore={true}
          loadingMore={false}
          onLoadMore={onLoadMore}
        /></MemoryRouter>,
      );

      onLoadMore.mockClear();
      currentScrollTop = 100;
      fireEvent.scroll(viewport);
      fireEvent.wheel(viewport, { deltaY: 100 });
      expect(onLoadMore).not.toHaveBeenCalled();
    } finally {
      if (originalClientHeight) {
        Object.defineProperty(HTMLElement.prototype, 'clientHeight', originalClientHeight);
      }
      if (originalScrollHeight) {
        Object.defineProperty(HTMLElement.prototype, 'scrollHeight', originalScrollHeight);
      }
      if (originalScrollTop) {
        Object.defineProperty(HTMLElement.prototype, 'scrollTop', originalScrollTop);
      }
    }
  });

  it('does not call onLoadMore immediately on initial render', () => {
    const onLoadMore = vi.fn();
    const originalClientHeight = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'clientHeight');
    const originalScrollHeight = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'scrollHeight');
    const originalScrollTop = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'scrollTop');

    try {
      Object.defineProperty(HTMLElement.prototype, 'clientHeight', {
        configurable: true,
        get() {
          return 100;
        },
      });

      Object.defineProperty(HTMLElement.prototype, 'scrollHeight', {
        configurable: true,
        get() {
          return 300;
        },
      });

      Object.defineProperty(HTMLElement.prototype, 'scrollTop', {
        configurable: true,
        get() {
          return 0;
        },
        set() {},
      });

      render(
        <MessageList
          messages={[makeMessage({ id: 'msg-1', content: 'First' })]}
          loading={false}
          hasMore={true}
          loadingMore={false}
          onLoadMore={onLoadMore}
        />,
      );

      expect(onLoadMore).not.toHaveBeenCalled();
    } finally {
      if (originalClientHeight) {
        Object.defineProperty(HTMLElement.prototype, 'clientHeight', originalClientHeight);
      }
      if (originalScrollHeight) {
        Object.defineProperty(HTMLElement.prototype, 'scrollHeight', originalScrollHeight);
      }
      if (originalScrollTop) {
        Object.defineProperty(HTMLElement.prototype, 'scrollTop', originalScrollTop);
      }
    }
  });

  it('keeps loading-earlier banner hidden for automatic loadingMore state', () => {
    render(
      <MessageList
        messages={[makeMessage({ id: 'msg-1', content: 'First' })]}
        loading={false}
        hasMore={true}
        loadingMore={true}
        onLoadMore={vi.fn()}
      />,
    );

    expect(screen.queryByText('Loading earlier messages...')).not.toBeInTheDocument();
  });
});
