import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, fireEvent, render, screen } from '@testing-library/react';
import { PlatformReadyStep } from './PlatformReadyStep';
import { api } from '../../lib/api';

vi.mock('../../lib/api', () => ({
  api: {
    infra: {
      status: vi.fn(),
    },
    routing: {
      config: vi.fn(),
    },
  },
}));

describe('PlatformReadyStep', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.clearAllMocks();
  });

  it('shows a delayed continue option if platform checks are taking a while', async () => {
    vi.mocked(api.infra.status).mockReturnValue(new Promise(() => {}) as never);
    vi.mocked(api.routing.config).mockResolvedValue({ configured: false, version: 'test' } as never);
    const onComplete = vi.fn();

    render(<PlatformReadyStep onComplete={onComplete} />);

    expect(screen.queryByRole('button', { name: 'Continue without waiting' })).not.toBeInTheDocument();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(8000);
    });

    const button = screen.getByRole('button', { name: 'Continue without waiting' });
    fireEvent.click(button);

    expect(onComplete).toHaveBeenCalled();
  });
});
