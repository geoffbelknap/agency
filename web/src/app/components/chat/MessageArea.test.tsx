import { describe, it, expect, vi, beforeAll } from 'vitest';
import { render as baseRender, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { http, HttpResponse } from 'msw';
import { server } from '../../../test/server';
import { MessageArea } from './MessageArea';
import type { Channel, Message } from '../../types';

function render(ui: React.ReactElement, opts?: any) { return baseRender(<MemoryRouter>{ui}</MemoryRouter>, opts); }

const BASE = 'http://localhost:8200/api/v1';

beforeAll(() => {
  window.HTMLElement.prototype.scrollIntoView = () => {};
  // MentionInput calls api.agents.list() on mount
  server.use(http.get(`${BASE}/agents`, () => HttpResponse.json([])));
});

const channel: Channel = {
  id: 'general', name: 'general', topic: 'General chat',
  unreadCount: 0, mentionCount: 0, lastActivity: '', members: ['scout', 'analyst'],
};

const messages: Message[] = [
  { id: 'm1', channelId: 'general', author: 'scout', displayAuthor: 'scout', isAgent: true, isSystem: false, timestamp: '10:30', content: 'Hello', flag: null },
  { id: 'm2', channelId: 'general', author: 'operator', displayAuthor: 'operator', isAgent: false, isSystem: false, timestamp: '10:31', content: 'Hi there', flag: null },
];

describe('MessageArea', () => {
  it('renders channel header with name and topic', () => {
    render(<MessageArea channel={channel} messages={messages} loading={false} onSend={() => {}} />);
    expect(screen.getByText('general')).toBeInTheDocument();
    expect(screen.getByText('General chat')).toBeInTheDocument();
  });

  it('renders all messages', () => {
    render(<MessageArea channel={channel} messages={messages} loading={false} onSend={() => {}} />);
    expect(screen.getAllByText('scout').length).toBeGreaterThan(0);
    expect(screen.getByText('Hello')).toBeInTheDocument();
    expect(screen.getByText('Hi there')).toBeInTheDocument();
  });

  it('shows member count', () => {
    render(<MessageArea channel={{ ...channel, topic: '' }} messages={messages} loading={false} onSend={() => {}} />);
    expect(screen.getByText('2 members')).toBeInTheDocument();
  });

  it('shows loading state', () => {
    render(<MessageArea channel={channel} messages={[]} loading={true} onSend={() => {}} />);
    // Loading state now shows skeleton elements (not text)
    const skeletons = document.querySelectorAll('[data-slot="skeleton"]');
    expect(skeletons.length).toBeGreaterThan(0);
  });
});
