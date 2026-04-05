import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { server } from './server';
import { renderWithRouter } from './render';
import { CreateChannelDialog } from '../app/components/chat/CreateChannelDialog';
import { toast } from 'sonner';

vi.mock('../app/lib/ws', () => ({ socket: { on: () => () => {} } }));
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

const BASE = 'http://localhost:8200/api/v1';

function renderDialog(
  props: Partial<{
    open: boolean;
    onOpenChange: (v: boolean) => void;
    onCreated: () => void;
  }> = {},
) {
  const defaultProps = {
    open: true,
    onOpenChange: vi.fn(),
    onCreated: vi.fn(),
    ...props,
  };
  return { ...renderWithRouter(<CreateChannelDialog {...defaultProps} />), ...defaultProps };
}

describe('CreateChannelDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders name and topic inputs when open', async () => {
    renderDialog();
    await waitFor(() => {
      expect(screen.getByLabelText(/name/i)).toBeInTheDocument();
    });
    expect(screen.getByLabelText(/topic/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /create/i })).toBeInTheDocument();
  });

  it('does not render when closed', () => {
    renderDialog({ open: false });
    expect(screen.queryByLabelText(/name/i)).not.toBeInTheDocument();
  });

  it('shows validation error for name too short', async () => {
    const user = userEvent.setup();
    renderDialog();
    const nameInput = await screen.findByLabelText(/name/i);
    await user.type(nameInput, 'a');
    await user.click(screen.getByRole('button', { name: /create/i }));
    await waitFor(() => {
      expect(screen.getByText(/at least 2 characters/i)).toBeInTheDocument();
    });
  });

  it('shows validation error for invalid name pattern', async () => {
    const user = userEvent.setup();
    renderDialog();
    const nameInput = await screen.findByLabelText(/name/i);
    await user.type(nameInput, 'My Channel');
    await user.click(screen.getByRole('button', { name: /create/i }));
    await waitFor(() => {
      expect(screen.getByText(/lowercase alphanumeric/i)).toBeInTheDocument();
    });
  });

  it('calls api.channels.create on submit with valid data', async () => {
    const user = userEvent.setup();
    let capturedBody: unknown;
    server.use(
      http.post(`${BASE}/channels`, async ({ request }) => {
        capturedBody = await request.json();
        return HttpResponse.json({ ok: true }, { status: 201 });
      }),
    );
    renderDialog();
    const nameInput = await screen.findByLabelText(/name/i);
    const topicInput = screen.getByLabelText(/topic/i);
    await user.type(nameInput, 'my-channel');
    await user.type(topicInput, 'A test topic');
    await user.click(screen.getByRole('button', { name: /create/i }));
    await waitFor(() => {
      expect(capturedBody).toMatchObject({ name: 'my-channel', topic: 'A test topic' });
    });
  });

  it('closes and calls onCreated on success', async () => {
    const user = userEvent.setup();
    server.use(
      http.post(`${BASE}/channels`, () =>
        HttpResponse.json({ ok: true }, { status: 201 }),
      ),
    );
    const { onCreated, onOpenChange } = renderDialog();
    const nameInput = await screen.findByLabelText(/name/i);
    await user.type(nameInput, 'my-channel');
    await user.click(screen.getByRole('button', { name: /create/i }));
    await waitFor(() => {
      expect(onCreated).toHaveBeenCalled();
    });
    expect(onOpenChange).toHaveBeenCalledWith(false);
    expect(toast.success).toHaveBeenCalledWith(expect.stringContaining('Channel created'));
  });

  it('shows error toast on failure and stays open', async () => {
    const user = userEvent.setup();
    server.use(
      http.post(`${BASE}/channels`, () =>
        HttpResponse.json({ error: 'channel already exists' }, { status: 409 }),
      ),
    );
    const { onCreated, onOpenChange } = renderDialog();
    const nameInput = await screen.findByLabelText(/name/i);
    await user.type(nameInput, 'my-channel');
    await user.click(screen.getByRole('button', { name: /create/i }));
    await waitFor(() => {
      expect(toast.error).toHaveBeenCalled();
    });
    expect(onCreated).not.toHaveBeenCalled();
    expect(onOpenChange).not.toHaveBeenCalledWith(false);
  });
});
