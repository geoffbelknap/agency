import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { AgencyMessage } from './AgencyMessage';
import type { Message } from '../../types';

const baseMessage: Message = {
  id: 'm1',
  channelId: 'general',
  author: 'scout',
  displayAuthor: 'scout',
  isAgent: true,
  isSystem: false,
  timestamp: '10:30',
  content: 'Hello **world**',
  flag: null,
};

function renderMsg(message: Message = baseMessage) {
  return render(
    <MemoryRouter>
      <AgencyMessage message={message} />
    </MemoryRouter>,
  );
}

describe('AgencyMessage', () => {
  it('renders author, timestamp, and markdown content', () => {
    renderMsg();

    expect(screen.getByText('scout')).toBeInTheDocument();
    expect(screen.getByText('10:30')).toBeInTheDocument();
    expect(screen.getByText('world')).toBeInTheDocument();
  });

  it('renders flag badges', () => {
    renderMsg({ ...baseMessage, flag: 'BLOCKER' });

    expect(screen.getByText('BLOCKER')).toBeInTheDocument();
  });

  it('renders configured tool call metadata', () => {
    renderMsg({
      ...baseMessage,
      metadata: {
        tool_calls: [
          { tool: 'knowledge.search', input: { q: 'field notes Q2', k: 20 }, output: '18 matches' },
        ],
      },
    });

    expect(screen.getByText(/knowledge\.search/)).toBeInTheDocument();
    expect(screen.getByText(/18 matches/)).toBeInTheDocument();
  });
});
