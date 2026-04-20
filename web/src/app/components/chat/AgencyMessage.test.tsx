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

function renderMsg(props: Partial<Parameters<typeof AgencyMessage>[0]> = {}) {
  return render(
    <MemoryRouter>
      <AgencyMessage message={baseMessage} {...props} />
    </MemoryRouter>,
  );
}

describe('AgencyMessage', () => {
  it('renders author, timestamp, and markdown content', () => {
    renderMsg();
    expect(screen.getByText('scout')).toBeInTheDocument();
    expect(screen.getByText('10:30')).toBeInTheDocument();
    expect(screen.getByText('world')).toBeInTheDocument(); // bold rendered
  });

  it('does not show legacy AGENT badge for agent messages', () => {
    renderMsg();
    expect(screen.queryByText('AGENT')).not.toBeInTheDocument();
  });

  it('does not show AGENT badge for operator messages', () => {
    renderMsg({ message: { ...baseMessage, author: 'operator', displayAuthor: 'operator', isAgent: false, isSystem: false } });
    expect(screen.queryByText('AGENT')).not.toBeInTheDocument();
  });

  it('renders flag badges', () => {
    renderMsg({ message: { ...baseMessage, flag: 'BLOCKER' } });
    expect(screen.getByText('BLOCKER')).toBeInTheDocument();
  });

  it('renders artifact links when metadata present', () => {
    const msg: Message = {
      ...baseMessage,
      metadata: { has_artifact: true, agent: 'scout', task_id: 'task-1' },
    };
    renderMsg({ message: msg });
    expect(screen.getByText('View full report')).toBeInTheDocument();
    expect(screen.getByText('Download .md')).toBeInTheDocument();
  });

  it('renders tool calls as compact terminal pills', () => {
    const msg: Message = {
      ...baseMessage,
      metadata: {
        tool_calls: [
          { tool: 'knowledge.search', input: { q: 'field notes Q2', k: 20 }, output: '18 matches' },
        ],
      },
    };
    renderMsg({ message: msg });
    expect(screen.getByText(/knowledge\.search/)).toBeInTheDocument();
    expect(screen.getByText(/18 matches/)).toBeInTheDocument();
    expect(screen.queryByText(/ran/)).not.toBeInTheDocument();
  });
});
