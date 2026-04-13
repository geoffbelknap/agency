import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, fireEvent, render, screen } from '@testing-library/react';
import { HubSyncStep } from './HubSyncStep';
import { api } from '../../lib/api';

vi.mock('../../lib/api', () => ({
  api: {
    hub: {
      update: vi.fn(),
      upgrade: vi.fn(),
    },
  },
}));

describe('HubSyncStep', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.clearAllMocks();
  });

  it('shows a delayed continue option if hub sync is taking a while', async () => {
    vi.mocked(api.hub.update).mockReturnValue(new Promise(() => {}));
    vi.mocked(api.hub.upgrade).mockResolvedValue({} as never);
    const onComplete = vi.fn();

    render(<HubSyncStep onComplete={onComplete} />);

    expect(screen.queryByRole('button', { name: 'Continue without sync' })).not.toBeInTheDocument();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(8000);
    });

    const button = screen.getByRole('button', { name: 'Continue without sync' });
    fireEvent.click(button);

    expect(onComplete).toHaveBeenCalled();
  });
});
