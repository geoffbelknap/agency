import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { server } from './server';
import { renderWithRouter } from './render';
import { CreateAgentDialog } from '../app/components/CreateAgentDialog';
import { toast } from 'sonner';

vi.mock('../app/lib/ws', () => ({ socket: { on: () => () => {} } }));
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

const BASE = 'http://localhost:8200/api/v1';

function renderDialog(props: Partial<{ open: boolean; onOpenChange: (v: boolean) => void; onCreated: () => void }> = {}) {
  const defaultProps = {
    open: true,
    onOpenChange: vi.fn(),
    onCreated: vi.fn(),
    ...props,
  };
  return { ...renderWithRouter(<CreateAgentDialog {...defaultProps} />), ...defaultProps };
}

describe('CreateAgentDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders form fields when open', async () => {
    renderDialog();
    await waitFor(() => {
      expect(screen.getByLabelText(/name/i)).toBeInTheDocument();
    });
    expect(screen.getByText(/preset/i)).toBeInTheDocument();
    expect(screen.getByText(/mode/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /create/i })).toBeInTheDocument();
  });

  it('fetches presets on open', async () => {
    renderDialog();
    // Radix Select renders both a visible span and a hidden native <option> — use getAllByText
    await waitFor(() => {
      expect(screen.getAllByText(/generalist/i).length).toBeGreaterThan(0);
    });
  });

  it('shows inline error for name too short', async () => {
    const user = userEvent.setup();
    renderDialog();
    const nameInput = await screen.findByLabelText(/name/i);
    await user.type(nameInput, 'a');
    await user.click(screen.getByRole('button', { name: /create/i }));
    await waitFor(() => {
      expect(screen.getByText(/at least 2 characters/i)).toBeInTheDocument();
    });
  });

  it('auto-corrects invalid name characters to hyphens', async () => {
    const user = userEvent.setup();
    renderDialog();
    const nameInput = await screen.findByLabelText(/name/i);
    await user.type(nameInput, 'Agent_One');
    // Input auto-corrects: lowercases and replaces invalid chars with hyphens
    expect(nameInput).toHaveValue('agent-one');
  });

  it('shows inline error for reserved name', async () => {
    const user = userEvent.setup();
    renderDialog();
    const nameInput = await screen.findByLabelText(/name/i);
    await user.type(nameInput, 'gateway');
    await user.click(screen.getByRole('button', { name: /create/i }));
    await waitFor(() => {
      expect(screen.getByText(/reserved/i)).toBeInTheDocument();
    });
  });

  it('calls API and closes on successful creation', async () => {
    const user = userEvent.setup();
    server.use(
      http.post(`${BASE}/agents`, async ({ request }) => {
        const body = await request.json() as any;
        return HttpResponse.json({ status: 'created', name: body.name }, { status: 201 });
      }),
    );
    const { onCreated, onOpenChange } = renderDialog();
    const nameInput = await screen.findByLabelText(/name/i);
    await user.type(nameInput, 'test-agent');
    await user.click(screen.getByRole('button', { name: /create/i }));
    await waitFor(() => {
      expect(onCreated).toHaveBeenCalled();
    });
    expect(toast.success).toHaveBeenCalledWith(expect.stringContaining('created'));
  });

  it('shows error toast on API failure', async () => {
    const user = userEvent.setup();
    server.use(
      http.post(`${BASE}/agents`, () =>
        HttpResponse.json({ error: 'agent "test-agent" already exists' }, { status: 409 }),
      ),
    );
    const { onCreated } = renderDialog();
    const nameInput = await screen.findByLabelText(/name/i);
    await user.type(nameInput, 'test-agent');
    await user.click(screen.getByRole('button', { name: /create/i }));
    await waitFor(() => {
      expect(toast.error).toHaveBeenCalled();
    });
    expect(onCreated).not.toHaveBeenCalled();
  });

  it('falls back to text input when preset fetch fails', async () => {
    server.use(
      http.get(`${BASE}/hub/presets`, () => HttpResponse.error()),
    );
    renderDialog();
    await waitFor(() => {
      expect(screen.getByLabelText(/preset/i)).toBeInTheDocument();
    });
    // Should be a text input, not a select
    const presetInput = screen.getByLabelText(/preset/i);
    expect(presetInput.tagName).toBe('INPUT');
  });
});
