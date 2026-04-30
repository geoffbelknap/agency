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

  it('falls back to a recoverable error instead of spinning forever when checks hang', async () => {
    vi.mocked(api.infra.status).mockReturnValue(new Promise(() => {}) as never);
    vi.mocked(api.routing.config).mockResolvedValue({ configured: false, version: 'test' } as never);
    const onComplete = vi.fn();

    render(<PlatformReadyStep onComplete={onComplete} />);

    expect(screen.queryByRole('button', { name: 'Continue anyway' })).not.toBeInTheDocument();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(8000);
    });

    expect(screen.getByText(/gateway check timed out/i)).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Continue anyway' }));

    expect(onComplete).toHaveBeenCalled();
  });
});
