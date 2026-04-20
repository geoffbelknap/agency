import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { server } from './server';
import { ComposeBar } from '../app/components/chat/ComposeBar';

vi.mock('../app/lib/ws', () => ({ socket: { on: () => () => {} } }));

const BASE = 'http://localhost:8200/api/v1';

describe('ComposeBar', () => {
  const defaultProps = {
    onSend: vi.fn(),
    channelName: 'general',
  };

  beforeEach(() => {
    vi.clearAllMocks();
    // MentionInput fetches agents for @-mention autocomplete; provide targets
    server.use(
      http.get(`${BASE}/agents`, () =>
        HttpResponse.json([{ name: 'scout', status: 'running' }]),
      ),
    );
  });

  it('renders input and send button', () => {
    render(<ComposeBar {...defaultProps} />);
    expect(screen.getByPlaceholderText('Message general...')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /send/i })).toBeInTheDocument();
  });

  it('does not render the old flag dropdown', () => {
    render(<ComposeBar {...defaultProps} />);
    expect(screen.queryByRole('button', { name: /set message flag/i })).not.toBeInTheDocument();
  });

  it('send button is disabled when input is empty', () => {
    render(<ComposeBar {...defaultProps} />);
    expect(screen.getByRole('button', { name: /send/i })).toBeDisabled();
  });

  it('send button is enabled when input has content', async () => {
    const user = userEvent.setup();
    render(<ComposeBar {...defaultProps} />);
    await user.type(screen.getByPlaceholderText('Message general...'), 'hello');
    expect(screen.getByRole('button', { name: /send/i })).not.toBeDisabled();
  });

  it('calls onSend with content on Enter key', async () => {
    const user = userEvent.setup();
    render(<ComposeBar {...defaultProps} />);
    const input = screen.getByPlaceholderText('Message general...');
    await user.type(input, 'hello world{Enter}');
    await waitFor(() => {
      expect(defaultProps.onSend).toHaveBeenCalledWith('hello world', undefined);
    });
  });

  it('clears input after send via Enter', async () => {
    const user = userEvent.setup();
    render(<ComposeBar {...defaultProps} />);
    const input = screen.getByPlaceholderText('Message general...');
    await user.type(input, 'hello{Enter}');
    await waitFor(() => {
      expect(input).toHaveValue('');
    });
  });

  it('calls onSend with content on send button click', async () => {
    const user = userEvent.setup();
    render(<ComposeBar {...defaultProps} />);
    await user.type(screen.getByPlaceholderText('Message general...'), 'hello');
    await user.click(screen.getByRole('button', { name: /send/i }));
    await waitFor(() => {
      expect(defaultProps.onSend).toHaveBeenCalledWith('hello', undefined);
    });
  });

  it('clears input after send via button click', async () => {
    const user = userEvent.setup();
    render(<ComposeBar {...defaultProps} />);
    const input = screen.getByPlaceholderText('Message general...');
    await user.type(input, 'hello');
    await user.click(screen.getByRole('button', { name: /send/i }));
    await waitFor(() => {
      expect(input).toHaveValue('');
    });
  });

  it('does not call onSend on Shift+Enter', async () => {
    const user = userEvent.setup();
    render(<ComposeBar {...defaultProps} />);
    const input = screen.getByPlaceholderText('Message general...');
    await user.type(input, 'hello');
    await user.keyboard('{Shift>}{Enter}{/Shift}');
    expect(defaultProps.onSend).not.toHaveBeenCalled();
  });

  it('does not call onSend when input is only whitespace', async () => {
    const user = userEvent.setup();
    render(<ComposeBar {...defaultProps} />);
    const input = screen.getByPlaceholderText('Message general...');
    await user.type(input, '   {Enter}');
    expect(defaultProps.onSend).not.toHaveBeenCalled();
  });

  it('supports @mention autocomplete via MentionInput', async () => {
    const user = userEvent.setup();
    render(<ComposeBar {...defaultProps} />);
    const input = screen.getByPlaceholderText('Message general...');
    await user.type(input, '@');
    // MentionInput shows a "Mentions" dropdown header when @ is typed and targets exist
    await waitFor(() => {
      expect(screen.getByText('Mentions')).toBeInTheDocument();
    });
  });

  it('uses channelName in placeholder', () => {
    render(<ComposeBar onSend={vi.fn()} channelName="ops-alerts" />);
    expect(screen.getByPlaceholderText('Message ops-alerts...')).toBeInTheDocument();
  });
});
