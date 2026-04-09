import { describe, it, expect, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ConfirmDialog } from './ConfirmDialog';

describe('ConfirmDialog', () => {
  it('renders title and description when open', () => {
    render(
      <ConfirmDialog
        open={true}
        onOpenChange={() => {}}
        title="Delete agent?"
        description="This cannot be undone."
        onConfirm={() => {}}
      />,
    );
    expect(screen.getByText('Delete agent?')).toBeInTheDocument();
    expect(screen.getByText('This cannot be undone.')).toBeInTheDocument();
  });

  it('calls onConfirm when confirm button is clicked', async () => {
    const onConfirm = vi.fn();
    render(
      <ConfirmDialog
        open={true}
        onOpenChange={() => {}}
        title="Delete?"
        description="Sure?"
        confirmLabel="Yes, delete"
        onConfirm={onConfirm}
      />,
    );
    await userEvent.click(screen.getByText('Yes, delete'));
    expect(onConfirm).toHaveBeenCalledOnce();
  });

  it('keeps async confirmations pending until they finish', async () => {
    const onOpenChange = vi.fn();
    let resolveConfirm!: () => void;
    const onConfirm = vi.fn(() => new Promise<void>((resolve) => { resolveConfirm = resolve; }));

    render(
      <ConfirmDialog
        open={true}
        onOpenChange={onOpenChange}
        title="Destroy?"
        description="This cannot be undone."
        confirmLabel="Destroy Everything"
        onConfirm={onConfirm}
      />,
    );

    await userEvent.click(screen.getByRole('button', { name: 'Destroy Everything' }));

    expect(onConfirm).toHaveBeenCalledOnce();
    expect(screen.getByRole('button', { name: 'Working...' })).toBeDisabled();
    expect(onOpenChange).not.toHaveBeenCalled();

    resolveConfirm();

    await waitFor(() => {
      expect(onOpenChange).toHaveBeenCalledWith(false);
    });
  });

  it('does not render when closed', () => {
    render(
      <ConfirmDialog
        open={false}
        onOpenChange={() => {}}
        title="Delete?"
        description="Sure?"
        onConfirm={() => {}}
      />,
    );
    expect(screen.queryByText('Delete?')).not.toBeInTheDocument();
  });
});
