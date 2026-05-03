import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
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
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn(async (url: RequestInfo | URL) => {
      const target = String(url);
      if (target.includes('/metadata')) {
        return new Response(JSON.stringify({
          task_id: 'task-1',
          has_metadata: true,
          pact: {
            kind: 'current_info',
            verdict: 'completed',
            source_urls: ['https://nodejs.org/en/blog/release/v24.15.0'],
          },
        }), { status: 200, headers: { 'Content-Type': 'application/json' } });
      }
      return new Response('{}', { status: 200, headers: { 'Content-Type': 'application/json' } });
    }));
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

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

  it('renders PACT status for artifact metadata', async () => {
    const msg: Message = {
      ...baseMessage,
      metadata: { has_artifact: true, agent: 'scout', task_id: 'task-1' },
    };
    renderMsg({ message: msg });
    expect(await screen.findByText('completed')).toBeInTheDocument();
    expect(screen.getByText('1 source')).toBeInTheDocument();
    expect(fetch).toHaveBeenCalledWith(expect.stringContaining('/api/v1/agents/scout/results/task-1/metadata'), expect.any(Object));
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

  it('extracts pseudo search markup into compact tool cards', () => {
    const msg: Message = {
      ...baseMessage,
      content: 'I will check.\n\n<search> query: Microsoft latest SEC filing </search>\n\nThe latest filing is here.',
    };
    renderMsg({ message: msg });
    expect(screen.getByText(/web\.search/)).toBeInTheDocument();
    expect(screen.getByText(/Microsoft latest SEC filing/)).toBeInTheDocument();
    expect(screen.queryByText(/<search>/)).not.toBeInTheDocument();
    expect(screen.getByText(/The latest filing is here/)).toBeInTheDocument();
  });

  it('renders generic metadata links as attachment chips', () => {
    const msg: Message = {
      ...baseMessage,
      metadata: {
        attachments: [{ label: 'sec-filing.md', url: '/api/v1/agents/scout/results/sec-filing?download=true' }],
      },
    };
    renderMsg({ message: msg });
    expect(screen.getByRole('link', { name: /sec-filing\.md/ })).toHaveAttribute('href', '/api/v1/agents/scout/results/sec-filing?download=true');
  });

  it('renders blocker markdown as a list with wrapped links', () => {
    const msg: Message = {
      ...baseMessage,
      content: [
        'I cannot verify this from an official/current source without guessing.',
        '',
        '- Blocked: Available source URLs did not satisfy the official/current-source evidence contract.',
        '- Evidence checked: tools=provider-web-search',
        '- Source URLs observed:',
        '  - https://nodejs.org/en/blog/release/v25.9.0',
        '- Checked: April 22, 2026.',
      ].join('\n'),
    };
    const { container } = renderMsg({ message: msg });

    expect(screen.getByText(/Blocked:/)).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'https://nodejs.org/en/blog/release/v25.9.0' })).toBeInTheDocument();
    expect(container.querySelector('.break-words')).toBeInTheDocument();
  });
});
