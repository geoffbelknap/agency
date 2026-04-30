import { describe, it, expect, beforeAll } from 'vitest';
import { render as baseRender, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { http, HttpResponse } from 'msw';
import { server } from '../../../test/server';
import { MessageArea } from './MessageArea';
import type { Channel, Message } from '../../types';

function render(ui: React.ReactElement, opts?: any) {
  return baseRender(<MemoryRouter>{ui}</MemoryRouter>, opts);
}

const BASE = 'http://localhost:8200/api/v1';

beforeAll(() => {
  window.HTMLElement.prototype.scrollIntoView = () => {};
  server.use(http.get(`${BASE}/agents`, () => HttpResponse.json([])));
});

const channel: Channel = {
  id: 'general',
  name: 'general',
  topic: 'General chat',
  unreadCount: 0,
  mentionCount: 0,
  lastActivity: '',
  members: ['scout', 'analyst'],
};

const messages: Message[] = [
  { id: 'm1', channelId: 'general', author: 'scout', displayAuthor: 'scout', isAgent: true, isSystem: false, timestamp: '10:30', content: 'Hello', flag: null },
  { id: 'm2', channelId: 'general', author: 'operator', displayAuthor: 'operator', isAgent: false, isSystem: false, timestamp: '10:31', content: 'Hi there', flag: null },
];

describe('MessageArea', () => {
  it('renders channel header and messages', () => {
    render(<MessageArea channel={channel} messages={messages} loading={false} onSend={() => {}} />);

    expect(screen.getByText('general')).toBeInTheDocument();
    expect(screen.getByText('General chat')).toBeInTheDocument();
    expect(screen.getByText('Hello')).toBeInTheDocument();
    expect(screen.getByText('Hi there')).toBeInTheDocument();
  });

  it('shows loading state', () => {
    render(<MessageArea channel={channel} messages={[]} loading={true} onSend={() => {}} />);

    expect(document.querySelectorAll('[data-slot="skeleton"]').length).toBeGreaterThan(0);
  });
});
